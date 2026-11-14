#!/data/data/com.termux/files/usr/bin/python
"""Guarded native launchers. Real services require verified current-Go transfer."""
import hashlib
import json
import os
from pathlib import Path
import signal
import sys

PREFIX='/data/data/com.termux/files/usr'
ROOT=Path(os.environ.get('BIFROST_PHONE_ROOT',str(Path.home()/'bifrost-phone')))
ROLES=('postgres','redis','openwa','telegram-api','bifrost')
BIN='/opt/bifrost-candidate/bin/'

def private_json(path):
    if path.is_symlink() or path.stat().st_mode & 0o077:
        raise RuntimeError('Runtime configuration must be a private regular file')
    return json.loads(path.read_text())

def verified(root):
    android=private_json(root/'state/ANDROID-PROCESS-CONTROL.json')
    if android.get('max_phantom_processes',0)<256 or android.get('scope') not in ['until-reboot','persistent']:
        raise RuntimeError('Android process limit has not been verified for this server')
    if android['scope']=='until-reboot' and android.get('boot_id')!=Path('/proc/sys/kernel/random/boot_id').read_text().strip():
        raise RuntimeError('Temporary Android process-limit tuning must be renewed after reboot')
    config_path=root/'state/runtime.env.json'
    config=private_json(config_path)
    proof=private_json(root/'state/CUTOVER-VERIFIED.json')
    if not all(proof.get(key) is True for key in ['source_writers_stopped','databases_verified','assets_verified']):
        raise RuntimeError('Current production state transfer is not verified')
    if proof.get('policy')!='current-go-state-transfer' or proof.get('account_id')!=config.get('account_id'):
        raise RuntimeError('Activation must refer to this current-Go account')
    if proof.get('config_sha256')!=hashlib.sha256(config_path.read_bytes()).hexdigest():
        raise RuntimeError('Runtime configuration differs from the reviewed activation')
    envs=config.get('environments',{})
    if not config.get('account_id') or envs.get('bifrost',{}).get('BIFROST_ACCOUNT_ID')!=config['account_id']:
        raise RuntimeError('Permanent account identity is missing or mismatched')
    return envs

def proc_info(pid):
    fields=Path(f'/proc/{pid}/stat').read_text().rsplit(')',1)[1].split()
    return int(fields[1]),fields[19]

def record_pid(root,role):
    directory=root/'state/run';directory.mkdir(parents=True,exist_ok=True,mode=0o700)
    pid=os.getpid();_,ticks=proc_info(pid)
    target=directory/(role+'.json');temporary=directory/(role+f'.{pid}.tmp')
    temporary.write_text(json.dumps({'pid':pid,'start_ticks':ticks})+'\n')
    temporary.chmod(0o600);temporary.replace(target)

def stop_guest(root,role,parent):
    path=root/'state/run'/(role+'.json')
    if not path.exists():return 1  # Pre-exec bootstrap only: normal runsv TERM.
    identity=private_json(path);pid=int(identity['pid'])
    try:
        _,ticks=proc_info(pid)
        if ticks!=identity['start_ticks']:return 0  # Refuse a reused PID.
        current=pid
        while current>1 and current!=parent:current,_=proc_info(current)
        if current!=parent:
            print('Refused guest signal outside this supervised process tree',file=sys.stderr)
            return 0
        os.kill(pid,signal.SIGTERM)
    except (FileNotFoundError,ProcessLookupError):return 1
    return 0

