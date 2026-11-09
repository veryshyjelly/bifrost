import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Bridge } from '../src/bridge.ts';
import { Api, ApiError } from '../src/api.ts';
import { Store } from '../src/store.ts';
import { configuration } from '../src/config.ts';

class Fixture extends Api {
  calls: any[] = []; sequence = 100; members = new Set([42, 43]); unsupported = new Set<string>(); failure?: ApiError; noticeFailure?: ApiError;
  customEmojis: any[] = [];
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
    if (method === 'getChatMember') return { status: this.members.has(Number(body.user_id)) ? 'member' : 'left' };
    if (method === 'getCustomEmojiStickers') return this.customEmojis.filter(item => body.custom_emoji_ids.includes(item.custom_emoji_id));
    if (method === 'setMessageReaction') {
      if (this.failure) { const e = this.failure; this.failure = undefined; throw e; }
      if (this.unsupported.has(body.reaction[0]?.emoji)) throw new ApiError('Reaction unavailable', 400);
    }
    if (method === 'sendMessage') {
      if (this.noticeFailure) { const e = this.noticeFailure; this.noticeFailure = undefined; throw e; }
      return { message_id: ++this.sequence, chat: { id: body.chat_id }, text: body.text };
    }
    return true;
  }
  async ow(path: string, body?: any) { this.calls.push({ method: path, body }); return path === '' ? { phone: '999' } : { success: true }; }
}
function fixture(t: any) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), masterToken: 'master', owner: '123', management: '-1001', openwaKey: 'key', session: 's1', history: 0 };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  for (const [id, token, name, phone] of [['42', 'alice', 'Alice', '111'], ['43', 'bob', 'Bob', '222']]) {
    store.put('bot', id, { token: store.seal(token) }); store.put('contact', phone + '@c.us', { bot: id, name });
  }
  t.after(() => store.close()); return { bridge, store, api };
}
function copy(f: ReturnType<typeof fixture>, id = 5, options: any = {}) {
  const bot = options.bot || '42', chat = options.chat || '-200';
  f.store.map('g@g.us', 'm1', chat, id, bot, 'in', !!options.media);
  f.bridge.control.track(bot, { message_id: id, chat: { id: chat, type: chat.startsWith('-') ? 'supergroup' : 'private' },
    ...(options.thread ? { message_thread_id: options.thread, is_topic_message: true } : {}) }, 'chat');
}
const react = (b: Bridge, sender: string, emoji: string, key: string, extra = {}) => b.waChange({ event: 'message.reaction', data: { chatId: 'g@g.us', messageId: 'm1', senderId: sender, reaction: emoji, ...extra } }, key);
const native = (api: Fixture) => api.calls.filter(c => c.method === 'setMessageReaction');

test('reactions to missing messages and notices targeting deleted topics do not block later deliveries', async t => {
  const f = fixture(t); copy(f);
  f.api.failure = Object.assign(new ApiError('Missing', 400), { reason: 'message_missing' as const });
  await react(f.bridge, '222@c.us', '👍', 'missing-message');
  assert.ok(!f.api.calls.some(c => c.method === 'sendMessage'));
  f.api.unsupported.add('🚀');
  f.api.noticeFailure = Object.assign(new ApiError('Missing topic', 400), { reason: 'thread_missing' as const });
  await react(f.bridge, '222@c.us', '🚀', 'missing-topic');
  await react(f.bridge, '222@c.us', '🚀', 'missing-topic');
  assert.equal(f.api.calls.filter(c => c.method === 'sendMessage').length, 1);
});

test('WhatsApp reactions appear natively as the reactor’s available member bot without a reply notice', async t => {
  const f = fixture(t); copy(f); await react(f.bridge, '222@c.us', '👍', 'add');
  assert.equal(native(f.api).length, 1); assert.equal(native(f.api)[0].token, 'bob');
  assert.deepEqual(native(f.api)[0].body, { chat_id: '-200', message_id: 5, reaction: [{ type: 'emoji', emoji: '👍' }] });
  assert.ok(!f.api.calls.some(c => c.method === 'sendMessage' || c.method.startsWith('messages/')));
});

test('removing one member’s reaction clears only that bot’s reaction', async t => {
  const f = fixture(t); copy(f);
  await react(f.bridge, '222@c.us', '👍', 'bob-add'); await react(f.bridge, '111@c.us', '❤️', 'alice-add'); await react(f.bridge, '222@c.us', '', 'bob-remove');
  assert.deepEqual(native(f.api).map(c => [c.token, c.body.reaction]), [['bob', [{ type: 'emoji', emoji: '👍' }]], ['alice', [{ type: 'emoji', emoji: '❤' }]], ['bob', []]]);
});

