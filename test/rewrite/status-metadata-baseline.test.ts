import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { statusMetadataGoldenPath, recordStatusMetadataBaseline } from '../../scripts/rewrite/status-metadata-baseline.ts';

test('original Status races, persisted mutes, metadata permissions and quiet scheduling reproduce independently', () => {
  const first = recordStatusMetadataBaseline(), second = recordStatusMetadataBaseline();
  assert.deepEqual(first, second);
  assert.deepEqual(first.summary, JSON.parse(readFileSync(statusMetadataGoldenPath, 'utf8')));
  assert.equal(first.summary.cases.length, 33);
  assert.equal(first.summary.cases.filter((c: any) => c.inventory).length, 32);
  const photo = first.transcripts.find((c: any) => c.name.startsWith('content-hash picture caching'));
  const uploads = photo.calls.filter((c: any) => c.method === 'upload');
  assert.equal(uploads.length, 2);
  assert.ok(uploads.every((c: any) => c.args[3] === '$PHOTO-1/profile.jpg' && c.file.bytes === '/9j/2Q==' && c.file.sha256.length === 64));
  const mutes = first.transcripts.find((c: any) => c.name.startsWith('refresh honors mutes'));
  assert.ok(JSON.stringify(mutes.calls).includes(new Date(1794240000000).toISOString()), 'Date constructor and Date.now must share the injected clock');
  const reopened = first.transcripts.find((c: any) => c.name.startsWith('muted-person preferences survive'));
  assert.ok(reopened.state.length >= 2, 'the original must actually reopen SQLite');
  const quiet = first.transcripts.find((c: any) => c.name.startsWith('an unchanged scheduled metadata run'));
  assert.equal(quiet.state[0].kv.filter((r: any) => r.kind === 'sync-run').length, 2);
  assert.ok(first.transcripts.some((c: any) => c.name.startsWith('incomplete metadata and failed lookups')));
  assert.ok(first.transcripts.some((c: any) => c.name.startsWith('missing group permission')));
});
