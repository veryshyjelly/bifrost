import type { Bridge } from './bridge.ts';
import { digest } from './config.ts';
import type { Json } from './config.ts';
import { ApiError } from './api.ts';
import { concat, plain, styled, whatsappURL } from './format.ts';
import type { Formatted } from './format.ts';

export class MentionError extends Error {}
const cleanName = (name: string) => Array.from(String(name).replace(/[{}|\r\n\t\x00-\x1f]/g, ' ').trim()).slice(0, 48).join('');
const local = (id: string) => id.split('@')[0].split(':')[0];
const person = (id: unknown): id is string => typeof id === 'string' && /^\d+(?::\d+)?@(?:c\.us|s\.whatsapp\.net|lid)$/.test(id);

export class Mentions {
  constructor(public b: Bridge) {}
  get session() { return digest(this.b.c.session); }
  canonical(id: string) { return this.b.store.canonical(id.replace(/:\d+@/, '@')); }
  cached(chat: string) { return this.b.store.get('sync-group', `${this.session}:${chat}`); }
  contact(id: string) { return this.b.store.all('contact').find(c => this.canonical(c.id) === this.canonical(id)); }
  name(id: string, chat: string) {
    return cleanName(this.contact(id)?.name || this.cached(chat)?.members.find((m: Json) => this.canonical(m.id) === this.canonical(id))?.name || local(id));
  }
  async roster(chat: string, allowCached = false) {
    const old = this.cached(chat);
    if (old && Date.now() - old.fetchedAt < 300000) return { ...old, cached: false };
    try {
      const info = await this.b.api.ow('groups/' + encodeURIComponent(chat));
      if (!Array.isArray(info.participants) || typeof info.name !== 'string' || !info.name.trim() || info.participants.some((m: Json) => !person(m.id))) throw new ApiError('OpenWA group member list is incomplete', 502);
      const members = info.participants.map((m: Json) => ({ id: m.id, canonical: this.canonical(m.id), name: m.name || '', isAdmin: !!m.isAdmin, isSuperAdmin: !!m.isSuperAdmin }));
      const result = { ...old, name: info.name, description: info.description, members,
        rosterHash: digest(JSON.stringify(members.map((m: Json) => `${m.canonical}:${m.isAdmin}:${m.isSuperAdmin}`).sort())), fetchedAt: Date.now() };
      this.b.store.put('sync-group', `${this.session}:${chat}`, result); return { ...result, cached: false };
    } catch {
      if (old && allowCached) return { ...old, cached: true };
      throw new MentionError('Could not refresh the WhatsApp member list. Try /sync, then send your message again.');
    }
  }
  token(chat: string, id: string) {
    id = this.canonical(id);
    const hash = digest(JSON.stringify([this.session, chat, id]));
    let key = hash.slice(0, 8);
    while (this.b.store.get('mention-token', key) && JSON.stringify(this.b.store.get('mention-token', key)) !== JSON.stringify({ session: this.session, chat, person: id })) key = hash.slice(0, key.length + 2);
    this.b.store.put('mention-token', key, { session: this.session, chat, person: id });
    return `@{${this.name(id, chat)}|${key}}`;
  }
  async incoming(body: string, ids: string[] | undefined, chat: string, destination: string, key: string): Promise<Formatted> {
    body = String(body || '');
    const cacheKey = digest(JSON.stringify([this.session, key, body, ids || []]));
    const saved = this.b.store.get('mention-render', cacheKey); if (saved) return saved;
    const targets = new Map<string, Set<string>>();
    for (const id of (Array.isArray(ids) ? ids : []).filter(person)) {
      const canonical = this.canonical(id);
      for (const candidate of [id, canonical, ...this.b.store.db.prepare('SELECT alias FROM aliases WHERE canonical=?').all(canonical).map(row => String(row.alias))]) {
        const n = local(candidate), matches = targets.get(n) || new Set(); matches.add(canonical); targets.set(n, matches);
      }
    }
    let result = plain(''), cursor = 0;
    const rendered = new Map<string, Formatted>();
    for (const match of body.matchAll(/(?<![\p{L}\p{N}_@])@(\d+)(?![\p{L}\p{N}_])/gu)) {
      const people = targets.get(match[1]); if (people?.size !== 1) continue;
      const id = [...people][0];
      let tag = rendered.get(id);
      if (!tag) {
        const contact = this.contact(id), bot = contact?.bot && this.b.store.get('bot', contact.bot), label = '@' + this.name(id, chat);
        let member = false;
        if (bot && destination.startsWith('-')) {
          try { const response = await this.b.api.tg(this.b.token('master'), 'getChatMember', { chat_id: destination, user_id: Number(contact.bot) }); member = ['member', 'administrator', 'creator'].includes(response.status); }
          catch (e) { if (!(e instanceof ApiError) || ![400, 403].includes(e.status)) throw e; }
        }
        tag = member ? styled(label, 'text_mention', { user: { id: Number(contact.bot), is_bot: true, first_name: cleanName(contact.name || label), ...(bot.username ? { username: bot.username } : {}) } })
          : styled(label, 'text_link', { url: bot?.username ? `https://t.me/${bot.username}` : whatsappURL(id) });
        rendered.set(id, tag);
      }
      result = concat(result, body.slice(cursor, match.index), tag); cursor = match.index! + match[0].length;
    }
    result = concat(result, body.slice(cursor));
    this.b.store.put('mention-render', cacheKey, result); return result;
  }
  async outgoing(chat: string, m: Json, key: string) {
    const cacheKey = digest(JSON.stringify([this.session, key]));
    const previous = this.b.store.get('outbound-mentions', cacheKey); if (previous) return previous;
    const text = String(m.text ?? m.caption ?? ''), entities = m.text !== undefined ? m.entities || [] : m.caption_entities || [];
    const spans: { start: number; end: number; id: string }[] = [];
    const excluded = (start: number, end: number) => entities.some((e: Json) => ['code', 'pre', 'url', 'text_link', 'email'].includes(e.type) && e.offset < end && e.offset + e.length > start);
    const forBot = (id: string) => this.b.store.all('contact').find(c => String(c.bot) === id);
    const add = (start: number, end: number, id: string) => { if (!spans.some(s => s.start < end && s.end > start)) spans.push({ start, end, id: this.canonical(id) }); };
    for (const entity of entities) {
      if (entity.type !== 'text_mention' || !entity.user?.id) continue;
      const contact = forBot(String(entity.user.id)); if (contact && this.b.store.get('bot', String(entity.user.id))) add(entity.offset, entity.offset + entity.length, contact.id);
    }
    for (const match of text.matchAll(/@\{[^{}\n]*(?:\}|(?=\n|$))/g)) {
      if (excluded(match.index!, match.index! + match[0].length)) continue;
      const id = /\|([a-f0-9]{8,64})\}$/.exec(match[0])?.[1], token = id && this.b.store.get('mention-token', id);
      if (!token || token.session !== this.session || token.chat !== chat) throw new MentionError('This mention tag is unknown or belongs to another group/session. Use /mention to copy a new tag, then resend.');
      add(match.index!, match.index! + match[0].length, token.person);
    }
    for (const match of text.matchAll(/(?<![\p{L}\p{N}_@/])@([a-zA-Z0-9_]+)(?![\p{L}\p{N}_])/gu)) {
      if (excluded(match.index!, match.index! + match[0].length)) continue;
      const bot = this.b.store.all('bot').find(b => b.username?.toLowerCase() === match[1].toLowerCase()), contact = bot && forBot(bot.id);
      if (contact) add(match.index!, match.index! + match[0].length, contact.id);
    }
    if (spans.length && chat.endsWith('@g.us')) {
      const roster = await this.roster(chat);
      if (spans.some(span => !roster.members.some((member: Json) => this.canonical(member.id) === span.id))) throw new MentionError('A tagged person is no longer in this WhatsApp group. Use /mention to find a current member, then resend.');
    }
    let converted = '', cursor = 0; const mentions = new Set<string>();
    for (const span of spans.sort((a, b) => a.start - b.start)) {
      converted += text.slice(cursor, span.start) + '@' + local(span.id); cursor = span.end; mentions.add(span.id);
    }
    const result = { text: converted + text.slice(cursor), mentions: [...mentions] };
    this.b.store.put('outbound-mentions', cacheKey, result); return result;
  }
  async page(bot: string, m: Json, chatId: string, search: string, page: number, key: string) {
    const chat = String(m.chat.id), { thread } = this.b.telegramContext(bot, m), roster = await this.roster(chatId, true);
    const people = new Map<string, Json>();
    for (const member of roster.members) people.set(this.canonical(member.id), member);
    const rows = [...people].map(([id]) => {
      const contact = this.contact(id), registered = contact?.bot && this.b.store.get('bot', contact.bot);
      return { id, name: this.name(id, chatId), username: registered?.username };
    }).filter(row => `${row.name} ${row.id} ${row.username || ''}`.toLocaleLowerCase().includes(search.toLocaleLowerCase())).sort((a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id));
    const pages = Math.max(1, Math.ceil(rows.length / 10)); page = Math.min(pages, Math.max(1, page));
    let text = plain(`WhatsApp mentions · page ${page}/${pages}${roster.cached ? '\nUsing the cached member list; refresh failed.' : ''}\nCopy a tag and paste it anywhere in your message. Tags are checked again when sending.`);
    const buttons = rows.slice((page - 1) * 10, page * 10).map(row => {
      const token = row.username ? '@' + row.username : this.token(chatId, row.id);
      text = concat(text, '\n', row.name, ' · ', row.id.endsWith('@lid') ? 'ID …' : '…', local(row.id).slice(-6), '\n', styled(token, 'code'));
      return [{ text: 'Copy tag · ' + row.name, copy_text: { text: token } }];
    });
    if (!rows.length) text = concat(text, '\nNo matching group members. Try /mention with a different name.');
    const navigation = [];
    for (const [label, next] of [['Previous', page - 1], ['Next', page + 1]] as const) if (next >= 1 && next <= pages) navigation.push({ text: label, callback_data: 'mention:' + this.b.action({ owner: this.b.c.owner, session: this.session, chat, thread: Number(thread || 0), chatId, bot, search, page: next, expires: Date.now() + this.b.c.controlTTL * 1000 }) });
    if (navigation.length) buttons.push(navigation as any);
    await this.b.sendText(bot, chat, text, key, { ...(thread ? { message_thread_id: thread } : {}), reply_markup: { inline_keyboard: buttons } });
  }
  async command(bot: string, m: Json, args: string[], key: string) {
    const { route, thread } = this.b.telegramContext(bot, m), extra = thread ? { message_thread_id: thread } : {};
    if (String(m.from?.id) !== this.b.c.owner) return;
    if (!route?.chatId.endsWith('@g.us') || ['muted', 'ignored'].includes(route.mode)) { await this.b.sendText(bot, String(m.chat.id), 'Use /mention [name or number] in an active WhatsApp group’s management topic or Telegram mirror group.', key, extra); return; }
    try { await this.page(bot, m, route.chatId, args.join(' ').slice(0, 128), 1, key); }
    catch (e) { if (!(e instanceof MentionError)) throw e; await this.b.sendText(bot, String(m.chat.id), e.message, key + ':error', extra); }
  }
  async callback(bot: string, q: Json, key: string) {
    if (!q.data?.startsWith('mention:')) return false;
    const action = this.b.store.get('action', q.data.slice(8)), m = q.message, context = m && this.b.telegramContext(bot, m);
    const valid = action && action.owner === this.b.c.owner && String(q.from?.id) === action.owner && action.session === this.session && action.bot === bot
      && action.chat === String(m?.chat.id) && action.thread === Number(context?.thread || 0) && action.chatId === context?.route?.chatId && !['muted', 'ignored'].includes(context.route.mode) && action.expires > Date.now();
    try { await this.b.api.tg(this.b.token(bot), 'answerCallbackQuery', { callback_query_id: q.id, ...(!valid ? { text: 'This picker expired. Use /mention again.' } : {}) }); }
    catch (e) { if (!(e instanceof ApiError) || e.status !== 400) this.b.control.retry(e); }
    if (valid) {
      try { await this.page(bot, m, action.chatId, action.search, action.page, key); }
      catch (e) { if (!(e instanceof MentionError)) throw e; await this.b.sendText(bot, String(m.chat.id), e.message, key + ':error', context.thread ? { message_thread_id: context.thread } : {}); }
    }
    return true;
  }
}