test('Hermes shows the latest unbound reaction and preserves remaining reactors when one removes theirs', async t => {
  const f = fixture(t); copy(f);
  await react(f.bridge, '333@c.us', '👍', 'first'); await react(f.bridge, '444@c.us', '❤️', 'second');
  await react(f.bridge, '333@c.us', '', 'remove-first'); assert.equal(native(f.api).length, 2);
  await react(f.bridge, '444@c.us', '', 'remove-last');
  assert.deepEqual(native(f.api).map(c => [c.token, c.body.reaction]), [['master', [{ type: 'emoji', emoji: '👍' }]], ['master', [{ type: 'emoji', emoji: '❤' }]], ['master', []]]);
});

test('full OpenWA reaction snapshots and LID aliases reconstruct every actor and remove missing reactions', async t => {
  const f = fixture(t); copy(f); f.store.alias('777@lid', '222@c.us');
  await react(f.bridge, '777@lid', '👍', 'snapshot', { reactions: { '777@lid': '👍', '111@c.us': '❤️' } });
  assert.equal(native(f.api).length, 2);
  await react(f.bridge, '777@lid', '', 'snapshot-remove', { reactions: { '111@c.us': '❤️' } });
  assert.equal(native(f.api).length, 3); assert.deepEqual(native(f.api)[2].body.reaction, []); assert.equal(native(f.api)[2].token, 'bob');
});

test('private message IDs remain scoped to their original bot, regardless of who reacted in WhatsApp', async t => {
  const f = fixture(t); copy(f, 5, { chat: '123' }); copy(f, 5, { chat: '123', bot: 'master' });
  await react(f.bridge, '222@c.us', '👍', 'private');
  assert.deepEqual(native(f.api).map(c => c.token).sort(), ['alice', 'master']);
  assert.ok(!f.api.calls.some(c => c.method === 'getChatMember'));
});

test('split messages react on the attachment once and skip Telegram copies already cleared', async t => {
  const f = fixture(t); copy(f, 4); copy(f, 5, { media: true });
  await react(f.bridge, '222@c.us', '👍', 'media');
  assert.equal(native(f.api).length, 1); assert.equal(native(f.api)[0].body.message_id, 5);
  f.store.db.exec('UPDATE telegram_messages SET deleted=1'); await react(f.bridge, '222@c.us', '❤️', 'gone');
  assert.equal(native(f.api).length, 1);
});

test('a member bot absent from the group falls back to Hermes', async t => {
  const f = fixture(t); copy(f); f.api.members.delete(43); await react(f.bridge, '222@c.us', '👍', 'absent');
  assert.equal(native(f.api)[0].token, 'master');
});

test('unavailable emoji clears a stale native reaction and uses one attributed fallback in the same topic', async t => {
  const f = fixture(t); copy(f, 5, { chat: '-1001', thread: 7 });
  await react(f.bridge, '222@c.us', '👍', 'allowed'); f.api.unsupported.add('unavailable');
  await react(f.bridge, '222@c.us', 'unavailable', 'unsupported');
  assert.deepEqual(native(f.api).at(-1)!.body.reaction, []);
  const notice = f.api.calls.find(c => c.method === 'sendMessage');
  assert.equal(notice.body.message_thread_id, 7); assert.match(notice.body.text, /^Bob reacted unavailable/);
  await react(f.bridge, '222@c.us', 'unavailable', 'duplicate'); assert.equal(f.api.calls.filter(c => c.method === 'sendMessage').length, 1);
  await react(f.bridge, '222@c.us', '', 'clear'); f.api.unsupported.delete('unavailable');
  await react(f.bridge, '222@c.us', 'unavailable', 'now-allowed'); assert.equal(native(f.api).at(-1)!.body.reaction[0].emoji, 'unavailable');
});

test('a rate-limited fallback notice is not lost when the original reaction event retries', async t => {
  const f = fixture(t); copy(f); f.api.unsupported.add('unavailable'); f.api.noticeFailure = new ApiError('Rate limited', 429, true);
  await assert.rejects(react(f.bridge, '222@c.us', 'unavailable', 'refused-notice'));
  await react(f.bridge, '222@c.us', 'unavailable', 'refused-notice');
  assert.equal(f.api.calls.filter(c => c.method === 'sendMessage').length, 2); assert.equal(native(f.api).length, 1);
});

