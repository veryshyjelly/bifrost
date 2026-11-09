import { mkdir, mkdtemp, readFile, copyFile, rm, stat, chmod } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { randomUUID } from 'node:crypto';
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { ApiError } from './api.ts';

export type StickerTarget = 'telegram' | 'whatsapp';
export type StickerFile = { path: string; name: string; mime?: string; stickerFormat?: 'static' | 'animated' | 'video' };
// Chromium and encoders are deliberately serialized across all Media instances.
let pending: Promise<unknown> = Promise.resolve();

export function prepareSticker(input: StickerFile, target: StickerTarget, media: string, limit: number, timeoutMs = 120000): Promise<StickerFile> {
  const job = pending.then(async () => {
    media = resolve(media);
    await mkdir(media, { recursive: true, mode: 0o700 });
    const directory = await mkdtemp(join(media, '.sticker-'));
    try {
      if ((await stat(input.path)).size > limit) throw new Error('Input too large');
      await new Promise<void>((accept, reject) => {
        const child = spawn(process.execPath, [fileURLToPath(new URL('./sticker-worker.ts', import.meta.url)), resolve(input.path), target, directory],
          { stdio: 'ignore', detached: true });
        const kill = () => { try { process.kill(-child.pid!, 'SIGKILL'); } catch {} };
        const timer = setTimeout(() => { kill(); reject(new Error('Sticker conversion timed out')); }, timeoutMs);
        child.on('error', error => { clearTimeout(timer); kill(); reject(error); });
        child.on('close', code => { clearTimeout(timer); kill(); code === 0 ? accept() : reject(new Error('Sticker conversion failed')); });
      });
      const result = JSON.parse(await readFile(join(directory, 'result.json'), 'utf8'));
      const extension = result.format === 'video' ? 'webm' : 'webp';
      const path = join(media, randomUUID() + '.' + extension);
      const source = join(directory, 'output.' + extension);
      if ((await stat(source)).size > limit) throw new Error('Output too large');
      await copyFile(source, path);
      await chmod(path, 0o600);
      return { ...input, path, name: 'sticker.' + extension, mime: extension === 'webm' ? 'video/webm' : 'image/webp', stickerFormat: result.format };
    } catch {
      throw new ApiError('Sticker conversion unavailable; use original attachment', 415);
    } finally { await rm(directory, { recursive: true, force: true }); }
  });
  pending = job.catch(() => {});
  return job;
}
