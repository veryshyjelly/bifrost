import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHmac } from 'node:crypto';
import { root, oracle } from '../../scripts/rewrite/oracle.ts';

const { configuration } = await import(`${oracle()}/src/config.ts`);
const config = { ...configuration({}), key: 'aa'.repeat(32), admin: 'synthetic-admin'.repeat(4), webhookSecret: 'synthetic-webhook'.repeat(4),
  owner: '123', management: '-1001', masterToken: 'master-token', account: 'stable-account', session: 's1', openwaKey: 'synthetic-openwa-key' };
const now = 1794240000000;
const wa = (id: string, extra = {}) => ({ sessionId: 's1', event: 'message.received', idempotencyKey: id, data: { id, chatId: '12025550123@c.us', type: 'text', body: '😀' }, ...extra });
const message = (id: number, extra = {}) => ({ message_id: id, from: { id: 123 }, chat: { id: -1001, type: 'supergroup' }, message_thread_id: 101, text: 'Hello', ...extra });
const tg = (id: number, extra = {}) => ({ update_id: id, message: message(id), ...extra });
const put = (kind: string, id: string, value: any) => ({ op: 'put', args: [kind, id, value] });
const post = (bot: string, body: any) => ({ path: `/webhooks/telegram/${bot}`, bot, body });
const http = (path: string, body: any, extra = {}) => ({ path, body, ...extra });
const signedAction = (body: any) => ({ ...body, signature: createHmac('sha256', config.admin).update(JSON.stringify(body)).digest('hex') });

function compare(steps: any[]) {
  function replay(candidate: boolean) {
    const args = candidate ? ['x', '--bun', '--no-install', 'nbb', 'scripts/rewrite/ingress.cljs'] : ['scripts/rewrite/original-ingress.ts'];
    const result = Bun.spawnSync([process.execPath, ...args], { cwd: root, env: { PATH: process.env.PATH!, TZ: 'UTC' },
      stdin: Buffer.from(JSON.stringify({ config, now, steps })), timeout: 20000 });
    assert.equal(result.exitCode, 0, result.stderr.toString());
    return JSON.parse(result.stdout.toString());
  }
  const actual = replay(true), expected = replay(false);
  assert.equal(actual.length, steps.length);
  for (let i = 0; i < steps.length; i++) assert.deepEqual(actual[i], expected[i], `Ingress transition ${i}: ${steps[i].path || steps[i].op}`);
  return actual;
}

test('raw HTTP authentication, size caps, status and ignored/malformed events match the frozen oracle', () => {
  const event = wa('raw');
  const raw = ' \n' + JSON.stringify(event);
  const action = { id: 'action-1', type: 'mark_read', data: { chatId: '12025550123@c.us' }, at: now };
  const signed = signedAction(action);
  const results = compare([
    http('/healthz', {}, { method: 'GET' }), http('/v1/status', {}, { method: 'GET' }), http('/v1/status', {}, { method: 'GET', admin: true }),
    http('/unknown', {}), http('/webhooks/openwa', {}, { method: 'GET' }),
    http('/webhooks/openwa', event), http('/webhooks/openwa', event, { openwa: true }),
    http('/webhooks/openwa', event, { openwa: true }),
    http('/webhooks/openwa', {}, { raw, signedRaw: JSON.stringify(event), openwa: true, chunkSize: 7 }),
    http('/webhooks/openwa', {}, { raw, openwa: true, chunkSize: 3 }),
    http('/webhooks/openwa', wa('bad-session', { sessionId: 'other' }), { openwa: true }),
    http('/webhooks/openwa', wa('bad-type', { event: 'message.unknown' }), { openwa: true }),
    http('/webhooks/openwa', wa('bad-id', { idempotencyKey: '' }), { openwa: true }),
    http('/webhooks/openwa', {}, { raw: '{', openwa: true }),
    ...[null, [], 1, true, 'text'].map(body => http('/webhooks/openwa', body, { raw: JSON.stringify(body), openwa: true })),
    http('/webhooks/openwa', {}, { raw: 'x'.repeat(2 * 1024 * 1024), openwa: true, chunkSize: 8191 }),
    http('/webhooks/openwa', {}, { raw: 'x'.repeat(2 * 1024 * 1024 + 1), openwa: true, chunkSize: 8191 }),
    http('/v1/management', signed), http('/v1/management', signed),
    http('/v1/management', { ...signed, at: now - 7 * 86400000 - 1 }),
    http('/v1/management', { ...action, signature: 'bad' }),
    post('unknown', tg(1)), http('/webhooks/telegram/master', tg(1)),
    post('master', tg(1, { message: message(1, { from: { id: 999 } }) })),
    post('master', tg(2, { message: message(2, { from: { id: 123, is_bot: true } }) })),
    post('master', tg(3)), post('master', tg(3)), post('master', tg(1.5)),
    http('/v1/status', {}, { method: 'GET', admin: true }),
    ...[-7 * 86400000 - 1, -7 * 86400000, 7 * 86400000, 7 * 86400000 + 1].map(offset =>
      http('/v1/management', signedAction({ ...action, id: `boundary:${offset}`, at: now + offset }))),
    http('/v1/management', signedAction({ type: 'mark_read', data: action.data, at: now })),
    http('/v1/management', signedAction({ id: null, type: 'mark_read', data: action.data, at: now })),
  ]);
  assert.equal(results[5].status, 401); assert.equal(results[6].body.accepted, true); assert.equal(results[7].body.accepted, false);
  assert.equal(results[8].status, 401, 'a signature over parsed/canonical JSON cannot authenticate different raw bytes');
  assert.equal(results[20].status, 413);
});

