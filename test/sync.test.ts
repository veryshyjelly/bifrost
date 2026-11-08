import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { Bridge } from '../src/bridge.ts';
import { Api, ApiError } from '../src/api.ts';
import { Store } from '../src/store.ts';
import { configuration, sign } from '../src/config.ts';
import { cronSchedule } from '../src/cron.ts';

class Fixture extends Api {
  calls: any[] = []; sequence = 100;
  groups: any = { 'g@g.us': { name: 'New group', description: 'New description', participants: [{ id: '111@c.us', name: 'Alice', isAdmin: true }] } };
  contacts: any = { '111@c.us': { name: 'Saved Alice', pushName: 'Alice' } };
  chats: any = { '-200': { type: 'supergroup', title: 'Old group', description: 'Old description', photo: { big_file_unique_id: 'old-group' } } };
  names: any = { master: 'Hermes', child: 'Old Alice' }; descriptions: any = { child: '' };
  photos: any = { '42': { photos: [[{ file_unique_id: 'old-contact' }]] } };
  changeInfo = true; hiddenPictures = true; groupError?: ApiError;
  async ow(path: string, body?: any) {
    this.calls.push({ source: 'wa', path, body });
    assert.equal(body, undefined, 'Sync may only read WhatsApp metadata');
    const [kind, id, action] = path.split('/').map(decodeURIComponent);
    if (kind === 'groups') {
      if (this.groupError) throw this.groupError;
      return action === 'picture' ? { url: this.hiddenPictures ? null : 'https://pps.whatsapp.net/group.jpg' } : structuredClone(this.groups[id]);
    }
    if (kind === 'contacts') return action === 'profile-picture' ? { url: this.hiddenPictures ? null : 'https://pps.whatsapp.net/contact.jpg' } : structuredClone(this.contacts[id]);
    throw new Error('Unexpected WhatsApp route: ' + path);
  }
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ source: 'tg', token, method, body });
    if (method === 'getMe') return { id: token === 'master' ? 99 : 42, username: 'hermes_bot', first_name: this.names[token] };
    if (method === 'getChat') return structuredClone(this.chats[body.chat_id]);
    if (method === 'getChatMember') return body.user_id === 99 ? { status: 'administrator', can_change_info: this.changeInfo } : { status: 'left' };
    if (method === 'getMyDescription') return { description: this.descriptions[token] || '' };
    if (method === 'getUserProfilePhotos') return structuredClone(this.photos[String(body.user_id)] || { photos: [] });
    if (method === 'setChatTitle') this.chats[body.chat_id].title = body.title;
    if (method === 'setChatDescription') this.chats[body.chat_id].description = body.description;
    if (method === 'setMyName') this.names[token] = body.name;
    if (method === 'setMyDescription') this.descriptions[token] = body.description;
    if (method === 'sendMessage') return { message_id: ++this.sequence, chat: { id: body.chat_id } };
    return true;
  }
  async upload(token: string, method: string, field: string, path: string, name: string, body: any) {
    this.calls.push({ source: 'tg', token, method, field, path, name, body });
    if (method === 'setChatPhoto') this.chats[body.chat_id].photo = { big_file_unique_id: 'photo-' + (++this.sequence) };
    if (method === 'setMyProfilePhoto') this.photos['42'] = { photos: [[{ file_unique_id: 'photo-' + (++this.sequence) }]] };
    return true;
  }
}
function fixture(t: any) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), masterToken: 'master', openwaKey: 'key', owner: '123', management: '-1001', session: 's1', history: 0 };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', name: 'Old group', mode: 'group' });
  t.after(() => store.close()); return { bridge, store, api };
}
function bot(store: Store) { store.put('contact', '111@c.us', { name: 'Old Alice', bot: '42' }); store.put('bot', '42', { token: store.seal('child'), username: 'alice_bot', ready: true }); }
async function drain(b: Bridge) {
  for (let n = 0; n < 100; n++) {
    if (!Number(b.store.db.prepare("SELECT count(*) AS n FROM jobs WHERE state='pending' AND due<=?").get(Date.now())!.n)) return;
    await b.tick();
  }
  throw new Error('Queue failed to drain');
}

