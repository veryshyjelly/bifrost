import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import fixtures from './fixtures/rules.json';
import { root } from '../../scripts/rewrite/oracle.ts';

function replay(args: string[]) {
  const result = Bun.spawnSync([process.execPath, ...args], {
    cwd: root, env: { PATH: process.env.PATH!, TZ: 'UTC' }, stdin: Buffer.from(JSON.stringify(fixtures)),
  });
  assert.equal(result.exitCode, 0, result.stderr.toString());
  return JSON.parse(result.stdout.toString());
}

test('ClojureScript rules match the frozen oracle across UTF-16, history, configuration and cron fixtures', () => {
  const expected = replay(['scripts/rewrite/original-rules.ts']);
  const actual = replay(['x', '--bun', '--no-install', 'nbb', resolve(root, 'scripts/rewrite/rules.cljs')]);
  assert.equal(actual.length, fixtures.length);
  for (let i = 0; i < fixtures.length; i++) assert.deepEqual(actual[i], expected[i], `Fixture ${i}: ${fixtures[i].op}`);
});
