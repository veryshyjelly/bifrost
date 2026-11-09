export type Entity = { type: string; offset: number; length: number; [key: string]: unknown };
export type Formatted = { text: string; entities: Entity[] };
export const plain = (text: string): Formatted => ({ text: String(text), entities: [] });
export function styled(text: string, type: string, attributes = {}): Formatted {
  return { text, entities: text ? [{ type, offset: 0, length: text.length, ...attributes }] : [] };
}
export function concat(...parts: (Formatted | string)[]): Formatted {
  const result = plain('');
  for (const part of parts) {
    const value = typeof part === 'string' ? plain(part) : part;
    result.entities.push(...value.entities.map(entity => ({ ...entity, offset: result.text.length + entity.offset })));
    result.text += value.text;
  }
  return result;
}
export function whatsappURL(id: string) {
  const phone = /^\+?([1-9]\d{4,14})@(?:c\.us|s\.whatsapp\.net)$/.exec(id)?.[1];
  return 'https://wa.me/' + (phone || '');
}
export function senderName(name: string, id: string): Formatted {
  const displayed = String(name).replace(/[\r\n\t]+/g, ' ').trim();
  const result = styled(displayed, 'bold'), url = whatsappURL(id);
  if (displayed) result.entities.push({ type: 'text_link', offset: 0, length: displayed.length, url });
  return result;
}
export function historyTime(timestamp: unknown): Formatted {
  const unix = Number(timestamp), max = Math.floor(Date.now() / 1000) + 1098 * 86400;
  if (!Number.isSafeInteger(unix) || unix <= 0 || unix > max) return styled('History', 'italic');
  const fallback = new Date(unix * 1000).toISOString().slice(0, 16).replace('T', ' ') + ' UTC';
  return concat(styled('History', 'italic'), ' · ', styled(fallback, 'date_time', { unix_time: unix, date_time_format: 'dt' }));
}
export type RelayFormat = { bot: string; sender: string; name: string; own?: boolean; historical?: boolean; timestamp?: unknown; editedNotice?: boolean };
export function relayHeader(options: RelayFormat): Formatted {
  const parts: Formatted[] = [];
  if (options.editedNotice) parts.push(styled('Edited', 'italic'));
  if (options.historical) parts.push(historyTime(options.timestamp));
  if (options.own) parts.push(concat(senderName('You', options.sender), ' · ', styled('sent from WhatsApp', 'italic')));
  else if (options.bot === 'master') parts.push(senderName(options.name, options.sender));
  return parts.reduce((result, part) => result.text ? concat(result, ' · ', part) : part, plain(''));
}
export function relayText(options: RelayFormat, body: string | Formatted): Formatted {
  const header = relayHeader(options), content = typeof body === 'string' ? plain(body) : body;
  if (!header.text) return content;
  if (!content.text) return header;
  return concat(header, options.own || options.bot === 'master' ? ': ' : ' · ', content);
}
function boundary(value: Formatted, start: number, limit: number) {
  let end = Math.min(value.text.length, start + limit);
  if (end < value.text.length && /[\uD800-\uDBFF]/.test(value.text[end - 1]) && /[\uDC00-\uDFFF]/.test(value.text[end])) end--;
  for (const entity of value.entities) if (entity.type === 'date_time' && entity.offset < end && entity.offset + entity.length > end) end = entity.offset;
  if (end <= start) throw new Error('Formatted atomic span exceeds the message limit');
  return end;
}
export function sliceFormatted(value: Formatted, start: number, end: number): Formatted {
  return { text: value.text.slice(start, end), entities: value.entities.flatMap(entity => {
    const left = Math.max(start, entity.offset), right = Math.min(end, entity.offset + entity.length);
    if (right <= left || (entity.type === 'date_time' && (left !== entity.offset || right !== entity.offset + entity.length))) return [];
    return [{ ...entity, offset: left - start, length: right - left }];
  }) };
}
export function splitFormatted(value: Formatted, limit = 3800): Formatted[] {
  const result: Formatted[] = [];
  for (let start = 0; start < value.text.length;) {
    const end = boundary(value, start, limit); result.push(sliceFormatted(value, start, end)); start = end;
  }
  return result.length ? result : [plain('')];
}
export function captionFields(value: Formatted) {
  // Long captions are delivered as text separately; keep only their header on the attachment.
  if (value.text.length > 1000) throw new Error('Caption exceeds the bridge limit');
  return { caption: value.text, caption_entities: value.entities };
}
