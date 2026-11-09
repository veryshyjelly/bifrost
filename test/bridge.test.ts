import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Bridge } from '../src/bridge.ts';
import { Store } from '../src/store.ts';
import { configuration, sign } from '../src/config.ts';
import { Api, ApiError } from '../src/api.ts';

const config = () => ({ ...configuration({}), key: 'ab'.repeat(32), admin: 'secret'.repeat(8), webhookSecret: 'webhook'.repeat(8), owner: '123', management: '-1001', masterToken: 'master-token', session: 's1', openwaKey: 'key', history: 0 });
class Fixture extends Api {
  calls: any[] = []; id = 100; members = new Set<string>(); historyRows: any[] = []; failure?: ApiError;
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
    if (this.failure) { const e = this.failure; this.failure = undefined; throw e; }
    if (method === 'getMe') return { id: 99, username: 'master_bot' };
    if (method === 'getChatMember') return { status: this.members.has(String(body.user_id)) ? 'member' : 'left' };
    if (method === 'createForumTopic') return { message_thread_id: ++this.id };
    if (method === 'getManagedBotToken') return 'child-token';
    if (method === 'getChat') return { type: 'supergroup', is_forum: true };
    return { message_id: ++this.id, chat: { id: body.chat_id }, text: body.text };
  }
  async ow(path: string, body?: any, key?: string) {
    this.calls.push({ method: path, body, key });
    if (path.includes('/history')) return this.historyRows;
    if (this.failure) { const e = this.failure; this.failure = undefined; throw e; }
    return { messageId: 'wa-' + (++this.id) };
  }
  async request(url: string, init: any = {}) { this.calls.push({ url, init }); return { ok: true }; }
  async upload(token: string, method: string, field: string, path: string, name: string, body: any) {
    this.calls.push({ token, method, field, path, name, body }); return { message_id: ++this.id };
  }
}
function fixture(t: any) {
  const c = config(), s = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, s, api);
  t.after(() => s.close()); return { c, s, api, bridge };
}
const message = (id = 'm1', extra: any = {}) => ({ sessionId: 's1', event: 'message.received', idempotencyKey: id, data: { id, chatId: '111@c.us', from: '111@c.us', type: 'text', body: 'Hello', timestamp: 1700000000, fromMe: false, isGroup: false, contact: { pushName: 'Alice' }, ...extra } });
const telegram = (id = 1, extra: any = {}) => ({ update_id: id, message: { message_id: id, from: { id: 123 }, chat: { id: -1001, type: 'supergroup' }, message_thread_id: 101, text: 'Reply', ...extra } });
const missingThread = () => Object.assign(new ApiError('API refused request (400)', 400), { service: 'telegram', reason: 'thread_missing' as const });

test('a group-only contact bot creates its direct route on first DM and sends ordinary and quoted replies', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('contact', '111@c.us', { name: 'Alice', bot: '42' }); s.put('bot', '42', { token: s.seal('child'), ready: true });
  const dm = (id: number, extra = {}) => telegram(id, { chat: { id: 123, type: 'private' }, message_thread_id: undefined, text: 'Hello', ...extra });
  await bridge.telegram('42', dm(1), 'first-dm');
  assert.equal(s.get('route', '111@c.us').mode, 'dedicated');
  assert.equal(api.calls.find(c => c.method === 'messages/send-text').body.chatId, '111@c.us');
  await bridge.whatsApp(message('incoming'), 'incoming');
  const received = s.byWhatsApp('111@c.us', 'incoming')[0]; assert.equal(received.bot, '42');
  await bridge.telegram('42', dm(2, { reply_to_message: { message_id: received.tg_id } }), 'quoted-dm');
  assert.equal(api.calls.find(c => c.method === 'messages/reply').body.quotedMessageId, 'incoming');
  assert.equal(api.calls.filter(c => c.method === 'createForumTopic').length, 1);
});

test('unbound member DMs explain the missing binding instead of silently dropping the message', async t => {
  const { bridge, api, s } = fixture(t); s.put('bot', '42', { token: s.seal('child') });
  await bridge.telegram('42', telegram(1, { chat: { id: 123, type: 'private' }, text: 'Hello' }), 'unbound-dm');
  assert.ok(api.calls.some(c => c.method === 'sendMessage' && c.token === 'child' && /Connect this bot/.test(c.body.text)));
  assert.ok(!api.calls.some(c => c.method.startsWith('messages/')));
});

