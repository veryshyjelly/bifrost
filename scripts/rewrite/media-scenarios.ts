// Public, synthetic inputs only. The recorder loads the frozen implementation
// in another process; no candidate workflow produces these expected results.
import { createCipheriv } from 'node:crypto';

export const person = '12025550123@c.us';
const bytes = Buffer.from('synthetic-file-😀').toString('base64');
export const step = (op: string, ...args: any[]) => ({ op, args });
const route = step('put', 'route', person, { chatId: person, tgChat: '-200', mode: 'group' });
const wa = (id: string, type: string, extra: any = {}) => ({ event: 'message.received', data: {
  id, chatId: person, from: person, type, body: 'Caption 😀', timestamp: 1700000000,
  contact: { pushName: 'Alice' }, media: { data: bytes, mimetype: type === 'voice' ? 'audio/ogg' : type === 'image' ? 'image/jpeg' : undefined }, ...extra,
} });
const tg = (id: number, content: any) => ({ update_id: id, message: { message_id: id, from: { id: 123 }, chat: { id: -200, type: 'supergroup' }, caption: 'Caption 😀', ...content } });
const incoming = (id: string, type: string, extra: any = {}, options: any = {}) => ({ id, ...options, steps: [route, step('deliver', wa(id, type, extra), id)] });
const plan = (method: string, result: any) => step('plan', { method, result });
const fail = (method: string, error: any) => step('plan', { method, error });
const file = { file_id: 'file /😀', file_size: 19 };
const getFile = (path = 'folder /😀/file.bin') => plan('getFile', { file_path: path });
const download = () => ({ base64: bytes });
const cipher = createCipheriv('aes-256-gcm', Buffer.from('aa'.repeat(32), 'hex'), Buffer.alloc(12, 7));
const token = Buffer.concat([Buffer.alloc(12, 7), cipher.update('synthetic-token-42'), cipher.final(), cipher.getAuthTag()]).toString('base64');
const bot = step('put', 'bot', '42', { username: 'alice_bot', token, ready: true });
const contact = step('put', 'contact', person, { name: 'Alice', bot: '42', metadataSyncedAt: 1700000000000 });

export const mediaScenarios: any[] = [];
for (const type of ['image', 'video', 'audio', 'voice', 'sticker', 'document']) mediaScenarios.push(incoming(`incoming-${type}`, type));
for (const kind of ['photo', 'video', 'video_note', 'animation', 'audio', 'voice', 'sticker', 'document']) {
  mediaScenarios.push({ id: `outgoing-${kind}`, downloads: [download()], steps: [route, getFile(), step('telegram', 'master', tg(10, {
    [kind]: kind === 'photo' ? [{ ...file, file_id: 'small' }, file] : file,
  }), `outgoing-${kind}`)] });
}
for (const type of ['voice', 'image']) {
  mediaScenarios.push(incoming(`${type}-converted`, type, { media: { data: bytes, mimetype: type === 'voice' ? 'audio/mpeg' : 'image/heic' } }));
  mediaScenarios.push(incoming(`${type}-conversion-failed`, type, { media: { data: bytes, mimetype: 'application/unknown' } }, { conversions: [{ error: { status: 415 } }] }));
}
mediaScenarios.push(incoming('sticker-unsupported', 'sticker', {}, { conversions: [{ error: { status: 415 } }] }));
mediaScenarios.push(incoming('long-caption', 'image', { body: '😀'.repeat(600) }));
mediaScenarios.push(incoming('view-once-image', 'image', { isViewOnce: true }));
mediaScenarios.push(incoming('sticker-webm', 'sticker', { quotedMessage: { id: 'parent' } }, { conversions: [{ name: 'prepared.webm', mime: 'video/webm' }] }));
mediaScenarios.push({ id: 'long-caption-native-fallback', steps: [route, fail('sendPhoto', { status: 415, service: 'telegram' }), step('deliver', wa('long-fallback', 'image', { body: '😀'.repeat(600) }), 'long-fallback')] });
for (const type of ['image', 'sticker', 'location', 'contact', 'poll']) {
  const extra = type === 'location' ? { location: { latitude: 1, longitude: 2 } } : type === 'contact' ? { body: 'BEGIN:VCARD\nFN:Alice\nTEL:+12025550123\nEND:VCARD' } : type === 'poll' ? { poll: { name: 'Choose', options: ['One', 'Two'] } } : {};
  mediaScenarios.push({ id: `member-${type}`, steps: [route, bot, contact, step('members', ['42']), step('deliver', wa(`member-${type}`, type, extra), `member-${type}`)] });
}
for (const status of [404, 413, 503]) mediaScenarios.push(incoming(`incoming-download-${status}`, 'document', { media: {} }, { downloads: [{ status }] }));
mediaScenarios.push(incoming('incoming-inline-oversize', 'image', { media: { data: Buffer.alloc(65).toString('base64') } }));
mediaScenarios.push(incoming('incoming-omitted-status', 'image', { statusId: 'status-id', media: { omitted: true } }));
for (const status of [400, 415, 429, 503]) {
  const id = `upload-${status}`, event = wa(id, 'image');
  mediaScenarios.push({ id, steps: [route, fail('sendPhoto', { status, retry: status === 429, uncertain: status === 503, service: 'telegram' }),
    step('deliver', event, id), step('deliver', event, id)] });
}
mediaScenarios.push({ id: 'sticker-partial-retry', steps: [route,
  fail('sendSticker', { status: 429, retry: true, service: 'telegram' }), step('deliver', wa('partial', 'sticker'), 'partial'),
  step('deliver', wa('partial', 'sticker'), 'partial')] });
