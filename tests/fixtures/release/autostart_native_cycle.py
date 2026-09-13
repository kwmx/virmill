#!/usr/bin/env python3
"""Toggle only the retained owned stopped test guest, then restore its policy.

Explicit disposable execution; no start/stop/reboot or XML/storage mutation.
Uses only public reviewed plans/jobs. Unknown job outcomes are retained without
replay; inspect the report and actual policy before recovery. Not a boot test.
"""
import argparse, hashlib, json, os, socket, stat, time
import xml.etree.ElementTree as ET
from pathlib import Path
from autostart_tui_probe import IDENTITY, NAME, URI, TERMINAL_STATES, normalize
from tui_workspace_probe import Runner, canonical_path, require, strict_json, media_listing


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


p=argparse.ArgumentParser();p.add_argument('--root',required=True,type=Path);p.add_argument('--execute-disposable',required=True,action='store_true');a=p.parse_args()
require(authorized_test_host() and os.getuid()==os.geteuid()==1000,'wrong authorized host/actor')
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
 prior_path=canonical_path(str(Path.home()/'virmill-tests/wizard-drafts-tools-f371fa8/guest-tools-fedora/report.json'))
 prior_raw=prior_path.read_bytes();prior=strict_json(prior_raw)
 require(prior['status']=='passed' and prior['vmID']==IDENTITY and prior['name']==NAME and prior['ownedFixtureStopped'] and prior['sourcesAndNetworksPreserved'],'prior test ownership report differs')
 report['originalFixtureReportSHA256']=hashlib.sha256(prior_raw).hexdigest()
 before=r.cli('vm','list');jobs=r.cli('operation','list');media=media_listing(Path.home()/'images')
 require(all(j['state'] in TERMINAL_STATES for j in jobs),'active job prevents test')
 original=next(v for v in before if v['key']['resourceUUID']==IDENTITY)
 require(original['name']==NAME and original['ownership']=='external' and original['state']=='stopped' and original['persistentXML'] and original['autostart'] is False,'retained owned stopped fixture differs')
 tree=ET.fromstring(original['persistentXML']);marker=tree.find('metadata/{urn:virmill:guest-tools-fixture:v1}fixture')
 require(tree.findtext('uuid')==IDENTITY and marker is not None and marker.get('id')==IDENTITY and [d.get('file') for d in tree.findall("devices/disk[@device='disk']/source")]==[prior['installedDisk']],'fixture definition differs from retained ownership evidence')
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