test('phone-originated private messages stay in their started member-bot DM and can be quoted by incoming replies', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('contact', '111@c.us', { name: 'Alice', bot: '42' }); s.put('bot', '42', { token: s.seal('child'), ready: true });
  s.put('route', '111@c.us', { chatId: '111@c.us', tgChat: '-1001', thread: 77, mode: 'dedicated' });
  await bridge.whatsApp(message('phone', { fromMe: true }), 'phone');
  const own = s.byWhatsApp('111@c.us', 'phone')[0]; assert.equal(own.bot, '42'); assert.equal(own.tg_chat, '123');
  await bridge.whatsApp(message('answer', { quotedMessage: { id: 'phone' } }), 'answer');
  const last = api.calls.filter(c => c.method === 'sendMessage').at(-1);
  assert.equal(last.token, 'child'); assert.equal(last.body.reply_parameters.message_id, own.tg_id);
});

test('a deleted inbox topic is recreated after a definitive pre-delivery refusal and stale quotes are omitted', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('route', '111@c.us', { chatId: '111@c.us', name: 'Alice', tgChat: '-1001', thread: 77, mode: 'inbox' });
  s.map('111@c.us', 'old', '-1001', 5, 'master', 'in');
  bridge.control.track('master', { message_id: 5, chat: { id: -1001, type: 'supergroup' }, is_topic_message: true, message_thread_id: 77 }, 'chat');
  api.failure = missingThread(); bridge.acceptWhatsApp(message('new', { quotedMessage: { id: 'old' } })); await bridge.tick();
  const job = s.db.prepare("SELECT state FROM jobs WHERE type='wa'").get(); assert.equal(job!.state, 'done');
  const sends = api.calls.filter(c => c.method === 'sendMessage'); assert.equal(sends.length, 2);
  assert.notEqual(sends[1].body.message_thread_id, 77); assert.equal(sends[1].body.reply_parameters, undefined);
  assert.equal(s.get('route', '111@c.us').thread, sends[1].body.message_thread_id);
});

test('an old frozen master target can recover into the ready contact DM only before any delivery', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('contact', '111@c.us', { name: 'Alice', bot: '42' }); s.put('bot', '42', { token: s.seal('child'), ready: true });
  s.put('route', '111@c.us', { chatId: '111@c.us', tgChat: '-1001', thread: 77, mode: 'dedicated' });
  s.put('delivery-target', 'legacy-target', { bot: 'master', destination: '-1001', thread: 77, formatVersion: 2 });
  api.failure = missingThread(); await bridge.deliverWhatsApp(message('phone', { fromMe: true }), 'legacy-target');
  const row = s.byWhatsApp('111@c.us', 'phone')[0]; assert.equal(row.bot, '42'); assert.equal(row.tg_chat, '123');
  assert.equal(api.calls.filter(c => c.method === 'createForumTopic').length, 0);
  for (const state of ['done', 'sending', 'uncertain']) {
    const key = 'protected-' + state;
    s.put('delivery-target', key, { bot: 'master', destination: '-1001', thread: 77, formatVersion: 2 });
    s.db.prepare('INSERT INTO effects VALUES(?,?,?)').run(key + ':to:master:-1001:text:0', state, state === 'done' ? '{}' : null);
    assert.equal(await bridge.repairTopic(message('protected-' + state), key, missingThread()), false);
  }
});

test('a rejected media relay moves to the ready member DM without trying document fallback in the missing topic', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('contact', '111@c.us', { name: 'Alice', bot: '42' }); s.put('bot', '42', { token: s.seal('child'), ready: true });
  s.put('route', '111@c.us', { chatId: '111@c.us', tgChat: '-1001', thread: 77, mode: 'dedicated' });
  s.put('delivery-target', 'old-media', { bot: 'master', destination: '-1001', thread: 77, formatVersion: 2 });
  bridge.media.fromWhatsApp = async () => ({ path: '/fixture', name: 'image.jpg' });
  const upload = api.upload.bind(api);
  api.upload = async (token, method, field, path, name, body) => {
    if (body.message_thread_id === 77) { api.calls.push({ token, method, body }); throw missingThread(); }
    return upload(token, method, field, path, name, body);
  };
  await bridge.deliverWhatsApp(message('media', { fromMe: true, type: 'image' }), 'old-media');
  assert.equal(api.calls.filter(c => c.method === 'sendPhoto').length, 2);
  assert.ok(!api.calls.some(c => c.method === 'sendDocument'));
  const row = s.byWhatsApp('111@c.us', 'media')[0]; assert.equal(row.bot, '42'); assert.equal(row.tg_chat, '123');
});

