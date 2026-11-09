import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Bridge } from '../src/bridge.ts';
import { Store } from '../src/store.ts';
import { Api, ApiError } from '../src/api.ts';
import { configuration, digest } from '../src/config.ts';

const config = () => ({ ...configuration({}), key: 'ab'.repeat(32), admin: 'admin', masterToken: 'master', owner: '123', management: '-1001', session: 'original-session', openwaKey: 'original-key', history: 0 });

test('legacy database keeps its original state identity after restart with new OpenWA credentials', async t => {
  const root = mkdtempSync(join(tmpdir(), 'bifrost-identity-')), path = join(root, 'bridge.db'), c = config();
  const before = new Store(path, c.key);
  // Seed genuine legacy keys before any account-identity record exists.
  before.put('status-mute', 'legacy-mute', { session: digest(c.session), sender: '111@c.us', active: true, revision: 1 });
  before.put('contact', '111@c.us', { name: 'Alice', bot: '42' });
  before.put('bot', '42', { token: before.seal('child-token'), ready: true });
  const original = new Bridge(c, before);
  const tag = original.mentions.token('group@g.us', '111@c.us');
  before.put('sync-group', `${digest(c.session)}:group@g.us`, { members: [{ id: '111@c.us' }], fetchedAt: Date.now() });
  before.put('reaction-self', digest(c.session), { sender: '999@c.us' });
  const historyKey = original.conversationHistory.scope('group@g.us', { chat: '-200' });
  const reactionKey = original.reactions.sourceKey('group@g.us', 'message');
  const feed = original.statuses.id;
  before.close();
  const after = new Store(path, c.key);
  t.after(() => { after.close(); rmSync(root, { recursive: true, force: true }); });
  // No BIFROST_ACCOUNT_ID supplied: the persisted record wins over the new connection ID.
  const rotated = new Bridge({ ...config(), session: 'new-session', openwaKey: 'new-key' }, after);
  assert.equal(rotated.c.account, 'original-session');
  assert.equal(rotated.statuses.isMuted('111@c.us'), true);
  assert.equal(rotated.token('42'), 'child-token');
  assert.equal(rotated.statuses.id, feed);
  assert.equal(rotated.conversationHistory.scope('group@g.us', { chat: '-200' }), historyKey);
  assert.equal(rotated.reactions.sourceKey('group@g.us', 'message'), reactionKey);
  assert.deepEqual(await rotated.mentions.outgoing('group@g.us', { text: 'Hello ' + tag }, 'after-move'), { text: 'Hello @111', mentions: ['111@c.us'] });
  assert.deepEqual(after.get('reaction-self', digest(rotated.c.account)), { sender: '999@c.us' });
});

test('explicit permanent identity is independent of OpenWA and a conflicting identity is refused', t => {
  const c = { ...config(), account: 'permanent-account' }, store = new Store(':memory:', c.key);
  t.after(() => store.close());
  assert.equal(new Bridge(c, store).c.account, 'permanent-account');
  assert.equal(new Bridge({ ...config(), session: 'different' }, store).c.account, 'permanent-account');
  assert.throws(() => new Bridge({ ...config(), account: 'wrong-account' }, store), /does not match/);
  assert.deepEqual(store.get('identity', 'account'), { id: 'permanent-account' });
});

test('new webhook transport rejects the old session and cannot override the permanent account', async t => {
  const c = config(), store = new Store(':memory:', c.key); t.after(() => store.close());
  new Bridge(c, store);
  const next = { ...config(), session: 'new-session', openwaKey: 'new-key', openwa: 'http://new-openwa' };
  const api = new Api(next);
  api.request = async (url, init) => {
    assert.equal(url, 'http://new-openwa/api/sessions/new-session/chats');
    assert.equal((init!.headers as any)['X-API-Key'], 'new-key'); return {};
  };
  const bridge = new Bridge(next, store, api);
  const event = { sessionId: 'original-session', event: 'message.received', idempotencyKey: 'incoming', data: { id: 'message', chatId: '111@c.us' } };
  assert.throws(() => bridge.acceptWhatsApp(event), (error: any) => error instanceof ApiError && error.status === 400);
  assert.equal(bridge.acceptWhatsApp({ ...event, sessionId: 'new-session', accountId: 'forged' }), true);
  assert.equal(store.next().payload.accountId, 'original-session');
  await api.ow('chats');
});

test('active history still holds new and legacy live jobs after the OpenWA session changes', t => {
  const c = config(), store = new Store(':memory:', c.key); t.after(() => store.close());
  new Bridge(c, store);
  store.put('history-run', 'pending-run', { session: c.account, chatId: '111@c.us', status: 'delivering' });
  store.enqueue('old-live', 'wa:111@c.us', 'wa', { sessionId: 'original-session', data: { chatId: '111@c.us' } });
  const bridge = new Bridge({ ...config(), session: 'new-session' }, store);
  bridge.acceptWhatsApp({ sessionId: 'new-session', event: 'message.received', idempotencyKey: 'new-live', data: { chatId: '111@c.us', id: 'new' } });
  assert.equal(store.next(), undefined);
  store.put('history-run', 'pending-run', { session: c.account, chatId: '111@c.us', status: 'complete' });
  const first = store.next(); assert.equal(first.dedup, 'old-live'); store.finish(first.id);
  assert.equal(store.next().payload.sessionId, 'new-session');
});

test('uncertain outgoing sends remain blocked when the API key and session are replaced', async t => {
  const c = config(), store = new Store(':memory:', c.key); t.after(() => store.close());
  const original = new Bridge(c, store);
  await assert.rejects(original.effect('outgoing', async () => { throw new ApiError('Unknown result', 0, false, true); }));
  const moved = new Bridge({ ...config(), session: 'new-session', openwaKey: 'new-key' }, store);
  let sends = 0;
  await assert.rejects(moved.effect('outgoing', async () => { sends++; return {}; }), (error: any) => error.uncertain);
  assert.equal(sends, 0);
});
