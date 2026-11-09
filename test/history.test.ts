import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Bridge } from '../src/bridge.ts';
import { Store } from '../src/store.ts';
import { Api, ApiError } from '../src/api.ts';
import { configuration, digest } from '../src/config.ts';

class Fixture extends Api {
  calls: any[] = []; rows: any[] = []; sequence = 100; failHistory = false; failDelete = false; failMessage = 0; contentSends = 0;
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
    if (method === 'getMe') return { id: token === 'master' ? 99 : 42, username: 'hermes_bot' };
    if (method === 'getChat') return { type: 'supergroup', is_forum: true };
    if (method === 'getChatMember') return { status: 'administrator', can_delete_messages: true, can_manage_topics: true };
    if (method === 'createForumTopic') return { message_thread_id: ++this.sequence };
    if (method === 'deleteForumTopic' && this.failDelete) throw new ApiError('Permission denied', 403);
    if (method === 'sendMessage' && body.entities?.some(e => e.type === 'date_time' && e.date_time_format === 't') && ++this.contentSends === this.failMessage) throw new ApiError('Rate limited', 429, true);
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
const content = (api: Fixture) => api.calls.filter(call => call.method === 'sendMessage' && call.body.entities?.some(e => e.type === 'date_time' && e.date_time_format === 't'));
async function drain(b: Bridge) { for (let i = 0; i < 100; i++) await b.tick(); }
async function choose(b: Bridge, bot: string, m: any, key: string, mode = 'skip') {
  const id = digest(JSON.stringify([b.c.session, bot, key])).slice(0, 24);
  await b.telegram(bot, { callback_query: { id: key + ':callback', from: m.from, message: { ...m, message_id: b.store.get('history-choice', id)?.promptMessage }, data: `history:${mode}:${id}` } }, key + ':choice');
}
async function history(b: Bridge, bot: string, m: any, key: string, mode = 'skip') {
  await b.command(bot, m, key); await choose(b, bot, m, key, mode);
}

test('/history n fetches the complete batch first, posts oldest first, and recreates native reply chains', async t => {
  const { bridge, api, store } = fixture(t);
  api.rows = [message('c', 3, 'third', { quotedMessage: { id: 'b', body: 'second' } }), message('a', 1, 'first'), message('b', 2, 'second', { quotedMessage: { id: 'a', body: 'first' } })];
  await history(bridge, 'master', command('/history 3'), 'history-three');
  assert.equal(content(api).length, 0); await bridge.tick(); await bridge.tick(); assert.equal(content(api).length, 0);
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
  await history(bridge, 'master', command('/history 1'), 'first-import'); await drain(bridge);
  await history(bridge, 'master', command('/history 1', { message_id: 2 }), 'second-import'); await drain(bridge);
  assert.equal(content(api).length, 1); assert.equal(store.byWhatsApp('g@g.us', 'old').length, 2);
  await bridge.waChange({ event: 'message.edited', data: { chatId: 'g@g.us', id: 'old', body: 'Edited' } }, 'edit');
  const edited = api.calls.filter(call => call.method === 'editMessageText');
  assert.ok(!edited[0].body.entities.some(e => e.type === 'date_time')); assert.ok(edited[1].body.entities.some(e => e.type === 'date_time' && e.date_time_format === 't'));
});

test('history in a new member DM includes own messages and preserves bot-scoped reply targets', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('contact', '12025550123@c.us', { name: 'Alice', bot: '42' }); store.put('bot', '42', { token: store.seal('child'), ready: true });
  api.rows = [message('own', 1, 'Mine', { chatId: '12025550123@c.us', fromMe: true }), message('answer', 2, 'Answer', { chatId: '12025550123@c.us', quotedMessage: { id: 'own', body: 'Mine' } })];
  await history(bridge, '42', command('/history 2', { chat: { id: 123, type: 'private' } }), 'dm-history'); await drain(bridge);
  assert.equal(content(api).length, 2); assert.ok(content(api).every(call => call.token === 'child' && call.body.chat_id === '123'));
  assert.ok(content(api)[0].body.text.includes('You'));
  assert.equal(content(api)[1].body.reply_parameters.message_id, store.byWhatsApp('12025550123@c.us', 'own')[0].tg_id);
  assert.ok(!api.calls.some(call => call.method === 'createForumTopic'));
});

