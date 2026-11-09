import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Bridge } from '../src/bridge.ts';
import { Store } from '../src/store.ts';
import { Api, ApiError } from '../src/api.ts';
import { configuration } from '../src/config.ts';
import { ensureOpenWAWebhook, waEvents } from '../src/openwa.ts';

const contact = { id: '12025550123@c.us', name: 'Abhishek' };
const story = (id = 'story', extra = {}) => ({ sessionId: 's1', event: 'status.received', idempotencyKey: id, data: {
  statusId: id, contact, type: 'text', caption: 'Hi <bro> & 😀', postedAt: Date.now() - 1000, expiresAt: Date.now() + 3600000, ...extra,
} });
class Fixture extends Api {
  calls: any[] = []; seq = 100; rows: any[] = []; hooks: any[] = [];
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
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
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='n8n'").get()!.n, 0);
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

test('/statuses opens and refreshes the feed from Hermes DM; /status remains the health command', async t => {
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
