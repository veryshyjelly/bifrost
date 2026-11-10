import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { goldenPath, recordMediaBaseline } from '../../scripts/rewrite/media-baseline.ts';

test('frozen media/structured/transport baseline reproduces exact ordered traces and durable transitions independently', () => {
  const first = recordMediaBaseline(), second = recordMediaBaseline();
  assert.deepEqual(first, second);
  assert.deepEqual(first.summary, JSON.parse(readFileSync(goldenPath, 'utf8')));
  const last = (id: string) => first.media[id].at(-1);
  const methods = (id: string, method: string) => last(id).calls.filter((c: any) => c.method === method);
  const kv = (id: string, kind: string) => last(id).state.kv.filter((r: any) => r.kind === kind);
  for (const kind of ['image', 'video', 'audio', 'voice', 'sticker', 'document']) {
    assert.equal(last(`incoming-${kind}`).error, undefined);
    assert.ok(last(`incoming-${kind}`).state.messages.some((r: any) => r.media === 1));
  }
  for (const kind of ['photo', 'video', 'video_note', 'animation', 'audio', 'voice', 'sticker', 'document', 'location', 'contact', 'poll']) {
    assert.equal(last(`outgoing-${kind}`).error, undefined);
    assert.equal(last(`outgoing-${kind}`).state.messages.length, 1);
  }
  assert.equal(methods('sticker-partial-retry', 'sendMessage').length, 1, 'accepted label must survive retry');
  assert.equal(methods('sticker-partial-retry', 'sendSticker').length, 2);
  assert.equal(methods('upload-400', 'sendDocument').length, 1, 'definitive native refusal uses original bytes');
  assert.equal(methods('upload-503', 'sendPhoto').length, 1, 'ambiguous mutation cannot automatically resend');
  assert.equal(methods('upload-503', 'sendDocument').length, 0);
  assert.equal(last('sticker-regeneration-changed').error.status, 409);
  assert.equal(methods('sticker-regeneration-changed', 'messages/send-sticker').length, 1);
  assert.equal(methods('sticker-regeneration-same', 'messages/send-sticker').length, 2);
  assert.equal(methods('outgoing-sticker-frozen-fallback', 'sticker').length, 1);
  assert.equal(methods('outgoing-sticker-frozen-fallback', 'messages/send-document').length, 2);
  assert.equal(methods('outgoing-sticker-frozen-fallback', 'messages/send-sticker').length, 0);
  assert.equal(methods('member-image', 'sendPhoto')[0].token, 'synthetic-token-42');
  assert.equal(methods('member-image', 'sendPhoto')[0].body.caption, 'Caption 😀');
  assert.equal(last('outgoing-missing-id').error.uncertain, true);
  assert.equal(last('outgoing-missing-id').state.messages.length, 0);
  assert.equal(last('shared-symlink-escape').error.status, 400);
  assert.equal(last('shared-outside').error.status, 400);
  assert.equal(last('shared-oversize').error.status, 413);
  assert.equal(last('shared-file').value.path, '$FIXTURE/shared-file/shared/inside.bin');
  assert.equal(last('shared-file').value.mime, 'application/octet-stream');
  assert.equal(last('stream-over-cap').error.status, 413);
  assert.equal(last('stream-over-cap').state.files.length, 0);
  assert.equal(Buffer.from(last('stream-exact-cap').state.files[0].bytes, 'base64').length, 64);
  assert.equal(last('stream-interrupted').state.files.length, 0);
  assert.equal(methods('archive-reuse', 'download').length, 1);
  assert.ok(methods('archive-reuse', 'download')[0].url.startsWith('http://synthetic-telegram/file/'));
  const buttons = methods('poll', 'sendMessage')[0].body.reply_markup.inline_keyboard;
  assert.equal(buttons.length, 12);
  // Preserve callback-to-row identity, including raw JSON payload values.
  assert.equal(new Set(buttons.map((row: any) => row[0].callback_data)).size, 12);
  assert.equal(kv('poll', 'action').length, 12);
  for (const row of buttons) assert.ok(kv('poll', 'action').some((a: any) => a.id === row[0].callback_data.slice(5)));
  for (const id of ['incoming-image', 'incoming-sticker', 'outgoing-document']) {
    assert.ok(last(id).state.files.every((f: any) => f.mode === 0o600));
  }
  assert.equal(first.transport[0].error.delay, 7000);
  assert.equal(first.transport[2].error.uncertain, true);
  assert.equal(first.transport[6].error.retry, true);
  assert.equal(first.transport[7].error.uncertain, true);
});
