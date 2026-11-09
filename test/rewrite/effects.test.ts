import { test } from 'node:test';
import assert from 'node:assert/strict';
import { root } from '../../scripts/rewrite/oracle.ts';

test('done receipts never resend, definite refusals can retry, and ambiguous effects remain blocked', () => {
  const scenarios = [
    { id: 'done', result: { message_id: 1, absent: undefined, present: null } },
    { id: 'done', result: { message_id: 2 } },
    { id: 'refused', apiError: { status: 429, retry: true, uncertain: false } },
    { id: 'refused', result: { message_id: 3 } },
    { id: 'ambiguous', apiError: { status: 500, retry: false, uncertain: true } },
    { id: 'ambiguous', result: { message_id: 4 } },
    { id: 'unexpected', genericError: true },
    { id: 'unexpected', result: { message_id: 5 } },
    { id: 'empty' }, { id: 'empty', result: { changed: true } },
  ];
  function replay(candidate: boolean) {
    const args = candidate ? ['x', '--bun', '--no-install', 'nbb', 'scripts/rewrite/effects.cljs'] : ['scripts/rewrite/original-effects.ts'];
    const result = Bun.spawnSync([process.execPath, ...args], { cwd: root, env: { PATH: process.env.PATH!, TZ: 'UTC' }, stdin: Buffer.from(JSON.stringify(scenarios)) });
    assert.equal(result.exitCode, 0, result.stderr.toString());
    return JSON.parse(result.stdout.toString());
  }
  const actual = replay(true);
  assert.deepEqual(actual, replay(false));
  assert.equal(actual[1].calls, 1);
  assert.equal(actual[3].calls, 3);
  assert.equal(actual[5].calls, 4);
  assert.equal(actual[7].calls, 5);
  assert.equal(actual[5].error.uncertain, true);
  assert.equal(actual[9].result, null);
});
