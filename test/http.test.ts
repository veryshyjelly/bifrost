import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { once } from 'node:events';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Bridge } from '../src/bridge.ts';
import { Store } from '../src/store.ts';
import { configuration, sign } from '../src/config.ts';
import { handler } from '../src/server.ts';
import { Api, ApiError } from '../src/api.ts';

test('Telegram refusals expose safe reason codes without logging provider descriptions', async t => {
  let description = 'Bad Request: message thread not found';
  const server = createServer((req, res) => { req.resume(); res.writeHead(400, { 'content-type': 'application/json' }); res.end(JSON.stringify({ ok: false, description })); });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(async () => { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); });
  const api = new Api({ ...configuration({}), telegram: `http://127.0.0.1:${(server.address() as any).port}` });
  for (const [text, reason] of [['message thread not found', 'thread_missing'], ['message to react not found', 'message_missing'], ['message to be replied not found', 'reply_missing']]) {
    description = 'Bad Request: ' + text + ' private-provider-detail';
    await assert.rejects(api.tg('fixture-token', 'sendMessage', {}), (error: any) => error.reason === reason && error.service === 'telegram' && !error.message.includes('private-provider-detail'));
  }
});

test('real HTTP ingress authenticates raw OpenWA bytes, Telegram secrets and signed management actions', async t => {
  const c = { ...configuration({}), key: 'aa'.repeat(32), admin: 'admin'.repeat(8), webhookSecret: 'secret'.repeat(8), owner: '123', session: 's1' };
  const s = new Store(':memory:', c.key), bridge = new Bridge(c, s), server = createServer(handler(bridge));
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const base = `http://127.0.0.1:${(server.address() as any).port}`;
  t.after(async () => { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); s.close(); });
  const event = JSON.stringify({ sessionId: 's1', event: 'message.received', idempotencyKey: 'first', data: { chatId: '111@c.us', id: 'm1' } }, null, 2);
  const post = (path: string, body: string, headers = {}) => fetch(base + path, { method: 'POST', headers: { 'content-type': 'application/json', ...headers }, body });
  assert.equal((await post('/webhooks/openwa', event)).status, 401);
  const signature = 'sha256=' + sign(event, c.webhookSecret);
  assert.deepEqual(await (await post('/webhooks/openwa', event, { 'x-openwa-signature': signature })).json(), { accepted: true });
  assert.deepEqual(await (await post('/webhooks/openwa', event, { 'x-openwa-signature': signature })).json(), { accepted: false });
  assert.equal((await post('/webhooks/openwa', event.replace('first', 'second'), { 'x-openwa-signature': signature })).status, 401);
  assert.equal((await post('/webhooks/telegram/master', '{}')).status, 401);
  assert.equal((await fetch(base + '/v1/status')).status, 401);
  const management = { id: 'new', type: 'new_conversation', data: { chatId: '111@c.us' }, at: Date.now() };
  assert.equal((await post('/v1/management', JSON.stringify({ ...management, signature: sign(JSON.stringify(management), c.admin) }))).status, 200);
  assert.equal((await post('/v1/management', JSON.stringify({ ...management, signature: 'bad' }))).status, 401);
  assert.equal((await post('/v1/review', '{}')).status, 401);
});

