import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile, readdir } from 'node:fs/promises';
import { Media } from '../src/media.ts';
import { Api, ApiError } from '../src/api.ts';
import { configuration } from '../src/config.ts';
import { mediaServer, mediaError } from './helpers/media-server.ts';

const item = (type = 'document') => ({ id: 'false_g@g.us_FILE', chatId: 'g@g.us', type });
const configured = (url: string, directory: string, extra = {}) => {
  const c = { ...configuration({}), openwa: url, session: 'rotated-session', openwaKey: 'rotated-key', media: directory, ...extra };
  return new Media(c, new Api(c));
};

for (const [type, mime, name] of [
  ['image', 'image/jpeg', 'photo.jpg'], ['video', 'video/mp4', 'movie.mp4'],
  ['voice', 'audio/ogg', 'voice.ogg'], ['audio', 'audio/mpeg', 'song.mp3'],
  ['document', 'application/pdf', 'résumé.pdf'], ['sticker', 'image/webp', 'static.webp'],
  ['sticker', 'image/webp', 'animated.webp'],
]) test(`stored miss downloads ${name} on demand and preserves MIME/name`, async t => {
  const requests: string[] = [];
  const { url, directory } = await mediaServer(t, (req, res) => {
    requests.push(req.url!);
    assert.equal(req.headers['x-api-key'], 'rotated-key');
    assert.ok(req.url!.startsWith('/api/sessions/rotated-session/messages/'));
    if (!req.url!.includes('/download?')) return mediaError(res, 404, 'MEDIA_NOT_STORED');
    assert.ok(req.url!.includes('maxBytes=104857600'));
    res.writeHead(200, { 'content-type': mime, 'content-disposition': `attachment; filename*=UTF-8''${encodeURIComponent(name)}` });
    res.end('source bytes');
  });
  const attachment = await configured(url, directory).fromWhatsApp(item(type));
  assert.equal(attachment.name, name); assert.equal(attachment.mime, mime);
  assert.equal(await readFile(attachment.path, 'utf8'), 'source bytes'); assert.equal(requests.length, 2);
});

test('archive and inline reuse avoid a WhatsApp source request', async t => {
  let requests = 0;
  const { url, directory } = await mediaServer(t, (req, res) => {
    requests++; assert.ok(!req.url!.includes('/download')); res.end('archive bytes');
  });
  const media = configured(url, directory);
  assert.equal(await readFile((await media.fromWhatsApp(item())).path, 'utf8'), 'archive bytes');
  const inline = await media.fromWhatsApp({ ...item(), media: { data: Buffer.from('inline').toString('base64'), filename: 'name.pdf', mimetype: 'application/pdf' } });
  assert.equal(inline.name, 'name.pdf'); assert.equal(inline.mime, 'application/pdf'); assert.equal(requests, 1);
});

test('only a documented cache miss triggers source download; source absence remains distinct', async t => {
  const requests: string[] = [];
  const { url, directory } = await mediaServer(t, (req, res) => {
    requests.push(req.url!);
    if (req.url!.includes('/download')) return mediaError(res, 404, 'MEDIA_SOURCE_UNAVAILABLE');
    if (req.url!.includes('unknown')) return mediaError(res, 404, 'SESSION_NOT_FOUND');
    mediaError(res, 404, 'MEDIA_NOT_STORED');
  });
  const media = configured(url, directory);
  await assert.rejects(media.fromWhatsApp(item()), (e: ApiError) => e.status === 404 && e.code === 'MEDIA_SOURCE_UNAVAILABLE');
  assert.equal(requests.length, 2);
  await assert.rejects(media.fromWhatsApp({ ...item(), id: 'unknown' }), (e: ApiError) => e.status === 502 && e.retry);
  assert.equal(requests.length, 3); assert.deepEqual(await readdir(directory), []);
});

for (const status of [401, 403, 408, 409, 429, 503]) test(`stored HTTP ${status} does not trigger a source request`, async t => {
  let requests = 0;
  const { url, directory } = await mediaServer(t, (_req, res) => { requests++; mediaError(res, status, 'REFUSED'); });
  await assert.rejects(configured(url, directory).fromWhatsApp(item()), (e: ApiError) => e.status === status && e.retry === ([408, 409, 429, 503].includes(status)) && e.delay === 2000);
  assert.equal(requests, 1);
});

test('document MIME metadata survives OpenWA’s inert Content-Type and disabled downloads remain explicit', async t => {
  let disabled = false;
  const { url, directory } = await mediaServer(t, (req, res) => {
    if (!req.url!.includes('/download')) return mediaError(res, 404, 'MEDIA_NOT_STORED');
    if (disabled) return mediaError(res, 409, 'MEDIA_DOWNLOAD_DISABLED');
    res.writeHead(200, { 'content-type': 'application/octet-stream', 'x-media-mimetype': 'application/pdf' }); res.end('pdf');
  });
  const media = configured(url, directory);
  assert.equal((await media.fromWhatsApp(item())).mime, 'application/pdf');
  disabled = true;
  await assert.rejects(media.fromWhatsApp(item()), (e: ApiError) => e.status === 409 && !e.retry);
});

test('legacy cache misses work but a missing download route is retryable, without declaring source unavailable', async t => {
  const { url, directory } = await mediaServer(t, (req, res) => {
    res.writeHead(404, { 'content-type': 'application/json' });
    res.end(JSON.stringify({ message: req.url!.includes('/download') ? 'Cannot GET route' : 'No media stored for this message' }));
  });
  await assert.rejects(configured(url, directory).fromWhatsApp(item()), (e: ApiError) => e.status === 502 && e.retry);
});

test('source rate limits remain retryable and the next attempt can finish', async t => {
  let attempts = 0;
  const { url, directory } = await mediaServer(t, (req, res) => {
    if (!req.url!.includes('/download')) return mediaError(res, 404, 'MEDIA_NOT_STORED');
    if (++attempts === 1) return mediaError(res, 429, 'RATE_LIMITED');
    res.end('retried');
  });
  const media = configured(url, directory);
  await assert.rejects(media.fromWhatsApp(item()), (e: ApiError) => e.status === 429 && e.retry && e.delay === 2000);
  assert.equal(await readFile((await media.fromWhatsApp(item())).path, 'utf8'), 'retried');
});

for (const declared of [true, false]) test(`oversized ${declared ? 'declared' : 'streamed'} source bytes leave no partial file`, async t => {
  const { url, directory } = await mediaServer(t, (req, res) => {
    if (!req.url!.includes('/download')) return mediaError(res, 404, 'MEDIA_NOT_STORED');
    res.writeHead(200, declared ? { 'content-length': '32' } : {}); res.end('x'.repeat(32));
  });
  await assert.rejects(configured(url, directory, { mediaLimit: 8 }).fromWhatsApp(item()), (e: ApiError) => e.status === 413);
  assert.deepEqual(await readdir(directory), []);
});

test('cancelled source transfer removes incomplete bytes and remains retryable', async t => {
  const abort = new AbortController();
  const { url, directory } = await mediaServer(t, (req, res) => {
    if (!req.url!.includes('/download')) return mediaError(res, 404, 'MEDIA_NOT_STORED');
    res.write('partial'); setTimeout(() => abort.abort(), 20);
  });
  await assert.rejects(configured(url, directory).fromWhatsApp(item(), abort.signal), (e: ApiError) => e.retry);
  assert.deepEqual(await readdir(directory), []);
});
