import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
import { oracle } from './oracle.ts';

const baseline = oracle(), uri = (file: string) => pathToFileURL(`${baseline}/${file}`).href;
const { Bridge } = await import(uri('src/bridge.ts'));
const { Store } = await import(uri('src/store.ts'));
const { Api } = await import(uri('src/api.ts'));
const { configuration } = await import(uri('src/config.ts'));
const rows = ['a', 'b', 'self'].map((id, i) => ({ id, chatId: 'synthetic@g.us', author: '12025550123@c.us', from: 'synthetic@g.us', isGroup: true,
  type: 'text', body: id, timestamp: 1700000000 + i, quotedMessage: { id: id === 'a' ? 'b' : id === 'b' ? 'a' : 'self', body: 'Cycle preview' } }));
class Fixture extends Api {
  sequence = 100;
  async ow(path: string) { assert.ok(path.includes('/history?')); return rows; }
  async tg(_token: string, method: string, body: any) {
    assert.equal(method, 'sendMessage');
    return { message_id: ++this.sequence, chat: { id: body.chat_id }, text: body.text, entities: body.entities };
  }
}
test('cyclic and self-quoted history plans terminate and deliver each source once without losing the checkpoint', async t => {
  const c = { ...configuration({}), key: 'aa'.repeat(32), masterToken: 'master', owner: '123', management: '-1001', account: 'stable-account', session: 's1', history: 3 };
  const store = new Store(':memory:', c.key), bridge = new Bridge(c, store, new Fixture(c));
  t.after(() => store.close());
  store.put('route', 'synthetic@g.us', { chatId: 'synthetic@g.us', name: 'Synthetic', tgChat: '-200', mode: 'group' });
  store.put('contact', '12025550123@c.us', { name: 'Alice' });
  const run = bridge.conversationHistory.request('synthetic@g.us', { chat: '-200' }, 3, 'cycles', false);
  await bridge.history({ run: run.id }, 'fetch-cycles');
  assert.deepEqual(store.get('history-plan', run.id).items.map((m: any) => m.id), ['b', 'a', 'self']);
  for (let i = 0; i < 20; i++) await bridge.tick();
  assert.equal(store.get('history-run', run.id).status, 'complete');
  for (const row of rows) assert.equal(store.byWhatsApp('synthetic@g.us', row.id).length, 1);
});

test('a legacy history job without a run ID upgrades through original fetch and remains deduplicated', async t => {
  const c = { ...configuration({}), key: 'aa'.repeat(32), masterToken: 'master', owner: '123', management: '-1001', account: 'stable-account', session: 's1', history: 3 };
  const store = new Store(':memory:', c.key), bridge = new Bridge(c, store, new Fixture(c));
  t.after(() => store.close());
  store.put('route', 'synthetic@g.us', { chatId: 'synthetic@g.us', name: 'Synthetic', tgChat: '-200', mode: 'group' });
  store.put('contact', '12025550123@c.us', { name: 'Alice' });
  const payload = { chatId: 'synthetic@g.us', count: 3 };
  store.enqueue('legacy-history', 'history:synthetic@g.us', 'history', payload);
  for (let i = 0; i < 20; i++) await bridge.tick();
  const run = store.all('history-run')[0];
  assert.equal(run.status, 'complete');
  assert.equal(run.session, 'stable-account');
  await bridge.history(payload, 'legacy-history');
  assert.equal(store.all('history-run').length, 1);
  for (const row of rows) assert.equal(store.byWhatsApp('synthetic@g.us', row.id).length, 1);
});
