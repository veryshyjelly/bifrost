import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Bridge } from '../src/bridge.ts';
import { Store } from '../src/store.ts';
import { configuration, digest } from '../src/config.ts';
import { Api, ApiError } from '../src/api.ts';
import { MentionError } from '../src/mentions.ts';
import { splitFormatted } from '../src/format.ts';

const alice = '12025550123@c.us', bob = '12025550124@c.us', lid = '987654321@lid', group = 'g@g.us';
class Fixture extends Api {
  calls: any[] = []; sequence = 100; members = new Set(['42']); rows: any[] = []; failRoster = false; refuseSend = false;
  participants = [{ id: alice, name: 'Alice' }, { id: bob, name: 'Bob' }, { id: lid, name: 'Hidden' }];
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
    if (method === 'getMe') return { id: token === 'master' ? 99 : 42, username: token === 'master' ? 'hermes_bot' : 'alice_bot' };
    if (method === 'getChatMember') return { status: this.members.has(String(body.user_id)) ? 'member' : 'left' };
    return { message_id: ++this.sequence, chat: { id: body.chat_id }, text: body.text, entities: body.entities };
  }
  async ow(path: string, body?: any, key?: string) {
    this.calls.push({ path, body, key });
    if (path.startsWith('groups/')) { if (this.failRoster) throw new ApiError('Unavailable', 503, true); return { name: 'Group', participants: this.participants }; }
    if (path.includes('/history?')) return this.rows;
    if (this.refuseSend) { this.refuseSend = false; throw new ApiError('Rate limited', 429, true); }
    return { messageId: 'sent-' + (++this.sequence) };
  }
  async upload(token: string, method: string, field: string, path: string, name: string, body: any) {
    this.calls.push({ token, method, field, path, name, body }); return { message_id: ++this.sequence, chat: { id: body.chat_id } };
  }
  async request() { return { ok: true }; }
}
function fixture(t: any, path = ':memory:') {
  const c = { ...configuration({}), key: 'ab'.repeat(32), masterToken: 'master', owner: '123', management: '-1001', session: 's1' };
  const store = new Store(path, c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  store.put('route', group, { chatId: group, tgChat: '-200', mode: 'group' });
  store.put('contact', alice, { name: 'Alice', bot: '42' }); store.put('contact', bob, { name: 'Bob' }); store.put('contact', lid, { name: 'Hidden' });
  store.put('bot', '42', { token: store.seal('child'), username: 'alice_bot', ready: true });
  t.after(() => store.close()); return { bridge, api, store, c };
}
const event = (id = 'wa1', extra: any = {}) => ({ sessionId: 's1', event: 'message.received', idempotencyKey: id,
  data: { id, chatId: group, from: group, author: bob, isGroup: true, type: 'text', body: 'Hi @12025550123', mentionedIds: [alice], timestamp: 1700000000, ...extra } });
const tg = (text: string, extra: any = {}) => ({ message_id: 1, from: { id: 123 }, chat: { id: -200, type: 'supergroup' }, text, ...extra });
const sends = (api: Fixture) => api.calls.filter(c => c.path?.startsWith('messages/') && c.body);
const sent = (api: Fixture) => api.calls.filter(c => c.method === 'sendMessage');

test('incoming WhatsApp tags become readable native bot mentions with correct UTF-16 offsets and no duplicate sender header', async t => {
  const { bridge, api } = fixture(t);
  await bridge.whatsApp(event('m', { author: alice, body: '😀 Hi @12025550123, @12025550123!' }), 'incoming');
  const message = sent(api).at(-1).body;
  assert.equal(message.text, '😀 Hi @Alice, @Alice!');
  assert.equal(message.entities.length, 2);
  for (const tag of message.entities) { assert.equal(message.text.slice(tag.offset, tag.offset + tag.length), '@Alice'); assert.equal(tag.type, 'text_mention'); assert.equal(tag.user.id, 42); assert.equal(tag.user.is_bot, true); }
  assert.equal(api.calls.filter(c => c.method === 'getChatMember').length, 2); // sender selection plus one mention lookup
});

test('absent bots link to Telegram, unbound members link to WhatsApp, and hidden numbers use the uniform fallback', async t => {
  const { bridge, api } = fixture(t); api.members.clear();
  const result = await bridge.mentions.incoming('Hi @12025550123 @12025550124 @987654321', [alice, bob, lid], group, '-200', 'links');
  assert.equal(result.text, 'Hi @Alice @Bob @Hidden');
  assert.deepEqual(result.entities.map(e => e.url), ['https://t.me/alice_bot', 'https://wa.me/12025550124', 'https://wa.me/']);
});

test('mention metadata and phone/LID aliases determine replacements, while ambiguous or literal numbers stay unchanged', async t => {
  const { bridge, store } = fixture(t); store.alias(lid, alice);
  const result = await bridge.mentions.incoming('@987654321 @12025550123 @123456', [lid], group, '-200', 'aliases');
  assert.equal(result.text, '@Alice @Alice @123456');
  assert.equal((await bridge.mentions.incoming('@12025550123', [], group, '-200', 'literal')).text, '@12025550123');
  assert.equal((await bridge.mentions.incoming('@12025550123', [alice, '12025550123@lid'], group, '-200', 'ambiguous')).entities.length, 0);
});

test('incoming captions, replies and edits retain mentions, and explicit removal clears them', async t => {
  const { bridge, api, store } = fixture(t);
  bridge.media.fromWhatsApp = async () => ({ path: '/fixture', name: 'photo.jpg' });
  await bridge.whatsApp(event('parent'), 'parent');
  await bridge.whatsApp(event('photo', { type: 'image', quotedMessage: { id: 'parent' } }), 'photo');
  const upload = api.calls.find(c => c.method === 'sendPhoto');
  assert.ok(upload.body.caption.includes('@Alice')); assert.ok(upload.body.caption_entities.some(e => e.type === 'text_mention'));
  assert.equal(upload.body.reply_parameters.message_id, store.byWhatsApp(group, 'parent')[0].tg_id);
  await bridge.waChange({ event: 'message.edited', data: { chatId: group, id: 'photo', body: 'New @12025550123' } }, 'edit');
  assert.ok(api.calls.find(c => c.method === 'editMessageCaption').body.caption_entities.some(e => e.type === 'text_mention'));
  await bridge.waChange({ event: 'message.edited', data: { chatId: group, id: 'photo', body: 'Literal @12025550123', mentionedIds: [] } }, 'remove');
  assert.ok(!api.calls.filter(c => c.method === 'editMessageCaption').at(-1).body.caption_entities.some(e => e.type === 'text_mention'));
});

test('manual history renders mention entities alongside native dates and keeps source mappings', async t => {
  const { bridge, api, store } = fixture(t); api.rows = [event().data];
  const run = bridge.conversationHistory.request(group, { chat: '-200' }, 1, 'history', false);
  await bridge.history({ run: run.id }, 'fetch');
  const job = store.db.prepare("SELECT dedup,payload FROM jobs WHERE type='history-message'").get()!;
  await bridge.conversationHistory.process({ type: 'history-message', dedup: job.dedup, payload: JSON.parse(job.payload as string) });
  assert.ok(sent(api)[0].body.entities.some(e => e.type === 'date_time'));
  assert.ok(sent(api)[0].body.entities.some(e => e.type === 'text_mention'));
});

test('incoming mention rendering remains stable across retries and text splitting preserves offsets', async t => {
  const { bridge, api } = fixture(t);
  const first = await bridge.mentions.incoming('😀'.repeat(2000) + ' @12025550123', [alice], group, '-200', 'same');
  api.members.clear(); const again = await bridge.mentions.incoming('😀'.repeat(2000) + ' @12025550123', [alice], group, '-200', 'same');
  assert.deepEqual(again, first);
  const parts = splitFormatted(first); assert.equal(parts.map(p => p.text).join(''), first.text);
  const part = parts.find(p => p.entities.length)!; assert.equal(part.text.slice(part.entities[0].offset, part.entities[0].offset + part.entities[0].length), '@Alice');
});

test('outgoing bot usernames and name entities produce one native WhatsApp mention and preserve quoted replies', async t => {
  const { bridge, api, store } = fixture(t); store.map(group, 'parent', '-200', 55, 'master', 'in');
  const text = '😀 @Alice + @ALICE_BOT! @someone_else';
  await bridge.telegram('master', { message: tg(text, { entities: [{ type: 'text_mention', offset: 3, length: 6, user: { id: 42, is_bot: true } }], reply_to_message: { message_id: 55 } }) }, 'send');
  const out = sends(api)[0]; assert.equal(out.path, 'messages/reply'); assert.equal(out.body.text, '😀 @12025550123 + @12025550123! @someone_else');
  assert.deepEqual(out.body.mentions, [alice]); assert.equal(out.body.quotedMessageId, 'parent');
});

test('emails, URLs, code and unrelated numbers do not become outgoing mentions', async t => {
  const { bridge } = fixture(t);
  const text = 'x@alice_bot https://t.me/@alice_bot @alice_bot @12025550123 @other';
  const offset = text.lastIndexOf('@alice_bot');
  const result = await bridge.mentions.outgoing(group, tg(text, { entities: [{ type: 'code', offset, length: 10 }] }), 'excluded');
  assert.equal(result.text, text); assert.deepEqual(result.mentions, []);
});

test('LID-bound bots and copied unbound tags send their exact WhatsApp identities', async t => {
  const { bridge, store } = fixture(t);
  store.put('contact', lid, { name: 'Hidden', bot: '43' }); store.put('bot', '43', { username: 'hidden_bot' });
  const token = bridge.mentions.token(group, bob);
  const result = await bridge.mentions.outgoing(group, tg(`Hi @hidden_bot and ${token}`), 'lids');
  assert.equal(result.text, 'Hi @987654321 and @12025550124'); assert.deepEqual(result.mentions, [lid, bob]);
});

test('outgoing captions and edits carry updated mention lists, including explicit empty removal', async t => {
  const { bridge, api } = fixture(t);
  const dir = await mkdtemp(join(tmpdir(), 'mention-media-')); t.after(() => rm(dir, { recursive: true, force: true }));
  const { writeFile } = await import('node:fs/promises'); const path = join(dir, 'photo.jpg'); await writeFile(path, 'fixture');
  bridge.media.fromTelegram = async () => ({ kind: 'photo', path, name: 'photo.jpg', mime: 'image/jpeg' });
  await bridge.telegram('master', { message: tg('', { text: undefined, caption: 'Hi @Alice', caption_entities: [{ type: 'text_mention', offset: 3, length: 6, user: { id: 42 } }], photo: [{}] }) }, 'caption');
  assert.equal(sends(api)[0].body.caption, 'Hi @12025550123'); assert.deepEqual(sends(api)[0].body.mentions, [alice]);
  await bridge.telegram('master', { edited_message: tg('New @alice_bot') }, 'edit');
  assert.deepEqual(sends(api).at(-1).body.mentions, [alice]); assert.equal(sends(api).at(-1).body.body, 'New @12025550123');
  await bridge.telegram('master', { edited_message: tg('No tags') }, 'remove');
  assert.deepEqual(sends(api).at(-1).body.mentions, []);
});

test('invalid, wrong-group, previous-session and departed-member tokens are refused before sending', async t => {
  const { bridge, api, store } = fixture(t); const token = bridge.mentions.token(group, bob);
  await bridge.telegram('master', { message: tg('Hi @{Bob|00000000}') }, 'unknown'); assert.equal(sends(api).length, 0);
  await assert.rejects(bridge.mentions.outgoing('other@g.us', tg(token), 'other'), MentionError);
  const id = /\|([^}]+)/.exec(token)![1], saved = store.get('mention-token', id);
  store.put('mention-token', id, { ...saved, session: 'old' }); await assert.rejects(bridge.mentions.outgoing(group, tg(token), 'session'), MentionError);
  store.put('mention-token', id, saved); api.participants = api.participants.filter(m => m.id !== bob);
  await assert.rejects(bridge.mentions.outgoing(group, tg(token), 'departed'), MentionError);
  assert.ok(sent(api).some(c => c.body.text.includes('unknown')));
});

