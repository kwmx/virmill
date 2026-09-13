#!/usr/bin/env python3
"""Read-only owner-operated real OVA metadata + TUI probe; no import/guest actions."""
import argparse, hashlib, json, os, re, socket, time
from pathlib import Path
from tui_workspace_probe import Runner, Terminal, inventory, generation, require
from import_options_probe import selected_label


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False

p=argparse.ArgumentParser()
p.add_argument('--execute-disposable',action='store_true',required=True)
p.add_argument('--binary',default='/usr/bin/virmill');p.add_argument('--binary-sha256',required=True)
p.add_argument('--connection',default='qemu:///system');p.add_argument('--source',type=Path,required=True)
p.add_argument('--output',type=Path,required=True);a=p.parse_args()
require(authorized_test_host() and os.getuid()==1000,'wrong host/actor')
require(a.binary=='/usr/bin/virmill' and a.connection=='qemu:///system','installed native CLI required')
source=a.source.resolve(strict=True);require(source.is_relative_to(Path.home()/'images') and source.is_file(),'explicit owner media required')
root=a.output.absolute();require(root.parent.resolve().is_relative_to(Path.home()/'virmill-tests') and not root.exists(),'new private output required');root.mkdir(mode=0o700)
fd=os.open(a.binary,os.O_RDONLY|os.O_NOFOLLOW)
with os.fdopen(os.dup(fd),'rb') as f:require(hashlib.file_digest(f,'sha256').hexdigest()==a.binary_sha256,'wrong binary')
r=Runner(a,root,fd);before=inventory(r.cli('vm','list'),a.connection);jobs=r.cli('operation','list');media=generation(source.stat())
report={'status':'failed','scope':'metadata description and settings only; no preparation, apply or guest boot'}
try:
    started=time.monotonic();d=r.cli('import','describe',str(source));elapsed=time.monotonic()-started;r.save('description.json',d)
    require(d['integrity']=='not-verified' and d['readiness']=='metadata-only' and not d['sha256'],'description incorrectly claims verification')
    require(all(not m['sha256'] for m in d['members']),'payload digest fabricated')
    s=d['systems'][0];require(s['os']=='Windows11_64' and s['firmware']=='uefi','source OS/firmware missing')
    require(next(i for i in s['hardware'] if i['resourceType']=='3')['quantity']=='2','CPU metadata missing')
    require(next(i for i in s['hardware'] if i['resourceType']=='4')['memoryMiB']==8192,'RAM metadata missing')
    require(any(x['kind']=='audio' for x in s['devices']) and any(x['kind']=='usb' for x in s['devices']),'device metadata missing')
    report['describeSeconds']=elapsed;report['sourceBytes']=source.stat().st_size;report['layouts']=[]
    for columns,rows in [(80,24),(120,36)]:
        t=Terminal(r,f'appliance-{columns}x{rows}',columns,rows)
        try:
            def wait(label,predicate,key=None):return t.wait(label,predicate,t.send(key) if key is not None else -1)
            def focus(label):
                for i in range(80):
                    text=t.screen.text()
                    if selected_label(text,label):return
                    wait('Focus '+label+' '+str(i),lambda s:s!=text,b'\t')
                raise RuntimeError('unreachable '+label)
            def edit(label,value):
                focus(label);t.send(b'\x15')
                while t.read(.05) and not t.screen.complete():pass
                wait('Edit '+label,lambda s:value in s and selected_label(s,label),value.encode())
            wait('Overview',lambda s:'Virtual machines' in s)
            wait('VM workspace',lambda s:' / VMs ' in s,b'2')
            wait('Import source choices',lambda s:'OVA appliance' in s,b'i')
            wait('Browser',lambda s:'Choose a file' in s,b'\r')
            wait('Browser path',lambda s:'Path:' in s,b'\x0c');t.send(b'\x15')
            while t.read(.05) and not t.screen.complete():pass
            wait('Source folder typed',lambda s:'Path:' in s,str(source.parent).encode())
            wait('Source folder opened',lambda s:'Path:' not in s,b'\r')
            wait('Filter',lambda s:'Enter done' in s,b'/')
            wait('Source listed',lambda s:source.name in s,source.name.encode())
            wait('Filter complete',lambda s:'Enter open/select' in s,b'\r')
            started=time.monotonic()
            text=wait('Automatic appliance summary',lambda s:'Review appliance' in s and 'CPU cores' in s,b'\r')
            require('Windows 11' in text and '8192' in text and 'Advanced settings' in text and 'Continue' in text,'summary omitted essentials/actions')
            report.setdefault('summarySeconds',[]).append(time.monotonic()-started)
            edit('VM name','owner-preview');edit('CPU cores','3');edit('Memory (MiB)','4096')
            focus('Advanced settings');wait('Advanced before destination',lambda s:'Advanced hardware' in s and 'Machine' in s,b'\r')
            wait('Return summary preserves basics',lambda s:'Review appliance' in s and '4096' in s and 'owner-preview' in s,b'\x1b')
            focus('Continue');wait('Choose destination',lambda s:'New folder name' in s,b'\r')
            wait('Return summary',lambda s:'Review appliance' in s and 'owner-preview' in s,b'\x1b')
            report['layouts'].append([columns,rows])
        finally:t.close()
    report['status']='passed'
except BaseException as e:report['error']=repr(e)
finally:
    report['preserved']=inventory(r.cli('vm','list'),a.connection)==before and r.cli('operation','list')==jobs and generation(source.stat())==media
    if not report['preserved']:report['status']='failed'
    r.save('report.json',report);print(json.dumps(report,sort_keys=True));os.close(fd)
raise SystemExit(report['status']!='passed')