test('a blocked member can fall back even when its management topic was deleted', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('contact', '111@c.us', { name: 'Alice', bot: '42' }); s.put('bot', '42', { token: s.seal('child'), ready: true });
  s.put('route', '111@c.us', { chatId: '111@c.us', tgChat: '-1001', thread: 77, mode: 'dedicated' });
  const original = api.tg.bind(api);
  api.tg = async (token, method, body = {}) => {
    if (method === 'sendMessage' && token === 'child') throw Object.assign(new ApiError('Blocked', 403), { service: 'telegram' });
    if (method === 'sendMessage' && body.message_thread_id === 77) throw missingThread();
    return original(token, method, body);
  };
  bridge.acceptWhatsApp(message()); await bridge.tick();
  assert.equal(s.db.prepare("SELECT state FROM jobs WHERE type='wa'").get()!.state, 'done');
  const copy = s.byWhatsApp('111@c.us', 'm1')[0]; assert.equal(copy.bot, 'master'); assert.equal(copy.tg_chat, '-1001');
  assert.notEqual(s.get('route', '111@c.us').thread, 77);
});

test('edits of Telegram copies explicitly cleared from the archive do not recreate them', async t => {
  const { bridge, api, s } = fixture(t);
  s.map('111@c.us', 'old', '-1001', 5, 'master', 'in');
  bridge.control.track('master', { message_id: 5, chat: { id: -1001, type: 'supergroup' }, is_topic_message: true, message_thread_id: 77 }, 'chat');
  s.db.prepare('UPDATE telegram_messages SET deleted=1 WHERE message=5').run();
  await bridge.waChange({ event: 'message.edited', data: { chatId: '111@c.us', id: 'old', body: 'Changed' } }, 'edit-cleared');
  assert.equal(api.calls.length, 0);
});

test('unknown conversation gets one topic; replay is deduplicated and never marks read', async t => {
  const { bridge, api, s } = fixture(t);
  assert.equal(bridge.acceptWhatsApp(message()), true); assert.equal(bridge.acceptWhatsApp(message()), false);
  await bridge.tick();
  assert.equal(api.calls.filter(c => c.method === 'createForumTopic').length, 1);
  assert.match(api.calls.find(c => c.method === 'sendMessage').body.text, /Alice: Hello/);
  assert.equal(api.calls.some(c => c.method === 'chats/read'), false);
  assert.equal(s.byWhatsApp('111@c.us', 'm1').length, 1);
});

test('dedicated bot wins in a group only when present; master falls back with sender name', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  s.put('bot', '42', { token: s.seal('alice-token') }); s.put('contact', '111@c.us', { name: 'Alice', bot: '42' });
  api.members.add('42');
  await bridge.whatsApp(message('g1', { chatId: 'g@g.us', isGroup: true, author: '111@c.us' }), 'g1');
  assert.equal(api.calls.find(c => c.method === 'sendMessage').token, 'alice-token');
  assert.equal(api.calls.find(c => c.method === 'sendMessage').body.text, 'Hello');
  api.members.clear();
  await bridge.whatsApp(message('g2', { chatId: 'g@g.us', isGroup: true, author: '111@c.us' }), 'g2');
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').at(-1).token, 'master-token');
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').at(-1).body.text, 'Alice: Hello');
});

test('member-bot media, stickers, locations, cards and polls omit redundant sender headers', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  s.put('bot', '42', { token: s.seal('alice-token') }); s.put('contact', '111@c.us', { name: 'Alice', bot: '42' }); api.members.add('42');
  bridge.media.fromWhatsApp = async () => ({ path: '/fixture', name: 'media.jpg' });
  const group = { chatId: 'g@g.us', isGroup: true, author: '111@c.us' };
  await bridge.whatsApp(message('photo', { ...group, type: 'image', body: 'Caption' }), 'photo');
  await bridge.whatsApp(message('sticker', { ...group, type: 'sticker', body: '' }), 'sticker');
  await bridge.whatsApp(message('location', { ...group, type: 'location', body: '', location: { latitude: 1, longitude: 2 } }), 'location');
  await bridge.whatsApp(message('card', { ...group, type: 'contact', body: 'BEGIN:VCARD\nFN:Bob\nTEL:+123\nEND:VCARD' }), 'card');
  assert.equal(api.calls.find(c => c.method === 'sendPhoto').body.caption, 'Caption');
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, 0);
  assert.equal(api.calls.find(c => c.method === 'sendContact').body.first_name, 'Bob');
  await bridge.whatsApp(message('poll', { ...group, type: 'poll', body: '', poll: { name: 'Choose', options: ['Yes', 'No'] } }), 'poll');
  assert.equal(api.calls.find(c => c.method === 'sendMessage').body.text, '📊 Choose\n1. Yes\n2. No');
});

