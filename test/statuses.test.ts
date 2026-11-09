import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Bridge } from '../src/bridge.ts';
import { Store } from '../src/store.ts';
import { Api, ApiError } from '../src/api.ts';
import { configuration } from '../src/config.ts';
import { ensureOpenWAWebhook, waEvents } from '../src/openwa.ts';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const contact = { id: '12025550123@c.us', name: 'Abhishek' };
const story = (id = 'story', extra = {}) => ({ sessionId: 's1', event: 'status.received', idempotencyKey: id, data: {
  statusId: id, contact, type: 'text', caption: 'Hi <bro> & 😀', postedAt: Date.now() - 1000, expiresAt: Date.now() + 3600000, ...extra,
} });
class Fixture extends Api {
  calls: any[] = []; seq = 100; rows: any[] = []; hooks: any[] = []; failSend = false; failAck = false;
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
    if ((method === 'sendMessage' && this.failSend) || (method === 'answerCallbackQuery' && this.failAck)) {
      this.failSend = this.failAck = false; throw new ApiError('Rate limited', 429, true);
    }
    if (method === 'getMe') return { id: 99, username: 'hermes_bot' };
    if (method === 'createForumTopic') return { message_thread_id: ++this.seq };
    return { message_id: ++this.seq, chat: { id: body.chat_id }, message_thread_id: body.message_thread_id, text: body.text, entities: body.entities };
  }
  async ow(path: string, body?: any, key?: string, method = 'POST') {
    this.calls.push({ path, body, key, method: body === undefined ? 'GET' : method });
    if (path === 'status') return { statuses: this.rows };
    if (path === 'webhooks' && !body) return this.hooks;
    if (path.startsWith('webhooks/') && method === 'PUT') {
      this.hooks.find(h => h.id === decodeURIComponent(path.slice('webhooks/'.length))).events = body.events;
      return {};
    }
    if (path === 'webhooks' && body) { this.hooks.push({ ...body, id: 'new' }); return {}; }
    throw new Error('Status feeds must not call WhatsApp messaging or read-receipt APIs');
  }
  async upload(token: string, method: string, field: string, path: string, name: string, body: any) {
    this.calls.push({ token, method, field, path, name, body });
    return { message_id: ++this.seq, chat: { id: body.chat_id }, message_thread_id: body.message_thread_id };
  }
}
function fixture(t: any) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), masterToken: 'master', owner: '123', management: '-1001234', session: 's1', history: 0 };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  t.after(() => store.close()); return { c, store, api, bridge };
}
async function drain(bridge: Bridge) { for (let i = 0; i < 30; i++) await bridge.tick(); }
function replyToStatus(bridge: Bridge, id = 1, text = '/mute_status', statusId = 'story') {
  const feed = bridge.store.all('status-feed').find(feed => feed.tgChat === bridge.c.management)!;
  const original = bridge.store.byWhatsApp('status@broadcast', statusId)[0];
  return { message_id: id, from: { id: Number(bridge.c.owner) }, chat: { id: Number(bridge.c.management), type: 'supergroup' },
    message_thread_id: feed.thread, is_topic_message: true, text, reply_to_message: { message_id: original.tg_id } };
}
function callbackFor(bridge: Bridge, body: any, data: string, id = 'callback') {
  return { id, from: { id: Number(bridge.c.owner) }, data, message: { message_id: 900, chat: { id: body.chat_id, type: String(body.chat_id).startsWith('-') ? 'supergroup' : 'private' },
    ...(body.message_thread_id ? { message_thread_id: body.message_thread_id, is_topic_message: true } : {}), reply_markup: body.reply_markup } };
}

