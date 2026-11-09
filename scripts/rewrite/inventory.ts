import { readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { manifest, oracle, root, sha256 } from './oracle.ts';

const baseline = oracle();
const tests = [];
for (const file of readdirSync(resolve(baseline, 'test')).filter(file => file.endsWith('.test.ts')).sort()) {
  const path = `test/${file}`, source = readFileSync(resolve(baseline, path), 'utf8');
  for (const match of source.matchAll(/\b(test|mediaTest|dockerTest)\(\s*'((?:\\.|[^'\\])*)'/g)) {
    const line = source.slice(0, match.index).split('\n').length;
    tests.push({ id: `baseline:${path}:${line}`, reference: `${path}:${line}`, name: match[2],
      sourceSha256: manifest.files[path], baselineEvidence: match[1] === 'test' ? 'implemented and locally verified' : 'opt-in check; skipped in baseline default run',
      candidateStatus: 'pending', plannedCandidateFixtures: `test/rewrite/${file.replace('.test.ts', '')} workflow replay; docs/rewrite/coverage-matrix.md` });
  }
}
const investigation = Object.fromEntries(readdirSync(resolve(root, 'docs/rewrite')).filter(file => /^0[1-8]-.*\.md$/.test(file))
  .sort().map(file => [`docs/rewrite/${file}`, sha256(readFileSync(resolve(root, 'docs/rewrite', file)))]));
writeFileSync(resolve(root, 'docs/rewrite/evidence/inventory.json'), JSON.stringify({
  baseline: manifest.revision, investigation, notes: 'Per-test inventory is a coverage floor. Workflow and failure-path mapping is in coverage-matrix.md. Pending means the entire original case has not yet been replayed through the candidate.', tests,
}, null, 2) + '\n');
console.log(JSON.stringify({ baseline: manifest.revision, cases: tests.length, investigationDocuments: Object.keys(investigation).length }));
