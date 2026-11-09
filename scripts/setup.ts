import { configuration, validate } from '../src/config.ts';
import { Bridge } from '../src/bridge.ts';
import { ApiError } from '../src/api.ts';
import { fileURLToPath } from 'node:url';
import type { Config } from '../src/config.ts';
import { ensureOpenWAWebhook } from '../src/openwa.ts';

export async function setup(c: Config, bridge: Bridge) {
  const session = await bridge.api.ow('');
  if (session.status !== 'ready') throw new Error('OpenWA session is not ready; finish linking it in OpenWA first');
  await bridge.migrateBot('master');
  const bot = await bridge.api.tg(c.masterToken, 'getMe');
  const group = await bridge.api.tg(c.masterToken, 'getChat', { chat_id: c.management });
  if (!group.is_forum || group.username) throw new Error('Management destination must be a private forum supergroup');
  const member = await bridge.api.tg(c.masterToken, 'getChatMember', { chat_id: c.management, user_id: bot.id });
  if (member.status !== 'administrator' || !member.can_manage_topics) throw new Error('Master must be an administrator with Manage Topics permission');
  await bridge.installWebhook('master');
  for (const { id } of bridge.store.all('bot')) await bridge.installWebhook(id);
  await ensureOpenWAWebhook(c, bridge.api);
  bridge.statuses.startup();
  return { connected: true, master: '@' + bot.username, sessionStatus: session.status };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const c = configuration(); validate(c);
  for (const field of ['masterToken', 'owner', 'management', 'openwaKey', 'session'] as const) if (!c[field]) throw new Error(`Missing configuration: ${field}`);
  const bridge = new Bridge(c);
  try { console.log(JSON.stringify(await setup(c, bridge))); }
  catch (e) {
  if (e instanceof ApiError) console.error(e.message + '. If webhook registration is refused, add bifrost to OpenWA SSRF_ALLOWED_HOSTS and recreate only its API service.');
  else console.error((e as Error).message);
  process.exitCode = 1;
  } finally { bridge.store.close(); }
}