test('Status webhooks share one archive topic with linked authors and native times, without new conversations', async t => {
  const { bridge, store, api } = fixture(t);
  store.put('bot', '42', { token: store.seal('member') }); store.put('contact', contact.id, { name: contact.name, bot: '42' });
  assert.equal(bridge.acceptWhatsApp(story()), true); assert.equal(bridge.acceptWhatsApp(story()), false);
  await bridge.tick();
  await bridge.whatsApp(story('second', { contact: { id: 'opaque@lid', pushName: 'Bob' }, caption: 'Hello' }), 'second');
  const calls = api.calls.filter(c => c.method === 'sendMessage');
  assert.equal(api.calls.filter(c => c.method === 'createForumTopic').length, 1);
  assert.ok(calls.every(c => c.token === 'master' && c.body.message_thread_id === 101));
  assert.match(calls[0].body.text, /^Status · .* · Abhishek: Hi <bro> & 😀$/);
  assert.ok(calls[0].body.entities.some((e: any) => e.type === 'date_time' && e.date_time_format === 'dt'));
  assert.ok(calls[0].body.entities.some((e: any) => e.type === 'text_link' && e.url === 'https://wa.me/12025550123'));
  assert.ok(calls[1].body.entities.some((e: any) => e.type === 'text_link' && e.url === 'https://wa.me/'));
  assert.equal(store.all('route').length, 0); assert.equal(store.all('bot').length, 1);
  assert.equal(store.byWhatsApp('status@broadcast', 'story').length, 1);
  assert.ok(store.db.prepare("SELECT kind,expires FROM telegram_messages").all().every(row => row.kind === 'chat' && row.expires === 0));
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='management'").get()!.n, 0);
});

test('startup upgrades only bridge callback events, imports available stored Status posts and deduplicates webhook races', async t => {
  const { bridge, store, api, c } = fixture(t);
  api.hooks = [{ id: 'own', url: c.callback + '/webhooks/openwa', events: ['call.received', 'message.received'], secret: 'unchanged', filters: { conditions: [] } },
    { id: 'other', url: 'https://unrelated.example', events: ['message.received'] }];
  api.rows = [{ id: 'existing', contact, type: 'text', caption: 'Existing', timestamp: new Date(Date.now() - 2000).toISOString(), expiresAt: new Date(Date.now() + 3600000).toISOString() }];
  store.put('migrated', 'master', true); bridge.statuses.startup(); await bridge.tick();
  const update = api.calls.find(c => c.method === 'PUT');
  assert.equal(update.path, 'webhooks/own'); assert.deepEqual(Object.keys(update.body), ['events']);
  assert.ok(update.body.events.includes('status.received')); assert.ok(update.body.events.includes('call.received'));
  assert.equal(api.hooks[0].secret, 'unchanged'); assert.deepEqual(api.hooks[1].events, ['message.received']);
  bridge.acceptWhatsApp(story('existing')); await drain(bridge);
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, 1);
  assert.ok(api.calls.find(c => c.method === 'sendMessage').body.text.endsWith('Abhishek: Existing'));
  bridge.statuses.startup(); await drain(bridge);
  assert.equal(api.calls.filter(c => c.method === 'createForumTopic').length, 1);
  assert.equal(api.calls.filter(c => c.method === 'PUT').length, 1);
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, 1);
});

test('a fresh or wildcard webhook installation remains idempotent', async t => {
  const { bridge, api, c } = fixture(t);
  await ensureOpenWAWebhook(c, api); await ensureOpenWAWebhook(c, api);
  assert.equal(api.calls.filter(c => c.path === 'webhooks' && c.method === 'POST').length, 1);
  assert.deepEqual(api.hooks[0].events, waEvents);
  api.hooks[0].events = ['*']; await ensureOpenWAWebhook(c, api);
  assert.equal(api.calls.filter(c => c.method === 'PUT').length, 0);
  assert.equal(bridge.store.all('status-feed').length, 0);
});

test('image, video and voice Status posts use native media with attributed captions in the feed', async t => {
  const { bridge, api, store } = fixture(t);
  bridge.media.fromWhatsApp = async m => { assert.equal(m.statusId, m.id); return { path: '/fixture', name: 'story.bin' }; };
  bridge.media.convert = async () => '/converted.ogg';
  for (const type of ['image', 'video', 'voice']) await bridge.whatsApp(story(type, { type, hasMedia: true, caption: 'Caption' }), type);
  for (const method of ['sendPhoto', 'sendVideo', 'sendVoice']) {
    const call = api.calls.find(c => c.method === method);
    assert.equal(call.token, 'master'); assert.equal(call.body.message_thread_id, 101);
    assert.match(call.body.caption, /^Status · .* · Abhishek: Caption$/);
    assert.ok(call.body.caption_entities.some((e: any) => e.type === 'date_time'));
    assert.ok(call.body.caption_entities.some((e: any) => e.type === 'text_link'));
  }
  assert.equal(store.db.prepare('SELECT count(*) AS n FROM messages WHERE media=1').get()!.n, 3);
});

