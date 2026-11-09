import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Bridge } from '../src/bridge.ts';
import { Store } from '../src/store.ts';
import { Api, ApiError } from '../src/api.ts';
import { configuration, digest } from '../src/config.ts';
import { plain, concat, styled, relayText, historyTime, historyClock, historyDay, whatsappURL, splitFormatted, captionFields } from '../src/format.ts';
import type { Formatted } from '../src/format.ts';

const phone = '12025550123', sender = phone + '@c.us', timestamp = 1700000000;
const options = { bot: 'master', sender, name: 'Abhishek' };
function validEntities(value: Formatted) {
  for (const e of value.entities) {
    assert.ok(e.offset >= 0 && e.length > 0 && e.offset + e.length <= value.text.length);
    const span = value.text.slice(e.offset, e.offset + e.length);
    assert.ok(!/^[\uDC00-\uDFFF]/.test(span) && !/[\uD800-\uDBFF]$/.test(span));
    if (e.type === 'date_time') assert.ok(e.length <= 31);
  }
}

class Fixture extends Api {
  calls: any[] = []; seq = 100; uploadFailure = false; failText = 0; sent = 0;
  message(body: any) { return { message_id: ++this.seq, chat: { id: body.chat_id }, text: body.text, entities: body.entities }; }
  async tg(token: string, method: string, body: any = {}) {
    this.calls.push({ token, method, body });
    if (method === 'sendMessage' && ++this.sent === this.failText) throw new ApiError('Rate limited', 429, true);
    return this.message(body);
  }
  async upload(token: string, method: string, field: string, path: string, name: string, body: any) {
    this.calls.push({ token, method, field, path, name, body });
    if (this.uploadFailure) { this.uploadFailure = false; throw new ApiError('Rate limited', 429, true); }
    return this.message(body);
  }
  async ow() { throw new Error('Presentation tests must not contact WhatsApp'); }
}
function fixture(t: any) {
  const c = { ...configuration({}), key: 'aa'.repeat(32), masterToken: 'master', owner: '123', management: '-1001', session: 's1', history: 0 };
  const store = new Store(':memory:', c.key), api = new Fixture(c), bridge = new Bridge(c, store, api);
  store.put('route', 'g@g.us', { chatId: 'g@g.us', tgChat: '-200', mode: 'group' });
  store.put('contact', sender, { name: 'Abhishek', metadataSyncedAt: Date.now() });
  bridge.media.fromWhatsApp = async () => ({ path: '/fixture', name: 'photo.jpg' });
  t.after(() => store.close()); return { bridge, store, api };
}
const event = (id: string, body: string, extra: any = {}) => ({ sessionId: 's1', event: 'message.received', data: {
  id, chatId: 'g@g.us', isGroup: true, author: sender, from: sender, fromMe: false, type: 'text', body, timestamp, ...extra,
} });

test('master relay uses a bold WhatsApp-linked name and body on one line', () => {
  const message = relayText(options, 'Hi bro');
  assert.equal(message.text, 'Abhishek: Hi bro');
  assert.deepEqual(message.entities, [{ type: 'bold', offset: 0, length: 8 }, { type: 'text_link', offset: 0, length: 8, url: 'https://wa.me/' + phone }]);
});

test('verified phone JIDs open their chat; unknown identities use the generic WhatsApp launch link', () => {
  assert.equal(whatsappURL(sender), 'https://wa.me/' + phone);
  assert.equal(whatsappURL('+' + phone + '@s.whatsapp.net'), 'https://wa.me/' + phone);
  for (const id of ['', '123456789012345@lid', '123@g.us', '111@c.us', '0' + phone + '@c.us', 'https://example.com@c.us']) assert.equal(whatsappURL(id), 'https://wa.me/');
  const message = relayText({ ...options, sender: '123456789012345@lid' }, 'Hi');
  assert.deepEqual(message.entities.filter(e => e.type === 'text_link'), [{ type: 'text_link', offset: 0, length: 8, url: 'https://wa.me/' }]);
});

test('untrusted HTML characters remain literal text and cannot inject markup or links', () => {
  const name = 'A <b>& "B"\nC', body = '<a href="https://evil.example">click</a> & 2 < 3';
  const message = relayText({ ...options, name }, body);
  assert.equal(message.text, 'A <b>& "B" C: ' + body);
  assert.equal(message.entities.filter(e => e.type === 'text_link').length, 1);
  assert.ok(message.entities.filter(e => e.type === 'text_link').every(e => e.url === 'https://wa.me/' + phone));
  validEntities(message);
});

