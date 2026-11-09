import { mkdirSync, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { spawnSync } from 'node:child_process';
import { manifest, oracle, root, sha256 } from './oracle.ts';

const label = process.argv[2];
if (!['local', 'linux', 'baseline-linux', 'privacy'].includes(label)) throw new Error('Choose local, linux, baseline-linux or privacy');
oracle();
const sourceFiles: string[] = [];
function collect(dir: string) {
  for (const entry of readdirSync(resolve(root, dir), { withFileTypes: true })) {
    const path = `${dir}/${entry.name}`;
    if (entry.isDirectory()) collect(path);
    else if (entry.isFile()) sourceFiles.push(path);
  }
}
for (const dir of ['src', 'scripts', 'test']) collect(dir);
sourceFiles.push('package.json', 'bun.lock', '.bun-version', 'nbb.edn', 'docs/rewrite/experiments/Dockerfile');
const inputs = Object.fromEntries(sourceFiles.sort().map(path => [path, sha256(readFileSync(resolve(root, path)))]));
let command: string[], image: string | undefined, imageInputs: string | undefined;
if (label.includes('linux')) {
  const inspected = spawnSync('docker', ['image', 'inspect', 'local/bifrost-rewrite-checks:foundation', '--format', '{{.Id}}'], { encoding: 'utf8' });
  if (inspected.status !== 0) throw new Error('Build the isolated verification image first');
  image = inspected.stdout.trim();
  const captured = spawnSync('docker', ['run', '--rm', '--network', 'none', '--read-only', '--entrypoint', 'cat', image, '/app/candidate-inputs.json'], { encoding: 'utf8' });
  if (captured.status !== 0) throw new Error('Could not verify image source inputs');
  const built = JSON.parse(captured.stdout);
  for (const [file, hash] of Object.entries(inputs)) {
    if (built.files[file] !== hash) throw new Error(`Verification image is stale: ${file}; rebuild before checking`);
  }
  imageInputs = built.fingerprint;
  command = ['docker', 'run', '--rm', '--network', 'none', '--read-only', '--tmpfs', '/tmp:rw,nosuid,size=256m',
    '--env', 'TMPDIR=/tmp', '--workdir', label === 'baseline-linux' ? `/app/${manifest.oraclePath}` : '/app', '--entrypoint', 'bun', image, 'test', './test'];
} else command = [process.execPath, ...(label === 'privacy' ? ['run', 'check:repository'] : ['test', './test'])];
const startedAt = new Date().toISOString();
const result = spawnSync(command[0], command.slice(1), { cwd: root, env: { PATH: process.env.PATH!, TZ: 'UTC', HOME: process.env.HOME! }, maxBuffer: 16 * 1024 * 1024, timeout: 120000 });
const completedAt = new Date().toISOString();
const log = Buffer.concat([result.stdout || Buffer.alloc(0), result.stderr || Buffer.alloc(0)]);
const logPath = `data/rewrite/evidence/${label}.txt`, reportPath = `docs/rewrite/evidence/checks/${label}.json`;
mkdirSync(resolve(root, 'data/rewrite/evidence'), { recursive: true, mode: 0o700 });
mkdirSync(resolve(root, 'docs/rewrite/evidence/checks'), { recursive: true });
writeFileSync(resolve(root, logPath), log, { mode: 0o600 });
const summary = log.toString().split('\n').filter(line => /\d+ (?:pass|skip|fail)|Ran \d+|Repository|repository/.test(line));
const report = { baseline: manifest.revision, candidate: { status: 'uncommitted', sourceFingerprint: sha256(Buffer.from(JSON.stringify(inputs))), files: inputs },
  startedAt, completedAt, command, image, imageInputs, environment: label.includes('linux') ? 'Linux arm64, network none, read-only filesystem, tmpfs only; no production mounts or published ports' : 'macOS arm64, synthetic tests with loopback permitted',
  bun: Bun.version, nbb: JSON.parse(readFileSync(resolve(root, 'node_modules/nbb/package.json'), 'utf8')).version,
  outcome: result.status === 0 ? 'pass' : 'fail', exitCode: result.status, signal: result.signal,
  summary, skips: label === 'privacy' ? [] : ['9 opt-in sticker/codec cases', '2 opt-in Docker transfer cases'],
  artifact: { path: logPath, sha256: sha256(log) }, limitations: 'This check does not complete an acceptance gate. See evidence-ledger.md.' };
writeFileSync(resolve(root, reportPath), JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify({ report: reportPath, outcome: report.outcome, summary }));
process.exitCode = result.status === 0 ? 0 : 1;