test('Status media uses the authenticated local status endpoint and never an advertised media URL', async t => {
  const { bridge, c } = fixture(t), requests: any[] = [];
  bridge.media.download = async (url, headers, name) => { requests.push({ url, headers, name }); return '/fixture'; };
  const result = await bridge.media.fromWhatsApp({ statusId: 'abc/def?', id: 'wrong', chatId: 'status@broadcast', type: 'image', mediaUrl: 'https://untrusted.example' });
  assert.equal(result.path, '/fixture'); assert.equal(requests.length, 1);
  assert.equal(requests[0].url, c.openwa + '/api/sessions/s1/status/abc%2Fdef%3F/media');
  assert.equal(requests[0].headers['X-API-Key'], c.openwaKey);
});

test('omitted or expired media keeps its caption and a readable unavailable notice', async t => {
  const { bridge, api } = fixture(t);
  bridge.media.download = async () => { throw new Error('Omitted status media must not be downloaded'); };
  await bridge.whatsApp(story('omitted', { type: 'image', hasMedia: false, mediaOmitted: true, caption: 'Keep me' }), 'omitted');
  assert.match(api.calls.find(c => c.method === 'sendMessage').body.text, /Abhishek: Keep me\n\[image unavailable/);
  assert.ok(!api.calls.some(c => c.method === 'sendPhoto'));
  bridge.media.fromWhatsApp = async () => { throw new ApiError('Attachment unavailable', 404); };
  await bridge.whatsApp(story('gone', { type: 'video', hasMedia: true }), 'gone');
  assert.match(api.calls.filter(c => c.method === 'sendMessage').at(-1).body.text, /video unavailable/);
});

test('expired and malformed incoming stories are skipped while already archived posts remain', async t => {
  const { bridge, api, store } = fixture(t);
  await bridge.whatsApp(story('archived'), 'archived');
  for (const extra of [{ expiresAt: Date.now() - 1 }, { postedAt: 'invalid' }, { contact: { id: 'status@broadcast' } }, { statusId: '' }]) {
    await bridge.whatsApp(story('invalid', extra), 'invalid');
  }
  await bridge.whatsApp({ event: 'message.received', data: { id: 'raw', chatId: 'status@broadcast', isStatusBroadcast: true } }, 'raw');
  await bridge.statuses.process({ type: 'status-refresh' });
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, 1);
  assert.equal(store.byWhatsApp('status@broadcast', 'archived').length, 1);
  assert.ok(!api.calls.some(c => /deleteMessage/.test(c.method || '')));
});

test('typing, edits, message actions and reactions in the Status topic cannot publish or send WhatsApp messages', async t => {
  const { bridge, store, api, c } = fixture(t);
  await bridge.whatsApp(story(), 'story');
  const feed = store.all('status-feed')[0], original = store.byWhatsApp('status@broadcast', 'story')[0];
  const message = (id: number, text: string) => ({ message_id: id, from: { id: 123 }, chat: { id: Number(c.management), type: 'supergroup' }, message_thread_id: feed.thread, is_topic_message: true, text,
    reply_to_message: { message_id: original.tg_id } });
  await bridge.telegram('master', { message: message(1, 'Reply') }, 'reply');
  await bridge.telegram('master', { edited_message: message(1, 'Changed') }, 'edit');
  for (const [i, command] of ['/delete', '/read', '/react 👍', '/create', '/sync', '/group', '/history', '/keep'].entries()) {
    await bridge.command('master', message(i + 10, command), 'command-' + i);
  }
  await bridge.telegram('master', { message_reaction: { chat: { id: c.management }, message_id: original.tg_id, new_reaction: [{ type: 'emoji', emoji: '👍' }] } }, 'reaction');
  assert.equal(api.calls.filter(c => c.path).length, 0);
  assert.ok(api.calls.filter(c => c.method === 'sendMessage').slice(1).every(c => /Status/.test(c.body.text) && c.body.message_thread_id === feed.thread));
});