test('sync updates group metadata, existing member bots and missing-bot buttons without WhatsApp writes', async t => {
  const { bridge, store, api } = fixture(t); bot(store);
  const run = bridge.metadata.queue({ scope: 'group', id: 'g@g.us', source: 'command', requestId: 'first' });
  await drain(bridge);
  assert.equal(api.chats['-200'].title, 'New group');
  assert.equal(api.chats['-200'].description, 'New description');
  assert.equal(api.names.child, 'Saved Alice');
  assert.match(api.descriptions.child, /WhatsApp bridge/);
  const status = store.get('sync-status', 'last');
  assert.equal(status.run, run.run); assert.equal(status.groups, 1); assert.equal(status.contacts, 1); assert.equal(status.changed, 4);
  assert.equal(status.missingBots, 1);
  assert.ok(api.calls.some(c => c.body?.reply_markup?.inline_keyboard?.[0]?.[0]?.url?.includes('alice_bot?startgroup')));
  assert.equal(store.all('bot').length, 1);
  assert.ok(!api.calls.some(c => /banChatMember|unbanChatMember|createForumTopic/.test(c.method || '')));
  assert.ok(!api.calls.some(c => c.source === 'wa' && /messages|read|history/.test(c.path)));
});

test('an inbox group only renames its topic and never overwrites the shared management group', async t => {
  const { bridge, store, api } = fixture(t);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-1001', thread: 5, name: 'Old group', mode: 'inbox' });
  bridge.metadata.queue({ scope: 'group', id: 'g@g.us', source: 'command', requestId: 'topic' }); await drain(bridge);
  assert.ok(api.calls.some(c => c.method === 'editForumTopic' && c.body.message_thread_id === 5 && c.body.name === 'New group'));
  assert.ok(!api.calls.some(c => ['setChatTitle', 'setChatDescription', 'setChatPhoto'].includes(c.method)));
});

test('content-hash picture caching avoids repeated uploads even when signed source URLs rotate', async t => {
  const { bridge, store, api } = fixture(t); bot(store); api.hiddenPictures = false;
  const dir = await mkdtemp(join(tmpdir(), 'hermes-sync-')), file = join(dir, 'profile.jpg');
  await writeFile(file, Buffer.from([0xff, 0xd8, 0xff, 0xd9]));
  t.after(() => rm(dir, { recursive: true, force: true }));
  bridge.media.download = async (_url, _headers, _name, cap) => { assert.ok(cap! <= 8 * 1024 * 1024); return file; };
  bridge.metadata.queue({ scope: 'all', source: 'command', requestId: 'photos1' }); await drain(bridge);
  assert.equal(api.calls.filter(c => c.method === 'setChatPhoto').length, 1);
  const profile = api.calls.find(c => c.method === 'setMyProfilePhoto');
  assert.equal(profile.field, 'image'); assert.deepEqual(profile.body.photo, { type: 'static', photo: 'attach://image' });
  bridge.metadata.queue({ scope: 'all', source: 'command', requestId: 'photos2' }); await drain(bridge);
  assert.equal(api.calls.filter(c => c.method === 'setChatPhoto').length, 1);
  assert.equal(api.calls.filter(c => c.method === 'setMyProfilePhoto').length, 1);
  assert.equal(store.get('sync-status', 'last').changed, 0);
});

test('hidden pictures and unavailable descriptions preserve Telegram data', async t => {
  const { bridge, store, api } = fixture(t); delete api.groups['g@g.us'].description;
  bridge.metadata.queue({ scope: 'group', id: 'g@g.us', source: 'command', requestId: 'hidden' }); await drain(bridge);
  assert.equal(api.chats['-200'].description, 'Old description');
  assert.equal(api.chats['-200'].photo.big_file_unique_id, 'old-group');
  assert.ok(!api.calls.some(c => ['deleteChatPhoto', 'removeMyProfilePhoto', 'setChatPhoto'].includes(c.method)));
  assert.equal(store.get('sync-status', 'last').skippedPhotos, 1);
});

