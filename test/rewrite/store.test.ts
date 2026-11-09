import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { root } from '../../scripts/rewrite/oracle.ts';

const key = 'aa'.repeat(32), now = 1794240000000;
type Step = { op: string; args: any[] };
const step = (op: string, ...args: any[]): Step => ({ op, args });
function replay(candidate: boolean, path: string, steps: Step[], recover = false) {
  const command = candidate ? ['x', '--bun', '--no-install', 'nbb', 'scripts/rewrite/store.cljs'] : ['scripts/rewrite/original-store.ts'];
  const result = Bun.spawnSync([process.execPath, ...command], {
    cwd: root, env: { PATH: process.env.PATH!, TZ: 'UTC' },
    stdin: Buffer.from(JSON.stringify({ path, key, now, steps, recover })),
  });
  assert.equal(result.exitCode, 0, result.stderr.toString());
  return JSON.parse(result.stdout.toString());
}

const scenarios: Record<string, Step[]> = {
  'JSON bytes, alias conflicts, stable account and bot-scoped mappings': [
    step('schema'), step('get', 'absent', 'id'),
    step('put', 'ordered', 'a', { z: null, b: 2, a: '😀' }), step('all', 'ordered'),
    step('put', 'ordered', 'a', { z: 3, a: null }), step('get', 'ordered', 'a'),
    step('canonical', '12025550123@s.whatsapp.net'), step('alias', '123@lid', '12025550123@c.us'),
    step('canonical', '123@lid'), step('alias', '123@lid', '12025550123@c.us'), step('alias', '123@lid', 'other@c.us'),
    step('account', 'stable-account', 'session-before'), step('account', '', 'session-after'), step('account', 'wrong-account', ''),
    step('map', 'a@c.us', 'wa-1', '123', 1, 'master', 'in'),
    step('map', 'b@c.us', 'wa-2', '123', 1, '42', 'in', true),
    step('map', 'a@c.us', 'wa-1', '-200', 2, 'master', 'in'),
    step('map', 'a@c.us', 'wa-1', '-200', 2, 'master', 'out'),
    step('byTelegram', '123', 1, 'master'), step('byTelegram', '123', 1, '42'),
    step('byTelegram', '-200', 2, '42'), step('byWhatsApp', 'a@c.us', 'wa-1'),
  ],
  'queue priority, due times, durable dedup and stream blockers': [
    step('enqueue', 'history', 'history', 'history-item', {}),
    step('enqueue', 'control', 'control', 'control-delete', {}),
    step('enqueue', 'sync', 'sync', 'sync-target', {}),
    step('enqueue', 'status', 'status', 'status-refresh', {}),
    step('enqueue', 'first', 'a', 'tg', { a: null }), step('enqueue', 'second', 'a', 'tg', {}),
    step('enqueue', 'future', 'b', 'tg', {}, now + 1), step('enqueue', 'first', 'different', 'tg', { changed: true }),
    step('next'), step('fail', 5, 'uncertain', 'synthetic ambiguous delivery'),
    step('next'), step('finish', 3), step('next'), step('finish', 4),
    step('next'), step('finish', 2), step('next'), step('finish', 1), step('next'),
    step('fail', 5, 'failed', 'x'.repeat(600)), step('next'),
    step('fail', 5, 'pending', 'reviewed', 1), step('next'),
    step('fail', 5, 'pending', 'reviewed', 0), step('next'), step('finish', 5), step('next'),
  ],
  'history holds, legacy payloads and metadata finish ordering': [
    step('account', 'stable-account', 's1'), step('alias', '123@lid', '12025550123@c.us'),
    step('put', 'history-run', 'active', { status: 'delivering', session: 'stable-account', chatId: '12025550123@c.us' }),
    step('enqueue', 'live-held', 'a', 'wa', { sessionId: 's1', accountId: 'stable-account', data: { chatId: '123@lid' } }),
    step('enqueue', 'legacy-held', 'b', 'wa', { sessionId: 'stable-account', data: { chatId: '12025550123@s.whatsapp.net' } }),
    step('enqueue', 'other', 'c', 'wa', { sessionId: 's1', data: { chatId: 'other@c.us' } }),
    step('enqueue', 'finish', 'sync-finish', 'sync-finish', { run: 'r1' }),
    step('enqueue', 'target', 'sync-target', 'sync-target', { run: 'r1' }),
    step('next'), step('finish', 3), step('next'), step('finish', 5), step('next'), step('finish', 4), step('next'),
    step('put', 'history-run', 'active', { status: 'failed', session: 'stable-account', chatId: '12025550123@c.us' }),
    step('next'), step('finish', 1), step('next'),
  ],
  'crash recovery retains done receipts and makes interrupted sends uncertain': [
    step('enqueue', 'first', 'a', 'tg', { update: { message: { message_id: 1 } } }),
    step('enqueue', 'later', 'a', 'tg', {}), step('enqueue', 'other', 'b', 'tg', {}), step('next'),
    step('sql', "INSERT INTO effects VALUES('first','sending',NULL),('already-done','done','{\"message_id\":99}')"),
    step('reopen', false), step('next'), step('reopen', true), step('next'),
    step('fail', 1, 'uncertain', 'inspect delivery'), step('next'),
  ],
  'transaction rollback preserves old JSON and receipts': [
    step('put', 'checkpoint', 'old', { version: 1, session: 'stable-account', receipt: null }),
    step('sql', "BEGIN; UPDATE kv SET value='{\"version\":2}' WHERE kind='checkpoint'; INSERT INTO effects VALUES('rollback','done','{}'); ROLLBACK"),
    step('get', 'checkpoint', 'old'),
  ],
};

