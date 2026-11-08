import { configuration } from '../src/config.ts';
import { Store } from '../src/store.ts';
import { Bridge } from '../src/bridge.ts';

// Safe to invoke with docker compose exec: only queues work; the running worker performs API calls.
const c = configuration(), store = new Store(c.db, c.key), bridge = new Bridge(c, store);
try {
  const id = process.argv[2];
  const scope = !id || id === 'all' ? 'all' : id.endsWith('@g.us') ? 'group' : 'contact';
  const result = bridge.metadata.queue({ scope, id: scope === 'all' ? undefined : id, source: 'command', requestId: 'cli:' + Date.now() });
  console.log(JSON.stringify(result));
} finally { store.close(); }
