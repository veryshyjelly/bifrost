import type { Bridge } from './bridge.ts';
import { digest } from './config.ts';
import type { Json } from './config.ts';
import { ApiError } from './api.ts';

export type HistoryTarget = { chat: string; thread?: number; bot?: string };
export class ConversationHistory {
  b: Bridge;
  constructor(bridge: Bridge) { this.b = bridge; }
  scope(chat: string, target: HistoryTarget) {
    return digest(JSON.stringify([this.b.c.session, this.b.store.canonical(chat), String(target.chat), Number(target.thread || 0), String(target.chat).startsWith('-') ? '*' : target.bot]));
  }
  target(chat: string): HistoryTarget | undefined {
    const route = this.b.store.get('route', chat); if (!route) return;
    const contact = this.b.store.get('contact', chat), bot = contact?.bot && this.b.store.get('bot', contact.bot);
    return route.mode === 'dedicated' && bot?.ready ? { chat: this.b.c.owner, bot: contact.bot } : { chat: String(route.tgChat), thread: route.thread };
  }
  at(row: Json, target: HistoryTarget) {
    if (row.tg_chat !== String(target.chat) || (!String(target.chat).startsWith('-') && row.bot !== target.bot)) return false;
    const record = this.b.store.db.prepare('SELECT thread,deleted FROM telegram_messages WHERE scope=? AND chat=? AND message=?')
      .get(this.b.control.scope(row.bot, row.tg_chat), row.tg_chat, row.tg_id);
    return !record || (!record.deleted && Number(record.thread || 0) === Number(target.thread || 0));
  }
  delivered(chat: string, id: string, target: HistoryTarget) {
    const key = this.scope(chat, target) + ':' + id;
    if (this.b.store.get('destination-complete', key)) return this.b.store.byWhatsApp(chat, id).some(row => this.at(row, target));
    if (this.b.store.get('destination-start', key)) return false;
    return this.b.store.byWhatsApp(chat, id).some(row => this.at(row, target) && (row.direction === 'out' || this.b.store.get('wa-complete', chat + ':' + id)));
  }
  begin(chat: string, id: string, target: HistoryTarget, key: string) {
    const scoped = this.scope(chat, target) + ':' + id, old = this.b.store.get('destination-start', scoped);
    if (this.b.store.get('destination-complete', scoped) && !this.b.store.byWhatsApp(chat, id).some(row => this.at(row, target))) {
      this.b.store.db.prepare('DELETE FROM kv WHERE kind=? AND id=?').run('destination-complete', scoped);
      this.b.store.put('destination-start', scoped, { key }); return key;
    }
    if (!old) this.b.store.put('destination-start', scoped, { key });
    return old?.key || key;
  }
  complete(chat: string, id: string, target: HistoryTarget) {
    this.b.store.put('destination-complete', this.scope(chat, target) + ':' + id, true);
    const current = this.target(chat);
    if (current && this.scope(chat, current) === this.scope(chat, target)) this.b.store.put('wa-complete', chat + ':' + id, true);
  }
  request(chatId: string, target: HistoryTarget, count: number, key: string, enqueue = true) {
    chatId = this.b.store.canonical(chatId);
    const id = digest(JSON.stringify([this.b.c.session, chatId, target, key]));
    const previous = this.b.store.get('history-run', id); if (previous) return previous;
    const active = this.b.store.all('history-run').find(run => run.session === this.b.c.session && run.chatId === chatId && ['queued', 'fetching', 'delivering'].includes(run.status));
    if (active) return active;
    const run = { id, chatId, target, count, status: 'queued', createdAt: Date.now(), session: this.b.c.session };
    this.b.store.put('history-run', id, run);
    if (enqueue) this.b.store.enqueue('history-fetch:' + id, 'history-fetch:' + id, 'history', { run: id });
    return run;
  }
  receipt(row: Json) {
    const cached = this.b.store.get('telegram-receipt', digest(JSON.stringify([row.bot, row.tg_chat, row.tg_id])));
    if (cached) return cached;
    const results = this.b.store.db.prepare("SELECT result FROM effects WHERE state='done' AND json_extract(result,'$.message_id')=? AND CAST(json_extract(result,'$.chat.id') AS TEXT)=?")
      .all(row.tg_id, row.tg_chat).map(result => JSON.parse(result.result as string));
    const identity = row.bot === 'master' ? this.b.c.masterToken.split(':')[0] : row.bot;
    return results.find(message => String(row.tg_chat).startsWith('-') || String(message.from?.id) === identity);
  }
  archivedMedia(chat: string, id: string) {
    for (const row of this.b.store.byWhatsApp(chat, id).filter(row => row.media)) {
      let message = this.receipt(row);
      if (!message && row.direction === 'out') {
        message = this.b.store.db.prepare("SELECT payload FROM jobs WHERE type='tg' AND json_extract(payload,'$.bot')=? AND json_extract(payload,'$.update.message.message_id')=?").all(row.bot, row.tg_id)
          .map(item => JSON.parse(item.payload as string).update.message).find(m => String(m.chat.id) === row.tg_chat);
      }
      if (message && ['photo', 'video', 'animation', 'audio', 'voice', 'sticker', 'document', 'video_note'].some(kind => message[kind])) return { bot: row.bot, message };
    }
  }
  ordered(rows: Json[]) {
    const sorted = [...rows].sort((a, b) => a.timestamp - b.timestamp || a.id.localeCompare(b.id)), byId = new Map(sorted.map(row => [row.id, row]));
    const visited = new Set<string>(), visiting = new Set<string>(), result: Json[] = [];
    for (const row of sorted) {
      const stack = [{ row, expanded: false }];
      while (stack.length) {
        const next = stack.pop()!, id = next.row.id;
        if (visited.has(id)) continue;
        if (next.expanded) { visiting.delete(id); visited.add(id); result.push(next.row); continue; }
        visiting.add(id); stack.push({ row: next.row, expanded: true });
        const parent = byId.get(next.row.quotedMessage?.id);
        if (parent && !visited.has(parent.id) && !visiting.has(parent.id)) stack.push({ row: parent, expanded: false });
      }
    }
    return result;
  }
  async fetch(data: Json, key: string) {
    let run = data.run && this.b.store.get('history-run', data.run);
    if (!run) {
      const target = this.target(data.chatId); if (!target) return;
      run = this.request(data.chatId, target, data.count || this.b.c.history || 20, key, false);
    }
    if (run.session !== this.b.c.session || run.status === 'complete') return;
    let plan = this.b.store.get('history-plan', run.id);
    if (!plan) {
      this.b.store.put('history-run', run.id, { ...run, status: 'fetching' });
      const rows = await this.b.api.ow(`messages/${encodeURIComponent(run.chatId)}/history?limit=${run.count}&includeMedia=false${run.count > 100 ? '&deep=true' : ''}`);
      if (!Array.isArray(rows)) throw new ApiError('Unexpected OpenWA history response', 502);
      const unique = new Map<string, Json>();
      for (const row of rows) {
        if (!row.id || row.type === 'revoked' || row.isStatusBroadcast || (row.chatId && this.b.store.canonical(row.chatId) !== run.chatId)) continue;
        const message = { ...row, chatId: run.chatId, isGroup: run.chatId.endsWith('@g.us') };
        const archive = this.archivedMedia(run.chatId, row.id);
        if (archive) message.archiveMedia = archive;
        unique.set(row.id, message);
      }
      plan = { items: this.ordered([...unique.values()].sort((a, b) => a.timestamp - b.timestamp).slice(-run.count)) };
      this.b.store.put('history-plan', run.id, plan);
    }
    this.b.store.put('history-run', run.id, { ...run, status: 'delivering', available: plan.items.length });
    for (let index = 0; index < plan.items.length; index++) this.b.store.enqueue(`history-item:${run.id}:${index}`, 'history-delivery:' + run.id, 'history-message', { run: run.id, index });
    this.b.store.enqueue('history-finish:' + run.id, 'history-delivery:' + run.id, 'history-finish', { run: run.id });
  }
  async process(job: Json) {
    const run = this.b.store.get('history-run', job.payload.run); if (!run || run.session !== this.b.c.session) return;
    if (job.type === 'history-message') {
      const message = this.b.store.get('history-plan', run.id)?.items[job.payload.index];
      if (!message) throw new ApiError('History snapshot is missing', 409);
      await this.b.deliverWhatsApp({ event: 'message.received', sessionId: this.b.c.session, historical: true, replay: run.target, data: message }, job.dedup);
    } else {
      this.b.store.put('history-run', run.id, { ...run, status: 'complete', finishedAt: Date.now() });
      await this.b.sendText(run.target.bot || 'master', run.target.chat, `History ready: ${run.available || 0} available messages checked, oldest first. Existing or deleted messages were skipped.`, job.dedup,
        run.target.thread ? { message_thread_id: run.target.thread } : {});
    }
  }
  failed(job: Json) {
    const run = job.payload.run && this.b.store.get('history-run', job.payload.run);
    if (run && run.status !== 'complete') this.b.store.put('history-run', run.id, { ...run, status: 'failed' });
  }
}