for (const [name, steps] of Object.entries(scenarios)) {
  test(`ClojureScript persistence matches frozen transitions: ${name}`, t => {
    const dir = mkdtempSync(join(tmpdir(), 'bifrost-rewrite-'));
    t.after(() => rmSync(dir, { recursive: true, force: true }));
    const expected = replay(false, join(dir, 'original.db'), steps);
    const actual = replay(true, join(dir, 'candidate.db'), steps);
    for (let i = 0; i < steps.length; i++) assert.deepEqual(actual[i], expected[i], `Transition ${i}: ${steps[i].op}`);
  });
}

test('both implementations continue the latest database after the other has written and recovered it', t => {
  const dir = mkdtempSync(join(tmpdir(), 'bifrost-rewrite-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const expectedPath = join(dir, 'original.db'), mixedPath = join(dir, 'mixed.db');
  const stages = [
    [step('account', 'stable-account', 's1'), step('enqueue', 'legacy', 'a', 'n8n', { action: 'new_conversation' }), step('next')],
    [step('next'), step('finish', 1), step('enqueue', 'candidate', 'a', 'tg', { unchanged: null }), step('next'),
      step('sql', "INSERT INTO effects VALUES('candidate:send','sending',NULL)")],
    [step('next'), step('fail', 2, 'uncertain', 'review required'), step('enqueue', 'later', 'a', 'tg', {}),
      step('enqueue', 'independent', 'b', 'tg', {}), step('next'), step('finish', 4)],
    [step('next'), step('fail', 2, 'pending', 'explicitly reviewed no delivery'), step('next'), step('finish', 2), step('next')],
  ];
  for (let i = 0; i < stages.length; i++) {
    const expected = replay(false, expectedPath, stages[i], i > 0);
    const actual = replay(i % 2 === 1, mixedPath, stages[i], i > 0);
    assert.deepEqual(actual, expected, `Cross-version continuation stage ${i}`);
  }
});

test('AES-GCM credentials decrypt in both directions and reject altered tags', () => {
  const plaintext = 'synthetic-test-token-😀', digest = createHash('sha256').update(plaintext).digest('hex');
  for (const writer of [false, true]) {
    const [{ value: sealed }] = replay(writer, ':memory:', [step('seal', plaintext)]);
    assert.equal(Buffer.from(sealed, 'base64').length, 12 + Buffer.byteLength(plaintext) + 16);
    const [{ value: decrypted }] = replay(!writer, ':memory:', [step('unsealDigest', sealed)]);
    assert.equal(decrypted, digest);
    const altered = Buffer.from(sealed, 'base64'); altered[altered.length - 1] ^= 1;
    const [{ error }] = replay(!writer, ':memory:', [step('unsealDigest', altered.toString('base64'))]);
    assert.ok(error, 'tampering must fail authentication');
  }
});