mediaScenarios.push({ id: 'document-refusal-no-fallback', steps: [route, fail('sendDocument', { status: 400, service: 'telegram' }),
  step('deliver', wa('document-refusal', 'document'), 'document-refusal')] });
for (const reason of ['thread_missing', 'reply_missing']) mediaScenarios.push({ id: `upload-${reason}`, steps: [route,
  fail('sendPhoto', { status: 400, service: 'telegram', reason }), step('whatsapp', wa(reason, 'image'), reason)] });
for (const status of [400, 403, 404, 503]) mediaScenarios.push({ id: `archive-${status}`, steps: [route,
  fail('getFile', { status, retry: status === 503 }), step('deliver', wa(`archive-${status}`, 'image', {
    archiveMedia: { bot: 'master', message: { photo: [file] } },
  }), `archive-${status}`)] });
mediaScenarios.push({ id: 'archive-reuse', downloads: [download()], steps: [route, getFile(), step('deliver', wa('archive', 'image', {
  archiveMedia: { bot: 'master', message: { photo: [file] } },
}), 'archive')] });
for (const variant of ['same', 'changed']) mediaScenarios.push({ id: `sticker-regeneration-${variant}`, downloads: [download(), variant === 'same' ? download() : { base64: Buffer.from('changed').toString('base64') }], steps: [route,
  getFile(), fail('messages/send-sticker', { status: 429, retry: true, service: 'openwa' }),
  step('telegram', 'master', tg(20, { sticker: file }), 'sticker'), step('removeMedia'), getFile(),
  step('telegram', 'master', tg(20, { sticker: file }), 'sticker')] });
