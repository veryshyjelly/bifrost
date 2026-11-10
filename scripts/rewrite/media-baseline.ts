import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { manifest, root, sha256 } from './oracle.ts';
import { mediaScenarios, transportScenarios } from './media-scenarios.ts';

export const goldenPath = join(root, 'test/rewrite/fixtures/media-baseline.json');
export function recordMediaBaseline() {
  const directory = realpathSync(mkdtempSync(join(tmpdir(), 'bifrost-media-baseline-')));
  try {
    const run = (script: string, input: any) => {
      const result = Bun.spawnSync([process.execPath, script], { cwd: root, env: { PATH: process.env.PATH!, TZ: 'UTC' },
        stdin: Buffer.from(JSON.stringify(input)), timeout: 20000, maxBuffer: 16 * 1024 * 1024 });
      assert.equal(result.exitCode, 0, result.stderr.toString());
      return JSON.parse(result.stdout.toString());
    };
    const media = run('scripts/rewrite/original-media.ts', { directory, scenarios: mediaScenarios });
    const file = join(directory, 'fixture.bin'); writeFileSync(file, 'synthetic-file-😀');
    const transport = JSON.parse(JSON.stringify(run('scripts/rewrite/original-api.ts', transportScenarios(file))).split(directory).join('$FIXTURE'));
    const hash = (value: any) => sha256(Buffer.from(JSON.stringify(value)));
    const summary = {
      baseline: manifest.revision, clock: 1794240000000, timezone: 'UTC',
      media: Object.fromEntries(Object.entries(media).map(([id, transitions]: [string, any]) => [id, {
        // Compare exact serialized fields, including every raw persisted JSON
        // string. Digests keep the checked-in manifest small; the CLI retains
        // complete reproducible transcripts separately for review.
        transitions: transitions.map((r: any) => ({ trace: hash(r.calls), state: hash(r.state), outcome: hash({ value: r.value, error: r.error, logs: r.logs }) })),
        methods: transitions.at(-1).calls.map((c: any) => c.method),
        errors: transitions.flatMap((r: any, i: number) => r.error ? [{ step: i, ...r.error }] : []),
      }])),
      transport: transport.map((r: any) => ({ trace: hash(r.calls), outcome: hash({ result: r.result, error: r.error }), error: r.error })),
    };
    return { summary, media, transport };
  } finally { rmSync(directory, { recursive: true, force: true }); }
}

if (import.meta.main) {
  const mode = process.argv[2] || 'check';
  assert.ok(['record', 'check'].includes(mode), 'Choose record or check');
  const first = recordMediaBaseline(), second = recordMediaBaseline();
  assert.deepEqual(first, second, 'Independent original processes must reproduce exact traces and per-transition state');
  if (mode === 'record') writeFileSync(goldenPath, JSON.stringify(first.summary, null, 2) + '\n');
  else assert.deepEqual(first.summary, JSON.parse(readFileSync(goldenPath, 'utf8')));
  const artifact = join(root, 'data/rewrite/evidence/media-baseline.json');
  mkdirSync(join(root, 'data/rewrite/evidence'), { recursive: true, mode: 0o700 });
  const raw = Buffer.from(JSON.stringify(first, null, 2) + '\n'); writeFileSync(artifact, raw, { mode: 0o600 });
  console.log(JSON.stringify({ mode, media: mediaScenarios.length, transport: first.transport.length, artifact, sha256: sha256(raw) }));
}
