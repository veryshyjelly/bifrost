import { createHash, createHmac, timingSafeEqual } from 'node:crypto';
import { cronSchedule } from './cron.ts';

export type Json = any;
export function equal(a: string, b: string) {
  const x = Buffer.from(a), y = Buffer.from(b);
  return x.length === y.length && timingSafeEqual(x, y);
}
export const digest = (value: string) => createHash('sha256').update(value).digest('hex');
export const sign = (value: string, key: string) => createHmac('sha256', key).update(value).digest('hex');
export function configuration(env = process.env) {
  return {
    port: Number(env.BRIDGE_PORT || 3210),
    db: env.BRIDGE_DB || './data/bridge.db', media: env.BRIDGE_MEDIA || './media',
    key: env.BRIDGE_ENCRYPTION_KEY || '', admin: env.BRIDGE_ADMIN_TOKEN || '',
    webhookSecret: env.OPENWA_WEBHOOK_SECRET || '',
    owner: env.TELEGRAM_OWNER_ID || '', management: env.TELEGRAM_MANAGEMENT_CHAT_ID || '',
    masterToken: env.TELEGRAM_MASTER_TOKEN || '',
    telegram: env.TELEGRAM_API_URL || 'http://telegram-api:8081',
    callback: env.BRIDGE_CALLBACK_URL || 'http://bifrost:3210',
    openwa: env.OPENWA_URL || 'http://openwa-api:2785',
    openwaKey: env.OPENWA_API_KEY || '', session: env.OPENWA_SESSION_ID || '',
    account: env.BIFROST_ACCOUNT_ID || '',
    history: Math.min(2000, Math.max(0, Number(env.HISTORY_LIMIT || 20))),
    mediaLimit: Number(env.MAX_MEDIA_BYTES || 100 * 1024 * 1024),
    pace: Number(env.DELIVERY_INTERVAL_MS || 1100),
    syncCron: env.SYNC_CRON || '0 * * * *',
    syncTimezone: env.SYNC_TIMEZONE || 'Asia/Kolkata',
    commandTTL: Number(env.COMMAND_TTL_SECONDS || 60),
    controlTTL: Number(env.CONTROL_TTL_SECONDS || 600),
  };
}
export type Config = ReturnType<typeof configuration>;
export function validate(c: Config) {
  for (const key of ['key', 'admin', 'webhookSecret'] as const) {
    if (c[key].length < 32) throw new Error(`${key} must contain at least 32 characters`);
  }
  if (!/^[a-f0-9]{64}$/i.test(c.key)) throw new Error('BRIDGE_ENCRYPTION_KEY must be 32 bytes in hex');
  if (!Number.isSafeInteger(c.port) || c.port < 1 || c.port > 65535) throw new Error('Invalid port');
  if (!Number.isFinite(c.mediaLimit) || c.mediaLimit < 1024) throw new Error('Invalid media limit');
  if (!Number.isSafeInteger(c.history) || c.history < 0) throw new Error('Invalid history limit');
  if (c.owner && !/^\d+$/.test(c.owner)) throw new Error('TELEGRAM_OWNER_ID must be your numeric user ID');
  if (c.management && !/^-\d+$/.test(c.management)) throw new Error('TELEGRAM_MANAGEMENT_CHAT_ID must be a group ID');
  if (!Number.isFinite(c.pace) || c.pace < 100) throw new Error('DELIVERY_INTERVAL_MS must be at least 100');
  cronSchedule(c.syncCron, c.syncTimezone);
  for (const ttl of [c.commandTTL, c.controlTTL]) if (!Number.isSafeInteger(ttl) || ttl < 5 || ttl > 86400) throw new Error('Command lifetimes must be 5–86400 seconds');
}
