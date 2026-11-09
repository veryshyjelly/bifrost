import { pathToFileURL } from 'node:url';
import { oracle } from './oracle.ts';
import { exercise } from './ingress-driver.ts';

globalThis.fetch = async () => { throw new Error('Provider network is forbidden in ingress replay'); };
const baseline = oracle();
const { Store } = await import(pathToFileURL(`${baseline}/src/store.ts`).href);
const { Bridge } = await import(pathToFileURL(`${baseline}/src/bridge.ts`).href);
const { handler } = await import(pathToFileURL(`${baseline}/src/server.ts`).href);
const { commandOf } = await import(pathToFileURL(`${baseline}/src/control.ts`).href);
const input = await Bun.stdin.json();
Date.now = () => input.now;
const store = new Store(':memory:', input.config.key), bridge = new Bridge(input.config, store);
try {
  console.log(JSON.stringify(await exercise({ handler: handler(bridge),
    put: (...args: any[]) => store.put(...args), sql: (sql: string) => store.db.exec(sql),
    context: (...args: any[]) => bridge.telegramContext(...args), command: commandOf,
    snapshot: () => Object.fromEntries(['kv', 'jobs', 'effects', 'messages', 'aliases', 'telegram_messages'].map(table => [table, store.db.prepare(`SELECT * FROM ${table} ORDER BY rowid`).all()])),
  }, input)));
} finally { store.close(); }