test('ambiguous native updates safely retry without uncertain outgoing-send effects', async t => {
  const f = fixture(t); copy(f); f.api.failure = new ApiError('timeout', 0, false, true);
  f.bridge.acceptWhatsApp({ sessionId: 's1', idempotencyKey: 'r1', event: 'message.reaction', data: { chatId: 'g@g.us', messageId: 'm1', senderId: '222@c.us', reaction: '👍' } });
  await f.bridge.tick(); assert.equal(f.store.db.prepare("SELECT state FROM jobs WHERE type='wa'").get()!.state, 'pending');
  f.store.db.exec("UPDATE jobs SET due=0 WHERE type='wa'"); await f.bridge.tick();
  assert.equal(f.store.db.prepare("SELECT state FROM jobs WHERE type='wa'").get()!.state, 'done');
  assert.equal(f.store.db.prepare("SELECT count(*) AS n FROM effects WHERE state='uncertain'").get()!.n, 0);
});

test('Telegram-owner reactions echoing from WhatsApp do not add duplicate bot reactions or loop back', async t => {
  const f = fixture(t); copy(f);
  const update = { update_id: 1, message_reaction: { chat: { id: -200 }, message_id: 5, user: { id: 123 }, new_reaction: [{ type: 'emoji', emoji: '👍' }] } };
  assert.equal(f.bridge.acceptTelegram('master', update), true); await f.bridge.tick();
  await react(f.bridge, '999@c.us', '👍', 'own-echo'); assert.equal(native(f.api).length, 0);
  await react(f.bridge, '333@c.us', '👍', 'another-user'); assert.equal(native(f.api).length, 1);
  assert.equal(f.bridge.acceptTelegram('master', { ...update, update_id: 2, message_reaction: { ...update.message_reaction, user: { id: 99, is_bot: true } } }), false);
  assert.equal(f.api.calls.filter(c => c.method === 'messages/react').length, 1);
});

test('later repeats of a reaction already represented by the Telegram owner remain suppressed', async t => {
  const f = fixture(t); copy(f);
  const map = f.store.byTelegram('-200', 5)!;
  await f.bridge.reactions.rememberOutgoing(map, { chat: { id: -200 }, message_id: 5, new_reaction: [{ type: 'emoji', emoji: '👍' }] }, 'master');
  await react(f.bridge, '999@c.us', '👍', 'echo');
  const key = f.bridge.reactions.sourceKey('g@g.us', 'm1'), echo = f.store.get('reaction-echo', key);
  f.store.put('reaction-echo', key, { ...echo, at: Date.now() - 180000 });
  await react(f.bridge, '999@c.us', '👍', 'late-repeat'); assert.equal(native(f.api).length, 0);
  await react(f.bridge, '999@c.us', '❤️', 'phone-changed'); assert.equal(native(f.api).length, 1);
});

test('WhatsApp heart variations normalize to Telegram emoji and do not create a second reaction', async t => {
  const f = fixture(t); copy(f);
  await react(f.bridge, '222@c.us', '❤️', 'emoji-style');
  await react(f.bridge, '222@c.us', '❤', 'plain-style');
  assert.equal(native(f.api).length, 1); assert.equal(native(f.api)[0].body.reaction[0].emoji, '❤');
});

test('unsupported Telegram custom reactions do not clear an existing WhatsApp reaction', async t => {
  const f = fixture(t); copy(f);
  await f.bridge.telegram('master', { message_reaction: { chat: { id: -200 }, message_id: 5, new_reaction: [{ type: 'custom_emoji', custom_emoji_id: 'custom' }] } }, 'custom');
  assert.ok(!f.api.calls.some(c => c.method === 'messages/react'));
});

test('Hermes maps laughing reactions on owner messages, deduplicates equivalent changes and clears on removal', async t => {
  const f = fixture(t); copy(f); f.store.db.exec("UPDATE messages SET direction='out'");
  await react(f.bridge, '333@c.us', '😂', 'laugh');
  assert.equal(native(f.api)[0].token, 'master'); assert.equal(native(f.api)[0].body.reaction[0].emoji, '🤣');
  assert.equal(f.store.get('wa-reaction-state', f.bridge.reactions.sourceKey('g@g.us', 'm1')).senders['333@c.us'].emoji, '😂');
  await react(f.bridge, '333@c.us', '🤣', 'same-native'); assert.equal(native(f.api).length, 1);
  await react(f.bridge, '333@c.us', '', 'removed'); assert.deepEqual(native(f.api).at(-1)!.body.reaction, []);
  assert.ok(!f.api.calls.some(c => c.method === 'sendMessage' || c.method.startsWith('messages/')));
});

