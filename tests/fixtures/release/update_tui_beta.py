#!/usr/bin/env python3
"""Replace the beta core package while preserving the running coordinator.

Only the explicitly authorized disposable host is accepted. No guest, helper or
coordinator is stopped or started. The source change must be UI-only; this fixture
is not a general live service upgrade procedure.
"""
import argparse,hashlib,json,os,socket,subprocess
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--execute-disposable',action='store_true',required=True);p.add_argument('--root',type=Path,required=True);p.add_argument('--revision',required=True);a=p.parse_args()
assert socket.gethostname() in ('virmill-test','virmill-test.home') and os.getuid()==1000
root=a.root.resolve(strict=True);assert root.is_relative_to(Path.home()/'virmill-tests')
results=root/'upgrade';results.mkdir(mode=0o700)
log=[];report={'status':'failed','revision':a.revision,'scope':'UI-only core RPM replacement; running coordinator preserved, no guest actions'}
def run(args):
 r=subprocess.run(args,text=True,capture_output=True,timeout=120);log.append({'argv':args,'exitCode':r.returncode,'stdout':r.stdout,'stderr':r.stderr});(results/'commands.json').write_text(json.dumps(log,indent=2)+'\n');assert r.returncode==0,(args,r.stderr);return r.stdout

def digest(path):
 with Path(path).open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def cli(*args):
 v=json.loads(run(['/usr/bin/virmill',*args,'--output','json','--non-interactive']));assert v['error'] is None;return v['data']
def coordinator():
 pid=run(['systemctl','--user','show','virmilld.service','--property=MainPID','--value']).strip();assert int(pid)>1
 return {'pid':pid,'exeSHA256':digest('/proc/'+pid+'/exe'),'startTimeTicks':Path('/proc/'+pid+'/stat').read_text().split()[21]}
def observations():
 vms=cli('vm','list');jobs=cli('operation','list')
 return {'vms':hashlib.sha256(json.dumps(vms,sort_keys=True).encode()).hexdigest(),'jobs':hashlib.sha256(json.dumps(jobs,sort_keys=True).encode()).hexdigest()}
before=None;native=None
try:
 before=coordinator();native=observations();report['coordinatorBefore']=before
 expected=json.loads((root/'checksums.json').read_text());assert set(expected)=={'virmill-1.0.0-0.beta.1.x86_64.rpm'}
 for name,sha in expected.items():assert digest(root/name)==sha
 report['packageSHA256']=expected
 run(['sudo','-n','rpm','-Uvh','--replacepkgs',str(root/'virmill-1.0.0-0.beta.1.x86_64.rpm')])
 wanted=json.loads((root/'binaries.json').read_text());assert digest('/usr/bin/virmill')==wanted['virmill'] and digest('/usr/bin/virmilld')==wanted['virmilld']
 report['installedBinarySHA256']=wanted;report['version']=cli('version');assert report['version']['revision']==a.revision
 report['status']='passed'
except BaseException as e:report['error']=repr(e)
finally:
 try:
  report['runningCoordinatorPreserved']=before is not None and coordinator()==before
  report['guestInventoryAndJobsPreserved']=native is not None and observations()==native
  if not report['runningCoordinatorPreserved'] or not report['guestInventoryAndJobsPreserved']:report['status']='failed'
 except BaseException as e:report['preservationError']=repr(e);report['status']='failed'
 (results/'report.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report,sort_keys=True))
raise SystemExit(report['status']!='passed')