test('member-bot history keeps its history marker and body while phone-originated sends keep owner attribution', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  s.put('bot', '42', { token: s.seal('alice-token') }); s.put('contact', '111@c.us', { name: 'Alice', bot: '42' }); api.members.add('42');
  const group = { chatId: 'g@g.us', isGroup: true, author: '111@c.us' };
  await bridge.whatsApp({ ...message('history', { ...group, body: 'Alice is coming' }), historical: true }, 'history');
  const text = api.calls.find(c => c.method === 'sendMessage').body.text;
  assert.match(text, /^History · [^·]+ · Alice is coming$/);
  assert.ok(api.calls.find(c => c.method === 'sendMessage').body.entities.some((e: any) => e.type === 'date_time' && e.unix_time === 1700000000));
  await bridge.whatsApp(message('phone', { ...group, body: 'From my phone', fromMe: true }), 'phone');
  const phone = api.calls.filter(c => c.method === 'sendMessage').at(-1);
  assert.equal(phone.token, 'master-token'); assert.equal(phone.body.text, 'You · sent from WhatsApp: From my phone');
});

test('member-bot edits do not restore a sender header; master edit fallback remains attributed', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  s.put('bot', '42', { token: s.seal('alice-token') }); s.put('contact', '111@c.us', { name: 'Alice', bot: '42' }); api.members.add('42');
  await bridge.whatsApp(message('edit', { chatId: 'g@g.us', isGroup: true, author: '111@c.us' }), 'initial-edit');
  await bridge.waChange({ event: 'message.edited', data: { chatId: 'g@g.us', id: 'edit', body: 'Updated' } }, 'edited');
  assert.equal(api.calls.find(c => c.method === 'editMessageText').body.text, 'Updated');
  api.failure = new ApiError('Cannot edit', 400);
  await bridge.waChange({ event: 'message.edited', data: { chatId: 'g@g.us', id: 'edit', body: 'Updated again' } }, 'edit-fallback');
  const fallback = api.calls.filter(c => c.method === 'sendMessage').at(-1);
  assert.equal(fallback.token, 'master-token'); assert.equal(fallback.body.text, 'Edited · Alice: Updated again');
});

test('private member-bot messages and edit fallbacks stay nameless in the correct bot DM', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('contact', '111@c.us', { name: 'Alice', bot: '42' }); s.put('bot', '42', { token: s.seal('alice-token'), ready: true });
  s.put('route', '111@c.us', { chatId: '111@c.us', tgChat: '-1001', thread: 101, mode: 'dedicated' });
  await bridge.whatsApp(message(), 'private');
  assert.equal(api.calls.find(c => c.method === 'sendMessage').body.text, 'Hello');
  api.failure = new ApiError('Cannot edit', 400);
  await bridge.waChange({ event: 'message.edited', data: { chatId: '111@c.us', id: 'm1', body: 'Updated' } }, 'private-edit');
  const fallback = api.calls.filter(c => c.method === 'sendMessage').at(-1);
  assert.equal(fallback.token, 'alice-token'); assert.equal(fallback.body.chat_id, '123'); assert.equal(fallback.body.text, 'Edited · Updated');
});

test('Telegram reply maps to WhatsApp quote; send echoes and overlapping bot updates do not loop', async t => {
  const { bridge, api, s } = fixture(t);
  await bridge.whatsApp(message(), 'initial');
  const original = s.byWhatsApp('111@c.us', 'm1')[0];
  const update = telegram(9, { reply_to_message: { message_id: original.tg_id } });
  bridge.acceptTelegram('master', update); await bridge.tick(); await bridge.tick();
  const send = api.calls.find(c => c.method === 'messages/reply');
  assert.equal(send.body.quotedMessageId, 'm1'); assert.ok(send.key);
  const mapped = s.byTelegram('-1001', 9);
  const count = api.calls.filter(c => c.method === 'sendMessage').length;
  await bridge.whatsApp(message(mapped.wa_id, { fromMe: true }), 'echo');
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, count);
  s.put('bot', '42', { token: s.seal('child') });
  assert.equal(bridge.acceptTelegram('42', update), false);
  assert.equal(bridge.acceptTelegram('master', { ...update, update_id: 10, message: { ...update.message, from: { id: 888 } } }), false);
});