mediaScenarios.push({ id: 'outgoing-sticker-unsupported', conversions: [{ error: { status: 415 } }], downloads: [download()], steps: [route, getFile(), step('telegram', 'master', tg(30, { sticker: file }), 'unsupported')] });
mediaScenarios.push({ id: 'outgoing-sticker-frozen-fallback', conversions: [{ error: { status: 415 } }], downloads: [download()], steps: [route, getFile(), fail('messages/send-document', { status: 429, retry: true, service: 'openwa' }), step('telegram', 'master', tg(30, { sticker: file }), 'unsupported'), step('telegram', 'master', tg(30, { sticker: file }), 'unsupported')] });
for (const kind of ['tgs', 'webm']) mediaScenarios.push({ id: `outgoing-sticker-${kind}`, downloads: [download()], steps: [route, getFile(), step('telegram', 'master', tg(33, { sticker: { ...file, is_animated: kind === 'tgs', is_video: kind === 'webm' } }), kind)] });
mediaScenarios.push({ id: 'outgoing-missing-id', downloads: [download()], steps: [route, getFile(), plan('messages/send-document', {}), step('telegram', 'master', tg(31, { document: file }), 'missing')] });
mediaScenarios.push({ id: 'outgoing-media-quote', downloads: [download()], steps: [route, step('map', person, 'parent', '-200', 9, 'master', 'in'), getFile(), step('telegram', 'master', tg(32, { document: file, reply_to_message: { message_id: 9 } }), 'quote')] });
mediaScenarios.push(incoming('location', 'location', { location: { latitude: 12.25, longitude: -32.5 } }));
mediaScenarios.push(incoming('contact', 'contact', { body: 'BEGIN:VCARD\nFN:Alice 😀\nTEL;TYPE=CELL:+1 (202) 555-0123\nEND:VCARD' }));
mediaScenarios.push(incoming('contact-malformed', 'contact', { body: 'BEGIN:VCARD\nFN:No phone\nEND:VCARD' }));
mediaScenarios.push(incoming('poll', 'poll', { poll: { name: 'Choose 😀', allowMultipleAnswers: true, options: Array.from({ length: 14 }, (_, i) => ({ name: `Option ${i} 😀` })) } }));
mediaScenarios.push(incoming('unsupported', 'unknown', { body: '' }));
for (const [kind, content] of Object.entries({ location: { latitude: 12.25, longitude: -32.5 }, contact: { first_name: 'Alice', last_name: '😀', phone_number: '+12025550123' }, poll: { question: 'Choose 😀', options: [{ text: 'One' }, { text: 'Two' }], allows_multiple_answers: true } })) {
  mediaScenarios.push({ id: `outgoing-${kind}`, steps: [route, step('telegram', 'master', tg(40, { [kind]: content }), kind)] });
}
for (const [id, message, responses] of [
  ['telegram-oversize', { document: { ...file, file_size: 65 } }, []],
  ['shared-file', { document: file }, [getFile('$SHARED/inside.bin')]],
  ['shared-symlink-escape', { document: file }, [getFile('$SHARED/escape.bin')]],
  ['shared-outside', { document: file }, [getFile('$OUTSIDE')]],
  ['shared-oversize', { document: file }, [getFile('$SHARED/large.bin')]],
] as any[]) mediaScenarios.push({ id, steps: [...responses, step('fromTelegram', message)] });
for (const [id, response] of [
  ['stream-exact-cap', { chunks: [32, 32] }],
  ['header-over-cap', { headers: { 'content-length': '65' } }],
  ['stream-over-cap', { chunks: [32, 33] }],
  ['stream-interrupted', { chunks: [10], streamError: true }],
  ['download-refused', { status: 403 }],
  ['download-timeout', { transport: { code: 'ETIMEDOUT' } }],
] as any[]) mediaScenarios.push({ id, downloads: [response], steps: [step('fromWhatsApp', { chatId: person, id: 'id /😀', type: 'document', media: { filename: '../synthetic.bin' } })] });

// Extend the already recorded JSON transport matrix with mutating multipart,
// OpenWA read/mutation and header/envelope failure combinations.
export function transportScenarios(filePath: string) {
  const rows: any[] = [];
  const adapters = [
    { adapter: 'upload', args: ['synthetic-token', 'sendPhoto', 'photo', filePath, 'fixture-😀.bin', { chat_id: '-200', caption: '😀', caption_entities: [{ type: 'bold', offset: 0, length: 2 }], absent: undefined, present: null }] },
    { adapter: 'ow', args: ['messages/send-document', { base64: bytes, chatId: person }, 'stable-key'] },
    { adapter: 'ow', args: ['chats?limit=2'] },
  ];
  for (const base of adapters) for (const response of [
    { status: 429, response: { ok: false }, headers: { 'retry-after': '7' } },
    { status: 200, response: { ok: false, error_code: 429, parameters: { retry_after: 2 } } },
    { status: 200, response: { ok: false, code: 'IDEMPOTENCY_OUTCOME_UNKNOWN', error_code: 409 } },
    { status: 500 }, { status: 501 }, { invalid: true },
    { transport: { code: 'ECONNREFUSED' } }, { transport: { code: 'ETIMEDOUT' } },
    { status: 400, response: { ok: false, description: 'private Replied message not found' } },
  ]) rows.push({ ...base, ...response });
  return rows;
}
