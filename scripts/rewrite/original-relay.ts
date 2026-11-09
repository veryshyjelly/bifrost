import { pathToFileURL } from 'node:url';
import { oracle } from './oracle.ts';
import { providers, exercise, installCrashHooks } from './relay-driver.ts';

globalThis.fetch = async () => { throw new Error('Provider network is forbidden in relay replay'); };
const baseline = oracle();
const { Store } = await import(pathToFileURL(`${baseline}/src/store.ts`).href);
const { Bridge } = await import(pathToFileURL(`${baseline}/src/bridge.ts`).href);
const { ApiError } = await import(pathToFileURL(`${baseline}/src/api.ts`).href);
const input = await Bun.stdin.json();
const fixture = providers(e => e.generic ? new Error('synthetic unexpected failure') : Object.assign(new ApiError('synthetic provider refusal', e.status ?? 0, e.retry ?? false, e.uncertain ?? false, e.delay ?? 5000),
  ...(e.service ? [{ service: e.service }] : []), ...(e.reason ? [{ reason: e.reason }] : [])), input.crash);
Date.now = fixture.now;
let store = new Store(input.path, input.config.key, input.recover), bridge = new Bridge(input.config, store, fixture.api);
installCrashHooks(store.db, input.crash);
const originalLog = console.error; console.error = fixture.logError;
try {
  console.log(JSON.stringify(await exercise({
    put: (...args: any[]) => store.put(...args), sql: (sql: string) => store.db.exec(sql), map: (...args: any[]) => store.map(...args),
    acceptWhatsApp: (payload: any) => bridge.acceptWhatsApp(payload), acceptTelegram: (bot: string, update: any) => bridge.acceptTelegram(bot, update),
    whatsapp: (event: any, key: string) => bridge.whatsApp(event, key), deliver: (event: any, key: string) => bridge.deliverWhatsApp(event, key),
    telegram: (...args: any[]) => bridge.telegram(...args), tick: () => bridge.tick(), tickConcurrent: () => Promise.all([bridge.tick(), bridge.tick()]),
    sendText: (...args: any[]) => bridge.sendText(...args), incomingMention: (...args: any[]) => bridge.mentions.incoming(...args), outgoingMention: (...args: any[]) => bridge.mentions.outgoing(...args),
    reopen: (recover: boolean) => { store.close(); store = new Store(input.path, input.config.key, recover); bridge = new Bridge(input.config, store, fixture.api); },
    error: (e: any) => e instanceof ApiError ? { message: e.message, status: e.status, retry: e.retry, uncertain: e.uncertain, delay: e.delay, service: e.service, reason: e.reason } : { message: e.message },
    snapshot: () => Object.fromEntries(['kv', 'jobs', 'effects', 'messages', 'aliases', 'telegram_messages'].map(table => [table, store.db.prepare(`SELECT * FROM ${table} ORDER BY rowid`).all()])),
  }, fixture, input)));
} finally { console.error = originalLog; store.close(); }