test('/create in a linked whole group accepts Telegram reply-thread IDs and selects the replied-to participant', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  s.put('wamessage', 'g@g.us:wa-original', { sender: '111@c.us', name: 'Alice' });
  s.put('contact', '111@c.us', { name: 'Alice' });
  s.map('g@g.us', 'wa-original', '-200', 17, 'master', 'in');
  const update = telegram(18, { text: '/create@master_bot', chat: { id: -200, type: 'supergroup' }, message_thread_id: 17, reply_to_message: { message_id: 17 } });
  assert.equal(bridge.acceptTelegram('master', update), true);
  await bridge.tick();
  const job = s.db.prepare("SELECT payload FROM jobs WHERE type='n8n'").get();
  assert.ok(job, 'The command must reach management instead of emitting unlinked-chat guidance');
  const event = JSON.parse(job.payload as string);
  assert.equal(event.type, 'operator_action');
  assert.equal(event.data.operation, 'create');
  assert.equal(event.data.chatId, 'g@g.us');
  assert.equal(event.data.sender, '111@c.us');
  await bridge.manage(event, 'create-result');
  const prompt = api.calls.find(c => c.method === 'sendMessage' && c.body.reply_markup?.inline_keyboard);
  assert.equal(prompt.body.chat_id, '-200');
  assert.equal(prompt.body.message_thread_id, undefined);
  assert.ok(prompt.body.reply_markup.inline_keyboard[0][0].url.startsWith('https://t.me/newbot/'));
  assert.ok(!api.calls.some(c => c.method?.startsWith('messages/')));
});

test('ordinary replies in a whole group share one queue stream and retain WhatsApp quotes', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  s.map('g@g.us', 'wa-first', '-200', 17, 'master', 'in');
  s.map('g@g.us', 'wa-second', '-200', 25, 'master', 'in');
  for (const [id, target] of [[18, 17], [26, 25]]) {
    bridge.acceptTelegram('master', telegram(id, { text: 'Reply', chat: { id: -200, type: 'supergroup' }, message_thread_id: target, reply_to_message: { message_id: target } }));
  }
  assert.deepEqual(s.db.prepare("SELECT DISTINCT stream FROM jobs WHERE type='tg'").all().map(r => r.stream), ['tg:-200:0']);
  await bridge.tick(); await bridge.tick();
  const sends = api.calls.filter(c => c.method === 'messages/reply');
  assert.deepEqual(sends.map(c => c.body.quotedMessageId), ['wa-first', 'wa-second']);
  assert.ok(sends.every(c => c.body.chatId === 'g@g.us'));
});