test('equivalent reactions work through member bots and private copies without altering supported emoji', async t => {
  const f = fixture(t); copy(f); copy(f, 5, { chat: '123' });
  for (const [index, [source, target]] of [['😄', '😁'], ['😠', '😡'], ['😞', '😢'], ['😚', '😘'], ['🥳', '🎉'], ['❤', '❤']].entries()) {
    await react(f.bridge, '222@c.us', source, 'reaction-' + index);
    assert.deepEqual(native(f.api).slice(-2).map(call => [call.token, call.body.reaction[0].emoji]), [['bob', target], ['alice', target]]);
  }
});

test('a refused equivalent and an unsupported unmapped emoji both keep the original in text fallbacks', async t => {
  const f = fixture(t); copy(f); f.api.unsupported.add('🤣'); f.api.unsupported.add('💙');
  await react(f.bridge, '333@c.us', '😂', 'refused-map');
  const notice = f.api.calls.find(c => c.method === 'sendMessage'); assert.ok(notice.body.text.includes('reacted 😂')); assert.ok(!notice.body.text.includes('🤣'));
  await react(f.bridge, '333@c.us', '😂', 'repeat'); assert.equal(f.api.calls.filter(c => c.method === 'sendMessage').length, 1);
  await react(f.bridge, '333@c.us', '💙', 'unmapped'); assert.equal(native(f.api).at(-1)!.body.reaction[0].emoji, '💙');
  assert.ok(f.api.calls.filter(c => c.method === 'sendMessage').at(-1)!.body.text.includes('reacted 💙'));
});

test('Telegram-owner reaction echoes stay suppressed after adding equivalent rendering', async t => {
  const f = fixture(t); copy(f);
  const map = f.store.byTelegram('-200', 5)!;
  await f.bridge.reactions.rememberOutgoing(map, { chat: { id: -200 }, message_id: 5, new_reaction: [{ type: 'emoji', emoji: '🤣' }] }, 'master');
  await react(f.bridge, '999@c.us', '🤣', 'owner-echo'); assert.equal(native(f.api).length, 0);
  await react(f.bridge, '333@c.us', '😂', 'other-person'); assert.equal(native(f.api).length, 1);
  assert.equal(native(f.api)[0].body.reaction[0].emoji, '🤣');
});

test('Telegram custom reactions forward their Unicode equivalent and cache it without converting it to Telegram approximations', async t => {
  const f = fixture(t); copy(f); f.api.customEmojis = [{ custom_emoji_id: 'custom-laugh', emoji: '😆' }];
  const update = { update_id: 1, message_reaction: { chat: { id: -200, type: 'supergroup' }, message_id: 5, user: { id: 123 }, old_reaction: [], new_reaction: [{ type: 'custom_emoji', custom_emoji_id: 'custom-laugh' }] } };
  assert.equal(f.bridge.acceptTelegram('master', update), true); await f.bridge.tick();
  assert.equal(f.api.calls.find(c => c.method === 'messages/react').body.emoji, '😆');
  await f.bridge.telegram('master', { message_reaction: { ...update.message_reaction, message_id: 5 } }, 'second');
  assert.equal(f.api.calls.filter(c => c.method === 'getCustomEmojiStickers').length, 1);
  await react(f.bridge, '999@c.us', '😆', 'own-custom-echo'); assert.equal(native(f.api).length, 0);
});

test('member DM reactions are admitted with bot-scoped mappings while group duplicates are ignored', async t => {
  const f = fixture(t); copy(f, 5, { chat: '123', bot: '42' }); copy(f, 5, { chat: '123', bot: '43' });
  const update = { update_id: 1, message_reaction: { chat: { id: 123, type: 'private' }, message_id: 5, user: { id: 123 }, new_reaction: [{ type: 'emoji', emoji: '👍' }] } };
  assert.equal(f.bridge.acceptTelegram('42', update), true); await f.bridge.tick();
  assert.equal(f.api.calls.filter(c => c.method === 'messages/react').length, 1);
  assert.equal(f.bridge.acceptTelegram('42', { ...update, update_id: 2, message_reaction: { ...update.message_reaction, chat: { id: -200, type: 'supergroup' } } }), false);
  assert.equal(f.bridge.acceptTelegram('42', { ...update, update_id: 3, message_reaction: { ...update.message_reaction, user: { id: 999 } } }), false);
});