test('/statuses opens and refreshes the feed from Bifrost DM; /status remains the health command', async t => {
  const { bridge, store, api } = fixture(t);
  const message = { message_id: 1, from: { id: 123 }, chat: { id: 123, type: 'private' }, text: '/statuses' };
  assert.equal(bridge.acceptTelegram('master', { update_id: 1, message: { ...message, from: { id: 999 } } }), false);
  await bridge.command('master', message, 'open-feed');
  const button = api.calls.find(c => c.method === 'sendMessage').body.reply_markup.inline_keyboard[0][0];
  assert.equal(button.url, 'https://t.me/c/1234/101');
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='status-refresh'").get()!.n, 1);
  await bridge.command('master', { ...message, message_id: 2, text: '/status' }, 'health');
  assert.equal(JSON.parse(api.calls.filter(c => c.method === 'sendMessage').at(-1).body.text).configured, false);
  await bridge.control.install('master');
  assert.ok(api.calls.filter(c => c.method === 'setMyCommands').every(c => c.body.commands.some((command: any) => command.command === 'statuses')));
  for (const name of ['mute_status', 'unmute_status', 'muted_statuses']) assert.ok(api.calls.filter(c => c.method === 'setMyCommands').every(c => c.body.commands.some((command: any) => command.command === name)));
});

test('replying /mute in Status suppresses only that author before media download, preserving chats and archived posts', async t => {
  const { bridge, store, api, c } = fixture(t);
  await bridge.whatsApp(story(), 'initial');
  await bridge.command('master', replyToStatus(bridge, 1, '/mute'), 'mute');
  assert.equal(bridge.statuses.isMuted(contact.id), true);
  bridge.media.fromWhatsApp = async () => { throw new Error('Muted Status media must not be fetched'); };
  bridge.acceptWhatsApp(story('hidden', { type: 'image', hasMedia: true })); await bridge.tick();
  await bridge.whatsApp(story('other', { contact: { id: '12025550124@c.us', name: 'Bob' }, caption: 'Other story' }), 'other');
  store.put('route', contact.id, { chatId: contact.id, tgChat: '-200', mode: 'inbox' });
  await bridge.whatsApp({ event: 'message.received', data: { id: 'chat', chatId: contact.id, from: contact.id, type: 'text', body: 'Ordinary chat', timestamp: Math.floor(Date.now() / 1000) } }, 'chat');
  assert.equal(store.get('route', contact.id).mode, 'inbox');
  assert.equal(store.byWhatsApp('status@broadcast', 'story').length, 1);
  assert.equal(store.byWhatsApp('status@broadcast', 'hidden').length, 0);
  assert.equal(store.byWhatsApp('status@broadcast', 'other').length, 1);
  assert.equal(store.byWhatsApp(contact.id, 'chat').length, 1);
  assert.ok(!api.calls.some(call => call.path || call.method === 'deleteMessages'));
  assert.ok(api.calls.some(call => call.method === 'sendMessage' && call.body.chat_id === '-200' && call.body.text.endsWith('Ordinary chat')));
});

test('refresh honors mutes and unmuting resumes new posts without replaying stories skipped while muted', async t => {
  const { bridge, store, api } = fixture(t);
  await bridge.whatsApp(story(), 'initial');
  await bridge.command('master', replyToStatus(bridge), 'mute');
  api.rows = [{ id: 'muted-snapshot', contact, type: 'text', caption: 'Suppressed', timestamp: new Date().toISOString(), expiresAt: new Date(Date.now() + 3600000).toISOString() }];
  await bridge.statuses.process({ type: 'status-refresh' }); await drain(bridge);
  assert.equal(store.byWhatsApp('status@broadcast', 'muted-snapshot').length, 0);
  await bridge.command('master', replyToStatus(bridge, 2, '/unmute_status'), 'unmute');
  assert.equal(bridge.statuses.isMuted(contact.id), false);
  await bridge.statuses.process({ type: 'status-refresh' }); await drain(bridge);
  await bridge.whatsApp(story('muted-snapshot'), 'snapshot-webhook');
  assert.equal(store.byWhatsApp('status@broadcast', 'muted-snapshot').length, 0);
  await bridge.whatsApp(story('new-after-unmute'), 'new');
  assert.equal(store.byWhatsApp('status@broadcast', 'new-after-unmute').length, 1);
});

