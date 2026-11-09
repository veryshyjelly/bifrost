import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Bridge } from '../src/bridge.ts';
import { Store } from '../src/store.ts';
import { Api, ApiError } from '../src/api.ts';
import { configuration } from '../src/config.ts';

class Fixture extends Api {
  calls: any[] = []; rows: any[] = []; sequence = 100; failHistory = false; failDelete = false; failMessage = 0; contentSends = 0;
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
    if (method === 'getMe') return { id: token === 'master' ? 99 : 42, username: 'hermes_bot' };
    if (method === 'getChat') return { type: 'supergroup', is_forum: true };
    if (method === 'getChatMember') return { status: 'administrator', can_delete_messages: true, can_manage_topics: true };
    if (method === 'createForumTopic') return { message_thread_id: ++this.sequence };
    if (method === 'deleteForumTopic' && this.failDelete) throw new ApiError('Permission denied', 403);
    if (method === 'sendMessage' && body.text.startsWith('History ·') && ++this.contentSends === this.failMessage) throw new ApiError('Rate limited', 429, true);
    return { message_id: ++this.sequence, chat: { id: body.chat_id }, from: { id: token === 'master' ? 99 : 42 }, text: body.text, entities: body.entities,
      ...(body.reply_parameters ? { reply_to_message: { message_id: body.reply_parameters.message_id } } : {}) };
  }
  async ow(path: string, body?: any) {
    this.calls.push({ path, body });
    if (path.includes('/history?')) { if (this.failHistory) throw new ApiError('Unavailable history', 502); return this.rows; }
    if (path.startsWith('messages/')) return { messageId: 'sent-' + (++this.sequence) };
    throw new Error('History must not call WhatsApp write or read-receipt endpoints');
  }
  async upload(token: string, method: string, field: string, path: string, name: string, body: any) {
    this.calls.push({ token, method, field, path, name, body });
    return { message_id: ++this.sequence, chat: { id: body.chat_id }, from: { id: token === 'master' ? 99 : 42 }, document: { file_id: 'archive-file', file_name: name }, caption: body.caption, caption_entities: body.caption_entities };
  }
  async request() { return { ok: true }; }
}
function fixture(t: any) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), masterToken: 'master', owner: '123', management: '-1001', session: 's1', openwaKey: 'key', history: 20 };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', name: 'Group', tgChat: '-200', mode: 'group' });
  store.put('contact', '12025550123@c.us', { name: 'Alice' });
  t.after(() => store.close()); return { bridge, store, api, c };
}
const message = (id: string, timestamp: number, body = id, extra = {}) => ({ id, chatId: 'g@g.us', from: 'g@g.us', author: '12025550123@c.us', isGroup: true, fromMe: false, type: 'text', body, timestamp, ...extra });
const event = (m: any) => ({ event: 'message.received', sessionId: 's1', idempotencyKey: m.id, data: m });
const command = (text: string, extra = {}) => ({ message_id: 1, from: { id: 123 }, chat: { id: -200, type: 'supergroup' }, text, ...extra });
const content = (api: Fixture) => api.calls.filter(call => call.method === 'sendMessage' && call.body.text?.startsWith('History ·'));
async function drain(b: Bridge) { for (let i = 0; i < 100; i++) await b.tick(); }

test('/history n fetches the complete batch first, posts oldest first, and recreates native reply chains', async t => {
  const { bridge, api, store } = fixture(t);
  api.rows = [message('c', 3, 'third', { quotedMessage: { id: 'b', body: 'second' } }), message('a', 1, 'first'), message('b', 2, 'second', { quotedMessage: { id: 'a', body: 'first' } })];
  await bridge.command('master', command('/history 3'), 'history-three');
  assert.equal(content(api).length, 0); await bridge.tick(); assert.equal(content(api).length, 0);
  assert.equal(store.all('history-plan')[0].items.length, 3);
  await drain(bridge);
  const sent = content(api); assert.deepEqual(sent.map(call => call.body.text.split(': ').at(-1)), ['first', 'second', 'third']);
  assert.equal(sent[1].body.reply_parameters.message_id, store.byWhatsApp('g@g.us', 'a')[0].tg_id);
  assert.equal(sent[2].body.reply_parameters.message_id, store.byWhatsApp('g@g.us', 'b')[0].tg_id);
  assert.ok(api.calls.find(call => call.path)?.path.includes('limit=3&includeMedia=false'));
  assert.equal(store.all('history-run')[0].status, 'complete');
});