test('history retries use the saved batch, preserve earlier parts and hold only live events for this conversation', async t => {
  const { bridge, api, store } = fixture(t);
  api.rows = [message('a', 1), message('b', 2), message('c', 3)]; api.failMessage = 2;
  await history(bridge, 'master', command('/history 3'), 'retry-history');
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
  await history(bridge, 'master', command('/history 3'), 'dependencies'); await drain(bridge);
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
  api.rows = [item]; await history(bridge, 'master', command('/history 1'), 'media-history'); await drain(bridge);
  assert.equal(archived, true); assert.equal(api.calls.filter(call => call.method === 'sendDocument').at(-1).path, '/cached');
  assert.equal(store.byWhatsApp('g@g.us', 'file').length, 2);
});

test('manual history can restore a copy explicitly cleared from the current destination without reusing deleted receipts', async t => {
  const { bridge, api, store } = fixture(t); api.rows = [message('a', 1)];
  await history(bridge, 'master', command('/history 1'), 'first'); await drain(bridge);
  const first = store.byWhatsApp('g@g.us', 'a')[0]; store.db.prepare('UPDATE telegram_messages SET deleted=1 WHERE message=?').run(first.tg_id);
  await history(bridge, 'master', command('/history 1', { message_id: 2 }), 'restore'); await drain(bridge);
  assert.equal(content(api).length, 2); assert.equal(store.byWhatsApp('g@g.us', 'a').length, 2);
});

test('history sends retained WebM sticker media through the Telegram sticker preparation path', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-1001', thread: 77, mode: 'inbox' });
  bridge.media.fromWhatsApp = async () => ({ path: '/source', name: 'source.webp' });
  bridge.media.sticker = async file => ({ ...file, path: '/converted', name: 'sticker.webm', mime: 'video/webm' });
  const item = message('sticker', 1, '', { type: 'sticker' }); await bridge.whatsApp(event(item), 'archived-sticker');
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  let prepared = false;
  bridge.media.fromTelegram = async () => ({ kind: 'sticker', path: '/retained', name: 'sticker.webm', mime: 'video/webm' });
  bridge.media.fromWhatsApp = async () => { throw new Error('Source media has expired'); };
  bridge.media.sticker = async (file, target) => { assert.equal(file.path, '/retained'); assert.equal(file.mime, 'video/webm'); assert.equal(target, 'telegram'); prepared = true; return file; };
  api.rows = [item]; await history(bridge, 'master', command('/history 1'), 'sticker-history'); await drain(bridge);
  assert.equal(prepared, true);
  const sent = api.calls.filter(call => call.method === 'sendSticker').at(-1);
  assert.equal(sent.path, '/retained'); assert.equal(sent.name, 'sticker.webm');
  assert.equal(store.byWhatsApp('g@g.us', 'sticker').filter(row => row.media).length, 2);
});

test('history validates counts and uses the gateway deep-history option above 100', async t => {
  const { bridge, api, store } = fixture(t);
  for (const [i, value] of ['0', '-1', '2001', 'abc', '1 2', '1.5'].entries()) await bridge.command('master', command('/history ' + value, { message_id: i + 1 }), 'invalid-' + i);
  assert.equal(store.all('history-run').length, 0);
  await history(bridge, 'master', command('/history 2000', { message_id: 20 }), 'deep'); await drain(bridge);
  assert.ok(api.calls.find(call => call.path?.includes('/history?')).path.includes('limit=2000&includeMedia=false&deep=true'));
});

