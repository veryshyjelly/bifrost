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
  sends = 0;
  async ow(path: string) {
    if (path.endsWith('/picture')) return { url: null };
    assert.ok(path.startsWith('groups/'));
    return { name: 'Synthetic', description: '', participants: [] };
  }
  async tg(_token: string, method: string, body: any) {
    if (method === 'getMe') return { id: 99, username: 'synthetic_master' };
    if (method === 'getChat') return { type: 'supergroup', title: 'Synthetic', description: '' };
    if (method === 'getChatMember') return { status: 'administrator', can_change_info: true };
    if (method === 'sendMessage') return { message_id: ++this.sends, chat: { id: body.chat_id } };
    throw new Error('Unexpected quiet-sync mutation: ' + method);
  }
}
test('an unchanged scheduled metadata run completes quietly after a populated group was synchronized', async t => {
  const c = { ...configuration({}), key: 'aa'.repeat(32), owner: '123', management: '-1001', masterToken: 'master', session: 's1', history: 0 };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  t.after(() => store.close());
  store.put('route', 'synthetic@g.us', { chatId: 'synthetic@g.us', tgChat: '-200', name: 'Synthetic', mode: 'group' });
  bridge.metadata.queue({ scope: 'all', source: 'cron', requestId: 'first-scheduled' });
  for (let i = 0; i < 20; i++) await bridge.tick();
  const before = api.sends;
  bridge.metadata.queue({ scope: 'all', source: 'cron', requestId: 'unchanged-scheduled' });
  for (let i = 0; i < 20; i++) await bridge.tick();
  assert.equal(api.sends, before);
  assert.equal(store.all('sync-run').length, 2);
  assert.ok(store.all('sync-run').every((r: any) => r.status === 'completed'));
  assert.equal(store.get('sync-status', 'last').changed, 0);
});