test('copy tokens persist across reopening and renaming, and duplicate names get distinct identities', async t => {
  const dir = await mkdtemp(join(tmpdir(), 'mention-db-')); t.after(() => rm(dir, { recursive: true, force: true }));
  const { bridge, store, c } = fixture(t, join(dir, 'bridge.db')); store.put('contact', lid, { name: 'Bob' });
  const bobTag = bridge.mentions.token(group, bob), otherTag = bridge.mentions.token(group, lid); assert.notEqual(bobTag, otherTag);
  const reopened = new Store(c.db = join(dir, 'bridge.db'), c.key); t.after(() => reopened.close());
  reopened.put('contact', bob, { name: 'Renamed' }); const next = new Bridge(c, reopened, new Fixture(c));
  const out = await next.mentions.outgoing(group, tg(bobTag.replace('Bob', 'Decorative name') + ' ' + otherTag), 'reopened');
  assert.deepEqual(out.mentions, [bob, lid]);
});

test('/mention searches names and usernames, gives copy buttons, and never writes to WhatsApp', async t => {
  const { bridge, api } = fixture(t);
  await bridge.command('master', tg('/mention aLiCe'), 'picker');
  const result = sent(api).at(-1).body; assert.ok(result.text.includes('Alice')); assert.ok(!result.text.includes('Hidden'));
  assert.equal(result.reply_markup.inline_keyboard[0][0].copy_text.text, '@alice_bot');
  await bridge.command('master', tg('/mention Hidden'), 'hidden');
  assert.match(sent(api).at(-1).body.reply_markup.inline_keyboard[0][0].copy_text.text, /^@\{Hidden\|[a-f0-9]{8}\}$/);
  assert.ok(api.calls.filter(c => c.path).every(c => c.path.startsWith('groups/') && c.body === undefined));
});