test('configured inbox topics still require an exact topic match', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-1001', thread: 101, mode: 'inbox' });
  s.put('wamessage', 'g@g.us:wa-original', { sender: '111@c.us' });
  s.map('g@g.us', 'wa-original', '-1001', 17, 'master', 'in');
  await bridge.telegram('master', telegram(18, { text: '/create', chat: { id: -1001, type: 'supergroup', is_forum: true }, is_topic_message: true, reply_to_message: { message_id: 17 } }), 'right-topic');
  assert.equal(s.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='n8n'").get()!.n, 1);
  await bridge.telegram('master', telegram(19, { text: '/create', chat: { id: -1001, type: 'supergroup', is_forum: true }, is_topic_message: true, message_thread_id: 102, reply_to_message: { message_id: 17 } }), 'wrong-topic');
  assert.equal(s.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='n8n'").get()!.n, 1);
  const guidance = api.calls.find(c => c.method === 'sendMessage');
  assert.match(guidance.body.text, /inside a linked conversation/);
  assert.equal(guidance.body.message_thread_id, 102);
});

test('whole-group reply-thread handling still rejects a quoted message from another WhatsApp conversation', async t => {
  const { bridge, s } = fixture(t);
  s.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  s.map('other@g.us', 'wa-original', '-200', 17, 'master', 'in');
  const m = telegram(18, { text: '/create', chat: { id: -200, type: 'supergroup' }, message_thread_id: 17, reply_to_message: { message_id: 17 } }).message;
  await assert.rejects(bridge.command('master', m, 'wrong-source'), /another WhatsApp conversation/);
  assert.equal(s.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='n8n'").get()!.n, 0);
});

test('uncertain remote send pauses a conversation, while known 429 retries', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('route', '111@c.us', { chatId: '111@c.us', tgChat: '-1001', thread: 101, mode: 'inbox' });
  api.failure = new ApiError('timeout', 0, false, true);
  bridge.acceptTelegram('master', telegram(1)); await bridge.tick();
  assert.equal(s.db.prepare("SELECT state FROM jobs WHERE type='tg'").get()?.state, 'uncertain');
  assert.equal(s.db.prepare("SELECT state FROM effects WHERE id='send:-1001:1'").get()?.state, 'uncertain');
  assert.equal(s.enqueue('later', 'tg:-1001:101', 'tg', { bot: 'master', update: telegram(2) }), true);
  assert.equal(s.next()?.type, 'n8n'); // The failed stream cannot overtake its first send.
});

test('sealed tokens authenticate and are absent from SQLite plaintext', t => {
  const { s } = fixture(t), secret = '123:token-very-secret';
  const encrypted = s.seal(secret); assert.equal(s.unseal(encrypted), secret); assert.ok(!encrypted.includes(secret));
  const corrupt = Buffer.from(encrypted, 'base64'); corrupt[20] ^= 1;
  assert.throws(() => s.unseal(corrupt.toString('base64')));
});

test('management signatures reject forgery and repeated actions are durable', t => {
  const { c, bridge } = fixture(t), body = { id: 'decision1', type: 'operator_action', data: { operation: 'keep' }, at: Date.now() };
  assert.throws(() => bridge.acceptManagement({ ...body, signature: 'forged' }));
  const event = { ...body, signature: sign(JSON.stringify(body), c.admin) };
  assert.equal(bridge.acceptManagement(event), true); assert.equal(bridge.acceptManagement(event), false);
});

test('out-of-order WhatsApp revocation uses revokedId and prevents history resurrection', async t => {
  const { bridge, s } = fixture(t);
  await bridge.waChange({ event: 'message.revoked', data: { id: 'notification', revokedId: 'm1', chatId: '111@c.us' } }, 'revoke');
  await bridge.whatsApp(message(), 'history');
  assert.equal(s.byWhatsApp('111@c.us', 'm1').length, 0);
});

test('phone/LID aliases share a contact bot across conversations', async t => {
  const { bridge, s } = fixture(t);
  await bridge.whatsApp(message('lid1', { chatId: '999@lid', from: '999@lid', senderPhone: '111' }), 'lid');
  assert.equal(s.canonical('999@lid'), '111@c.us');
  assert.equal(s.all('contact').length, 1);
  assert.equal(s.all('route')[0].id, '111@c.us');
});

test('each WhatsApp media class uses native Telegram methods; view-once content exposed by OpenWA is archived', async t => {
  const { bridge, api, s } = fixture(t);
  bridge.media.fromWhatsApp = async () => ({ path: '/fixture', name: 'attachment' });
  for (const [kind, method] of Object.entries({ image: 'sendPhoto', video: 'sendVideo', audio: 'sendAudio', voice: 'sendVoice', document: 'sendDocument', sticker: 'sendSticker' })) {
    await bridge.whatsApp(message(kind, { type: kind, media: { mimetype: kind === 'voice' ? 'audio/ogg' : undefined } }), kind);
    assert.ok(api.calls.some(c => c.method === method));
    assert.ok(s.byWhatsApp('111@c.us', kind).some(m => m.media === 1));
  }
});

test('missing media is visible, and location/contact/poll use structured representations', async t => {
  const { bridge, api } = fixture(t);
  bridge.media.fromWhatsApp = async () => { throw new ApiError('No stored media', 404); };
  await bridge.whatsApp(message('missing', { type: 'image' }), 'missing');
  assert.match(api.calls.filter(c => c.method === 'sendMessage').at(-1).body.text, /image unavailable/);
  await bridge.whatsApp(message('location', { type: 'location', location: { latitude: 1, longitude: 2 } }), 'location');
  await bridge.whatsApp(message('contact', { type: 'contact', body: 'BEGIN:VCARD\nFN:Bob\nTEL:+1234567\nEND:VCARD' }), 'contact');
  await bridge.whatsApp(message('poll', { type: 'poll', poll: { name: 'Lunch?', options: ['Yes', 'No'] } }), 'poll');
  assert.ok(api.calls.some(c => c.method === 'sendLocation'));
  assert.ok(api.calls.some(c => c.method === 'sendContact'));
  assert.ok(api.calls.some(c => c.body?.reply_markup?.inline_keyboard?.[0]?.[0]?.callback_data?.startsWith('vote:')));
});

test('dedicated private bot waits for Start, then routes private replies to the contact', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('contact', '111@c.us', { name: 'Alice', bot: '42' });
  s.put('bot', '42', { token: s.seal('alice-token'), ready: false });
  await bridge.whatsApp(message(), 'before');
  assert.equal(api.calls.find(c => c.method === 'sendMessage').token, 'master-token');
  await bridge.telegram('42', telegram(11, { text: '/start', chat: { id: 123, type: 'private' }, message_thread_id: undefined }), 'start');
  await bridge.whatsApp(message('m2'), 'after');
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').at(-1).token, 'alice-token');
  await bridge.telegram('42', telegram(12, { text: 'Hello', chat: { id: 123, type: 'private' }, message_thread_id: undefined }), 'reply');
  assert.equal(api.calls.find(c => c.method === 'messages/send-text').body.chatId, '111@c.us');
});

