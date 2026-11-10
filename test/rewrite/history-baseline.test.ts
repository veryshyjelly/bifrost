import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { historyGoldenPath, recordHistoryBaseline } from '../../scripts/rewrite/history-baseline.ts';

test('original history choices, checkpoints, fresh copies, cycles and retry boundaries reproduce independently', () => {
  const first = recordHistoryBaseline(), second = recordHistoryBaseline();
  assert.deepEqual(first, second);
  assert.deepEqual(first.summary, JSON.parse(readFileSync(historyGoldenPath, 'utf8')));
  assert.equal(first.summary.cases.length, 37);
  assert.equal(first.summary.cases.filter((c: any) => c.inventory).length, 35);
  const legacy = first.transcripts.find((c: any) => c.name.startsWith('older in-progress history runs'));
  const missingStyle = legacy.transitions.flatMap((t: any) => t.state[0].kv.filter((r: any) => r.kind === 'history-run').map((r: any) => JSON.parse(r.value)))
    .find((r: any) => r.status === 'queued' && !Object.hasOwn(r, 'historyStyle'));
  assert.ok(missingStyle);
  assert.equal(Object.hasOwn(missingStyle, 'timeZone'), false);
  assert.deepEqual(missingStyle.target, { chat: '-200' });
  const upgraded = first.transcripts.find((c: any) => c.name.startsWith('a legacy history job without'));
  assert.deepEqual(JSON.parse(upgraded.state[0].jobs.find((j: any) => j.type === 'history').payload), { chatId: 'synthetic@g.us', count: 3 });
  assert.equal(JSON.parse(upgraded.state[0].kv.find((r: any) => r.kind === 'history-run').value).status, 'complete');
  const cycles = first.transcripts.find((c: any) => c.name.startsWith('cyclic and self-quoted'));
  assert.equal(cycles.state[0].messages.length, 3);
});
