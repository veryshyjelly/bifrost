import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { gzipSync } from 'node:zlib';
import { execFileSync } from 'node:child_process';
import { prepareSticker } from '../src/stickers.ts';
import { ApiError } from '../src/api.ts';

// Codec tests are opt-in locally; CI and the container validation enable them.
const mediaTest = process.env.STICKER_TESTS === '1' ? test : test.skip;
const python = process.env.STICKER_PYTHON || 'python3';
const execute = (command: string, args: string[]) => execFileSync(command, args, { encoding: 'utf8', maxBuffer: 1024 * 1024 });
const probe = (path: string) => JSON.parse(execute('ffprobe', ['-v', 'error', '-show_streams', '-show_format', '-of', 'json', path]));
async function fixture(t: any) {
  const root = await mkdtemp(join(tmpdir(), 'bridge-stickers-'));
  t.after(() => rm(root, { recursive: true, force: true }));
  const convert = (path: string, target: 'whatsapp' | 'telegram') => prepareSticker({ path, name: 'original' }, target, root, 1024 * 1024);
  const webp = (name: string, durations: number[], size = 512) => {
    const path = join(root, name + '.webp');
    execute(python, ['-c', `
import json,sys
from PIL import Image,ImageDraw
durations=json.loads(sys.argv[2]); size=int(sys.argv[3]); frames=[]
for i in range(len(durations)):
    im=Image.new('RGBA',(size,size)); draw=ImageDraw.Draw(im)
    x=round(i*300/max(1,len(durations)-1))
    draw.rectangle((x+50,100,x+100,150), fill=(255,0,0,255) if i<len(durations)/2 else (0,0,255,255))
    frames.append(im)
frames[0].save(sys.argv[1],save_all=len(frames)>1,append_images=frames[1:],duration=durations,loop=0,lossless=True)
`, path, JSON.stringify(durations), String(size)]);
    return path;
  };
  const pixels = (path: string, x: number, y: number) => JSON.parse(execute(python, ['-c', 'from PIL import Image; import json,sys; print(json.dumps(Image.open(sys.argv[1]).convert("RGBA").getpixel((int(sys.argv[2]),int(sys.argv[3])))))', path, String(x), String(y)]));
  return { root, convert, webp, pixels };
}

function decode(path: string, output: string, last = false) {
  execute('ffmpeg', ['-v', 'error', '-y', '-c:v', 'libvpx-vp9', '-i', path,
    ...(last ? ['-vf', 'reverse'] : []), '-frames:v', '1', output]);
}

mediaTest('static WebP passes through byte-for-byte; non-square stickers get transparent padding', async t => {
  const f = await fixture(t), input = f.webp('static', [100]);
  const result = await f.convert(input, 'telegram');
  assert.equal(result.stickerFormat, 'static'); assert.equal(result.mime, 'image/webp');
  assert.deepEqual(await readFile(result.path), await readFile(input));
  const small = f.webp('small', [100], 400), normalized = await f.convert(small, 'whatsapp');
  assert.equal(probe(normalized.path).streams[0].width, 512);
  assert.equal(f.pixels(normalized.path, 0, 0)[3], 0);
});

mediaTest('animated WebP becomes a moving transparent VP9 sticker with variable frame timing', async t => {
  const f = await fixture(t), input = f.webp('variable', [50, 200, 400]);
  const result = await f.convert(input, 'telegram'), info = probe(result.path);
  assert.equal(result.name, 'sticker.webm'); assert.equal(result.mime, 'video/webm');
  assert.equal(info.streams.length, 1); assert.equal(info.streams[0].codec_name, 'vp9');
  assert.ok(Math.abs(Number(info.format.duration) - 0.65) < 0.05);
  assert.ok((await readFile(result.path)).length <= 256000);
  const first = join(f.root, 'first.png'), last = join(f.root, 'last.png');
  decode(result.path, first); decode(result.path, last, true);
  assert.equal(f.pixels(first, 0, 0)[3], 0); assert.equal(f.pixels(last, 0, 0)[3], 0);
  assert.ok(f.pixels(first, 75, 125)[0] > 200);
  assert.ok(f.pixels(last, 375, 125)[2] > 200);
  assert.equal(f.pixels(last, 75, 125)[3], 0); // previous subrectangle was disposed
});

