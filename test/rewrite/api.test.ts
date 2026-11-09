import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { root } from '../../scripts/rewrite/oracle.ts';

const scenarios: any[] = [];
for (const method of ['sendMessage', 'getMe', 'getMyDescription', 'getManagedBotToken', 'getCustomEmojiStickers']) {
  for (const status of [200, 400, 403, 404, 413, 422, 429, 500, 501, 503]) scenarios.push({
    adapter: 'tg', args: ['synthetic-token', method, { chat_id: '-200', text: '😀', entities: [], absent: undefined, present: null }], status,
    response: status === 200 ? { ok: true, result: { message_id: 99 } } : { ok: false, parameters: { retry_after: 2 }, description: 'private provider detail' },
  });
}
for (const response of [
  { ok: false, error_code: 429 }, { ok: false, error_code: 500 }, { ok: false, code: 'IDEMPOTENCY_OUTCOME_UNKNOWN', error_code: 409 },
  { ok: false, error_code: 400, description: 'Bad Request: message thread not found: private details' },
  { ok: false, error_code: 400, description: 'Replied message not found: private details' },
  { ok: false, error_code: 400, description: 'Message to edit not found: private details' },
]) scenarios.push({ adapter: 'tg', args: ['synthetic-token', 'sendMessage'], response });
for (const transport of [{ code: 'ECONNREFUSED' }, { code: 'ENOTFOUND' }, { code: 'EAI_AGAIN' }, { code: 'ETIMEDOUT' }, {}, { errors: [{ code: 'ECONNREFUSED' }, { code: 'ENOTFOUND' }] }, { errors: [{ code: 'ECONNREFUSED' }, { code: 'ETIMEDOUT' }] }, { errors: [] }]) {
  for (const method of ['sendMessage', 'getMe']) scenarios.push({ adapter: 'tg', args: ['synthetic-token', method], transport });
}
for (const method of ['sendMessage', 'getMe']) scenarios.push({ adapter: 'tg', args: ['synthetic-token', method], invalid: true });
scenarios.push(
  { adapter: 'tg', args: ['synthetic-token', 'sendMessage'], status: 429, headers: { 'retry-after': '7' }, response: { ok: false } },
  { adapter: 'ow', args: ['messages'] },
  { adapter: 'ow', args: ['messages/send-text', { body: '😀', to: '12025550123@c.us', quotedMessageId: null }, 'stable-idempotency'] },
  { adapter: 'ow', args: ['messages/edit', { body: 'edited', mentions: [] }, 'stable-key', 'PUT'] },
  { adapter: 'ow', args: ['messages/send-text', { body: 'hi' }, 'key'], transport: { code: 'ETIMEDOUT' } },
  { adapter: 'upload', args: ['synthetic-token', 'sendDocument', 'document', '/synthetic', 'fixture.bin', { chat_id: '-200', caption: '😀', caption_entities: [{ type: 'bold', offset: 0, length: 2 }], empty: null, false: false, zero: 0 }] },
);

test('ClojureScript adapters match frozen wire requests and conservative delivery error classification', t => {
  const dir = mkdtempSync(join(tmpdir(), 'bifrost-rewrite-api-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const file = join(dir, 'fixture.bin');
  writeFileSync(file, 'synthetic-file-😀');
  scenarios.at(-1).args[3] = file;
  function replay(candidate: boolean) {
    const command = candidate ? ['x', '--bun', '--no-install', 'nbb', 'scripts/rewrite/api.cljs'] : ['scripts/rewrite/original-api.ts'];
    const result = Bun.spawnSync([process.execPath, ...command], { cwd: root, env: { PATH: process.env.PATH!, TZ: 'UTC' }, stdin: Buffer.from(JSON.stringify(scenarios)) });
    assert.equal(result.exitCode, 0, result.stderr.toString());
    return JSON.parse(result.stdout.toString());
  }
  const expected = replay(false), actual = replay(true);
  assert.equal(actual.length, scenarios.length);
  for (let i = 0; i < scenarios.length; i++) assert.deepEqual(actual[i], expected[i], `Transport scenario ${i}`);
  assert.equal(actual[6].error.retry, true, 'documented 429 refusal can retry');
  assert.equal(actual[7].error.uncertain, true, 'mutating 500 outcome must remain uncertain');
  assert.equal(actual.at(-2).error.uncertain, true, 'timeout after send must remain uncertain');
  assert.ok(actual.every((r: any) => !r.error?.message.includes('private')), 'provider descriptions must stay private');
});
