import { randomUUID } from 'node:crypto';
import type { Bridge } from './bridge.ts';
import type { Json } from './config.ts';
import { ApiError } from './api.ts';

export function commandOf(message: Json) {
  const match = /^\/([a-z0-9_]+)(?:@([a-z0-9_]+))?(?:\s+([\s\S]*))?$/i.exec((message.text || message.caption || '').trim());
  return match ? { name: match[1].toLowerCase(), recipient: match[2]?.toLowerCase(), args: (match[3] || '').trim().split(/\s+/).filter(Boolean) } : undefined;
}
export function serviceMessage(m: Json) {
  return Object.keys(m).some(k => /^(new_chat_participant|new_chat_member|new_chat_members|left_chat_participant|left_chat_member|new_chat_title|new_chat_photo|delete_chat_photo|group_chat_created|supergroup_chat_created|channel_chat_created|pinned_message|migrate_to_chat_id|migrate_from_chat_id|message_auto_delete_timer_changed|forum_topic_|general_forum_topic_|video_chat_|write_access_allowed|users_shared|chat_shared|boost_added|chat_background_set|community_chat_joined)/.test(k));
}
const commands = (entries: string[][]) => entries.map(([command, description]) => ({ command, description }));
const common = commands([['start', 'Show bridge help'], ['sync', 'Refresh WhatsApp metadata'], ['history', 'Import messages here: /history <n>'], ['mention', 'Find a WhatsApp group member and copy their tag'], ['status', 'Show bridge and queue status'], ['clear', 'Clear bot notices in this chat or topic'], ['clear_log', 'Clear bot notices in this chat or topic']]);
const groupCommands = [...common, ...commands([['statuses', 'Open and refresh WhatsApp Status posts'], ['mute_status', 'Reply to a Status post to mute its author'], ['unmute_status', 'Reply to a Status post to unmute its author'], ['muted_statuses', 'List and unmute people in the Status feed'], ['create', 'Reply to a participant to create their bot'], ['group', 'Create or select a Telegram mirror group'], ['read', 'Mark this WhatsApp conversation read'], ['keep', 'Resume this conversation'], ['mute', 'Mute this conversation; reply in Status to mute its author'], ['ignore', 'Ignore this conversation'], ['delete', 'Delete a replied-to WhatsApp message'], ['react', 'React to a replied-to WhatsApp message']])];

