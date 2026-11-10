import assert from 'node:assert/strict';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { manifest, root, sha256 } from './oracle.ts';

export function recordWorkflowBaseline(files: string[], selection: Record<string, string[]>, supplement: string | false = 'managed-bot-scenarios.ts', options: { omitIdleTicks?: boolean; captureUploadBytes?: boolean; aliasPhotoPaths?: boolean; freezeDateConstructor?: boolean; observeTooling?: boolean } = {}) {
  const result = Bun.spawnSync([process.execPath, 'scripts/rewrite/original-controls.ts'], {
    cwd: root, env: { PATH: process.env.PATH!, TZ: 'UTC' }, timeout: 20000, maxBuffer: 64 * 1024 * 1024,
    stdin: Buffer.from(JSON.stringify({ files, selection, supplement, ...options })),
  });
  assert.equal(result.exitCode, 0, result.stderr.toString());
  const transcripts = JSON.parse(result.stdout.toString());
  const inventory = JSON.parse(readFileSync(join(root, 'docs/rewrite/evidence/inventory.json'), 'utf8'));
  const hash = (value: any) => sha256(Buffer.from(JSON.stringify(value)));
  const summary = { baseline: manifest.revision, clock: 1794240000000, timezone: 'UTC', cases: transcripts.map((row: any) => {
    const original = inventory.tests.find((t: any) => t.name === row.name && t.reference.startsWith(row.file + ':'));
    assert.ok(original || row.file === 'scripts/rewrite/' + supplement, `Missing original inventory reference: ${row.name}`);
    return { file: row.file, name: row.name, ...(original ? { inventory: original.id, sourceSha256: original.sourceSha256 } : {}),
      calls: hash(row.calls), logs: hash(row.logs), state: hash(row.state),
      transitions: row.transitions.map((t: any) => ({ operation: t.operation, trace: hash(t) })) };
  }) };
  for (const [file, names] of Object.entries(selection)) for (const name of names) {
    assert.ok(summary.cases.some((c: any) => c.file === file && c.name === name), `Selected original fixture missing: ${name}`);
  }
  return { summary, transcripts };
}

export function checkWorkflowBaseline(record: () => ReturnType<typeof recordWorkflowBaseline>, golden: string, artifactName: string) {
  const mode = process.argv[2] || 'check'; assert.ok(['record', 'check'].includes(mode), 'Choose record or check');
  const first = record(), second = record();
  assert.deepEqual(first, second, 'Independent original workflows must reproduce exactly');
  if (mode === 'record') writeFileSync(golden, JSON.stringify(first.summary, null, 2) + '\n');
  else assert.deepEqual(first.summary, JSON.parse(readFileSync(golden, 'utf8')));
  const artifact = join(root, 'data/rewrite/evidence', artifactName);
  mkdirSync(join(root, 'data/rewrite/evidence'), { recursive: true, mode: 0o700 });
  const bytes = Buffer.from(JSON.stringify(first, null, 2) + '\n'); writeFileSync(artifact, bytes, { mode: 0o600 });
  console.log(JSON.stringify({ mode, cases: first.summary.cases.length, transitions: first.summary.cases.reduce((n: number, c: any) => n + c.transitions.length, 0), artifact, sha256: sha256(bytes) }));
}
