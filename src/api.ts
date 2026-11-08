import { openAsBlob } from 'node:fs';
import type { Config, Json } from './config.ts';

export class ApiError extends Error {
  retry: boolean; uncertain: boolean; delay: number; status: number;
  service?: string;
  constructor(message: string, status = 0, retry = false, uncertain = false, delay = 5000) {
    super(message); Object.assign(this, { status, retry, uncertain, delay });
  }
}
export class Api {
  c: Config;
  constructor(config: Config) { this.c = config; }
  async request(url: string, init: RequestInit = {}, mutation = false): Promise<Json> {
    let response: Response;
    try { response = await fetch(url, { ...init, redirect: 'error', signal: AbortSignal.timeout(120000) }); }
    catch (error) {
      const cause = (error as any)?.cause;
      const codes = cause?.errors ? cause.errors.map((e: any) => e.code) : [cause?.code];
      const notConnected = codes.length && codes.every((code: string) => ['ECONNREFUSED', 'ENOTFOUND', 'EAI_AGAIN'].includes(code));
      throw new ApiError(notConnected ? 'Remote service is not reachable yet' : 'Remote request failed; inspect delivery outcome', 0, !mutation || notConnected, mutation && !notConnected);
    }
    let body: Json;
    try { body = await response.json(); } catch { throw new ApiError('Invalid API response', response.status, !mutation, mutation); }
    if (!response.ok || body.ok === false) {
      const code = response.status >= 400 ? response.status : Number(body.error_code || 500);
      const uncertain = body.code === 'IDEMPOTENCY_OUTCOME_UNKNOWN' || (mutation && code >= 500 && code !== 501);
      // Only a documented rate refusal proves that an outgoing send did not happen.
      const retry = code === 429 || (!mutation && code >= 500);
      throw new ApiError(`API refused request (${code}${body.code ? ':' + body.code : ''})`, code, retry, uncertain,
        Number(body.parameters?.retry_after || response.headers.get('retry-after') || 5) * 1000);
    }
    return body;
  }
  tg(token: string, method: string, body: Json = {}) {
    return this.request(`${this.c.telegram}/bot${token}/${method}`, {
      method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(body),
    }, !['getMe', 'getChat', 'getChatMember', 'getFile', 'getWebhookInfo', 'getManagedBotToken', 'getManagedBotAccessSettings', 'getMyDescription', 'getUserProfilePhotos'].includes(method)).then(r => r.result).catch(e => { if (e instanceof ApiError) e.service = 'telegram'; throw e; });
  }
  async upload(token: string, method: string, field: string, path: string, name: string, body: Json) {
    const form = new FormData();
    for (const [k, v] of Object.entries(body)) if (v !== undefined) form.set(k, typeof v === 'object' ? JSON.stringify(v) : String(v));
    form.set(field, await openAsBlob(path), name);
    return this.request(`${this.c.telegram}/bot${token}/${method}`, { method: 'POST', body: form }, true).then(r => r.result).catch(e => { if (e instanceof ApiError) e.service = 'telegram'; throw e; });
  }
  ow(path: string, body?: Json, key?: string) {
    return this.request(`${this.c.openwa}/api/sessions/${encodeURIComponent(this.c.session)}/${path}`, {
      method: body === undefined ? 'GET' : 'POST',
      headers: { 'X-API-Key': this.c.openwaKey, 'content-type': 'application/json', ...(key ? { 'Idempotency-Key': key } : {}) },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    }, body !== undefined);
  }
}