test('history fetch failure releases live delivery and never deletes a management topic', async t => {
  const { bridge, api, store } = fixture(t); api.failHistory = true;
  await history(bridge, 'master', command('/history 10'), 'failed-history'); bridge.acceptWhatsApp(event(message('live', 4)));
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

test('/history waits for a choice without fetching or holding live messages, and Cancel does not start an import', async t => {
  const { bridge, api, store } = fixture(t); const m = command('/history 50');
  await bridge.command('master', m, 'ask');
  const prompt = api.calls.find(call => call.body?.reply_markup?.inline_keyboard?.[0]?.[0]?.text === 'Skip existing');
  assert.deepEqual(prompt.body.reply_markup.inline_keyboard.flat().map(button => button.text), ['Skip existing', 'Import all', 'Cancel']);
  assert.equal(store.all('history-run').length, 0);
  bridge.acceptWhatsApp(event(message('live', 1))); await drain(bridge);
  assert.equal(store.byWhatsApp('g@g.us', 'live').length, 1);
  assert.ok(!api.calls.some(call => call.path?.includes('/history?')));
  await choose(bridge, 'master', m, 'ask', 'cancel'); await choose(bridge, 'master', m, 'ask', 'all');
  assert.equal(store.all('history-run').length, 0);
  assert.equal(api.calls.filter(call => call.body?.text === 'History import cancelled.').length, 1);
});

test('Import all restores manually cleared messages despite old delivery records and links replies to the new copies', async t => {
  const { bridge, api, store } = fixture(t);
  const parent = message('parent', 1, 'Parent'), child = message('child', 2, 'Child', { quotedMessage: { id: 'parent', body: 'Parent' } });
  await bridge.whatsApp(event(parent), 'old-parent'); await bridge.whatsApp(event(child), 'old-child');
  const oldParent = store.byWhatsApp('g@g.us', 'parent')[0].tg_id;
  // Manual Telegram clearing leaves no deletion update or ledger change.
  api.rows = [child, parent];
  await history(bridge, 'master', command('/history 2'), 'skip'); await drain(bridge);
  assert.equal(content(api).length, 0);
  await history(bridge, 'master', command('/history 2'), 'all', 'all'); await drain(bridge);
  const copies = content(api), newParent = store.byWhatsApp('g@g.us', 'parent').at(-1)!;
  assert.equal(copies.length, 2); assert.notEqual(newParent.tg_id, oldParent);
  assert.equal(copies[1].body.reply_parameters.message_id, newParent.tg_id);
  assert.ok(store.all('history-run').some(run => run.mode === 'all' && run.status === 'complete'));
  await bridge.whatsApp(event(message('new-reply', 3, 'Live reply', { quotedMessage: { id: 'parent' } })), 'live-reply');
  assert.equal(api.calls.filter(call => call.method === 'sendMessage').at(-1).body.reply_parameters.message_id, newParent.tg_id);
  await bridge.waChange({ event: 'message.reaction', data: { chatId: 'g@g.us', messageId: 'parent', senderId: '12025550123@c.us', reaction: '👍' } }, 'react');
  assert.equal(api.calls.find(call => call.method === 'setMessageReaction').body.message_id, newParent.tg_id);
});

test('Import all previews out-of-window quotes instead of replying to stale copies from a cleared chat', async t => {
  const { bridge, api } = fixture(t);
  await bridge.whatsApp(event(message('old-parent', 1, 'Old parent')), 'old-parent');
  api.rows = [message('child', 2, 'Child', { quotedMessage: { id: 'old-parent', body: 'Old parent' } })];
  await history(bridge, 'master', command('/history 1'), 'outside', 'all'); await drain(bridge);
  const restored = content(api)[0].body;
  assert.equal(restored.reply_parameters, undefined); assert.ok(restored.text.includes('↪ Old parent'));
});

test('Import all retries a frozen batch without duplicating parts and repeated clicks cannot start another run or switch mode', async t => {
  const { bridge, api, store } = fixture(t);
  const parent = message('parent', 1), child = message('child', 2, 'Child', { quotedMessage: { id: 'parent' } });
  await bridge.whatsApp(event(parent), 'old-parent'); await bridge.whatsApp(event(child), 'old-child');
  api.rows = [parent, child]; api.failMessage = 2;
  const m = command('/history 2'); await history(bridge, 'master', m, 'retry-all', 'all'); await drain(bridge);
  assert.equal(store.all('history-run')[0].status, 'delivering');
  await choose(bridge, 'master', m, 'retry-all', 'skip'); await choose(bridge, 'master', m, 'retry-all', 'cancel');
  assert.equal(store.all('history-run').length, 1); assert.equal(store.all('history-run')[0].mode, 'all');
  api.rows = [message('different', 3)]; store.db.prepare("UPDATE jobs SET due=0 WHERE type='history-message' AND state='pending'").run(); await drain(bridge);
  assert.equal(store.byWhatsApp('g@g.us', 'parent').length, 2); assert.equal(store.byWhatsApp('g@g.us', 'child').length, 2);
  assert.equal(store.byWhatsApp('g@g.us', 'different').length, 0);
  assert.equal(api.calls.filter(call => call.path?.includes('/history?')).length, 1);
  await history(bridge, 'master', command('/history 2', { message_id: 2 }), 'separate-all', 'all'); await drain(bridge);
  assert.equal(store.byWhatsApp('g@g.us', 'different').length, 1);
});

test('Import all still excludes WhatsApp-revoked messages and preserves other destinations', async t => {
  const { bridge, api, store } = fixture(t);
  const row = message('old', 1); await bridge.whatsApp(event(row), 'old');
  store.map('g@g.us', 'other-copy', '-300', 12, 'master', 'in');
  store.put('revoked', 'g@g.us:revoked', true);
  api.rows = [row, message('revoked', 2)];
  await history(bridge, 'master', command('/history 2'), 'revocations', 'all'); await drain(bridge);
  assert.equal(store.byWhatsApp('g@g.us', 'revoked').length, 0);
  assert.equal(store.byWhatsApp('g@g.us', 'old').length, 2);
  assert.equal(store.byTelegram('-300', 12, 'master').wa_id, 'other-copy');
  const prompt = store.all('history-choice')[0].promptMessage;
  assert.ok(api.calls.filter(call => call.method === 'deleteMessages').every(call => call.body.message_ids.every(id => id === prompt)));
  assert.ok(!api.calls.some(call => ['deleteMessage', 'deleteForumTopic'].includes(call.method)));
});

test('history choices validate owner, bot, session, destination, topic, binding and expiry', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-1001', thread: 77, mode: 'inbox' });
  store.put('bot', '42', { token: store.seal('child') });
  const m = command('/history 5', { chat: { id: -1001, type: 'supergroup' }, message_thread_id: 77, is_topic_message: true });
  await bridge.command('master', m, 'secure');
  const id = store.all('history-choice')[0].id, original = store.get('history-choice', id);
  const q = { id: 'q', from: { id: 123 }, message: { ...m, message_id: original.promptMessage }, data: `history:all:${id}` };
  for (const changes of [{ from: { id: 999 } }, { message: { ...q.message, message_thread_id: 78 } }, { message: { ...q.message, chat: { id: -1002, type: 'supergroup' } } }, { message: { ...q.message, message_id: original.promptMessage + 1 } }]) await bridge.conversationHistory.callback('master', { ...q, ...changes });
  await bridge.conversationHistory.callback('42', q);
  store.put('history-choice', id, { ...original, session: 'old' }); await bridge.conversationHistory.callback('master', q);
  store.put('history-choice', id, { ...original, expires: 0 }); await bridge.conversationHistory.callback('master', q);
  store.put('history-choice', id, original);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' }); await bridge.conversationHistory.callback('master', q);
  assert.equal(store.all('history-run').length, 0); assert.ok(!api.calls.some(call => call.path?.includes('/history?')));
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE dedup LIKE 'history-choice-delete:%'").get()!.n, 0);
});

test('member DM Import all remains bot scoped and legacy Recent history buttons now show the choice', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('contact', '12025550123@c.us', { name: 'Alice', bot: '42' }); store.put('bot', '42', { token: store.seal('child'), ready: true });
  store.put('route', '12025550123@c.us', { chatId: '12025550123@c.us', tgChat: '-1001', mode: 'dedicated', inboxRetired: true });
  const row = message('dm-old', 1, 'Private', { chatId: '12025550123@c.us', isGroup: false });
  await bridge.whatsApp(event(row), 'dm-old');
  store.map('12025550123@c.us', 'dm-old', '123', 888, 'master', 'in'); api.rows = [row];
  await history(bridge, '42', command('/history 1', { chat: { id: 123, type: 'private' } }), 'dm-all', 'all'); await drain(bridge);
  assert.equal(content(api)[0].token, 'child'); assert.equal(content(api)[0].body.chat_id, '123');
  assert.equal(store.byTelegram('123', 888, 'master').bot, 'master');
  const action = bridge.action({ chatId: 'g@g.us' });
  await bridge.telegram('master', { callback_query: { id: 'legacy', from: { id: 123 }, message: command('header'), data: 'history:' + action } }, 'legacy');
  const queued = store.db.prepare("SELECT payload FROM jobs WHERE type='management' AND json_extract(payload,'$.type')='operator_action'").get()!;
  await bridge.manage(JSON.parse(queued.payload as string), 'legacy-management');
  assert.equal(store.all('history-choice').length, 2); assert.equal(store.all('history-run').length, 1);
});