test('muted-person preferences survive reopening SQLite and resolve aliases with the latest preference winning', t => {
  const root = mkdtempSync(join(tmpdir(), 'status-mutes-')), path = join(root, 'bridge.db');
  const c = { ...configuration({}), key: 'aa'.repeat(32), owner: '123', session: 's1' };
  const first = new Store(path, c.key), original = new Bridge(c, first, new Fixture(c));
  original.statuses.setMuted('opaque@lid', true, 'lid-mute');
  original.statuses.setMuted(contact.id, true, 'phone-mute');
  original.statuses.setMuted(contact.id, false, 'phone-unmute'); first.close();
  const store = new Store(path, c.key), bridge = new Bridge(c, store, new Fixture(c));
  t.after(() => { store.close(); rmSync(root, { recursive: true, force: true }); });
  assert.equal(bridge.statuses.isMuted('opaque@lid'), true);
  store.alias('opaque@lid', contact.id);
  assert.equal(bridge.statuses.isMuted('opaque@lid'), false); assert.equal(bridge.statuses.muted().length, 0);
  bridge.statuses.setMuted('opaque@lid', true, 'mute-again'); assert.equal(bridge.statuses.isMuted(contact.id), true);
  bridge.c.session = 'different'; assert.equal(bridge.statuses.isMuted(contact.id), false);
  bridge.c.session = 's1'; assert.equal(bridge.statuses.isMuted(contact.id), true);
});

test('muted list supports one-tap unmute and pagination without changing other people’s preferences', async t => {
  const { bridge, store, api } = fixture(t);
  for (let i = 0; i < 11; i++) {
    const sender = '1202555' + String(i).padStart(4, '0'); store.put('contact', sender + '@c.us', { name: 'Person ' + String(i).padStart(2, '0') });
    bridge.statuses.setMuted(sender + '@c.us', true, 'mute-' + i);
  }
  const dm = { message_id: 1, from: { id: 123 }, chat: { id: 123, type: 'private' }, text: '/muted_statuses' };
  await bridge.command('master', dm, 'list');
  const body = api.calls.filter(call => call.method === 'sendMessage').at(-1).body;
  assert.match(body.text, /page 1\/2/);
  const next = body.reply_markup.inline_keyboard.at(-1)[0]; assert.equal(next.text, 'Next');
  await bridge.telegram('master', { callback_query: callbackFor(bridge, body, next.callback_data, 'next') }, 'next-page');
  const page = api.calls.filter(call => call.method === 'editMessageText').at(-1).body;
  assert.match(page.text, /page 2\/2/);
  const unmute = page.reply_markup.inline_keyboard[0][0]; assert.match(unmute.text, /Unmute Person 10/);
  await bridge.telegram('master', { callback_query: callbackFor(bridge, page, unmute.callback_data, 'unmute') }, 'unmute-person');
  assert.equal(bridge.statuses.muted().length, 10); assert.equal(bridge.statuses.isMuted('12025550010@c.us'), false);
  assert.ok(bridge.statuses.isMuted('12025550000@c.us'));
  assert.match(api.calls.filter(call => call.method === 'editMessageText').at(-1).body.text, /10 people, page 1\/1/);
});

test('mute controls enforce owner, bot, session, chat, topic and expiry; stale buttons cannot undo a later mute', async t => {
  const { bridge, store, api } = fixture(t);
  await bridge.whatsApp(story(), 'initial'); await bridge.command('master', replyToStatus(bridge), 'mute');
  const body = api.calls.filter(call => call.method === 'sendMessage').at(-1).body, data = body.reply_markup.inline_keyboard[0][0].callback_data;
  const query = callbackFor(bridge, body, data), actionId = data.slice('statusctl:'.length), action = store.get('action', actionId);
  assert.equal(bridge.acceptTelegram('master', { update_id: 1, callback_query: { ...query, from: { id: 999 } } }), false);
  for (const changed of [
    { ...query, from: { id: 999 } },
    { ...query, message: { ...query.message, chat: { id: -100999, type: 'supergroup' } } },
    { ...query, message: { ...query.message, message_thread_id: query.message.message_thread_id + 1 } },
    { ...query, data: 'statusctl:unknown' },
  ]) {
    await bridge.statuses.callback('master', changed, 'invalid'); assert.equal(bridge.statuses.isMuted(contact.id), true);
  }
  for (const changed of [{ ...action, session: 'another' }, { ...action, expires: Date.now() - 1 }]) {
    store.put('action', actionId, changed); await bridge.statuses.callback('master', query, 'invalid'); assert.equal(bridge.statuses.isMuted(contact.id), true);
  }
  store.put('action', actionId, action); store.put('bot', '42', { token: store.seal('child') });
  await bridge.statuses.callback('42', query, 'wrong-bot'); assert.equal(bridge.statuses.isMuted(contact.id), true);
  bridge.statuses.setMuted(contact.id, false, 'off'); bridge.statuses.setMuted(contact.id, true, 'on');
  await bridge.statuses.callback('master', query, 'stale'); assert.equal(bridge.statuses.isMuted(contact.id), true);
  assert.match(api.calls.filter(call => call.method === 'answerCallbackQuery').at(-1).body.text, /changed/);
});