test('picker uses scoped pagination and rejects wrong owner, bot, topic, session and expired callbacks', async t => {
  const { bridge, api, store } = fixture(t);
  api.participants = Array.from({ length: 12 }, (_, i) => ({ id: `12025550${100 + i}@c.us`, name: `Person ${i.toString().padStart(2, '0')}` }));
  await bridge.command('master', tg('/mention'), 'picker');
  const data = sent(api).at(-1).body.reply_markup.inline_keyboard.at(-1)[0].callback_data;
  const action = store.get('action', data.slice(8));
  const q = { id: 'q', from: { id: 123 }, data, message: tg('picker') };
  const before = sent(api).length;
  for (const changed of [{ from: { id: 999 } }, { message: tg('picker', { chat: { id: -201, type: 'supergroup' } }) }]) await bridge.mentions.callback('master', { ...q, ...changed }, 'invalid');
  await bridge.mentions.callback('42', q, 'wrong-bot');
  store.put('action', data.slice(8), { ...action, session: 'old' }); await bridge.mentions.callback('master', q, 'session');
  store.put('action', data.slice(8), { ...action, expires: 0 }); await bridge.mentions.callback('master', q, 'expired');
  assert.equal(sent(api).length, before);
  store.put('action', data.slice(8), action); await bridge.telegram('master', { callback_query: q }, 'next');
  assert.ok(sent(api).at(-1).body.text.includes('page 2/2')); assert.equal(sent(api).at(-1).body.reply_markup.inline_keyboard.length, 3);
});

