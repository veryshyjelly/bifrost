import { createServer, request } from 'node:http';
import { createHmac } from 'node:crypto';
import { once } from 'node:events';

// Both independent runtimes use the same byte-level client, fixture clock and
// snapshot boundary. The driver has no provider endpoints or credentials.
export async function exercise(dependencies: any, input: any) {
  const server = createServer(dependencies.handler);
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const port = (server.address() as any).port;
  const results = [];
  try {
    for (const step of input.steps) {
      let result: any;
      if (step.op === 'put') { dependencies.put(...step.args); result = { value: true }; }
      else if (step.op === 'sql') { dependencies.sql(step.args[0]); result = { value: true }; }
      else if (step.op === 'context') result = { value: dependencies.context(...step.args) };
      else if (step.op === 'command') result = { value: dependencies.command(step.args[0]) };
      else {
        const raw = Buffer.from(step.raw ?? JSON.stringify(step.body ?? {}));
        const headers = { ...(step.headers || {}) };
        const sign = (value: Buffer | string, key: string) => createHmac('sha256', key).update(value).digest('hex');
        if (step.openwa) headers['x-openwa-signature'] = 'sha256=' + sign(step.signedRaw === undefined ? raw : step.signedRaw, input.config.webhookSecret);
        if (step.bot) headers['x-telegram-bot-api-secret-token'] = sign(step.bot, input.config.admin);
        if (step.admin) headers.authorization = 'Bearer ' + input.config.admin;
        result = await new Promise((resolve, reject) => {
          const req = request({ host: '127.0.0.1', port, path: step.path, method: step.method || 'POST', headers, agent: false }, res => {
            const chunks: Buffer[] = [];
            res.on('data', c => chunks.push(c)); res.on('error', reject);
            res.on('end', () => {
              try { resolve({ status: res.statusCode, body: JSON.parse(Buffer.concat(chunks).toString()), contentType: res.headers['content-type'] }); }
              catch { reject(new Error(`Invalid fixture response for ${step.path}: status ${res.statusCode}, ${Buffer.concat(chunks).length} bytes`)); }
            });
          });
          req.on('error', reject); req.setTimeout(10000, () => req.destroy(new Error('Offline ingress request timed out')));
          const size = step.chunkSize || raw.length || 1;
          for (let offset = 0; offset < raw.length; offset += size) req.write(raw.subarray(offset, offset + size));
          req.end();
        });
      }
      results.push({ ...result, state: dependencies.snapshot() });
    }
    return results;
  } finally { await new Promise<void>(resolve => server.close(() => resolve())); }
}