test('incomplete metadata and failed lookups cannot clear the old roster or group identity', async t => {
  const { bridge, store, api } = fixture(t);
  const old = { name: 'Previous source', members: [{ canonical: '111@c.us' }], rosterHash: 'old' };
  store.put('sync-group', bridge.metadata.session + ':g@g.us', old);
  delete api.groups['g@g.us'].participants;
  bridge.metadata.queue({ scope: 'group', id: 'g@g.us', source: 'command', requestId: 'incomplete' }); await drain(bridge);
  assert.deepEqual(store.get('sync-group', bridge.metadata.session + ':g@g.us'), old);
  assert.equal(store.get('route', 'g@g.us').name, 'Old group');
  assert.equal(store.get('sync-status', 'last').warnings, 1);
  assert.equal(api.chats['-200'].title, 'Old group');
});

test('missing group permission yields a scoped grant button while contact metadata can still sync', async t => {
  const { bridge, store, api } = fixture(t); bot(store); api.changeInfo = false;
  bridge.metadata.queue({ scope: 'group', id: 'g@g.us', source: 'command', requestId: 'permissions' }); await drain(bridge);
  assert.equal(api.chats['-200'].title, 'Old group'); assert.equal(api.names.child, 'Saved Alice');
  assert.equal(store.get('sync-status', 'last').warnings, 1);
  const message = api.calls.find(c => c.body?.reply_markup?.inline_keyboard?.some((r: any) => r[0].text.startsWith('Allow info edits')));
  const url = message.body.reply_markup.inline_keyboard.find((r: any) => r[0].text.startsWith('Allow info edits'))[0].url;
  const action = new URL(url).searchParams.get('startgroup')!;
  await bridge.command('master', { text: '/start ' + action, chat: { id: -999, type: 'supergroup' } }, 'wrong-group');
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='n8n'").get()!.n, 0);
  await bridge.command('master', { text: '/start ' + action, chat: { id: -200, type: 'supergroup' } }, 'correct-group');
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='n8n'").get()!.n, 1);
});

test('one contact bot shared across groups is fetched once and LID aliases retain its binding', async t => {
  const { bridge, store, api } = fixture(t); bot(store);
  store.alias('999@lid', '111@c.us');
  api.groups['g@g.us'].participants[0].id = '999@lid';
  store.put('route', 'g2@g.us', { chatId: 'g2@g.us', tgChat: '-201', name: 'Other', mode: 'group' });
  api.groups['g2@g.us'] = structuredClone(api.groups['g@g.us']); api.chats['-201'] = structuredClone(api.chats['-200']);
  bridge.metadata.queue({ scope: 'all', source: 'command', requestId: 'shared' }); await drain(bridge);
  assert.equal(api.calls.filter(c => c.path === 'contacts/111%40c.us').length, 1);
  assert.equal(store.get('contact', '111@c.us').bot, '42');
  assert.equal(store.get('sync-status', 'last').contacts, 1);
  assert.equal(store.get('sync-status', 'last').groups, 2);
});

