import type { Bridge } from './bridge.ts';
import type { Json } from './config.ts';
import { digest } from './config.ts';
import { ApiError } from './api.ts';

export class Revocations {
  b: Bridge;
  constructor(b: Bridge) { this.b = b; }
  get session() { return digest(this.b.c.session); }
  retry(e: unknown): never {
    if (e instanceof ApiError && (e.retry || e.uncertain)) throw new ApiError('Telegram deletion handling will retry', e.status, true, false, e.delay);
    throw e;
  }
  queue(id: string, step: string, due = 0) {
    const key = `control-revoke:${id}:${step}`;
    this.b.store.enqueue(key, key, 'control-revoke', { id, step }, due);
  }
  ownMessage(chat: string, id: string, rows: Json[]) {
    const saved = this.b.store.get('wamessage', `${chat}:${id}`);
    if (rows.some(row => row.direction === 'out') || saved?.fromMe) return true;
    if (saved?.fromMe === false) return false;
    // Existing phone-originated messages predate the stored fromMe flag.
    return this.b.store.db.prepare("SELECT payload FROM jobs WHERE type='wa' AND json_extract(payload,'$.data.id')=?").all(id).some(row => {
      const event = JSON.parse(row.payload as string);
      return ['message.received', 'message.sent'].includes(event.event) && this.b.store.canonical(event.data.chatId || '') === chat && event.data.fromMe === true;
    });
  }
  async notify(chat: string, message: string) {
    const rows = this.b.store.byWhatsApp(chat, message), own = this.ownMessage(chat, message, rows);
    const destinations = new Map<string, Json[]>();
    for (const row of rows) {
      const scope = this.b.control.scope(row.bot, row.tg_chat);
      const recorded = this.b.store.db.prepare('SELECT thread,deleted FROM telegram_messages WHERE scope=? AND chat=? AND message=?').get(scope, row.tg_chat, row.tg_id);
      if (recorded?.deleted) continue;
      const route = this.b.store.get('route', chat);
      const thread = recorded ? Number(recorded.thread) : String(route?.tgChat) === row.tg_chat ? Number(route.thread || 0) : 0;
      const destination = `${scope}:${row.tg_chat}:${thread}${!recorded && row.tg_chat === this.b.c.management ? ':' + row.tg_id : ''}`;
      destinations.set(destination, [...(destinations.get(destination) || []), { ...row, thread }]);
    }
    for (const [destination, copies] of destinations) {
      const id = digest(`${this.session}:${chat}:${message}:${destination}`).slice(0, 24);
      let task = this.b.store.get('revocation', id);
      if (task?.status === 'deleting' && !task.own) { this.queue(id, 'delete:' + task.cursor); continue; }
      if (task && (task.presented || task.status !== 'pending')) continue;
      if (!task) {
        const primary = copies.find(row => row.media) || copies[0];
        task = { owner: this.b.c.owner, session: this.session, chat, message, copies, primary,
          own, status: own ? 'pending' : 'deleting', cursor: 0, blocked: 0, mode: 'inline', noticeBot: primary.bot,
          expires: Date.now() + this.b.c.controlTTL * 1000 };
        this.b.store.put('revocation', id, task);
      }
      if (!own) { this.queue(id, 'delete:0'); continue; }
      const markup = { inline_keyboard: [[{ text: 'Delete', callback_data: 'rev-delete:' + id }, { text: 'Keep', callback_data: 'rev-keep:' + id }]] };
      if (task.mode === 'inline') {
        try { await this.b.api.tg(this.b.token(task.primary.bot), 'editMessageReplyMarkup', { chat_id: task.primary.tg_chat, message_id: task.primary.tg_id, reply_markup: markup }); }
        catch (e) {
          if (!(e instanceof ApiError) || ![400, 403].includes(e.status)) this.retry(e);
          task.mode = 'reply'; task.noticeBot = task.primary.tg_chat.startsWith('-') ? 'master' : task.primary.bot;
          this.b.store.put('revocation', id, task);
        }
      }
      if (task.mode === 'reply') {
        try {
          const [prompt] = await this.b.sendText(task.noticeBot, task.primary.tg_chat, 'Your message was deleted on WhatsApp. Delete this Telegram copy or keep it?', 'revocation-prompt:' + id,
            { ...(task.primary.thread ? { message_thread_id: task.primary.thread } : {}), reply_parameters: { message_id: task.primary.tg_id }, reply_markup: markup });
          task.prompt = prompt.message_id;
        } catch (e) {
          if (!(e instanceof ApiError) || e.status !== 400) throw e;
          this.b.store.put('revocation', id, { ...task, status: 'gone' }); continue;
        }
      }
      this.b.store.db.exec('BEGIN IMMEDIATE');
      try {
        this.b.store.put('revocation', id, { ...task, presented: true }); this.queue(id, 'expire', task.expires);
        this.b.store.db.exec('COMMIT');
      } catch (e) { this.b.store.db.exec('ROLLBACK'); throw e; }
    }
  }
  async callback(bot: string, q: Json) {
    const [operation, id] = String(q.data || '').split(':'), task = this.b.store.get('revocation', id);
    const valid = ['rev-delete', 'rev-keep'].includes(operation) && task?.own && task.owner === this.b.c.owner
      && String(q.from?.id) === this.b.c.owner && task.session === this.session && task.noticeBot === bot
      && String(q.message?.chat?.id) === task.primary.tg_chat && Number(q.message?.message_id) === Number(task.mode === 'reply' ? task.prompt : task.primary.tg_id);
    const active = valid && task.status === 'pending' && task.expires > Date.now();
    try { await this.b.api.tg(this.b.token(bot), 'answerCallbackQuery', { callback_query_id: q.id,
      text: active ? operation === 'rev-delete' ? 'Removing the Telegram copy…' : 'Keeping the Telegram copy.' : 'This choice is no longer active.' }); }
    catch (e) { if (!(e instanceof ApiError) || e.status !== 400) this.retry(e); }
    if (!active) return;
    const choice = operation === 'rev-delete' ? 'delete' : 'keep';
    this.b.store.db.exec('BEGIN IMMEDIATE');
    try {
      this.b.store.put('revocation', id, { ...task, status: choice === 'delete' ? 'deleting' : 'keeping' });
      this.queue(id, choice === 'delete' ? 'delete:0' : 'keep'); this.b.store.db.exec('COMMIT');
    } catch (e) { this.b.store.db.exec('ROLLBACK'); throw e; }
  }
  async retire(task: Json) {
    try {
      if (task.mode === 'reply' && task.prompt) {
        await this.b.api.tg(this.b.token(task.noticeBot), 'deleteMessages', { chat_id: task.primary.tg_chat, message_ids: [task.prompt] });
        this.b.store.db.prepare('UPDATE telegram_messages SET deleted=1,error=NULL WHERE scope=? AND chat=? AND message=?')
          .run(this.b.control.scope(task.noticeBot, task.primary.tg_chat), task.primary.tg_chat, task.prompt);
      } else if (task.presented) await this.b.api.tg(this.b.token(task.primary.bot), 'editMessageReplyMarkup', {
        chat_id: task.primary.tg_chat, message_id: task.primary.tg_id, reply_markup: { inline_keyboard: [] } });
    } catch (e) { if (!(e instanceof ApiError) || ![400, 403].includes(e.status)) this.retry(e); }
  }
  async process(job: Json) {
    const { id, step } = job.payload, task = this.b.store.get('revocation', id);
    if (!task || task.session !== this.session || task.owner !== this.b.c.owner || ['kept', 'deleted', 'partial', 'gone'].includes(task.status)) return;
    if (step === 'expire' && task.status !== 'pending') return;
    if (step === 'keep' || step === 'expire') {
      await this.retire(task); this.b.store.put('revocation', id, { ...task, status: 'kept' }); return;
    }
    if (task.status !== 'deleting') return;
    const row = task.copies[task.cursor];
    if (!row) {
      await this.retire(task);
      if (task.blocked) await this.b.sendText(task.noticeBot, task.primary.tg_chat, 'Telegram could not remove every copy because of its deletion limits or permissions. Remaining content was kept.', 'revocation-limit:' + id,
        task.primary.thread ? { message_thread_id: task.primary.thread } : {});
      this.b.store.put('revocation', id, { ...task, status: task.blocked ? 'partial' : 'deleted' }); return;
    }
    let removed = false;
    try {
      await this.b.api.tg(this.b.token(row.tg_chat.startsWith('-') ? 'master' : row.bot), 'deleteMessages', { chat_id: row.tg_chat, message_ids: [row.tg_id] }); removed = true;
    } catch (e) {
      if (!(e instanceof ApiError) || ![400, 403].includes(e.status)) this.retry(e);
      task.blocked++;
    }
    task.cursor++;
    this.b.store.db.exec('BEGIN IMMEDIATE');
    try {
      if (removed) this.b.store.db.prepare('UPDATE telegram_messages SET deleted=1,error=NULL WHERE scope=? AND chat=? AND message=?')
        .run(this.b.control.scope(row.bot, row.tg_chat), row.tg_chat, row.tg_id);
      this.b.store.put('revocation', id, task); this.queue(id, 'delete:' + task.cursor); this.b.store.db.exec('COMMIT');
    } catch (e) { this.b.store.db.exec('ROLLBACK'); throw e; }
  }
}
