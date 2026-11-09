import { execFileSync } from 'node:child_process';
import { readFileSync, existsSync } from 'node:fs';
import { resolve } from 'node:path';

const staged = process.argv.includes('--staged');
const root = execFileSync('git', ['rev-parse', '--show-toplevel'], { encoding: 'utf8' }).trim();
const paths = execFileSync('git', ['ls-files', '-z'], { cwd: root, encoding: 'utf8' }).split('\0').filter(Boolean);
const secretNames = new Set(['BRIDGE_ENCRYPTION_KEY', 'BRIDGE_ADMIN_TOKEN', 'OPENWA_WEBHOOK_SECRET', 'OPENWA_API_KEY', 'TELEGRAM_MASTER_TOKEN', 'TELEGRAM_API_HASH', 'N8N_ENCRYPTION_KEY']);
const secrets: string[] = [];
for (const file of ['bridge.env', 'telegram-api.env', 'n8n.env']) {
  const path = resolve(root, file);
  if (!existsSync(path)) continue;
  for (const line of readFileSync(path, 'utf8').split('\n')) {
    const index = line.indexOf('='), key = line.slice(0, index), value = line.slice(index + 1);
    if (secretNames.has(key) && value.length >= 16) secrets.push(value);
  }
}
let errors = 0;
for (const path of paths) {
  const basename = path.split('/').at(-1)!;
  const forbidden = /(^|\/)(data|media|backups|vendor|node_modules|\.bifrost-(?:export|import)-[^/]*)\//.test(path) || /\.bifrost\.tar\.gz$/.test(path)
    || /\.(db|sqlite)(?:$|[.-])/.test(basename)
    || ((!basename.endsWith('.example')) && (basename === '.env' || basename.startsWith('.env.') || /\.env(?:\.|$)/.test(basename)));
  if (forbidden) { console.error('Private/runtime file tracked: ' + path); errors++; continue; }
  let content: string;
  try { content = staged ? execFileSync('git', ['show', ':' + path], { cwd: root, encoding: 'utf8', maxBuffer: 10 * 1024 * 1024 }) : readFileSync(resolve(root, path), 'utf8'); }
  catch { console.error('Cannot inspect tracked file: ' + path); errors++; continue; }
  if (/\b\d{6,}:[A-Za-z0-9_-]{30,}\b/.test(content) || /-----BEGIN (?:[A-Z]+ )?PRIVATE KEY-----/.test(content) || secrets.some(secret => content.includes(secret))) {
    console.error('Possible credential in tracked file: ' + path); errors++;
  }
}
if (errors) { console.error(`Repository check failed (${errors} files). No credential values were printed.`); process.exitCode = 1; }
else console.log(`Repository check passed: ${paths.length} files inspected.`);