mediaTest('long animations preserve their ending while speeding up to three seconds', async t => {
  const f = await fixture(t), result = await f.convert(f.webp('long', Array(60).fill(100)), 'telegram');
  assert.ok(Math.abs(Number(probe(result.path).format.duration) - 3) < 0.04);
  const last = join(f.root, 'end.png'); decode(result.path, last, true);
  assert.ok(f.pixels(last, 370, 125)[2] > 200);
});

mediaTest('Telegram VP9 alpha survives conversion to WhatsApp animated WebP', async t => {
  const f = await fixture(t), video = await f.convert(f.webp('source', [200, 200, 200]), 'telegram');
  const result = await f.convert(video.path, 'whatsapp');
  const archived = await f.convert(video.path, 'telegram');
  assert.deepEqual(await readFile(archived.path), await readFile(video.path));
  assert.equal(result.stickerFormat, 'animated'); assert.equal(result.mime, 'image/webp');
  assert.ok((await readFile(result.path)).length <= 500000);
  const meta = JSON.parse(execute(python, ['-c', `
from PIL import Image
import json,sys
im=Image.open(sys.argv[1]); first=im.convert('RGBA').getpixel((75,125)); im.seek(im.n_frames-1)
last=im.convert('RGBA'); print(json.dumps([im.size,im.n_frames,first,last.getpixel((375,125)),last.getpixel((0,0))]))
`, result.path]));
  assert.deepEqual(meta[0], [512, 512]); assert.ok(meta[1] > 1);
  assert.ok(meta[2][0] > 180); assert.ok(meta[3][2] > 180); assert.equal(meta[4][3], 0);
});

mediaTest('timeout kills encoder descendants and removes the private job directory', async t => {
  const f = await fixture(t), script = join(f.root, 'slow-python'), pidfile = join(f.root, 'pid');
  await writeFile(script, '#!/bin/sh\nsleep 60 &\nprintf "%s" "$!" > "$STICKER_TEST_PID"\nwait\n', { mode: 0o700 });
  const previous = process.env.STICKER_PYTHON;
  process.env.STICKER_PYTHON = script; process.env.STICKER_TEST_PID = pidfile;
  try {
    const input = join(f.root, 'fake.webp'); await writeFile(input, 'RIFF0000WEBP');
    await assert.rejects(prepareSticker({ path: input, name: 'fake' }, 'telegram', f.root, 1024, 2000), /Sticker conversion/);
    const pid = Number(await readFile(pidfile, 'utf8'));
    for (let i = 0; i < 20; i++) {
      try { process.kill(pid, 0); } catch { break; }
      await new Promise(resolve => setTimeout(resolve, 50));
    }
    assert.throws(() => process.kill(pid, 0));
    assert.ok(!(await readdir(f.root)).some(name => name.startsWith('.sticker-')));
  } finally {
    if (previous === undefined) delete process.env.STICKER_PYTHON; else process.env.STICKER_PYTHON = previous;
    delete process.env.STICKER_TEST_PID;
  }
});

mediaTest('TGS uses its frame rate and nonzero in-point, preserving motion and transparency', async t => {
  const f = await fixture(t), path = join(f.root, 'sticker.tgs');
  const data = { v: '5.7.4', fr: 20, ip: 10, op: 70, w: 512, h: 512, nm: 'fixture', assets: [], layers: [{
    ty: 4, ind: 1, ip: 10, op: 70, st: 0, ks: { o: { a: 0, k: 100 }, r: { a: 0, k: 0 },
      p: { a: 1, k: [{ t: 10, s: [75,125,0], e: [375,125,0], o: { x: 0.33, y: 0.33 }, i: { x: 0.67, y: 0.67 } }, { t: 70, s: [375,125,0] }] },
      a: { a: 0, k: [0,0,0] }, s: { a: 0, k: [100,100,100] } },
    shapes: [{ ty: 'rc', p: { a: 0, k: [0,0] }, s: { a: 0, k: [50,50] }, r: { a: 0, k: 0 } }, { ty: 'fl', c: { a: 0, k: [1,0,0,1] }, o: { a: 0, k: 100 }, r: 1 }],
  }] };
  await writeFile(path, gzipSync(JSON.stringify(data)));
  const result = await f.convert(path, 'whatsapp');
  const meta = JSON.parse(execute(python, ['-c', `
from PIL import Image
import json,sys
im=Image.open(sys.argv[1]); first=im.convert('RGBA').getpixel((75,125)); duration=0
for i in range(im.n_frames):
    im.seek(i); im.load(); duration+=im.info['duration']
last=im.convert('RGBA'); print(json.dumps([duration,first,last.getpixel((370,125)),last.getpixel((0,0))]))
`, result.path]));
  assert.ok(Math.abs(meta[0] - 3000) < 40); assert.ok(meta[1][0] > 200);
  assert.ok(meta[2][0] > 200); assert.equal(meta[3][3], 0);
});

