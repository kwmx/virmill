#!/usr/bin/env python3
"""Toggle only the retained owned stopped test guest, then restore its policy.

Explicit disposable execution; no start/stop/reboot or XML/storage mutation.
Uses only public reviewed plans/jobs. Unknown job outcomes are retained without
replay; inspect the report and actual policy before recovery. Not a boot test.
"""
import argparse, hashlib, json, os, socket, stat, time
from pathlib import Path
from autostart_tui_probe import IDENTITY, NAME, URI, TERMINAL_STATES, normalize
from tui_workspace_probe import Runner, canonical_path, require, strict_json, media_listing

p=argparse.ArgumentParser();p.add_argument('--root',required=True,type=Path);p.add_argument('--execute-disposable',required=True,action='store_true');a=p.parse_args()
require(socket.gethostname() in ('virmill-test','virmill-test.home') and os.getuid()==os.geteuid()==1000,'wrong authorized host/actor')
stage=canonical_path(str(a.root.absolute()));require(stage.parent==Path.home()/'virmill-tests' and stage.stat().st_uid==1000 and stat.S_IMODE(stage.stat().st_mode)==0o700,'private stage required')
os.umask(0o077);out=stage/'autostart-cycle';out.mkdir(mode=0o700)
expected=strict_json((stage/'binaries.json').read_bytes())
for name in ('virmill','virmilld'):
 with Path('/usr/bin/'+name).open('rb') as f:require(hashlib.file_digest(f,'sha256').hexdigest()==expected[name],'installed binary mismatch')
fd=os.open('/usr/bin/virmill',os.O_RDONLY|os.O_NOFOLLOW)
with os.fdopen(os.dup(fd),'rb') as f:require(hashlib.file_digest(f,'sha256').hexdigest()==expected['virmill'],'held binary mismatch')
r=Runner(argparse.Namespace(binary='/usr/bin/virmill',connection=URI),out,fd)
report={'status':'failed','vmID':IDENTITY,'scope':'native SetAutostart true/false and durable readback only; no guest power action or reboot test','binaries':expected,'jobs':[]}
before=jobs=media=None
try:
 before=r.cli('vm','list');jobs=r.cli('operation','list');media=media_listing(Path.home()/'images')
 require(all(j['state'] in TERMINAL_STATES for j in jobs),'active job prevents test')
 original=next(v for v in before if v['key']['resourceUUID']==IDENTITY)
 require(original['name']==NAME and original['ownership']=='managed' and original['state']=='stopped' and original['persistentXML'] and original['autostart'] is False,'retained owned stopped fixture differs')
 r.save('before-vms.json',before);r.save('before-jobs.json',jobs)
 for label,old in [('enable',False),('restore',True)]:
  plan=r.cli('vm','autostart',IDENTITY,'--input',json.dumps({'enabled':not old}),'--plan')
  normalize(plan,old)
  require(plan['acknowledgements']==['host-mutation'],'unexpected grants')
  r.save(label+'-plan.json',plan)
  job=r.cli('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--ack','host-mutation','--detach','--idempotency-key',stage.name+'-'+label)
  report['jobs'].append(job['operationID']);r.save(label+'-accepted.json',job)
  deadline=time.monotonic()+30
  while job['state'] not in TERMINAL_STATES and time.monotonic()<deadline:
   time.sleep(.2);job=r.cli('operation','show',job['operationID'])
  r.save(label+'-job.json',job);require(job['state']=='succeeded','uncertain outcome; do not replay')
  observed=r.cli('vm','show',IDENTITY)
  require(observed['autostart'] is (not old) and observed['state']=='stopped' and observed['persistentXML']==original['persistentXML'] and observed.get('liveXML')==original.get('liveXML'),'policy, power or XML readback differs')
  report[label+'Verified']=True
 require(r.cli('vm','list')==before,'VM inventory not restored')
 afterjobs=r.cli('operation','list');require([j for j in afterjobs if j['operationID'] not in report['jobs']]==jobs,'prior job records changed')
 require(media_listing(Path.home()/'images')==media,'source media changed')
 report.update(status='passed',autostartRestored=True,priorGuestsJobsAndMediaPreserved=True)
except BaseException as e:report['error']=repr(e)
finally:
 try:
  v=r.cli('vm','show',IDENTITY);report['finalAutostart']=v['autostart'];report['finalState']=v['state']
 except BaseException as e:report['observationError']=repr(e)
 r.save('report.json',report);os.close(fd);print(json.dumps(report,sort_keys=True))
raise SystemExit(report['status']!='passed')