export class TelegramControl {
  b: Bridge;
  constructor(b: Bridge) { this.b = b; }
  scope(bot: string, chat: string) { return String(chat).startsWith('-') ? '*' : bot; }
  async addressed(bot: string, command: Json, chatType: string) {
    if (!command.recipient) return chatType === 'private' || bot === 'master';
    let identity = bot === 'master' ? this.b.store.get('telegram-identity', bot) : this.b.store.get('bot', bot);
    if (!identity?.username) { identity = await this.b.api.tg(this.b.token(bot), 'getMe'); this.b.store.put('telegram-identity', bot, identity); }
    return identity.username?.toLowerCase() === command.recipient;
  }
  track(bot: string, m: Json, kind: string, expires = 0) {
    if (!Number.isSafeInteger(m.message_id) || !m.chat?.id) return;
    const chat = String(m.chat.id), scope = this.scope(bot, chat), { thread } = this.b.telegramContext(bot, m);
    this.b.store.db.prepare(`INSERT INTO telegram_messages(scope,chat,message,bot,thread,kind,date,expires) VALUES(?,?,?,?,?,?,?,?)
      ON CONFLICT(scope,chat,message) DO UPDATE SET kind=CASE WHEN telegram_messages.kind IN ('chat','command') THEN telegram_messages.kind ELSE excluded.kind END,
      expires=CASE WHEN telegram_messages.kind='chat' THEN 0 ELSE max(telegram_messages.expires,excluded.expires) END`)
      .run(scope, chat, m.message_id, bot, Number(thread || 0), kind, m.date ? m.date * 1000 : Date.now(), expires);
    if (expires) {
      const row = this.b.store.db.prepare('SELECT * FROM telegram_messages WHERE scope=? AND chat=? AND message=?').get(scope, chat, m.message_id);
      if (row?.kind === 'chat') return;
      const key = `control-expire:${scope}:${chat}:${m.message_id}`;
      this.b.store.enqueue(key, key, 'control-expire', { scope, chat, message: m.message_id, bot: chat.startsWith('-') ? 'master' : bot }, expires);
    }
  }
  observe(bot: string, m: Json) {
    const protectedMessage = m.supergroup_chat_created || m.channel_chat_created || m.forum_topic_created;
    const sender = String(m.from?.id), master = String(this.b.store.get('telegram-identity', 'master')?.id || this.b.c.masterToken.split(':')[0]);
    const bridgeBot = m.from?.is_bot && (sender === master || this.b.store.get('bot', sender));
    this.track(bot, m, protectedMessage ? 'protected' : serviceMessage(m) ? 'service' : bridgeBot ? 'log' : commandOf(m) ? 'command' : 'chat');
  }
  command(bot: string, m: Json) { this.track(bot, m, 'command', Date.now() + this.b.c.commandTTL * 1000); }
  reply(bot: string, receipt: Json, extra: Json) {
    this.track(bot, { ...receipt, ...(extra.message_thread_id ? { message_thread_id: extra.message_thread_id, is_topic_message: true } : {}) }, 'log', Date.now() + (extra.reply_markup ? this.b.c.controlTTL : this.b.c.commandTTL) * 1000);
  }
  async install(bot: string) {
    const token = this.b.token(bot), identity = await this.b.api.tg(token, 'getMe');
    this.b.store.put('telegram-identity', bot, identity);
    await this.b.api.tg(token, 'setMyCommands', { commands: bot === 'master' ? groupCommands : common, scope: { type: 'default' } });
    await this.b.api.tg(token, 'setChatMenuButton', { menu_button: { type: 'commands' } });
    if (bot === 'master') await this.b.api.tg(token, 'setMyCommands', { scope: { type: 'chat_member', chat_id: this.b.c.management, user_id: Number(this.b.c.owner) },
      commands: [...groupCommands, ...commands([['inbox', 'Check the management forum'], ['link', 'Link a WhatsApp group manually'], ['clear_all', 'Clear messages in the current management topic']])] });
    this.b.store.put('telegram-menu', bot, { installedAt: Date.now() });
  }
  startup() {
    this.indexHistory();
    for (const bot of ['master', ...this.b.store.all('bot').map(b => b.id)]) {
      if (!this.b.store.get('migrated', bot)) continue;
      const key = `control-menu:${randomUUID()}:${bot}`;
      this.b.store.enqueue(key, key, 'control-menu', { bot });
    }
  }
  indexHistory() {
    if (this.b.store.get('control-index', 'v1')) return;
    for (const row of this.b.store.db.prepare("SELECT id,result FROM effects WHERE state='done' AND result IS NOT NULL").all()) {
      const m = JSON.parse(row.result as string);
      if (!m?.message_id || !m.chat?.id) continue;
      const known = this.b.store.get('bot', String(m.from?.id));
      const bot = known ? String(m.from.id) : /^tg:([^:]+):/.exec(String(row.id))?.[1] || 'master';
      const mapped = this.b.store.byTelegram(String(m.chat.id), m.message_id, bot);
      this.track(mapped?.bot || bot, m, mapped ? 'chat' : 'log');
    }
    for (const row of this.b.store.db.prepare("SELECT payload FROM jobs WHERE type='tg' ORDER BY id").all()) {
      const p = JSON.parse(row.payload as string), m = p.update.message || p.update.edited_message;
      if (m) this.observe(p.bot, m);
    }
    this.b.store.put('control-index', 'v1', true);
  }
  queueClear(bot: string, m: Json, mode: string) {
    const chat = String(m.chat.id), { thread } = this.b.telegramContext(bot, m);
    const id = randomUUID(), cutoff = Number(this.b.store.db.prepare('SELECT max(seq) AS n FROM telegram_messages').get()!.n || 0);
    this.b.store.put('control-clear', id, { bot, chat, thread: Number(thread || 0), scope: this.scope(bot, chat), mode, cutoff, cursor: 0, removed: 0, skipped: 0, blocked: 0, status: 'queued' });
    this.nextClear(id, 0, false);
    return id;
  }
  nextClear(id: string, cursor: number, single: boolean) {
    const key = `control-clear:${id}:${cursor}:${single}`;
    this.b.store.enqueue(key, key, 'control-clear', { id });
  }
  retry(e: unknown) {
    if (e instanceof ApiError && (e.retry || e.uncertain)) throw new ApiError('Telegram control operation will retry', e.status, true, false, e.delay);
    throw e;
  }
  async process(job: Json) {
    const p = job.payload;
    if (job.type === 'control-retire-inbox') {
      try { await this.b.retireInbox(p.chatId); }
      catch (e) {
        if (!(e instanceof ApiError) || ![400, 403, 409].includes(e.status)) this.retry(e);
        await this.b.sendText('master', this.b.c.owner, 'The conversation is linked, but its old management topic could not be removed. Check Hermes’s Delete Messages permission and retry /start in the contact bot or link the mirror again.', job.dedup + ':notice');
      }
      return;
    }
    if (job.type === 'control-revoke') return this.b.revocations.process(job);
    if (job.type === 'control-menu') { try { await this.install(p.bot); } catch (e) { this.retry(e); } return; }
    if (job.type === 'control-expire') {
      const row = this.b.store.db.prepare('SELECT * FROM telegram_messages WHERE scope=? AND chat=? AND message=?').get(p.scope, p.chat, p.message);
      if (!row || row.deleted || row.kind === 'chat') return;
      try {
        await this.b.api.tg(this.b.token(p.bot), 'deleteMessages', { chat_id: p.chat, message_ids: [p.message] });
        this.b.store.db.prepare('UPDATE telegram_messages SET deleted=1,error=NULL WHERE seq=?').run(row.seq);
      } catch (e) {
        if (!(e instanceof ApiError) || ![400, 403].includes(e.status)) this.retry(e);
        this.b.store.db.prepare('UPDATE telegram_messages SET error=? WHERE seq=?').run('Telegram could not remove this notice', row.seq);
      }
      return;
    }
    const run = this.b.store.get('control-clear', p.id);
    if (!run || ['completed', 'failed'].includes(run.status)) return;
    const rows = this.b.store.db.prepare(`SELECT * FROM telegram_messages WHERE scope=? AND chat=? AND thread=? AND seq>? AND seq<=? AND deleted=0
      AND (?='all' OR kind IN ('log','command')) ORDER BY seq LIMIT ?`).all(run.scope, run.chat, run.thread, run.cursor, run.cutoff, run.mode, run.single ? 1 : 100);
    if (!rows.length) {
      await this.b.sendText(run.bot, run.chat, `Cleared ${run.removed} recorded Telegram ${run.mode === 'all' ? 'messages' : 'notices'}. ${run.skipped} too old or protected; ${run.blocked} could not be removed.`, job.dedup,
        run.thread ? { message_thread_id: run.thread } : {});
      this.b.store.put('control-clear', p.id, { ...run, status: 'completed' }); return;
    }
    const eligible = rows.filter(r => Number(r.date) > Date.now() - 48 * 3600000 && r.kind !== 'protected');
    let deleted: Json[] = [];
    if (eligible.length) {
      try {
        await this.b.api.tg(this.b.token(run.chat.startsWith('-') ? 'master' : run.bot), 'deleteMessages', { chat_id: run.chat, message_ids: eligible.map(r => Number(r.message)) });
        deleted = eligible;
        run.removed += eligible.length;
      } catch (e) {
        if (e instanceof ApiError && e.status === 400) {
          if (!run.single) { this.b.store.put('control-clear', p.id, { ...run, single: true }); this.nextClear(p.id, run.cursor, true); return; }
          run.blocked++;
        } else if (e instanceof ApiError && e.status === 403) {
          await this.b.sendText(run.bot, run.chat, 'Telegram refused cleanup. Give Hermes Delete Messages permission in this group.', job.dedup, run.thread ? { message_thread_id: run.thread } : {});
          this.b.store.put('control-clear', p.id, { ...run, status: 'failed' }); return;
        } else this.retry(e);
      }
    }
    run.skipped += rows.length - eligible.length;
    run.cursor = Number(rows.at(-1)!.seq); run.status = 'running';
    this.b.store.db.exec('BEGIN IMMEDIATE');
    try {
      for (const row of deleted) this.b.store.db.prepare('UPDATE telegram_messages SET deleted=1,error=NULL WHERE seq=?').run(row.seq);
      this.b.store.put('control-clear', p.id, run); this.nextClear(p.id, run.cursor, !!run.single);
      this.b.store.db.exec('COMMIT');
    } catch (e) { this.b.store.db.exec('ROLLBACK'); throw e; }
  }
}
