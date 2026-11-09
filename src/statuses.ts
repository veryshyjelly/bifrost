import { randomUUID } from 'node:crypto';
import type { Bridge } from './bridge.ts';
import { digest } from './config.ts';
import type { Json } from './config.ts';
import { ApiError } from './api.ts';
import { ensureOpenWAWebhook } from './openwa.ts';

const milliseconds = (value: unknown) => typeof value === 'number' ? value : typeof value === 'string' ? Date.parse(value) : NaN;

export class StatusFeed {
  b: Bridge;
  constructor(bridge: Bridge) { this.b = bridge; }
  get id() { return digest(JSON.stringify([this.b.c.session, this.b.c.management])); }
  async topic() {
    const saved = this.b.store.get('status-feed', this.id);
    if (saved) return saved;
    const receipt = await this.b.effect('status-topic:' + this.id, () => this.b.api.tg(this.b.token('master'), 'createForumTopic', {
      chat_id: this.b.c.management, name: 'WhatsApp Status',
    }));
    const feed = { chatId: 'status@broadcast', tgChat: this.b.c.management, thread: receipt.message_thread_id, mode: 'status' };
    this.b.store.put('status-feed', this.id, feed);
    return feed;
  }
  isTopic(message: Json) {
    const feed = this.b.store.get('status-feed', this.id);
    return !!feed && String(message.chat.id) === feed.tgChat && Number(message.message_thread_id) === Number(feed.thread);
  }
  url(feed: Json) { return `https://t.me/c/${String(feed.tgChat).replace(/^-100/, '')}/${feed.thread}`; }
  startup() {
    if (!this.b.store.get('migrated', 'master')) return;
    const key = 'status-startup:' + randomUUID();
    this.b.store.enqueue(key, 'status-control:' + this.id, 'status-startup', {});
  }
  queueRefresh(key: string) {
    this.b.store.enqueue('status-refresh:' + key, 'status-control:' + this.id, 'status-refresh', {});
  }
  async process(job: Json) {
    await this.topic();
    if (job.type === 'status-startup') await ensureOpenWAWebhook(this.b.c, this.b.api);
    const response = await this.b.api.ow('status');
    if (!Array.isArray(response.statuses)) throw new ApiError('Unexpected OpenWA Status response', 502);
    for (const status of [...response.statuses].sort((a, b) => milliseconds(a.timestamp) - milliseconds(b.timestamp))) {
      const data = { ...status, statusId: status.id, postedAt: milliseconds(status.timestamp), hasMedia: !!status.mediaUrl };
      if (!this.message(data) || this.b.store.get('wa-complete', `status@broadcast:${data.statusId}`)) continue;
      this.b.store.enqueue(`status-post:${this.id}:${data.statusId}`, 'wa:status@broadcast', 'wa', {
        sessionId: this.b.c.session, event: 'status.received', data,
      });
    }
  }
  message(data: Json): Json | undefined {
    if (!data || typeof data.statusId !== 'string' || !data.statusId || typeof data.contact?.id !== 'string'
      || !/@(?:c\.us|s\.whatsapp\.net|lid)$/.test(data.contact.id)) return;
    const posted = milliseconds(data.postedAt ?? data.timestamp), expires = milliseconds(data.expiresAt) || posted + 86400000;
    if (!Number.isFinite(posted) || posted <= 0 || !Number.isFinite(expires) || expires <= Date.now()) return;
    const supported = ['text', 'image', 'video', 'voice'].includes(data.type);
    return { id: data.statusId, statusId: data.statusId, chatId: 'status@broadcast', from: data.contact.id, author: data.contact.id,
      contact: data.contact, isGroup: false, fromMe: false, isStatusBroadcast: true, kind: 'status',
      type: supported ? data.type : 'text', body: (data.caption || '') + (supported ? '' : `\n[${data.type || 'Unsupported'} status]`),
      timestamp: Math.floor(posted / 1000), media: { omitted: data.mediaOmitted === true || data.hasMedia === false },
    };
  }
}
