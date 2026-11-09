import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync, copyFileSync, chmodSync, rmSync, openSync, readSync, closeSync } from 'node:fs';
import { resolve, dirname, join } from 'node:path';
import { createHash, randomUUID } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { Store } from '../src/store.ts';

const root = resolve(import.meta.dirname, '..');
const imageDefault = 'local/bifrost:0.1.0';
const roles = ['data', 'media', 'telegram-data'] as const;
const docker = (args: string[], inherit = false) => execFileSync('docker', args, { encoding: 'utf8', stdio: inherit ? 'inherit' : ['ignore', 'pipe', 'pipe'], maxBuffer: 16 * 1024 * 1024 }) || '';
const inspect = (ids: string[]) => JSON.parse(docker(['inspect', ...ids]));
const mount = (source: string, target: string, type = 'bind', readonly = false) => {
  if (source.includes(',')) throw new Error('Transfer paths cannot contain commas');
  return `type=${type},source=${source},target=${target}${readonly ? ',readonly' : ''}`;
};
function helper(image: string, staging: string, args: string[], mounts: string[] = [], bundle?: string) {
  // docker cp works with remote/read-only host shares too (e.g. Lima). Private
  // staging stays in a disposable Docker volume, never in the application image.
  const scratch = 'bifrost-transfer-' + randomUUID();
  let container: string | undefined;
  docker(['volume', 'create', scratch]);
  try {
    container = docker(['create', '--init', '--network', 'none', '--user', '0:0',
      '-e', `TRANSFER_UID=${process.getuid?.() || 0}`, '-e', `TRANSFER_GID=${process.getgid?.() || 0}`,
      '--mount', mount(scratch, '/transfer', 'volume'), ...mounts,
      '--entrypoint', '/opt/sticker-python/bin/python', image, '/transfer-helper.py', ...args]).trim();
    docker(['cp', staging + '/.', container + ':/transfer']);
    docker(['cp', join(root, 'scripts/transfer-volume.py'), container + ':/transfer-helper.py']);
    if (bundle) docker(['cp', bundle, container + ':/export.tar.gz']);
    docker(['start', '-a', container], true);
    if (docker(['inspect', '--format', '{{.State.ExitCode}}', container]).trim() !== '0') throw new Error('Archive helper rejected the transfer');
    if (args[0] !== 'restore') docker(['cp', container + ':/transfer/.', staging]);
  } finally {
    try { if (container) docker(['rm', '-f', container]); }
    finally { docker(['volume', 'rm', scratch]); }
  }
}
export function environment(text: string) {
  return Object.fromEntries(text.split('\n').filter(line => /^[A-Z_]+=/.test(line))
    .map(line => [line.slice(0, line.indexOf('=')), line.slice(line.indexOf('=') + 1)]));
}
export function restoredEnvironment(text: string) {
  const overrides: Record<string, string> = { BRIDGE_CALLBACK_URL: 'http://bifrost:3210', TELEGRAM_API_URL: 'http://telegram-api:8081', BRIDGE_DB: '/data/bridge.db', BRIDGE_MEDIA: '/media' };
  const lines = text.split('\n').filter(line => !/^N8N_/.test(line)).map(line => {
    const key = line.slice(0, line.indexOf('='));
    if (!(key in overrides)) return line;
    const value = overrides[key]; delete overrides[key]; return key + '=' + value;
  });
  return [...lines, ...Object.entries(overrides).map(([key, value]) => key + '=' + value)].join('\n').trimEnd() + '\n';
}
export function exportVolumes(containers: any[]) {
  const bridge = containers.filter(c => ['bifrost', 'wa-telegram-bridge'].includes(c.Config.Labels?.['com.docker.compose.service']));
  const telegram = containers.filter(c => c.Config.Labels?.['com.docker.compose.service'] === 'telegram-api');
  if (bridge.length !== 1 || telegram.length !== 1) throw new Error('Expected exactly one Bifrost/legacy bridge and one telegram-api container in this project');
  const selected = [...bridge, ...telegram];
  if (selected.some(c => c.State.Paused)) throw new Error('Unpause the source containers before exporting');
  const volumes = Object.fromEntries(roles.map(role => {
    const destination = role === 'data' ? '/data' : role === 'media' ? '/media' : '/telegram-data';
    const container = role === 'telegram-data' ? telegram[0] : bridge[0];
    const entry = container.Mounts.find((m: any) => m.Destination === destination);
    if (entry?.Type !== 'volume' || !entry.Name) throw new Error('Export requires named data, media and Telegram volumes');
    return [role, entry.Name];
  }));
  return { selected, volumes };
}
function validateConfig(directory: string) {
  const values = environment(readFileSync(join(directory, 'bridge.env'), 'utf8'));
  if (!/^[a-f0-9]{64}$/i.test(values.BRIDGE_ENCRYPTION_KEY || '')) throw new Error('Missing original bridge encryption key');
  for (const key of ['TELEGRAM_MASTER_TOKEN', 'TELEGRAM_OWNER_ID', 'TELEGRAM_MANAGEMENT_CHAT_ID', 'TELEGRAM_API_ID', 'TELEGRAM_API_HASH']) {
    if (!values[key]) throw new Error('Incomplete account configuration: ' + key);
  }
  const telegram = environment(readFileSync(join(directory, 'telegram-api.env'), 'utf8'));
  if (telegram.TELEGRAM_API_ID !== values.TELEGRAM_API_ID || telegram.TELEGRAM_API_HASH !== values.TELEGRAM_API_HASH) throw new Error('Telegram API configuration does not match bridge.env');
  return values;
}
function fingerprint(directory: string, names: string[]) {
  return Object.fromEntries(names.map(name => {
    const hash = createHash('sha256'), chunk = Buffer.alloc(1024 * 1024), fd = openSync(join(directory, name), 'r');
    try { let count: number; while ((count = readSync(fd, chunk))) hash.update(chunk.subarray(0, count)); }
    finally { closeSync(fd); }
    return [name, hash.digest('hex')];
  }));
}
export function exportState(file: string, project: string, image = imageDefault, directory = root) {
  file = resolve(file);
  if (existsSync(file)) throw new Error('Export destination already exists');
  validateConfig(directory);
  console.log('Discovering source containers and volumes for ' + project);
  const ids = docker(['ps', '-aq', '--filter', 'label=com.docker.compose.project=' + project]).trim().split(/\s+/).filter(Boolean);
  if (!ids.length) throw new Error('No containers found for source project ' + project);
  const { selected, volumes } = exportVolumes(inspect(ids));
  const writers = docker(['ps', '-q']).trim().split(/\s+/).filter(Boolean);
  if (writers.length && inspect(writers).some((c: any) => !selected.some(s => s.Id === c.Id) && c.Mounts.some((m: any) => m.RW && Object.values(volumes).includes(m.Name)))) throw new Error('Another running container writes to the source volumes');
  mkdirSync(dirname(file), { recursive: true, mode: 0o700 });
  const staging = mkdtempSync(join(dirname(file), '.bifrost-export-'));
  const running = selected.filter(c => c.State.Running).map(c => c.Id);
  let stopped = false;
  try {
    for (const name of ['bridge.env', 'telegram-api.env']) { copyFileSync(join(directory, name), join(staging, name)); chmodSync(join(staging, name), 0o600); }
    console.log('Stopping only the source bridge and Telegram API for a consistent export');
    // Mark before the stop call: even a partial stop must be recovered in finally.
    stopped = true;
    if (running.length) docker(['stop', '--time', '15', ...running]);
    helper(image, staging, ['export'], roles.flatMap(role => ['--mount', mount(volumes[role], '/volumes/' + role, 'volume', true)]));
    const names = ['bridge.env', 'telegram-api.env', ...roles.map(role => role + '.tar.gz')];
    let revision = 'unknown', dirty = true;
    try {
      revision = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: directory, encoding: 'utf8' }).trim();
      dirty = !!execFileSync('git', ['status', '--porcelain'], { cwd: directory, encoding: 'utf8' }).trim();
    } catch {}
    writeFileSync(join(staging, 'manifest.json'), JSON.stringify({ format: 'bifrost', version: 1, created: new Date().toISOString(), source: { revision, dirty, project }, files: fingerprint(staging, names) }, null, 2) + '\n', { mode: 0o600 });
    // Reserve exclusively and set permissions before tar writes private account state.
    writeFileSync(file, '', { flag: 'wx', mode: 0o600 });
    try { execFileSync('tar', ['-czf', file, '-C', staging, '.', ], { stdio: 'inherit', env: { ...process.env, COPYFILE_DISABLE: '1' } }); }
    catch (e) { rmSync(file, { force: true }); throw e; }
    console.log('Private Bifrost export created: ' + file);
  } finally {
    try { if (stopped && running.length) { console.log('Restarting the original source containers'); docker(['start', ...running]); } }
    finally { rmSync(staging, { recursive: true, force: true }); }
  }
}
export function importState(file: string, project = 'bifrost', image = imageDefault, directory = root) {
  file = resolve(file);
  if (!/^[a-z0-9][a-z0-9_-]*$/.test(project)) throw new Error('Invalid destination project name');
  for (const name of ['bridge.env', 'telegram-api.env', '.env']) if (existsSync(join(directory, name))) throw new Error('Import requires a fresh checkout without private configuration: ' + name);
  const volumes = Object.fromEntries(roles.map(role => [role, project + '_' + (role === 'telegram-data' ? role : 'bridge-' + role)]));
  const existing = new Set(docker(['volume', 'ls', '--format', '{{.Name}}']).trim().split('\n'));
  if (Object.values(volumes).some(name => existing.has(name))) throw new Error('Destination volumes already exist; import into a fresh project');
  const staging = mkdtempSync(join(directory, '.bifrost-import-'));
  const created: string[] = [], written: string[] = [];
  try {
    console.log('Validating export checksums, archive paths and database integrity');
    helper(image, staging, ['unpack'], [], file);
    const values = validateConfig(staging);
    const store = new Store(join(staging, 'check-data/bridge.db'), values.BRIDGE_ENCRYPTION_KEY);
    try { for (const bot of store.all('bot')) if (bot.token) store.unseal(bot.token); }
    finally { store.close(); }
    for (const role of roles) { docker(['volume', 'create', volumes[role]]); created.push(volumes[role]); }
    helper(image, staging, ['restore'], roles.flatMap(role => ['--mount', mount(volumes[role], '/volumes/' + role, 'volume')]));
    const outputs: Record<string, string> = {
      'bridge.env': restoredEnvironment(readFileSync(join(staging, 'bridge.env'), 'utf8')),
      'telegram-api.env': readFileSync(join(staging, 'telegram-api.env'), 'utf8'),
      '.env': readFileSync(join(root, '.env.example'), 'utf8').replace(/^COMPOSE_PROJECT_NAME=.*$/m, 'COMPOSE_PROJECT_NAME=' + project),
    };
    for (const [name, text] of Object.entries(outputs)) { writeFileSync(join(directory, name), text, { mode: 0o600, flag: 'wx' }); written.push(name); }
    console.log('Imported Bifrost identity and state. No services started.');
    console.log('Stop the old deployment before starting this identity. Configure/pair OpenWA and update only its connection fields in bridge.env.');
  } catch (e) {
    for (const name of written) rmSync(join(directory, name), { force: true });
    for (const name of created) { try { docker(['volume', 'rm', name]); } catch {} }
    throw e;
  } finally { rmSync(staging, { recursive: true, force: true }); }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const [operation, file, ...args] = process.argv.slice(2);
  try {
    if (!['export', 'import'].includes(operation) || !file || args.length % 2) throw new Error('Usage: bun run export|import FILE [--project NAME] [--image IMAGE]');
    const options: Record<string, string> = {};
    for (let i = 0; i < args.length; i += 2) { if (!['--project', '--image'].includes(args[i])) throw new Error('Unknown transfer option'); options[args[i]] = args[i + 1]; }
    const project = options['--project'] || environment(existsSync(join(root, '.env')) ? readFileSync(join(root, '.env'), 'utf8') : '').COMPOSE_PROJECT_NAME || 'bifrost';
    (operation === 'export' ? exportState : importState)(file, project, options['--image'] || imageDefault);
  } catch (e) { console.error(e instanceof Error && !('stderr' in e) ? e.message : 'Transfer failed; the source was retained and partial import resources were cleaned up.'); process.exitCode = 1; }
}
