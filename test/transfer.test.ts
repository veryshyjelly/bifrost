import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, readFileSync, writeFileSync, rmSync, mkdirSync, statSync, existsSync } from 'node:fs';
import { join } from 'node:path';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { environment, restoredEnvironment, exportVolumes, exportState, importState } from '../scripts/transfer.ts';

test('import keeps credentials literal, normalizes only local paths and removes the retired n8n setting', () => {
  const text = 'BRIDGE_ENCRYPTION_KEY=original\nTELEGRAM_API_HASH=literal$hash\nOPENWA_SESSION_ID=same-session\nBRIDGE_CALLBACK_URL=http://old\nN8N_MANAGEMENT_WEBHOOK=http://old-n8n\n';
  const result = environment(restoredEnvironment(text));
  assert.equal(result.BRIDGE_ENCRYPTION_KEY, 'original'); assert.equal(result.TELEGRAM_API_HASH, 'literal$hash');
  assert.equal(result.OPENWA_SESSION_ID, 'same-session'); assert.equal(result.BRIDGE_CALLBACK_URL, 'http://bifrost:3210');
  assert.equal(result.BIFROST_ACCOUNT_ID, 'same-session');
  assert.equal(result.BRIDGE_DB, '/data/bridge.db'); assert.equal(result.N8N_MANAGEMENT_WEBHOOK, undefined);
});

test('export discovers legacy and Bifrost named volumes and refuses ambiguous or bind-mounted state', () => {
  const container = (service: string, mounts: any[]) => ({ Config: { Labels: { 'com.docker.compose.service': service } }, State: {}, Mounts: mounts });
  const volume = (Destination: string, Name: string) => ({ Destination, Name, Type: 'volume' });
  const bridge = container('wa-telegram-bridge', [volume('/data', 'old-data'), volume('/media', 'old-media')]);
  const telegram = container('telegram-api', [volume('/telegram-data', 'old-telegram')]);
  assert.deepEqual(exportVolumes([bridge, telegram]).volumes, { data: 'old-data', media: 'old-media', 'telegram-data': 'old-telegram' });
  bridge.Config.Labels['com.docker.compose.service'] = 'bifrost';
  assert.equal(exportVolumes([bridge, telegram]).selected.length, 2);
  assert.throws(() => exportVolumes([bridge, bridge, telegram]), /exactly one/);
  bridge.Mounts[0].Type = 'bind'; assert.throws(() => exportVolumes([bridge, telegram]), /named/);
});

