import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createCipheriv, createHash } from 'node:crypto';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { root, oracle } from '../../scripts/rewrite/oracle.ts';

const { configuration } = await import(`${oracle()}/src/config.ts`);
const key = 'aa'.repeat(32), person = '12025550123@c.us', group = 'test@g.us';
const config = { ...configuration({}), key, admin: 'synthetic-admin'.repeat(4), webhookSecret: 'synthetic-webhook'.repeat(4),
  owner: '123', management: '-1001', masterToken: 'master-token', account: 'stable-account', session: 's1', openwaKey: 'synthetic-openwa-key' };
const hash = (value: string) => createHash('sha256').update(value).digest('hex');
const step = (op: string, ...args: any[]) => ({ op, args });
const put = (kind: string, id: string, value: any) => step('put', kind, id, value);
const wa = (id: string, body = 'Hello 😀', extra: any = {}) => ({ sessionId: 's1', event: 'message.received', idempotencyKey: id,
  data: { id, chatId: person, from: person, type: 'text', body, timestamp: 1700000000, fromMe: false, isGroup: false, contact: { pushName: 'Alice' }, ...extra } });
const tg = (id: number, extra: any = {}) => ({ update_id: id, message: { message_id: id, from: { id: 123 }, chat: { id: -200, type: 'supergroup' }, text: 'Reply 😀', ...extra } });
const route = (id = person, extra: any = {}) => put('route', id, { chatId: id, tgChat: '-200', mode: 'group', ...extra });
function bot(id = '42', ready = true) {
  const iv = Buffer.alloc(12, 7), cipher = createCipheriv('aes-256-gcm', Buffer.from(key, 'hex'), iv);
  const token = Buffer.concat([iv, cipher.update(`synthetic-token-${id}`), cipher.final(), cipher.getAuthTag()]).toString('base64');
  return put('bot', id, { username: `person_${id}_bot`, token, ready });
}
const bound = (id = person, bot = '42') => put('contact', id, { name: 'Alice', bot, metadataSyncedAt: 1700000000000 });

