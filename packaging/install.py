#!/usr/bin/env python3
"""Install/uninstall only an explicit package-stage manifest; no VM/data cleanup."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
p = argparse.ArgumentParser()
p.add_argument('action',choices=['plan','install','uninstall'])
p.add_argument('--stage',required=True,type=Path)
p.add_argument('--destdir',required=True,type=Path,help='Explicit installation root; no implicit production host target')
a = p.parse_args()
manifest = json.loads((a.stage/'install-manifest.json').read_text())
if not a.destdir.is_absolute():
    p.error('destdir must be absolute')
root = a.destdir.resolve()
files = manifest['files']
for item in files:
    relative = Path(item['path'])
    if relative.is_absolute() or '..' in relative.parts or not relative.parts or relative.parts[0] != 'usr':
        p.error('unsafe manifest path')
    target = root/relative
    if target.resolve().is_relative_to(root) is False:
        p.error('destination symlink escape')
    if a.action in ('plan','install'):
        source = a.stage/relative
        if source.is_symlink() or hashlib.sha256(source.read_bytes()).hexdigest()!=item['sha256']:
            p.error('source integrity mismatch')
    if a.action == 'install' and target.exists():
        p.error('existing installation requires reviewed package-manager upgrade')
if a.action == 'plan':
    print(json.dumps({'action':'install','files':[str(root/item['path']) for item in files],'servicesEnabled':False},indent=2))
elif a.action == 'install':
    for item in files:
        target = root/item['path']
        target.parent.mkdir(parents=True,exist_ok=True)
        with target.open('xb') as out:
            out.write((a.stage/item['path']).read_bytes())
            out.flush()
            os.fsync(out.fileno())
        target.chmod(item['mode'])
else:
    for item in files:
        target = root/item['path']
        if not target.exists():
            continue
        if target.is_symlink() or hashlib.sha256(target.read_bytes()).hexdigest()!=item['sha256']:
            p.error('modified installation file preserved: '+str(target))
    for item in files:
        target = root/item['path']
        if target.exists():
            target.unlink()
print('Completed; VMs, disks, backups, bridges and user state are outside this manifest.')
