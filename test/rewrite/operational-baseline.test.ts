import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { operationalGoldenPath, recordOperationalBaseline } from '../../scripts/rewrite/operational-baseline.ts';

test('original configuration, setup, transfer rules, ignored updates and legacy recovery reproduce independently', () => {
  const first = recordOperationalBaseline(), second = recordOperationalBaseline();
  assert.deepEqual(first, second);
  assert.deepEqual(first.summary, JSON.parse(readFileSync(operationalGoldenPath, 'utf8')));
  assert.equal(first.summary.cases.length, 17);
  assert.equal(first.summary.cases.filter((c: any) => c.inventory).length, 12);
  const find = (prefix: string) => { const row = first.transcripts.find((c: any) => c.name.startsWith(prefix)); assert.ok(row); return row; };
  const fresh = find('fresh configuration').transitions.find((t: any) => t.operation === 'configure');
  assert.equal(fresh.files.length, 2);
  assert.ok(fresh.files.every((f: any) => f.mode === 0o600));
  const preserved = find('reconfiguration preserves').transitions.find((t: any) => t.operation === 'configure');
  assert.equal(Buffer.from(preserved.files.find((f: any) => f.name === 'telegram-api.env').bytes, 'base64').toString(), 'TELEGRAM_API_ID=12345\nTELEGRAM_API_HASH=literal$hash\n');
  const setup = find('setup accepts OpenWA ready');
  assert.ok(setup.calls.some((c: any) => c.method === 'migrateBot'));
  assert.ok(setup.calls.some((c: any) => c.method === 'statuses.startup'));
  assert.ok(find('export discovers legacy').calls.some((c: any) => c.error?.message.includes('named')));
  assert.equal(find('admitted unsupported WhatsApp').calls.length, 0);
  for (const [label, suffix] of [['absent', 'undefined'], ['null', 'null']]) {
    const row = find(`legacy recovery retains ${label}`);
    assert.ok(row.state[0].jobs.some((j: any) => j.dedup === 'management:' + suffix && j.state === 'failed'));
  }
});
