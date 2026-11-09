import { randomUUID, randomInt } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import { Store } from './store.ts';
import { Api, ApiError } from './api.ts';
import { Media } from './media.ts';
import { digest, sign, equal } from './config.ts';
import type { Config, Json } from './config.ts';
import { MetadataSync } from './sync.ts';
import { TelegramControl, commandOf, serviceMessage } from './control.ts';
import { Revocations } from './revocations.ts';
import { NativeReactions } from './reactions.ts';
import { plain, concat, styled, relayHeader, relayText, splitFormatted, captionFields } from './format.ts';
import type { Formatted, RelayFormat } from './format.ts';
import { waEvents } from './openwa.ts';
import { StatusFeed } from './statuses.ts';
import { ConversationHistory } from './history.ts';
import { Mentions, MentionError } from './mentions.ts';

const updates = ['message', 'edited_message', 'callback_query', 'my_chat_member', 'managed_bot', 'message_reaction', 'poll_answer'];
const chunks = (text: string, length = 3800) => {
  const result: string[] = []; let current = '';
  for (const char of text) { if (current.length + char.length > length) { result.push(current); current = ''; } current += char; }
  if (current || !result.length) result.push(current); return result;
};

export class Bridge {
  c: Config; store: Store; api: Api; media: Media; metadata: MetadataSync; control: TelegramControl; revocations: Revocations; reactions: NativeReactions; statuses: StatusFeed; busy = false;
  constructor(c: Config, store = new Store(c.db, c.key), api = new Api(c)) {
    this.c = c; this.store = store; this.api = api; this.media = new Media(c, api); this.metadata = new MetadataSync(this); this.control = new TelegramControl(this);
    this.revocations = new Revocations(this);
    this.reactions = new NativeReactions(this);
    this.statuses = new StatusFeed(this);
    this.conversationHistory = new ConversationHistory(this);
    this.mentions = new Mentions(this);
  }
  mentions: Mentions;
  conversationHistory: ConversationHistory;
  token(id: string) {
    if (id === 'master') return this.c.masterToken;
    const bot = this.store.get('bot', id);
    if (!bot) throw new ApiError('Unregistered bot', 404);
    return this.store.unseal(bot.token);
  }
  webhookSecret(id: string) { return sign(id, this.c.admin); }
  telegramContext(bot: string, message: Json) {
    const chat = String(message.chat.id);
    let route: Json, contact: Json;
    if (message.chat.type === 'private') {
      contact = this.store.all('contact').find(c => c.bot === bot);
      route = contact && this.store.get('route', contact.id);
    } else {
      // A mirror without a configured topic covers the whole group. Telegram can
      // include a reply-thread ID even when this group has no forum topics.
      route = this.store.all('route').find(r => String(r.tgChat) === chat
        && !(chat === this.c.management && r.inboxRetired && !r.thread)
        && (!r.thread || Number(r.thread) === Number(message.message_thread_id || 0)));
    }
    const thread = message.chat.type === 'private' ? undefined : route ? route.thread
      : message.is_topic_message ? message.message_thread_id : undefined;
    return { route, thread, contact };
  }
  async effect(id: string, action: () => Promise<Json>): Promise<Json> {
    const effect = this.store.db.prepare('SELECT * FROM effects WHERE id=?').get(id);
    if (effect?.state === 'done') return JSON.parse(effect.result as string);
    if (effect?.state === 'uncertain' || effect?.state === 'sending') throw new ApiError('Uncertain external delivery; review before retrying', 0, false, true);
    this.store.db.prepare("INSERT INTO effects(id,state) VALUES(?,'sending') ON CONFLICT(id) DO UPDATE SET state='sending'").run(id);
    try {
      const result = await action();
      this.store.db.prepare("UPDATE effects SET state='done',result=? WHERE id=?").run(JSON.stringify(result ?? null), id);
      return result;
    } catch (e) {
      this.store.db.prepare('UPDATE effects SET state=? WHERE id=?').run(e instanceof ApiError && !e.uncertain ? 'pending' : 'uncertain', id);
      throw e;
    }
  }
  acceptWhatsApp(payload: Json) {
    if (payload.sessionId !== this.c.session || !waEvents.includes(payload.event) || !payload.idempotencyKey || !payload.data) throw new ApiError('Unexpected OpenWA event or session', 400);
    const m = payload.data;
    return this.store.enqueue('ow:' + payload.idempotencyKey, 'wa:' + (payload.event === 'status.received' ? 'status@broadcast' : m.chatId || 'session'), 'wa', payload);
  }
  acceptTelegram(bot: string, update: Json) {
    if (bot !== 'master' && !this.store.get('bot', bot)) throw new ApiError('Unknown bot', 404);
    const observed = update.message || update.edited_message;
    if (observed?.chat && (String(observed.chat.id) === this.c.owner || (observed.chat.type !== 'private' && (String(observed.chat.id) === this.c.management || this.telegramContext(bot, observed).route)))) this.control.observe(bot, observed);
    const actor = update.callback_query?.from || update.message?.from || update.edited_message?.from || update.message_reaction?.user || update.my_chat_member?.from || update.managed_bot?.user || update.poll_answer?.user;
    // Bot membership events have an administrator as actor; still require our owner.
    if (!actor || actor.is_bot || String(actor.id) !== this.c.owner) return false;
    if (!Number.isSafeInteger(update.update_id)) throw new ApiError('Invalid update', 400);
    const m = update.message || update.edited_message || update.callback_query?.message;
    const reaction = update.message_reaction;
    const recipient = m && commandOf(m)?.recipient;
    const directed = !!recipient && recipient === this.store.get('bot', bot)?.username?.toLowerCase();
    if ((m?.chat || reaction?.chat)?.type !== 'private' && bot !== 'master' && !update.my_chat_member && !update.callback_query && !directed) return false;
    const stream = m?.chat ? `tg:${m.chat.type === 'private' ? bot + ':' : ''}${m.chat.id}:${this.telegramContext(bot, m).thread || 0}` : reaction?.chat ? `tg-reaction:${bot}:${reaction.chat.id}` : `tg:${bot}`;
    return this.store.enqueue(`tg:${bot}:${update.update_id}`, stream, 'tg', { bot, update });
  }
  management(type: string, data: Json, dedup: string) {
    this.store.enqueue('management:' + dedup, 'management:' + dedup, 'management', { id: dedup, type, data, at: Date.now() });
  }
  acceptManagement(event: Json) {
    const { signature, ...body } = event;
    if (typeof signature !== 'string' || !equal(signature, sign(JSON.stringify(body), this.c.admin)) || Math.abs(Date.now() - body.at) > 7 * 86400000) throw new ApiError('Invalid management signature', 401);
    return this.store.enqueue('management:' + body.id, 'management:' + body.id, 'management', body);
  }
  async tick() {
    if (this.busy) return;
    this.busy = true;
    try {
      const job = this.store.next(); if (!job) return;
      try {
        if (job.type === 'wa') {
          await this.deliverWhatsApp(job.payload, job.dedup);
        }
        else if (job.type === 'tg') await this.telegram(job.payload.bot, job.payload.update, job.dedup);
        else if (job.type === 'history') await this.history(job.payload, job.dedup);
        else if (['history-message', 'history-finish'].includes(job.type)) await this.conversationHistory.process(job);
        else if (job.type === 'management') await this.manage(job.payload, job.dedup);
        else if (job.type.startsWith('control-')) await this.control.process(job);
        else if (job.type.startsWith('sync-')) await this.metadata.process(job);
        else if (job.type.startsWith('status-')) await this.statuses.process(job);
        else if (job.type === 'n8n') {
          // Old exports may contain undelivered forwarding jobs. Converge on the
          // same receipt key as an event already forwarded by the old workflow.
          const event = job.payload;
          this.store.enqueue('management:' + event.id, 'management:' + event.id, 'management', event);
        }
        this.store.finish(job.id);
      } catch (e) {
        const error = e instanceof ApiError ? e : new ApiError('Internal processing error; inspect local service logs', 0, false, true);
        const state = error.uncertain ? 'uncertain' : error.retry && job.attempts < 8 ? 'pending' : 'failed';
        this.store.fail(job.id, state, error.message, Math.max(error.delay, Math.min(300000, 1000 * 2 ** job.attempts)));
        if (state !== 'pending' && (job.type === 'history' || job.type.startsWith('history-'))) this.conversationHistory.failed(job);
        if (['sync-start', 'sync-finish'].includes(job.type) && state !== 'pending') {
          const run = this.store.get('sync-run', job.payload.run);
          if (run) this.store.put('sync-run', job.payload.run, { ...run, status: 'failed', error: error.message });
        }
        console.error(JSON.stringify({ event: 'job_failed', job: job.id, state, error: error.message }));
        if (job.type !== 'n8n' && !job.type.startsWith('control-') && !(job.type === 'management' && ['delivery_problem', 'session_status'].includes(job.payload.type))) {
          this.management('delivery_problem', { job: job.id, state, error: error.message }, `failure:${job.id}:${job.attempts}`);
        }
      }
    } finally { this.busy = false; }
  }
  async deliverWhatsApp(event: Json, key: string) {
    try { await this.whatsApp(event, key); }
    catch (e) {
      if (await this.repairTopic(event, key, e)) await this.whatsApp(event, key);
      else if (e instanceof ApiError && e.service === 'telegram' && e.status === 403 && !event.forceMaster && !event.replay) {
        await this.deliverWhatsApp({ ...event, forceMaster: true }, key);
      } else throw e;
    }
  }
  async repairTopic(event: Json, key: string, error: unknown) {
    if (event.replay) return false;
    if (!(error instanceof ApiError) || error.service !== 'telegram' || error.status !== 400 || error.reason !== 'thread_missing') return false;
    const chat = this.store.canonical(event.data?.chatId || ''), snapshotKey = key + (event.forceMaster ? ':fallback' : '');
    const target = this.store.get('delivery-target', snapshotKey), route = this.store.get('route', chat);
    if (!target?.thread || target.bot !== 'master' || String(target.destination) !== this.c.management || String(route?.tgChat) !== this.c.management) return false;
    const prefix = `${key}:to:${target.bot}:${target.destination}:`;
    // A definite refusal can be moved only if no part was delivered or left uncertain.
    if (this.store.byWhatsApp(chat, event.data.id).length || this.store.db.prepare("SELECT 1 FROM effects WHERE state IN ('done','sending','uncertain') AND substr(id,1,length(?))=? LIMIT 1").get(prefix, prefix)) return false;
    if (route.mode === 'dedicated' && !event.forceMaster) {
      const bot = await this.preferred(chat, route.tgChat, true);
      if (bot !== 'master') {
        this.store.put('delivery-target', snapshotKey, { ...target, bot, destination: this.c.owner, thread: undefined,
          ...(target.presentation ? { presentation: { ...target.presentation, bot } } : {}) });
        return true;
      }
    }
    let thread = route.thread;
    if (!thread || Number(thread) === Number(target.thread)) {
      const repaired = await this.effect('topic-repair:' + digest(JSON.stringify([this.c.management, chat, thread])), () => this.api.tg(this.token('master'), 'createForumTopic', {
        chat_id: this.c.management, name: String(route.name || this.store.get('contact', chat)?.name || 'WhatsApp conversation').slice(0, 128),
      }));
      thread = repaired.message_thread_id;
      this.store.put('route', chat, { ...route, thread, inboxRetired: false });
    }
    if (!thread) return false;
    this.store.put('delivery-target', snapshotKey, { ...target, thread });
    return true;
  }
  async sendText(bot: string, chat: string, text: string | Formatted, key: string, extra: Json = {}, kind = 'log') {
    const results: Json[] = [];
    let packets: Json[];
    if (typeof text === 'string') packets = chunks(text).map(part => ({ text: part }));
    else {
      packets = this.store.get('formatted-packets', key);
      if (!packets) {
        packets = splitFormatted(text).map(part => ({ text: part.text, entities: part.entities,
          ...(part.entities.some(entity => entity.type === 'text_link') ? { link_preview_options: { is_disabled: true } } : {}) }));
        this.store.put('formatted-packets', key, packets);
      }
    }
    for (const [i, packet] of packets.entries()) {
      const receipt = await this.effect(`${key}:text:${i}`, () => this.api.tg(this.token(bot), 'sendMessage', { chat_id: chat, ...extra, ...packet, text: packet.text || '[Empty message]' }));
      if (kind === 'chat') this.control.track(bot, { ...receipt, ...(extra.message_thread_id ? { message_thread_id: extra.message_thread_id, is_topic_message: true } : {}) }, 'chat');
      else this.control.reply(bot, receipt, extra);
      results.push(receipt);
    }
    return results;
  }
  async topic(chatId: string, name: string, key: string) {
    let route = this.store.get('route', chatId);
    if (route) return route;
    const contact = this.store.get('contact', chatId), bot = contact?.bot && this.store.get('bot', contact.bot);
    if (!chatId.endsWith('@g.us') && bot?.ready) {
      route = { chatId, name, tgChat: this.c.management, mode: 'dedicated', history: false, inboxRetired: true };
      this.store.put('route', chatId, route); return route;
    }
    if (!this.c.management) throw new ApiError('Management forum group has not been configured', 409, true);
    const topic = await this.effect(`${key}:topic`, () => this.api.tg(this.token('master'), 'createForumTopic', { chat_id: this.c.management, name: name.slice(0, 128) || chatId }));
    route = { chatId, name, tgChat: this.c.management, thread: topic.message_thread_id,
      mode: !chatId.endsWith('@g.us') && this.store.get('contact', chatId)?.bot ? 'dedicated' : 'inbox', history: false };
    this.store.put('route', chatId, route);
    this.management('new_conversation', { chatId }, 'new:' + chatId);
    return route;
  }
  queueRetireInbox(chatId: string, key: string) {
    this.store.enqueue('retire-inbox:' + key, 'retire-inbox:' + chatId, 'control-retire-inbox', { chatId });
  }
  async retireInbox(chatId: string) {
    const route = this.store.get('route', chatId); if (!route) return;
    const contact = this.store.get('contact', chatId), bot = contact?.bot && this.store.get('bot', contact.bot);
    if (!(route.mode === 'group' && String(route.tgChat) !== this.c.management) && !(route.mode === 'dedicated' && bot?.ready)) return;
    const topics = new Set<number>();
    if (String(route.inbox?.chat) === this.c.management && route.inbox.thread > 1) topics.add(Number(route.inbox.thread));
    if (String(route.tgChat) === this.c.management && route.thread > 1) topics.add(Number(route.thread));
    for (const row of this.store.db.prepare("SELECT DISTINCT t.thread FROM messages m JOIN telegram_messages t ON t.chat=m.tg_chat AND t.message=m.tg_id AND t.scope='*' WHERE m.wa_chat=? AND t.chat=? AND t.thread>1 AND t.deleted=0").all(chatId, this.c.management)) topics.add(Number(row.thread));
    for (const thread of topics) {
      if (this.store.all('route').some(other => other.chatId !== chatId && String(other.tgChat) === this.c.management && Number(other.thread) === thread)) throw new ApiError('This management topic is shared with another conversation', 409);
      if (this.store.db.prepare("SELECT 1 FROM messages m JOIN telegram_messages t ON t.chat=m.tg_chat AND t.message=m.tg_id AND t.scope='*' WHERE t.chat=? AND t.thread=? AND m.wa_chat!=? LIMIT 1").get(this.c.management, thread, chatId)) throw new ApiError('This management topic contains another conversation', 409);
      await this.effect('retire-inbox:' + digest(JSON.stringify([this.c.management, thread])), async () => {
        try { return await this.api.tg(this.token('master'), 'deleteForumTopic', { chat_id: this.c.management, message_thread_id: thread }); }
        catch (e) {
          if (e instanceof ApiError && e.status === 400 && e.reason === 'thread_missing') return { alreadyDeleted: true };
          if (e instanceof ApiError && (e.retry || e.uncertain)) throw new ApiError('Management topic deletion will retry', e.status, true, false, e.delay);
          throw e;
        }
      });
      this.store.put('retired-topic', digest(JSON.stringify([this.c.management, thread])), { chatId });
      this.store.db.prepare("UPDATE telegram_messages SET deleted=1,error=NULL WHERE scope='*' AND chat=? AND thread=?").run(this.c.management, thread);
    }
    const current = this.store.get('route', chatId);
    this.store.put('route', chatId, { ...current, inbox: undefined, ...(String(current.tgChat) === this.c.management ? { thread: undefined, inboxRetired: true } : {}) });
  }
  async preferred(sender: string, tgChat: string, privateChat = false) {
    const contact = this.store.get('contact', sender), bot = contact?.bot && this.store.get('bot', contact.bot);
    if (!bot) return 'master';
    if (privateChat) return bot.ready ? String(contact.bot) : 'master';
    try {
      const member = await this.api.tg(this.token('master'), 'getChatMember', { chat_id: tgChat, user_id: Number(contact.bot) });
      return ['member', 'administrator', 'creator'].includes(member.status) ? String(contact.bot) : 'master';
    } catch (e) { if (e instanceof ApiError && e.status === 400) return 'master'; throw e; }
  }
  async whatsApp(event: Json, key: string) {
    const status = event.event === 'status.received', m = status ? this.statuses.message(event.data) : event.data;
    if (!m) return;
    if (status && this.statuses.suppressed(m)) return;
    if (event.event.startsWith('session.') || event.event === 'message.failed') {
      this.management('session_status', { event: event.event, status: m.status || 'changed' }, key); return;
    }
    if (['message.edited', 'message.revoked', 'message.reaction', 'poll.vote'].includes(event.event)) return this.waChange(event, key);
    if (!m.id || !m.chatId || (!status && (m.isStatusBroadcast || ['channel', 'status', 'broadcast'].includes(m.kind)))) return;
    const rawSender = status || m.isGroup ? m.author || m.from : m.chatId;
    const sender = m.senderPhone && !m.fromMe ? `${m.senderPhone}@c.us` : this.store.canonical(rawSender);
    if (m.senderPhone && rawSender?.endsWith('@lid')) this.store.alias(rawSender, sender);
    const chat = this.store.canonical(m.chatId);
    if (this.store.get('revoked', `${chat}:${m.id}`)) return;
    const edit = this.store.get('latest-edit', `${chat}:${m.id}`);
    if (edit) { m.body = edit.body; if (edit.mentionedIds) m.mentionedIds = edit.mentionedIds; }
    if (!event.replay && (this.store.get('wa-complete', `${chat}:${m.id}`) || this.store.byWhatsApp(chat, m.id).some(row => row.direction === 'out'))) return;
    const contact = this.store.get('contact', sender) || {};
    this.store.put('contact', sender, { ...contact, name: contact.metadataSyncedAt ? contact.name : m.contact?.name || m.contact?.pushName || m.contact?.shortName || contact.name || sender });
    // In a private outgoing event, sender is the peer's chat ID. Only the session's
    // independently resolved self identity may be used for the "You" link.
    const linkSender = m.fromMe ? this.store.get('reaction-self', digest(this.c.session))?.sender || '' : m.senderPhone ? `${m.senderPhone}@c.us` : sender;
    if (!event.replay || !this.store.get('wamessage', `${chat}:${m.id}`)) this.store.put('wamessage', `${chat}:${m.id}`, { sender, name: this.store.get('contact', sender).name, fromMe: !!m.fromMe,
      historical: !!event.historical, timestamp: m.timestamp, status, linkSender: this.store.canonical(linkSender), mentionedIds: m.mentionedIds || [] });
    if (event.replay) {
      if (event.historyMode === 'all' && event.historyRun) {
        if (this.store.get('history-copy-complete', this.conversationHistory.copyKey(event.historyRun, chat, m.id))) return;
      } else {
        if (this.conversationHistory.delivered(chat, m.id, event.replay)) return;
        key = this.conversationHistory.begin(chat, m.id, event.replay, key);
      }
    }
    let route = event.replay ? { chatId: chat, tgChat: String(event.replay.chat), thread: event.replay.thread, mode: String(event.replay.chat).startsWith('-') ? 'group' : 'dedicated' }
      : status ? await this.statuses.topic() : await this.topic(chat, m.isGroup ? m.chatName || chat : this.store.get('contact', sender).name, key);
    if (['muted', 'ignored'].includes(route.mode)) return;
    const privateChat = route.mode === 'dedicated';
    const snapshotKey = key + (event.forceMaster ? ':fallback' : '');
    let snapshot = this.store.get('delivery-target', snapshotKey);
    if (!event.replay && snapshot?.thread && this.store.get('retired-topic', digest(JSON.stringify([String(snapshot.destination), snapshot.thread])))) snapshot = undefined;
    const legacy = !!snapshot && snapshot.formatVersion !== 2;
    const bot = snapshot?.bot || event.replay?.bot || (status || (m.fromMe && !privateChat) || event.forceMaster ? 'master' : await this.preferred(sender, route.tgChat, privateChat));
    if (!event.replay && bot === 'master' && route.inboxRetired) {
      const topic = await this.effect(key + ':restore-inbox', () => this.api.tg(this.token('master'), 'createForumTopic', { chat_id: this.c.management, name: String(route.name || 'WhatsApp conversation').slice(0, 128) }));
      route = { ...route, tgChat: this.c.management, thread: topic.message_thread_id, inboxRetired: false }; this.store.put('route', chat, route);
    }
    // A dedicated bot not yet started falls back to the management topic.
    const destination = snapshot?.destination || (privateChat && bot !== 'master' ? this.c.owner : route.tgChat);
    const thread = snapshot ? snapshot.thread : privateChat && bot !== 'master' ? undefined : route.thread;
    const deliveredTarget = { chat: String(destination), thread, ...(!String(destination).startsWith('-') ? { bot } : {}) };
    if (!event.replay) this.conversationHistory.begin(chat, m.id, deliveredTarget, key);
    this.store.put('delivery-target', snapshotKey, { bot, destination, thread, formatVersion: legacy ? 1 : 2 });
    // Message ids and file ids in private chats are scoped to each bot.
    key += `:to:${bot}:${destination}`;
    const label = m.fromMe ? 'You · sent from WhatsApp' : bot === 'master' ? this.store.get('contact', sender)?.name || sender : '';
    const historical = legacy && event.historical ? `[History · ${new Date(m.timestamp * 1000).toISOString()}]\n` : '';
    const prefix = historical + (label ? label + '\n' : '');
    const format: RelayFormat = snapshot?.presentation || { bot, sender: event.replay ? this.store.canonical(linkSender) : this.store.get('wamessage', `${chat}:${m.id}`).linkSender,
      name: this.store.get('contact', sender)?.name || sender, own: !!m.fromMe, historical: !!event.historical, status, timestamp: m.timestamp,
      ...(event.historical && !status ? { historyStyle: event.historyStyle || 'compact', timeZone: event.historyTimeZone || this.c.syncTimezone } : {}) };
    if (!legacy) this.store.put('delivery-target', snapshotKey, { bot, destination, thread, formatVersion: 2, presentation: format });
    const extra: Json = { ...(thread ? { message_thread_id: thread } : {}) };
    const freshQuote = event.historyMode === 'all' && event.historyRun && m.quotedMessage?.id
      ? this.store.get('history-copy', this.conversationHistory.copyKey(event.historyRun, chat, m.quotedMessage.id)) : undefined;
    const quoted = m.quotedMessage?.id && this.store.byWhatsApp(chat, m.quotedMessage.id).reverse().find(r => {
      if (event.historyMode === 'all' && (!freshQuote || r.tg_id !== freshQuote.tg_id || r.bot !== freshQuote.bot)) return false;
      if (r.tg_chat !== String(destination) || (privateChat && bot !== 'master' && r.bot !== bot)) return false;
      const recorded = this.store.db.prepare('SELECT deleted,thread FROM telegram_messages WHERE scope=? AND chat=? AND message=?').get(this.control.scope(r.bot, r.tg_chat), r.tg_chat, r.tg_id);
      return !recorded || (!recorded.deleted && Number(recorded.thread || 0) === Number(thread || 0));
    });
    if (quoted) extra.reply_parameters = { message_id: quoted.tg_id, allow_sending_without_reply: true };
    const preview = event.historical && !quoted && m.quotedMessage?.body ? styled('↪ ' + Array.from(String(m.quotedMessage.body)).slice(0, 300).join(''), 'italic') : undefined;
    const render = (body: string | Formatted): string | Formatted => legacy ? prefix + (typeof body === 'string' ? body : body.text)
      : relayText(format, preview ? concat(preview, '\n', body) : body);
    const mentionBody = legacy ? plain(m.body || '') : await this.mentions.incoming(m.body || '', m.mentionedIds, chat, String(destination), key + ':body');
    const text = render(mentionBody), textValue = typeof text === 'string' ? text : text.text;
    const header = legacy ? prefix.trimEnd() : relayHeader(format), headerValue = typeof header === 'string' ? header : header.text;
    const annotated = (notice: string) => legacy ? textValue + '\n[' + notice + ']' : render(concat(mentionBody, m.body ? '\n' : '', styled('[' + notice + ']', 'italic')));
    const record = (result: Json, media = false) => {
      this.store.map(chat, m.id, destination, result.message_id, bot, 'in', media);
      this.control.track(bot, { ...result, ...(thread ? { message_thread_id: thread, is_topic_message: true } : {}) }, 'chat');
      this.store.put('telegram-receipt', digest(JSON.stringify([bot, String(destination), result.message_id])), result);
      this.store.put('message-presentation', digest(JSON.stringify([bot, String(destination), result.message_id])), format);
      if (event.historyMode === 'all' && event.historyRun) {
        const id = this.conversationHistory.copyKey(event.historyRun, chat, m.id), previous = this.store.get('history-copy', id);
        if (!previous || (media && !previous.media)) this.store.put('history-copy', id, { tg_id: result.message_id, bot, media });
      }
    };
    const complete = () => {
      this.conversationHistory.complete(chat, m.id, deliveredTarget);
      if (event.historyMode === 'all' && event.historyRun) this.store.put('history-copy-complete', this.conversationHistory.copyKey(event.historyRun, chat, m.id), true);
      if (!event.replay) this.store.put('wa-complete', `${chat}:${m.id}`, true);
    };
    if (['image', 'video', 'audio', 'voice', 'sticker', 'document'].includes(m.type)) {
      let attachment: Json;
      try {
        if (m.archiveMedia) {
          try { attachment = await this.media.fromTelegram(this.token(m.archiveMedia.bot), m.archiveMedia.message); }
          catch (e) {
            if ((e instanceof ApiError && [400, 403, 404].includes(e.status)) || (e as any)?.code === 'ENOENT') attachment = await this.media.fromWhatsApp(m);
            else throw e;
          }
        }
        else attachment = await this.media.fromWhatsApp(m);
        if (!attachment) throw new ApiError('Archived attachment is unavailable', 404);
        if (attachment.mime) m.media = { ...m.media, mimetype: attachment.mime };
      }
      catch (e) {
        if (!(e instanceof ApiError) || ![404, 413].includes(e.status)) throw e;
        for (const r of await this.sendText(bot, destination, annotated(`${m.type} unavailable: ${e.message}`), key, extra, 'chat')) record(r);
        complete();
        return;
      }
      let method = ({ image: 'sendPhoto', video: 'sendVideo', audio: 'sendAudio', voice: 'sendVoice', sticker: 'sendSticker', document: 'sendDocument' } as Json)[m.type];
      let field = ({ image: 'photo', video: 'video', audio: 'audio', voice: 'voice', sticker: 'sticker', document: 'document' } as Json)[m.type];
      let path = attachment.path;
      let uploadName = attachment.name;
      if (m.type === 'sticker') {
        try {
          const prepared = await this.media.sticker(attachment, 'telegram');
          path = prepared.path; uploadName = prepared.name;
        } catch (e) {
          if (!(e instanceof ApiError) || e.status !== 415) throw e;
          method = 'sendDocument'; field = 'document';
        }
      }
      if (m.type === 'voice' && !m.media?.mimetype?.includes('ogg')) {
        try { path = await this.media.convert(path, true); } catch { method = 'sendDocument'; field = 'document'; }
      }
      if (m.type === 'image' && m.media?.mimetype && !/image\/(jpeg|png)/.test(m.media.mimetype)) {
        try { path = await this.media.convert(path); } catch { method = 'sendDocument'; field = 'document'; }
      }
      if ((m.type === 'sticker' && textValue.trim()) || textValue.length > 1000) {
        for (const r of await this.sendText(bot, destination, text, key + ':label', extra, 'chat')) record(r);
      }
      try {
        record(await this.effect(key + ':media', () => this.api.upload(this.token(bot), method, field, path, uploadName,
          { chat_id: destination, ...extra, ...(m.type === 'sticker' ? method === 'sendDocument' ? { caption: '[Original attachment; native presentation unavailable]' } : {} : legacy ? { caption: textValue.slice(0, 1000) }
            : captionFields(splitFormatted((textValue.length > 1000 ? header : text) as Formatted, 1000)[0])) })), true);
      } catch (e) {
        if (e instanceof ApiError && ['thread_missing', 'reply_missing'].includes(e.reason || '')) throw e;
        if (!(e instanceof ApiError) || ![400, 415].includes(e.status) || method === 'sendDocument') throw e;
        const notice = '\n[Original attachment; native presentation unavailable]';
        let fallback: string | Formatted = legacy ? (textValue + notice).slice(0, 1000) : annotated('Original attachment; native presentation unavailable');
        if (!legacy && (fallback as Formatted).text.length > 1000) {
          if (textValue.length <= 1000) for (const r of await this.sendText(bot, destination, text, key + ':original-label', extra, 'chat')) record(r);
          fallback = render(styled('[Original attachment; native presentation unavailable]', 'italic'));
        }
        record(await this.effect(key + ':original', () => this.api.upload(this.token(bot), 'sendDocument', 'document', attachment.path, attachment.name,
          { chat_id: destination, ...extra, ...(typeof fallback === 'string' ? { caption: fallback } : captionFields(splitFormatted(fallback, 1000)[0])) })), true);
      }
    } else if (m.type === 'location' && m.location) {
      if (textValue.trim()) for (const r of await this.sendText(bot, destination, text, key, extra, 'chat')) record(r);
      record(await this.effect(key + ':location', () => this.api.tg(this.token(bot), 'sendLocation', { chat_id: destination, ...extra, latitude: m.location.latitude, longitude: m.location.longitude })));
    } else if (m.type === 'contact' && /BEGIN:VCARD/i.test(m.body || '')) {
      const name = /(?:^|\n)FN:(.+)/i.exec(m.body)?.[1]?.trim();
      const number = /(?:^|\n)TEL[^:]*:([+\d ()-]+)/i.exec(m.body)?.[1]?.trim();
      if (name && number) {
        if (headerValue.trim()) for (const r of await this.sendText(bot, destination, header, key + ':label', extra, 'chat')) record(r);
        record(await this.effect(key + ':contact', () => this.api.tg(this.token(bot), 'sendContact', { chat_id: destination, ...extra, first_name: name, phone_number: number, vcard: m.body })));
      } else for (const r of await this.sendText(bot, destination, text, key, extra, 'chat')) record(r);
    } else if (m.type === 'poll' && m.poll) {
      const choices = m.poll.options.map((o: Json) => typeof o === 'string' ? o : o.name || o.text);
      const buttons = choices.slice(0, 12).map((choice: string) => [{ text: choice.slice(0, 64), callback_data: 'vote:' + this.action({ chat, message: m.id, choice, multiple: !!m.poll.allowMultipleAnswers }) }]);
      const poll = concat(styled('📊 ' + m.poll.name, 'bold'), '\n' + choices.map((o: string, i: number) => `${i + 1}. ${o}`).join('\n'));
      for (const r of await this.sendText(bot, destination, render(poll), key,
        { ...extra, reply_markup: { inline_keyboard: buttons } }, 'chat')) record(r);
    } else {
      const fallback = ['text', 'contact'].includes(m.type) ? '' : `${m.type || 'Unsupported message'}${m.call?.missed ? ' · missed' : ''}`;
      for (const r of await this.sendText(bot, destination, fallback ? annotated(fallback) : text, key, extra, 'chat')) record(r);
    }
    complete();
  }
  action(data: Json) {
    const id = randomUUID().slice(0, 12); this.store.put('action', id, data); return id;
  }
  async waChange(event: Json, key: string) {
    const m = event.data, chat = this.store.canonical(m.chatId || ''), id = m.revokedId || m.pollMessageId || m.messageId || m.id;
    if (event.event === 'message.revoked') {
      this.store.put('revoked', `${chat}:${id}`, true); await this.revocations.notify(chat, id); return;
    }
    if (this.store.get('revoked', `${chat}:${id}`)) return;
    if (event.event === 'message.edited') {
      this.store.put('latest-edit', `${chat}:${id}`, { body: m.body ?? m.newBody ?? m.text ?? '', ...(Array.isArray(m.mentionedIds) ? { mentionedIds: m.mentionedIds } : {}) });
      const original = this.store.get('wamessage', `${chat}:${id}`);
      if (original && Array.isArray(m.mentionedIds)) this.store.put('wamessage', `${chat}:${id}`, { ...original, mentionedIds: m.mentionedIds });
      if (this.store.get('outbound-edit', `${chat}:${id}`) === digest(m.body || '')) return;
    }
    const rows = this.store.byWhatsApp(chat, id);
    if (event.event === 'message.reaction') { await this.reactions.apply(chat, id, m, rows, key); return; }
    for (const [i, row] of rows.entries()) {
      const recorded = this.store.db.prepare('SELECT deleted FROM telegram_messages WHERE scope=? AND chat=? AND message=?').get(this.control.scope(row.bot, row.tg_chat), row.tg_chat, row.tg_id);
      if (recorded?.deleted) continue;
      const extra = { chat_id: row.tg_chat, message_id: row.tg_id };
      if (event.event === 'message.edited') {
        let source = this.store.get('wamessage', `${chat}:${id}`) || {};
        if (source.historical === undefined) {
          const saved = this.store.db.prepare("SELECT payload FROM jobs WHERE type='wa' AND json_extract(payload,'$.data.id')=? ORDER BY id").all(id)
            .map(row => JSON.parse(row.payload as string)).find(event => ['message.received', 'message.sent'].includes(event.event) && this.store.canonical(event.data.chatId || '') === chat);
          if (saved) source = { ...source, historical: !!saved.historical, timestamp: saved.data.timestamp, mentionedIds: saved.data.mentionedIds };
        }
        const own = source?.fromMe || row.direction === 'out', body = m.body ?? m.newBody ?? m.text ?? '';
        const linkSender = own ? this.store.get('reaction-self', digest(this.c.session))?.sender || '' : source.linkSender || source.sender || chat;
        const format: RelayFormat = this.store.get('message-presentation', digest(JSON.stringify([row.bot, row.tg_chat, row.tg_id]))) || { bot: row.bot, sender: this.store.canonical(linkSender), name: source.name || 'WhatsApp', own,
          historical: source.historical, status: source.status, timestamp: source.timestamp };
        const mentionBody = await this.mentions.incoming(body, m.mentionedIds ?? source.mentionedIds, chat, row.tg_chat, `${key}:edit-body:${i}`);
        const text = relayText(format, mentionBody), part = splitFormatted(text, row.media ? 1000 : 3800)[0];
        try { await this.effect(`${key}:edit:${i}`, () => this.api.tg(this.token(row.bot), row.media ? 'editMessageCaption' : 'editMessageText', { ...extra,
          ...(row.media ? captionFields(part) : { text: part.text || '[Empty message]', entities: part.entities, link_preview_options: { is_disabled: true } }) })); }
        catch (e) {
          if (!(e instanceof ApiError) || e.status !== 400) throw e;
          if (['message_missing', 'thread_missing'].includes(e.reason || '')) continue;
          const fallback = row.tg_chat.startsWith('-') ? 'master' : row.bot;
          const ledger = this.store.db.prepare('SELECT thread FROM telegram_messages WHERE scope=? AND chat=? AND message=?').get(this.control.scope(row.bot, row.tg_chat), row.tg_chat, row.tg_id);
          await this.sendText(fallback, row.tg_chat, relayText({ ...format, bot: fallback, editedNotice: true }, mentionBody), `${key}:notice:${i}`,
            { ...(ledger?.thread ? { message_thread_id: ledger.thread } : {}), reply_parameters: { message_id: row.tg_id, allow_sending_without_reply: true } });
        }
      } else {
        const text = event.event === 'poll.vote' ? `[Poll vote · ${m.voterId}] ${(m.selectedOptions || []).join(', ') || 'cleared'}` : `[Reaction · ${m.senderId || 'WhatsApp'}] ${m.reaction || 'removed'}`;
        await this.sendText('master', row.tg_chat, text, `${key}:reaction:${i}`, { reply_parameters: { message_id: row.tg_id, allow_sending_without_reply: true } });
      }
    }
  }
  async telegram(bot: string, update: Json, key: string) {
    if (update.managed_bot) return this.registerBot(update.managed_bot, key);
    const m = update.message || update.edited_message;
    if (m?.managed_bot_created && bot === 'master') return this.registerBot({ user: m.from, bot: m.managed_bot_created.bot }, key);
    if (update.my_chat_member) return; // Membership is checked live before selecting a participant bot.
    if (update.callback_query) {
      const q = update.callback_query;
      if (await this.conversationHistory.callback(bot, q)) return;
      if (await this.mentions.callback(bot, q, key)) return;
      if (await this.statuses.callback(bot, q, key)) return;
      if (/^rev-(delete|keep):/.test(q.data || '')) { await this.revocations.callback(bot, q); return; }
      try { await this.api.tg(this.token(bot), 'answerCallbackQuery', { callback_query_id: q.id }); }
      catch (e) { if (!(e instanceof ApiError) || e.status !== 400) throw e; }
      const [operation, id] = (q.data || '').split(':');
      const action = this.store.get('action', id);
      if (!action) return;
      this.management('operator_action', { operation, ...action }, key);
      return;
    }
    if (update.message_reaction) {
      const r = update.message_reaction, map = this.store.byTelegram(r.chat.id, r.message_id, bot);
      if (map && map.wa_chat !== 'status@broadcast') {
        const result = await this.reactions.outgoing(bot, r, key);
        if (result.emoji === undefined) {
          const ledger = this.store.db.prepare('SELECT thread FROM telegram_messages WHERE scope=? AND chat=? AND message=?').get(this.control.scope(bot, String(r.chat.id)), String(r.chat.id), r.message_id);
          this.store.enqueue('reaction-notice:' + key, 'reaction-notice:' + key, 'control-reaction-notice', { bot, chat: String(r.chat.id), thread: ledger?.thread,
            message: r.message_id, text: 'This reaction has no standard emoji equivalent and cannot be forwarded to WhatsApp. Choose a standard emoji reaction instead.' });
          return;
        }
        await this.effect(key + ':react', () => this.api.ow('messages/react', { chatId: map.wa_chat, messageId: map.wa_id, emoji: result.emoji }));
        await this.reactions.rememberOutgoing(map, { ...r, new_reaction: result.emoji ? [{ type: 'emoji', emoji: result.emoji }] : [] }, bot);
      }
      return;
    }
    if (!m) return;
    const chat = String(m.chat.id), text = m.text || m.caption || '';
    if (bot === 'master' && m.chat.type === 'private' && !update.edited_message && m.chat_shared) {
      this.control.command(bot, m);
      const shared = m.chat_shared, request = this.store.get('chat-request', String(shared.request_id));
      if (!this.validMirrorRequest(request) || request.used || !/^-\d+$/.test(String(shared.chat_id))) {
        await this.sendText('master', chat, 'This group selection is no longer active. Use /group in the WhatsApp group’s inbox topic to start again.', key); return;
      }
      this.management('link_group', { chatId: request.chatId, tgChat: String(shared.chat_id), requestId: String(shared.request_id) }, key);
      return;
    }
    const parsed = commandOf(m);
    if (parsed || text.trimStart().startsWith('/')) {
      if (update.edited_message || !parsed) return;
      return this.command(bot, m, key);
    }
    if (serviceMessage(m)) return;
    if (this.statuses.isTopic(m)) {
      if (!update.edited_message) await this.sendText(bot, chat, 'This topic archives WhatsApp Status posts. Reply to a post with /mute to mute its author’s Status posts, or open the person’s chat to reply.', key, { message_thread_id: m.message_thread_id });
      return;
    }
    if (update.edited_message && this.store.db.prepare("SELECT 1 FROM telegram_messages WHERE scope=? AND chat=? AND message=? AND kind='command'").get(this.control.scope(bot, chat), chat, m.message_id)) return;
    let { route, thread, contact } = this.telegramContext(bot, m);
    if (m.chat.type === 'private' && bot !== 'master') {
      if (!contact) {
        await this.sendText(bot, chat, 'Connect this bot to a WhatsApp contact in the master bot first, then send your message again.', key); return;
      }
      if (!route) route = await this.topic(contact.id, contact.name || contact.id, key + ':dm-route');
      const registered = this.store.get('bot', bot);
      if (!registered.ready) this.store.put('bot', bot, { ...registered, ready: true });
    }
    if (!route || ['muted', 'ignored'].includes(route.mode)) return;
    const reply = m.reply_to_message && this.store.byTelegram(chat, m.reply_to_message.message_id, bot);
    if (reply && reply.wa_chat !== route.chatId) throw new ApiError('Reply belongs to a different WhatsApp conversation', 400);
    const quotedMessageId = reply?.wa_id;
    const mapped = this.store.byTelegram(chat, m.message_id, bot);
    if (mapped && (!update.edited_message || mapped.direction !== 'out')) return;
    const sendKey = `send:${m.chat.type === 'private' ? bot + ':' : ''}${chat}:${m.message_id}`;
    let outgoing: Json;
    try { outgoing = await this.mentions.outgoing(route.chatId, m, update.edited_message ? key + ':edit-mentions' : sendKey); }
    catch (e) {
      if (!(e instanceof MentionError)) throw e;
      await this.sendText(bot, chat, e.message, key + ':mention-error', thread ? { message_thread_id: thread } : {}); return;
    }
    const waText = outgoing.text, mentions = outgoing.mentions;
    if (update.edited_message) {
      if (mapped?.direction === 'out') {
        const refusalKey = digest(JSON.stringify([this.c.session, key]));
        const refused = (notice: string, status: number) => {
          this.store.db.exec('BEGIN IMMEDIATE');
          try {
            this.store.put('edit-refusal', refusalKey, { status, at: Date.now() });
            this.store.enqueue('edit-notice:' + key, 'edit-notice:' + key, 'control-edit-notice', { bot, chat, thread, message: m.message_id, text: notice });
            this.store.db.exec('COMMIT');
          } catch (e) { this.store.db.exec('ROLLBACK'); throw e; }
        };
        if (this.store.get('edit-refusal', refusalKey)) return;
        if (!waText) { refused('Clearing a WhatsApp caption is not supported here. Its WhatsApp caption was kept; new messages can continue.', 400); return; }
        try { await this.effect(key + ':edit', () => this.api.ow('messages/edit', { chatId: mapped.wa_chat, messageId: mapped.wa_id, body: waText, mentions })); }
        catch (e) {
          if (!(e instanceof ApiError) || e.service !== 'openwa' || e.retry || e.uncertain || ![400, 403, 404, 413, 422].includes(e.status)) throw e;
          refused('This edit was not applied on WhatsApp. WhatsApp may refuse edits to older or unsupported messages. New messages can continue.', e.status); return;
        }
        this.store.put('outbound-edit', `${mapped.wa_chat}:${mapped.wa_id}`, digest(waText));
      }
      return;
    }
    let result: Json;
    const stickerSnapshot = this.store.get('outbound-sticker', sendKey);
    let attachment = stickerSnapshot?.attachment || await this.media.fromTelegram(this.token(bot), m);
    if (attachment) {
      let stickerReady = stickerSnapshot?.native || false;
      if (attachment.kind === 'sticker' && !stickerSnapshot) {
        try { attachment = { ...attachment, ...await this.media.sticker(attachment, 'whatsapp') }; stickerReady = true; }
        catch (e) { if (!(e instanceof ApiError) || e.status !== 415) throw e; }
      }
      const method = ({ photo: 'send-image', video: 'send-video', video_note: 'send-video', animation: 'send-video', audio: 'send-audio', voice: 'send-audio', sticker: stickerReady ? 'send-sticker' : 'send-document', document: 'send-document' } as Json)[attachment.kind];
      let bytes: Buffer;
      try { bytes = await readFile(attachment.path); }
      catch (e) {
        if (!stickerSnapshot || (e as any)?.code !== 'ENOENT') throw e;
        // Media expires after a day. Regeneration must match the frozen send bytes.
        attachment = await this.media.fromTelegram(this.token(bot), m);
        if (stickerReady) attachment = { ...attachment, ...await this.media.sticker(attachment!, 'whatsapp') };
        bytes = await readFile(attachment!.path);
      }
      if (attachment.kind === 'sticker') {
        const hash = digest(bytes.toString('base64'));
        if (stickerSnapshot && (hash !== stickerSnapshot.hash || attachment.mime !== stickerSnapshot.attachment.mime || attachment.name !== stickerSnapshot.attachment.name)) {
          throw new ApiError('Prepared sticker changed; inspect before retrying', 409);
        }
        this.store.put('outbound-sticker', sendKey, { attachment, native: stickerReady, hash });
      }
      result = await this.effect(sendKey, () => this.api.ow('messages/' + method, {
        chatId: route.chatId, base64: bytes.toString('base64'), mimetype: attachment.mime,
        filename: attachment.name, caption: waText || undefined, quotedMessageId, mentions,
        ...(attachment.kind === 'voice' ? { ptt: true } : {}),
      }, digest(sendKey)));
    } else if (m.location) result = await this.effect(sendKey, () => this.api.ow('messages/send-location', { chatId: route.chatId, latitude: m.location.latitude, longitude: m.location.longitude }, digest(sendKey)));
    else if (m.contact) result = await this.effect(sendKey, () => this.api.ow('messages/send-contact', { chatId: route.chatId, contactName: [m.contact.first_name, m.contact.last_name].filter(Boolean).join(' '), contactNumber: m.contact.phone_number }, digest(sendKey)));
    else if (m.poll) result = await this.effect(sendKey, () => this.api.ow('messages/send-poll', { chatId: route.chatId, name: m.poll.question, options: m.poll.options.map((o: Json) => o.text), allowMultipleAnswers: m.poll.allows_multiple_answers, quotedMessageId }, digest(sendKey)));
    else if (waText) result = await this.effect(sendKey, () => this.api.ow(quotedMessageId ? 'messages/reply' : 'messages/send-text', { chatId: route.chatId, text: waText, quotedMessageId, mentions }, digest(sendKey)));
    else { await this.sendText('master', chat, 'This Telegram message type cannot be sent to WhatsApp.', key, thread ? { message_thread_id: thread } : {}); return; }
    if (!result.messageId) throw new ApiError('WhatsApp did not return a message ID; inspect before retrying', 0, false, true);
    this.store.map(route.chatId, result.messageId, chat, m.message_id, bot, 'out', !!attachment);
    // Read receipts get their own job: a receipt failure must never replay the send.
    this.management('mark_read', { chatId: route.chatId }, sendKey + ':read');
  }
  async command(bot: string, m: Json, key: string) {
    const parsed = commandOf(m);
    if (!parsed || !(await this.control.addressed(bot, parsed, m.chat.type))) return;
    this.control.command(bot, m);
    const { name: command, args } = parsed;
    const { route, thread, contact } = this.telegramContext(bot, m);
    const chat = String(m.chat.id), extra = thread ? { message_thread_id: thread } : {};
    if (command === 'mention') { await this.mentions.command(bot, m, args, key); return; }
    if (await this.statuses.command(bot, m, command, args, key)) return;
    if (command === 'start' && m.chat.type === 'private') {
      const parameter = /^mirror_([a-f0-9-]+)$/.exec(args[0] || '')?.[1];
      if (bot === 'master' && parameter) { await this.beginMirrorGroup(parameter, key); return; }
      if (bot !== 'master') {
        const registered = this.store.get('bot', bot); registered.ready = true; this.store.put('bot', bot, registered);
        await this.sendText(bot, chat, contact ? 'Ready. Messages here will be sent to your linked WhatsApp contact. Use /history 50 to import recent messages here.'
          : 'Connect this bot to a WhatsApp contact in the master bot first: select the contact in its binding message, then return here.', key);
        if (contact) { await this.topic(contact.id, contact.name || contact.id, key + ':direct'); this.queueRetireInbox(contact.id, key); }
      } else await this.sendText(bot, chat, 'WhatsApp bridge ready. Use /statuses for WhatsApp Status posts and /muted_statuses to manage Status mutes. In the Status topic, reply to a post with /mute or /unmute_status. Use /sync all here for every group and contact bot. In a conversation: /create, /read, /history 50, /keep, /mute, /ignore, /group. Reply with /delete or /react 👍. /clear log removes notices; /clear all clears the current management topic, including General. Commands and notices disappear automatically. Use /status for delivery and sync state.', key);
      return;
    }
    if (command === 'start' && bot !== 'master') {
      const registered = this.store.get('bot', bot);
      await this.sendText(bot, chat, 'Open this member bot privately to chat with its WhatsApp contact.', key,
        registered.username ? { ...extra, reply_markup: { inline_keyboard: [[{ text: 'Open private chat', url: `https://t.me/${registered.username}?start=bridge` }]] } } : extra); return;
    }
    if (['clear', 'clear_log', 'clear_all'].includes(command)) {
      const mode = command === 'clear_all' ? 'all' : command === 'clear_log' ? 'log' : args[0] || 'log';
      if (!['all', 'log'].includes(mode) || args.length > (command === 'clear' ? 1 : 0)) { await this.sendText(bot, chat, 'Use /clear log for notices, or /clear all in a management topic.', key, extra); return; }
      if (mode === 'all' && !(bot === 'master' && chat === this.c.management)) { await this.sendText(bot, chat, '/clear all is available only in the management group, where it clears the current topic. Use /clear log here to remove bot notices.', key, extra); return; }
      this.control.queueClear(bot, m, mode);
      await this.sendText(bot, chat, `Clearing ${mode === 'all' ? 'recorded Telegram messages in this management topic' : 'bot notices in this chat/topic'} in the background. Telegram limits deletion to messages under 48 hours old.`, key, extra); return;
    }
    if (command === 'inbox' && bot === 'master') {
      const details = await this.api.tg(this.token('master'), 'getChat', { chat_id: chat });
      if (!details.is_forum || chat !== this.c.management) {
        await this.sendText(bot, chat, `Set TELEGRAM_MANAGEMENT_CHAT_ID=${chat} in bridge.env and enable Topics in this private group, then restart the bridge.`, key, extra); return;
      }
      await this.sendText(bot, chat, 'Management inbox connected. Incoming WhatsApp conversations will get separate topics.', key, extra); return;
    }
    if (command === 'status') {
      await this.sendText(bot, chat, JSON.stringify(this.status(), null, 2), key, extra); return;
    }
    if (command === 'statuses' && bot === 'master') {
      const feed = await this.statuses.topic();
      this.statuses.queueRefresh(key);
      await this.sendText(bot, chat, 'Refreshing available WhatsApp Status posts. New posts appear in the Status topic automatically.', key,
        { ...extra, reply_markup: { inline_keyboard: [[{ text: 'Open WhatsApp Status', url: this.statuses.url(feed) }]] } }); return;
    }
    if (command === 'start' && bot === 'master' && args[0]?.startsWith('sync_')) {
      const request = this.store.get('action', args[0].slice('sync_'.length));
      const route = request && this.store.get('route', request.chatId);
      if (!request || request.kind !== 'sync-permission' || request.owner !== this.c.owner || request.session !== digest(this.c.session)
        || request.expires < Date.now() || String(request.tgChat) !== chat || String(route?.tgChat) !== chat) {
        await this.sendText(bot, chat, 'This permission link is no longer active for this group. Use /sync in the correct group for a fresh link.', key, extra); return;
      }
      this.management('sync_metadata', { scope: 'group', id: request.chatId, notify: { bot, chat, thread } }, key);
      await this.sendText(bot, chat, 'Checking group permissions and queuing its metadata sync.', key, extra); return;
    }
    if (command === 'start' && bot === 'master') {
      await this.sendText(bot, chat, 'Use /create while replying to a participant, /sync for metadata, /group for a mirror group, or /clear log for notices. Use /clear all in management to clear only the current topic, including General. Open Bifrost privately for full help.', key, extra); return;
    }
    if (command === 'sync' && bot === 'master' && (args[0] === 'all' || (m.chat.type === 'private' && !args[0]) || (chat === this.c.management && !thread && !args[0]))) {
      if ((args[0] && args[0] !== 'all') || !(m.chat.type === 'private' || chat === this.c.management)) { await this.sendText(bot, chat, 'Use /sync all in your private chat with Bifrost or in the management group.', key, extra); return; }
      this.management('sync_metadata', { scope: 'all', notify: { bot, chat, thread } }, key);
      await this.sendText(bot, chat, 'Metadata sync queued. WhatsApp group and contact details will be checked in the background.', key, extra); return;
    }
    if (command === 'sync' && bot !== 'master') {
      if (args.length) { await this.sendText(bot, chat, 'Use /sync here for this contact, or /sync all in your private chat with the master bot.', key); return; }
      // A member bot can be bound before this contact has a one-to-one inbox topic.
      const bound = contact || this.store.all('contact').find(c => c.bot === bot);
      if (!bound) { await this.sendText(bot, chat, 'Connect this bot to a WhatsApp contact in the master bot first: select the contact in its binding message, then run /sync here again.', key, extra); return; }
      this.management('sync_metadata', { scope: 'contact', id: bound.id, notify: { bot, chat, ...(thread ? { thread } : {}) } }, key);
      await this.sendText(bot, chat, 'Metadata sync queued for this contact.', key); return;
    }
    if (command === 'link' && bot === 'master' && chat !== this.c.management && ['group', 'supergroup'].includes(m.chat.type)) {
      const chatId = args[0];
      if (!chatId?.endsWith('@g.us')) { await this.sendText(bot, chat, 'Use /link <WhatsApp group ID ending in @g.us>. The ID is shown in its management topic.', key, extra); return; }
      this.management('link_group', { chatId, tgChat: chat }, key); return;
    }
    const reply = m.reply_to_message && this.store.byTelegram(chat, m.reply_to_message.message_id, bot);
    if (this.statuses.isTopic(m)) {
      await this.sendText(bot, chat, 'This is the WhatsApp Status archive. Use /statuses to refresh it, reply to a post with /mute to mute its author, or use /muted_statuses to manage mutes. Open a person’s chat to reply.', key, extra); return;
    }
    if (command === 'history') {
      const count = args.length ? Number(args[0]) : this.c.history || 20;
      if (args.length > 1 || (args.length && !/^[1-9]\d{0,3}$/.test(args[0])) || count > 2000) { await this.sendText(bot, chat, 'Use /history <n>, with n from 1 to 2000.', key, extra); return; }
      let conversation = route;
      if (m.chat.type === 'private' && bot !== 'master' && contact) {
        const registered = this.store.get('bot', bot); if (!registered.ready) this.store.put('bot', bot, { ...registered, ready: true });
      }
      if (!conversation && contact && m.chat.type === 'private') conversation = await this.topic(contact.id, contact.name || contact.id, key + ':direct');
      if (!conversation) { await this.sendText(bot, chat, 'Use /history <n> inside a mirror group, a linked management topic, or the contact’s bot DM.', key, extra); return; }
      if (['muted', 'ignored'].includes(conversation.mode)) { await this.sendText(bot, chat, 'Use /keep to resume this conversation before importing history.', key, extra); return; }
      await this.conversationHistory.prompt(conversation.chatId, { chat, thread, ...(m.chat.type === 'private' ? { bot } : {}) }, count, bot, key); return;
    }
    if (!route) { await this.sendText(bot, chat, 'Use this command inside a linked conversation or its management topic.', key, extra); return; }
    if (reply && reply.wa_chat !== route.chatId) throw new ApiError('Command target belongs to another WhatsApp conversation', 400);
    if (command === 'sync') {
      if (args.length) { await this.sendText(bot, chat, 'Use /sync here for this conversation, or /sync all in your private chat with Bifrost.', key, extra); return; }
      this.management('sync_metadata', { scope: route.chatId.endsWith('@g.us') ? 'group' : 'contact', id: route.chatId, notify: { bot, chat, thread } }, key);
      await this.sendText(bot, chat, 'Metadata sync queued for this conversation.', key, extra); return;
    }
    if (command === 'group' && bot === 'master') {
      if (!route.chatId.endsWith('@g.us')) { await this.sendText(bot, chat, 'Use /group in a WhatsApp group’s inbox topic.', key, extra); return; }
      await this.sendMirrorButton(route.chatId, key); return;
    }
    if (['read', 'keep', 'mute', 'ignore', 'history', 'create', 'delete', 'react'].includes(command)) {
      let sender = route.chatId;
      if (sender.endsWith('@g.us') && command === 'create') {
        sender = reply && this.store.get('wamessage', `${reply.wa_chat}:${reply.wa_id}`)?.sender;
        if (!sender) { await this.sendText(bot, chat, 'Reply to a participant’s message with /create to create their bot.', key, extra); return; }
      }
      if (['delete', 'react'].includes(command) && !reply) { await this.sendText(bot, chat, 'Reply to the message you want to act on.', key, extra); return; }
      this.management('operator_action', { operation: command, chatId: route.chatId, sender, message: reply?.wa_id, emoji: args[0] || '' }, key);
    } else await this.sendText(bot, chat, 'Unknown command. Use /start in the master bot for help.', key, extra);
  }
  async registerBot(info: Json, key: string) {
    if (String(info.user?.id) !== this.c.owner) return;
    const id = String(info.bot.id), old = this.store.get('bot', id);
    const token = await this.api.tg(this.token('master'), 'getManagedBotToken', { user_id: Number(id) });
    this.store.put('bot', id, { ...old, token: this.store.seal(token), username: info.bot.username, ready: old?.ready || false });
    await this.installWebhook(id);
    if (old) return;
    const pending = this.store.all('provision').filter(p => p.state === 'pending');
    const buttons = pending.map(p => [{ text: this.store.get('contact', p.id)?.name || p.id,
      callback_data: 'bind:' + this.action({ bot: id, sender: p.id }) }]);
    await this.sendText('master', this.c.owner, `Created @${info.bot.username}. Choose the WhatsApp contact to bind it to.`, key,
      buttons.length ? { reply_markup: { inline_keyboard: buttons.slice(0, 20) } } : {});
  }
  async manage(event: Json, key: string) {
    const d = event.data;
    if (event.type === 'sync_metadata') {
      if (!['all', 'group', 'contact'].includes(d.scope)) throw new ApiError('Invalid metadata sync scope', 400);
      this.metadata.queue({ scope: d.scope, id: d.id, source: 'command', requestId: key, notify: d.notify }); return;
    }
    if (event.type === 'mark_read') { await this.markRead(d.chatId); return; }
    if (event.type === 'session_status' || event.type === 'delivery_problem') {
      const text = event.type === 'delivery_problem' ? `Delivery job ${d.job} is ${d.state}: ${d.error || 'review required'}. Use /status. Review it with the local bridge CLI before retrying.` : `OpenWA: ${d.event} · ${d.status}`;
      await this.sendText('master', this.c.management || this.c.owner, text, key); return;
    }
    if (event.type === 'link_group') {
      const old = this.store.get('route', d.chatId);
      if (!old) throw new ApiError('Receive a message from this WhatsApp group in the inbox before linking it', 400);
      if (!d.chatId.endsWith('@g.us') || String(d.tgChat) === this.c.management) throw new ApiError('Choose a separate private group for this WhatsApp group', 400);
      const request = d.requestId && this.store.get('chat-request', String(d.requestId));
      if (d.requestId && (!request || request.chatId !== d.chatId || !this.validMirrorRequest(request, String(d.tgChat)))) throw new ApiError('This group selection expired or its conversation changed; use /group again', 400);
      const tgChat = await this.api.tg(this.token('master'), 'getChat', { chat_id: d.tgChat });
      if (!['group', 'supergroup'].includes(tgChat.type) || tgChat.username) throw new ApiError('Mirror destination must be a private Telegram group', 400);
      const me = await this.api.tg(this.token('master'), 'getMe');
      const membership = await this.api.tg(this.token('master'), 'getChatMember', { chat_id: d.tgChat, user_id: me.id });
      if (membership.status !== 'administrator') throw new ApiError('Make the master an administrator of the mirror group first', 400);
      if (this.store.all('route').some(r => r.chatId !== d.chatId && r.tgChat === d.tgChat)) throw new ApiError('Telegram group is already linked', 400);
      this.store.put('route', d.chatId, { ...old, inbox: old.inbox || (String(old.tgChat) === this.c.management && old.thread ? { chat: old.tgChat, thread: old.thread } : undefined), tgChat: d.tgChat, thread: undefined, mode: 'group' });
      if (request) this.store.put('chat-request', String(d.requestId), { ...request, used: true, selectedChat: String(d.tgChat) });
      await this.sendText('master', d.tgChat, `Linked WhatsApp group ${d.chatId}. Your messages here will be sent to that group. Use /history 50 to import recent messages here.`, key);
      if (request) await this.sendText('master', this.c.owner, `Linked ${old.name || d.chatId} to its Telegram group. You can now chat there.`, key + ':confirmed', { reply_markup: { remove_keyboard: true } });
      this.metadata.queue({ scope: 'group', id: d.chatId, source: 'link', requestId: key });
      this.queueRetireInbox(d.chatId, key); return;
    }
    if (event.type === 'new_conversation') {
      const route = this.store.get('route', d.chatId); if (!route) return;
      const buttons = ['create', 'keep', 'mute', 'ignore', 'read', 'history'].filter(op => !(d.chatId.endsWith('@g.us') && op === 'create'))
        .map(op => ({ text: ({ create: 'Create bot', keep: 'Keep here', mute: 'Mute', ignore: 'Ignore', read: 'Mark read', history: 'Recent history' } as Json)[op], callback_data: op + ':' + this.action({ chatId: d.chatId, sender: d.chatId }) }));
      const groupButton = d.chatId.endsWith('@g.us') ? await this.mirrorButton(d.chatId) : undefined;
      await this.sendText('master', route.tgChat, `${route.name}\nWhatsApp ID: ${d.chatId}\nReply in this topic to send a message. ${d.chatId.endsWith('@g.us') ? 'Reply to a participant with /create to provision their bot.' : 'Choose whether to keep this conversation here or create a dedicated bot.'}`, key,
        { message_thread_id: route.thread, reply_markup: { inline_keyboard: [...(groupButton ? [[groupButton]] : []), buttons.slice(0, 3), buttons.slice(3)] } });
      this.metadata.queue({ scope: d.chatId.endsWith('@g.us') ? 'group' : 'contact', id: d.chatId, source: 'new', requestId: key });
      return;
    }
    if (event.type !== 'operator_action') throw new ApiError('Unknown management action', 400);
    const op = d.operation, chatId = d.chatId || d.chat;
    if (op === 'bind') {
      const pending = this.store.get('provision', d.sender), contact = this.store.get('contact', d.sender), bot = this.store.get('bot', d.bot);
      if (!pending || pending.state !== 'pending' || !contact || !bot) throw new ApiError('Provisioning request is no longer pending', 400);
      if (this.store.all('contact').some(c => c.bot === d.bot && c.id !== d.sender)) throw new ApiError('This bot is already bound to another person', 400);
      this.store.put('contact', d.sender, { ...contact, bot: d.bot });
      this.store.put('provision', d.sender, { ...pending, state: 'bound' });
      const direct = this.store.get('route', d.sender);
      if (direct && ['inbox', 'dedicated'].includes(direct.mode)) this.store.put('route', d.sender, { ...direct, mode: 'dedicated' });
      this.metadata.queue({ scope: 'contact', id: d.sender, source: 'bind', requestId: key });
      await this.sendText('master', this.c.owner, `Bound @${bot.username} to ${contact.name}. Start it for private chat, and add it to any mirrored groups where you want its identity.`, key,
        { reply_markup: { inline_keyboard: [[{ text: 'Start contact bot', url: `https://t.me/${bot.username}?start=bridge` }], [{ text: 'Add to a group', url: `https://t.me/${bot.username}?startgroup=bridge` }]] } });
      if (bot.ready) this.queueRetireInbox(d.sender, key); return;
    }
    if (op === 'vote') {
      const previous = this.store.get('poll-selection', `${chatId}:${d.message}`) || [];
      const options = d.multiple ? previous.includes(d.choice) ? previous.filter((o: string) => o !== d.choice) : [...previous, d.choice] : [d.choice];
      await this.effect(key + ':vote', () => this.api.ow('messages/vote-poll', { chatId, messageId: d.message, options }));
      this.store.put('poll-selection', `${chatId}:${d.message}`, options); return;
    }
    const route = this.store.get('route', chatId); if (!route) throw new ApiError('Unknown conversation', 404);
    const extra = route.thread ? { message_thread_id: route.thread } : {};
    if (op === 'create') {
      const sender = d.sender || chatId;
      if (sender.endsWith('@g.us')) throw new ApiError('Choose a group participant to create a contact bot', 400);
      const contact = this.store.get('contact', sender) || { name: sender };
      if (contact.bot) { await this.sendText('master', route.tgChat, `This person already has @${this.store.get('bot', contact.bot).username}. Add that bot to this group to give it priority.`, key, extra); return; }
      this.store.put('provision', sender, { state: 'pending', chatId });
      const me = await this.api.tg(this.token('master'), 'getMe');
      const username = `wa_${digest(sender + this.c.owner).slice(0, 16)}_bot`;
      const url = `https://t.me/newbot/${me.username}/${username}?name=${encodeURIComponent(contact.name.slice(0, 64))}`;
      await this.sendText('master', route.tgChat, 'Create this contact’s bot, then confirm its contact binding in your private chat with the master. If Telegram refuses creation at your bot limit, messages continue through the master.', key,
        { ...extra, reply_markup: { inline_keyboard: [[{ text: 'Create contact bot', url }]] } }); return;
    }
    if (op === 'history') { await this.queueHistory(chatId, key); return; }
    if (op === 'read') { await this.markRead(chatId); return; }
    if (op === 'delete') { await this.effect(key + ':delete', () => this.api.ow('messages/delete', { chatId, messageId: d.message, forEveryone: true })); return; }
    if (op === 'react') { await this.effect(key + ':react', () => this.api.ow('messages/react', { chatId, messageId: d.message, emoji: d.emoji })); return; }
    if (['keep', 'mute', 'ignore'].includes(op)) {
      this.store.put('route', chatId, { ...route, mode: ({ keep: route.mode === 'group' ? 'group' : 'inbox', mute: 'muted', ignore: 'ignored' } as Json)[op] });
      await this.sendText('master', route.tgChat, ({ keep: 'Conversation stays here.', mute: 'Conversation muted. Use /keep to resume.', ignore: 'Conversation ignored. Use /keep to resume.' } as Json)[op], key, extra); return;
    }
    throw new ApiError('Unsupported management operation', 400);
  }
  validMirrorRequest(request: Json, completedChat?: string) {
    if (!request || request.owner !== this.c.owner || request.session !== digest(this.c.session) || request.expires < Date.now()) return false;
    const route = this.store.get('route', request.chatId);
    if (!route) return false;
    if (completedChat && request.used && request.selectedChat === completedChat && String(route.tgChat) === completedChat) return true;
    return !request.used && String(route.tgChat) === request.fromChat && Number(route.thread || 0) === request.fromThread;
  }
  async mirrorButton(chatId: string) {
    const route = this.store.get('route', chatId);
    if (!route || !chatId.endsWith('@g.us')) throw new ApiError('Choose a known WhatsApp group', 400);
    const me = await this.api.tg(this.token('master'), 'getMe');
    const id = this.action({ kind: 'mirror-group', chatId, owner: this.c.owner, session: digest(this.c.session),
      fromChat: String(route.tgChat), fromThread: Number(route.thread || 0), expires: Date.now() + 86400000 });
    return { text: 'Create / link Telegram group', url: `https://t.me/${me.username}?start=mirror_${id}` };
  }
  async sendMirrorButton(chatId: string, key: string) {
    const route = this.store.get('route', chatId), button = await this.mirrorButton(chatId);
    await this.sendText('master', route.tgChat, 'Create a private Telegram group for this conversation, or select one you own. Telegram will ask you to confirm adding Bifrost as an administrator.', key,
      { ...(route.thread ? { message_thread_id: route.thread } : {}), reply_markup: { inline_keyboard: [[button]] } });
  }
  async beginMirrorGroup(id: string, key: string) {
    const request = this.store.get('action', id);
    if (request?.kind !== 'mirror-group' || !this.validMirrorRequest(request)) {
      await this.sendText('master', this.c.owner, 'This group button expired or its conversation changed. Use /group in the WhatsApp group’s inbox topic for a fresh button.', key); return;
    }
    let requestId: number;
    do { requestId = randomInt(1, 2147483647); } while (this.store.get('chat-request', String(requestId)));
    this.store.put('chat-request', String(requestId), request);
    const rights = { is_anonymous: false, can_manage_chat: true, can_delete_messages: true, can_manage_video_chats: false,
      can_restrict_members: false, can_promote_members: false, can_change_info: true, can_invite_users: true,
      can_post_stories: false, can_edit_stories: false, can_delete_stories: false, can_pin_messages: false, can_manage_topics: false };
    await this.sendText('master', this.c.owner, `Choose or create a private Telegram group for ${this.store.get('route', request.chatId).name || request.chatId}. Confirm Bifrost’s administrator permissions. If your Telegram client has no create option in the picker, create a group first, then select it here.`, key,
      { reply_markup: { keyboard: [[{ text: 'Create / select Telegram group', request_chat: { request_id: requestId, chat_is_channel: false,
        chat_has_username: false, chat_is_created: true, bot_is_member: true,
        user_administrator_rights: rights, bot_administrator_rights: rights, request_title: true } }]], resize_keyboard: true, one_time_keyboard: true } });
  }
  async queueHistory(chatId: string, suffix = 'manual') {
    const target = this.conversationHistory.target(chatId);
    if (target) await this.conversationHistory.prompt(chatId, target, this.c.history || 20, target.bot || 'master', suffix);
  }
  async markRead(chatId: string) {
    const result = await this.api.ow('chats/read', { chatId });
    if (result.success === false) throw new ApiError('OpenWA declined the read receipt; mark read in WhatsApp', 409);
  }
  async history(data: Json, key: string) {
    await this.conversationHistory.fetch(data, key);
  }
  status() {
    return { configured: !!(this.c.masterToken && this.c.owner && this.c.management && this.c.openwaKey && this.c.session),
      bots: this.store.all('bot').length, conversations: this.store.all('route').length,
      metadataSync: { cron: this.c.syncCron, timezone: this.c.syncTimezone, last: this.store.get('sync-status', 'last') || null },
      telegramControls: { commandTTLSeconds: this.c.commandTTL, buttonTTLSeconds: this.c.controlTTL,
        cleanupProblems: this.store.db.prepare('SELECT count(*) AS n FROM telegram_messages WHERE deleted=0 AND error IS NOT NULL').get()!.n },
      jobs: this.store.db.prepare('SELECT state,count(*) AS count FROM jobs GROUP BY state').all() };
  }
  async migrateBot(bot: string) {
    // A bot must be logged out of the hosted API before first use on a local API server.
    if (!this.store.get('migrated', bot)) {
      const token = this.token(bot);
      await this.api.request(`https://api.telegram.org/bot${token}/logOut`, { method: 'POST' }, true);
      this.store.put('migrated', bot, true);
    }
  }
  async installWebhook(bot: string) {
    await this.migrateBot(bot);
    await this.api.tg(this.token(bot), 'setWebhook', { url: `${this.c.callback}/webhooks/telegram/${bot}`, secret_token: this.webhookSecret(bot), allowed_updates: updates, drop_pending_updates: false });
    await this.control.install(bot);
  }
}
