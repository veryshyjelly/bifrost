import { createHash } from 'node:crypto';
import { readFile } from 'node:fs/promises';
import type { Bridge } from './bridge.ts';
import type { Json } from './config.ts';
import { digest } from './config.ts';
import { ApiError } from './api.ts';
import { cronSchedule } from './cron.ts';

const clip = (value: string, max: number) => {
  let result = '';
  for (const char of String(value).replace(/[\x00-\x08\x0b\x0c\x0e-\x1f]/g, '')) { if (result.length + char.length > max) break; result += char; }
  return result;
};
export type SyncRequest = { scope: 'all' | 'group' | 'contact'; id?: string; source: string; requestId: string; notify?: Json };

export class MetadataSync {
  b: Bridge; matches: (date: Date) => boolean;
  constructor(bridge: Bridge) { this.b = bridge; this.matches = cronSchedule(bridge.c.syncCron, bridge.c.syncTimezone); }
  get session() { return digest(this.b.c.account); }
  queue(request: SyncRequest) {
    if (request.scope !== 'all' && !request.id) throw new ApiError('Choose a conversation to sync', 400);
    const existing = this.b.store.all('sync-run').find(r => r.session === this.session && ['queued', 'running'].includes(r.status)
      && (r.scope === 'all' || (r.scope === request.scope && r.target === request.id)));
    if (existing) {
      if (request.notify) this.b.store.put('sync-waiter', `${existing.id}:${digest(JSON.stringify(request.notify))}`, { run: existing.id, notify: request.notify });
      return { run: existing.id, queued: false };
    }
    const run = digest(this.session + ':' + request.requestId).slice(0, 24);
    if (this.b.store.get('sync-run', run)) return { run, queued: false };
    this.b.store.put('sync-run', run, { ...request, target: request.id, session: this.session, status: 'queued', startedAt: Date.now() });
    this.b.store.enqueue('sync-start:' + run, 'sync-start:' + run, 'sync-start', { run });
    return { run, queued: true };
  }
  cron(now = new Date()) {
    if (!this.b.status().configured || !this.matches(now)) return;
    return this.queue({ scope: 'all', source: 'cron', requestId: 'cron:' + this.b.c.syncCron + ':' + Math.floor(now.getTime() / 60000) });
  }
  target(run: string, kind: string, id: string) {
    this.b.store.enqueue(`sync-target:${run}:${kind}:${id}`, `sync-target:${run}:${kind}:${id}`, 'sync-target', { run, kind, id });
  }
  async process(job: Json) {
    const p = job.payload, run = this.b.store.get('sync-run', p.run);
    if (!run || run.session !== this.session) return;
    if (job.type === 'sync-start') {
      this.b.store.put('sync-run', p.run, { ...run, status: 'running' });
      if (run.scope === 'all') {
        for (const r of this.b.store.all('route').filter(r => r.chatId.endsWith('@g.us') && !['muted', 'ignored'].includes(r.mode))) this.target(p.run, 'group', r.chatId);
        for (const c of this.b.store.all('contact').filter(c => c.bot && this.b.store.get('bot', c.bot))) this.target(p.run, 'contact', c.id);
      } else this.target(p.run, run.scope, run.target);
      this.b.store.enqueue('sync-finish:' + p.run, 'sync-finish:' + p.run, 'sync-finish', { run: p.run });
    } else if (job.type === 'sync-target') {
      const result: Json = { run: p.run, kind: p.kind, target: p.id, changed: 0, warnings: [], missingBots: [], skippedPhotos: 0 };
      try {
        if (p.kind === 'group') await this.group(p.id, p.run, result);
        else if (p.kind === 'contact') await this.contact(p.id, p.run, result);
      } catch (e) {
        if (e instanceof ApiError && e.retry && job.attempts < 4) throw e;
        result.warnings.push(e instanceof ApiError ? e.message : 'Metadata lookup failed');
      }
      this.b.store.put('sync-result', `${p.run}:${p.kind}:${p.id}`, result);
    } else if (job.type === 'sync-finish') await this.finish(p.run, run, job.dedup);
  }
  async change(result: Json, key: string, action: () => Promise<Json>) {
    try { await this.b.effect(key, action); result.changed++; }
    catch (e) {
      if (e instanceof ApiError && e.retry) throw e;
      result.warnings.push(e instanceof ApiError ? e.message : 'Telegram metadata update failed');
    }
  }
  async picture(kind: 'group' | 'contact', id: string, url: string | null | undefined, token: string, destination: string, current: Json, run: string, result: Json) {
    if (!url) { result.skippedPhotos++; return; }
    const parsed = new URL(url);
    if (parsed.protocol !== 'https:' || parsed.username || parsed.password || !/(^|\.)(whatsapp\.net|whatsapp\.com|fbcdn\.net)$/i.test(parsed.hostname)) throw new ApiError('Profile picture URL is outside the WhatsApp CDN', 400);
    const path = await this.b.media.download(url, {}, 'profile.jpg', Math.min(this.b.c.mediaLimit, 8 * 1024 * 1024));
    const bytes = await readFile(path), hash = createHash('sha256').update(bytes).digest('hex');
    const cacheKey = `${this.session}:${kind}:${id}`, cache = this.b.store.get('sync-photo', cacheKey);
    const photoId = kind === 'group' ? current.photo?.big_file_unique_id : current.photos?.[0]?.at(-1)?.file_unique_id;
    if (cache?.hash === hash && cache.photoId === photoId) return;
    const jpg = bytes[0] === 0xff && bytes[1] === 0xd8 ? path : await this.b.media.convert(path);
    const before = result.changed;
    await this.change(result, `sync-photo:${run}:${kind}:${id}:${hash}`, () => kind === 'group'
      ? this.b.api.upload(token, 'setChatPhoto', 'photo', jpg, 'profile.jpg', { chat_id: destination })
      : this.b.api.upload(token, 'setMyProfilePhoto', 'image', jpg, 'profile.jpg', { photo: { type: 'static', photo: 'attach://image' } }));
    if (result.changed !== before) {
      const latest = kind === 'group' ? await this.b.api.tg(token, 'getChat', { chat_id: destination }) : await this.b.api.tg(token, 'getUserProfilePhotos', { user_id: Number(destination), limit: 1 });
      this.b.store.put('sync-photo', cacheKey, { hash, photoId: kind === 'group' ? latest.photo?.big_file_unique_id : latest.photos?.[0]?.at(-1)?.file_unique_id });
    }
  }
  async group(id: string, run: string, result: Json) {
    const route = this.b.store.get('route', id);
    if (!route) throw new ApiError('WhatsApp group is not linked to an inbox topic or mirror', 404);
    const info = await this.b.api.ow('groups/' + encodeURIComponent(id));
    if (typeof info.name !== 'string' || !info.name.trim() || !Array.isArray(info.participants)) throw new ApiError('OpenWA group metadata or member list is incomplete; existing data preserved', 502);
    const members = info.participants.map((p: Json) => {
      if (typeof p.id !== 'string') throw new ApiError('OpenWA returned an invalid group member; existing roster preserved', 502);
      return { id: p.id, canonical: this.b.store.canonical(p.id), name: p.name || '', isAdmin: !!p.isAdmin, isSuperAdmin: !!p.isSuperAdmin };
    });
    const old = this.b.store.get('sync-group', `${this.session}:${id}`);
    const normalized = members.map((m: Json) => `${m.canonical}:${m.isAdmin}:${m.isSuperAdmin}`).sort();
    result.added = old ? members.filter((m: Json) => !old.members.some((o: Json) => o.canonical === m.canonical)).length : 0;
    result.removed = old ? old.members.filter((o: Json) => !members.some((m: Json) => m.canonical === o.canonical)).length : 0;
    result.rosterChanged = !old || digest(JSON.stringify(normalized)) !== old.rosterHash;
    result.members = members.length;
    this.b.store.put('sync-group', `${this.session}:${id}`, { name: info.name, description: info.description, members, rosterHash: digest(JSON.stringify(normalized)), fetchedAt: Date.now() });
    const previousTopicName = route.topicSyncedName ?? route.name;
    this.b.store.put('route', id, { ...route, name: info.name, topicSyncedName: previousTopicName, metadataSyncedAt: Date.now() });
    for (const m of members) {
      const contact = this.b.store.get('contact', m.canonical);
      if (contact?.bot && this.b.store.get('bot', contact.bot)) this.target(run, 'contact', m.canonical);
    }
    if (route.tgChat === this.b.c.management) {
      if (route.thread && clip(previousTopicName, 128) !== clip(info.name, 128)) {
        const before = result.changed;
        await this.change(result, `sync:${run}:topic:${id}`, () => this.b.api.tg(this.b.token('master'), 'editForumTopic', { chat_id: route.tgChat, message_thread_id: route.thread, name: clip(info.name, 128) }));
        if (result.changed !== before) this.b.store.put('route', id, { ...this.b.store.get('route', id), topicSyncedName: info.name });
      }
      return; // A group's metadata must never overwrite the shared management group's identity.
    }
    const token = this.b.token('master'), me = await this.b.api.tg(token, 'getMe');
    const [tg, membership] = await Promise.all([this.b.api.tg(token, 'getChat', { chat_id: route.tgChat }), this.b.api.tg(token, 'getChatMember', { chat_id: route.tgChat, user_id: me.id })]);
    if (!['group', 'supergroup'].includes(tg.type) || tg.username) throw new ApiError('Metadata target must remain a private Telegram group', 400);
    if (membership.status === 'administrator' && membership.can_change_info) {
      if (tg.title !== clip(info.name, 128)) await this.change(result, `sync:${run}:title:${id}`, () => this.b.api.tg(token, 'setChatTitle', { chat_id: route.tgChat, title: clip(info.name, 128) }));
      if (typeof info.description === 'string' && (tg.description || '') !== clip(info.description, 255)) await this.change(result, `sync:${run}:description:${id}`, () => this.b.api.tg(token, 'setChatDescription', { chat_id: route.tgChat, description: clip(info.description, 255) }));
      const picture = await this.b.api.ow(`groups/${encodeURIComponent(id)}/picture`);
      await this.picture('group', id, picture.url, token, route.tgChat, tg, run, result);
    } else {
      result.warnings.push('Enable Change Group Info for Bifrost to synchronize this group’s title, description and picture.');
      const action = this.b.action({ kind: 'sync-permission', chatId: id, tgChat: String(route.tgChat), owner: this.b.c.owner, session: this.session, expires: Date.now() + 86400000 });
      result.permissionLink = `https://t.me/${me.username}?startgroup=sync_${action}&admin=change_info+delete_messages+invite_users`;
    }
    for (const m of members) {
      const contact = this.b.store.get('contact', m.canonical), bot = contact?.bot && this.b.store.get('bot', contact.bot);
      if (!bot?.username) continue;
      const member = await this.b.api.tg(token, 'getChatMember', { chat_id: route.tgChat, user_id: Number(contact.bot) }).catch(e => { if (e instanceof ApiError && e.status === 400) return { status: 'left' }; throw e; });
      if (!['member', 'administrator', 'creator'].includes(member.status)) result.missingBots.push({ bot: contact.bot, username: bot.username, name: contact.name || bot.username });
    }
  }
  async contact(id: string, run: string, result: Json) {
    const contact = this.b.store.get('contact', id);
    if (!contact) throw new ApiError('WhatsApp contact is not registered', 404);
    const source = await this.b.api.ow('contacts/' + encodeURIComponent(id));
    const name = source.name?.trim() || source.pushName?.trim() || contact.name || id;
    this.b.store.put('contact', id, { ...contact, name, metadataSyncedAt: Date.now() });
    const route = this.b.store.get('route', id);
    if (route) {
      const previous = route.topicSyncedName ?? route.name;
      this.b.store.put('route', id, { ...route, name, topicSyncedName: previous });
      if (route.tgChat === this.b.c.management && route.thread && clip(previous, 128) !== clip(name, 128)) {
        const before = result.changed;
        await this.change(result, `sync:${run}:contact-topic:${id}`, () => this.b.api.tg(this.b.token('master'), 'editForumTopic', { chat_id: route.tgChat, message_thread_id: route.thread, name: clip(name, 128) }));
        if (result.changed !== before) this.b.store.put('route', id, { ...this.b.store.get('route', id), topicSyncedName: name });
      }
    }
    if (!contact.bot || !this.b.store.get('bot', contact.bot)) return;
    const token = this.b.token(contact.bot), me = await this.b.api.tg(token, 'getMe');
    if (me.first_name !== clip(name, 64)) await this.change(result, `sync:${run}:bot-name:${id}`, () => this.b.api.tg(token, 'setMyName', { name: clip(name, 64) }));
    const description = clip(`WhatsApp bridge for ${name}. Replies are sent through your WhatsApp account.`, 512);
    const current = await this.b.api.tg(token, 'getMyDescription');
    if (current.description !== description) await this.change(result, `sync:${run}:bot-description:${id}`, () => this.b.api.tg(token, 'setMyDescription', { description }));
    const [picture, photos] = await Promise.all([this.b.api.ow(`contacts/${encodeURIComponent(id)}/profile-picture`), this.b.api.tg(token, 'getUserProfilePhotos', { user_id: me.id, limit: 1 })]);
    await this.picture('contact', id, picture.url, token, String(me.id), photos, run, result);
  }
  async finish(id: string, run: Json, key: string) {
    const results = this.b.store.all('sync-result').filter(r => r.run === id);
    const warnings = results.flatMap(r => r.warnings), missing = results.flatMap(r => r.missingBots);
    const report = { run: id, finishedAt: Date.now(), source: run.source, targets: results.length,
      groups: results.filter(r => r.kind === 'group').length, contacts: results.filter(r => r.kind === 'contact').length,
      changed: Number(this.b.store.db.prepare("SELECT count(*) AS n FROM effects WHERE state='done' AND (id LIKE ? OR id LIKE ?)").get(`sync:${id}:%`, `sync-photo:${id}:%`)!.n), warnings: warnings.length, missingBots: missing.length,
      skippedPhotos: results.reduce((n, r) => n + r.skippedPhotos, 0) };
    this.b.store.put('sync-run', id, { ...run, ...report, status: 'completed' });
    this.b.store.put('sync-status', 'last', report);
    const rosterChanged = results.some(r => r.rosterChanged);
    const waiters = this.b.store.all('sync-waiter').filter(w => w.run === id).map(w => w.notify);
    const quiet = run.source === 'cron' && !report.changed && !warnings.length && !rosterChanged;
    if (quiet && !waiters.length) return;
    const text = `WhatsApp metadata sync complete: ${report.groups} groups, ${report.contacts} contacts, ${report.changed} fields updated.\n${warnings.length} warnings; ${missing.length} contact bots need adding; ${report.skippedPhotos} pictures unavailable or hidden.`
      + (warnings.length ? '\n' + [...new Set(warnings)].slice(0, 4).join('\n') : '');
    const buttons = missing.slice(0, 8).map(b => [{ text: 'Add ' + clip(b.name, 40), url: `https://t.me/${b.username}?startgroup=bridge` }]);
    for (const result of results.filter(r => r.permissionLink).slice(0, 3)) buttons.push([{ text: 'Allow info edits · ' + clip(this.b.store.get('route', result.target)?.name || 'group', 30), url: result.permissionLink }]);
    const destinations = [...(quiet ? [] : [run.notify || { bot: 'master', chat: this.b.c.management }]), ...waiters];
    const unique = new Map(destinations.map(d => [JSON.stringify(d), d]));
    for (const [destinationKey, destination] of unique) await this.b.sendText(destination.bot || 'master', destination.chat, text, key + ':' + digest(destinationKey),
      { ...(destination.thread ? { message_thread_id: destination.thread } : {}), ...(buttons.length ? { reply_markup: { inline_keyboard: buttons } } : {}) });
  }
}
