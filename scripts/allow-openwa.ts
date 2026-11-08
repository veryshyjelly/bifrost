import { readFileSync, writeFileSync, copyFileSync, chmodSync } from 'node:fs';
import { resolve } from 'node:path';

if (!process.argv[2]) throw new Error('Usage: node scripts/allow-openwa.ts /path/to/OpenWA/.env');
const path = resolve(process.argv[2]);
const original = readFileSync(path, 'utf8');
const match = /^SSRF_ALLOWED_HOSTS=(.*)$/m.exec(original);
const hosts = (match?.[1] || '').trim().replace(/^['"]|['"]$/g, '').split(',').map(s => s.trim()).filter(Boolean);
if (hosts.includes('wa-telegram-bridge')) console.log('OpenWA callback hostname is already allowed; no change.');
else {
  hosts.push('wa-telegram-bridge');
  const backup = path + '.bridge-backup-' + Date.now(); copyFileSync(path, backup); chmodSync(backup, 0o600);
  const line = 'SSRF_ALLOWED_HOSTS=' + hosts.join(',');
  writeFileSync(path, match ? original.replace(/^SSRF_ALLOWED_HOSTS=.*$/m, line) : original.trimEnd() + '\n' + line + '\n', { mode: 0o600 });
  console.log('Added only wa-telegram-bridge to OpenWA SSRF_ALLOWED_HOSTS; preserved other entries and saved a private backup. Recreate only openwa-api to apply.');
}
