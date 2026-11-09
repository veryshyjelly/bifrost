import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Bridge } from '../src/bridge.ts';
import { Api, ApiError } from '../src/api.ts';
import { Store } from '../src/store.ts';
import { configuration } from '../src/config.ts';

class Fixture extends Api {
  calls: any[] = []; sequence = 100; messages = new Map<string, any>(); owned = new Set<string>(); blocked = new Set<number>(); failure?: ApiError; deleteFailure?: ApiError;
  key(token: string, chat: string, id: number) { return `${String(chat).startsWith('-') ? '*' : token}:${chat}:${id}`; }
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
    if (this.failure && method === 'editMessageReplyMarkup') { const e = this.failure; this.failure = undefined; throw e; }
    const key = this.key(token, body.chat_id, body.message_id);
    if (method === 'editMessageReplyMarkup') {
      if (this.owned.has(key) || !this.messages.has(key)) throw new ApiError('Cannot edit this message', 400);
      this.messages.get(key).reply_markup = body.reply_markup; return this.messages.get(key);
    }
    if (method === 'sendMessage') {
      if (body.reply_parameters && !this.messages.has(this.key(token, body.chat_id, body.reply_parameters.message_id))) throw new ApiError('Reply target is gone', 400);
      const message = { message_id: ++this.sequence, date: Math.floor(Date.now() / 1000), chat: { id: body.chat_id, type: String(body.chat_id).startsWith('-') ? 'supergroup' : 'private' }, text: body.text, reply_markup: body.reply_markup };
      this.messages.set(this.key(token, body.chat_id, message.message_id), message); return message;
    }
    if (method === 'deleteMessages') {
      if (this.deleteFailure) { const e = this.deleteFailure; this.deleteFailure = undefined; throw e; }
      if (body.message_ids.some((id: number) => this.blocked.has(id))) throw new ApiError('Deletion refused', 400);
      for (const id of body.message_ids) this.messages.delete(this.key(token, body.chat_id, id));
    }
    return true;
  }
  async ow() { throw new Error('Deletion choices must never contact WhatsApp'); }
}
function fixture(t: any) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), masterToken: 'master', owner: '123', management: '-1001', openwaKey: 'key', session: 's1', history: 0 };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  store.put('bot', '42', { token: store.seal('child'), username: 'alice_bot' });
  t.after(() => store.close()); return { bridge, store, api };
}
function copy(f: ReturnType<typeof fixture>, id = 5, options: any = {}) {
  const { bridge, store, api } = f, bot = options.bot || 'master', chat = options.chat || '-200';
  const token = bot === 'master' ? 'master' : 'child';
  const m = { message_id: id, date: Math.floor(Date.now() / 1000), chat: { id: chat, type: chat.startsWith('-') ? 'supergroup' : 'private' }, text: 'Original message', ...options.message };
  api.messages.set(api.key(token, chat, id), m);
  if (options.out) api.owned.add(api.key(token, chat, id));
  store.map('g@g.us', 'wa-original', chat, id, bot, options.out ? 'out' : 'in', !!options.media);
  bridge.control.track(bot, m, 'chat'); return m;
}
const revoke = (b: Bridge, key = 'revoke') => b.waChange({ event: 'message.revoked', data: { chatId: 'g@g.us', revokedId: 'wa-original' } }, key);
function query(task: any, id: string, choice: string, options: any = {}) {
  return { id: 'query-' + choice, from: { id: 123 }, data: 'rev-' + choice + ':' + id,
    message: { message_id: task.mode === 'reply' ? task.prompt : task.primary.tg_id, chat: { id: task.primary.tg_chat } }, ...options };
}
async function drain(b: Bridge) {
  for (let n = 0; n < 200; n++) {
    if (!Number(b.store.db.prepare("SELECT count(*) AS n FROM jobs WHERE state='pending' AND due<=?").get(Date.now())!.n)) return;
    await b.tick();
  }
  throw new Error('Deletion jobs did not drain');
}