test('adding a custom reaction beside an existing reaction forwards the new one; removal keeps the remaining one', async t => {
  const f = fixture(t); copy(f); f.api.customEmojis = [{ custom_emoji_id: 'pointer', emoji: '👉' }];
  const standard = { type: 'emoji', emoji: '👍' }, custom = { type: 'custom_emoji', custom_emoji_id: 'pointer' };
  const base = { chat: { id: -200 }, message_id: 5, user: { id: 123 } };
  await f.bridge.telegram('master', { message_reaction: { ...base, old_reaction: [standard], new_reaction: [standard, custom] } }, 'added');
  assert.equal(f.api.calls.filter(c => c.method === 'messages/react').at(-1)!.body.emoji, '👉');
  await f.bridge.telegram('master', { message_reaction: { ...base, old_reaction: [standard, custom], new_reaction: [standard] } }, 'remaining');
  assert.equal(f.api.calls.filter(c => c.method === 'messages/react').at(-1)!.body.emoji, '👍');
  await f.bridge.telegram('master', { message_reaction: { ...base, old_reaction: [standard], new_reaction: [] } }, 'removed');
  assert.equal(f.api.calls.filter(c => c.method === 'messages/react').at(-1)!.body.emoji, '');
});

test('unresolvable custom reactions produce a temporary independent notice without clearing WhatsApp or blocking a chat', async t => {
  const f = fixture(t); copy(f);
  await f.bridge.telegram('master', { message_reaction: { chat: { id: -200 }, message_id: 5, new_reaction: [{ type: 'custom_emoji', custom_emoji_id: 'unknown' }] } }, 'unknown');
  assert.equal(f.api.calls.filter(c => c.method === 'messages/react').length, 0);
  const job = f.store.db.prepare("SELECT * FROM jobs WHERE type='control-reaction-notice'").get()!;
  assert.ok(job); await f.bridge.control.process({ ...job, payload: JSON.parse(job.payload as string) });
  assert.ok(f.api.calls.find(c => c.method === 'sendMessage').body.text.includes('no standard emoji equivalent'));
});

test('a member unable to access another bot’s message falls back to Hermes and cached refusals do not suppress that fallback', async t => {
  const f = fixture(t); copy(f);
  const original = f.api.tg.bind(f.api);
  f.api.tg = async (token, method, body = {}) => {
    if (token === 'bob' && method === 'setMessageReaction') { f.api.calls.push({ token, method, body }); throw Object.assign(new ApiError('Missing for bot', 400), { reason: 'message_missing' as const }); }
    return original(token, method, body);
  };
  await react(f.bridge, '222@c.us', '👍', 'fallback');
  assert.deepEqual(native(f.api).map(call => call.token), ['bob', 'master']);
  await react(f.bridge, '222@c.us', '👍', 'same'); assert.equal(native(f.api).length, 2);
  await react(f.bridge, '222@c.us', '', 'remove-fallback');
  assert.equal(native(f.api).at(-1)!.token, 'master'); assert.deepEqual(native(f.api).at(-1)!.body.reaction, []);
  assert.ok(!f.api.calls.some(c => c.method === 'sendMessage'));
});

test('old definitive reaction refusals expire so corrected permissions can recover', async t => {
  const f = fixture(t); copy(f); f.api.unsupported.add('👍');
  await react(f.bridge, '333@c.us', '👍', 'refused');
  f.api.unsupported.clear(); await react(f.bridge, '333@c.us', '👍', 'still-cached'); assert.equal(native(f.api).length, 1);
  const cache = f.store.all('tg-reaction-state')[0];
  f.store.put('tg-reaction-state', cache.id, { ...cache, refusedAt: { master: Date.now() - 60001 } });
  await react(f.bridge, '333@c.us', '👍', 'recovered');
  assert.equal(native(f.api).length, 2); assert.equal(f.store.all('tg-reaction-state')[0].applied.master, JSON.stringify([5, '👍']));
});

test('custom-emoji resolution and outbound payload stay frozen across a known send retry', async t => {
  const f = fixture(t); copy(f); f.api.customEmojis = [{ custom_emoji_id: 'custom', emoji: '😆' }];
  const original = f.api.ow.bind(f.api); let first = true;
  f.api.ow = async (path, body) => { if (path === 'messages/react' && first) { first = false; throw new ApiError('Rate limited', 429, true); } return original(path, body); };
  const update = { message_reaction: { chat: { id: -200 }, message_id: 5, new_reaction: [{ type: 'custom_emoji', custom_emoji_id: 'custom' }] } };
  await assert.rejects(f.bridge.telegram('master', update, 'retry-custom'));
  f.api.customEmojis = [{ custom_emoji_id: 'custom', emoji: '👍' }]; await f.bridge.telegram('master', update, 'retry-custom');
  assert.equal(f.api.calls.find(c => c.method === 'messages/react').body.emoji, '😆');
});
