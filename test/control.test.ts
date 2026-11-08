import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Bridge } from '../src/bridge.ts';
import { Api, ApiError } from '../src/api.ts';
import { Store } from '../src/store.ts';
import { configuration } from '../src/config.ts';

class Fixture extends Api {
  calls: any[] = []; sequence = 1000; failedIds = new Set<number>(); deleteFailure?: ApiError;
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
    if (method === 'getMe') return { id: token === 'master' ? 99 : 42, username: token === 'master' ? 'hermes_bot' : 'alice_bot' };
    if (method === 'sendMessage') return { message_id: ++this.sequence, date: Math.floor(Date.now() / 1000), from: { id: token === 'master' ? 99 : 42, is_bot: true },
      chat: { id: body.chat_id, type: String(body.chat_id).startsWith('-') ? 'supergroup' : 'private' }, text: body.text,
      ...(body.message_thread_id ? { message_thread_id: body.message_thread_id, is_topic_message: true } : {}) };
    if (method === 'deleteMessages') {
      if (this.deleteFailure) { const error = this.deleteFailure; this.deleteFailure = undefined; throw error; }
      if (body.message_ids.some((id: number) => this.failedIds.has(id))) throw new ApiError('Message cannot be deleted', 400);
    }
    return true;
  }
  async ow(path: string, body?: any) { this.calls.push({ method: path, body }); return { messageId: 'wa-sent' }; }
  async request() { throw new Error('Controls must not call external management endpoints in these fixtures'); }
}
function fixture(t: any) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), masterToken: 'master', owner: '123', management: '-1001', openwaKey: 'key', session: 's1', history: 0, commandTTL: 10, controlTTL: 100 };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  store.put('bot', '42', { token: store.seal('child'), username: 'alice_bot', ready: true });
  store.put('contact', '111@c.us', { name: 'Alice', bot: '42' });
  t.after(() => store.close()); return { bridge, store, api };
}
const message = (id: number, text: string, extra: any = {}) => ({ message_id: id, date: Math.floor(Date.now() / 1000), from: { id: 123 }, chat: { id: -200, type: 'supergroup' }, text, ...extra });
async function drain(b: Bridge) {
  for (let n = 0; n < 300; n++) {
    if (!Number(b.store.db.prepare("SELECT count(*) AS n FROM jobs WHERE state='pending' AND due<=?").get(Date.now())!.n)) return;
    await b.tick();
  }
  throw new Error('Control queue did not drain');
}
const deletedIds = (api: Fixture) => api.calls.filter(c => c.method === 'deleteMessages').flatMap(c => c.body.message_ids);

test('Hermes ignores commands for another bot, while the addressed member bot handles its own group Start', async t => {
  const { bridge, api, store } = fixture(t);
  const m = message(1, '/start@ALICE_bot');
  await bridge.telegram('master', { message: m }, 'foreign');
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, 0);
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='control-expire'").get()!.n, 0);
  assert.equal(bridge.acceptTelegram('42', { update_id: 1, message: m }), true);
  await bridge.tick();
  assert.ok(api.calls.some(c => c.method === 'sendMessage' && c.token === 'child' && c.body.text.includes('privately')));
  assert.equal(bridge.acceptTelegram('42', { update_id: 2, message: message(2, '/status') }), false);
  assert.ok(!api.calls.some(c => c.method.startsWith('messages/')));
});

test('join, leave, title and forum service events produce no unsupported-message replies or WhatsApp operations', async t => {
  const { bridge, api } = fixture(t);
  for (const [index, fields] of [{ new_chat_members: [{ id: 42 }] }, { left_chat_member: { id: 42 } }, { forum_topic_edited: { name: 'New title' } }, { new_chat_title: 'Title' }].entries()) {
    const m = message(index + 1, '', fields); delete (m as any).text;
    bridge.acceptTelegram('master', { update_id: index + 1, message: m }); await bridge.tick();
  }
  assert.deepEqual(api.calls, []);
});

test('edited commands and edits of former commands never become WhatsApp messages', async t => {
  const { bridge, api } = fixture(t);
  bridge.acceptTelegram('master', { update_id: 1, message: message(1, '/status') }); await bridge.tick();
  bridge.acceptTelegram('master', { update_id: 2, edited_message: message(1, 'Changed my command') }); await bridge.tick();
  for (const text of ['/read', '/sync', '/start@alice_bot']) await bridge.telegram('master', { edited_message: message(2, text) }, 'edit:' + text);
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, 1);
  assert.ok(!api.calls.some(c => c.method.startsWith('messages/') || c.method === 'chats/read'));
});

