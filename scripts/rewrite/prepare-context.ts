import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { relative, resolve } from 'node:path';
import { manifest, oracle, root, sha256 } from './oracle.ts';

// Only public source inputs enter this disposable Docker context. Never send
// the workspace, private configuration, runtime databases or media to a build.
const destination = resolve(root, 'data/rewrite/context');
const marker = resolve(destination, '.rewrite-context');
if (existsSync(destination)) {
  if (!existsSync(marker)) throw new Error('Refusing to replace an unrecognized build context');
  rmSync(destination, { recursive: true });
}
mkdirSync(destination, { recursive: true, mode: 0o755 });
writeFileSync(marker, 'Generated offline rewrite context\n');
const files: string[] = [];
function collect(dir: string) {
  for (const entry of readdirSync(resolve(root, dir), { withFileTypes: true })) {
    const name = `${dir}/${entry.name}`;
    if (entry.isDirectory()) collect(name);
    else if (entry.isFile() && /\.(?:ts|cljs|js|py|json|md|edn|sql)$/.test(name)) files.push(name);
  }
}
for (const dir of ['src', 'scripts', 'test', 'docs/rewrite']) collect(dir);
files.push('package.json', 'bun.lock', '.bun-version', 'nbb.edn', 'bridge.env.example', 'deployment.lock.json');
files.push('docs/rewrite/experiments/Dockerfile');
for (const file of files) {
  mkdirSync(resolve(destination, file, '..'), { recursive: true, mode: 0o755 });
  cpSync(resolve(root, file), resolve(destination, file));
}
const baseline = oracle();
for (const file of Object.keys(manifest.files)) {
  const target = resolve(destination, manifest.oraclePath, file);
  mkdirSync(resolve(target, '..'), { recursive: true, mode: 0o755 });
  cpSync(resolve(baseline, file), target);
}
const inputs = Object.fromEntries(files.sort().map(file => [file, sha256(readFileSync(resolve(root, file)))]));
const candidate = { baseline: manifest.revision, branch: 'clojure/rewrite', files: inputs, fingerprint: sha256(Buffer.from(JSON.stringify(inputs))) };
writeFileSync(resolve(destination, 'candidate-inputs.json'), JSON.stringify(candidate, null, 2) + '\n');
console.log(JSON.stringify({ context: relative(root, destination), fingerprint: candidate.fingerprint, publicInputs: files.length }));
