import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
import { oracle } from './oracle.ts';

const baseline = oracle(), uri = (file: string) => pathToFileURL(`${baseline}/${file}`).href;
const { Bridge } = await import(uri('src/bridge.ts'));
const { Store } = await import(uri('src/store.ts'));
const { Api } = await import(uri('src/api.ts'));
const { configuration } = await import(uri('src/config.ts'));
class Fixture extends Api {
  async tg() { throw new Error('Ignored updates must not call Telegram'); }
  async ow() { throw new Error('Ignored updates must not call WhatsApp'); }
}
function fixture(t: any) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), owner: '123', masterToken: 'master', account: 'stable-account', session: 's1', history: 0 };
  const store = new Store(':memory:', c.key), bridge = new Bridge(c, store, new Fixture(c));
  t.after(() => store.close()); return { store, bridge };
}
test('unhandled Telegram update envelopes and foreign or bot actors remain ignored without invented forwarding', async t => {
  const { store, bridge } = fixture(t);
  for (const kind of ['channel_post', 'edited_channel_post', 'business_message', 'unknown_future_update']) {
    const update = { update_id: 1, [kind]: { message_id: 1, from: { id: 123 }, chat: { id: 123, type: 'private' }, text: 'Ignored' } };
    assert.equal(bridge.acceptTelegram('master', update), false);
    await bridge.telegram('master', update, kind);
  }
  for (const from of [{ id: 999 }, { id: 123, is_bot: true }]) assert.equal(bridge.acceptTelegram('master', {
    update_id: 2, message: { message_id: 2, from, chat: { id: 123, type: 'private' }, text: 'Ignored' },
  }), false);
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='tg'").get().n, 0);
});
test('admitted unsupported WhatsApp scopes complete without mappings or provider calls', async t => {
  const { store, bridge } = fixture(t);
  const cases = [{ kind: 'channel' }, { kind: 'broadcast' }, { isStatusBroadcast: true }, { id: '' }];
  for (const [i, extra] of cases.entries()) {
    bridge.acceptWhatsApp({ sessionId: 's1', event: 'message.received', idempotencyKey: `ignored-${i}`, data: {
      id: `ignored-${i}`, chatId: '12025550123@c.us', from: '12025550123@c.us', type: 'text', body: 'Ignored', ...extra,
    } });
    await bridge.tick();
  }
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='wa' AND state='done'").get().n, cases.length);
  assert.equal(store.db.prepare('SELECT count(*) AS n FROM messages').get().n, 0);
});
test('invalid transport session, event, bot and owner update IDs remain explicit refusals', t => {
  const { store, bridge } = fixture(t);
  for (const payload of [{}, { sessionId: 'old', event: 'message.received', idempotencyKey: 'old', data: {} }, { sessionId: 's1', event: 'future.unknown', idempotencyKey: 'unknown', data: {} }]) {
    assert.throws(() => bridge.acceptWhatsApp(payload), (e: any) => e.status === 400);
  }
  assert.throws(() => bridge.acceptTelegram('unknown', {}), (e: any) => e.status === 404);
  assert.throws(() => bridge.acceptTelegram('master', { update_id: null, message: { from: { id: 123 }, chat: { id: 123, type: 'private' }, text: 'Invalid' } }), (e: any) => e.status === 400);
  assert.equal(store.db.prepare('SELECT count(*) AS n FROM jobs').get().n, 0);
});

for (const [label, payload, suffix] of [['absent', {}, 'undefined'], ['null', { id: null }, 'null']] as const) {
  test(`legacy recovery retains ${label} event IDs and the original unknown-job completion behavior`, async t => {
    const { store, bridge } = fixture(t);
    store.enqueue('unknown-job', 'unknown-job', 'future-job-type', { synthetic: true });
    await bridge.tick();
    assert.equal(store.db.prepare("SELECT state FROM jobs WHERE dedup='unknown-job'").get().state, 'done');
    store.enqueue('old-forward', 'old-forward', 'n8n', payload);
    await bridge.tick(); await bridge.tick();
    const forwarded = store.db.prepare('SELECT * FROM jobs WHERE dedup=?').get('management:' + suffix);
    assert.equal(forwarded.state, 'failed');
    assert.deepEqual(JSON.parse(forwarded.payload), payload);
    assert.equal(store.db.prepare("SELECT state FROM jobs WHERE dedup='old-forward'").get().state, 'done');
  });
}