test('muted conversations can resume; creating a bot at capacity leaves master routing intact', async t => {
  const { bridge, api, s } = fixture(t);
  await bridge.whatsApp(message(), 'initial');
  await bridge.manage({ type: 'operator_action', data: { operation: 'mute', chatId: '111@c.us' } }, 'mute');
  const count = api.calls.filter(c => c.method === 'sendMessage').length;
  await bridge.whatsApp(message('m2'), 'muted');
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, count);
  await bridge.manage({ type: 'operator_action', data: { operation: 'keep', chatId: '111@c.us' } }, 'keep');
  await bridge.manage({ type: 'operator_action', data: { operation: 'create', chatId: '111@c.us' } }, 'create');
  assert.equal(s.get('route', '111@c.us').mode, 'inbox');
  assert.ok(api.calls.filter(c => c.method === 'sendMessage').at(-1).body.reply_markup.inline_keyboard[0][0].url.startsWith('https://t.me/newbot/master_bot/'));
});

test('429 is retried with the same WhatsApp idempotency key', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('route', '111@c.us', { chatId: '111@c.us', tgChat: '-1001', thread: 101, mode: 'inbox' });
  api.failure = new ApiError('Rate limited', 429, true, false, 1);
  bridge.acceptTelegram('master', telegram(3)); await bridge.tick();
  assert.equal(s.db.prepare("SELECT state FROM jobs WHERE type='tg'").get()?.state, 'pending');
  s.db.exec('UPDATE jobs SET due=0'); await bridge.tick();
  const sends = api.calls.filter(c => c.method === 'messages/send-text');
  assert.equal(sends.length, 2); assert.equal(sends[0].key, sends[1].key);
  assert.equal(s.byTelegram('-1001', 3).direction, 'out');
});

test('child-bot group callbacks remain actionable while its ordinary group updates are ignored', t => {
  const { bridge, s } = fixture(t); s.put('bot', '42', { token: s.seal('token') });
  assert.equal(bridge.acceptTelegram('42', telegram()), false);
  assert.equal(bridge.acceptTelegram('42', { update_id: 20, callback_query: { id: 'q', from: { id: 123 }, data: 'vote:x', message: telegram().message } }), true);
});

test('private message ids are isolated between contact bots', async t => {
  const { bridge, api, s } = fixture(t);
  for (const [bot, phone] of [['42', '111'], ['43', '222']]) {
    s.put('bot', bot, { token: s.seal(bot + '-token'), ready: true });
    s.put('contact', phone + '@c.us', { name: phone, bot });
    s.put('route', phone + '@c.us', { chatId: phone + '@c.us', tgChat: '-1001', thread: Number(bot), mode: 'dedicated' });
    await bridge.telegram(bot, telegram(1, { chat: { id: 123, type: 'private' }, message_thread_id: undefined }), 'private-' + bot);
  }
  assert.equal(s.byTelegram('123', 1, '42').wa_chat, '111@c.us');
  assert.equal(s.byTelegram('123', 1, '43').wa_chat, '222@c.us');
  const sends = api.calls.filter(c => c.method === 'messages/send-text');
  assert.equal(sends.length, 2); assert.notEqual(sends[0].key, sends[1].key);
});

