import { appendFileSync, existsSync, readFileSync } from 'node:fs';

type CrashFixture = { point?: string; method?: string; journal?: string };

function crashAt(settings: CrashFixture, point: string) {
  if (settings.point === point) process.kill(process.pid, 'SIGKILL');
}

// Hook the same SQLite transition in both implementations. All hooks are
// confined to test subprocesses and synthetic databases; no production imports.
export function installCrashHooks(db: any, settings: CrashFixture = {}) {
  const patterns: Record<string, string> = {
    'after-enqueue': 'INSERT OR IGNORE INTO jobs(',
    'after-claim': "UPDATE jobs SET state='running'",
    'after-receipt': "UPDATE effects SET state='done',result=?",
    'after-mapping': 'INSERT OR IGNORE INTO messages ',
    'before-finish': "UPDATE jobs SET state='done',error=NULL",
  };
  const pattern = settings.point && patterns[settings.point];
  if (!pattern) return;
  const prepare = db.prepare.bind(db);
  db.prepare = (sql: string) => {
    const statement = prepare(sql);
    if (!sql.includes(pattern)) return statement;
    const run = statement.run.bind(statement);
    statement.run = (...args: any[]) => {
      if (settings.point === 'before-finish') crashAt(settings, settings.point);
      const result = run(...args);
      crashAt(settings, settings.point!);
      return result;
    };
    return statement;
  };
}

// Scripted providers cannot open a socket. Unmatched methods fail closed rather
// than obtain live answers. Both runtimes receive identical synthetic receipts.
export function providers(makeError: (error: any) => Error, settings: CrashFixture = {}) {
  const calls: any[] = [], logs: any[] = [], rules: any[] = [];
  let sequence = 100, clock = 1794240000000, members = new Set<string>();
  if (settings.journal && existsSync(settings.journal)) {
    for (const line of readFileSync(settings.journal, 'utf8').split('\n').filter(Boolean)) sequence = Math.max(sequence, JSON.parse(line).sequence);
  }
  function accepted(method: string, body: any, result: any) {
    if (method !== settings.method) return result;
    crashAt(settings, 'before-remote');
    if (settings.journal) appendFileSync(settings.journal, JSON.stringify({ method, body, result, sequence }) + '\n', { mode: 0o600 });
    crashAt(settings, 'after-remote');
    return result;
  }
  function planned(method: string) {
    const index = rules.findIndex(r => r.method === method);
    if (index < 0) return { matched: false };
    const rule = rules[index];
    if (rule.skip > 0) { rule.skip--; return { matched: false }; }
    rules.splice(index, 1);
    if (rule.error) throw makeError(rule.error);
    return { matched: true, result: rule.result };
  }
  const api = {
    async tg(token: string, method: string, body: any = {}) {
      calls.push({ token, method, body });
      const plan = planned(method); if (plan.matched) return accepted(method, body, plan.result);
      if (method === 'getChatMember') return { status: members.has(String(body.user_id)) ? 'member' : 'left' };
      if (method === 'createForumTopic') return { message_thread_id: ++sequence };
      if (method === 'sendMessage') return accepted(method, body, { message_id: ++sequence, chat: { id: body.chat_id }, text: body.text, entities: body.entities });
      throw new Error(`Unscripted Telegram call: ${method}`);
    },
    async ow(path: string, body?: any, key?: string, method?: string) {
      calls.push({ method: path, body, key, ...(method === undefined ? {} : { httpMethod: method }) });
      const plan = planned(path); if (plan.matched) return accepted(path, body, plan.result);
      if (['messages/reply', 'messages/send-text', 'chats/read'].includes(path)) return accepted(path, body, { messageId: 'wa-' + (++sequence) });
      throw new Error(`Unscripted OpenWA call: ${path}`);
    },
  };
  return { api, calls, logs, now: () => clock, advance: (ms: number) => { clock += ms; },
    members: (ids: string[]) => { members = new Set(ids); },
    plan: (rule: any) => rules.push({ skip: 0, ...rule }), logError: (message: string) => logs.push(JSON.parse(message)) };
}

export async function exercise(dependencies: any, fixture: any, input: any) {
  const results = [];
  for (const step of input.steps) {
    let result: any;
    try {
      let value;
      if (step.op === 'plan') { fixture.plan(step.args[0]); value = true; }
      else if (step.op === 'members') { fixture.members(step.args[0]); value = true; }
      else if (step.op === 'advance') { fixture.advance(step.args[0]); value = true; }
      else value = await dependencies[step.op](...step.args);
      if (['put', 'sql', 'map', 'reopen'].includes(step.op)) value = true;
      // Void workflow results are deliberately not a transport contract. The
      // observed calls and every durable table are compared after each step.
      result = ['put', 'sql', 'map', 'plan', 'members', 'advance', 'reopen', 'incomingMention', 'outgoingMention', 'acceptWhatsApp', 'acceptTelegram'].includes(step.op) ? { value } : {};
    } catch (error) { result = { error: dependencies.error(error) }; }
    results.push({ ...result, calls: structuredClone(fixture.calls), logs: structuredClone(fixture.logs), state: dependencies.snapshot() });
  }
  return results;
}