const dockerTest = process.env.BIFROST_DOCKER_TESTS === '1' ? test : test.skip;
const docker = (...args: string[]) => execFileSync('docker', args, { encoding: 'utf8', maxBuffer: 1024 * 1024 }).trim();
dockerTest('archive validation rejects traversal, duplicate paths and link entries before extraction', () => {
  const image = process.env.BIFROST_TEST_IMAGE || 'local/bifrost:0.1.0';
  docker('run', '--rm', '--network', 'none', '--entrypoint', '/opt/sticker-python/bin/python', image, '-c', `
import io,runpy,tarfile
safe=runpy.run_path('/app/scripts/transfer-volume.py',run_name='test')['safe_members']
def archive(entries):
    buffer=io.BytesIO()
    with tarfile.open(fileobj=buffer,mode='w') as out:
        for name,kind in entries:
            item=tarfile.TarInfo(name); item.type=kind
            if kind==tarfile.SYMTYPE: item.linkname='/escape'
            out.addfile(item)
    buffer.seek(0)
    return tarfile.open(fileobj=buffer)
safe(archive([('folder/file',tarfile.REGTYPE)]))
for entries in [[('../escape',tarfile.REGTYPE)],[('/absolute',tarfile.REGTYPE)],[('link',tarfile.SYMTYPE)],[('file',tarfile.REGTYPE),('file',tarfile.REGTYPE)],[('link',tarfile.LNKTYPE)]]:
    try: safe(archive(entries))
    except ValueError: pass
    else: raise AssertionError('unsafe archive accepted')
`);
});
dockerTest('export/import round trip preserves identity, state, media and ownership without starting the restored bots', () => {
  const backupRoot = join(import.meta.dirname, '../backups'); mkdirSync(backupRoot, { recursive: true, mode: 0o700 });
  const root = mkdtempSync(join(backupRoot, 'bifrost-roundtrip-')), source = join(root, 'source'), destination = join(root, 'destination');
  mkdirSync(source); mkdirSync(destination);
  const project = 'bifrost-test-' + process.pid + '-' + Date.now(), restored = project + '-restored', legacy = project + '-legacy';
  const key = 'cd'.repeat(32), image = process.env.BIFROST_TEST_IMAGE || 'local/bifrost:0.1.0';
  const roles = ['data', 'media', 'telegram-data'];
  const volume = (p: string, role: string) => p + '_' + (role === 'telegram-data' ? role : 'bridge-' + role);
  const containers: string[] = [];
  try {
    writeFileSync(join(source, 'bridge.env'), `BRIDGE_ENCRYPTION_KEY=${key}\nBRIDGE_ADMIN_TOKEN=fixture-admin\nOPENWA_WEBHOOK_SECRET=fixture-hook\nOPENWA_URL=http://fixture-openwa\nOPENWA_SESSION_ID=original-session\nTELEGRAM_MASTER_TOKEN=fixture-master\nTELEGRAM_OWNER_ID=123\nTELEGRAM_MANAGEMENT_CHAT_ID=-1001\nTELEGRAM_API_ID=12345\nTELEGRAM_API_HASH=fixture-hash\nBRIDGE_CALLBACK_URL=http://wa-telegram-bridge:3210\n`);
    writeFileSync(join(source, 'telegram-api.env'), 'TELEGRAM_API_ID=12345\nTELEGRAM_API_HASH=fixture-hash\n');
    for (const role of roles) docker('volume', 'create', volume(project, role));
    const mounts = roles.flatMap(role => ['--mount', `type=volume,source=${volume(project, role)},target=/${role}`]);
    docker('run', '--rm', '--network', 'none', '--user', '0:0', ...mounts, image, 'bun', '-e', `
      import {Store} from './src/store.ts'; import {writeFileSync,chownSync} from 'node:fs';
      const s=new Store('/data/bridge.db','${key}'); s.put('bot','42',{token:s.seal('original-child-token'),ready:true});
      s.put('contact','111@c.us',{bot:'42',name:'Fixture'}); s.map('111@c.us','wa-id','123',77,'42','in',true);
      const {digest}=await import('./src/config.ts');s.put('status-mute','legacy',{session:digest('original-session'),sender:'111@c.us',active:true,revision:1});
      s.enqueue('n8n:old','n8n:old','n8n',{id:'old',type:'mark_read',data:{chatId:'111@c.us'},at:1});s.close();
      writeFileSync('/media/pending.bin','original-media');writeFileSync('/telegram-data/cached.bin','telegram-cache');
    `);
    for (const service of ['wa-telegram-bridge', 'telegram-api']) {
      const id = docker('create', '--network', 'none', '--label', 'com.docker.compose.project=' + project,
        '--label', 'com.docker.compose.service=' + service, ...(service === 'telegram-api' ? mounts.slice(4) : mounts), image,
        'bun', '-e', 'setInterval(()=>{},1000)');
      containers.push(id); docker('start', id);
    }
    const file = join(root, 'state.bifrost.tar.gz'); exportState(file, project, image, source);
    assert.equal(statSync(file).mode & 0o777, 0o600);
    for (const id of containers) assert.equal(docker('inspect', '--format', '{{.State.Running}}', id), 'true');
    importState(file, restored, image, destination);
    const values = environment(readFileSync(join(destination, 'bridge.env'), 'utf8'));
    assert.equal(values.BRIDGE_ENCRYPTION_KEY, key); assert.equal(values.TELEGRAM_MASTER_TOKEN, 'fixture-master');
    assert.equal(values.BRIDGE_CALLBACK_URL, 'http://bifrost:3210');
    assert.equal(values.BIFROST_ACCOUNT_ID, 'original-session');
    // Bun loads this checkout's .env; let the imported project's .env take precedence here.
    const composeEnv = { ...process.env };
    delete composeEnv.COMPOSE_PROJECT_NAME; delete composeEnv.BIFROST_EXTERNAL_VOLUMES;
    const compose = JSON.parse(execFileSync('docker', ['compose', '--project-directory', destination,
      '--env-file', join(destination, '.env'), '-f', join(import.meta.dirname, '../compose.yaml'), 'config', '--format', 'json'],
    { encoding: 'utf8', env: composeEnv }));
    for (const role of roles) {
      const key = role === 'telegram-data' ? role : 'bridge-' + role;
      assert.equal(compose.volumes[key].external, true);
      assert.equal(compose.volumes[key].name, volume(restored, role));
    }
    for (const name of ['bridge.env', 'telegram-api.env', '.env']) assert.equal(statSync(join(destination, name)).mode & 0o777, 0o600);
    const restoredMounts = roles.flatMap(role => ['--mount', `type=volume,source=${volume(restored, role)},target=/${role}`]);
    const result = JSON.parse(docker('run', '--rm', '--network', 'none', ...restoredMounts, image, 'bun', '-e', `
      import {Store} from './src/store.ts'; import {readFileSync,writeFileSync} from 'node:fs';
      import {Bridge} from './src/bridge.ts'; import {configuration} from './src/config.ts';
      const s=new Store('/data/bridge.db','${key}');writeFileSync('/media/writable','yes');
      const b=new Bridge({...configuration({}),key:'${key}',account:'original-session',session:'replacement-session',openwaKey:'replacement-key'},s);
      console.log(JSON.stringify({token:s.unseal(s.get('bot','42').token),bot:s.get('contact','111@c.us').bot,
       messages:s.byWhatsApp('111@c.us','wa-id').length,jobs:s.db.prepare('SELECT type FROM jobs').all(),
       media:readFileSync('/media/pending.bin','utf8'),telegram:readFileSync('/telegram-data/cached.bin','utf8'),account:b.c.account,muted:b.statuses.isMuted('111@c.us')}));s.close();
    `));
    assert.deepEqual(result, { token: 'original-child-token', bot: '42', messages: 1, jobs: [{ type: 'n8n' }], media: 'original-media', telegram: 'telegram-cache', account: 'original-session', muted: true });
    assert.equal(docker('ps', '-aq', '--filter', 'label=com.docker.compose.project=' + restored), '');
    assert.throws(() => importState(file, restored, image, destination), /fresh checkout/);
    // A future export must use the database identity even when connection settings changed.
    writeFileSync(join(destination, 'bridge.env'), readFileSync(join(destination, 'bridge.env'), 'utf8').replace(/^BIFROST_ACCOUNT_ID=.*\n/m, '').replace('OPENWA_SESSION_ID=original-session', 'OPENWA_SESSION_ID=replacement-session'));
    for (const service of ['bifrost', 'telegram-api']) containers.push(docker('create', '--network', 'none',
      '--label', 'com.docker.compose.project=' + restored, '--label', 'com.docker.compose.service=' + service,
      ...(service === 'telegram-api' ? restoredMounts.slice(4) : restoredMounts), image, 'bun', '-e', 'setInterval(()=>{},1000)'));
    const nextExport = join(root, 'rotated.bifrost.tar.gz'); exportState(nextExport, restored, image, destination);
    const nextEnv = environment(execFileSync('tar', ['-xOf', nextExport, './bridge.env'], { encoding: 'utf8' }));
    assert.equal(nextEnv.BIFROST_ACCOUNT_ID, 'original-session'); assert.equal(nextEnv.OPENWA_SESSION_ID, 'replacement-session');
    for (const id of containers.slice(2)) assert.equal(docker('inspect', '--format', '{{.State.Running}}', id), 'false');
    const corrupt = join(root, 'bad.bifrost.tar.gz'), empty = join(root, 'empty'); mkdirSync(empty); writeFileSync(corrupt, 'corrupt');
    assert.throws(() => importState(corrupt, project + '-bad', image, empty));
    assert.equal(existsSync(join(empty, 'bridge.env')), false);
    assert.equal(docker('volume', 'ls', '--filter', 'name=' + project + '-bad', '--format', '{{.Name}}'), '');
    const tampered = join(root, 'tampered'); mkdirSync(tampered);
    execFileSync('tar', ['-xzf', file, '-C', tampered]);
    // Old version-1 exports have no account setting or database identity marker.
    writeFileSync(join(tampered, 'bridge.env'), readFileSync(join(tampered, 'bridge.env'), 'utf8').replace(/^BIFROST_ACCOUNT_ID=.*\n/m, ''));
    const oldManifest = JSON.parse(readFileSync(join(tampered, 'manifest.json'), 'utf8'));
    oldManifest.files['bridge.env'] = createHash('sha256').update(readFileSync(join(tampered, 'bridge.env'))).digest('hex');
    writeFileSync(join(tampered, 'manifest.json'), JSON.stringify(oldManifest));
    const legacyFile = join(root, 'legacy.bifrost.tar.gz'), legacyDirectory = join(root, 'legacy'); mkdirSync(legacyDirectory);
    execFileSync('tar', ['-czf', legacyFile, '-C', tampered, '.'], { env: { ...process.env, COPYFILE_DISABLE: '1' } });
    importState(legacyFile, legacy, image, legacyDirectory);
    assert.equal(environment(readFileSync(join(legacyDirectory, 'bridge.env'), 'utf8')).BIFROST_ACCOUNT_ID, 'original-session');
    writeFileSync(join(tampered, 'bridge.env'), readFileSync(join(tampered, 'bridge.env'), 'utf8').replace(key, 'ef'.repeat(32)));
    const broken = join(root, 'tampered.bifrost.tar.gz');
    execFileSync('tar', ['-czf', broken, '-C', tampered, '.'], { env: { ...process.env, COPYFILE_DISABLE: '1' } });
    assert.throws(() => importState(broken, project + '-bad', image, empty));
    const manifest = JSON.parse(readFileSync(join(tampered, 'manifest.json'), 'utf8'));
    manifest.files['bridge.env'] = createHash('sha256').update(readFileSync(join(tampered, 'bridge.env'))).digest('hex');
    writeFileSync(join(tampered, 'manifest.json'), JSON.stringify(manifest));
    execFileSync('tar', ['-czf', broken, '-C', tampered, '.'], { env: { ...process.env, COPYFILE_DISABLE: '1' } });
    assert.throws(() => importState(broken, project + '-bad', image, empty)); // correct checksums, wrong decryption key
    assert.equal(existsSync(join(empty, 'bridge.env')), false);
    assert.equal(docker('volume', 'ls', '--filter', 'name=' + project + '-bad', '--format', '{{.Name}}'), '');
  } finally {
    for (const id of containers) docker('rm', '-f', id);
    const volumes = new Set(docker('volume', 'ls', '--format', '{{.Name}}').split('\n'));
    for (const p of [project, restored, legacy]) for (const role of roles) if (volumes.has(volume(p, role))) docker('volume', 'rm', volume(p, role));
    rmSync(root, { recursive: true, force: true });
  }
});
