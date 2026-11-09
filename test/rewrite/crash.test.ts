import { test } from 'node:test';
import assert from 'node:assert/strict';
import { cpSync, existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { oracle, root } from '../../scripts/rewrite/oracle.ts';

const baseline = oracle();
const { configuration } = await import(`${baseline}/src/config.ts`);
const { Store } = await import(`${baseline}/src/store.ts`);
const key = 'aa'.repeat(32), person = '12025550123@c.us';
const config = { ...configuration({}), key, admin: 'synthetic-admin'.repeat(4), webhookSecret: 'synthetic-webhook'.repeat(4),
  owner: '123', management: '-1001', masterToken: 'master-token', account: 'stable-account', session: 's1', openwaKey: 'synthetic-openwa-key' };
const step = (op: string, ...args: any[]) => ({ op, args });
const tables = ['kv', 'jobs', 'effects', 'messages', 'aliases', 'telegram_messages'];
const points = ['after-enqueue', 'after-claim', 'before-remote', 'after-remote', 'after-receipt', 'after-mapping', 'before-finish'];

function run(candidate: boolean, input: any) {
  const args = candidate ? ['x', '--bun', '--no-install', 'nbb', 'scripts/rewrite/relay.cljs'] : ['scripts/rewrite/original-relay.ts'];
  return Bun.spawnSync([process.execPath, ...args], { cwd: root, env: { PATH: process.env.PATH!, TZ: 'UTC' },
    stdin: Buffer.from(JSON.stringify(input)), timeout: 15000 });
}

function snapshot(path: string) {
  // Inspect without recovery. Never assume a close checkpoints a crashed WAL.
  // Reading candidate-written state through the frozen Store is intentional.
  const store = new Store(path, key, false);
  try { return Object.fromEntries(tables.map(table => [table, store.db.prepare(`SELECT * FROM ${table} ORDER BY rowid`).all()])); }
  finally { store.close(); }
}

function clone(path: string, destination: string) {
  const store = new Store(path, key, false);
  try { store.db.exec(`VACUUM INTO '${destination.replaceAll("'", "''")}'`); }
  finally { store.close(); }
}

for (const direction of ['incoming', 'outgoing']) for (const point of points) {
  test(`${direction} SIGKILL at ${point} preserves receipts and safe old/new continuation`, t => {
    const dir = mkdtempSync(join(tmpdir(), 'bifrost-crash-'));
    t.after(() => rmSync(dir, { recursive: true, force: true }));
    const event = { sessionId: 's1', event: 'message.received', idempotencyKey: 'first', data: { id: 'first', chatId: person,
      from: person, type: 'text', body: 'Crash fixture 😀', timestamp: 1700000000, fromMe: false, isGroup: false, contact: { pushName: 'Alice' } } };
    const update = { update_id: 1, message: { message_id: 1, from: { id: 123 }, chat: { id: -200, type: 'supergroup' }, text: 'Crash fixture 😀' } };
    const method = direction === 'incoming' ? 'sendMessage' : 'messages/send-text';
    let expectedCrashed: any, expectedResumed: any, expectedJournal: any;
    for (const writer of [false, true]) {
      const path = join(dir, `writer-${writer}.db`), journal = join(dir, `writer-${writer}.jsonl`);
      const result = run(writer, { config, path, crash: { point, method, journal }, steps: [
        step('put', 'route', person, { chatId: person, tgChat: '-200', mode: 'group' }),
        ...(direction === 'incoming' ? [step('acceptWhatsApp', event)] : [step('acceptTelegram', 'master', update)]), step('tick'),
      ] });
      assert.equal(result.signalCode, 'SIGKILL', result.stderr.toString());
      const crashed = snapshot(path);
      assert.equal(crashed.jobs.filter((job: any) => job.dedup === (direction === 'incoming' ? 'ow:first' : 'tg:master:1')).length, 1,
        'the accepted job must survive the crash');
      if (!writer) expectedCrashed = crashed; else assert.deepEqual(crashed, expectedCrashed, 'same durable transition before recovery');
      const uncertain = ['before-remote', 'after-remote'].includes(point);
      for (const reader of [false, true]) {
        const copy = join(dir, `writer-${writer}-reader-${reader}.db`), journalCopy = copy + '.jsonl';
        clone(path, copy);
        if (existsSync(journal)) cpSync(journal, journalCopy);
        const resumed = run(reader, { config, path: copy, recover: true, crash: { method, journal: journalCopy }, steps: [step('tick')] });
        assert.equal(resumed.exitCode, 0, resumed.stderr.toString());
        const [outcome] = JSON.parse(resumed.stdout.toString());
        assert.equal(outcome.state.jobs[0].state, uncertain ? 'uncertain' : 'done');
        assert.equal(outcome.state.messages.length, uncertain ? 0 : 1);
        const accepted = existsSync(journalCopy) ? readFileSync(journalCopy, 'utf8').trim().split('\n').filter(Boolean).map(line => JSON.parse(line)) : [];
        assert.equal(accepted.length, point === 'before-remote' ? 0 : 1, 'remote acceptance must never duplicate after restart');
        if (!writer && !reader) { expectedResumed = outcome; expectedJournal = accepted; }
        else { assert.deepEqual(outcome, expectedResumed, 'latest state must resume safely in both implementations'); assert.deepEqual(accepted, expectedJournal); }
      }
    }
  });
}
