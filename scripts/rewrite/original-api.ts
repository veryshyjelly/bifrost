import { pathToFileURL } from 'node:url';
import { oracle } from './oracle.ts';

const baseline = oracle();
const { Api, ApiError } = await import(pathToFileURL(`${baseline}/src/api.ts`).href);
const { digest } = await import(pathToFileURL(`${baseline}/src/config.ts`).href);
const input = await Bun.stdin.json(), results = [];
for (const row of input) {
  const calls: any[] = [];
  globalThis.fetch = (async (url: string, init: any) => {
    let body = init.body;
    if (body instanceof FormData) {
      const entries: any = {};
      for (const [key, value] of body.entries()) entries[key] = typeof value === 'string' ? value : {
        name: value.name, size: value.size, type: value.type, digest: digest(Buffer.from(await value.arrayBuffer())),
      };
      body = entries;
    }
    calls.push({ url, method: init.method, headers: init.headers, body, redirect: init.redirect, signal: init.signal instanceof AbortSignal });
    if (row.transport) throw Object.assign(new Error('synthetic transport error'), { cause: row.transport });
    const status = row.status || 200;
    return { status, ok: status < 400, headers: new Headers(row.headers || {}), json: async () => {
      if (row.invalid) throw new Error('synthetic invalid JSON');
      return row.response || { ok: true, result: { message_id: 99 } };
    } };
  }) as any;
  const api = new Api({ telegram: 'http://fake-telegram', openwa: 'http://fake-openwa', openwaKey: 'synthetic-key', session: 'session /😀' });
  try {
    let result;
    result = await api[row.adapter](...row.args);
    results.push({ result, calls });
  } catch (e) {
    if (!(e instanceof ApiError)) throw e;
    results.push({ error: { message: e.message, status: e.status, retry: e.retry, uncertain: e.uncertain, delay: e.delay, reason: e.reason, service: e.service }, calls });
  }
}
console.log(JSON.stringify(results));