test('/sync scopes to its conversation, and global sync uses the signed management path', async t => {
  const { bridge, store } = fixture(t);
  const requests = () => store.db.prepare("SELECT payload FROM jobs WHERE type='n8n' ORDER BY id").all().map(r => JSON.parse(r.payload as string));
  await bridge.command('master', { text: '/sync@hermes_bot', chat: { id: -200, type: 'supergroup' } }, 'group-sync');
  assert.equal(requests()[0].type, 'sync_metadata');
  assert.equal(requests()[0].data.scope, 'group');
  assert.equal(requests()[0].data.id, 'g@g.us');
  await bridge.command('master', { text: '/sync all', chat: { id: -200, type: 'supergroup' } }, 'group-global-sync');
  assert.equal(requests().length, 1);
  await bridge.command('master', { text: '/sync all', chat: { id: 123, type: 'private' } }, 'private-global-sync');
  assert.equal(requests()[1].data.scope, 'all');
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='sync-start'").get()!.n, 0);
  await bridge.manage(requests()[0], 'group-delivered');
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='sync-start'").get()!.n, 1);
});

test('/sync in a bound member bot DM works without a one-to-one route and reports back through that bot', async t => {
  const { bridge, store, api } = fixture(t); bot(store);
  assert.equal(store.get('route', '111@c.us'), undefined);
  bridge.acceptTelegram('42', { update_id: 1, message: { message_id: 1, from: { id: 123 }, chat: { id: 123, type: 'private' }, text: '/sync' } });
  await bridge.tick();
  const job = store.db.prepare("SELECT id,payload FROM jobs WHERE type='n8n'").get()!;
  assert.ok(job);
  const event = JSON.parse(job.payload as string);
  assert.equal(event.type, 'sync_metadata');
  assert.equal(event.data.scope, 'contact');
  assert.equal(event.data.id, '111@c.us');
  assert.deepEqual(event.data.notify, { bot: '42', chat: '123' });
  store.finish(Number(job.id));
  bridge.acceptManagement({ ...event, signature: sign(JSON.stringify(event), bridge.c.admin) });
  await drain(bridge);
  const status = store.get('sync-status', 'last');
  assert.equal(status.contacts, 1); assert.equal(status.groups, 0); assert.equal(status.warnings, 0);
  assert.equal(api.names.child, 'Saved Alice');
  const report = api.calls.find(c => c.method === 'sendMessage' && c.body.text.includes('metadata sync complete'));
  assert.equal(report.token, 'child'); assert.equal(report.body.chat_id, '123');
  assert.equal(report.body.message_thread_id, undefined);
  assert.equal(store.get('route', '111@c.us'), undefined, 'Metadata sync must not create a new conversation route');
  assert.equal(store.get('contact', '111@c.us').bot, '42');
});

test('an unbound member bot explains the missing binding for Start and /sync without accessing WhatsApp', async t => {
  const { bridge, store, api } = fixture(t);
  store.put('bot', '42', { token: store.seal('child'), ready: false });
  const dm = { from: { id: 123 }, chat: { id: 123, type: 'private' } };
  await bridge.telegram('42', { message: { ...dm, message_id: 1, text: '/start' } }, 'unbound-start');
  await bridge.telegram('42', { message: { ...dm, message_id: 2, text: '/sync' } }, 'unbound-sync');
  assert.equal(store.get('bot', '42').ready, true);
  const messages = api.calls.filter(c => c.method === 'sendMessage');
  assert.equal(messages.length, 2);
  assert.ok(messages.every(c => c.token === 'child' && c.body.text.includes('select the contact in its binding message')));
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='n8n'").get()!.n, 0);
  assert.ok(!api.calls.some(c => c.source === 'wa'));
});

test('member-bot DM sync keeps the contact scope with an existing inbox topic and rejects global sync', async t => {
  const { bridge, store, api } = fixture(t); bot(store);
  store.put('route', '111@c.us', { chatId: '111@c.us', tgChat: '-1001', thread: 5, mode: 'dedicated' });
  const dm = { chat: { id: 123, type: 'private' } };
  await bridge.command('42', { ...dm, text: '/sync@alice_bot' }, 'bound-sync');
  const request = JSON.parse(store.db.prepare("SELECT payload FROM jobs WHERE type='n8n'").get()!.payload as string);
  assert.equal(request.data.scope, 'contact');
  assert.equal(request.data.id, '111@c.us');
  assert.equal(request.data.notify.thread, undefined);
  assert.equal(api.calls.find(c => c.method === 'sendMessage').body.message_thread_id, undefined);
  await bridge.command('42', { ...dm, text: '/sync all' }, 'rejected-global-sync');
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='n8n'").get()!.n, 1);
  assert.ok(api.calls.some(c => c.method === 'sendMessage' && c.body.text.includes('/sync all in your private chat with the master bot')));
  assert.equal(bridge.acceptTelegram('42', { update_id: 3, message: { ...dm, message_id: 3, from: { id: 999 }, text: '/sync' } }), false);
});

