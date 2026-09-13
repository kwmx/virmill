#!/usr/bin/env python3
"""Install exact local beta RPMs on the explicitly authorized disposable VM.

Preserves every existing guest/network definition, user source-media metadata,
helper policy and stopped coordinator state. Leaves only the ordinary user
coordinator running for owner testing; never starts a guest or root helper.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import time

p=argparse.ArgumentParser()
p.add_argument('--execute-disposable',action='store_true',required=True)
p.add_argument('--root',type=Path,required=True)
p.add_argument('--revision',required=True)
p.add_argument('--product-version',required=True)
p.add_argument('--verify-installed',action='store_true',help='Read installed artifacts; do not reinstall or back up a running journal')
a=p.parse_args()
assert socket.gethostname() in ('virmill-test','virmill-test.home') and os.getuid()==1000
root=a.root.resolve(strict=True)
assert root.is_relative_to(Path.home()/'virmill-tests')
results=root/('verification' if a.verify_installed else 'results');results.mkdir(mode=0o700)
events=[]
report={'status':'failed','revision':a.revision,'scope':'native Fedora RPM upgrade and ordinary user CLI/TUI smoke; no guest or helper mutation'}
def run(args,check=True):
 r=subprocess.run(args,capture_output=True,text=True,timeout=120)
 events.append({'argv':args,'exitCode':r.returncode,'stdout':r.stdout,'stderr':r.stderr})
 (results/'commands.json').write_text(json.dumps(events,indent=2)+'\n')
 if check and r.returncode:raise RuntimeError('command failed: '+repr(args))
 return r

def sha(path):
 with Path(path).open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def inventory():
 out={}
 for kind, listing in [('domain',['list','--all','--uuid']),('network',['net-list','--all','--uuid'])]:
  for identity in run(['virsh','-c','qemu:///system',*listing]).stdout.split():
   verb='dumpxml' if kind=='domain' else 'net-dumpxml'
   args=['virsh','-c','qemu:///system',verb,identity,'--inactive']
   out[kind+':'+identity]=hashlib.sha256(run(args).stdout.encode()).hexdigest()
   if kind=='domain':assert run(['virsh','-c','qemu:///system','domstate',identity]).stdout.strip()=='shut off'
 return out

def media():
 return {str(f.relative_to(Path.home()/'images')):[f.stat().st_ino,f.stat().st_size,f.stat().st_mtime_ns]
   for f in (Path.home()/'images').rglob('*') if f.is_file()}
def helper():
 policy=run(['sudo','-n','sha256sum','/etc/virmill/helper-policy.json']).stdout.split()[0]
 units=run(['systemctl','is-active','virmill-host-helper.socket','virmill-host-helper.service'],False).stdout.split()
 assert units==['inactive','inactive']
 return policy

def cli(*args,check=True):
 r=run(['/usr/bin/virmill',*args,'--output','json','--non-interactive'],check)
 v=json.loads(r.stdout)
 if check:assert v['error'] is None,v
 return v

baseline=None;before_media=None;policy=None
try:
 if not a.verify_installed:assert run(['pgrep','-x','virmilld'],False).returncode==1
 baseline=inventory();before_media=media();policy=helper()
 for part in ['.local/state/virmill','.config/virmill']:
  source=Path.home()/part
  if source.exists() and not a.verify_installed:
   dest=root/'pre-upgrade'/part
   dest.parent.mkdir(mode=0o700,parents=True,exist_ok=True)
   shutil.copytree(source,dest,symlinks=True)
 report['priorPackages']=run(['rpm','-q','virmill','virmill-host-helper']).stdout.splitlines()
 checks=json.loads((root/'checksums.json').read_text())
 report['artifactSHA256']=checks
 for name,wanted in checks.items():assert '/' not in name and sha(root/name)==wanted
 rpms=[str(root/name) for name in checks if name.endswith('.rpm')]
 assert len(rpms)==2
 if not a.verify_installed:run(['sudo','-n','rpm','-Uvh',*rpms])
 expected=json.loads((root/'binaries.json').read_text())
 paths={'virmill':'/usr/bin/virmill','virmilld':'/usr/bin/virmilld','virmill-host-helper':'/usr/libexec/virmill-host-helper'}
 for name,path in paths.items():assert sha(path)==expected[name]
 report['installedBinarySHA256']=expected
 if not a.verify_installed:
  run(['systemctl','--user','daemon-reload'])
  run(['systemctl','--user','start','virmilld.service'])
 for _ in range(100):
  if (Path('/run/user/1000/virmill/control.sock')).exists():break
  time.sleep(.05)
 version=cli('version')['data'];report['version']=version
 assert version['version']==a.product_version and version['revision']==a.revision and not version['releaseQualified']
 for args in [('doctor',),('host','capabilities'),('vm','list'),('storage','pool','list'),('network','list'),('device','usb','list'),('operation','list'),('config','validate','/usr/share/virmill/examples/guest-recipes/posix-readiness.json')]:
  cli(*args)
 # Real terminal startup and clean keyboard exit of the installed TUI.
 import fcntl,pty,select,struct,termios
 master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',24,80,0,0))
 proc=subprocess.Popen(['/usr/bin/virmill','tui'],stdin=slave,stdout=slave,stderr=slave,env=dict(os.environ,TERM='xterm-256color'))
 os.close(slave);transcript=bytearray()
 try:
  until=time.monotonic()+10
  while time.monotonic()<until and b'Overview' not in transcript:
   assert proc.poll() is None
   if select.select([master],[],[],.1)[0]:transcript.extend(os.read(master,16384))
   assert len(transcript)<262144
  assert b'Overview' in transcript
  os.write(master,b'q');assert proc.wait(timeout=5)==0
 finally:
  if proc.poll() is None:proc.terminate();proc.wait(timeout=5)
  os.close(master)
  (results/'tui.log').write_bytes(transcript)
 report['installedTUI80x24StartedAndExited']=True
 report['userCoordinatorActive']=run(['systemctl','--user','is-active','virmilld.service']).stdout.strip()=='active'
 report['status']='passed'
except BaseException as exc:report['error']=repr(exc)
finally:
 try:
  report['allPriorGuestAndNetworkDefinitionsPreserved']=baseline is not None and inventory()==baseline
  report['sourceMediaMetadataPreserved']=before_media is not None and media()==before_media
  report['helperPolicyPreservedAndInactive']=policy is not None and helper()==policy
  if not all(report[k] for k in ('allPriorGuestAndNetworkDefinitionsPreserved','sourceMediaMetadataPreserved','helperPolicyPreservedAndInactive')):report['status']='failed'
 except BaseException as exc:report['preservationError']=repr(exc);report['status']='failed'
 (results/'report.json').write_text(json.dumps(report,indent=2)+'\n')
 print(json.dumps(report,sort_keys=True))
raise SystemExit(report['status']!='passed')
