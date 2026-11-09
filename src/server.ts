import { createServer } from 'node:http';
import { fileURLToPath } from 'node:url';
import { Bridge } from './bridge.ts';
import { configuration, validate, equal, sign } from './config.ts';
import { ApiError } from './api.ts';
import { Store } from './store.ts';

export function handler(bridge: Bridge) {
  return async (request: any, response: any) => {
    const reply = (status: number, body: any) => { response.writeHead(status, { 'content-type': 'application/json' }); response.end(JSON.stringify(body)); };
    try {
      const path = new URL(request.url, 'http://bridge').pathname;
      if (request.method === 'GET' && path === '/healthz') return reply(200, { status: 'ok', configured: bridge.status().configured });
      const telegram = /^\/webhooks\/telegram\/([a-zA-Z0-9_-]+)$/.exec(path);
      const admin = equal(request.headers.authorization || '', 'Bearer ' + bridge.c.admin);
      if (request.method === 'GET' && path === '/v1/status') {
        if (!admin) return reply(401, { error: 'Unauthorized' });
        return reply(200, bridge.status());
      }
      if (request.method !== 'POST') return reply(404, { error: 'Not found' });
      if (telegram && !equal(request.headers['x-telegram-bot-api-secret-token'] || '', bridge.webhookSecret(telegram[1]))) return reply(401, { error: 'Invalid Telegram signature' });
      if (path === '/v1/review' && !admin) return reply(401, { error: 'Unauthorized' });
      if (!telegram && !['/webhooks/openwa', '/v1/management', '/v1/review'].includes(path)) return reply(404, { error: 'Not found' });
      const buffers: Buffer[] = []; let size = 0;
      for await (const chunk of request) {
        size += chunk.length;
        if (size > 2 * 1024 * 1024) { reply(413, { error: 'Webhook too large; use media download' }); request.resume(); return; }
        buffers.push(chunk);
      }
      const raw = Buffer.concat(buffers);
      if (path === '/webhooks/openwa' && !equal(request.headers['x-openwa-signature'] || '', 'sha256=' + sign(raw.toString(), bridge.c.webhookSecret))) return reply(401, { error: 'Invalid OpenWA signature' });
      let body: any;
      try { body = JSON.parse(raw.toString()); } catch { return reply(400, { error: 'Invalid JSON' }); }
      if (!body || typeof body !== 'object' || Array.isArray(body)) return reply(400, { error: 'Expected object' });
      let accepted: boolean;
      if (telegram) accepted = bridge.acceptTelegram(telegram[1], body);
      else if (path === '/webhooks/openwa') accepted = bridge.acceptWhatsApp(body);
      else if (path === '/v1/management') accepted = bridge.acceptManagement(body);
      else {
        const job = bridge.store.db.prepare("SELECT * FROM jobs WHERE id=? AND state IN ('failed','uncertain')").get(Number(body.job));
        if (!job || !['retry', 'skip'].includes(body.action)) return reply(400, { error: 'Choose retry or skip for an existing failed/uncertain job' });
        if (job.state === 'uncertain' && body.confirmNoDelivery !== true && body.action === 'retry') return reply(409, { error: 'Inspect both chats first, then explicitly confirmNoDelivery to retry' });
        if (body.action === 'retry') {
          // Reset only effects belonging to this job. Completed effects remain immutable.
          const prefix = String(job.dedup);
          bridge.store.db.prepare("UPDATE effects SET state='pending' WHERE state='uncertain' AND (id=? OR substr(id,1,?)=?)").run(prefix, prefix.length + 1, prefix + ':');
          // WhatsApp sends use their stable Telegram-message key, not their delivery update id.
          const payload = JSON.parse(job.payload as string), m = payload.update?.message;
          if (m) bridge.store.db.prepare("UPDATE effects SET state='pending' WHERE state='uncertain' AND id=?").run(`send:${m.chat.type === 'private' ? payload.bot + ':' : ''}${m.chat.id}:${m.message_id}`);
          bridge.store.fail(Number(job.id), 'pending', 'Reviewed and retried');
        } else bridge.store.finish(Number(job.id));
        accepted = true;
      }
      reply(200, { accepted });
    } catch (e) {
      const status = e instanceof ApiError && e.status >= 400 && e.status < 600 ? e.status : 500;
      reply(status, { error: e instanceof ApiError ? e.message : 'Internal error' });
    }
  };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const c = configuration(); validate(c);
  const bridge = new Bridge(c, new Store(c.db, c.key, true));
  const server = createServer(handler(bridge));
  let timer: NodeJS.Timeout;
  server.listen(c.port, '0.0.0.0', () => {
    console.log(JSON.stringify({ event: 'listening', port: c.port, configured: bridge.status().configured }));
    timer = setInterval(() => { if (bridge.status().configured) void bridge.tick(); }, c.pace);
    if (bridge.status().configured) { bridge.control.startup(); bridge.statuses.startup(); }
  });
  const cleanup = setInterval(() => void bridge.media.clean().catch(() => console.error('{"event":"media_cleanup_failed"}')), 3600000);
  const schedule = () => { try { bridge.metadata.cron(); } catch { console.error('{"event":"metadata_schedule_failed"}'); } };
  schedule();
  const syncTimer = setInterval(schedule, 15000);
  for (const signal of ['SIGTERM', 'SIGINT']) process.on(signal, () => {
    clearInterval(timer); clearInterval(cleanup); clearInterval(syncTimer);
    server.close(() => {
      const drain = setInterval(() => { if (!bridge.busy) { clearInterval(drain); bridge.store.close(); process.exit(0); } }, 100);
      setTimeout(() => process.exit(1), 10000).unref();
    });
  });
}