test('retrying a mute confirmation or unmute callback cannot reverse a newer decision from another chat', async t => {
  const { bridge, api } = fixture(t);
  await bridge.whatsApp(story(), 'initial'); api.failSend = true;
  await assert.rejects(bridge.command('master', replyToStatus(bridge), 'mute-retry'));
  bridge.statuses.setMuted(contact.id, false, 'unmute-elsewhere');
  await bridge.command('master', replyToStatus(bridge), 'mute-retry'); assert.equal(bridge.statuses.isMuted(contact.id), false);
  await bridge.command('master', replyToStatus(bridge, 2), 'mute-new');
  const body = api.calls.filter(call => call.method === 'sendMessage').at(-1).body, query = callbackFor(bridge, body, body.reply_markup.inline_keyboard[0][0].callback_data);
  api.failAck = true;
  await assert.rejects(bridge.statuses.callback('master', query, 'unmute-retry'), (error: any) => error.retry && !error.uncertain);
  assert.equal(bridge.statuses.isMuted(contact.id), false);
  bridge.statuses.setMuted(contact.id, true, 'remute-elsewhere');
  await bridge.statuses.callback('master', query, 'unmute-retry'); assert.equal(bridge.statuses.isMuted(contact.id), true);
});

test('Status mute commands require a mapped Status reply and ordinary conversation mute keeps its existing behavior', async t => {
  const { bridge, store, api } = fixture(t);
  const dm = { message_id: 1, from: { id: 123 }, chat: { id: 123, type: 'private' }, text: '/mute_status' };
  await bridge.command('master', dm, 'missing'); assert.equal(bridge.statuses.muted().length, 0);
  assert.match(api.calls.filter(call => call.method === 'sendMessage').at(-1).body.text, /Reply to a post in WhatsApp Status/);
  store.put('route', contact.id, { chatId: contact.id, tgChat: '-200', mode: 'inbox' });
  await bridge.command('master', { ...dm, message_id: 2, chat: { id: -200, type: 'supergroup' }, text: '/mute' }, 'chat-mute');
  const action = JSON.parse(store.db.prepare("SELECT payload FROM jobs WHERE type='management'").get()!.payload as string);
  assert.equal(action.data.operation, 'mute'); assert.equal(action.data.chatId, contact.id);
  assert.equal(bridge.statuses.muted().length, 0);
});

test('Status topic clear log preserves posts, while clear all removes only this topic’s recorded posts', async t => {
  const { bridge, api, store, c } = fixture(t);
  await bridge.whatsApp(story(), 'story'); const feed = store.all('status-feed')[0];
  const message = { message_id: 1, from: { id: 123 }, chat: { id: Number(c.management), type: 'supergroup' }, message_thread_id: feed.thread, is_topic_message: true, text: '/clear log' };
  bridge.control.track('master', { ...message, message_id: 9000, text: 'Other topic', message_thread_id: feed.thread + 100 }, 'chat');
  await bridge.command('master', message, 'log'); await drain(bridge);
  const original = store.byWhatsApp('status@broadcast', 'story')[0];
  assert.ok(!api.calls.filter(c => c.method === 'deleteMessages').some(c => c.body.message_ids.includes(original.tg_id)));
  await bridge.command('master', { ...message, message_id: 2, text: '/clear_all' }, 'all'); await drain(bridge);
  assert.ok(api.calls.filter(c => c.method === 'deleteMessages').some(c => c.body.message_ids.includes(original.tg_id)));
  assert.ok(!api.calls.filter(c => c.method === 'deleteMessages').some(c => c.body.message_ids.includes(9000)));
  assert.ok(store.byTelegram(c.management, original.tg_id));
});
