import { randomBytes } from 'node:crypto';
import { existsSync, writeFileSync, chmodSync, readFileSync, mkdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export function configure(directory = resolve(import.meta.dirname, '..')) {
  mkdirSync(directory, { recursive: true, mode: 0o700 });
  const path = resolve(directory, 'bridge.env');
  const created = !existsSync(path);
  if (created) {
    let template = readFileSync(new URL('../bridge.env.example', import.meta.url), 'utf8');
    for (const key of ['BRIDGE_ENCRYPTION_KEY', 'BRIDGE_ADMIN_TOKEN', 'OPENWA_WEBHOOK_SECRET']) {
      template = template.replace(new RegExp('^' + key + '=$', 'm'), key + '=' + randomBytes(32).toString('hex'));
    }
    writeFileSync(path, template, { mode: 0o600 });
  }
  chmodSync(path, 0o600);
  const values = Object.fromEntries(readFileSync(path, 'utf8').split('\n').filter(l => /^[A-Z_]+=/.test(l))
    .map(l => [l.slice(0, l.indexOf('=')), l.slice(l.indexOf('=') + 1)]));
  const telegram = resolve(directory, 'telegram-api.env');
  writeFileSync(telegram, `TELEGRAM_API_ID=${values.TELEGRAM_API_ID || ''}\nTELEGRAM_API_HASH=${values.TELEGRAM_API_HASH || ''}\n`, { mode: 0o600 });
  chmodSync(telegram, 0o600);
  const n8n = resolve(directory, 'n8n.env'), createdN8n = !existsSync(n8n);
  if (createdN8n) writeFileSync(n8n, `N8N_ENCRYPTION_KEY=${randomBytes(32).toString('hex')}\nN8N_ENFORCE_SETTINGS_FILE_PERMISSIONS=true\nN8N_SECURE_COOKIE=false\nGENERIC_TIMEZONE=${values.SYNC_TIMEZONE || 'Asia/Kolkata'}\nTZ=${values.SYNC_TIMEZONE || 'Asia/Kolkata'}\n`, { mode: 0o600 });
  chmodSync(n8n, 0o600);
  return { created, createdN8n };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const result = configure();
  console.log(result.created ? 'Created private bridge.env with fresh bridge secrets. Fill the empty account fields locally.' : 'Preserved every bridge.env value.');
  console.log('Updated private telegram-api.env. ' + (result.createdN8n ? 'Created private n8n.env for the optional standalone n8n service.' : 'Preserved n8n.env.'));
}
