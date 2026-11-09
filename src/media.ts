import { mkdir, writeFile, realpath, stat, unlink, readdir } from 'node:fs/promises';
import { createWriteStream } from 'node:fs';
import { pipeline } from 'node:stream/promises';
import { Readable, Transform } from 'node:stream';
import { randomUUID } from 'node:crypto';
import { resolve, basename, extname, sep } from 'node:path';
import { spawn } from 'node:child_process';
import { Api, ApiError } from './api.ts';
import type { Config, Json } from './config.ts';

export class Media {
  c: Config; api: Api;
  constructor(c: Config, api: Api) { this.c = c; this.api = api; }
  async save(bytes: Buffer, filename: string) {
    if (bytes.length > this.c.mediaLimit) throw new ApiError('Attachment exceeds configured bridge media limit', 413);
    await mkdir(this.c.media, { recursive: true, mode: 0o700 });
    const path = resolve(this.c.media, randomUUID() + extname(basename(filename)).slice(0, 12));
    await writeFile(path, bytes, { mode: 0o600 }); return path;
  }
  async download(url: string, headers: Json = {}, name = 'attachment', limit = this.c.mediaLimit) {
    const response = await fetch(url, { headers, redirect: 'error', signal: AbortSignal.timeout(120000) });
    if (!response.ok) throw new ApiError(`Attachment unavailable (${response.status})`, response.status, response.status >= 500);
    if (Number(response.headers.get('content-length')) > limit) throw new ApiError('Attachment exceeds configured bridge media limit', 413);
    const path = await this.save(Buffer.alloc(0), name);
    let size = 0;
    const cap = new Transform({ transform: (chunk, _encoding, callback) => {
      size += chunk.length; callback(size > limit ? new ApiError('Attachment exceeds configured bridge media limit', 413) : null, chunk);
    } });
    try { await pipeline(Readable.fromWeb(response.body as any), cap, createWriteStream(path, { mode: 0o600 })); }
    catch (e) { await unlink(path).catch(() => {}); throw e; }
    return path;
  }
  async fromWhatsApp(message: Json) {
    const name = basename(message.media?.filename || `attachment.${({ image: 'jpg', video: 'mp4', voice: 'ogg', sticker: 'webp', audio: 'mp3' } as Json)[message.type] || 'bin'}`);
    if (message.media?.data) return { path: await this.save(Buffer.from(message.media.data.replace(/^data:[^,]*,/, ''), 'base64'), name), name };
    if (message.statusId && message.media?.omitted) throw new ApiError('OpenWA did not retain this status media', 404);
    const endpoint = message.statusId ? `status/${encodeURIComponent(message.statusId)}/media`
      : `messages/${encodeURIComponent(message.chatId)}/${encodeURIComponent(message.id)}/media`;
    const path = await this.download(`${this.c.openwa}/api/sessions/${encodeURIComponent(this.c.session)}/${endpoint}`, { 'X-API-Key': this.c.openwaKey }, name);
    return { path, name };
  }
  async fromTelegram(token: string, message: Json) {
    const kind = ['photo', 'video', 'animation', 'voice', 'audio', 'document', 'sticker', 'video_note'].find(k => message[k]);
    if (!kind) return undefined;
    const file = kind === 'photo' ? message.photo.at(-1) : message[kind];
    if (file.file_size > this.c.mediaLimit) throw new ApiError('Attachment exceeds configured bridge media limit', 413);
    const info = await this.api.tg(token, 'getFile', { file_id: file.file_id });
    const mime = file.mime_type || ({ photo: 'image/jpeg', voice: 'audio/ogg; codecs=opus', sticker: file.is_animated ? 'application/x-tgsticker' : file.is_video ? 'video/webm' : 'image/webp', video: 'video/mp4', animation: 'video/mp4', video_note: 'video/mp4' } as Json)[kind] || 'application/octet-stream';
    const name = basename(file.file_name || `telegram.${mime.includes('webp') ? 'webp' : mime.includes('ogg') ? 'ogg' : mime.includes('jpeg') ? 'jpg' : mime.includes('mp4') ? 'mp4' : 'bin'}`);
    let path: string;
    if (info.file_path.startsWith('/')) {
      const root = await realpath('/telegram-data'); path = await realpath(info.file_path);
      if (!path.startsWith(root + sep)) throw new ApiError('Telegram returned a file outside its shared directory', 400);
      if ((await stat(path)).size > this.c.mediaLimit) throw new ApiError('Attachment exceeds configured bridge media limit', 413);
    } else {
      // Cloud-compatible fallback; the host is always the configured Telegram API endpoint.
      path = await this.download(`${this.c.telegram}/file/bot${token}/${info.file_path.split('/').map(encodeURIComponent).join('/')}`, {}, name);
    }
    return { kind, path, name, mime };
  }
  async convert(path: string, voice = false) {
    const output = await this.save(Buffer.alloc(0), voice ? 'voice.ogg' : 'image.jpg');
    const args = ['-nostdin', '-hide_banner', '-loglevel', 'error', '-y', '-i', path,
      ...(voice ? ['-vn', '-c:a', 'libopus', '-b:a', '48k'] : ['-frames:v', '1']), output];
    await new Promise<void>((accept, reject) => {
      const child = spawn('ffmpeg', args, { stdio: 'ignore', timeout: 120000 });
      child.on('error', () => reject(new ApiError('Media conversion unavailable; use original attachment', 415)));
      child.on('close', code => code === 0 ? accept() : reject(new ApiError('Media conversion failed; use original attachment', 415)));
    });
    return output;
  }
  async clean() {
    await mkdir(this.c.media, { recursive: true, mode: 0o700 });
    for (const name of await readdir(this.c.media)) {
      const path = resolve(this.c.media, name), info = await stat(path);
      if (info.isFile() && info.mtimeMs < Date.now() - 86400000) await unlink(path);
    }
  }
}