test('compact history uses linked sender and bracketed native time while member bots omit their names', () => {
  const historical = { ...options, historical: true, historyStyle: 'compact' as const, timeZone: 'UTC', timestamp };
  const master = relayText(historical, 'Hi bro');
  assert.equal(master.text, 'Abhishek [22:13]: Hi bro');
  const time = master.entities.find(e => e.type === 'date_time')!;
  assert.equal(time.unix_time, timestamp); assert.equal(time.date_time_format, 't');
  assert.equal(master.text.slice(time.offset, time.offset + time.length), '22:13');
  const member = relayText({ ...historical, bot: '42' }, 'Hi bro');
  assert.equal(member.text, '[22:13]: Hi bro');
  assert.equal(relayText({ ...historical, own: true }, 'Mine').text, 'You [22:13]: Mine');
  assert.ok(!member.entities.some(e => e.type === 'text_link')); validEntities(master); validEntities(member);
});

test('invalid source timestamps remain clearly historical without inventing a date', () => {
  for (const value of [undefined, 'invalid', -1, 1700000000000, Number.MAX_SAFE_INTEGER]) {
    const time = historyTime(value); assert.equal(time.text, 'History'); assert.ok(!time.entities.some(e => e.type === 'date_time'));
  }
});

test('compact clocks handle invalid timestamps and dates group in the configured timezone', () => {
  assert.equal(historyClock(timestamp, 'Asia/Kolkata').text, '03:43');
  assert.equal(historyClock(timestamp, 'UTC').text, '22:13');
  assert.notEqual(historyDay(timestamp, 'Asia/Kolkata').key, historyDay(timestamp, 'UTC').key);
  assert.equal(historyDay(timestamp, 'Asia/Kolkata').label.text, '15 November 2023');
  for (const value of [undefined, 'invalid', -1, 1700000000000]) {
    const message = relayText({ ...options, historical: true, historyStyle: 'compact', timestamp: value }, 'Message');
    assert.equal(message.text, 'Abhishek [time unknown]: Message');
    assert.ok(!message.entities.some(e => e.type === 'date_time'));
    assert.equal(historyDay(value, 'UTC').label.text, 'Date unknown'); validEntities(message);
  }
});

test('saved history presentations retain legacy rendering when compact style was not selected', () => {
  const old = relayText({ ...options, historical: true, timestamp }, 'Original');
  assert.equal(old.text, 'History · 2023-11-14 22:13 UTC · Abhishek: Original');
  assert.equal(old.entities.find(e => e.type === 'date_time')!.date_time_format, 'dt');
});

test('formatted splitting preserves every UTF-16 character, clips links safely and keeps native time atomic', () => {
  const message = concat(styled('😀'.repeat(2100), 'bold'), historyTime(timestamp), ' ', relayText({ ...options, name: '🦉 A&B' }, '𐐀'.repeat(2400)));
  const parts = splitFormatted(message);
  assert.equal(parts.map(p => p.text).join(''), message.text);
  for (const part of parts) { assert.ok(part.text.length <= 3800); validEntities(part); assert.ok(!/^[\uDC00-\uDFFF]/.test(part.text) && !/[\uD800-\uDBFF]$/.test(part.text)); }
  assert.equal(parts.flatMap(p => p.entities).filter(e => e.type === 'date_time').length, 1);
  const cross = splitFormatted(concat('x'.repeat(3785), historyTime(timestamp)));
  assert.equal(cross.length, 2); assert.equal(cross[1].entities.find(e => e.type === 'date_time')!.offset, 0);
});

test('caption entity offsets are safe and oversize raw captions must be handled as text', () => {
  const caption = relayText({ ...options, historical: true, timestamp }, '<photo> & 😀');
  const fields = captionFields(caption); assert.equal(fields.caption, caption.text); validEntities({ text: fields.caption, entities: fields.caption_entities });
  assert.throws(() => captionFields(plain('x'.repeat(1001))), /Caption exceeds/);
});

test('relay transport carries native entities and disables the generated WhatsApp header preview', async t => {
  const { bridge, api } = fixture(t);
  await bridge.whatsApp(event('one', 'Hi <bro> & 😀'), 'one');
  const call = api.calls.find(c => c.method === 'sendMessage');
  assert.equal(call.body.text, 'Abhishek: Hi <bro> & 😀');
  assert.equal(call.body.parse_mode, undefined);
  assert.ok(call.body.entities.some((e: any) => e.type === 'text_link' && e.url === 'https://wa.me/' + phone));
  assert.equal(call.body.link_preview_options.is_disabled, true);
});

test('sender links resolve known LID aliases rather than treating a LID as a phone number', async t => {
  const { bridge, store, api } = fixture(t); store.alias('999@lid', sender);
  await bridge.whatsApp(event('alias', 'Hi', { author: '999@lid' }), 'alias');
  assert.ok(api.calls.find(c => c.method === 'sendMessage').body.entities.some((e: any) => e.type === 'text_link' && e.url === 'https://wa.me/' + phone));
});

