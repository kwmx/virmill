#!/usr/bin/env python3
"""Restore only recorded logical subnet facts for retained disposable fixtures.

No native network/helper/guest mutation. The service must match both supplied
UUID/CIDRs to exact native intent before an absent user allocation file is created.
Facts come from protected-network-native-002/003 retained release evidence.
"""
import argparse, hashlib, json, os, socket, stat, subprocess
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--execute-disposable',action='store_true',required=True);a=p.parse_args()
assert socket.gethostname() in ('virmill-test','virmill-test.home') and os.getuid()==os.geteuid()==1000
root=a.root.absolute();assert root.resolve(strict=True)==root and root.parent==Path.home()/'virmill-tests' and root.stat().st_uid==1000 and stat.S_IMODE(root.stat().st_mode)==0o700
os.umask(0o077);out=root/'allocation-facts';out.mkdir(mode=0o700)
def sha(path):
 with Path(path).open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
expected=json.loads((root/'binaries.json').read_text());assert all(sha('/usr/bin/'+n)==expected[n] for n in ('virmill','virmilld'))
log=[]
def cli(*args):
 r=subprocess.run(['/usr/bin/virmill',*args,'--output','json','--non-interactive'],capture_output=True,timeout=30)
 assert len(r.stdout)+len(r.stderr)<2<<20
 log.append({'args':args,'exitCode':r.returncode,'stdout':r.stdout.decode(),'stderr':r.stderr.decode()});(out/'commands.json').write_text(json.dumps(log,indent=2)+'\n')
 value=json.loads(r.stdout);assert r.returncode==0 and value['error'] is None;return value['data']
def observed():return {kind:cli(*args) for kind,args in [('vms',['vm','list']),('networks',['network','list']),('jobs',['operation','list'])]}
planned=[{'id':'76e1eaa8-42b2-46ac-93d3-be9d5a31caac','cidr':'10.193.78.0/24'},{'id':'ac06e66c-5f16-4997-b02a-55b327c522e6','cidr':'10.193.75.0/24'}]
report={'status':'failed','scope':'restore exact historical logical allocation facts only; no network mutation','planned':planned,'sources':['protected-network-native-003','protected-network-native-002']}
before=None
try:
 before=observed()
 checked=cli('network','cidr','check','--input',json.dumps({'candidates':['10.0.0.0/24'],'planned':planned}))
 assert checked['reserved'] is False and checked['isolationVerified'] is False
 report['nativeIntentMatchVerifiedByService']=True
 target=Path.home()/'.config/virmill/network-allocation.json';target.parent.mkdir(mode=0o700,parents=True,exist_ok=True)
 assert target.parent.resolve()==target.parent and target.parent.stat().st_uid==1000 and not target.parent.stat().st_mode&0o022
 config={'version':1,'ranges':[{'cidr':x,'prefixLength':24} for x in ('10.0.0.0/8','172.16.0.0/12','192.168.0.0/16')],'planned':planned}
 raw=(json.dumps(config,indent=2)+'\n').encode()
 fd=os.open(target,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
 with os.fdopen(fd,'wb') as f:f.write(raw);f.flush();os.fsync(f.fileno())
 report['configurationSHA256']=sha(target);report['createdAbsentConfiguration']=True
 repeat=cli('network','cidr','check','--input',json.dumps({'candidates':['10.0.0.0/24']}))
 assert repeat==checked
 report['defaultConfigurationReadbackMatches']=True;report['status']='passed'
except BaseException as e:report['error']=repr(e)
finally:
 try:
  report['guestNetworkAndJobInventoryPreserved']=before is not None and observed()==before
  if not report['guestNetworkAndJobInventoryPreserved']:report['status']='failed'
 except BaseException as e:report.update(status='failed',preservationError=repr(e))
 (out/'report.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report,sort_keys=True))
raise SystemExit(report['status']!='passed')
