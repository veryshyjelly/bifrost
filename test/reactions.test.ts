import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Bridge } from '../src/bridge.ts';
import { Api, ApiError } from '../src/api.ts';
import { Store } from '../src/store.ts';
import { configuration } from '../src/config.ts';

class Fixture extends Api {
  calls: any[] = []; sequence = 100; members = new Set([42, 43]); unsupported = new Set<string>(); failure?: ApiError; noticeFailure?: ApiError;
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
    if (method === 'getChatMember') return { status: this.members.has(Number(body.user_id)) ? 'member' : 'left' };
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
