"""Install guarded Termux launchers; never create credentials or activation proofs."""
import os
from pathlib import Path

PREFIX = '/data/data/com.termux/files/usr/bin/'
SOURCE = Path(__file__).resolve().parent


def directory(path):
    if path.is_symlink():
        raise RuntimeError('Refusing a symlink deployment directory')
    path.mkdir(mode=0o700, parents=True, exist_ok=True)


def write(path, content, mode=0o700):
    # Exclusive temporary creation avoids following a pre-planted link; publish
    # the fully written launcher by rename after flushing its contents.
    if path.is_symlink():
        raise RuntimeError('Refusing a symlink deployment file')
    temporary = path.with_name(path.name + '.install-tmp')
    if temporary.exists() or temporary.is_symlink():
        raise RuntimeError('Refusing an existing installation temporary file')
    try:
        with temporary.open('x') as output:
            output.write(content)
            output.flush()
            os.fsync(output.fileno())
        temporary.chmod(mode)
        temporary.replace(path)
    finally:
        temporary.unlink(missing_ok=True)


def install(home):
    os.umask(0o077)
    root = home / 'bifrost-phone'
    for path in [root, root/'state', root/'state/run', root/'logs', root/'services', root/'admin-services']:
        directory(path)
    for name in ['phone-runtime.py', 'phone-control.py', 'phone-boot.py',
                 'start-postgres.sh', 'loopback-listener.cjs']:
        write(root/name, (SOURCE/name).read_text())

    def unit(base, role, command, guest=False):
        path = root/base/role
        fresh = not path.exists()
        directory(path)
        fresh_log = not (path/'log').exists()
        directory(path/'log')
        log = root/'logs'/('admin-'+role if base=='admin-services' else role)
        directory(log)
        write(log/'config', 's1000000\nn10\n', 0o600)
        write(path/'run', f'#!{PREFIX}sh\nexport PATH={PREFIX}:/system/bin\nexec 2>&1\nexec {command}\n')
        write(path/'log/run', f'#!{PREFIX}sh\nexec {PREFIX}svlogd -tt {log}\n')
        if fresh:
            write(path/'down', '', 0o600)
        if fresh_log:
            write(path/'log/down', '', 0o600)
        if guest:
            directory(path/'control')
            write(path/'control/t', f'#!{PREFIX}sh\nexec {PREFIX}python {root}/phone-runtime.py stop {role} "$1"\n')

    for role in ['postgres', 'redis', 'openwa', 'telegram-api', 'bifrost']:
        unit('services', role, f'{PREFIX}python {root}/phone-runtime.py role {role}', guest=True)
    unit('admin-services', 'sshd', f'{PREFIX}sshd -D')
    unit('admin-services', 'cloudflared', f'{PREFIX}cloudflared tunnel run --token-file {home}/.cloudflared/token')
    directory(home/'.termux')
    directory(home/'.termux/boot')
    write(home/'.termux/boot/20-bifrost', (SOURCE/'20-bifrost').read_text())
    return root


if __name__ == '__main__':
    root = install(Path.home())
    print('Installed guarded launchers at', root)
    print('No services started; credentials, current state and activation proofs were preserved.')
