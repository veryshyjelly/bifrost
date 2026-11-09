// Runs in its own process group so the parent can stop Chromium and FFmpeg together.
import { readFile, writeFile, copyFile, readdir, stat } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import { gunzipSync } from 'node:zlib';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const [input, target, directory] = process.argv.slice(2);
const python = process.env.STICKER_PYTHON || 'python3';
const helper = fileURLToPath(new URL('../scripts/sticker-webp.py', import.meta.url));
const ffmpeg = ['-nostdin', '-hide_banner', '-loglevel', 'error', '-y'];
type Frames = { width: number; height: number; count: number; duration: number; durations: number[]; normalizeAlpha?: boolean };

async function run(command: string, args: string[]) {
  return new Promise<string>((accept, reject) => {
    const child = spawn(command, args, { cwd: directory, stdio: ['ignore', 'pipe', 'ignore'] });
    let output = '';
    child.stdout.on('data', data => {
      output += data;
      if (output.length > 1024 * 1024) { child.kill('SIGKILL'); reject(new Error('Excessive process output')); }
    });
    child.on('error', reject);
    child.on('close', code => code === 0 ? accept(output) : reject(new Error('Conversion process failed')));
  });
}
const inspect = async (path: string, decode = false): Promise<Frames> => JSON.parse(await run(python, [helper, 'inspect', path, ...(decode ? [directory] : [])]));
const probe = async (path: string) => JSON.parse(await run('ffprobe', ['-v', 'error', '-show_format', '-show_streams', '-of', 'json', path]));
const fits = async (path: string, limit: number) => { const size = (await stat(path)).size; return size > 0 && size <= limit; };

async function renderTgs(): Promise<Frames> {
  const data = JSON.parse(gunzipSync(await readFile(input), { maxOutputLength: 4 * 1024 * 1024 }).toString());
  const duration = (data.op - data.ip) / data.fr;
  if (![data.w, data.h, data.ip, data.op, data.fr, duration].every(Number.isFinite)
    || data.w <= 0 || data.h <= 0 || Math.max(data.w, data.h) > 2048 || data.fr <= 0 || duration <= 0 || duration > 60) throw new Error('Invalid Lottie');
  // TGS does not need expressions or external assets. Never evaluate supplied expressions.
  const check = (value: any): void => {
    if (!value || typeof value !== 'object') return;
    if (typeof value.x === 'string') throw new Error('Lottie expressions unsupported');
    if (value.p && typeof value.p === 'string' && !value.p.startsWith('data:')) throw new Error('External Lottie asset');
    for (const nested of Object.values(value)) check(nested);
  };
  check(data);
  const { default: puppeteer } = await import('puppeteer-core');
  const browser = await puppeteer.launch({ executablePath: process.env.STICKER_CHROMIUM || '/usr/bin/chromium',
    headless: true, args: ['--disable-dev-shm-usage', '--disable-background-networking', ...(process.env.STICKER_CHROMIUM_NO_SANDBOX === '1' ? ['--no-sandbox'] : [])] });
  const outputDuration = Math.min(duration, 10), count = Math.max(2, Math.ceil(outputDuration * 30));
  try {
    const page = await browser.newPage();
    await page.setViewport({ width: 512, height: 512, deviceScaleFactor: 1 });
    await page.setRequestInterception(true);
    page.on('request', request => void (request.url().startsWith('data:') ? request.continue() : request.abort()));
    await page.setContent('<html><body style="margin:0;background:transparent"><div id="sticker" style="width:512px;height:512px"></div></body></html>');
    await page.addScriptTag({ path: fileURLToPath(import.meta.resolve('lottie-web/build/player/lottie_light.min.js')) });
    await page.evaluate(animationData => {
      const w = window as any;
      w.animation = w.lottie.loadAnimation({ container: document.getElementById('sticker'), renderer: 'svg', loop: false, autoplay: false, animationData });
      w.animation.setSubframe(true);
    }, data);
    await page.waitForFunction('window.animation.isLoaded');
    for (let i = 0; i < count; i++) {
      await page.evaluate(frame => (window as any).animation.goToAndStop(frame, true), i * (data.op - data.ip) / count);
      await page.screenshot({ path: join(directory, `frame${String(i).padStart(4, '0')}.png`), omitBackground: true });
    }
  } finally { await browser.close(); }
  return { width: 512, height: 512, count, duration: outputDuration, durations: Array.from({ length: count }, (_, i) => Math.round((i + 1) * outputDuration * 1000 / count) - Math.round(i * outputDuration * 1000 / count)) };
}

async function decodeVideo(): Promise<Frames> {
  const info = await probe(input), stream = info.streams.find((s: any) => s.codec_type === 'video');
  const duration = Number(info.format.duration);
  if (!stream || !['vp8', 'vp9'].includes(stream.codec_name) || !Number.isFinite(duration) || duration <= 0 || duration > 60
    || stream.width > 2048 || stream.height > 2048) throw new Error('Invalid video sticker');
  const outputDuration = Math.min(duration, 10);
  await run('ffmpeg', [...ffmpeg, '-c:v', stream.codec_name === 'vp9' ? 'libvpx-vp9' : 'libvpx', '-i', input,
    '-an', '-vf', `setpts=${outputDuration / duration}*(PTS-STARTPTS),fps=30,scale=512:512:force_original_aspect_ratio=decrease,format=rgba,pad=512:512:(ow-iw)/2:(oh-ih)/2:color=0x00000000`,
    '-t', String(outputDuration), '-frames:v', '300', '-start_number', '0', join(directory, 'frame%04d.png')]);
  const count = (await readdir(directory)).filter(name => /^frame\d+\.png$/.test(name)).length;
  if (!count) throw new Error('Empty video');
  return { width: 512, height: 512, count, duration: count / 30, durations: Array(count).fill(1000 / 30) };
}