test('phone-originated private messages and edits never link You to the conversation partner', async t => {
  const { bridge, store, api } = fixture(t);
  store.put('route', sender, { chatId: sender, tgChat: '-200', mode: 'inbox' });
  await bridge.whatsApp(event('own', 'Hello', { chatId: sender, isGroup: false, fromMe: true }), 'own');
  const message = api.calls.find(c => c.method === 'sendMessage').body;
  assert.equal(message.text, 'You · sent from WhatsApp: Hello');
  assert.deepEqual(message.entities.filter((e: any) => e.type === 'text_link').map((e: any) => e.url), ['https://wa.me/']);
  await bridge.waChange({ event: 'message.edited', data: { chatId: sender, id: 'own', body: 'Updated' } }, 'own-edit');
  assert.deepEqual(api.calls.find(c => c.method === 'editMessageText').body.entities.filter((e: any) => e.type === 'text_link').map((e: any) => e.url), ['https://wa.me/']);
});

test('You links only to the independently verified session phone', async t => {
  const { bridge, store, api } = fixture(t), self = '12025550124';
  store.put('reaction-self', digest(bridge.c.session), { sender: self + '@c.us' });
  store.put('route', sender, { chatId: sender, tgChat: '-200', mode: 'inbox' });
  await bridge.whatsApp(event('own-known', 'Hello', { chatId: sender, isGroup: false, fromMe: true }), 'own-known');
  const links = api.calls.find(c => c.method === 'sendMessage').body.entities.filter((e: any) => e.type === 'text_link');
  assert.equal(links.length, 1); assert.equal(links[0].url, 'https://wa.me/' + self);
});

test('long media captions keep all content in formatted text and a compact linked attachment header', async t => {
  const { bridge, api } = fixture(t), body = '😀<text>&'.repeat(800);
  await bridge.whatsApp({ ...event('photo', body, { type: 'image' }), historical: true }, 'photo');
  const messages = api.calls.filter(c => c.method === 'sendMessage').map(c => c.body);
  const full = messages.map(p => p.text).join(''); assert.ok(full.endsWith(body));
  for (const packet of messages) validEntities(packet);
  const media = api.calls.find(c => c.method === 'sendPhoto').body;
  assert.equal(media.caption, 'Abhishek [03:43]');
  validEntities({ text: media.caption, entities: media.caption_entities });
  assert.ok(media.caption_entities.some((e: any) => e.type === 'date_time'));
});

test('history metadata survives formatted message edits and captions do not contain raw HTML', async t => {
  const { bridge, api } = fixture(t);
  await bridge.whatsApp({ ...event('edit', 'Original'), historical: true }, 'original');
  bridge.c.syncTimezone = 'UTC';
  await bridge.waChange({ event: 'message.edited', data: { chatId: 'g@g.us', id: 'edit', body: '<Updated> & 😀' } }, 'edit');
  const update = api.calls.find(c => c.method === 'editMessageText').body;
  assert.equal(update.text, 'Abhishek [03:43]: <Updated> & 😀');
  assert.ok(update.entities.some((e: any) => e.type === 'date_time' && e.unix_time === timestamp)); validEntities(update);
});

test('formatted packets remain immutable across a partial send retry', async t => {
  const { bridge, api } = fixture(t); api.failText = 2;
  const original = relayText(options, '😀'.repeat(4000));
  await assert.rejects(bridge.sendText('master', '-200', original, 'frozen'));
  const result = await bridge.sendText('master', '-200', relayText(options, 'Different content'), 'frozen');
  assert.equal(result.map(m => m.text).join(''), original.text);
});

test('presentation stays frozen while a media upload retries after a contact rename', async t => {
  const { bridge, store, api } = fixture(t); api.uploadFailure = true;
  const message = event('retry', 'Long caption '.repeat(150), { type: 'image' });
  await assert.rejects(bridge.whatsApp(message, 'retry'));
  store.put('contact', sender, { name: 'Changed name', metadataSyncedAt: Date.now() });
  await bridge.whatsApp(message, 'retry');
  assert.equal(api.calls.filter(c => c.method === 'sendPhoto').at(-1).body.caption, 'Abhishek');
  assert.equal(api.calls.filter(c => c.method === 'sendMessage').length, 1);
});

test('pre-upgrade partial deliveries retain their legacy packet boundaries', async t => {
  const { bridge, store, api } = fixture(t), body = 'x'.repeat(6000), original = 'Abhishek\n' + body;
  store.put('delivery-target', 'legacy', { bot: 'master', destination: '-200' });
  store.db.prepare("INSERT INTO effects VALUES(?,'done',?)").run('legacy:to:master:-200:text:0', JSON.stringify({ message_id: 99, chat: { id: '-200' }, text: original.slice(0, 3800) }));
  await bridge.whatsApp(event('legacy', body), 'legacy');
  assert.equal(original.slice(0, 3800) + api.calls.find(c => c.method === 'sendMessage').body.text, original);
});
