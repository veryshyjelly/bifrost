import { Bridge } from '../src/bridge.ts';
import { configuration } from '../src/config.ts';

const bridge = new Bridge(configuration());
try {
  const routes = bridge.store.all('route').filter(r => r.chatId.endsWith('@g.us') && r.thread && r.tgChat === bridge.c.management && !['muted', 'ignored'].includes(r.mode));
  for (const route of routes) await bridge.sendMirrorButton(route.chatId, 'group-button-v1:' + route.chatId);
  console.log(JSON.stringify({ inboxGroupButtonsAdded: routes.length }));
} catch (e) { console.error((e as Error).message); process.exitCode = 1; }
finally { bridge.store.close(); }
