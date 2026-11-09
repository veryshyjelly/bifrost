import { test } from 'node:test';
import assert from 'node:assert/strict';
import { root } from '../../scripts/rewrite/oracle.ts';

test('nbb runs file, stream, multipart, SQLite, crypto, npm resources and child Bun paths', () => {
  const result = Bun.spawnSync([process.execPath, 'x', '--bun', '--no-install', 'nbb', 'scripts/rewrite/runtime.cljs'], {
    cwd: root, env: { PATH: process.env.PATH!, TZ: 'UTC' }, timeout: 15000,
  });
  assert.equal(result.exitCode, 0, result.stderr.toString());
  const report = JSON.parse(result.stdout.toString());
  assert.equal(report.bun, '1.4.2');
  for (const capability of ['sqlite', 'aesGcm', 'fetch', 'streams', 'multipart', 'openAsBlob', 'filesystem', 'npmResources', 'workerBun']) assert.equal(report[capability], true, capability);
});
