import { mock } from 'bun:test';
import { pathToFileURL } from 'node:url';
import { existsSync, readFileSync, realpathSync, statSync } from 'node:fs';
import { basename, dirname, join } from 'node:path';
import { tmpdir } from 'node:os';
import { oracle } from './oracle.ts';

// Replay original fixture assertions, with observation confined to this process.
// No source or assertion from the frozen archive is rewritten.
const crypto = await import('node:crypto');
let entropy = 0, active: any, currentFile = '', depth = 0;
const next = () => ++entropy;
mock.module('node:crypto', () => ({ ...crypto,
  randomUUID: () => `${next().toString(16).padStart(8, '0')}-0000-4000-8000-000000000000`,
  randomBytes: (size: number) => Buffer.alloc(size, next() % 256),
  randomInt: (low: number, high?: number) => high === undefined ? next() % low : low + next() % (high - low),
}));
const cases: any[] = [];
const photoPaths = new Map<string, string>();
await import('node:test');
const register: any = (name: string, run: any) => cases.push({ file: currentFile, name, run });
register.skip = (name: string, run: any) => cases.push({ file: currentFile, name, run, skip: true });
mock.module('node:test', () => ({ test: register }));
globalThis.fetch = async () => { throw new Error('Provider network forbidden in control baseline'); };
const baseline = oracle(), uri = (file: string) => pathToFileURL(`${baseline}/${file}`).href;
const input = await Bun.stdin.json();
if (input.freezeDateConstructor) {
  const RealDate = Date;
  globalThis.Date = new Proxy(RealDate, {
    construct: (target, args, newTarget) => Reflect.construct(target, args.length ? args : [Date.now()], newTarget),
    apply: () => new RealDate(Date.now()).toString(),
  });
}
const { Store: OriginalStore } = await import(uri('src/store.ts'));
const copy = (value: any) => JSON.parse(JSON.stringify(value));
const tables = ['kv', 'jobs', 'effects', 'messages', 'aliases', 'telegram_messages'];
const stores: any[] = [];
const snapshot = (store: any) => Object.fromEntries(tables.map(table => [table, store.db.prepare(`SELECT * FROM ${table} ORDER BY rowid`).all()]));
const allState = () => stores.map(store => store.closed ? store.finalState : snapshot(store));
class RecordingStore extends OriginalStore {
  closed = false; finalState: any;
  constructor(...args: any[]) {
    super(...args); stores.push(this);
    const prepare = this.db.prepare.bind(this.db), exec = this.db.exec.bind(this.db);
    this.db.prepare = (sql: string) => {
      const statement = prepare(sql), run = statement.run.bind(statement);
      statement.run = (...values: any[]) => {
        const result = run(...values);
        if (!depth) active.transitions.push(copy({ operation: 'sql', sql, values, state: allState() }));
        return result;
      };
      return statement;
    };
    this.db.exec = (sql: string) => {
      const result = exec(sql);
      if (!depth) active.transitions.push(copy({ operation: 'exec', sql, state: allState() }));
      return result;
    };
    active.transitions.push(copy({ operation: 'open', state: allState() }));
  }
  close() {
    this.finalState = snapshot(this); this.closed = true;
    active.transitions.push(copy({ operation: 'close', state: allState() }));
    return super.close();
  }
}
mock.module(uri('src/store.ts'), () => ({ Store: RecordingStore }));
const { Api } = await import(uri('src/api.ts'));
const { Bridge: OriginalBridge } = await import(uri('src/bridge.ts'));
function apiObserver(api: any) {
  return new Proxy(api, { get(target, property, receiver) {
    const value = Reflect.get(target, property, receiver);
    if (!['tg', 'ow', 'upload', 'request'].includes(String(property)) || typeof value !== 'function') return value;
    return async (...args: any[]) => {
      const call: any = { method: property, args: copy(args) }; active.calls.push(call);
      if (property === 'upload' && input.captureUploadBytes && existsSync(args[3])) {
        const bytes = readFileSync(args[3]);
        call.file = { bytes: bytes.toString('base64'), sha256: crypto.createHash('sha256').update(bytes).digest('hex'), mode: statSync(args[3]).mode & 0o777 };
        const parent = dirname(args[3]);
        if (input.aliasPhotoPaths && basename(parent).startsWith('hermes-sync-') && realpathSync(dirname(parent)) === realpathSync(tmpdir())) {
          if (!photoPaths.has(parent)) photoPaths.set(parent, `$PHOTO-${photoPaths.size + 1}`);
          call.args[3] = photoPaths.get(parent) + '/' + basename(args[3]);
        }
      }
      try { const result = await value.apply(target, args); call.result = copy({ value: result }); return result; }
      catch (e: any) { call.error = copy({ message: e.message, status: e.status, retry: e.retry, uncertain: e.uncertain, delay: e.delay, service: e.service, reason: e.reason }); throw e; }
    };
  } });
}
class RecordingBridge extends OriginalBridge {
  constructor(c: any, store: any, api = new Api(c)) {
    super(c, store, apiObserver(api));
    for (const method of ['acceptTelegram', 'acceptWhatsApp', 'telegram', 'whatsApp', 'deliverWhatsApp', 'manage', 'command', 'registerBot', 'tick', 'migrateBot', 'installWebhook']) {
      const invoke = (this as any)[method].bind(this);
      (this as any)[method] = (...args: any[]) => {
        const outer = depth++ === 0, start = active.calls.length;
        const before = outer && method === 'tick' && input.omitIdleTicks ? JSON.stringify(allState()) : undefined;
        const finish = (error?: any) => {
          depth--;
          if (outer) {
            const state = allState();
            // A drain loop may deliberately probe an empty queue 100 times.
            // Elide only an error-free tick with no API calls and byte-identical
            // state. No timing, selected work or mutation is hidden.
            if (before !== undefined && !error && active.calls.length === start && before === JSON.stringify(state)) return;
            active.transitions.push(copy({ operation: method, args, calls: active.calls.slice(start),
              ...(error ? { error: { message: error.message, status: error.status, retry: error.retry, uncertain: error.uncertain } } : {}), state }));
          }
        };
        try {
          const result = invoke(...args);
          if (result?.then) return result.then((value: any) => { finish(); return value; }, (error: any) => { finish(error); throw error; });
          finish(); return result;
        } catch (error) { finish(error); throw error; }
      };
    }
  }
}
mock.module(uri('src/bridge.ts'), () => ({ Bridge: RecordingBridge }));
if (input.observeTooling) {
  const toolCall = (name: string, invoke: (...args: any[]) => any) => (...args: any[]) => {
    const call: any = { method: name, args: copy(args) }; active.calls.push(call);
    try {
      const result = invoke(...args);
      if (result?.then) return result.then((value: any) => { call.result = copy({ value }); return value; }, (e: any) => { call.error = { message: e.message }; throw e; });
      call.result = copy({ value: result }); return result;
    } catch (e: any) { call.error = { message: e.message }; throw e; }
  };
  for (const file of ['scripts/configure.ts', 'scripts/setup.ts', 'scripts/transfer.ts']) {
    const original = await import(uri(file)), replacement = { ...original };
    for (const name of file.endsWith('configure.ts') ? ['configure'] : file.endsWith('setup.ts') ? ['setup'] : ['environment', 'restoredEnvironment', 'exportVolumes']) {
      const invoke = original[name];
      replacement[name] = (...args: any[]) => {
        if (name === 'configure') {
          const directory = args[0];
          if (typeof directory !== 'string' || !basename(directory).startsWith('bridge-configure-') || realpathSync(dirname(directory)) !== realpathSync(tmpdir())) throw new Error('Configuration recording requires an original disposable test directory');
          const value = invoke(...args);
          const files = ['bridge.env', 'telegram-api.env', 'n8n.env'].filter(f => existsSync(join(directory, f))).map(f => ({ name: f, bytes: readFileSync(join(directory, f)).toString('base64'), mode: statSync(join(directory, f)).mode & 0o777 }));
          active.transitions.push(copy({ operation: 'configure', args: ['$CONFIG'], result: value, files, state: allState() }));
          return value;
        }
        if (name === 'setup') {
          const bridge = args[1];
          args[1] = { ...bridge, api: apiObserver(bridge.api),
            migrateBot: toolCall('migrateBot', bridge.migrateBot.bind(bridge)), installWebhook: toolCall('installWebhook', bridge.installWebhook.bind(bridge)),
            store: { all: toolCall('store.all', bridge.store.all.bind(bridge.store)) },
            statuses: { startup: toolCall('statuses.startup', bridge.statuses.startup.bind(bridge.statuses)) } };
        }
        const result = toolCall(file + ':' + name, invoke)(...args);
        const finish = (value: any) => { active.transitions.push(copy({ operation: name, result: { value }, state: allState() })); return value; };
        return result?.then ? result.then(finish) : finish(result);
      };
    }
    mock.module(uri(file), () => replacement);
  }
}
for (const file of input.files) {
  currentFile = file;
  await import(uri(file));
}
const supplement = input.supplement ?? 'managed-bot-scenarios.ts';
if (supplement !== false) {
  if (!['managed-bot-scenarios.ts', 'action-scenarios.ts', 'history-scenarios.ts', 'metadata-scenarios.ts', 'operational-scenarios.ts'].includes(supplement)) throw new Error('Unknown baseline supplement');
  currentFile = 'scripts/rewrite/' + supplement;
  await import('./' + supplement);
}
const output: any[] = [];
for (const entry of cases) {
  if (entry.skip) continue;
  if (input.selection[entry.file] && !input.selection[entry.file].includes(entry.name)) continue;
  const watchdog = setTimeout(() => { process.stderr.write(`Control baseline timed out: ${entry.name}\n`); process.exit(2); }, 5000);
  entropy = 0; depth = 0; stores.length = 0; photoPaths.clear();
  Date.now = () => 1794240000000;
  active = { file: entry.file, name: entry.name, calls: [], transitions: [], logs: [] };
  const after: any[] = [], previousLog = console.error;
  console.error = (...args) => active.logs.push(copy(args));
  try {
    await entry.run({ after: (f: any) => after.push(f) });
    for (const callback of after) await callback();
    active.state = allState(); output.push(active);
  } catch (error: any) { process.stderr.write(`${entry.file}: ${entry.name}\n${error.stack}\n`); process.exit(1); }
  finally { console.error = previousLog; clearTimeout(watchdog); }
}
await Bun.write(Bun.stdout, JSON.stringify(output) + '\n');
process.exit(0);