test('history delivered to management can be imported into a mirror and repeated requests do not duplicate it', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-1001', thread: 77, mode: 'inbox' });
  const original = message('old', 1, 'Archived'); await bridge.whatsApp(event(original), 'old-live');
  assert.equal(store.get('wa-complete', 'g@g.us:old'), true);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' }); api.rows = [original];
  await bridge.command('master', command('/history 1'), 'first-import'); await drain(bridge);
  await bridge.command('master', command('/history 1', { message_id: 2 }), 'second-import'); await drain(bridge);
  assert.equal(content(api).length, 1); assert.equal(store.byWhatsApp('g@g.us', 'old').length, 2);
  await bridge.waChange({ event: 'message.edited', data: { chatId: 'g@g.us', id: 'old', body: 'Edited' } }, 'edit');
  const edited = api.calls.filter(call => call.method === 'editMessageText');
  assert.ok(!edited[0].body.text.startsWith('History')); assert.ok(edited[1].body.text.startsWith('History'));
});

test('history in a new member DM includes own messages and preserves bot-scoped reply targets', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('contact', '12025550123@c.us', { name: 'Alice', bot: '42' }); store.put('bot', '42', { token: store.seal('child'), ready: true });
  api.rows = [message('own', 1, 'Mine', { chatId: '12025550123@c.us', fromMe: true }), message('answer', 2, 'Answer', { chatId: '12025550123@c.us', quotedMessage: { id: 'own', body: 'Mine' } })];
  await bridge.command('42', command('/history 2', { chat: { id: 123, type: 'private' } }), 'dm-history'); await drain(bridge);
  assert.equal(content(api).length, 2); assert.ok(content(api).every(call => call.token === 'child' && call.body.chat_id === '123'));
  assert.ok(content(api)[0].body.text.includes('You'));
  assert.equal(content(api)[1].body.reply_parameters.message_id, store.byWhatsApp('12025550123@c.us', 'own')[0].tg_id);
  assert.ok(!api.calls.some(call => call.method === 'createForumTopic'));
});

test('history retries use the saved batch, preserve earlier parts and hold only live events for this conversation', async t => {
  const { bridge, api, store } = fixture(t);
  api.rows = [message('a', 1), message('b', 2), message('c', 3)]; api.failMessage = 2;
  await bridge.command('master', command('/history 3'), 'retry-history');
  bridge.acceptWhatsApp(event(message('live', 4)));
  store.put('route', 'other@g.us', { chatId: 'other@g.us', tgChat: '-300', mode: 'group' });
  bridge.acceptWhatsApp(event(message('unrelated', 5, 'Other conversation', { chatId: 'other@g.us' })));
  await drain(bridge);
  assert.equal(store.byWhatsApp('g@g.us', 'live').length, 0);
  assert.equal(store.byWhatsApp('other@g.us', 'unrelated').length, 1);
  assert.equal(store.all('history-run')[0].status, 'delivering');
  api.rows = [message('different', 10)]; store.db.prepare("UPDATE jobs SET due=0 WHERE type='history-message' AND state='pending'").run();
  await drain(bridge);
  assert.equal(store.byWhatsApp('g@g.us', 'a').length, 1); assert.equal(store.byWhatsApp('g@g.us', 'b').length, 1); assert.equal(store.byWhatsApp('g@g.us', 'c').length, 1);
  assert.equal(store.byWhatsApp('g@g.us', 'live').length, 1); assert.equal(store.byWhatsApp('g@g.us', 'different').length, 0);
  assert.equal(api.calls.filter(call => call.path?.includes('/history?')).length, 1);
  const sent = api.calls.filter(call => call.method === 'sendMessage'); assert.ok(sent.findIndex(call => call.body.text.endsWith(': c')) < sent.findIndex(call => call.body.text.endsWith(': live')));
});

