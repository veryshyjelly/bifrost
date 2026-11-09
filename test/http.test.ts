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
import { Api } from '../src/api.ts';

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
    else if (/getMyDescription|getUserProfilePhotos/.test(req.url || '')) { res.statusCode = 503; res.end(JSON.stringify({ ok: false })); }
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
  for (const method of ['getMyDescription', 'getUserProfilePhotos']) await assert.rejects(api.tg('private-token', method), (e: any) => e.retry && !e.uncertain);
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