test('a deleted owner message gets Delete/Keep without overwriting or deleting its Telegram content', async t => {
  const f = fixture(t); copy(f, 5, { out: true });
  await revoke(f.bridge);
  const task = f.store.all('revocation')[0]; assert.equal(task.status, 'pending'); assert.equal(task.mode, 'reply');
  const prompt = f.api.calls.find(c => c.method === 'sendMessage');
  assert.deepEqual(prompt.body.reply_markup.inline_keyboard[0].map((b: any) => b.text), ['Delete', 'Keep']);
  assert.equal(prompt.body.reply_parameters.message_id, 5);
  assert.equal(f.api.messages.get('*:-200:5').text, 'Original message');
  assert.ok(!f.api.calls.some(c => ['editMessageText', 'editMessageCaption', 'deleteMessages'].includes(c.method)));
});

test('Keep removes the fallback notice and preserves the original, including after late edits/history', async t => {
  const f = fixture(t); copy(f, 5, { out: true }); await revoke(f.bridge);
  const task = f.store.all('revocation')[0];
  assert.equal(f.bridge.acceptTelegram('master', { update_id: 1, callback_query: query(task, task.id, 'keep') }), true);
  await drain(f.bridge);
  assert.equal(f.store.get('revocation', task.id).status, 'kept');
  assert.ok(f.api.messages.has('*:-200:5')); assert.ok(!f.api.messages.has(`*:-200:${task.prompt}`));
  const count = f.api.calls.length;
  await f.bridge.waChange({ event: 'message.edited', data: { chatId: 'g@g.us', id: 'wa-original', body: 'Late change' } }, 'late-edit');
  await f.bridge.whatsApp({ event: 'message.received', data: { chatId: 'g@g.us', id: 'wa-original', isGroup: true, from: '111@c.us', type: 'text', body: 'History' } }, 'history');
  assert.equal(f.api.calls.length, count); assert.equal(f.store.get('revoked', 'g@g.us:wa-original'), true);
});

test('own phone-originated messages get inline choices; Keep and expiration remove only the buttons', async t => {
  const f = fixture(t); copy(f); f.store.put('wamessage', 'g@g.us:wa-original', { fromMe: true });
  await revoke(f.bridge);
  const task = f.store.all('revocation')[0]; assert.equal(task.mode, 'inline');
  assert.deepEqual(f.api.messages.get('*:-200:5').reply_markup.inline_keyboard[0].map((b: any) => b.text), ['Delete', 'Keep']);
  await f.bridge.revocations.callback('master', query(task, task.id, 'keep')); await drain(f.bridge);
  assert.equal(f.api.messages.get('*:-200:5').text, 'Original message');
  assert.deepEqual(f.api.messages.get('*:-200:5').reply_markup.inline_keyboard, []);
  assert.ok(!f.api.calls.some(c => c.method === 'deleteMessages' || c.method === 'sendMessage'));
});

test('Delete removes every text/media part in the chosen destination while preserving other copies and mappings', async t => {
  const f = fixture(t); copy(f); copy(f, 6, { media: true }); copy(f, 7, { chat: '-201' });
  f.store.put('wamessage', 'g@g.us:wa-original', { fromMe: true }); await revoke(f.bridge);
  const task = f.store.all('revocation').find(x => x.primary.tg_chat === '-200')!;
  assert.equal(task.primary.tg_id, 6);
  await f.bridge.revocations.callback('master', query(task, task.id, 'delete')); await drain(f.bridge);
  assert.ok(!f.api.messages.has('*:-200:5')); assert.ok(!f.api.messages.has('*:-200:6')); assert.ok(f.api.messages.has('*:-201:7'));
  assert.equal(f.store.byWhatsApp('g@g.us', 'wa-original').length, 3);
  assert.equal(f.store.get('revocation', task.id).status, 'deleted');
});

test('other people’s deleted text/media copies are removed automatically without a replacement or choices', async t => {
  const f = fixture(t); copy(f); copy(f, 6, { media: true }); await revoke(f.bridge); await drain(f.bridge);
  assert.ok(!f.api.messages.has('*:-200:5')); assert.ok(!f.api.messages.has('*:-200:6'));
  assert.ok(!f.api.calls.some(c => ['sendMessage', 'editMessageText', 'editMessageCaption', 'editMessageReplyMarkup'].includes(c.method)));
  assert.equal(f.store.all('revocation')[0].status, 'deleted');
});