test('history orders parents before children even with equal or inconsistent source timestamps and previews outside-window quotes', async t => {
  const { bridge, api } = fixture(t);
  api.rows = [message('child', 1, 'Child', { quotedMessage: { id: 'parent', body: 'Parent' } }), message('parent', 2, 'Parent'), message('outside', 3, 'Outside', { quotedMessage: { id: 'missing', body: '<Older quote> & 😀' } })];
  await bridge.command('master', command('/history 3'), 'dependencies'); await drain(bridge);
  const sent = content(api); assert.ok(sent[0].body.text.endsWith(': Parent')); assert.ok(sent[1].body.reply_parameters);
  assert.ok(sent[2].body.text.includes('↪ <Older quote> & 😀\nOutside')); assert.equal(sent[2].body.reply_parameters, undefined);
});

test('history reuses archived Telegram media even after its management topic has been removed', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-1001', thread: 77, mode: 'inbox' });
  bridge.media.fromWhatsApp = async () => ({ path: '/source', name: 'original.bin' });
  const item = message('file', 1, 'Caption', { type: 'document' }); await bridge.whatsApp(event(item), 'archived-file');
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  await bridge.retireInbox('g@g.us');
  let archived = false;
  bridge.media.fromTelegram = async (token, m) => { assert.equal(token, 'master'); assert.equal(m.document.file_id, 'archive-file'); archived = true; return { kind: 'document', path: '/cached', name: 'original.bin', mime: 'application/octet-stream' }; };
  bridge.media.fromWhatsApp = async () => { throw new Error('Use the Telegram archive'); };
  api.rows = [item]; await bridge.command('master', command('/history 1'), 'media-history'); await drain(bridge);
  assert.equal(archived, true); assert.equal(api.calls.filter(call => call.method === 'sendDocument').at(-1).path, '/cached');
  assert.equal(store.byWhatsApp('g@g.us', 'file').length, 2);
});

test('manual history can restore a copy explicitly cleared from the current destination without reusing deleted receipts', async t => {
  const { bridge, api, store } = fixture(t); api.rows = [message('a', 1)];
  await bridge.command('master', command('/history 1'), 'first'); await drain(bridge);
  const first = store.byWhatsApp('g@g.us', 'a')[0]; store.db.prepare('UPDATE telegram_messages SET deleted=1 WHERE message=?').run(first.tg_id);
  await bridge.command('master', command('/history 1', { message_id: 2 }), 'restore'); await drain(bridge);
  assert.equal(content(api).length, 2); assert.equal(store.byWhatsApp('g@g.us', 'a').length, 2);
});

test('history validates counts and uses the gateway deep-history option above 100', async t => {
  const { bridge, api, store } = fixture(t);
  for (const [i, value] of ['0', '-1', '2001', 'abc', '1 2', '1.5'].entries()) await bridge.command('master', command('/history ' + value, { message_id: i + 1 }), 'invalid-' + i);
  assert.equal(store.all('history-run').length, 0);
  await bridge.command('master', command('/history 2000', { message_id: 20 }), 'deep'); await drain(bridge);
  assert.ok(api.calls.find(call => call.path?.includes('/history?')).path.includes('limit=2000&includeMedia=false&deep=true'));
});

test('history fetch failure releases live delivery and never deletes a management topic', async t => {
  const { bridge, api, store } = fixture(t); api.failHistory = true;
  await bridge.command('master', command('/history 10'), 'failed-history'); bridge.acceptWhatsApp(event(message('live', 4)));
  await drain(bridge);
  assert.equal(store.all('history-run')[0].status, 'failed'); assert.equal(store.byWhatsApp('g@g.us', 'live').length, 1);
  assert.ok(!api.calls.some(call => call.method === 'deleteForumTopic'));
});

test('linking a mirror queues old-topic deletion but never imports history automatically', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', name: 'Group', tgChat: '-1001', thread: 77, mode: 'inbox' });
  await bridge.whatsApp(event(message('old', 1)), 'old');
  await bridge.manage({ type: 'link_group', data: { chatId: 'g@g.us', tgChat: '-200' } }, 'link');
  assert.ok(!api.calls.some(call => call.method === 'deleteForumTopic')); assert.equal(store.all('history-run').length, 0);
  const job = store.db.prepare("SELECT * FROM jobs WHERE type='control-retire-inbox'").get()!;
  await bridge.control.process({ ...job, payload: JSON.parse(job.payload as string) });
  const deletion = api.calls.find(call => call.method === 'deleteForumTopic'); assert.deepEqual(deletion.body, { chat_id: '-1001', message_thread_id: 77 });
  assert.equal(store.get('route', 'g@g.us').tgChat, '-200'); assert.equal(store.byWhatsApp('g@g.us', 'old').length, 1);
});

