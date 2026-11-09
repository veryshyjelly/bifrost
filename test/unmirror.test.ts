import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Bridge } from '../src/bridge.ts';
import { Store } from '../src/store.ts';
import { Api, ApiError } from '../src/api.ts';
import { configuration } from '../src/config.ts';

class Fixture extends Api {
  calls: any[] = []; sequence = 100; topicFailure?: ApiError; confirmationFailure?: ApiError;
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
    if (method === 'getMe') return { id: 99, username: 'hermes_bot' };
    if (method === 'createForumTopic') {
      if (this.topicFailure) { const error = this.topicFailure; this.topicFailure = undefined; throw error; }
      return { message_thread_id: ++this.sequence };
    }
    if (method === 'sendMessage' && body.chat_id === '-200' && this.confirmationFailure) {
      const error = this.confirmationFailure; this.confirmationFailure = undefined; throw error;
    }
    return { message_id: ++this.sequence, chat: { id: body.chat_id }, text: body.text };
  }
  async ow(path: string, body?: any) { this.calls.push({ method: path, body }); return { messageId: 'wa-sent' }; }
}
function fixture(t: any) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), owner: '123', management: '-1001', masterToken: 'master', session: 's1' };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  store.put('route', 'group@g.us', { chatId: 'group@g.us', tgChat: '-200', name: 'Friends', mode: 'group', history: false,
    inbox: { chat: '-1001', thread: 7 }, inboxRetired: true });
  t.after(() => store.close()); return { bridge, store, api };
}
const message = (id = 1, text = '/unmirror', extra: any = {}) => ({ message_id: id, from: { id: 123 }, chat: { id: -200, type: 'supergroup' }, text, ...extra });
const event = (extra: any = {}) => ({ type: 'return_to_inbox', data: { chatId: 'group@g.us', tgChat: '-200', previousReturn: '', ...extra } });

test('/unmirror queues a durable move and restores the topic controls without deleting archived messages', async t => {
  const { bridge, store, api } = fixture(t);
  store.map('group@g.us', 'archived', '-200', 20, 'master', 'in');
  const button = await bridge.mirrorButton('group@g.us');
  const picker = store.get('action', new URL(button.url).searchParams.get('start')!.slice('mirror_'.length));
  await bridge.command('master', message(1, '/unmirror', { message_thread_id: 20 }), 'command');
  assert.equal(store.get('route', 'group@g.us').tgChat, '-200');
  const job = store.db.prepare("SELECT payload FROM jobs WHERE type='management'").get()!;
  const move = JSON.parse(job.payload as string); assert.equal(move.type, 'return_to_inbox');
  await bridge.manage(move, 'move');
  const route = store.get('route', 'group@g.us');
  assert.equal(route.tgChat, '-1001'); assert.equal(route.mode, 'inbox');
  assert.ok(route.thread > 0 && route.thread !== 7); assert.equal(route.inbox, undefined); assert.equal(route.inboxRetired, false);
  assert.equal(route.name, 'Friends'); assert.equal(route.history, false);
  assert.equal(store.byWhatsApp('group@g.us', 'archived')[0].tg_chat, '-200');
  assert.equal(bridge.validMirrorRequest(picker), false);
  const header = api.calls.find(c => c.method === 'sendMessage' && c.body.chat_id === '-1001');
  assert.equal(header.body.message_thread_id, route.thread);
  assert.ok(header.body.reply_markup.inline_keyboard.flat().some((b: any) => b.text === 'Create / link Telegram group'));
  const notice = api.calls.find(c => c.method === 'sendMessage' && c.body.chat_id === '-200');
  assert.match(notice.body.reply_markup.inline_keyboard[0][0].url, new RegExp('/' + route.thread + '$'));
  assert.equal(notice.body.message_thread_id, undefined);
  await bridge.retireInbox('group@g.us');
  assert.ok(!api.calls.some(c => ['deleteForumTopic', 'deleteMessages', 'leaveChat'].includes(c.method)));
  assert.ok(!api.calls.some(c => c.method.startsWith('messages/')));
});

test('new WhatsApp messages and Telegram replies use management while the former mirror stops sending', async t => {
  const { bridge, store, api } = fixture(t);
  store.map('group@g.us', 'archived', '-200', 20, 'master', 'in');
  await bridge.manage(event(), 'move');
  const route = store.get('route', 'group@g.us'); api.calls = [];
  await bridge.whatsApp({ sessionId: 's1', event: 'message.received', data: {
    id: 'new', chatId: 'group@g.us', from: '111@c.us', type: 'text', body: 'Hello', timestamp: 1700000000, isGroup: true, contact: { pushName: 'Alice' },
  } }, 'incoming');
  const incoming = api.calls.find(c => c.method === 'sendMessage');
  assert.equal(incoming.body.chat_id, '-1001'); assert.equal(incoming.body.message_thread_id, route.thread);
  await bridge.telegram('master', { message: message(2, 'Reply', { chat: { id: -1001, type: 'supergroup' }, is_topic_message: true,
    message_thread_id: route.thread, reply_to_message: { message_id: store.byWhatsApp('group@g.us', 'new')[0].tg_id } }) }, 'reply');
  assert.equal(api.calls.find(c => c.method === 'messages/reply').body.quotedMessageId, 'new');
  api.calls = [];
  await bridge.telegram('master', { message: message(3, 'Old group text', { reply_to_message: { message_id: 20 } }) }, 'old-reply');
  await bridge.telegram('master', { edited_message: message(3, 'Edited') }, 'old-edit');
  await bridge.telegram('master', { message_reaction: { chat: { id: -200 }, message_id: 20, new_reaction: [{ type: 'emoji', emoji: '👍' }] } }, 'old-reaction');
  assert.equal(api.calls.length, 0);
});

