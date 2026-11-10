import { mock } from 'bun:test';
import { mkdirSync, readFileSync, readdirSync, rmSync, statSync, symlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { oracle } from './oracle.ts';
import { exercise, providers } from './relay-driver.ts';

// Isolated subprocess only: freeze entropy before loading the immutable oracle.
// Bun's module mock preserves every other crypto export (including hashing).
const crypto = await import('node:crypto');
let entropy = 0;
mock.module('node:crypto', () => ({ ...crypto, randomUUID: () => `${(++entropy).toString(16).padStart(8, '0')}-0000-4000-8000-000000000000` }));
const filesystem = await import('node:fs/promises');
const realpath = filesystem.realpath;
let sharedRoot = '';
// Bind the production mount name to a disposable directory. Every returned
// file path, including symlinks, still passes through the real realpath/stat.
mock.module('node:fs/promises', () => ({ ...filesystem, realpath: (path: any, ...args: any[]) => realpath(path === '/telegram-data' ? sharedRoot : path, ...args) }));
const baseline = oracle();
const { Store } = await import(pathToFileURL(`${baseline}/src/store.ts`).href);
const { Bridge } = await import(pathToFileURL(`${baseline}/src/bridge.ts`).href);
const { ApiError } = await import(pathToFileURL(`${baseline}/src/api.ts`).href);
const { configuration } = await import(pathToFileURL(`${baseline}/src/config.ts`).href);
const input = await Bun.stdin.json(), output: any = {};
const makeError = (e: any) => e.code ? Object.assign(new Error('synthetic missing file'), { code: e.code }) :
  Object.assign(new ApiError('synthetic refusal', e.status ?? 0, e.retry ?? false, e.uncertain ?? false, e.delay ?? 5000),
    ...(e.service ? [{ service: e.service }] : []), ...(e.reason ? [{ reason: e.reason }] : []));

for (const source of input.scenarios) {
  const watchdog = setTimeout(() => { process.stderr.write(`Baseline scenario timed out: ${source.id}\n`); process.exit(2); }, 5000);
  entropy = 0;
  const directory = join(input.directory, source.id), media = join(directory, 'media');
  sharedRoot = join(directory, 'shared');
  const scenario = JSON.parse(JSON.stringify(source).split('$SHARED').join(sharedRoot).split('$OUTSIDE').join(join(directory, 'outside')));
  mkdirSync(media, { recursive: true });
  mkdirSync(sharedRoot, { recursive: true });
  writeFileSync(join(sharedRoot, 'inside.bin'), 'shared-file-😀');
  writeFileSync(join(directory, 'outside'), 'outside');
  writeFileSync(join(sharedRoot, 'large.bin'), Buffer.alloc(65));
  symlinkSync(join(directory, 'outside'), join(sharedRoot, 'escape.bin'));
  const config = { ...configuration({}), key: 'aa'.repeat(32), owner: '123', management: '-1001', masterToken: 'master-token',
    account: 'stable-account', session: 'session /😀', telegram: 'http://synthetic-telegram', openwa: 'http://synthetic-openwa',
    openwaKey: 'synthetic-openwa-key', media, mediaLimit: 64, ...scenario.config };
  const fixture = providers(makeError), downloads = [...(scenario.downloads || [])], conversions = [...(scenario.conversions || [])];
  Date.now = fixture.now;
  globalThis.fetch = (async (url: string, init: any) => {
    fixture.calls.push({ method: 'download', url, headers: init.headers, redirect: init.redirect, timed: init.signal instanceof AbortSignal });
    const next = downloads.shift();
    if (!next) throw new Error('Unscripted download; provider networking forbidden');
    if (next.transport) throw Object.assign(new Error('synthetic download failure'), { cause: next.transport });
    const chunks = next.chunks || [Buffer.from(next.base64 ?? 'c3ludGhldGljLWZpbGUt8J+YgA==', 'base64')];
    return new Response(new ReadableStream({ start(controller) {
      for (const chunk of chunks) controller.enqueue(typeof chunk === 'number' ? new Uint8Array(chunk) : chunk);
      if (next.streamError) controller.error(new Error('synthetic interrupted stream')); else controller.close();
    } }), { status: next.status || 200, headers: next.headers || {} });
  }) as any;
  const store = new Store(join(directory, 'store.db'), config.key), bridge = new Bridge(config, store, fixture.api);
  // Codec behavior is a scripted boundary here. Real codecs remain mandatory
  // in Gate 7; downloads, byte reads, limits, save and routing use original code.
  bridge.media.convert = async (path: string, voice = false) => {
    fixture.calls.push({ method: 'convert', voice, bytes: readFileSync(path).toString('base64') });
    const plan = conversions.shift(); if (plan?.error) throw makeError(plan.error);
    return bridge.media.save(Buffer.from('converted:' + readFileSync(path).toString('base64')), voice ? 'voice.ogg' : 'image.jpg');
  };
  bridge.media.sticker = async (attachment: any, target: string) => {
    fixture.calls.push({ method: 'sticker', target, name: attachment.name, bytes: readFileSync(attachment.path).toString('base64') });
    const plan = conversions.shift(); if (plan?.error) throw makeError(plan.error);
    const bytes = Buffer.from('prepared:' + readFileSync(attachment.path).toString('base64'));
    const name = plan?.name || 'prepared.webp', mime = plan?.mime || 'image/webp';
    return { path: await bridge.media.save(bytes, name), name, mime };
  };
  const files = () => readdirSync(media).sort().filter(name => statSync(join(media, name)).isFile()).map(name => {
    const path = join(media, name), bytes = readFileSync(path);
    return { name, bytes: bytes.toString('base64'), sha256: crypto.createHash('sha256').update(bytes).digest('hex'), mode: statSync(path).mode & 0o777 };
  });
  const previousLog = console.error; console.error = fixture.logError;
  try {
    output[scenario.id] = await exercise({
      put: (...args: any[]) => store.put(...args), map: (...args: any[]) => store.map(...args),
      whatsapp: (event: any, key: string) => bridge.whatsApp(event, key), deliver: (event: any, key: string) => bridge.deliverWhatsApp(event, key),
      telegram: (...args: any[]) => bridge.telegram(...args),
      fromWhatsApp: (m: any) => bridge.media.fromWhatsApp(m), fromTelegram: (m: any) => bridge.media.fromTelegram('master-token', m),
      removeMedia: () => { for (const name of readdirSync(media)) rmSync(join(media, name), { recursive: true }); },
      error: (e: any) => e instanceof ApiError ? { status: e.status, message: e.message, retry: e.retry, uncertain: e.uncertain, delay: e.delay, service: e.service, reason: e.reason } : { message: e.message, code: e.code },
      snapshot: () => ({ ...Object.fromEntries(['kv', 'jobs', 'effects', 'messages', 'aliases', 'telegram_messages'].map(table => [table, store.db.prepare(`SELECT * FROM ${table} ORDER BY rowid`).all()])), files: files() }),
    }, fixture, scenario);
    if (downloads.length || conversions.length) throw new Error(`Unused script entries in ${scenario.id}: downloads=${downloads.length}, conversions=${conversions.length}`);
  } finally { clearTimeout(watchdog); console.error = previousLog; store.close(); }
}
// Only the temporary root differs between independent runs. Preserve UUIDs,
// byte hashes, raw persisted JSON, identities, scope, missing/null and API order.
await Bun.write(Bun.stdout, JSON.stringify(output).split(input.directory).join('$FIXTURE') + '\n');
// Synthetic fetch does not consume the oracle's 120-second AbortSignal timers.
// All workflows and database closes finished; flush stdout before ending this
// dedicated recorder process instead of waiting for those unused timers.
process.exit(0);
