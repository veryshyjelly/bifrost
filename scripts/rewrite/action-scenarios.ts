import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pathToFileURL } from 'node:url';
import { oracle } from './oracle.ts';

const baseline = oracle(), uri = (file: string) => pathToFileURL(`${baseline}/${file}`).href;
const { Bridge } = await import(uri('src/bridge.ts'));
const { Store } = await import(uri('src/store.ts'));
const { Api } = await import(uri('src/api.ts'));
const { configuration } = await import(uri('src/config.ts'));
const person = '12025550123@c.us';
class Fixture extends Api {
  calls: any[] = []; sequence = 100;
  async tg(_token: string, method: string, body: any = {}) {
    this.calls.push({ method, body });
    if (method === 'sendMessage') return { message_id: ++this.sequence, chat: { id: body.chat_id }, text: body.text };
    if (['editMessageText', 'editMessageCaption', 'answerCallbackQuery'].includes(method)) return true;
    throw new Error(`Unscripted action provider: ${method}`);
  }
  async ow(path: string, body: any) {
    this.calls.push({ method: path, body });
    if (path === 'messages/vote-poll') return { ok: true };
    throw new Error(`Unscripted action provider: ${path}`);
  }
}
function fixture(t: any, chatId = person) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), owner: '123', management: '-1001', masterToken: 'master',
    callback: 'https://synthetic.invalid', account: 'stable-account', session: 's1', history: 0 };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  store.put('route', chatId, { chatId, tgChat: '-200', mode: 'group' });
  t.after(() => store.close()); return { store, api, bridge };
}
const message = (id: string, body: string) => ({ event: 'message.received', data: { id, chatId: person, from: person, type: 'text', body,
  timestamp: 1700000000, contact: { pushName: 'Alice' } } });

test('candidate KEEP-001 characterizes mute then keep routing in a mirror without choosing a correction', async t => {
  const chatId = 'synthetic@g.us', { store, bridge } = fixture(t, chatId);
  await bridge.manage({ type: 'operator_action', data: { operation: 'mute', chatId } }, 'mute');
  assert.equal(store.get('route', chatId).mode, 'muted');
  await bridge.manage({ type: 'operator_action', data: { operation: 'keep', chatId } }, 'keep');
  assert.deepEqual(store.get('route', chatId), { chatId, tgChat: '-200', mode: 'inbox' });
});

test('candidate EDIT-001 characterizes the first edited text part applied to every split copy', async t => {
  const { store, bridge, api } = fixture(t);
  await bridge.whatsApp(message('split', 'A'.repeat(5000)), 'original');
  assert.equal(store.byWhatsApp(person, 'split').length, 2);
  await bridge.waChange({ event: 'message.edited', data: { chatId: person, id: 'split', body: 'B'.repeat(6000) } }, 'edit');
  const edits = api.calls.filter(c => c.method === 'editMessageText');
  assert.equal(edits.length, 2);
  assert.notEqual(edits[0].body.message_id, edits[1].body.message_id);
  assert.equal(edits[0].body.text, edits[1].body.text);
  assert.ok(edits[0].body.text.length <= 3800);
});

test('candidate CALLBACK-001 characterizes retained generic actions in a stale different destination', async t => {
  const { store, bridge } = fixture(t);
  store.put('action', 'stale-action', { chatId: person, sender: person, owner: '123', session: 'old-account', expires: Date.now() - 1 });
  const update = (id: number, operation: string) => ({ update_id: id, callback_query: { id: `callback-${id}`, from: { id: 123 }, data: `${operation}:stale-action`,
    message: { message_id: 99, chat: { id: -999, type: 'supergroup' }, message_thread_id: 77 } } });
  assert.equal(bridge.acceptTelegram('master', update(1, 'mute')), true);
  await bridge.tick(); await bridge.tick();
  assert.equal(store.get('route', person).mode, 'muted');
  assert.ok(store.get('action', 'stale-action'));
  assert.equal(bridge.acceptTelegram('master', update(2, 'keep')), true);
  await bridge.tick(); await bridge.tick();
  assert.equal(store.get('route', person).mode, 'inbox');
  assert.ok(store.get('action', 'stale-action'));
});

test('poll selections toggle single and multiple choices with independently keyed effects', async t => {
  const { store, bridge, api } = fixture(t);
  const vote = (choice: string, multiple: boolean, key: string) => bridge.manage({ type: 'operator_action', data: {
    operation: 'vote', chatId: person, message: 'poll', choice, multiple,
  } }, key);
  await vote('One', true, 'one'); await vote('Two', true, 'two'); await vote('One', true, 'clear-one');
  assert.deepEqual(store.get('poll-selection', `${person}:poll`), ['Two']);
  await vote('One', false, 'single');
  assert.deepEqual(store.get('poll-selection', `${person}:poll`), ['One']);
  assert.equal(api.calls.filter(c => c.method === 'messages/vote-poll').length, 4);
});

test('an owner poll_answer is admitted and completed without inventing WhatsApp forwarding', async t => {
  const { store, bridge, api } = fixture(t);
  assert.equal(bridge.acceptTelegram('master', { update_id: 1, poll_answer: { user: { id: 123 }, poll_id: 'poll', option_ids: [0] } }), true);
  await bridge.tick();
  assert.equal(store.db.prepare("SELECT state FROM jobs WHERE type='tg'").get().state, 'done');
  assert.equal(api.calls.length, 0);
});

test('late edits override original text while revoked copies and outbound edit echoes remain suppressed', async t => {
  const { store, bridge, api } = fixture(t);
  await bridge.waChange({ event: 'message.edited', data: { chatId: person, id: 'late', body: 'Edited before original' } }, 'late-edit');
  await bridge.whatsApp(message('late', 'Old body'), 'late-original');
  assert.ok(api.calls.find(c => c.method === 'sendMessage').body.text.includes('Edited before original'));
  const { digest } = await import(uri('src/config.ts'));
  store.put('outbound-edit', `${person}:late`, digest('Phone echo'));
  const count = api.calls.length;
  await bridge.waChange({ event: 'message.edited', data: { chatId: person, id: 'late', body: 'Phone echo' } }, 'echo');
  assert.equal(api.calls.length, count);
  store.put('revoked', `${person}:late`, true);
  await bridge.waChange({ event: 'message.edited', data: { chatId: person, id: 'late', body: 'After revoke' } }, 'after');
  assert.equal(api.calls.length, count);
});
