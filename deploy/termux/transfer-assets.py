"""Frozen, checksummed current-Go asset transfer. Never executes archive content."""
import hashlib,json,os,posixpath,re,stat,sys,tarfile
from pathlib import Path,PurePosixPath

ROLES={'media':'/media','openwa-data':'/app/data','telegram-data':'/telegram-data','redis':'/data'}
TOKEN=re.compile(r'^[1-9][0-9]*:[A-Za-z0-9_-]+$')

def safe_name(name,role):
    p=PurePosixPath(name)
    if p.is_absolute() or '..' in p.parts or '\\' in name or '\0' in name:raise ValueError('Unsafe archive path')
    for index,part in enumerate(p.parts):
        if ':' in part and not(role=='telegram-data' and index==0 and TOKEN.fullmatch(part)):
            raise ValueError('Unsafe drive or alternate-stream path')
    return str(p)

def safe_link(name,target,role):
    if role!='openwa-data' or '\\' in target or '\0' in target:raise ValueError('Unexpected asset link')
    if target.startswith('/'):
        prefix=ROLES[role].rstrip('/')+'/'
        if not target.startswith(prefix):raise ValueError('Link leaves retained asset role')
        normalized=target[len(prefix):]
    else:normalized=posixpath.normpath(posixpath.join(posixpath.dirname(name),target))
    if normalized=='..' or normalized.startswith('../'):raise ValueError('Link leaves retained asset role')
    safe_name(normalized,role)

def digest(path):
    with Path(path).open('rb') as source:return hashlib.file_digest(source,'sha256').hexdigest()

def pack(role,source,out):
    source=Path(source);out=Path(out);out.mkdir(parents=True,exist_ok=True,mode=0o700)
    target=out/(role+'.tar.gz')
    if target.exists():raise ValueError('Refusing an existing frozen asset archive')
    entries={}
    with tarfile.open(target,'w:gz',compresslevel=3,dereference=False) as archive:
        paths=[source]
        for parent,dirs,files in os.walk(source,followlinks=False):
            dirs.sort();files.sort()
            paths.extend(Path(parent)/name for name in dirs+files)
        for path in paths:
            name=safe_name(str(path.relative_to(source)),role)
            if name in entries:raise ValueError('Duplicate source entry')
            info=path.lstat();mode=stat.S_IMODE(info.st_mode)&0o777
            entry={'mode':mode,'uid':info.st_uid,'gid':info.st_gid}
            if stat.S_ISDIR(info.st_mode):entry['type']='directory'
            elif stat.S_ISREG(info.st_mode):
                entry.update(type='file',size=info.st_size,sha256=digest(path))
            elif stat.S_ISLNK(info.st_mode):
                link=os.readlink(path);safe_link(name,link,role);entry.update(type='symlink',target=link)
            else:raise ValueError('Refusing special asset entry')
            archive.add(path,arcname=name,recursive=False)
            after=path.lstat()
            if (info.st_size,info.st_mtime_ns)!=(after.st_size,after.st_mtime_ns):raise ValueError('Source asset changed during frozen export')
            entries[name]=entry
    target.chmod(0o600)
    receipt={'role':role,'archive_sha256':digest(target),'entries':entries}
    receipt_path=out/(role+'.manifest.json')
    with receipt_path.open('x') as output:json.dump(receipt,output,sort_keys=True)
    receipt_path.chmod(0o600)
    print(json.dumps({'role':role,'entries':len(entries),'bytes':sum(e.get('size',0) for e in entries.values()),'archive_bytes':target.stat().st_size}),flush=True)

def restore(role,archive_path,manifest_path,destination):
    archive_path=Path(archive_path);destination=Path(destination)
    receipt=json.loads(Path(manifest_path).read_text())
    if receipt['role']!=role or digest(archive_path)!=receipt['archive_sha256']:raise ValueError('Asset archive checksum mismatch')
    if destination.exists():raise ValueError('Refusing existing production asset destination')
    destination.mkdir(mode=0o700)
    expected=receipt['entries'];seen=set()
    with tarfile.open(archive_path,'r|gz') as archive:
        for member in archive:
            name=safe_name(member.name,role)
            if name in seen or name not in expected:raise ValueError('Duplicate or unmanifested archive member')
            seen.add(name);entry=expected[name]
            path=destination/name
            if name!='.':
                parent=destination
                for part in PurePosixPath(name).parts[:-1]:
                    parent=parent/part
                    if parent.is_symlink() or not parent.is_dir():raise ValueError('Unsafe archive ancestor')
            if entry['type']=='directory' and member.isdir():
                if name!='.':path.mkdir(mode=entry['mode'])
            elif entry['type']=='file' and member.isfile() and member.size==entry['size']:
                source=archive.extractfile(member);hasher=hashlib.sha256()
                fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,entry['mode'])
                with os.fdopen(fd,'wb') as output:
                    while chunk:=source.read(1<<20):output.write(chunk);hasher.update(chunk)
                    output.flush();os.fsync(output.fileno())
                if hasher.hexdigest()!=entry['sha256']:raise ValueError('Restored asset content mismatch')
            elif entry['type']=='symlink' and member.issym() and member.linkname==entry['target']:
                safe_link(name,member.linkname,role);os.symlink(member.linkname,path)
            else:raise ValueError('Asset entry type mismatch')
    if seen!=set(expected):raise ValueError('Incomplete asset restore')
    # Re-read restored bytes independently of extraction receipts.
    for name,entry in expected.items():
        path=destination/name
        if entry['type']=='file' and (path.is_symlink() or digest(path)!=entry['sha256']):raise ValueError('Restored bytes failed independent verification')
    print(json.dumps({'role':role,'verified_entries':len(seen),'archive_sha256':receipt['archive_sha256']}),flush=True)

if __name__=='__main__':
    os.umask(0o077)
    try:
        operation,role,*args=sys.argv[1:]
        if role not in ROLES:raise ValueError('Unknown asset role')
        if operation=='pack':pack(role,*args)
        elif operation=='restore':restore(role,*args)
        else:raise ValueError('Unknown asset operation')
    except Exception:
        print('Frozen asset transfer refused: unsafe, changed, incomplete or inaccessible state',file=sys.stderr);sys.exit(1)
