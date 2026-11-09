import { pathToFileURL } from 'node:url';
import { oracle } from './oracle.ts';

globalThis.fetch = async () => { throw new Error('Network is forbidden in effect replay'); };
const baseline = oracle();
const { Store } = await import(pathToFileURL(`${baseline}/src/store.ts`).href);
const { Bridge } = await import(pathToFileURL(`${baseline}/src/bridge.ts`).href);
const { ApiError } = await import(pathToFileURL(`${baseline}/src/api.ts`).href);
const store = new Store(':memory:', 'aa'.repeat(32)), input = await Bun.stdin.json(), results = [];
let calls = 0;
try {
  for (const row of input) {
    let result;
    try {
      const value = await Bridge.prototype.effect.call({ store }, row.id, async () => {
        calls++;
        if (row.apiError) throw new ApiError('synthetic refusal', row.apiError.status, row.apiError.retry, row.apiError.uncertain);
        if (row.genericError) throw new Error('synthetic unexpected error');
        return row.result;
      });
      result = { result: value };
    } catch (e) {
      result = { error: e instanceof ApiError ? { message: e.message, status: e.status, retry: e.retry, uncertain: e.uncertain, delay: e.delay } : { message: e.message } };
    }
    results.push({ ...result, calls, effects: store.db.prepare('SELECT * FROM effects ORDER BY rowid').all() });
  }
  console.log(JSON.stringify(results));
} finally { store.close(); }
