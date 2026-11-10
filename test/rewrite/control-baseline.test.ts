import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { controlGoldenPath, recordControlBaseline } from '../../scripts/rewrite/control-baseline.ts';

test('original registration, picker, unmirror and command assertions reproduce scoped control transcripts independently', () => {
  const first = recordControlBaseline(), second = recordControlBaseline();
  assert.deepEqual(first, second);
  assert.deepEqual(first.summary, JSON.parse(readFileSync(controlGoldenPath, 'utf8')));
  assert.equal(first.summary.cases.length, 47);
  assert.equal(first.summary.cases.filter((r: any) => r.inventory).length, 38);
  assert.ok(first.transcripts.every((r: any) => r.transitions.length > 0 && r.state.length > 0));
  const find = (fragment: string) => {
    const result = first.transcripts.find((r: any) => r.name.includes(fragment)); assert.ok(result, fragment); return result;
  };
  const choices = find('caps visible choices');
  assert.equal(choices.calls.find((c: any) => c.method === 'tg' && c.args[1] === 'sendMessage').args[2].reply_markup.inline_keyboard.length, 20);
  const registration = find('through binding and private Start');
  const token = JSON.parse(registration.state[0].kv.find((r: any) => r.kind === 'bot').value).token;
  assert.ok(!token.includes('synthetic-managed'), 'sealing must remain effective under isolated fixed entropy');
  assert.ok(registration.transitions.some((t: any) => t.operation === 'registerBot' || t.operation === 'telegram'));
  assert.ok(find('after setWebhook refusal').calls.some((c: any) => c.error?.status === 403));
  assert.ok(find('unauthorized binding callbacks').transitions.some((t: any) => t.operation === 'acceptTelegram'));
  assert.ok(find('ambiguous cleanup failures').calls.some((c: any) => c.error?.uncertain));
});