test('duplicate events and conflicting clicks cannot reopen or reverse a completed choice', async t => {
  const f = fixture(t); copy(f); f.store.put('wamessage', 'g@g.us:wa-original', { fromMe: true });
  await revoke(f.bridge); await revoke(f.bridge, 'duplicate');
  const task = f.store.all('revocation')[0];
  assert.equal(f.api.calls.filter(c => c.method === 'editMessageReplyMarkup').length, 1);
  await f.bridge.revocations.callback('master', query(task, task.id, 'keep'));
  await f.bridge.revocations.callback('master', query(task, task.id, 'delete')); await drain(f.bridge);
  await revoke(f.bridge, 'after-choice');
  assert.equal(f.store.all('revocation').length, 1); assert.equal(f.store.get('revocation', task.id).status, 'kept');
  assert.ok(f.api.messages.has('*:-200:5'));
});

test('choices validate owner, bot, destination, message, session and expiry before changing content', async t => {
  const f = fixture(t); copy(f); f.store.put('wamessage', 'g@g.us:wa-original', { fromMe: true }); await revoke(f.bridge);
  const task = f.store.all('revocation')[0];
  for (const [bot, options] of [['master', { from: { id: 999 } }], ['42', {}], ['master', { message: { message_id: 5, chat: { id: -201 } } }], ['master', { message: { message_id: 6, chat: { id: -200 } } }]] as any) {
    await f.bridge.revocations.callback(bot, query(task, task.id, 'delete', options));
  }
  f.store.put('revocation', task.id, { ...task, expires: Date.now() - 1 });
  await f.bridge.revocations.callback('master', query(task, task.id, 'delete'));
  f.store.put('revocation', task.id, task); f.bridge.c.session = 'another-session';
  await f.bridge.revocations.callback('master', query(task, task.id, 'delete'));
  assert.ok(!f.api.calls.some(c => c.method === 'deleteMessages')); assert.ok(f.api.messages.has('*:-200:5'));
});

test('expiration defaults to keeping the message and never deletes the original', async t => {
  const f = fixture(t); copy(f); f.store.put('wamessage', 'g@g.us:wa-original', { fromMe: true }); await revoke(f.bridge);
  const task = f.store.all('revocation')[0];
  await f.bridge.revocations.process({ payload: { id: task.id, step: 'expire' } });
  assert.equal(f.store.get('revocation', task.id).status, 'kept'); assert.ok(f.api.messages.has('*:-200:5'));
  assert.ok(!f.api.calls.some(c => c.method === 'deleteMessages'));
});

test('member-bot private copies keep their bot-scoped identity even when Telegram message IDs overlap', async t => {
  const f = fixture(t); copy(f, 5, { chat: '123', bot: 'master', out: true }); copy(f, 5, { chat: '123', bot: '42', out: true });
  await revoke(f.bridge);
  const task = f.store.all('revocation').find(x => x.noticeBot === '42')!;
  await f.bridge.revocations.callback('master', query(task, task.id, 'delete'));
  assert.equal(f.store.get('revocation', task.id).status, 'pending');
  await f.bridge.revocations.callback('42', query(task, task.id, 'delete')); await drain(f.bridge);
  assert.ok(!f.api.messages.has('child:123:5')); assert.ok(f.api.messages.has('master:123:5'));
});

test('a Telegram deletion refusal preserves the remaining copy and produces no destructive content replacement', async t => {
  const f = fixture(t); copy(f); copy(f, 6, { media: true }); f.api.blocked.add(5);
  f.store.put('wamessage', 'g@g.us:wa-original', { fromMe: true }); await revoke(f.bridge);
  const task = f.store.all('revocation')[0];
  await f.bridge.revocations.callback('master', query(task, task.id, 'delete')); await drain(f.bridge);
  assert.equal(f.store.get('revocation', task.id).status, 'partial');
  assert.ok(f.api.messages.has('*:-200:5')); assert.ok(!f.api.messages.has('*:-200:6'));
  assert.ok(!f.api.calls.some(c => ['editMessageText', 'editMessageCaption'].includes(c.method)));
});

