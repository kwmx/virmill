import hashlib,json,os,pathlib,sqlite3,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};vm='a19bf9ee-cd7f-4921-baac-39ce1694eb35'
def cli(*args):return subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=60)
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='running'
before=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:counts=list(db.execute('SELECT (SELECT count(*) FROM plans),(SELECT count(*) FROM jobs)'))
r=cli('vm','boot','show',vm);assert r.returncode==0;(root/'boot-media-live-view.json').write_text(r.stdout);print(r.stdout,flush=True);v=json.loads(r.stdout)['data']
assert v['state']=='running' and v['live'] is not None and v['persistent'] is not None and not v['guestBootVerified']
for layer in ('live','persistent'):
 assert {d['id']:d['order'] for d in v[layer]['devices']}=={'vda':1,'sda':0}
 assert not next(d for d in v[layer]['devices'] if d['id']=='sda')['mediaPresent']
r=cli('vm','set',vm,'--input','{"bootOrder":[{"kind":"disk","id":"vda"}],"applyMode":"next-boot"}','--plan');print(r.stdout+r.stderr,flush=True)
assert r.returncode!=0 and json.loads(r.stdout)['error']['code']=='UNSUPPORTED_CAPABILITY'
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])==before
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:assert list(db.execute('SELECT (SELECT count(*) FROM plans),(SELECT count(*) FROM jobs)'))==counts
print(json.dumps({'runningGuestConfigurationRefused':True,'noPlanOrJobAccepted':True,'persistentXMLUnchangedSHA256':hashlib.sha256(before).hexdigest(),'liveAndPersistentBootLayersObserved':True}),flush=True)
