import type { Bridge } from './bridge.ts';
import { digest } from './config.ts';
import type { Json } from './config.ts';
import { ApiError } from './api.ts';
import { historyDay } from './format.ts';

export type HistoryTarget = { chat: string; thread?: number; bot?: string };
export type HistoryMode = 'skip' | 'all';
export class ConversationHistory {
  b: Bridge;
  constructor(bridge: Bridge) { this.b = bridge; }
  scope(chat: string, target: HistoryTarget) {
    return digest(JSON.stringify([this.b.c.session, this.b.store.canonical(chat), String(target.chat), Number(target.thread || 0), String(target.chat).startsWith('-') ? '*' : target.bot]));
  }
  target(chat: string): HistoryTarget | undefined {
    const route = this.b.store.get('route', chat); if (!route) return;
    const contact = this.b.store.get('contact', chat), bot = contact?.bot && this.b.store.get('bot', contact.bot);
    return route.mode === 'dedicated' && bot?.ready ? { chat: this.b.c.owner, bot: contact.bot } : { chat: String(route.tgChat), thread: route.thread };
  }
  at(row: Json, target: HistoryTarget) {
    if (row.tg_chat !== String(target.chat) || (!String(target.chat).startsWith('-') && row.bot !== target.bot)) return false;
    const record = this.b.store.db.prepare('SELECT thread,deleted FROM telegram_messages WHERE scope=? AND chat=? AND message=?')
      .get(this.b.control.scope(row.bot, row.tg_chat), row.tg_chat, row.tg_id);
    return !record || (!record.deleted && Number(record.thread || 0) === Number(target.thread || 0));
  }
  delivered(chat: string, id: string, target: HistoryTarget) {
    const key = this.scope(chat, target) + ':' + id;
    if (this.b.store.get('destination-complete', key)) return this.b.store.byWhatsApp(chat, id).some(row => this.at(row, target));
    if (this.b.store.get('destination-start', key)) return false;
    return this.b.store.byWhatsApp(chat, id).some(row => this.at(row, target) && (row.direction === 'out' || this.b.store.get('wa-complete', chat + ':' + id)));
  }
  begin(chat: string, id: string, target: HistoryTarget, key: string) {
    const scoped = this.scope(chat, target) + ':' + id, old = this.b.store.get('destination-start', scoped);
    if (this.b.store.get('destination-complete', scoped) && !this.b.store.byWhatsApp(chat, id).some(row => this.at(row, target))) {
      this.b.store.db.prepare('DELETE FROM kv WHERE kind=? AND id=?').run('destination-complete', scoped);
      this.b.store.put('destination-start', scoped, { key }); return key;
    }
    if (!old) this.b.store.put('destination-start', scoped, { key });
    return old?.key || key;
  }
  complete(chat: string, id: string, target: HistoryTarget) {
    this.b.store.put('destination-complete', this.scope(chat, target) + ':' + id, true);
    const current = this.target(chat);
    if (current && this.scope(chat, current) === this.scope(chat, target)) this.b.store.put('wa-complete', chat + ':' + id, true);
  }
  active(chatId: string) {
    return this.b.store.all('history-run').find(run => run.session === this.b.c.session && run.chatId === chatId && ['queued', 'fetching', 'delivering'].includes(run.status));
  }
  async prompt(chatId: string, target: HistoryTarget, count: number, bot: string, key: string) {
    chatId = this.b.store.canonical(chatId);
    if (this.active(chatId)) { await this.b.sendText(bot, target.chat, 'History is already importing for this conversation. Wait for it to finish, then run /history again.', key, target.thread ? { message_thread_id: target.thread } : {}); return; }
    const id = digest(JSON.stringify([this.b.c.session, bot, key])).slice(0, 24);
    if (!this.b.store.get('history-choice', id)) this.b.store.put('history-choice', id, { chatId, target, count, bot, owner: this.b.c.owner, session: this.b.c.session, status: 'pending', expires: Date.now() + this.b.c.controlTTL * 1000 });
    const receipts = await this.b.sendText(bot, target.chat, `Import up to ${count} WhatsApp messages here?\nSkip existing avoids copies recorded as delivered. Import all restores a manually cleared chat, but can create duplicates if messages are still present.`, key,
      { ...(target.thread ? { message_thread_id: target.thread } : {}), reply_markup: { inline_keyboard: [[{ text: 'Skip existing', callback_data: `history:skip:${id}` }, { text: 'Import all', callback_data: `history:all:${id}` }], [{ text: 'Cancel', callback_data: `history:cancel:${id}` }]] } });
    this.b.store.put('history-choice', id, { ...this.b.store.get('history-choice', id), promptMessage: receipts[0].message_id });
  }
  async callback(bot: string, q: Json) {
    if (!String(q.data || '').startsWith('history:') || q.data.split(':').length !== 3) return false;
    const [, mode, id] = q.data.split(':'), choice = this.b.store.get('history-choice', id), context = q.message && this.b.telegramContext(bot, q.message);
    const promptMatches = choice?.promptMessage ? q.message?.message_id === choice.promptMessage
      : q.message?.reply_markup?.inline_keyboard?.flat().some((button: Json) => button.callback_data === q.data);
    const valid = ['skip', 'all', 'cancel'].includes(mode) && choice && choice.bot === bot && choice.owner === String(q.from?.id) && choice.owner === this.b.c.owner
      && choice.session === this.b.c.session && choice.expires > Date.now() && String(q.message?.chat.id) === choice.target.chat
      && promptMatches && Number(context?.thread || 0) === Number(choice.target.thread || 0) && this.b.store.canonical(context?.route?.chatId || '') === choice.chatId && !['muted', 'ignored'].includes(context.route.mode);
    try { await this.b.api.tg(this.b.token(bot), 'answerCallbackQuery', { callback_query_id: q.id, ...(!valid ? { text: 'This history choice expired or its conversation changed. Run /history again.' } : choice.status !== 'pending' ? { text: 'This choice was already used. Run /history for a new import.' } : {}) }); }
    catch (e) { if (!(e instanceof ApiError) || e.status !== 400) this.b.control.retry(e); }
    if (!valid) return true;
    let selected = choice;
    if (choice.status === 'pending') {
      this.b.store.db.exec('BEGIN IMMEDIATE');
      try {
        if (mode === 'cancel') selected = { ...choice, status: 'cancelled' };
        else if (this.active(choice.chatId)) selected = { ...choice, status: 'busy' };
        else {
          const run = this.request(choice.chatId, choice.target, choice.count, 'history-choice:' + id, true, mode as HistoryMode);
          selected = { ...choice, status: 'selected', mode, run: run.id };
        }
        this.b.store.put('history-choice', id, selected);
        this.b.store.enqueue('history-choice-delete:' + id, 'history-choice-delete:' + id, 'control-expire', {
          scope: this.b.control.scope(bot, choice.target.chat), bot, chat: choice.target.chat, message: choice.promptMessage || q.message.message_id,
        });
        this.b.store.db.exec('COMMIT');
      } catch (e) { this.b.store.db.exec('ROLLBACK'); throw e; }
    }
    const text = selected.status === 'cancelled' ? 'History import cancelled.' : selected.status === 'busy' ? 'History is already importing for this conversation. Wait for it to finish, then run /history again.'
      : `History queued: fetching up to ${choice.count} messages, then posting oldest first here. ${selected.mode === 'all' ? 'Previously delivered messages will be included.' : 'Already delivered messages will be skipped.'}`;
    await this.b.sendText(bot, choice.target.chat, text, 'history-choice-confirm:' + id, choice.target.thread ? { message_thread_id: choice.target.thread } : {});
    return true;
  }
  copyKey(run: string, chat: string, id: string) { return digest(JSON.stringify([this.b.c.session, run, chat, id])); }
  request(chatId: string, target: HistoryTarget, count: number, key: string, enqueue = true, mode: HistoryMode = 'skip') {
    chatId = this.b.store.canonical(chatId);
    const id = digest(JSON.stringify([this.b.c.session, chatId, target, key]));
    const previous = this.b.store.get('history-run', id); if (previous) return previous;
    const active = this.active(chatId);
    if (active) return active;
    const run = { id, chatId, target, count, mode, historyStyle: 'compact', timeZone: this.b.c.syncTimezone, status: 'queued', createdAt: Date.now(), session: this.b.c.session };
    this.b.store.put('history-run', id, run);
    if (enqueue) this.b.store.enqueue('history-fetch:' + id, 'history-fetch:' + id, 'history', { run: id });
    return run;
  }
  receipt(row: Json) {
    const cached = this.b.store.get('telegram-receipt', digest(JSON.stringify([row.bot, row.tg_chat, row.tg_id])));
    if (cached) return cached;
    const results = this.b.store.db.prepare("SELECT result FROM effects WHERE state='done' AND json_extract(result,'$.message_id')=? AND CAST(json_extract(result,'$.chat.id') AS TEXT)=?")
      .all(row.tg_id, row.tg_chat).map(result => JSON.parse(result.result as string));
    const identity = row.bot === 'master' ? this.b.c.masterToken.split(':')[0] : row.bot;
    return results.find(message => String(row.tg_chat).startsWith('-') || String(message.from?.id) === identity);
  }
  archivedMedia(chat: string, id: string) {
    for (const row of this.b.store.byWhatsApp(chat, id).filter(row => row.media)) {
      let message = this.receipt(row);
      if (!message && row.direction === 'out') {
        message = this.b.store.db.prepare("SELECT payload FROM jobs WHERE type='tg' AND json_extract(payload,'$.bot')=? AND json_extract(payload,'$.update.message.message_id')=?").all(row.bot, row.tg_id)
          .map(item => JSON.parse(item.payload as string).update.message).find(m => String(m.chat.id) === row.tg_chat);
      }
      if (message && ['photo', 'video', 'animation', 'audio', 'voice', 'sticker', 'document', 'video_note'].some(kind => message[kind])) return { bot: row.bot, message };
    }
  }
  ordered(rows: Json[]) {
    const sorted = [...rows].sort((a, b) => a.timestamp - b.timestamp || a.id.localeCompare(b.id)), byId = new Map(sorted.map(row => [row.id, row]));
    const visited = new Set<string>(), visiting = new Set<string>(), result: Json[] = [];
    for (const row of sorted) {
      const stack = [{ row, expanded: false }];
      while (stack.length) {
        const next = stack.pop()!, id = next.row.id;
        if (visited.has(id)) continue;
        if (next.expanded) { visiting.delete(id); visited.add(id); result.push(next.row); continue; }
        visiting.add(id); stack.push({ row: next.row, expanded: true });
        const parent = byId.get(next.row.quotedMessage?.id);
        if (parent && !visited.has(parent.id) && !visiting.has(parent.id)) stack.push({ row: parent, expanded: false });
      }
    }
    return result;
  }
  async fetch(data: Json, key: string) {
    let run = data.run && this.b.store.get('history-run', data.run);
    if (!run) {
      const target = this.target(data.chatId); if (!target) return;
      run = this.request(data.chatId, target, data.count || this.b.c.history || 20, key, false);
    }
    if (run.session !== this.b.c.session || run.status === 'complete') return;
    let plan = this.b.store.get('history-plan', run.id);
    if (!plan) {
      this.b.store.put('history-run', run.id, { ...run, status: 'fetching' });
      const rows = await this.b.api.ow(`messages/${encodeURIComponent(run.chatId)}/history?limit=${run.count}&includeMedia=false${run.count > 100 ? '&deep=true' : ''}`);
      if (!Array.isArray(rows)) throw new ApiError('Unexpected OpenWA history response', 502);
      const unique = new Map<string, Json>();
      for (const row of rows) {
        if (!row.id || row.type === 'revoked' || row.isStatusBroadcast || (row.chatId && this.b.store.canonical(row.chatId) !== run.chatId)) continue;
        const message = { ...row, chatId: run.chatId, isGroup: run.chatId.endsWith('@g.us') };
        const archive = this.archivedMedia(run.chatId, row.id);
        if (archive) message.archiveMedia = archive;
        unique.set(row.id, message);
      }
      plan = { items: this.ordered([...unique.values()].sort((a, b) => a.timestamp - b.timestamp).slice(-run.count)) };
      this.b.store.put('history-plan', run.id, plan);
    }
    this.b.store.put('history-run', run.id, { ...run, status: 'delivering', available: plan.items.length });
    for (let index = 0; index < plan.items.length; index++) this.b.store.enqueue(`history-item:${run.id}:${index}`, 'history-delivery:' + run.id, 'history-message', { run: run.id, index });
    this.b.store.enqueue('history-finish:' + run.id, 'history-delivery:' + run.id, 'history-finish', { run: run.id });
  }
  async process(job: Json) {
    const run = this.b.store.get('history-run', job.payload.run); if (!run || run.session !== this.b.c.session) return;
    if (job.type === 'history-message') {
      const message = this.b.store.get('history-plan', run.id)?.items[job.payload.index];
      if (!message) throw new ApiError('History snapshot is missing', 409);
      const skipped = run.mode === 'all' ? this.b.store.get('history-copy-complete', this.copyKey(run.id, run.chatId, message.id)) : this.delivered(run.chatId, message.id, run.target);
      if (run.historyStyle === 'compact' && !skipped && !this.b.store.get('revoked', `${run.chatId}:${message.id}`)) {
        const day = historyDay(message.timestamp, run.timeZone || this.b.c.syncTimezone), previous = this.b.store.get('history-day', run.id);
        if (previous?.day !== day.key) {
          await this.b.sendText(run.target.bot || 'master', run.target.chat, day.label, job.dedup + ':day', run.target.thread ? { message_thread_id: run.target.thread } : {}, 'chat');
          this.b.store.put('history-day', run.id, { day: day.key });
        }
      }
      await this.b.deliverWhatsApp({ event: 'message.received', sessionId: this.b.c.session, historical: true, replay: run.target, historyMode: run.mode || 'skip', historyRun: run.id,
        historyStyle: run.historyStyle || 'legacy', historyTimeZone: run.timeZone, data: message }, job.dedup);
    } else {
      this.b.store.put('history-run', run.id, { ...run, status: 'complete', finishedAt: Date.now() });
      await this.b.sendText(run.target.bot || 'master', run.target.chat, `History ready: ${run.available || 0} available messages checked, oldest first. ${run.mode === 'all' ? 'Previously delivered messages were included.' : 'Already delivered messages were skipped.'} WhatsApp-deleted messages remain excluded.`, job.dedup,
        run.target.thread ? { message_thread_id: run.target.thread } : {});
    }
  }
  failed(job: Json) {
    const run = job.payload.run && this.b.store.get('history-run', job.payload.run);
    if (run && run.status !== 'complete') this.b.store.put('history-run', run.id, { ...run, status: 'failed' });
  }
}