test('commands and notices expire durably, while WhatsApp text and poll relays remain ordinary chat messages', async t => {
  const { bridge, store, api } = fixture(t);
  await bridge.command('master', message(1, '/status'), 'status');
  for (const data of [{ type: 'text', body: 'Hello' }, { type: 'poll', poll: { name: 'Choose', options: ['A', 'B'] } }]) await bridge.whatsApp({
    sessionId: 's1', event: 'message.received', data: { id: data.type, chatId: 'g@g.us', from: '333@c.us', author: '333@c.us', isGroup: true, timestamp: Math.floor(Date.now() / 1000), ...data },
  }, 'wa:' + data.type);
  const expires = store.db.prepare("SELECT payload,due FROM jobs WHERE type='control-expire'").all();
  assert.equal(expires.length, 2);
  assert.ok(expires.every(r => Number(r.due) > Date.now()));
  const chats = store.db.prepare("SELECT message,expires FROM telegram_messages WHERE kind='chat'").all();
  assert.equal(chats.length, 2); assert.ok(chats.every(r => r.expires === 0));
  store.db.exec("UPDATE jobs SET due=0 WHERE type='control-expire'"); await drain(bridge);
  assert.ok(deletedIds(api).includes(1));
  assert.ok(chats.every(r => !deletedIds(api).includes(Number(r.message))));
  const restarted = new Bridge(bridge.c, store, api);
  assert.equal(restarted.store.db.prepare("SELECT count(*) AS n FROM telegram_messages WHERE deleted=1").get()!.n, 2);
});

test('interactive command replies get a longer lifetime', async t => {
  const { bridge, store } = fixture(t);
  await bridge.sendText('master', '-200', 'Create a bot', 'button', { reply_markup: { inline_keyboard: [[{ text: 'Create', url: 'https://t.me/example_bot' }]] } });
  const row = store.db.prepare('SELECT date,expires FROM telegram_messages').get()!;
  assert.ok(Number(row.expires) - Number(row.date) >= 99000);
});

test('observing a member-bot notice through Hermes keeps its expiration and owner-only execution checks', async t => {
  const { bridge, store, api } = fixture(t);
  const [receipt] = await bridge.sendText('42', '-200', 'Member bot notice', 'member-notice');
  assert.equal(bridge.acceptTelegram('master', { update_id: 1, message: receipt }), false);
  const row = store.db.prepare('SELECT kind,expires FROM telegram_messages WHERE message=?').get(receipt.message_id)!;
  assert.equal(row.kind, 'log'); assert.ok(Number(row.expires) > Date.now());
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, 1);
  assert.ok(!api.calls.some(c => c.method.startsWith('messages/')));
});

test('/clear log removes local notices and command messages, preserving chat messages, bindings and mappings', async t => {
  const { bridge, store, api } = fixture(t);
  bridge.control.track('master', message(10, 'Old notice'), 'log');
  bridge.control.track('master', message(11, 'Actual chat'), 'chat');
  store.map('g@g.us', 'original', '-200', 11, 'master', 'in');
  bridge.control.track('master', message(12, 'Other topic', { chat: { id: -1001, type: 'supergroup' }, message_thread_id: 7, is_topic_message: true }), 'log');
  await bridge.command('master', message(50, '/clear log'), 'clear-log'); await drain(bridge);
  assert.ok(deletedIds(api).includes(10)); assert.ok(deletedIds(api).includes(50));
  assert.ok(!deletedIds(api).includes(11)); assert.ok(!deletedIds(api).includes(12));
  assert.ok(store.byTelegram('-200', 11)); assert.equal(store.get('contact', '111@c.us').bot, '42');
  assert.equal(store.all('control-clear')[0].status, 'completed');
  assert.ok(!api.calls.some(c => c.method.startsWith('messages/') || c.method === 'chats/read'));
});

test('/clear all is exclusive to management General and preserves other topics and old/protected messages', async t => {
  const { bridge, store, api } = fixture(t);
  await bridge.command('master', message(1, '/clear all'), 'mirror-all');
  const forum = { id: -1001, type: 'supergroup', is_forum: true };
  await bridge.command('master', message(2, '/clear_all', { chat: forum, is_topic_message: true, message_thread_id: 7 }), 'topic-all');
  assert.equal(store.all('control-clear').length, 0);
  bridge.control.track('master', message(10, 'General chat', { chat: forum }), 'chat');
  bridge.control.track('master', message(11, 'Another topic', { chat: forum, message_thread_id: 7, is_topic_message: true }), 'chat');
  bridge.control.track('master', message(12, 'Old', { chat: forum, date: Math.floor(Date.now() / 1000) - 49 * 3600 }), 'log');
  bridge.control.track('master', message(13, '', { chat: forum }), 'protected');
  await bridge.command('master', message(50, '/clear all', { chat: forum, message_thread_id: 10 }), 'general-all'); await drain(bridge);
  assert.ok(deletedIds(api).includes(10)); assert.ok(deletedIds(api).includes(50));
  assert.ok(!deletedIds(api).includes(11)); assert.ok(!deletedIds(api).includes(12)); assert.ok(!deletedIds(api).includes(13));
  const run = store.all('control-clear')[0]; assert.equal(run.thread, 0); assert.equal(run.skipped, 2); assert.equal(run.status, 'completed');
});