test('two pending choices cannot start concurrent imports or change an active import mode', async t => {
  const { bridge, store } = fixture(t), m = command('/history 5');
  await bridge.command('master', m, 'one'); await bridge.command('master', m, 'two');
  await choose(bridge, 'master', m, 'one', 'skip'); await choose(bridge, 'master', m, 'two', 'all');
  assert.equal(store.all('history-run').length, 1); assert.equal(store.all('history-run')[0].mode, 'skip');
  assert.equal(store.all('history-choice').filter(choice => choice.status === 'busy').length, 1);
});

test('reactions prefer a fresh unavailable-media placeholder over a stale attachment from before clearing', async t => {
  const { bridge, api, store } = fixture(t);
  store.map('g@g.us', 'file', '-200', 55, 'master', 'in', true);
  bridge.control.track('master', { message_id: 55, chat: { id: -200, type: 'supergroup' } }, 'chat');
  bridge.media.fromWhatsApp = async () => { throw new ApiError('Media no longer available', 404); };
  api.rows = [message('file', 1, 'File', { type: 'document' })];
  await history(bridge, 'master', command('/history 1'), 'placeholder', 'all'); await drain(bridge);
  const replacement = store.byWhatsApp('g@g.us', 'file').at(-1)!; assert.notEqual(replacement.tg_id, 55);
  await bridge.waChange({ event: 'message.reaction', data: { chatId: 'g@g.us', messageId: 'file', senderId: '12025550123@c.us', reaction: '👍' } }, 'react');
  assert.equal(api.calls.find(call => call.method === 'setMessageReaction').body.message_id, replacement.tg_id);
});