async function webp(frames: Frames) {
  const output = join(directory, 'output.webp'), animated = frames.count > 1;
  const limit = animated ? 500000 : 100000;
  // Prefer the usual quality/frame rates, then give dense animations smaller encodings.
  for (const [quality, fps] of [[80, 30], [60, 30], [40, 30], [40, 20], [40, 15], [30, 15], [30, 12], [30, 10]]) {
    await run(python, [helper, 'encode', directory, output, String(quality), String(fps)]);
    if (!(await fits(output, limit))) continue;
    const result = await inspect(output);
    if (result.width !== 512 || result.height !== 512 || result.duration > 10.001 || (animated && result.count < 2)
      || (animated && result.durations.some(ms => ms < 8))) throw new Error('Invalid encoded WebP');
    return animated ? 'animated' : 'static';
  }
  throw new Error('WebP exceeds sticker limit');
}

async function telegram(frames: Frames) {
  if (frames.count === 1) {
    // WhatsApp WebP is normally already 512 square. Normalize anomalous static input too.
    await webp(frames);
    return 'static';
  }
  const output = join(directory, 'output.webm'), duration = Math.min(frames.duration, 3);
  const lines = frames.durations.flatMap((ms, i) => [`file 'frame${String(i).padStart(4, '0')}.png'`, `duration ${ms / 1000}`]);
  lines.push(`file 'frame${String(frames.count - 1).padStart(4, '0')}.png'`);
  await writeFile(join(directory, 'frames.txt'), lines.join('\n') + '\n');
  for (const [crf, fps] of [[32, 30], [42, 30], [52, 30], [56, 20], [60, 15]]) {
    await run('ffmpeg', [...ffmpeg, '-f', 'concat', '-safe', '0', '-i', join(directory, 'frames.txt'), '-an',
      '-vf', `setpts=${duration / frames.duration}*(PTS-STARTPTS),fps=${fps},scale=512:512:force_original_aspect_ratio=decrease,format=rgba,pad=512:512:(ow-iw)/2:(oh-ih)/2:color=0x00000000`,
      '-t', String(duration), '-c:v', 'libvpx-vp9', '-pix_fmt', 'yuva420p', '-b:v', '0', '-crf', String(crf), '-auto-alt-ref', '0', '-threads', '2', output]);
    if (!(await fits(output, 256000))) continue;
    const info = await probe(output), video = info.streams.find((s: any) => s.codec_type === 'video');
    const [n, d] = (video?.avg_frame_rate || '0/1').split('/').map(Number);
    if (!video || video.codec_name !== 'vp9' || video.width !== 512 || video.height !== 512 || n / d > 30
      || Number(info.format.duration) > 3.001 || info.streams.some((s: any) => s.codec_type === 'audio')) throw new Error('Invalid encoded video');
    return 'video';
  }
  throw new Error('WebM exceeds sticker limit');
}

async function main() {
  const bytes = await readFile(input);
  let frames: Frames;
  if (bytes.subarray(0, 4).toString() === 'RIFF' && bytes.subarray(8, 12).toString() === 'WEBP') {
    frames = await inspect(input);
    const limit = target === 'whatsapp' ? (frames.count > 1 ? 500000 : 100000) : 100000;
    if (frames.width === 512 && frames.height === 512 && (target === 'whatsapp' || frames.count === 1)
      && !frames.normalizeAlpha && frames.duration <= 10 && (frames.count === 1 || frames.durations.every(ms => ms >= 8)) && bytes.length <= limit) {
      await copyFile(input, join(directory, 'output.webp'));
      await writeFile(join(directory, 'result.json'), JSON.stringify({ format: frames.count > 1 ? 'animated' : 'static' }));
      return;
    }
    frames = await inspect(input, true);
  } else if (bytes[0] === 0x1f && bytes[1] === 0x8b) frames = await renderTgs();
  else if (bytes.subarray(0, 4).equals(Buffer.from([0x1a, 0x45, 0xdf, 0xa3]))) {
    // History may fetch a previously converted Telegram sticker. Keep it intact.
    if (target === 'telegram') {
      const info = await probe(input), stream = info.streams.find((s: any) => s.codec_type === 'video');
      const [n, d] = (stream?.avg_frame_rate || '0/1').split('/').map(Number);
      if (stream?.codec_name === 'vp9' && Math.max(stream.width, stream.height) === 512 && Math.min(stream.width, stream.height) > 0
        && Number(info.format.duration) > 0 && Number(info.format.duration) <= 3 && n / d > 0 && n / d <= 30
        && info.streams.length === 1 && bytes.length <= 256000) {
        await copyFile(input, join(directory, 'output.webm'));
        await writeFile(join(directory, 'result.json'), JSON.stringify({ format: 'video' }));
        return;
      }
    }
    frames = await decodeVideo();
  }
  else throw new Error('Unsupported sticker');
  await writeFile(join(directory, 'frames.json'), JSON.stringify(frames));
  const format = target === 'telegram' ? await telegram(frames) : await webp(frames);
  await writeFile(join(directory, 'result.json'), JSON.stringify({ format }));
}
main().catch(() => { process.exitCode = 1; });