test('a partial sticker delivery retries the attachment without losing it or duplicating the label', async t => {
  const { bridge, api, s } = fixture(t);
  bridge.media.fromWhatsApp = async () => ({ path: '/fixture', name: 'sticker.webp' });
  const upload = api.upload.bind(api); let count = 0;
  api.upload = async (...args: Parameters<typeof api.upload>) => {
    if (++count === 1) throw new ApiError('Rate limited', 429, true);
    return upload(...args);
  };
  await assert.rejects(bridge.whatsApp(message('sticker-partial', { type: 'sticker' }), 'partial'));
  assert.equal(s.byWhatsApp('111@c.us', 'sticker-partial').length, 1);
  await bridge.whatsApp(message('sticker-partial', { type: 'sticker' }), 'partial');
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, 1);
  assert.equal(api.calls.filter(c => c.method === 'sendSticker').length, 1);
  assert.equal(s.byWhatsApp('111@c.us', 'sticker-partial').length, 2);
});

test('blocked dedicated bot falls back to its management topic on a definitive Telegram refusal', async t => {
  const { bridge, api, s } = fixture(t);
  s.put('contact', '111@c.us', { name: 'Alice', bot: '42' });
  s.put('bot', '42', { token: s.seal('child'), ready: true });
  s.put('route', '111@c.us', { chatId: '111@c.us', tgChat: '-1001', thread: 101, mode: 'dedicated' });
  const refusal = new ApiError('Bot blocked', 403); refusal.service = 'telegram'; api.failure = refusal;
  bridge.acceptWhatsApp(message()); await bridge.tick();
  assert.equal(s.db.prepare("SELECT state FROM jobs WHERE type='wa'").get()?.state, 'done');
  const sends = api.calls.filter(c => c.method === 'sendMessage');
  assert.equal(sends[0].token, 'child'); assert.equal(sends[1].token, 'master-token');
  assert.equal(s.byWhatsApp('111@c.us', 'm1')[0].tg_chat, '-1001');
});

test('completed external effects retain their original destination when routing changes during a retry', async t => {
  const { bridge, api, s } = fixture(t);
  bridge.media.fromWhatsApp = async () => ({ path: '/fixture', name: 'sticker.webp' });
  const upload = api.upload.bind(api); let count = 0;
  api.upload = async (...args: Parameters<typeof api.upload>) => { if (++count === 1) throw new ApiError('429', 429, true); return upload(...args); };
  await assert.rejects(bridge.whatsApp(message('fixed-target', { type: 'sticker' }), 'fixed-target'));
  const route = s.get('route', '111@c.us'); s.put('route', '111@c.us', { ...route, tgChat: '-9999' });
  await bridge.whatsApp(message('fixed-target', { type: 'sticker' }), 'fixed-target');
  assert.equal(api.calls.find(c => c.method === 'sendSticker').body.chat_id, '-1001');
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, 1);
});

test('history is bounded and enqueued with historical markers', async t => {
  const { bridge, api, s } = fixture(t); bridge.c.history = 100;
  s.put('route', '111@c.us', { chatId: '111@c.us', mode: 'inbox', tgChat: '-1001', thread: 101 });
  api.historyRows = [message('old').data];
  await bridge.history({ chatId: '111@c.us' }, 'history'); await bridge.tick();
  assert.match(api.calls.find(c => c.method === 'sendMessage').body.text, /History/);
  assert.match(api.calls.find(c => c.method?.includes('/history')).method, /limit=100/);
});

test('opening a second store does not reset a live process; explicit recovery preserves uncertain sends', async () => {
  const directory = await mkdtemp(join(tmpdir(), 'bridge-'));
  try {
    const path = join(directory, 'db'), key = 'ab'.repeat(32), first = new Store(path, key);
    first.db.prepare("INSERT INTO effects VALUES('send','sending',NULL)").run();
    const inspector = new Store(path, key);
    assert.equal(inspector.db.prepare("SELECT state FROM effects WHERE id='send'").get()?.state, 'sending');
    inspector.close(); first.close();
    const recovered = new Store(path, key, true);
    assert.equal(recovered.db.prepare("SELECT state FROM effects WHERE id='send'").get()?.state, 'uncertain'); recovered.close();
  } finally { await rm(directory, { recursive: true, force: true }); }
});
