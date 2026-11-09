import { randomUUID } from 'node:crypto';
import type { Store } from './store.ts';

// Legacy state is keyed by the original OpenWA session. Freeze that value rather
// than re-keying hashes, receipts, saved controls or in-progress jobs. Persisted
// fields named "session" keep that name for compatibility, but now store this ID.
export function accountIdentity(store: Store, account = '', legacySession = '') {
  const saved = store.get('identity', 'account');
  if (saved) {
    if (typeof saved.id !== 'string' || !saved.id || (account && account !== saved.id)) {
      throw new Error('BIFROST_ACCOUNT_ID does not match the restored database');
    }
    return saved.id as string;
  }
  const id = account || legacySession || randomUUID();
  store.put('identity', 'account', { id });
  return id;
}
