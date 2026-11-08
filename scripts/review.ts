import { configuration } from '../src/config.ts';
import { Store } from '../src/store.ts';
const c = configuration();
const [operation = 'list', job, confirmation] = process.argv.slice(2);
if (operation === 'list') {
  const s = new Store(c.db, c.key);
  console.log(JSON.stringify(s.db.prepare("SELECT id,type,state,attempts,error,created FROM jobs WHERE state IN ('failed','uncertain') ORDER BY id").all(), null, 2)); s.close();
} else {
  if (!['retry', 'skip'].includes(operation) || !/^\d+$/.test(job || '')) throw new Error('Usage: review.ts list | retry JOB_ID [--confirmed-not-delivered] | skip JOB_ID');
  const response = await fetch(`http://127.0.0.1:${c.port}/v1/review`, { method: 'POST', headers: { authorization: 'Bearer ' + c.admin, 'content-type': 'application/json' }, body: JSON.stringify({ job: Number(job), action: operation, confirmNoDelivery: confirmation === '--confirmed-not-delivered' }) });
  console.log(await response.text()); if (!response.ok) process.exitCode = 1;
}