const dates = (api: Fixture) => api.calls.filter(call => call.method === 'sendMessage' && call.body.entities?.some(e => e.type === 'date_time' && e.date_time_format === 'D'));

test('history posts one retained date marker per day with time-only messages and replies across midnight', async t => {
  const { bridge, api, store } = fixture(t);
  const start = Date.parse('2023-11-14T18:29:00Z') / 1000;
  api.rows = [message('a', start, 'First'), message('b', start + 30, 'Second'), message('c', start + 120, 'Third', { quotedMessage: { id: 'b' } }), message('d', start + 150, 'Fourth')];
  await history(bridge, 'master', command('/history 4'), 'days'); await drain(bridge);
  assert.deepEqual(dates(api).map(call => call.body.text), ['14 November 2023', '15 November 2023']);
  assert.deepEqual(content(api).map(call => call.body.text), ['Alice [23:59]: First', 'Alice [23:59]: Second', 'Alice [00:01]: Third', 'Alice [00:01]: Fourth']);
  assert.equal(content(api)[2].body.reply_parameters.message_id, store.byWhatsApp('g@g.us', 'b')[0].tg_id);
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM telegram_messages WHERE kind='chat' AND expires=0").get()!.n, 6);
  assert.equal(store.db.prepare('SELECT count(*) AS n FROM messages').get()!.n, 4);
  assert.ok(content(api).every(call => !call.body.text.includes('History') && !call.body.text.includes('2023')));
});

test('history date markers survive a retry and a fresh worker without duplicate date posts', async t => {
  const { bridge, api, store } = fixture(t);
  api.rows = [message('a', 1700000000), message('b', 1700000030)]; api.failMessage = 1;
  await history(bridge, 'master', command('/history 2'), 'retry-date'); await drain(bridge);
  assert.equal(dates(api).length, 1); assert.equal(store.byWhatsApp('g@g.us', 'a').length, 0);
  const resumed = new Bridge(bridge.c, store, api);
  store.db.prepare("UPDATE jobs SET due=0 WHERE type='history-message' AND state='pending'").run(); await drain(resumed);
  assert.equal(dates(api).length, 1); assert.equal(store.byWhatsApp('g@g.us', 'a').length, 1); assert.equal(store.byWhatsApp('g@g.us', 'b').length, 1);
});

test('skipped and revoked messages do not produce empty date sections', async t => {
  const { bridge, api, store } = fixture(t);
  const existing = message('old', 1700000000); await bridge.whatsApp(event(existing), 'old');
  store.put('revoked', 'g@g.us:revoked', true); api.rows = [existing, message('revoked', 1700086400)];
  await history(bridge, 'master', command('/history 2'), 'skip-days'); await drain(bridge);
  assert.equal(dates(api).length, 0); assert.equal(content(api).length, 0);
  api.rows = [existing, message('fresh', 1700172800)];
  await history(bridge, 'master', command('/history 2'), 'one-new-day'); await drain(bridge);
  assert.equal(dates(api).length, 1); assert.equal(dates(api)[0].body.text, '17 November 2023');
});

