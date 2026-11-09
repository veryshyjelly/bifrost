import type { Api } from './api.ts';
import type { Config } from './config.ts';

export const waEvents = ['message.received', 'message.sent', 'message.edited', 'message.revoked', 'message.reaction', 'poll.vote', 'status.received', 'session.status', 'session.disconnected', 'message.failed'];

export async function ensureOpenWAWebhook(c: Config, api: Api) {
  const url = `${c.callback}/webhooks/openwa`, hooks = await api.ow('webhooks');
  const existing = hooks.filter((hook: any) => hook.url === url);
  if (!existing.length) {
    await api.ow('webhooks', { url, events: waEvents, secret: c.webhookSecret, retryCount: 5 });
    return;
  }
  for (const hook of existing) {
    const events = hook.events || [];
    if (events.includes('*') || waEvents.every(event => events.includes(event))) continue;
    // Preserve secrets, filters, headers and other subscriptions on this callback.
    await api.ow('webhooks/' + encodeURIComponent(hook.id), { events: [...new Set([...events, ...waEvents])] }, undefined, 'PUT');
  }
}
