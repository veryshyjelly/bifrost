import { pathToFileURL } from 'node:url';
import { oracle } from './oracle.ts';

globalThis.fetch = async () => { throw new Error('Network is forbidden in store replay'); };
const baseline = oracle();
const { Store } = await import(pathToFileURL(`${baseline}/src/store.ts`).href);
const { accountIdentity } = await import(pathToFileURL(`${baseline}/src/identity.ts`).href);
const { digest } = await import(pathToFileURL(`${baseline}/src/config.ts`).href);
const input = await Bun.stdin.json();
Date.now = () => input.now;
let store = new Store(input.path, input.key, input.recover);
const tables = ['kv', 'jobs', 'effects', 'messages', 'aliases', 'telegram_messages'];
const snapshot = () => Object.fromEntries(tables.map(table => [table, store.db.prepare(`SELECT * FROM ${table} ORDER BY rowid`).all()]));
try {
  const results = input.steps.map(({ op, args }: any) => {
    let outcome;
    try {
      let value;
      if (op === 'sql') { store.db.exec(args[0]); value = true; }
      else if (op === 'schema') value = tables.map(table => ({ table, columns: store.db.prepare(`PRAGMA table_info(${table})`).all(), indexes: store.db.prepare(`PRAGMA index_list(${table})`).all() }));
      else if (op === 'reopen') { store.close(); store = new Store(input.path, input.key, args[0]); value = true; }
      else if (op === 'account') value = accountIdentity(store, ...args);
      else if (op === 'unsealDigest') value = digest(store.unseal(...args));
      else { value = store[op](...args); if (['put', 'alias', 'finish', 'fail', 'map'].includes(op)) value = true; }
      outcome = { value };
    } catch (error) { outcome = { error: (error as Error).message }; }
    return { ...outcome, state: snapshot() };
  });
  console.log(JSON.stringify(results));
} finally { store.close(); }
