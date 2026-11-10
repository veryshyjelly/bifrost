import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { actionGoldenPath, recordActionBaseline } from '../../scripts/rewrite/action-baseline.ts';

test('original edits, reactions, revocations, polls and unapproved correctness candidates reproduce independently', () => {
  const first = recordActionBaseline(), second = recordActionBaseline();
  assert.deepEqual(first, second);
  assert.deepEqual(first.summary, JSON.parse(readFileSync(actionGoldenPath, 'utf8')));
  assert.equal(first.summary.cases.length, 54);
  assert.equal(first.summary.cases.filter((c: any) => c.inventory).length, 48);
  for (const id of ['KEEP-001', 'EDIT-001', 'CALLBACK-001']) {
    const row = first.transcripts.find((c: any) => c.name.includes(id)); assert.ok(row);
    assert.ok(row.calls.length > 0 && row.transitions.length > 0 && row.state.length > 0);
  }
  const poll = first.transcripts.find((c: any) => c.name.startsWith('an owner poll_answer'));
  assert.equal(poll.calls.length, 0);
  assert.ok(poll.state[0].jobs.some((j: any) => j.type === 'tg' && j.state === 'done'));
  const edit = first.transcripts.find((c: any) => c.name.includes('EDIT-001'));
  const packets = edit.calls.filter((c: any) => c.method === 'tg' && c.args[1] === 'editMessageText');
  assert.equal(packets.length, 2);
  assert.equal(packets[0].args[2].text, packets[1].args[2].text);
});