test('an ambiguous keyboard update safely retries without pausing as an uncertain send', async t => {
  const f = fixture(t); copy(f); f.store.put('wamessage', 'g@g.us:wa-original', { fromMe: true });
  f.api.failure = new ApiError('timeout', 0, false, true);
  f.bridge.acceptWhatsApp({ sessionId: 's1', idempotencyKey: 'r1', event: 'message.revoked', data: { chatId: 'g@g.us', revokedId: 'wa-original' } });
  await f.bridge.tick(); assert.equal(f.store.db.prepare("SELECT state FROM jobs WHERE type='wa'").get()!.state, 'pending');
  f.store.db.exec("UPDATE jobs SET due=0 WHERE type='wa'"); await f.bridge.tick();
  assert.equal(f.store.db.prepare("SELECT state FROM jobs WHERE type='wa'").get()!.state, 'done');
  assert.equal(f.store.all('revocation').length, 1);
});

test('saved WhatsApp phone events identify the owner for messages predating the fromMe metadata', async t => {
  const f = fixture(t); copy(f);
  f.store.enqueue('old-phone-event', 'old', 'wa', { event: 'message.sent', data: { chatId: 'g@g.us', id: 'wa-original', fromMe: true } });
  f.store.db.exec("UPDATE jobs SET state='done' WHERE dedup='old-phone-event'");
  await revoke(f.bridge);
  assert.equal(f.store.all('revocation')[0].own, true); assert.equal(f.store.all('revocation')[0].status, 'pending');
});

test('fallback prompt expiration keeps the owner’s original message', async t => {
  const f = fixture(t); copy(f, 5, { out: true }); await revoke(f.bridge);
  f.store.db.exec("UPDATE jobs SET due=0 WHERE type IN ('control-revoke','control-expire')"); await drain(f.bridge);
  assert.ok(f.api.messages.has('*:-200:5'));
  assert.equal(f.store.all('revocation')[0].status, 'kept');
});

test('a timed-out automatic deletion retries safely without an uncertain message send', async t => {
  const f = fixture(t); copy(f); await revoke(f.bridge);
  f.api.deleteFailure = new ApiError('timeout', 0, false, true); await f.bridge.tick();
  assert.equal(f.store.db.prepare("SELECT state FROM jobs WHERE type='control-revoke'").get()!.state, 'pending');
  f.store.db.exec("UPDATE jobs SET due=0 WHERE type='control-revoke'"); await drain(f.bridge);
  assert.equal(f.store.all('revocation')[0].status, 'deleted');
  assert.equal(f.store.db.prepare("SELECT count(*) AS n FROM effects WHERE state='uncertain'").get()!.n, 0);
});

test('a timed-out callback acknowledgement retries without blocking the conversation as uncertain', async t => {
  const f = fixture(t); copy(f); f.store.put('wamessage', 'g@g.us:wa-original', { fromMe: true }); await revoke(f.bridge);
  const task = f.store.all('revocation')[0], tg = f.api.tg.bind(f.api); let fail = true;
  f.api.tg = async (token, method, body) => {
    if (method === 'answerCallbackQuery' && fail) { fail = false; throw new ApiError('timeout', 0, false, true); }
    return tg(token, method, body);
  };
  f.bridge.acceptTelegram('master', { update_id: 1, callback_query: query(task, task.id, 'keep') }); await f.bridge.tick();
  assert.equal(f.store.db.prepare("SELECT state FROM jobs WHERE type='tg'").get()!.state, 'pending');
  f.store.db.exec("UPDATE jobs SET due=0 WHERE type='tg'"); await drain(f.bridge);
  assert.equal(f.store.get('revocation', task.id).status, 'kept'); assert.ok(f.api.messages.has('*:-200:5'));
});
