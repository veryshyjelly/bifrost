import type { Bridge } from './bridge.ts';
import type { Json } from './config.ts';
import { digest } from './config.ts';
import { ApiError } from './api.ts';

const emojiKey = (value: unknown) => typeof value === 'string' ? value.replace(/\uFE0F/g, '') : '';
// Approximate only clear counterparts missing from Telegram's standard reaction set.
// Keep the source emoji in WhatsApp state/echo tracking and in fallback notices.
const telegramEquivalents: Record<string, string> = {
  '😂': '🤣', '😀': '😁', '😃': '😁', '😄': '😁', '😆': '😁',
  '😠': '😡', '😔': '😢', '😞': '😢', '😥': '😢',
  '😙': '😘', '😚': '😘', '🥳': '🎉',
};

export class NativeReactions {
  b: Bridge;
  constructor(b: Bridge) { this.b = b; }
  sourceKey(chat: string, message: string) { return digest(JSON.stringify([this.b.c.session, chat, message])); }
  async rememberOutgoing(map: Json, reaction: Json, bot: string) {
    const sessionKey = digest(this.b.c.session);
    let self = this.b.store.get('reaction-self', sessionKey);
    if (!self) {
      try {
        const session = await this.b.api.ow(''), phone = String(session.phone || '').replace(/\D/g, '');
        if (phone) { self = { sender: phone + '@c.us' }; this.b.store.put('reaction-self', sessionKey, self); }
      } catch { /* The reaction already reached WhatsApp; identity lookup must not replay it. */ }
    }
    if (self) this.b.store.put('reaction-echo', this.sourceKey(map.wa_chat, map.wa_id), { sender: self.sender, bot,
      chat: String(reaction.chat.id), message: reaction.message_id, emoji: emojiKey(reaction.new_reaction?.find((value: Json) => typeof value.emoji === 'string')?.emoji), at: Date.now() });
  }
  async apply(chat: string, message: string, event: Json, rows: Json[], key: string) {
    const sourceKey = this.sourceKey(chat, message), sender = this.b.store.canonical(String(event.senderId || 'unknown'));
    const reaction = emojiKey(event.reaction);
    let state = this.b.store.get('wa-reaction-state', sourceKey) || { senders: {}, revision: 0 };
    if (state.event !== key) {
      const previous = new Map<string, Json>(Object.entries(state.senders));
      const next = event.reactions && typeof event.reactions === 'object' && !Array.isArray(event.reactions)
        ? new Map<string, Json>(Object.entries(event.reactions).filter(([, emoji]) => emojiKey(emoji)).map(([id, value]) => {
          const emoji = emojiKey(value);
          const canonical = this.b.store.canonical(id), old = previous.get(canonical);
          return [canonical, { emoji, order: old?.emoji === emoji ? old.order : ++state.revision, echo: old?.emoji === emoji ? old.echo : undefined }];
        })) : previous;
      const echo = this.b.store.get('reaction-echo', sourceKey);
      const ownSender = echo && (this.b.store.canonical(echo.sender) === sender || this.b.store.db.prepare("SELECT 1 FROM kv WHERE kind='wamessage' AND json_extract(value,'$.fromMe')=1 AND json_extract(value,'$.sender')=? LIMIT 1").get(sender));
      const isEcho = echo && ownSender && echo.emoji === reaction && Date.now() - echo.at < 120000;
      const representedByUser = isEcho ? echo : previous.get(sender)?.emoji === reaction ? previous.get(sender)?.echo : undefined;
      if (reaction) next.set(sender, { emoji: reaction, order: ++state.revision, ...(representedByUser ? { echo: representedByUser } : {}) }); else next.delete(sender);
      state = { ...state, event: key, senders: Object.fromEntries(next) }; this.b.store.put('wa-reaction-state', sourceKey, state);
    }
    const destinations = new Map<string, Json[]>();
    for (const row of rows) {
      const scope = this.b.control.scope(row.bot, row.tg_chat);
      const ledger = this.b.store.db.prepare('SELECT deleted,thread FROM telegram_messages WHERE scope=? AND chat=? AND message=?').get(scope, row.tg_chat, row.tg_id);
      if (ledger?.deleted) continue;
      const destination = `${scope}:${row.tg_chat}:${ledger?.thread || 0}`;
      destinations.set(destination, [...(destinations.get(destination) || []), { ...row, thread: Number(ledger?.thread || 0) }]);
    }
    for (const [destination, copies] of destinations) {
      const primary = copies.find(row => row.media) || copies[0], privateChat = !primary.tg_chat.startsWith('-');
      const cacheKey = sourceKey + ':' + digest(destination), cache = this.b.store.get('tg-reaction-state', cacheKey) || { applied: {}, refused: {} };
      const desired = new Map<string, Json>();
      for (const [reactor, value] of Object.entries(state.senders) as [string, Json][]) {
        if (value.echo?.chat === primary.tg_chat && value.echo.message === primary.tg_id && (!privateChat || value.echo.bot === primary.bot)) continue;
        const bot = privateChat ? primary.bot : await this.b.preferred(reactor, primary.tg_chat);
        if (!desired.has(bot) || desired.get(bot)!.order < value.order) desired.set(bot, value);
      }
      let fallback = false, unavailable = false;
      for (const bot of new Set([...Object.keys(cache.applied), ...Object.keys(cache.refused), ...desired.keys()])) {
        const originalEmoji = desired.get(bot)?.emoji || '', emoji = telegramEquivalents[originalEmoji] || originalEmoji;
        const signature = JSON.stringify([primary.tg_id, emoji]);
        if (cache.applied[bot] === signature) {
          if (!emoji && cache.refused[bot]) { delete cache.refused[bot]; this.b.store.put('tg-reaction-state', cacheKey, cache); }
          continue;
        }
        if (cache.refused[bot] === signature) continue;
        try {
          await this.b.api.tg(this.b.token(bot), 'setMessageReaction', { chat_id: primary.tg_chat, message_id: primary.tg_id,
            reaction: emoji ? [{ type: 'emoji', emoji }] : [] });
          cache.applied[bot] = signature; delete cache.refused[bot];
        } catch (e) {
          if (e instanceof ApiError && [400, 403].includes(e.status)) {
            if (['message_missing', 'thread_missing'].includes(e.reason || '')) {
              cache.refused[bot] = signature; delete cache.pendingFallback; unavailable = true;
              this.b.store.put('tg-reaction-state', cacheKey, cache); break;
            }
            if (cache.applied[bot] && JSON.parse(cache.applied[bot])[1]) {
              try {
                await this.b.api.tg(this.b.token(bot), 'setMessageReaction', { chat_id: primary.tg_chat, message_id: primary.tg_id, reaction: [] });
                cache.applied[bot] = JSON.stringify([primary.tg_id, '']);
              } catch (clear) {
                if (!(clear instanceof ApiError) || ![400, 403].includes(clear.status)) {
                  if (clear instanceof ApiError && (clear.retry || clear.uncertain)) throw new ApiError('Telegram reaction update will retry', clear.status, true, false, clear.delay);
                  throw clear;
                }
              }
            }
            cache.refused[bot] = signature; cache.pendingFallback = key; fallback = true;
          }
          else if (e instanceof ApiError && (e.retry || e.uncertain)) throw new ApiError('Telegram reaction update will retry', e.status, true, false, e.delay);
          else throw e;
        }
        this.b.store.put('tg-reaction-state', cacheKey, cache);
      }
      if (unavailable) continue;
      if (fallback || cache.pendingFallback === key) {
        const name = this.b.store.get('contact', sender)?.name || sender;
        try {
          await this.b.sendText(privateChat ? primary.bot : 'master', primary.tg_chat, `${name} ${event.reaction ? 'reacted ' + event.reaction : 'removed a reaction'} · native reaction unavailable`, key + ':reaction-fallback:' + digest(destination),
            { ...(primary.thread ? { message_thread_id: primary.thread } : {}), reply_parameters: { message_id: primary.tg_id, allow_sending_without_reply: true } });
        } catch (e) { if (!(e instanceof ApiError) || e.status !== 400 || !['message_missing', 'thread_missing', 'reply_missing'].includes(e.reason || '')) throw e; }
        delete cache.pendingFallback; this.b.store.put('tg-reaction-state', cacheKey, cache);
      }
    }
  }
}
