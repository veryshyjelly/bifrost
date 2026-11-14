"""Restore administrative access at boot; keep production behind its transfer gate."""
import fcntl
import json
import logging
from logging.handlers import RotatingFileHandler
import os
from pathlib import Path
import subprocess
import time

PREFIX='/data/data/com.termux/files/usr/bin/'
HOME=Path.home()
ROOT=HOME/'bifrost-phone'
os.umask(0o077)
os.environ['PATH']=PREFIX+':/system/bin'

def process_names():
    found=set()
    for path in Path('/proc').iterdir():
        if not path.name.isdigit():continue
        try:
            if path.stat().st_uid!=os.getuid():continue
            argv0=(path/'cmdline').read_bytes().split(b'\0',1)[0]
            found.add(os.path.basename(argv0.decode(errors='replace')))
        except OSError:pass
    return found

def process_start(pid):
    return Path(f'/proc/{pid}/stat').read_text().rsplit(')',1)[1].split()[19]

def supervisor_running():
    try:
        saved=json.loads((ROOT/'admin-supervisor.json').read_text())
        return process_start(saved['pid'])==saved['start_ticks'] and b'runsvdir' in Path(f'/proc/{saved["pid"]}/cmdline').read_bytes()
    except (OSError,ValueError,KeyError):return False

def control(action,role):
    command=[PREFIX+'sv','-w','10',action,str(ROOT/'admin-services'/role)]
    completed=subprocess.run(command,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=15)
    if completed.returncode:raise RuntimeError('Administrative supervisor operation failed: '+completed.stdout.strip())

def main():
    (ROOT/'logs').mkdir(parents=True,exist_ok=True,mode=0o700)
    lock=(ROOT/'boot.lock').open('a')
    try:fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
    except BlockingIOError:return
    handler=RotatingFileHandler(ROOT/'logs/boot.log',maxBytes=1_000_000,backupCount=3)
    handler.setFormatter(logging.Formatter('%(asctime)s %(levelname)s %(message)s'))
    log=logging.getLogger('phone-boot');log.setLevel(logging.INFO);log.addHandler(handler)
    result={'already_running':[],'started':[],'production_started':False}
    try:
        current=process_names()
        needed=[]
        for name in ['sshd','cloudflared']:
            if name in current:result['already_running'].append(name)
            else:needed.append(name)
        if 'cloudflared' in needed:
            token=HOME/'.cloudflared/token'
            if token.is_symlink() or not token.is_file() or token.stat().st_mode&0o077:
                raise RuntimeError('Existing tunnel token must be a private regular file')
        if needed:
            if not supervisor_running():
                supervisor=subprocess.Popen([PREFIX+'runsvdir','-P',str(ROOT/'admin-services')],
                    stdin=subprocess.DEVNULL,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,start_new_session=True)
                (ROOT/'admin-supervisor.json').write_text(json.dumps({'pid':supervisor.pid,'start_ticks':process_start(supervisor.pid)})+'\n')
                until=time.monotonic()+15
                while not all(subprocess.run([PREFIX+'sv','status',str(ROOT/'admin-services'/name)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0 for role in needed for name in [role,role+'/log']):
                    if time.monotonic()>until:raise RuntimeError('Administrative supervisor did not initialize')
                    time.sleep(.1)
            for name in needed:
                unit=ROOT/'admin-services'/name
                (unit/'log/down').unlink(missing_ok=True)
                control('up',name+'/log')
                (unit/'down').unlink(missing_ok=True)
                control('up',name)
                result['started'].append(name)
        if (ROOT/'state/CUTOVER-VERIFIED.json').exists():
            tuning=json.loads((ROOT/'state/ANDROID-PROCESS-CONTROL.json').read_text())
            expired=(tuning.get('scope')=='until-reboot' and tuning.get('boot_id')!=Path('/proc/sys/kernel/random/boot_id').read_text().strip())
            if expired:
                result['production_held_reason']='Renew Android process-limit tuning over ADB after reboot; administrative access remains available'
            else:
                controller=subprocess.run([PREFIX+'python',str(ROOT/'phone-control.py'),'start'],
                    stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=900)
                if controller.returncode:raise RuntimeError('Production controller refused startup; review activation and private service logs')
                result['production_started']=True
        else:
            result['production_held_reason']='No verified live state transfer; production remains disabled'
        result['success']=True
        log.info('Administrative boot complete: existing=%s started=%s production_started=%s',
                 result['already_running'],result['started'],result['production_started'])
    except Exception as error:
        result['success']=False;result['error']=str(error);log.error('%s',error)
        raise
    finally:
        target=ROOT/'boot-last-result.json'
        temporary=ROOT/'boot-last-result.tmp'
        temporary.write_text(json.dumps(result,indent=2)+'\n');temporary.replace(target)
        print(json.dumps(result))

if __name__=='__main__':main()
