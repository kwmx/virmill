#!/usr/bin/env python3
"""Copy and inspect only the named stopped Fedora source on the authorized VM.

No guest is started, edited, defined, or connected. The source is opened read-only
and preserved; all image parsers run as the ordinary user against a private copy.
The generated copy/evidence remain in a fresh private directory. This establishes
fixture prerequisites only, not guest-tools installation or first-boot support.
"""
import argparse, hashlib, json, os, socket, stat, subprocess, sys
from pathlib import Path

SOURCE = Path('/var/lib/libvirt/images/virmill-test/nested-fedora.qcow2')
VM = '2ec994ce-2950-498c-8b19-d2f7dbb53a78'

def run(args, timeout=30):
    r=subprocess.run(args,stdin=subprocess.DEVNULL,capture_output=True,timeout=timeout)
    if r.returncode: raise RuntimeError(f'{args[0]} failed ({r.returncode}): '+r.stderr.decode(errors='replace')[:1000])
    return r.stdout

def cli(*args):
    value=json.loads(run(['/usr/bin/virmill',*args,'--output','json','--non-interactive']))
    if value['error']: raise RuntimeError(str(value['error']))
    return value['data']

def identity(s):return [s.st_dev,s.st_ino,s.st_mode,s.st_uid,s.st_gid,s.st_size,s.st_mtime_ns,s.st_ctime_ns]

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--root',required=True,type=Path)
    p.add_argument('--execute-disposable',required=True,action='store_true')
    a=p.parse_args()
    assert socket.gethostname() in ('virmill-test','virmill-test.home') and os.getuid()==os.geteuid()==1000
    root=a.root
    assert root.is_absolute() and root==root.resolve() and root.parent==Path.home()/'virmill-tests'
    root.mkdir(mode=0o700)
    os.umask(0o077)
    report={'status':'failed','scope':'independent read-only source copy and offline OS inspection only','source':str(SOURCE)}
    before=None
    try:
        before=cli('vm','list'); source=cli('vm','show',VM)
        assert source['state']=='stopped' and not source['hasManagedSave']
        assert all(j['state'] in ('succeeded','failed','partial','canceled','recovery-required') for j in cli('operation','list'))
        (root/'before-vms.json').write_text(json.dumps(before))
        report['version']=cli('version')
        report['tools']={tool:run(['/usr/bin/'+tool,'--version']).decode().strip() for tool in ('virt-inspector','virt-customize','guestfish','qemu-img')}
        source_fd=os.open(SOURCE,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
        with os.fdopen(source_fd,'rb') as src:
            original=os.fstat(src.fileno())
            assert stat.S_ISREG(original.st_mode) and original.st_size<=4<<30 and original.st_nlink==1
            assert os.statvfs(root).f_bavail*os.statvfs(root).f_frsize > original.st_size*2+(4<<30)
            target=root/'fedora-copy.qcow2'
            dest_fd=os.open(target,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
            digest=hashlib.sha256()
            with os.fdopen(dest_fd,'wb') as dst:
                while block:=src.read(1<<20):digest.update(block);dst.write(block)
                dst.flush();os.fsync(dst.fileno())
            assert identity(original)==identity(os.fstat(src.fileno()))==identity(SOURCE.stat())
            with target.open('rb') as copied:assert hashlib.file_digest(copied,'sha256').hexdigest()==digest.hexdigest()
            report.update(sourceIdentity=identity(original),sourceSHA256=digest.hexdigest(),copy=str(target))
        assert cli('vm','show',VM)==source
        # The product's confined source inspector detects format and refuses
        # unapproved external disk dependencies before any libguestfs parsing.
        described=cli('import','source','describe',str(target))
        (root/'source-description.json').write_text(json.dumps(described,indent=2))
        report['sourceDescription']=described
        assert described['kind']=='disk' and described['format']=='qcow2' and len(described['disks'])==1
        disk=described['disks'][0]
        assert disk['format']=='qcow2' and not disk.get('backingPath') and not disk.get('backingFormat')
        assert 0<disk['virtualBytes']<=32<<30
        inspection=run(['/usr/bin/virt-inspector','--format=qcow2','-a',str(target)],timeout=180)
        assert len(inspection)<8<<20
        (root/'inspection.xml').write_bytes(inspection)
        report['inspectionSHA256']=hashlib.sha256(inspection).hexdigest()
        report['status']='passed'
    except BaseException as e:report['error']=repr(e)
    finally:
        try:
            if before is not None:
                assert cli('vm','list')==before
                report['existingGuestsPreserved']=True
            if 'sourceIdentity' in report:
                assert identity(SOURCE.stat())==report['sourceIdentity']
                report['sourceMetadataPreserved']=True
        except BaseException as e:report['status']='failed';report['preservationError']=repr(e)
        (root/'report.json').write_text(json.dumps(report,indent=2)+'\n')
        print(json.dumps({k:v for k,v in report.items() if k!='sourceDescription'},sort_keys=True))
    return report['status']!='passed'

if __name__=='__main__':sys.exit(main())
