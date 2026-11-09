import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

export const root = resolve(import.meta.dir, '../..');
export const manifest = JSON.parse(readFileSync(resolve(root, 'docs/rewrite/evidence/baseline.json'), 'utf8'));
export const sha256 = (bytes: Uint8Array) => createHash('sha256').update(bytes).digest('hex');

// Recreate only the recorded commit. Never import the evolving candidate as its
// own oracle, and fail closed if an archived source file was edited locally.
export function oracle() {
  const destination = resolve(root, manifest.oraclePath);
  if (!existsSync(destination)) {
    const archive = spawnSync('git', ['archive', '--format=tar', manifest.revision], { cwd: root, maxBuffer: 16 * 1024 * 1024 });
    if (archive.status !== 0 || sha256(archive.stdout) !== manifest.archiveSha256) throw new Error('Frozen oracle archive mismatch');
    mkdirSync(destination, { recursive: true, mode: 0o700 });
    const extracted = spawnSync('tar', ['-xf', '-', '-C', destination], { input: archive.stdout });
    if (extracted.status !== 0) throw new Error('Could not extract the frozen oracle');
  }
  for (const [file, expected] of Object.entries(manifest.files)) {
    if (sha256(readFileSync(resolve(destination, file))) !== expected) throw new Error(`Frozen oracle mismatch: ${file}`);
  }
  return destination;
}

if (import.meta.main) console.log(JSON.stringify({ revision: manifest.revision, oracle: oracle() }));
