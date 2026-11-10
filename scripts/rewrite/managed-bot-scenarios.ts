import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
import { oracle } from './oracle.ts';

const baseline = oracle(), uri = (file: string) => pathToFileURL(`${baseline}/${file}`).href;
const { Bridge } = await import(uri('src/bridge.ts'));
const { Store } = await import(uri('src/store.ts'));
const { Api, ApiError } = await import(uri('src/api.ts'));
const { configuration } = await import(uri('src/config.ts'));
class Fixture extends Api {
  version = 1; sequence = 100; failure?: string; messages: any[] = [];
  async tg(_token: string, method: string, body: any = {}) {
    if (this.failure === method) { this.failure = undefined; throw new ApiError('synthetic setup refusal', 403); }
    if (method === 'getManagedBotToken') return `synthetic-managed-${body.user_id}-v${this.version}`;
    if (method === 'getMe') return { id: 99, username: 'synthetic_master_bot' };
    if (method === 'sendMessage') { this.messages.push(body); return { message_id: ++this.sequence, chat: { id: body.chat_id }, text: body.text }; }
    if (['setWebhook', 'setMyCommands', 'setChatMenuButton', 'answerCallbackQuery'].includes(method)) return true;
    throw new Error(`Unscripted managed-bot method: ${method}`);
  }
  async request() {
    if (this.failure === 'logOut') { this.failure = undefined; throw new ApiError('synthetic migration refusal', 403); }
    return { ok: true };
  }
}
function fixture(t: any, pending = 2) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), owner: '123', management: '-1001', masterToken: 'master',
    callback: 'https://synthetic.invalid', account: 'stable-account', session: 's1', history: 0 };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  for (let i = 0; i < pending; i++) {
    const id = `${12025550100 + i}@c.us`;
    store.put('contact', id, { name: `Synthetic ${i}` });
    store.put('route', id, { chatId: id, tgChat: '-1001', thread: 100 + i, mode: 'inbox' });
    store.put('provision', id, { state: 'pending', chatId: id });
  }
  t.after(() => store.close()); return { store, api, bridge };
}
const info = { user: { id: 123 }, bot: { id: 42, username: 'synthetic_contact_bot' } };
const bind = (sender = '12025550100@c.us') => ({ type: 'operator_action', data: { operation: 'bind', sender, bot: '42' } });

test('managed registration preserves explicit contact choices through binding and private Start', async t => {
  const { store, bridge } = fixture(t);
  await bridge.telegram('master', { managed_bot: info }, 'registered');
  assert.equal(store.all('action').length, 2);
  assert.ok(store.all('contact').every((c: any) => !c.bot));
  await bridge.manage(bind(), 'bind');
  assert.equal(store.get('contact', '12025550100@c.us').bot, '42');
  assert.equal(store.get('bot', '42').ready, false);
  await bridge.telegram('42', { update_id: 1, message: { message_id: 1, text: '/start', from: { id: 123 }, chat: { id: 123, type: 'private' } } }, 'start');
  assert.equal(store.get('bot', '42').ready, true);
  assert.ok(store.all('contact').find((c: any) => c.id === '12025550101@c.us' && !c.bot));
});

test('managed registration rejects other users and unauthorized binding callbacks at ingress', async t => {
  const { store, bridge } = fixture(t);
  await bridge.registerBot({ ...info, user: { id: 999 } }, 'wrong-owner');
  assert.equal(store.all('bot').length, 0);
  await bridge.registerBot(info, 'registered');
  const action = store.all('action')[0];
  bridge.acceptTelegram('master', { update_id: 1, callback_query: { id: 'foreign', from: { id: 999 }, data: `bind:${action.id}`, message: { message_id: 10, chat: { id: 123, type: 'private' } } } });
  assert.ok(store.all('contact').every((c: any) => !c.bot));
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='tg'").get().n, 0);
});

test('managed token rotation keeps readiness and binding and does not reissue contact choices', async t => {
  const { store, bridge, api } = fixture(t);
  await bridge.registerBot(info, 'registered'); await bridge.manage(bind(), 'bind');
  store.put('bot', '42', { ...store.get('bot', '42'), ready: true });
  const before = store.get('bot', '42').token;
  api.version = 2;
  await bridge.registerBot({ ...info, bot: { ...info.bot, username: 'renamed_bot' } }, 'rotated');
  assert.notEqual(store.get('bot', '42').token, before);
  assert.equal(store.unseal(store.get('bot', '42').token), 'synthetic-managed-42-v2');
  assert.equal(store.get('bot', '42').ready, true);
  assert.equal(store.get('contact', '12025550100@c.us').bot, '42');
  assert.equal(store.all('action').length, 2);
});

test('managed registration caps visible choices at twenty while retaining all pending requests', async t => {
  const { store, bridge, api } = fixture(t, 22);
  await bridge.registerBot(info, 'registered');
  assert.equal(store.all('action').length, 22);
  assert.equal(store.all('provision').length, 22);
  // The transcript captures the exact visible keyboard; retained action rows
  // deliberately include choices beyond the visible cap in the original.
  assert.equal(store.all('bot').length, 1);
  assert.equal(api.messages[0].reply_markup.inline_keyboard.length, 20);
});

for (const failure of ['getManagedBotToken', 'logOut', 'setWebhook', 'setMyCommands']) {
  test(`managed registration resumes original partial setup after ${failure} refusal`, async t => {
    const { store, bridge, api } = fixture(t);
    api.failure = failure;
    await assert.rejects(() => bridge.registerBot(info, 'registration'), (e: any) => e.status === 403);
    assert.equal(store.all('bot').length, failure === 'getManagedBotToken' ? 0 : 1);
    await bridge.registerBot(info, 'registration');
    assert.ok(store.get('telegram-menu', '42'));
    // Existing original behavior suppresses the binding prompt when the bot
    // row survived a prior partial registration. Record rather than change it.
    assert.equal(store.all('action').length, failure === 'getManagedBotToken' ? 2 : 0);
  });
}

test('managed binding rejects consumed requests and bots already bound to another contact', async t => {
  const { store, bridge } = fixture(t);
  await bridge.registerBot(info, 'registered'); await bridge.manage(bind(), 'bind');
  await assert.rejects(() => bridge.manage(bind(), 'repeat'), (e: any) => e.status === 400);
  await assert.rejects(() => bridge.manage(bind('12025550101@c.us'), 'rebind'), (e: any) => e.status === 400);
  assert.equal(store.get('provision', '12025550101@c.us').state, 'pending');
});