test('stale-roster refresh failure shows a cached picker but refuses unvalidated sends', async t => {
  const { bridge, api, store } = fixture(t); await bridge.mentions.roster(group);
  const cached = bridge.mentions.cached(group); store.put('sync-group', `${digest('s1')}:${group}`, { ...cached, fetchedAt: 1 }); api.failRoster = true;
  await bridge.command('master', tg('/mention Bob'), 'cached'); assert.ok(sent(api).at(-1).body.text.includes('cached'));
  await bridge.telegram('master', { message: tg(bridge.mentions.token(group, bob)) }, 'refused'); assert.equal(sends(api).length, 0);
});

test('429 retries retain the original resolved text and identity after bot bindings change', async t => {
  const { bridge, api, store } = fixture(t); api.refuseSend = true;
  await assert.rejects(bridge.telegram('master', { message: tg('Hi @alice_bot') }, 'first'), ApiError);
  store.put('contact', alice, { name: 'Alice' }); store.put('contact', bob, { name: 'Bob', bot: '42' });
  await bridge.telegram('master', { message: tg('Hi @alice_bot') }, 'retry');
  assert.equal(sends(api).length, 2); assert.deepEqual(sends(api)[0].body, sends(api)[1].body); assert.equal(sends(api)[0].key, sends(api)[1].key);
});

