import { configuration, validate } from '../src/config.ts';
import { Bridge } from '../src/bridge.ts';
import { ApiError } from '../src/api.ts';
import { fileURLToPath } from 'node:url';
import type { Config } from '../src/config.ts';

export async function setup(c: Config, bridge: Bridge) {
  const session = await bridge.api.ow('');
  if (session.status !== 'ready') throw new Error('OpenWA session is not ready; finish linking it in OpenWA first');
  await bridge.migrateBot('master');
  const bot = await bridge.api.tg(c.masterToken, 'getMe');
  const group = await bridge.api.tg(c.masterToken, 'getChat', { chat_id: c.management });
  if (!group.is_forum || group.username) throw new Error('Management destination must be a private forum supergroup');
  const member = await bridge.api.tg(c.masterToken, 'getChatMember', { chat_id: c.management, user_id: bot.id });
  if (member.status !== 'administrator' || !member.can_manage_topics) throw new Error('Master must be an administrator with Manage Topics permission');
  // Confirm n8n has an active management endpoint before connecting message sources.
  const probe = { id: 'setup-probe', type: 'session_status', data: { event: 'bridge.setup', status: 'connected' }, at: Date.now() };
  const { sign } = await import('../src/config.ts');
  await bridge.api.request(c.n8n, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ ...probe, signature: sign(JSON.stringify(probe), c.admin) }) });
  await bridge.installWebhook('master');
  const webhooks = await bridge.api.ow('webhooks');
  const url = `${c.callback}/webhooks/openwa`;
  if (!webhooks.some((w: any) => w.url === url)) {
    await bridge.api.ow('webhooks', { url, events: ['message.received', 'message.sent', 'message.edited', 'message.revoked', 'message.reaction', 'poll.vote', 'session.status', 'session.disconnected', 'message.failed'], secret: c.webhookSecret, retryCount: 5 });
  }
  return { connected: true, master: '@' + bot.username, sessionStatus: session.status };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const c = configuration(); validate(c);
  for (const field of ['masterToken', 'owner', 'management', 'openwaKey', 'session'] as const) if (!c[field]) throw new Error(`Missing configuration: ${field}`);
  const bridge = new Bridge(c);
  try { console.log(JSON.stringify(await setup(c, bridge))); }
  catch (e) {
  if (e instanceof ApiError) console.error(e.message + '. If webhook registration is refused, add wa-telegram-bridge to OpenWA SSRF_ALLOWED_HOSTS and recreate only its API service.');
  else console.error((e as Error).message);
  process.exitCode = 1;
  } finally { bridge.store.close(); }
}