mediaTest('dense WhatsApp animations fit after extra compression while retaining motion, duration and alpha', async t => {
  const f = await fixture(t), input = join(f.root, 'dense.webp');
  execute(python, ['-c', `
from PIL import Image
import random,sys
r=random.Random(7); frames=[]
for i in range(45):
    frame=Image.new('RGBA',(512,512))
    patch=Image.frombytes('RGB',(128,128),r.randbytes(128*128*3)).resize((192,192),Image.Resampling.NEAREST)
    frame.paste(patch,(160,160)); frames.append(frame)
delays=[round((i+1)*3000/45)-round(i*3000/45) for i in range(45)]
frames[0].save(sys.argv[1],save_all=True,append_images=frames[1:],duration=delays,loop=0,lossless=True)
`, input]);
  assert.ok((await readFile(input)).length > 500000);
  const output = await prepareSticker({ path: input, name: 'dense.webp' }, 'whatsapp', f.root, 16 * 1024 * 1024);
  assert.equal(output.stickerFormat, 'animated'); assert.equal(output.mime, 'image/webp');
  assert.ok((await readFile(output.path)).length <= 500000);
  const meta = JSON.parse(execute(python, ['-c', `
from PIL import Image
import json,sys
im=Image.open(sys.argv[1]); first=im.convert('RGBA'); duration=0
for i in range(im.n_frames):
    im.seek(i); im.load(); duration+=im.info['duration']
last=im.convert('RGBA')
print(json.dumps([im.size,im.n_frames,duration,first.getpixel((0,0))[3],last.getpixel((0,0))[3],first.tobytes()!=last.tobytes()]))
`, output.path]));
  assert.deepEqual(meta[0], [512, 512]); assert.ok(meta[1] > 1);
  assert.ok(Math.abs(meta[2] - 3000) < 40);
  assert.equal(meta[3], 0); assert.equal(meta[4], 0); assert.equal(meta[5], true);
});

mediaTest('malformed and excessive inputs fail safely; concurrent jobs leave no intermediates', async t => {
  const f = await fixture(t), invalid = join(f.root, 'bad.tgs'); await writeFile(invalid, gzipSync('invalid'));
  await assert.rejects(f.convert(invalid, 'whatsapp'), (e: any) => e instanceof ApiError && e.status === 415);
  await assert.rejects(prepareSticker({ path: invalid, name: 'bad' }, 'telegram', f.root, 1), /Sticker conversion/);
  const input = f.webp('animated', [100, 100]);
  const outputs = await Promise.all([f.convert(input, 'telegram'), f.convert(input, 'whatsapp')]);
  assert.notEqual(outputs[0].path, outputs[1].path);
  assert.ok(!(await readdir(f.root)).some(name => name.startsWith('.sticker-')));
});

mediaTest('high-detail animations either meet the video size ceiling or retain the original fallback', async t => {
  const f = await fixture(t), input = join(f.root, 'noise.webp');
  execute(python, ['-c', `
from PIL import Image
import random,sys
r=random.Random(123); frames=[Image.frombytes('RGB',(128,128),r.randbytes(128*128*3)).resize((512,512),Image.Resampling.NEAREST) for _ in range(12)]
frames[0].save(sys.argv[1],save_all=True,append_images=frames[1:],duration=100,lossless=True)
`, input]);
  try {
    const output = await prepareSticker({ path: input, name: 'noise.webp' }, 'telegram', f.root, 16 * 1024 * 1024);
    assert.ok((await readFile(output.path)).length <= 256000);
  } catch (e) { assert.ok(e instanceof ApiError && e.status === 415); }
  assert.ok((await readFile(input)).length > 256000);
  assert.ok(!(await readdir(f.root)).some(name => name.startsWith('.sticker-')));
});
