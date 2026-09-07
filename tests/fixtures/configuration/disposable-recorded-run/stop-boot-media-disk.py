import json,os,pathlib,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};id='a19bf9ee-cd7f-4921-baac-39ce1694eb35'
def cli(*args):
 r=subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive','--timeout','90s'],env=env,capture_output=True,text=True,check=True,timeout=100);return r
r=cli('vm','stop',id,'--plan');(root/'boot-media-disk-stop-plan.json').write_text(r.stdout);print(r.stdout,flush=True);p=json.loads(r.stdout)['data']
assert p['operation']=='vm.stop' and p['review']['action']=='stop' and p['review']['vmID']==id and not p['review']['diskDeletion']
assert p['resourceIDs']==['libvirt|qemu:///system|vm|'+id] and p['acknowledgements']==['host-mutation']
r=cli('plan','apply',p['planID'],'--digest',p['planDigest'],'--idempotency-key','boot-media-disk-graceful-stop-8a092b4-001','--ack','host-mutation','--detach');(root/'boot-media-disk-stop-accepted.json').write_text(r.stdout);print(r.stdout,flush=True)
jid=json.loads(r.stdout)['data']['operationID']
for _ in range(110):
 r=cli('operation','show',jid);j=json.loads(r.stdout)['data']
 if j['state'] not in ('queued','validating','running'):break
 time.sleep(.5)
(root/'boot-media-disk-stop-result.json').write_text(r.stdout);print(r.stdout,flush=True)
assert j['state']=='succeeded',j
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
print(json.dumps({'vmID':id,'nativeState':'shut off','gracefulStop':'passed','hardStopAttempted':False,'sourceMediaDeleted':False}),flush=True)
