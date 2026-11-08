import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Bridge } from '../src/bridge.ts';
import { Store } from '../src/store.ts';
import { Api } from '../src/api.ts';
import { configuration } from '../src/config.ts';

class Fixture extends Api {
  calls: any[] = []; nextId = 10; public = false;
  async tg(_token: string, method: string, body: any = {}) {
    this.calls.push({ method, body });
    if (method === 'getMe') return { id: 99, username: 'hermes_bot' };
    if (method === 'getChat') return { type: 'supergroup', username: this.public ? 'public_group' : undefined };
    if (method === 'getChatMember') return { status: 'administrator' };
    return { message_id: ++this.nextId, chat: { id: body.chat_id } };
  }
  async ow() { throw new Error('Group selection must not send WhatsApp requests'); }
}
function fixture(t: any) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), owner: '123', management: '-1001', masterToken: 'token', session: 's1', history: 0 };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  store.put('route', 'group@g.us', { chatId: 'group@g.us', tgChat: '-1001', thread: 5, name: 'Friends', mode: 'inbox' });
  t.after(() => store.close()); return { bridge, store, api };
}
async function picker(b: Bridge, api: Fixture) {
  const button = await b.mirrorButton('group@g.us');
  const parameter = new URL(button.url).searchParams.get('start')!;
  await b.telegram('master', { message: { message_id: 1, from: { id: 123 }, chat: { id: 123, type: 'private' }, text: '/start ' + parameter } }, 'start-picker');
  return api.calls.find(c => c.body.reply_markup?.keyboard)?.body.reply_markup.keyboard[0][0].request_chat;
}

test('native group creation/selection grants master rights and links only after the owner shares a group', async t => {
  const { bridge, store, api } = fixture(t);
  const request = await picker(bridge, api);
  assert.equal(request.chat_is_channel, false);
  assert.equal(request.chat_has_username, false);
  assert.equal(request.chat_is_created, true);
  assert.equal(request.bot_is_member, true);
  assert.equal(request.bot_administrator_rights.can_delete_messages, true);
  assert.deepEqual(request.user_administrator_rights, request.bot_administrator_rights);
  assert.equal(store.get('route', 'group@g.us').tgChat, '-1001');
  await bridge.telegram('master', { message: { message_id: 2, chat: { id: 123, type: 'private' }, chat_shared: { request_id: request.request_id, chat_id: -200 } } }, 'selection');
  const event = JSON.parse(store.db.prepare("SELECT payload FROM jobs WHERE type='n8n'").get()!.payload as string);
  assert.equal(event.type, 'link_group');
  assert.equal(event.data.chatId, 'group@g.us');
  await bridge.manage(event, 'link-result');
  assert.equal(store.get('route', 'group@g.us').tgChat, '-200');
  assert.equal(store.get('chat-request', String(request.request_id)).used, true);
  assert.ok(api.calls.some(c => c.body.reply_markup?.remove_keyboard));
  // A retry of this exact completion is safe and cannot change the selected destination.
  await bridge.manage(event, 'link-result');
  await assert.rejects(bridge.manage({ ...event, data: { ...event.data, tgChat: '-300' } }, 'replay'), /selection expired/);
});

test('expired and previous-session buttons produce guidance without opening a selector', async t => {
  const { bridge, store, api } = fixture(t);
  const button = await bridge.mirrorButton('group@g.us');
  const id = new URL(button.url).searchParams.get('start')!.slice('mirror_'.length);
  const action = store.get('action', id);
  store.put('action', id, { ...action, expires: Date.now() - 1 });
  await bridge.beginMirrorGroup(id, 'expired');
  assert.ok(!api.calls.some(c => c.body.reply_markup?.keyboard));
  store.put('action', id, action); bridge.c.session = 'different-session';
  await bridge.beginMirrorGroup(id, 'different-session');
  assert.ok(!api.calls.some(c => c.body.reply_markup?.keyboard));
});

test('unknown requests and other Telegram users cannot link a group', async t => {
  const { bridge, store, api } = fixture(t);
  const request = await picker(bridge, api);
  const message = { message_id: 3, from: { id: 555 }, chat: { id: 555, type: 'private' }, chat_shared: { request_id: request.request_id, chat_id: -200 } };
  assert.equal(bridge.acceptTelegram('master', { update_id: 3, message }), false);
  await bridge.telegram('master', { message: { ...message, chat: { id: 123, type: 'private' }, chat_shared: { request_id: 999, chat_id: -200 } } }, 'unknown');
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='n8n'").get()!.n, 0);
  assert.equal(store.get('route', 'group@g.us').tgChat, '-1001');
});

test('picker rejects public destinations and the management group', async t => {
  const { bridge, store, api } = fixture(t);
  const request = await picker(bridge, api);
  const event = { type: 'link_group', data: { chatId: 'group@g.us', tgChat: '-1001', requestId: String(request.request_id) } };
  await assert.rejects(bridge.manage(event, 'management'), /separate private group/);
  api.public = true;
  await assert.rejects(bridge.manage({ ...event, data: { ...event.data, tgChat: '-200' } }, 'public'), /private Telegram group/);
  assert.equal(store.get('route', 'group@g.us').tgChat, '-1001');
});

test('new group inbox controls include the button and /group works for existing topics', async t => {
  const { bridge, api } = fixture(t);
  await bridge.manage({ type: 'new_conversation', data: { chatId: 'group@g.us' } }, 'controls');
  const controls = api.calls.find(c => c.body.reply_markup?.inline_keyboard)?.body;
  assert.equal(controls.reply_markup.inline_keyboard[0][0].text, 'Create / link Telegram group');
  await bridge.command('master', { text: '/group', chat: { id: -1001, type: 'supergroup' }, message_thread_id: 5 }, 'command');
  assert.equal(api.calls.filter(c => c.body.reply_markup?.inline_keyboard?.[0]?.[0]?.url?.includes('?start=mirror_')).length, 2);
});