function compare(t: any, steps: any[]) {
  const dir = mkdtempSync(join(tmpdir(), 'bifrost-relay-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  function replay(candidate: boolean) {
    const args = candidate ? ['x', '--bun', '--no-install', 'nbb', 'scripts/rewrite/relay.cljs'] : ['scripts/rewrite/original-relay.ts'];
    const result = Bun.spawnSync([process.execPath, ...args], { cwd: root, env: { PATH: process.env.PATH!, TZ: 'UTC' },
      stdin: Buffer.from(JSON.stringify({ config, path: join(dir, candidate ? 'candidate.db' : 'original.db'), steps })), timeout: 20000 });
    assert.equal(result.exitCode, 0, result.stderr.toString());
    return JSON.parse(result.stdout.toString());
  }
  const actual = replay(true), expected = replay(false);
  assert.equal(actual.length, steps.length);
  for (let i = 0; i < steps.length; i++) assert.deepEqual(actual[i], expected[i], `Relay transition ${i}: ${steps[i].op}`);
  return actual;
}

test('unknown contact creates one topic; completed/echo/revoked/unsupported-scope events never duplicate relay', t => {
  const first = wa('first');
  const results = compare(t, [
    step('deliver', first, 'ow:first'), step('deliver', first, 'ow:first'),
    step('deliver', wa('phone', 'Mine', { fromMe: true }), 'ow:phone'),
    step('map', person, 'echo', '-200', 9, 'master', 'out'), step('deliver', wa('echo'), 'ow:echo'),
    put('revoked', `${person}:deleted`, true), step('deliver', wa('deleted'), 'ow:deleted'),
    step('deliver', wa('channel', 'ignored', { kind: 'channel' }), 'ow:channel'),
    step('deliver', wa('broadcast', 'ignored', { isStatusBroadcast: true }), 'ow:broadcast'),
    step('deliver', wa('no-id', '', { id: '' }), 'ow:malformed'),
    step('deliver', { ...wa('session'), event: 'session.disconnected', data: { status: 'offline' } }, 'session'),
  ]);
  assert.equal(results.at(-1).calls.filter((c: any) => c.method === 'createForumTopic').length, 1);
  assert.equal(results.at(-1).calls.filter((c: any) => c.method === 'sendMessage').length, 2);
});

test('dedicated DM and group membership select the right bot; own messages use verified self identity', t => {
  compare(t, [bot(), bound(), route(person, { mode: 'dedicated', tgChat: '-1001', thread: 101 }),
    step('deliver', wa('private'), 'private'),
    step('deliver', wa('own', 'Mine', { fromMe: true }), 'own'),
    put('reaction-self', hash('stable-account'), { sender: '12025550124@c.us' }),
    step('deliver', wa('own-known', 'Mine', { fromMe: true }), 'own-known'),
    route(group), step('members', ['42']),
    step('deliver', wa('member', 'Group message', { chatId: group, isGroup: true, author: person }), 'member'),
    step('members', []), step('deliver', wa('absent', 'Group message', { chatId: group, isGroup: true, author: person }), 'absent'),
    step('deliver', wa('group-own', 'Mine', { chatId: group, isGroup: true, author: person, fromMe: true }), 'group-own'),
  ]);
});

test('text retries freeze packet bytes, target and sender presentation after binding and route changes', t => {
  const event = wa('split', '😀'.repeat(4000), { chatId: group, isGroup: true, author: person });
  const results = compare(t, [route(group), bot(), bound(), step('members', ['42']),
    step('plan', { method: 'sendMessage', skip: 1, error: { status: 429, retry: true, service: 'telegram', delay: 7000 } }),
    step('deliver', event, 'split'), route(group, { tgChat: '-300' }), bound(person, '43'), step('members', []),
    step('deliver', event, 'split'),
  ]);
  const calls = results.at(-1).calls;
  assert.equal(calls.filter((c: any) => c.method === 'getChatMember').length, 1, 'frozen target must not query new membership on retry');
  assert.ok(calls.filter((c: any) => c.method === 'sendMessage').every((c: any) => c.body.chat_id === '-200'));
});

test('a refused member send falls back to manager; missing topics repair only before any accepted or uncertain part', t => {
  compare(t, [bot(), bound(), route(person, { mode: 'dedicated', tgChat: '-1001', thread: 101 }),
    step('plan', { method: 'sendMessage', error: { status: 403, service: 'telegram' } }),
    step('deliver', wa('blocked'), 'blocked'),
    route('other@c.us', { tgChat: '-1001', thread: 201, name: 'Other' }),
    step('plan', { method: 'sendMessage', error: { status: 400, service: 'telegram', reason: 'thread_missing' } }),
    step('deliver', wa('repair', 'Hello', { chatId: 'other@c.us', from: 'other@c.us' }), 'repair'),
    route('third@c.us', { tgChat: '-1001', thread: 301 }),
    step('plan', { method: 'sendMessage', skip: 1, error: { status: 400, service: 'telegram', reason: 'thread_missing' } }),
    step('deliver', wa('partial', 'x'.repeat(5000), { chatId: 'third@c.us', from: 'third@c.us' }), 'partial'),
  ]);
});

test('ready contact DM repairs an old frozen manager target without recreating a missing topic', t => {
  compare(t, [bot(), bound(), route(person, { mode: 'dedicated', tgChat: '-1001', thread: 101 }),
    put('delivery-target', 'old', { bot: 'master', destination: '-1001', thread: 101 }),
    step('plan', { method: 'sendMessage', error: { status: 400, service: 'telegram', reason: 'thread_missing' } }),
    step('deliver', wa('old'), 'old'),
  ]);
});

test('incoming quotes honor destination/thread/deletion and private bot scope; historical previews retain UTF-16 entities', t => {
  const quote = { id: 'parent', body: '😀'.repeat(310) };
  compare(t, [route(), step('map', person, 'parent', '-200', 9, 'master', 'in'),
    step('sql', "INSERT INTO telegram_messages(scope,chat,message,bot,thread,kind,date,deleted) VALUES('*','-200',9,'master',2,'chat',1700000000000,0)"),
    step('deliver', wa('wrong-thread', 'Reply', { quotedMessage: quote }), 'wrong-thread'),
    route(person, { thread: 2 }), step('deliver', wa('right-thread', 'Reply', { quotedMessage: quote }), 'right-thread'),
    step('sql', "UPDATE telegram_messages SET deleted=1 WHERE message=9"),
    step('deliver', { ...wa('preview', 'Reply', { quotedMessage: quote }), historical: true }, 'preview'),
    bot(), bound(), route(person, { mode: 'dedicated' }),
    step('map', person, 'private-parent', '123', 20, '43', 'in'),
    step('deliver', wa('other-bot', 'Reply', { quotedMessage: { id: 'private-parent' } }), 'other-bot'),
    step('map', person, 'private-parent', '123', 21, '42', 'in'),
    step('deliver', wa('same-bot', 'Reply', { quotedMessage: { id: 'private-parent' } }), 'same-bot'),
  ]);
});

test('muted/ignored routes preserve provenance without sends; aliases/latest edits and legacy packets stay compatible', t => {
  const legacyText = 'Alice\n' + 'x'.repeat(5000);
  compare(t, [route(person, { mode: 'muted' }), step('deliver', wa('muted'), 'muted'),
    route(person, { mode: 'ignored' }), step('deliver', wa('ignored'), 'ignored'), route(),
    step('deliver', wa('lid', 'Hello', { chatId: '999@lid', senderPhone: '12025550123' }), 'lid'),
    put('latest-edit', `${person}:edited`, { body: 'Latest 😀', mentionedIds: [] }), step('deliver', wa('edited', 'Old'), 'edited'),
    put('delivery-target', 'legacy', { bot: 'master', destination: '-200' }),
    step('sql', `INSERT INTO effects VALUES('legacy:to:master:-200:text:0','done','${JSON.stringify({ message_id: 99, chat: { id: '-200' }, text: legacyText.slice(0, 3800) })}')`),
    step('deliver', wa('legacy', 'x'.repeat(5000)), 'legacy'),
  ]);
});

test('Telegram sends preserve whole-group/exact-topic routes, same-chat quotes and stable outgoing idempotency', t => {
  const results = compare(t, [route(), route('topic@g.us', { tgChat: '-1001', mode: 'inbox', thread: 101 }),
    step('telegram', 'master', tg(1, { message_thread_id: 777 }), 'tg:master:1'),
    step('telegram', 'master', { ...tg(1), update_id: 2 }, 'tg:master:2'),
    step('telegram', 'master', tg(2, { reply_to_message: { message_id: 1 } }), 'tg:master:3'),
    step('telegram', 'master', tg(3, { chat: { id: -1001, type: 'supergroup' }, message_thread_id: 102 }), 'tg:master:4'),
    step('telegram', 'master', tg(4, { chat: { id: -1001, type: 'supergroup' }, message_thread_id: 101, reply_to_message: { message_id: 1 } }), 'tg:master:5'),
    step('map', 'other@c.us', 'foreign', '-200', 20, 'master', 'in'),
    step('telegram', 'master', tg(5, { reply_to_message: { message_id: 20 } }), 'tg:master:6'),
  ]);
  assert.equal(results.at(-1).calls.filter((c: any) => c.method === 'messages/send-text').length, 2);
  assert.equal(results.at(-1).calls.find((c: any) => c.method === 'messages/send-text').key, hash('send:-200:1'));
});

test('member DMs are bot scoped, become ready, and unbound bots explain how to bind', t => {
  const dm = (id: number) => tg(id, { chat: { id: 123, type: 'private' } });
  compare(t, [bot('42', false), bot('43'), bound(),
    step('telegram', '42', dm(1), 'tg:42:1'),
    step('telegram', '43', dm(1), 'tg:43:1'),
    bound('12025550124@c.us', '43'), step('telegram', '43', dm(2), 'tg:43:2'),
    route(person, { mode: 'muted' }), step('telegram', '42', dm(3), 'tg:42:3'),
  ]);
});

test('commands/service/Status messages and admitted-but-unhandled update types cannot become WhatsApp sends', t => {
  const feedId = hash(JSON.stringify(['stable-account', '-1001']));
  compare(t, [route(), put('status-feed', feedId, { tgChat: '-1001', thread: 555 }),
    step('telegram', 'master', { update_id: 1, edited_message: tg(1, { text: '/start' }).message }, 'edit-command'),
    step('telegram', 'master', tg(2, { text: '/bad-command' }), 'bad-command'),
    step('telegram', 'master', tg(3, { new_chat_members: [{ id: 42 }] }), 'service'),
    step('telegram', 'master', tg(4, { chat: { id: -1001, type: 'supergroup' }, message_thread_id: 555 }), 'status'),
    step('telegram', 'master', { update_id: 5, poll_answer: { poll_id: 'poll', user: { id: 123 }, option_ids: [0] } }, 'poll-answer'),
    step('telegram', 'master', { update_id: 6, my_chat_member: { from: { id: 123 } } }, 'membership'),
    step('sql', "INSERT INTO telegram_messages(scope,chat,message,bot,thread,kind,date) VALUES('*','-200',7,'master',0,'command',1700000000000)"),
    step('telegram', 'master', { update_id: 7, edited_message: tg(7, { text: 'was command' }).message }, 'former-command'),
  ]);
});

test('paced worker retries definite refusals, blocks uncertain streams, preserves done receipts and separates mark-read failures', t => {
  const results = compare(t, [route(),
    step('plan', { method: 'messages/send-text', error: { status: 429, retry: true, delay: 7000, service: 'openwa' } }),
    step('acceptTelegram', 'master', tg(1)), step('acceptTelegram', 'master', tg(2)), step('tickConcurrent'),
    step('advance', 7000), step('tick'), step('plan', { method: 'chats/read', error: { status: 503, retry: true, service: 'openwa' } }),
    step('tick'), step('tick'), step('advance', 5000), step('tick'),
    step('plan', { method: 'messages/send-text', error: { status: 0, uncertain: true, service: 'openwa' } }),
    step('acceptTelegram', 'master', tg(3)), step('acceptTelegram', 'master', tg(4)), step('tick'),
    step('reopen', true), step('tick'), step('tick'), step('tick'),
  ]);
  assert.ok(results.at(-1).state.jobs.some((j: any) => j.state === 'uncertain'));
  assert.equal(results.at(-1).calls.filter((c: any) => c.method === 'messages/send-text').length, 4);
  assert.equal(results.at(-1).state.effects.find((e: any) => e.id === 'send:-200:1').state, 'done');
});

test('missing WhatsApp IDs remain uncertain and a later stream cannot bypass review', t => {
  compare(t, [route(), step('plan', { method: 'messages/send-text', result: {} }),
    step('acceptTelegram', 'master', tg(1)), step('acceptTelegram', 'master', tg(2)), step('tick'), step('tick'),
    step('reopen', true), step('tick'),
  ]);
});

test('legacy forwarding jobs converge on independent management read work', t => {
  compare(t, [step('sql', `INSERT INTO jobs(dedup,stream,type,payload,created) VALUES('legacy','legacy','n8n','${JSON.stringify({ id: 'old-read', type: 'mark_read', data: { chatId: person }, at: 1794240000000 })}',1794240000000)`),
    step('tick'), step('tick'), step('tick'),
  ]);
});

test('eight definite refusals exhaust retries while independent streams continue and unexpected errors stay uncertain', t => {
  const steps = [route(), route('other@c.us', { tgChat: '-300' }), route('third@c.us', { tgChat: '-400' }),
    step('acceptTelegram', 'master', tg(1))];
  for (let attempt = 0; attempt < 8; attempt++) steps.push(
    step('plan', { method: 'messages/send-text', error: { status: 429, retry: true, service: 'openwa' } }), step('tick'), step('advance', 300000));
  steps.push(step('acceptTelegram', 'master', tg(2, { chat: { id: -300, type: 'supergroup' } })));
  // Each failed attempt queues its own notice before the newly admitted chat.
  for (let i = 0; i < 9; i++) steps.push(step('tick'));
  steps.push(step('plan', { method: 'messages/send-text', error: { generic: true } }),
    step('acceptTelegram', 'master', tg(3, { chat: { id: -400, type: 'supergroup' } })), step('tick'), step('tick'));
  const results = compare(t, steps), jobs = results.at(-1).state.jobs;
  assert.equal(jobs.find((job: any) => job.dedup === 'tg:master:1').state, 'failed');
  assert.equal(jobs.find((job: any) => job.dedup === 'tg:master:1').attempts, 8);
  assert.equal(jobs.find((job: any) => job.dedup === 'tg:master:2').state, 'done',
    JSON.stringify(jobs.filter((job: any) => job.type === 'tg').map(({ dedup, state, attempts, error }: any) => ({ dedup, state, attempts, error }))));
  assert.equal(jobs.find((job: any) => job.dedup === 'tg:master:3').state, 'uncertain');
});

test('native mention rendering and outgoing copied/user tags preserve identity and require a current roster', t => {
  const session = hash('stable-account');
  compare(t, [bot(), bound(), step('members', ['42']), route(group),
    step('incomingMention', `Hello @12025550123 😀`, [person], group, '-200', 'mention'),
    step('members', []), step('incomingMention', `Hello @12025550123 😀`, [person], group, '-200', 'mention'),
    step('incomingMention', `Hello @12025550123`, [person], group, '-200', 'new-mention'),
    put('mention-token', 'abcd1234', { session, chat: group, person }),
    step('plan', { method: 'groups/' + encodeURIComponent(group), result: { name: 'Test', participants: [{ id: person, name: 'Alice', isAdmin: false }] } }),
    step('outgoingMention', group, { text: '😀 @person_42_bot @{Alice|abcd1234}' }, 'outgoing'),
    step('outgoingMention', group, { text: 'changed' }, 'outgoing'),
    step('outgoingMention', group, { text: '@{Alice|unknown}' }, 'bad-tag'),
    step('outgoingMention', group, { text: 'Alice', entities: [{ type: 'text_mention', offset: 0, length: 5, user: { id: 42 } }] }, 'entity'),
    step('advance', 300001), step('plan', { method: 'groups/' + encodeURIComponent(group), result: { name: 'Test', participants: [] } }),
    step('outgoingMention', group, { text: '@person_42_bot' }, 'departed'),
  ]);
});

test('invalid and departed mention tags show a scoped notice without sending or stalling later text', t => {
  const results = compare(t, [route(group), bot(), bound(),
    step('telegram', 'master', tg(1, { text: '@{Alice|unknown}' }), 'bad-mention'),
    step('plan', { method: 'groups/' + encodeURIComponent(group), result: { name: 'Test', participants: [] } }),
    step('telegram', 'master', tg(2, { text: '@person_42_bot' }), 'departed-mention'),
    step('telegram', 'master', tg(3, { text: 'Ordinary text continues' }), 'ordinary'),
  ]);
  const calls = results.at(-1).calls;
  assert.equal(calls.filter((call: any) => call.method === 'sendMessage').length, 2);
  assert.equal(calls.filter((call: any) => call.method === 'messages/send-text').length, 1);
});