test('cleanup isolates private message IDs for each member bot', async t => {
  const { bridge, store, api } = fixture(t);
  const dm = message(10, 'Notice', { chat: { id: 123, type: 'private' } });
  bridge.control.track('master', dm, 'log'); bridge.control.track('42', dm, 'log');
  await bridge.command('42', message(11, '/clear_log', { chat: dm.chat }), 'private-clear'); await drain(bridge);
  assert.equal(store.db.prepare("SELECT deleted FROM telegram_messages WHERE scope='master' AND message=10").get()!.deleted, 0);
  assert.equal(store.db.prepare("SELECT deleted FROM telegram_messages WHERE scope='42' AND message=10").get()!.deleted, 1);
  assert.ok(api.calls.filter(c => c.method === 'deleteMessages').every(c => c.token === 'child'));
});

test('one undeletable message does not prevent removal of other notices', async t => {
  const { bridge, store, api } = fixture(t); api.failedIds.add(11);
  for (const id of [10, 11, 12]) bridge.control.track('master', message(id, 'Notice'), 'log');
  await bridge.command('master', message(50, '/clear log'), 'clear-blocked'); await drain(bridge);
  const run = store.all('control-clear')[0]; assert.equal(run.status, 'completed'); assert.equal(run.blocked, 1);
  assert.equal(store.db.prepare('SELECT deleted FROM telegram_messages WHERE message=10').get()!.deleted, 1);
  assert.equal(store.db.prepare('SELECT deleted FROM telegram_messages WHERE message=11').get()!.deleted, 0);
  assert.equal(store.db.prepare('SELECT deleted FROM telegram_messages WHERE message=12').get()!.deleted, 1);
});

test('ambiguous cleanup failures retry as idempotent deletes without uncertain delivery effects', async t => {
  const { bridge, store, api } = fixture(t);
  await bridge.sendText('master', '-200', 'Temporary', 'expiry-retry');
  store.db.exec("UPDATE jobs SET due=0 WHERE type='control-expire'");
  api.deleteFailure = new ApiError('timeout', 0, false, true); await bridge.tick();
  assert.equal(store.db.prepare("SELECT state FROM jobs WHERE type='control-expire'").get()!.state, 'pending');
  store.db.exec("UPDATE jobs SET due=0 WHERE type='control-expire'"); await drain(bridge);
  assert.equal(store.db.prepare("SELECT state FROM jobs WHERE type='control-expire'").get()!.state, 'done');
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM effects WHERE state='uncertain'").get()!.n, 0);
});

test('startup registers command menus for migrated master/member bots, including General-only clear_all', async t => {
  const { bridge, store, api } = fixture(t);
  store.put('migrated', 'master', true); store.put('migrated', '42', true);
  bridge.control.startup(); await drain(bridge);
  const menus = api.calls.filter(c => c.method === 'setMyCommands');
  assert.ok(menus.some(c => c.token === 'master' && c.body.scope.type === 'default' && c.body.commands.some((x: any) => x.command === 'create')));
  assert.ok(menus.some(c => c.token === 'master' && c.body.scope.chat_id === '-1001' && c.body.commands.some((x: any) => x.command === 'clear_all')));
  assert.ok(menus.some(c => c.token === 'child' && c.body.commands.some((x: any) => x.command === 'sync')));
  assert.ok(menus.filter(c => c.token === 'child').every(c => !c.body.commands.some((x: any) => x.command === 'clear_all')));
  assert.equal(api.calls.filter(c => c.method === 'setChatMenuButton').length, 2);
  bridge.control.startup(); await drain(bridge);
  assert.equal(api.calls.filter(c => c.method === 'setChatMenuButton').length, 4);
});

test('existing recorded chat/history is indexed for cleanup without expiring mirrored conversations', t => {
  const { bridge, store } = fixture(t);
  const receipt = message(10, 'Existing notice');
  store.db.prepare("INSERT INTO effects VALUES(?,'done',?)").run('old-log', JSON.stringify(receipt));
  store.db.prepare("INSERT INTO effects VALUES(?,'done',?)").run('old-relay', JSON.stringify(message(11, 'Chat')));
  store.map('g@g.us', 'original', '-200', 11, 'master', 'in');
  bridge.control.indexHistory(); bridge.control.indexHistory();
  assert.equal(store.db.prepare("SELECT kind FROM telegram_messages WHERE message=10").get()!.kind, 'log');
  assert.equal(store.db.prepare("SELECT kind FROM telegram_messages WHERE message=11").get()!.kind, 'chat');
  assert.equal(store.db.prepare('SELECT count(*) AS n FROM telegram_messages').get()!.n, 2);
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='control-expire'").get()!.n, 0);
});
