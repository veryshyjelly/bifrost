#!/data/data/com.termux/files/usr/bin/python
# Control the supervised native stack; startup requires the reviewed transfer
# proof and waits for each dependency before enabling its consumer.
import importlib.util
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import time
import urllib.request

ROOT=Path.home()/'bifrost-phone'
PREFIX='/data/data/com.termux/files/usr/bin/'
os.environ['PATH']=PREFIX+':/system/bin'
spec=importlib.util.spec_from_file_location('phone_runtime',ROOT/'phone-runtime.py')
runtime=importlib.util.module_from_spec(spec);spec.loader.exec_module(runtime)

def supervisor_running():
    file=ROOT/'supervisor.json'
    if not file.exists():return False
    data=json.loads(file.read_text())
    try:
        _,ticks=runtime.proc_info(data['pid'])
        return ticks==data['start_ticks'] and b'runsvdir' in Path(f'/proc/{data["pid"]}/cmdline').read_bytes()
    except (FileNotFoundError,ProcessLookupError):return False

def sv(action,role,timeout=120):
    subprocess.run([PREFIX+'sv','-w',str(timeout),action,str(ROOT/'services'/role)],check=True)

def await_ready(check,label,timeout=240):
    until=time.monotonic()+timeout
    while time.monotonic()<until:
        try:
            if check():return
        except (OSError,subprocess.SubprocessError):pass
        time.sleep(1)
    raise RuntimeError(label+' did not become ready; inspect private service logs')

def http_ok(url):
    with urllib.request.urlopen(url,timeout=3) as response:return response.status==200

def port_open(port):
    with socket.create_connection(('127.0.0.1',port),timeout=2):return True

def start():
    runtime.verified(ROOT)  # Must precede any supervisor or provider startup.
    if not supervisor_running():
        output=(ROOT/'logs/supervisor.log').open('ab')
        process=subprocess.Popen([PREFIX+'runsvdir','-P',str(ROOT/'services')],
            stdout=output,stderr=subprocess.STDOUT,start_new_session=True)
        _,ticks=runtime.proc_info(process.pid)
        (ROOT/'supervisor.json').write_text(json.dumps({'pid':process.pid,'start_ticks':ticks})+'\n')
        await_ready(lambda:all(subprocess.run([PREFIX+'sv','status',str(ROOT/'services'/name)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0 for role in runtime.ROLES for name in [role,role+'/log']),'supervisor',15)
    subprocess.run([PREFIX+'termux-wake-lock'],check=True)
    for name in runtime.ROLES:
        (ROOT/'services'/name/'log/down').unlink(missing_ok=True)
        sv('up',name+'/log',15)
    for name in runtime.ROLES:
        (ROOT/'services'/name/'down').unlink(missing_ok=True)
        sv('up',name,15)
        if name=='postgres':
            await_ready(lambda:subprocess.run([PREFIX+'pg_isready','-h','127.0.0.1','-p','5432'],
                stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0,'PostgreSQL')
        elif name=='redis':await_ready(lambda:port_open(6379),'Redis')
        elif name=='openwa':await_ready(lambda:http_ok('http://127.0.0.1:2785/api/health/ready'),'OpenWA')
        elif name=='telegram-api':await_ready(lambda:port_open(8081),'Telegram API')
        elif name=='bifrost':await_ready(lambda:http_ok('http://127.0.0.1:3210/readyz'),'Bifrost',600)
    print('All native services ready; inspect receipts before declaring cutover complete.')

def stop():
    # Stop consumers before their dependencies. Keep runit down markers so a
    # surviving supervisor cannot restart writers during transfer or recovery.
    if not supervisor_running():
        print('Supervisor is not running.');return
    for name in reversed(runtime.ROLES):
        (ROOT/'services'/name/'down').touch(mode=0o600)
        sv('down',name)
    print('All provider processes stopped gracefully; logs remain available.')

if __name__=='__main__':
    try:
        action=sys.argv[1] if len(sys.argv)>1 else 'status'
        if action=='start':start()
        elif action=='stop':stop()
        elif action=='status':
            if supervisor_running():
                for name in runtime.ROLES:sv('status',name,2)
            else:print('Disabled preparation: supervisor is not running; no production activation.')
        else:raise RuntimeError('Use status, start or stop')
    except Exception as error:
        print('Phone control refused: '+str(error),file=sys.stderr);sys.exit(1)
