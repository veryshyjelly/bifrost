import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, writeFileSync, statSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { configure } from '../scripts/configure.ts';

test('fresh configuration generates independent private secrets from the tracked template', t => {
  const root = mkdtempSync(join(tmpdir(), 'bridge-configure-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  assert.deepEqual(configure(root), { created: true });
  const env = readFileSync(join(root, 'bridge.env'), 'utf8');
  const secrets = ['BRIDGE_ENCRYPTION_KEY', 'BRIDGE_ADMIN_TOKEN', 'OPENWA_WEBHOOK_SECRET'].map(key => new RegExp('^' + key + '=([a-f0-9]{64})$', 'm').exec(env)?.[1]);
  assert.ok(secrets.every(Boolean)); assert.equal(new Set(secrets).size, 3);
  assert.match(env, /^TELEGRAM_MASTER_TOKEN=$/m);
  assert.match(env, /^HISTORY_LIMIT=20$/m);
  for (const file of ['bridge.env', 'telegram-api.env']) assert.equal(statSync(join(root, file)).mode & 0o777, 0o600);
});

test('reconfiguration preserves existing encryption keys and all operator configuration', t => {
  const root = mkdtempSync(join(tmpdir(), 'bridge-configure-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const existing = 'BRIDGE_ENCRYPTION_KEY=keep-original\nTELEGRAM_API_ID=12345\nTELEGRAM_API_HASH=literal$hash\nHISTORY_LIMIT=7\n# preserve this comment\n';
  const n8n = 'N8N_ENCRYPTION_KEY=keep-this-too\n';
  writeFileSync(join(root, 'bridge.env'), existing); writeFileSync(join(root, 'n8n.env'), n8n);
  assert.deepEqual(configure(root), { created: false });
  assert.equal(readFileSync(join(root, 'bridge.env'), 'utf8'), existing);
  assert.equal(readFileSync(join(root, 'n8n.env'), 'utf8'), n8n);
  assert.equal(readFileSync(join(root, 'telegram-api.env'), 'utf8'), 'TELEGRAM_API_ID=12345\nTELEGRAM_API_HASH=literal$hash\n');
});