test('starting a bound DM retires its inbox separately and cleanup refusal does not block messaging', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('contact', '12025550123@c.us', { name: 'Alice', bot: '42' }); store.put('bot', '42', { token: store.seal('child'), ready: false });
  store.put('route', '12025550123@c.us', { chatId: '12025550123@c.us', tgChat: '-1001', thread: 77, mode: 'dedicated' });
  await bridge.command('42', command('/start', { chat: { id: 123, type: 'private' } }), 'start');
  assert.equal(store.all('history-run').length, 0); api.failDelete = true;
  const job = store.db.prepare("SELECT * FROM jobs WHERE type='control-retire-inbox'").get()!;
  await bridge.control.process({ ...job, payload: JSON.parse(job.payload as string) });
  await bridge.telegram('42', { message: command('Hello', { message_id: 2, chat: { id: 123, type: 'private' } }) }, 'dm');
  assert.ok(api.calls.some(call => call.path === 'messages/send-text'));
  assert.equal(store.get('route', '12025550123@c.us').thread, 77);
});

test('retiring a DM topic is durable and a blocked member recreates an inbox lazily', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('contact', '12025550123@c.us', { name: 'Alice', bot: '42' }); store.put('bot', '42', { token: store.seal('child'), ready: true });
  store.put('route', '12025550123@c.us', { chatId: '12025550123@c.us', tgChat: '-1001', thread: 77, mode: 'dedicated' });
  await bridge.retireInbox('12025550123@c.us'); await bridge.retireInbox('12025550123@c.us');
  assert.equal(api.calls.filter(call => call.method === 'deleteForumTopic').length, 1);
  const original = api.tg.bind(api);
  api.tg = async (token, method, body = {}) => { if (token === 'child' && method === 'sendMessage') throw Object.assign(new ApiError('Blocked', 403), { service: 'telegram' }); return original(token, method, body); };
  await bridge.deliverWhatsApp(event(message('fallback', 1, 'Fallback', { chatId: '12025550123@c.us', isGroup: false })), 'fallback');
  assert.ok(api.calls.some(call => call.method === 'createForumTopic')); assert.equal(store.get('route', '12025550123@c.us').inboxRetired, false);
});

test('General and management topics shared with another conversation cannot be deleted by retirement', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group', inbox: { chat: '-1001', thread: 1 } });
  await bridge.retireInbox('g@g.us'); assert.ok(!api.calls.some(call => call.method === 'deleteForumTopic'));
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group', inbox: { chat: '-1001', thread: 77 } });
  store.put('route', 'other@g.us', { chatId: 'other@g.us', tgChat: '-1001', thread: 77, mode: 'inbox' });
  await assert.rejects(bridge.retireInbox('g@g.us'), /shared/); assert.ok(!api.calls.some(call => call.method === 'deleteForumTopic'));
});

test('a retired direct route cannot capture General or another management topic', async t => {
  const { bridge, store } = fixture(t);
  store.put('route', '12025550123@c.us', { chatId: '12025550123@c.us', tgChat: '-1001', mode: 'dedicated', inboxRetired: true });
  store.put('route', 'other@g.us', { chatId: 'other@g.us', tgChat: '-1001', thread: 88, mode: 'inbox' });
  assert.equal(bridge.telegramContext('master', { chat: { id: -1001, type: 'supergroup' } }).route, undefined);
  assert.equal(bridge.telegramContext('master', { chat: { id: -1001, type: 'supergroup' }, is_topic_message: true, message_thread_id: 77 }).route, undefined);
  assert.equal(bridge.telegramContext('master', { chat: { id: -1001, type: 'supergroup' }, is_topic_message: true, message_thread_id: 88 }).route.chatId, 'other@g.us');
});

test('history requests from a previous session cannot block the current session', async t => {
  const { bridge, store } = fixture(t);
  store.put('history-run', 'old', { chatId: 'g@g.us', session: 'old-session', status: 'delivering' });
  const run = bridge.conversationHistory.request('g@g.us', { chat: '-200' }, 1, 'new');
  assert.equal(run.session, 's1'); assert.equal(run.count, 1); assert.equal(run.status, 'queued');
});