test('real API transport preserves JSON, quotes, idempotency and handles rate limits without leaking tokens', async t => {
  const requests: any[] = [];
  const server = createServer(async (req, res) => {
    let body = ''; for await (const chunk of req) body += chunk;
    requests.push({ url: req.url, method: req.method, key: req.headers['idempotency-key'], body: JSON.parse(body) });
    res.setHeader('content-type', 'application/json');
    if (req.url?.includes('sendMessage')) { res.statusCode = 429; res.end(JSON.stringify({ ok: false, parameters: { retry_after: 2 } })); }
    else if (/getMyDescription|getUserProfilePhotos|getCustomEmojiStickers/.test(req.url || '')) { res.statusCode = 503; res.end(JSON.stringify({ ok: false })); }
    else res.end(JSON.stringify({ messageId: 'sent-1' }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(async () => { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); });
  const base = `http://127.0.0.1:${(server.address() as any).port}`;
  const api = new Api({ ...configuration({}), openwa: base, telegram: base, session: 's1', openwaKey: 'private' });
  assert.equal((await api.ow('messages/reply', { chatId: 'a@c.us', text: 'Hello', quotedMessageId: 'q1' }, 'stable-key')).messageId, 'sent-1');
  assert.equal(requests[0].key, 'stable-key'); assert.equal(requests[0].body.quotedMessageId, 'q1');
  await api.ow('webhooks/own', { events: ['status.received'] }, undefined, 'PUT');
  assert.equal(requests[1].method, 'PUT'); assert.equal(requests[1].url, '/api/sessions/s1/webhooks/own');
  assert.deepEqual(requests[1].body, { events: ['status.received'] });
  await assert.rejects(api.tg('private-token', 'sendMessage', { text: 'hi' }), (e: any) => e.retry && !e.uncertain && e.delay === 2000 && !e.message.includes('private-token'));
  for (const method of ['getMyDescription', 'getUserProfilePhotos', 'getCustomEmojiStickers']) await assert.rejects(api.tg('private-token', method), (e: any) => e.retry && !e.uncertain);
});

test('real multipart transport preserves attachment bytes and caption entities', async t => {
  let received = Buffer.alloc(0), contentType = '';
  const root = await mkdtemp(join(tmpdir(), 'bridge-upload-')), path = join(root, 'sample.bin');
  const bytes = Buffer.from([0, 255, 13, 10, 65, 66, 128, 1]); await writeFile(path, bytes);
  const server = createServer(async (req, res) => {
    const chunks: Buffer[] = []; for await (const chunk of req) chunks.push(Buffer.from(chunk));
    received = Buffer.concat(chunks); contentType = String(req.headers['content-type']);
    res.setHeader('content-type', 'application/json'); res.end(JSON.stringify({ ok: true, result: { message_id: 1 } }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  t.after(async () => { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); await rm(root, { recursive: true, force: true }); });
  const api = new Api({ ...configuration({}), telegram: `http://127.0.0.1:${(server.address() as any).port}` });
  const entities = [{ type: 'bold', offset: 0, length: 4 }];
  const result = await api.upload('fixture-token', 'sendDocument', 'document', path, 'sample.bin', { chat_id: 123, caption: 'Name: 😀 <text>', caption_entities: entities });
  assert.equal(result.message_id, 1); assert.match(contentType, /^multipart\/form-data; boundary=/);
  assert.ok(received.includes(bytes)); assert.ok(received.includes(Buffer.from('filename="sample.bin"')));
  assert.ok(received.includes(Buffer.from('Name: 😀 <text>'))); assert.ok(received.includes(Buffer.from(JSON.stringify(entities))));
});

test('Telegram caption edits satisfy the OpenWA HTTP contract and a refused edit cannot stall later sends', async t => {
  const requests: any[] = []; let refusal = 0, sequence = 100;
  const server = createServer(async (req, res) => {
    let raw = ''; for await (const chunk of req) raw += chunk;
    const body = JSON.parse(raw || '{}'); requests.push({ path: req.url, body });
    res.setHeader('content-type', 'application/json');
    if (req.url?.endsWith('/messages/edit')) {
      const valid = typeof body.body === 'string' && body.body.length > 0 && !('text' in body);
      if (!valid || refusal) { res.statusCode = valid ? refusal : 400; res.end(JSON.stringify({ ok: false, description: 'private-refusal-detail' })); return; }
    }
    const result = req.url?.includes('/bot') ? { message_id: ++sequence, chat: { id: body.chat_id } } : { messageId: 'wa-' + (++sequence) };
    res.end(JSON.stringify({ ok: true, result, ...result }));
  });
  server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const base = `http://127.0.0.1:${(server.address() as any).port}`;
  const c = { ...configuration({}), key: 'aa'.repeat(32), masterToken: 'fixture', owner: '123', management: '-1001', session: 's1', openwa: base, telegram: base };
  const store = new Store(':memory:', c.key), bridge = new Bridge(c, store);
  t.after(async () => { server.closeAllConnections(); await new Promise<void>(resolve => server.close(() => resolve())); store.close(); });
  store.put('route', '111@c.us', { chatId: '111@c.us', tgChat: '-200', mode: 'group' });
  store.map('111@c.us', 'original', '-200', 1, 'master', 'out', true);
  const message = (id: number, extra = {}) => ({ message_id: id, from: { id: 123 }, chat: { id: -200, type: 'supergroup' }, ...extra });
  const edit = { update_id: 1, edited_message: message(1, { caption: 'Edited 😀 caption', photo: [{ file_id: 'unused' }] }) };
  assert.equal(bridge.acceptTelegram('master', edit), true); await bridge.tick();
  const changed = requests.find(r => r.path.endsWith('/messages/edit'));
  assert.deepEqual(changed.body, { chatId: '111@c.us', messageId: 'original', body: 'Edited 😀 caption', mentions: [] });
  assert.equal(store.get('outbound-edit', '111@c.us:original'), (await import('../src/config.ts')).digest('Edited 😀 caption'));
  for (const [index, status] of [400, 403, 404, 413, 422].entries()) {
    refusal = status;
    bridge.acceptTelegram('master', { ...edit, update_id: 2 + index * 2, edited_message: { ...edit.edited_message, caption: 'Refused edit ' + status } });
    bridge.acceptTelegram('master', { update_id: 3 + index * 2, message: message(2 + index, { text: 'Following message ' + status }) });
    for (let tick = 0; tick < 8; tick++) await bridge.tick();
  }
  assert.equal(requests.filter(r => r.path.endsWith('/messages/send-text')).length, 5);
  assert.equal(store.db.prepare("SELECT count(*) AS n FROM jobs WHERE type='tg' AND state!='done'").get()!.n, 0);
  assert.equal(store.all('edit-refusal').length, 5);
  for (let n = 0; n < 35; n++) await bridge.tick();
  const notices = requests.filter(r => r.path.endsWith('/sendMessage'));
  assert.equal(notices.length, 5); assert.ok(notices.every(r => r.body.reply_parameters.message_id === 1 && !r.body.text.includes('private-refusal-detail')));
  assert.ok(!requests.some(r => r.path.endsWith('/messages/send-image') || r.path.endsWith('/messages/send-document')));
});

test('edit transport keeps known rate refusals retryable and ambiguous mutations uncertain', async t => {
  const c = { ...configuration({}), key: 'ab'.repeat(32), masterToken: 'fixture', owner: '123', management: '-1001' };
  const store = new Store(':memory:', c.key), api = new Api(c), bridge = new Bridge(c, store, api); t.after(() => store.close());
  store.put('route', '111@c.us', { chatId: '111@c.us', tgChat: '-200', mode: 'group' });
  store.map('111@c.us', 'original', '-200', 1, 'master', 'out');
  const edit = { edited_message: { message_id: 1, from: { id: 123 }, chat: { id: -200, type: 'supergroup' }, text: 'Edited' } };
  for (const [index, error] of [new ApiError('Rate limited', 429, true), new ApiError('Unknown outcome', 503, false, true), new ApiError('Not authorized', 401)].entries()) {
    api.request = async () => { throw error; };
    await assert.rejects(bridge.telegram('master', edit, 'edit-' + index), e => e === error);
  }
  assert.equal(store.all('edit-refusal').length, 0);
  const count = store.db.prepare('SELECT count(*) AS n FROM effects').get()!.n;
  await bridge.telegram('master', { edited_message: { ...edit.edited_message, text: undefined, caption: '' } }, 'empty-caption');
  assert.equal(store.all('edit-refusal').length, 1); assert.equal(store.db.prepare('SELECT count(*) AS n FROM effects').get()!.n, count);
});