test('Telegram admission preserves observation before owner checks, bot scope, exact topics and whole-group routing', () => {
  compare([
    put('route', 'topic@g.us', { chatId: 'topic@g.us', tgChat: '-1001', mode: 'inbox', thread: 101 }),
    put('route', 'mirror@g.us', { chatId: 'mirror@g.us', tgChat: '-200', mode: 'group' }),
    put('route', 'retired@c.us', { chatId: 'retired@c.us', tgChat: '-1001', mode: 'dedicated', inboxRetired: true }),
    put('bot', '42', { username: 'Alice_bot', ready: true }), put('bot', '43', { username: 'Bob_bot', ready: true }),
    put('contact', '12025550123@c.us', { name: 'Alice', bot: '42' }),
    put('route', '12025550123@c.us', { chatId: '12025550123@c.us', tgChat: '-1001', mode: 'dedicated', thread: 102 }),
    ...[
      ['master', message(1)], ['master', message(2, { message_thread_id: 102 })],
      ['master', message(3, { message_thread_id: 0 })],
      ['master', message(4, { chat: { id: -200, type: 'supergroup' }, message_thread_id: 777 })],
      ['42', message(5, { chat: { id: 123, type: 'private' } })],
      ['43', message(6, { chat: { id: 123, type: 'private' } })],
    ].map(args => ({ op: 'context', args })),
    post('master', tg(1, { message: message(1, { from: { id: 999 } }) })),
    post('master', tg(2, { message: message(2, { from: { id: 42, is_bot: true } }) })),
    post('42', tg(3)), post('42', tg(4, { message: message(4, { text: '/start@ALICE_BOT' }) })),
    post('42', tg(5, { message: message(5, { text: '/start@another_bot' }) })),
    post('42', tg(6, { message: message(6, { chat: { id: 123, type: 'private' } }) })),
    post('43', tg(6, { message: message(6, { chat: { id: 123, type: 'private' } }) })),
    post('master', { update_id: 7, message_reaction: { chat: { id: -200, type: 'supergroup' }, message_id: 1, user: { id: 123 } } }),
    post('42', { update_id: 8, callback_query: { id: 'cb', from: { id: 123 }, data: 'old:unknown', message: message(8) } }),
    post('master', { update_id: 9, poll_answer: { poll_id: 'poll', user: { id: 123 }, option_ids: [0] } }),
    post('42', { update_id: 10, my_chat_member: { from: { id: 123 }, chat: { id: -200, type: 'supergroup' } } }),
    post('master', tg(11, { message: message(11, { chat: {} }) })),
    ...[' /history@BOT  50 all ', '/bad-command', '/start\nhello', '', '/sync ', '/start@bot trailing\ntext'].map(text => ({ op: 'command', args: [{ text }] })),
  ]);
});

test('explicit review scopes uncertain effects, preserves done receipts and requires confirmation before retry', () => {
  const payload = JSON.stringify({ bot: '42', update: { message: { message_id: 5, chat: { id: 123, type: 'private' } } } });
  compare([
    { op: 'sql', args: [`INSERT INTO jobs(dedup,stream,type,payload,state,created) VALUES('tg:42:99','s','tg','${payload}','uncertain',${now});
      INSERT INTO effects VALUES('tg:42:99','uncertain',NULL),('tg:42:99:text:0','uncertain',NULL),('tg:42:990','uncertain',NULL),
        ('tg:42:99:done','done','{"message_id":1}'),('send:42:123:5','uncertain',NULL),('send:43:123:5','uncertain',NULL);`] },
    http('/v1/review', { job: 1, action: 'retry' }),
    http('/v1/review', { job: 1, action: 'retry' }, { admin: true }),
    http('/v1/review', { job: 1, action: 'retry', confirmNoDelivery: 'true' }, { admin: true }),
    http('/v1/review', { job: 1, action: 'retry', confirmNoDelivery: true }, { admin: true }),
    http('/v1/review', { job: 1, action: 'skip' }, { admin: true }),
    { op: 'sql', args: ["UPDATE jobs SET state='failed' WHERE id=1"] },
    http('/v1/review', { job: 1, action: 'skip' }, { admin: true }),
    http('/v1/review', { job: 999, action: 'skip' }, { admin: true }),
  ]);
});