test('a failed group lookup does not prevent existing contact bots from syncing', async t => {
  const { bridge, store, api } = fixture(t); bot(store);
  api.groupError = new ApiError('Group is unavailable', 404);
  bridge.metadata.queue({ scope: 'all', source: 'command', requestId: 'lookup-failure' }); await drain(bridge);
  assert.equal(api.chats['-200'].title, 'Old group');
  assert.equal(api.names.child, 'Saved Alice');
  assert.equal(store.get('sync-status', 'last').warnings, 1);
  assert.equal(store.all('sync-run')[0].status, 'completed');
});

test('chat jobs take priority over metadata work, and coalesced manual requests receive a completion', async t => {
  const { bridge, store, api } = fixture(t);
  const first = bridge.metadata.queue({ scope: 'all', source: 'cron', requestId: 'cron-1' });
  const second = bridge.metadata.queue({ scope: 'group', id: 'g@g.us', source: 'command', requestId: 'manual-1', notify: { bot: 'master', chat: '123' } });
  assert.equal(first.run, second.run); assert.equal(second.queued, false);
  bridge.acceptTelegram('master', { update_id: 1, message: { message_id: 1, from: { id: 123 }, chat: { id: 123, type: 'private' }, text: '/status' } });
  await bridge.tick();
  assert.equal(store.db.prepare("SELECT state FROM jobs WHERE type='tg'").get()!.state, 'done');
  assert.equal(store.db.prepare("SELECT state FROM jobs WHERE type='sync-start'").get()!.state, 'pending');
  await drain(bridge);
  assert.ok(api.calls.some(c => c.method === 'sendMessage' && c.body.chat_id === '123' && c.body.text.includes('metadata sync complete')));
});

test('cron is timezone-aware and persists one run per scheduled minute across restarts', t => {
  const { bridge, store } = fixture(t);
  const first = bridge.metadata.cron(new Date('2026-11-09T05:30:02Z'))!; // 11:00 in Kolkata
  assert.equal(first.queued, true);
  assert.equal(bridge.metadata.cron(new Date('2026-11-09T05:30:50Z'))!.queued, false);
  assert.equal(bridge.metadata.cron(new Date('2026-11-09T05:31:00Z')), undefined);
  const newProcess = new Bridge(bridge.c, store, bridge.api);
  assert.equal(newProcess.metadata.cron(new Date('2026-11-09T05:30:59Z'))!.queued, false);
});

test('cron supports quarter-hour, daily, weekday and disabled schedules and rejects malformed fields', () => {
  assert.equal(cronSchedule('*/15 * * * *', 'UTC')(new Date('2026-11-09T00:15:00Z')), true);
  assert.equal(cronSchedule('0 */6 * * *', 'UTC')(new Date('2026-11-09T06:00:00Z')), true);
  assert.equal(cronSchedule('0 9 * * 1-5', 'UTC')(new Date('2026-11-09T09:00:00Z')), true);
  assert.equal(cronSchedule('0 9 * * 1-5', 'UTC')(new Date('2026-11-08T09:00:00Z')), false);
  assert.equal(cronSchedule('off', 'UTC')(new Date()), false);
  for (const bad of ['60 * * * *', '*/0 * * * *', '*/ * * * *', ', * * * *', '* * *', '* * 0 * *']) assert.throws(() => cronSchedule(bad, 'UTC'));
});