test('notification retries reuse the created topic and leave uncertain deliveries intact', async t => {
  const { bridge, store, api } = fixture(t);
  store.db.prepare("INSERT INTO effects(id,state) VALUES(?,'uncertain')").run('old-delivery');
  store.put('delivery-target', 'old-target', { destination: '-200', bot: 'master' });
  api.confirmationFailure = new ApiError('Rate limited', 429, true);
  await assert.rejects(bridge.manage(event(), 'move'), /Rate limited/);
  const thread = store.get('route', 'group@g.us').thread;
  await bridge.manage(event(), 'move'); await bridge.manage(event(), 'move');
  assert.equal(store.get('route', 'group@g.us').thread, thread);
  assert.equal(api.calls.filter(c => c.method === 'createForumTopic').length, 1);
  assert.equal(api.calls.filter(c => c.method === 'sendMessage' && c.body.chat_id === '-1001').length, 1);
  assert.equal(store.db.prepare('SELECT state FROM effects WHERE id=?').get('old-delivery')!.state, 'uncertain');
  assert.equal(store.get('delivery-target', 'old-target').destination, '-200');
});

test('topic permission errors keep the existing mirror and explain how to retry', async t => {
  const { bridge, store, api } = fixture(t); const old = store.get('route', 'group@g.us');
  api.topicFailure = new ApiError('Forbidden', 403);
  await bridge.manage(event(), 'move');
  assert.deepEqual(store.get('route', 'group@g.us'), old);
  assert.match(api.calls.find(c => c.method === 'sendMessage').body.text, /Manage Topics/);
  assert.equal(store.get('inbox-return', 'move'), undefined);
});

test('uncertain topic creation is preserved for review without replaying the creation', async t => {
  const { bridge, store, api } = fixture(t); api.topicFailure = new ApiError('Timeout', 0, false, true);
  await assert.rejects(bridge.manage(event(), 'move'));
  await assert.rejects(bridge.manage(event(), 'move'), /Uncertain/);
  assert.equal(store.get('route', 'group@g.us').tgChat, '-200');
  assert.equal(api.calls.filter(c => c.method === 'createForumTopic').length, 1);
});

test('muted and ignored mirrors retain their setting after returning to management', async t => {
  for (const mode of ['muted', 'ignored']) {
    const { bridge, store, api } = fixture(t);
    store.put('route', 'group@g.us', { ...store.get('route', 'group@g.us'), mode });
    await bridge.manage(event(), 'move');
    assert.equal(store.get('route', 'group@g.us').mode, mode);
    assert.match(api.calls.find(c => c.method === 'sendMessage' && c.body.chat_id === '-200').body.text, /Use \/keep/);
  }
});

test('an active history import finishes before the mirror can be moved', async t => {
  const { bridge, store, api } = fixture(t);
  bridge.conversationHistory.request('group@g.us', { chat: '-200' }, 50, 'history');
  await bridge.manage(event(), 'move');
  assert.equal(store.get('route', 'group@g.us').tgChat, '-200');
  assert.ok(!api.calls.some(c => c.method === 'createForumTopic'));
  assert.match(api.calls.find(c => c.method === 'sendMessage').body.text, /Wait for it to finish/);
});

test('stale move requests and notification retries cannot undo later routing choices', async t => {
  const { bridge, store, api } = fixture(t);
  store.put('route', 'group@g.us', { ...store.get('route', 'group@g.us'), tgChat: '-300' });
  await bridge.manage(event(), 'stale');
  assert.equal(store.get('route', 'group@g.us').tgChat, '-300');
  assert.ok(!api.calls.some(c => c.method === 'createForumTopic'));
  store.put('route', 'group@g.us', { ...store.get('route', 'group@g.us'), tgChat: '-200' });
  await bridge.manage(event(), 'move');
  store.put('route', 'group@g.us', { ...store.get('route', 'group@g.us'), tgChat: '-200', thread: undefined, mode: 'group' });
  await bridge.manage(event(), 'move'); await bridge.manage(event(), 'old-request');
  assert.equal(store.get('route', 'group@g.us').tgChat, '-200');
  assert.equal(api.calls.filter(c => c.method === 'createForumTopic').length, 1);
});

test('/unmirror requires the owner, master bot and a linked WhatsApp mirror group', async t => {
  const { bridge, store } = fixture(t);
  store.put('bot', '42', { token: store.seal('child'), username: 'alice_bot' });
  assert.equal(bridge.acceptTelegram('master', { update_id: 1, message: message(1, '/unmirror', { from: { id: 999 } }) }), false);
  await bridge.command('master', message(2, '/unmirror', { chat: { id: 123, type: 'private' } }), 'private');
  await bridge.command('master', message(3, '/unmirror', { chat: { id: -1001, type: 'supergroup' } }), 'management');
  await bridge.command('master', message(4, '/unmirror', { chat: { id: -300, type: 'supergroup' } }), 'unlinked');
  await bridge.command('master', message(5, '/unmirror extra'), 'args');
  await bridge.command('42', message(6, '/unmirror@alice_bot'), 'member');
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='management'").get()!.n, 0);
  assert.equal(store.get('route', 'group@g.us').tgChat, '-200');
});