test('group member bots ignore ordinary tagged updates so Hermes sends exactly once', async t => {
  const { bridge, store } = fixture(t);
  const update = { update_id: 1, message: tg('Hi @alice_bot') };
  assert.equal(bridge.acceptTelegram('42', update), false); assert.equal(bridge.acceptTelegram('master', update), true);
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='tg'").get()!.n, 1);
});

test('incomplete copied tokens are refused and fresh roster caches expire after five minutes', async t => {
  const { bridge, api, store } = fixture(t);
  await assert.rejects(bridge.mentions.outgoing(group, tg('Hi @{Bob|1234'), 'incomplete'), MentionError);
  await bridge.mentions.roster(group); await bridge.mentions.roster(group);
  assert.equal(api.calls.filter(c => c.path?.startsWith('groups/')).length, 1);
  const cache = bridge.mentions.cached(group); store.put('sync-group', `${digest('s1')}:${group}`, { ...cache, fetchedAt: Date.now() - 300001 });
  api.participants = api.participants.filter(m => m.id !== alice);
  await assert.rejects(bridge.mentions.outgoing(group, tg('@alice_bot'), 'departed-bot'), MentionError);
  assert.equal(api.calls.filter(c => c.path?.startsWith('groups/')).length, 2);
});

test('picker tokens handle short-ID collisions and aliases learned after copying', async t => {
  const { bridge, store } = fixture(t);
  const prefix = digest(JSON.stringify([digest('s1'), group, bob])).slice(0, 8);
  store.put('mention-token', prefix, { session: 'other', chat: group, person: alice });
  assert.match(bridge.mentions.token(group, bob), /\|[a-f0-9]{10}\}$/);
  const token = bridge.mentions.token(group, lid); store.alias(lid, bob);
  assert.deepEqual((await bridge.mentions.outgoing(group, tg(token), 'aliased-token')).mentions, [bob]);
});

test('picker accepts a linked management topic, isolates its callbacks and rejects unrelated DMs and owners', async t => {
  const { bridge, api, store } = fixture(t);
  store.put('route', group, { chatId: group, tgChat: '-1001', thread: 88, mode: 'inbox' });
  api.participants = Array.from({ length: 12 }, (_, i) => ({ id: `12025550${100 + i}@c.us`, name: `Person ${i}` }));
  const message = tg('/mention', { chat: { id: -1001, type: 'supergroup' }, message_thread_id: 88, is_topic_message: true });
  await bridge.command('master', message, 'topic');
  const result = sent(api).at(-1).body; assert.equal(result.message_thread_id, 88);
  const data = result.reply_markup.inline_keyboard.at(-1)[0].callback_data, count = sent(api).length;
  await bridge.telegram('master', { callback_query: { id: 'wrong-topic', from: { id: 123 }, data, message: { ...message, message_thread_id: 89 } } }, 'wrong-topic');
  assert.equal(sent(api).length, count);
  await bridge.command('master', tg('/mention', { from: { id: 999 } }), 'non-owner'); assert.equal(sent(api).length, count);
  await bridge.command('42', tg('/mention', { chat: { id: 123, type: 'private' } }), 'dm');
  assert.ok(sent(api).at(-1).body.text.includes('WhatsApp group'));
  assert.ok(!api.calls.some(c => ['messages/send-text', 'chats/read'].includes(c.path)));
});