def role(root,name):
    verified(root)
    env=dict(os.environ,PATH=PREFIX+'/bin:/system/bin',BIFROST_PHONE_ROOT=str(root))
    if name=='postgres':
        args=[PREFIX+'/bin/proot-distro','login','--env','PROOT_DONT_SHARE_LIBANDROID_SHMEM=1',
              '--user','postgres','--bind',str(root)+':/control',
              '--bind',str(root/'state')+':/state','bifrost-postgres','--',
              '/bin/sh','/control/start-postgres.sh']
        os.execve(args[0],args,env)
    state=root/'state'
    args=[PREFIX+'/bin/proot-distro','login','--bind',str(root)+':/control',
        '--bind',str(state)+':/state','--bind',str(state/'media')+':/media',
        '--bind',str(state/'telegram-data')+':/telegram-data',
        '--bind',str(state/'openwa-data')+':/app/data',
        '--bind',str(state/'openwa-data')+':/opt/bifrost-candidate/openwa/data',
        'bifrost-debian','--','/usr/bin/python3','/control/phone-runtime.py','guest',name]
    os.execve(args[0],args,env)

def guest(name):
    root=Path('/control');envs=verified(root)
    settings=envs.get(name,{})
    if not isinstance(settings,dict) or not all(isinstance(k,str) and isinstance(v,str) for k,v in settings.items()):
        raise RuntimeError('Service environment must contain literal string values')
    env=dict(os.environ,**settings)
    env.update(PATH=BIN+':/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin')
    if name=='redis':
        args=['/opt/bifrost-redis-glibc/8.10.3/bin/redis-server','/state/redis.conf']
    elif name=='openwa':
        os.chdir('/opt/bifrost-candidate/openwa')
        env.update(NODE_OPTIONS='--require=/control/loopback-listener.cjs',PORT='2785',
            DATABASE_HOST='127.0.0.1',REDIS_HOST='127.0.0.1',DATABASE_PORT='5432',REDIS_PORT='6379',
            POSTGRES_BUILTIN='false',REDIS_BUILTIN='false',MINIO_BUILTIN='false',
            PUPPETEER_EXECUTABLE_PATH='/usr/bin/chromium',WWEBJS_AUTH_TIMEOUT_MS='300000')
        for key,default in [('XDG_CONFIG_HOME','/tmp/.config'),('XDG_CACHE_HOME','/tmp/.cache')]:
            Path(env.get(key,default)).mkdir(parents=True,exist_ok=True)
        args=[BIN+'node','dist/main']
    elif name=='telegram-api':
        os.chdir('/telegram-data')
        args=[BIN+'telegram-bot-api','--local','--http-ip-address=127.0.0.1','--http-port=8081',
              '--dir=/telegram-data','--temp-dir=/telegram-data/tmp']
    elif name=='bifrost':
        env.update(BRIDGE_BIND_HOST='127.0.0.1',BRIDGE_PORT='3210',BRIDGE_MEDIA='/media',
            TELEGRAM_DATA_ROOT='/telegram-data',
            STICKER_PYTHON='/opt/bifrost-candidate/sticker-codec/bin/python',
            STICKER_CHROMIUM='/usr/bin/chromium',STICKER_CHROMIUM_NO_SANDBOX='true')
        for key,default in [('TELEGRAM_API_URL','http://telegram-api:8081'),
                            ('OPENWA_URL','http://openwa:2785'),
                            ('BRIDGE_CALLBACK_URL','http://bifrost:3210')]:
            env.setdefault(key,default)
        args=[BIN+'bifrost','run']
    else:raise RuntimeError('Unknown guest role')
    record_pid(root,name)
    os.execve(args[0],args,env)

if __name__=='__main__':
    try:
        mode,name=sys.argv[1:3]
        if name not in ROLES:raise RuntimeError('Unknown service')
        if mode=='stop':
            # Termux's runit does not pass a service PID to control scripts.
            parent=sys.argv[3].strip() if len(sys.argv)>3 else ''
            if not parent:parent=Path('supervise/pid').read_text().strip()
            sys.exit(stop_guest(ROOT,name,int(parent)))
        if mode=='role':role(ROOT,name)
        elif mode=='guest':guest(name)
        else:raise RuntimeError('Unknown runtime operation')
    except Exception as error:
        print('Phone runtime refused: '+str(error),file=sys.stderr);sys.exit(111)