test('member DM date markers use its bot and compact imported messages still identify the owner', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('contact', '12025550123@c.us', { name: 'Alice', bot: '42' }); store.put('bot', '42', { token: store.seal('child'), ready: true });
  api.rows = [message('own', 1700000000, 'Mine', { chatId: '12025550123@c.us', isGroup: false, fromMe: true }), message('peer', 1700000030, 'Reply', { chatId: '12025550123@c.us', isGroup: false })];
  await history(bridge, '42', command('/history 2', { chat: { id: 123, type: 'private' } }), 'compact-dm'); await drain(bridge);
  assert.equal(dates(api).length, 1); assert.equal(dates(api)[0].token, 'child');
  assert.deepEqual(content(api).map(call => call.body.text), ['You [03:43]: Mine', '[03:43]: Reply']);
  assert.ok(content(api).every(call => call.token === 'child'));
});

test('older in-progress history runs keep their legacy presentation and do not gain date markers mid-import', async t => {
  const { bridge, api, store } = fixture(t); api.rows = [message('legacy', 1700000000, 'Original')];
  const run = bridge.conversationHistory.request('g@g.us', { chat: '-200' }, 1, 'legacy', false);
  delete run.historyStyle; delete run.timeZone; store.put('history-run', run.id, run);
  await bridge.history({ run: run.id }, 'old-fetch'); await drain(bridge);
  assert.equal(dates(api).length, 0);
  assert.ok(api.calls.some(call => call.body?.text === 'History · 2023-11-14 22:13 UTC · Alice: Original'));
});

test('every history choice deletes only its own prompt and duplicate clicks cannot schedule more deletion', async t => {
  for (const mode of ['skip', 'all', 'cancel']) {
    const { bridge, api, store } = fixture(t), m = command('/history 1');
    api.rows = [message('a', 1700000000)]; await bridge.command('master', m, 'delete-' + mode);
    const choice = store.all('history-choice')[0];
    await choose(bridge, 'master', m, 'delete-' + mode, mode); await choose(bridge, 'master', m, 'delete-' + mode, mode); await drain(bridge);
    const calls = api.calls.filter(call => call.method === 'deleteMessages');
    assert.equal(calls.length, 1); assert.deepEqual(calls[0].body, { chat_id: '-200', message_ids: [choice.promptMessage] });
    assert.equal(store.db.prepare('SELECT deleted FROM telegram_messages WHERE message=?').get(choice.promptMessage)!.deleted, 1);
    assert.equal(content(api).length, mode === 'cancel' ? 0 : 1);
  }
});

test('prompt-deletion network failures retry independently without blocking an import', async t => {
  const { bridge, api, store } = fixture(t), m = command('/history 1');
  api.rows = [message('a', 1700000000)];
  const original = api.tg.bind(api); let fail = true;
  api.tg = async (token, method, body = {}) => { if (method === 'deleteMessages' && fail) { fail = false; throw new ApiError('Timeout', 0, false, true); } return original(token, method, body); };
  await history(bridge, 'master', m, 'retry-delete'); await drain(bridge);
  assert.equal(content(api).length, 1); assert.equal(store.all('history-run')[0].status, 'complete');
  store.db.prepare("UPDATE jobs SET due=0 WHERE dedup LIKE 'history-choice-delete:%' AND state='pending'").run(); await drain(bridge);
  assert.equal(api.calls.filter(call => call.method === 'deleteMessages').length, 1);
});

test('group history uses each present member bot while Bifrost handles absent/unbound authors, own messages and dates', async t => {
  const { bridge, api, store } = fixture(t);
  for (const [person, id, token, name] of [['12025550123@c.us', '42', 'alice', 'Alice'], ['12025550124@c.us', '43', 'bob', 'Bob'], ['12025550125@c.us', '44', 'absent', 'Absent']]) {
    store.put('contact', person, { name, bot: id }); store.put('bot', id, { token: store.seal(token) });
  }
  const original = api.tg.bind(api);
  api.tg = async (token, method, body = {}) => method === 'getChatMember' && body.user_id === 44 ? { status: 'left' } : original(token, method, body);
  api.rows = [message('alice', 1700000000), message('bob', 1700000001, 'Bob', { author: '12025550124@c.us' }), message('absent', 1700000002, 'Absent', { author: '12025550125@c.us' }), message('unbound', 1700000003, 'Unbound', { author: '12025550126@c.us' }), message('own', 1700000004, 'Mine', { fromMe: true })];
  await history(bridge, 'master', command('/history 5'), 'authors'); await drain(bridge);
  assert.deepEqual(content(api).map(call => call.token), ['alice', 'bob', 'master', 'master', 'master']);
  assert.equal(dates(api)[0].token, 'master');
  assert.ok(content(api)[0].body.text.startsWith('[')); assert.ok(content(api)[4].body.text.startsWith('You ['));
});
