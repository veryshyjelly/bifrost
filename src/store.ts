import { DatabaseSync } from 'node:sqlite';
import { mkdirSync } from 'node:fs';
import { dirname } from 'node:path';
import { createCipheriv, createDecipheriv, randomBytes } from 'node:crypto';
import type { Json } from './config.ts';

export class Store {
  db: DatabaseSync;
  key: Buffer;
  constructor(path: string, key: string, recover = false) {
    if (path !== ':memory:') mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
    this.key = Buffer.from(key, 'hex');
    this.db = new DatabaseSync(path);
    this.db.exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
      CREATE TABLE IF NOT EXISTS kv (kind TEXT, id TEXT, value TEXT NOT NULL, PRIMARY KEY(kind,id));
      CREATE TABLE IF NOT EXISTS jobs (id INTEGER PRIMARY KEY, dedup TEXT UNIQUE NOT NULL,
        stream TEXT NOT NULL, type TEXT NOT NULL, payload TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'pending',
        attempts INTEGER NOT NULL DEFAULT 0, due INTEGER NOT NULL DEFAULT 0, error TEXT, created INTEGER NOT NULL);
      CREATE INDEX IF NOT EXISTS jobs_ready ON jobs(state,due,id);
      CREATE TABLE IF NOT EXISTS effects (id TEXT PRIMARY KEY, state TEXT NOT NULL, result TEXT);
      CREATE TABLE IF NOT EXISTS messages (wa_chat TEXT NOT NULL, wa_id TEXT NOT NULL, tg_chat TEXT NOT NULL,
        tg_id INTEGER NOT NULL, bot TEXT NOT NULL, direction TEXT NOT NULL, media INTEGER NOT NULL DEFAULT 0,
        PRIMARY KEY(bot,tg_chat,tg_id));
      CREATE INDEX IF NOT EXISTS messages_wa ON messages(wa_chat,wa_id);
      CREATE TABLE IF NOT EXISTS aliases (alias TEXT PRIMARY KEY, canonical TEXT NOT NULL);
      CREATE TABLE IF NOT EXISTS telegram_messages (
        seq INTEGER PRIMARY KEY, scope TEXT NOT NULL, chat TEXT NOT NULL, message INTEGER NOT NULL,
        bot TEXT NOT NULL, thread INTEGER NOT NULL DEFAULT 0, kind TEXT NOT NULL,
        date INTEGER NOT NULL, expires INTEGER NOT NULL DEFAULT 0, deleted INTEGER NOT NULL DEFAULT 0, error TEXT,
        UNIQUE(scope,chat,message));
      CREATE INDEX IF NOT EXISTS telegram_messages_chat ON telegram_messages(scope,chat,thread,seq);
    `);
    // A process may have died after a remote send but before persisting its receipt.
    if (recover) this.db.exec("UPDATE effects SET state='uncertain' WHERE state='sending'; UPDATE jobs SET state='pending' WHERE state='running'");
  }
  get(kind: string, id: string): Json {
    const row = this.db.prepare('SELECT value FROM kv WHERE kind=? AND id=?').get(kind, id);
    return row ? JSON.parse(row.value as string) : undefined;
  }
  put(kind: string, id: string, value: Json) {
    this.db.prepare('INSERT INTO kv VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET value=excluded.value').run(kind, id, JSON.stringify(value));
  }
  all(kind: string): Json[] {
    return this.db.prepare('SELECT id,value FROM kv WHERE kind=?').all(kind).map(r => ({ ...JSON.parse(r.value as string), id: r.id }));
  }
  canonical(id: string) {
    return (this.db.prepare('SELECT canonical FROM aliases WHERE alias=?').get(id)?.canonical as string) || id.replace('@s.whatsapp.net', '@c.us');
  }
  alias(alias: string, canonical: string) {
    const existing = this.db.prepare('SELECT canonical FROM aliases WHERE alias=?').get(alias);
    if (existing && existing.canonical !== canonical) throw new Error('Conflicting contact identity; manual review required');
    this.db.prepare('INSERT OR IGNORE INTO aliases VALUES(?,?)').run(alias, canonical);
  }
  enqueue(dedup: string, stream: string, type: string, payload: Json, due = 0) {
    return !!this.db.prepare('INSERT OR IGNORE INTO jobs(dedup,stream,type,payload,created,due) VALUES(?,?,?,?,?,?)')
      .run(dedup, stream, type, JSON.stringify(payload), Date.now(), due).changes;
  }
  next(): Json {
    const row = this.db.prepare(`SELECT * FROM jobs j WHERE state='pending' AND due<=?
      AND NOT EXISTS (SELECT 1 FROM jobs p WHERE p.stream=j.stream AND p.id<j.id AND p.state IN ('pending','running','uncertain','failed'))
      AND (j.type!='sync-finish' OR NOT EXISTS (SELECT 1 FROM jobs t WHERE t.type IN ('sync-start','sync-target')
        AND json_extract(t.payload,'$.run')=json_extract(j.payload,'$.run') AND t.state IN ('pending','running')))
      ORDER BY CASE WHEN j.type LIKE 'control-%' THEN 2 WHEN j.type LIKE 'sync-%' OR j.type LIKE 'status-%' THEN 1 ELSE 0 END,id LIMIT 1`).get(Date.now());
    if (!row) return undefined;
    this.db.prepare("UPDATE jobs SET state='running',attempts=attempts+1 WHERE id=?").run(row.id);
    return { ...row, attempts: Number(row.attempts) + 1, payload: JSON.parse(row.payload as string) };
  }
  finish(id: number) { this.db.prepare("UPDATE jobs SET state='done',error=NULL WHERE id=?").run(id); }
  fail(id: number, state: string, error: string, delay = 0) {
    this.db.prepare('UPDATE jobs SET state=?,error=?,due=? WHERE id=?').run(state, error.slice(0, 500), Date.now() + delay, id);
  }
  map(waChat: string, waId: string, tgChat: string, tgId: number, bot: string, direction: string, media = false) {
    this.db.prepare('INSERT OR IGNORE INTO messages VALUES(?,?,?,?,?,?,?)').run(waChat, waId, String(tgChat), tgId, bot, direction, Number(media));
  }
  byTelegram(chat: string, message: number, bot?: string): Json {
    if (bot && !String(chat).startsWith('-')) return this.db.prepare('SELECT * FROM messages WHERE tg_chat=? AND tg_id=? AND bot=?').get(String(chat), message, bot);
    return this.db.prepare('SELECT * FROM messages WHERE tg_chat=? AND tg_id=?').get(String(chat), message);
  }
  byWhatsApp(chat: string, message: string): Json[] { return this.db.prepare('SELECT * FROM messages WHERE wa_chat=? AND wa_id=?').all(chat, message); }
  seal(text: string) {
    const iv = randomBytes(12), cipher = createCipheriv('aes-256-gcm', this.key, iv);
    return Buffer.concat([iv, cipher.update(text), cipher.final(), cipher.getAuthTag()]).toString('base64');
  }
  unseal(text: string) {
    const b = Buffer.from(text, 'base64'), decipher = createDecipheriv('aes-256-gcm', this.key, b.subarray(0, 12));
    decipher.setAuthTag(b.subarray(-16));
    return Buffer.concat([decipher.update(b.subarray(12, -16)), decipher.final()]).toString();
  }
  close() { this.db.close(); }
}
