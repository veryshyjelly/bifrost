import { randomUUID } from 'node:crypto';
import type { Bridge } from './bridge.ts';
import { digest } from './config.ts';
import type { Json } from './config.ts';
import { ApiError } from './api.ts';
import { ensureOpenWAWebhook } from './openwa.ts';
import { concat, plain, senderName } from './format.ts';

const milliseconds = (value: unknown) => typeof value === 'number' ? value : typeof value === 'string' ? Date.parse(value) : NaN;

export class StatusFeed {
  b: Bridge;
  constructor(bridge: Bridge) { this.b = bridge; }
  get id() { return digest(JSON.stringify([this.b.c.account, this.b.c.management])); }
  get session() { return digest(this.b.c.account); }
  records(sender: string) {
    const canonical = this.b.store.canonical(sender);
    return this.b.store.all('status-mute').filter(row => row.session === this.session && this.b.store.canonical(row.sender) === canonical)
      .sort((a, b) => b.revision - a.revision);
  }
  isMuted(sender: string) { return this.records(sender)[0]?.active === true; }
  muted() {
    const people = new Map<string, Json>();
    for (const row of this.b.store.all('status-mute').filter(row => row.session === this.session)) {
      const sender = this.b.store.canonical(row.sender), old = people.get(sender);
      if (!old || row.revision > old.revision) people.set(sender, { ...row, sender });
    }
    return [...people.values()].filter(row => row.active).sort((a, b) => this.name(a).localeCompare(this.name(b)));
  }
  name(row: Json) { return this.b.store.get('contact', this.b.store.canonical(row.sender))?.name || row.name || row.sender; }
  operation(key: string) { return this.b.store.get('status-mute-op', digest(JSON.stringify([this.session, this.b.c.owner, key]))); }
  setMuted(sender: string, active: boolean, key: string) {
    this.b.store.db.exec('BEGIN IMMEDIATE');
    try {
      let result = this.operation(key);
      if (!result) {
        sender = this.b.store.canonical(sender);
        const previous = this.records(sender)[0], revision = (this.b.store.get('status-mute-counter', this.session) || 0) + 1;
        const id = previous?.id || digest(JSON.stringify([this.session, sender]));
        result = { id, session: this.session, sender, name: this.name({ sender, name: previous?.name }), active, revision, updatedAt: Date.now() };
        this.b.store.put('status-mute', id, result);
        this.b.store.put('status-mute-counter', this.session, revision);
        this.b.store.put('status-mute-op', digest(JSON.stringify([this.session, this.b.c.owner, key])), result);
      }
      this.b.store.db.exec('COMMIT'); return result;
    } catch (e) { this.b.store.db.exec('ROLLBACK'); throw e; }
  }
  suppressed(message: Json) {
    const key = digest(JSON.stringify([this.session, message.statusId]));
    if (this.b.store.get('status-skipped', key)) return true;
    if (!this.isMuted(message.author)) return false;
    this.b.store.put('status-skipped', key, { sender: message.author, at: Date.now() });
    return true;
  }
  button(action: Json, chat: string, thread: number | undefined) {
    return 'statusctl:' + this.b.action({ ...action, session: this.session, owner: this.b.c.owner, chat, thread: Number(thread || 0), expires: Date.now() + this.b.c.controlTTL * 1000 });
  }
  page(page: number, chat: string, thread: number | undefined) {
    const people = this.muted(), pages = Math.max(1, Math.ceil(people.length / 10)); page = Math.min(page, pages);
    const rows = people.slice((page - 1) * 10, page * 10);
    let text = plain(people.length ? `Muted Status posts (${people.length} ${people.length === 1 ? 'person' : 'people'}, page ${page}/${pages}):` : 'Nobody’s Status posts are muted.');
    const buttons = rows.map((row, i) => {
      const name = Array.from(senderName(this.name(row), row.sender).text).slice(0, 50).join('');
      text = concat(text, '\n' + ((page - 1) * 10 + i + 1) + '. ', senderName(name, row.sender));
      return [{ text: 'Unmute ' + Array.from(name).slice(0, 25).join(''), callback_data: this.button({ kind: 'status-unmute', sender: row.sender, revision: row.revision, view: 'list', page }, chat, thread) }];
    });
    const navigation = [];
    if (page > 1) navigation.push({ text: 'Previous', callback_data: this.button({ kind: 'status-page', page: page - 1 }, chat, thread) });
    if (page < pages) navigation.push({ text: 'Next', callback_data: this.button({ kind: 'status-page', page: page + 1 }, chat, thread) });
    if (navigation.length) buttons.push(navigation);
    return { text, reply_markup: { inline_keyboard: buttons } };
  }
  async command(bot: string, m: Json, command: string, args: string[], key: string) {
    const contextual = this.isTopic(m) && ['mute', 'unmute', 'keep'].includes(command);
    if (!['mute_status', 'unmute_status', 'muted_statuses'].includes(command) && !contextual) return false;
    if (bot !== 'master' || String(m.from?.id) !== this.b.c.owner) return false;
    const chat = String(m.chat.id), { thread } = this.b.telegramContext(bot, m), extra = thread ? { message_thread_id: thread } : {};
    if (command === 'muted_statuses') {
      if (args.length > 1 || (args.length && !/^[1-9]\d{0,5}$/.test(args[0]))) {
        await this.b.sendText(bot, chat, 'Use /muted_statuses or /muted_statuses <page>.', key, extra); return true;
      }
      const page = this.page(Number(args[0] || 1), chat, thread);
      await this.b.sendText(bot, chat, page.text, key, { ...extra, ...(page.reply_markup.inline_keyboard.length ? { reply_markup: page.reply_markup } : {}) }); return true;
    }
    const reply = this.isTopic(m) && m.reply_to_message && this.b.store.byTelegram(chat, m.reply_to_message.message_id, bot);
    const source = reply?.wa_chat === 'status@broadcast' && this.b.store.get('wamessage', `${reply.wa_chat}:${reply.wa_id}`);
    if (args.length || !source?.status || !source.sender) {
      await this.b.sendText(bot, chat, 'Reply to a post in WhatsApp Status with /mute_status or /unmute_status. Use /muted_statuses to manage muted people.', key, extra); return true;
    }
    const active = ['mute', 'mute_status'].includes(command), row = this.setMuted(source.sender, active, key);
    const text = concat(active ? 'Muted Status posts from ' : 'Unmuted Status posts from ', senderName(this.name(row), row.sender), active ? '. Their chats continue as usual.' : '. New posts will appear again.');
    const reply_markup = active ? { inline_keyboard: [[{ text: 'Unmute', callback_data: this.button({ kind: 'status-unmute', sender: row.sender, revision: row.revision, view: 'confirmation' }, chat, thread) }]] } : undefined;
    await this.b.sendText(bot, chat, text, key, { ...extra, ...(reply_markup ? { reply_markup } : {}) }); return true;
  }
  async control(method: string, body: Json) {
    try { return await this.b.api.tg(this.b.token('master'), method, body); }
    catch (e) { if (!(e instanceof ApiError) || ![400, 403].includes(e.status)) this.b.control.retry(e); }
  }
  async callback(bot: string, q: Json, key: string) {
    if (!String(q.data || '').startsWith('statusctl:')) return false;
    const action = this.b.store.get('action', String(q.data).slice('statusctl:'.length)), m = q.message;
    const valid = bot === 'master' && String(q.from?.id) === this.b.c.owner && action?.owner === this.b.c.owner && action.session === this.session
      && action.expires > Date.now() && m?.chat && String(m.chat.id) === action.chat && Number(this.b.telegramContext(bot, m).thread || 0) === action.thread
      && ['status-unmute', 'status-page'].includes(action.kind);
    if (!valid) {
      try { await this.b.api.tg(this.b.token(bot), 'answerCallbackQuery', { callback_query_id: q.id, text: 'This control expired. Use /muted_statuses.' }); }
      catch (e) { if (!(e instanceof ApiError) || ![400, 403].includes(e.status)) this.b.control.retry(e); }
      return true;
    }
    let row: Json;
    if (action.kind === 'status-unmute') {
      row = this.operation(key);
      if (!row) {
        const current = this.records(action.sender)[0];
        if (!current?.active || current.revision !== action.revision) {
          await this.control('answerCallbackQuery', { callback_query_id: q.id, text: 'That mute has changed. Use /muted_statuses for the current list.' }); return true;
        }
        row = this.setMuted(action.sender, false, key);
      }
    }
    await this.control('answerCallbackQuery', { callback_query_id: q.id, ...(row ? { text: 'Unmuted this person’s Status posts.' } : {}) });
    const page = action.kind === 'status-page' || action.view === 'list' ? this.page(action.page || 1, action.chat, action.thread) : undefined;
    const text = page?.text || concat('Unmuted Status posts from ', senderName(this.name(row), row.sender), '. New posts will appear again.');
    await this.control('editMessageText', { chat_id: action.chat, message_id: m.message_id, text: text.text, entities: text.entities,
      link_preview_options: { is_disabled: true }, reply_markup: page?.reply_markup || { inline_keyboard: [] } });
    return true;
  }
  async topic() {
    const saved = this.b.store.get('status-feed', this.id);
    if (saved) return saved;
    const receipt = await this.b.effect('status-topic:' + this.id, () => this.b.api.tg(this.b.token('master'), 'createForumTopic', {
      chat_id: this.b.c.management, name: 'WhatsApp Status',
    }));
    const feed = { chatId: 'status@broadcast', tgChat: this.b.c.management, thread: receipt.message_thread_id, mode: 'status' };
    this.b.store.put('status-feed', this.id, feed);
    return feed;
  }
  isTopic(message: Json) {
    const feed = this.b.store.get('status-feed', this.id);
    return !!feed && String(message.chat.id) === feed.tgChat && Number(message.message_thread_id) === Number(feed.thread);
  }
  url(feed: Json) { return `https://t.me/c/${String(feed.tgChat).replace(/^-100/, '')}/${feed.thread}`; }
  startup() {
    if (!this.b.store.get('migrated', 'master')) return;
    const key = 'status-startup:' + randomUUID();
    this.b.store.enqueue(key, 'status-control:' + this.id, 'status-startup', {});
  }
  queueRefresh(key: string) {
    this.b.store.enqueue('status-refresh:' + key, 'status-control:' + this.id, 'status-refresh', {});
  }
  async process(job: Json) {
    await this.topic();
    if (job.type === 'status-startup') await ensureOpenWAWebhook(this.b.c, this.b.api);
    const response = await this.b.api.ow('status');
    if (!Array.isArray(response.statuses)) throw new ApiError('Unexpected OpenWA Status response', 502);
    for (const status of [...response.statuses].sort((a, b) => milliseconds(a.timestamp) - milliseconds(b.timestamp))) {
      const data = { ...status, statusId: status.id, postedAt: milliseconds(status.timestamp), hasMedia: !!status.mediaUrl };
      const message = this.message(data);
      if (!message || this.b.store.get('wa-complete', `status@broadcast:${data.statusId}`) || this.suppressed(message)) continue;
      this.b.store.enqueue(`status-post:${this.id}:${data.statusId}`, 'wa:status@broadcast', 'wa', {
        sessionId: this.b.c.session, event: 'status.received', data,
      });
    }
  }
  message(data: Json): Json | undefined {
    if (!data || typeof data.statusId !== 'string' || !data.statusId || typeof data.contact?.id !== 'string'
      || !/@(?:c\.us|s\.whatsapp\.net|lid)$/.test(data.contact.id)) return;
    const posted = milliseconds(data.postedAt ?? data.timestamp), expires = milliseconds(data.expiresAt) || posted + 86400000;
    if (!Number.isFinite(posted) || posted <= 0 || !Number.isFinite(expires) || expires <= Date.now()) return;
    const supported = ['text', 'image', 'video', 'voice'].includes(data.type);
    return { id: data.statusId, statusId: data.statusId, chatId: 'status@broadcast', from: data.contact.id, author: data.contact.id,
      contact: data.contact, isGroup: false, fromMe: false, isStatusBroadcast: true, kind: 'status',
      type: supported ? data.type : 'text', body: (data.caption || '') + (supported ? '' : `\n[${data.type || 'Unsupported'} status]`),
      timestamp: Math.floor(posted / 1000), media: { omitted: data.mediaOmitted === true || data.hasMedia === false },
    };
  }
}
