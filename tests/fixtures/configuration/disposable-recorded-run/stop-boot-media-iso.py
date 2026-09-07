import json,os,pathlib,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};vm='a19bf9ee-cd7f-4921-baac-39ce1694eb35'
def cli(*args):return subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True,timeout=60)
assert (root/'boot-media-iso-001.png').is_file()
r=cli('vm','stop',vm,'--hard','--plan');(root/'boot-media-iso-stop-plan.json').write_text(r.stdout);print(r.stdout,flush=True);p=json.loads(r.stdout)['data']
assert p['operation']=='vm.hard-stop' and p['resourceIDs']==['libvirt|qemu:///system|vm|'+vm]
assert set(p['acknowledgements'])=={'host-mutation','data-loss-hard-stop'} and p['review']['action']=='hard-stop'
r=cli('plan','apply',p['planID'],'--digest',p['planDigest'],'--idempotency-key','boot-media-iso-hard-stop-8a092b4-001','--ack','host-mutation','--ack','data-loss-hard-stop','--detach');(root/'boot-media-iso-stop-accepted.json').write_text(r.stdout);print(r.stdout,flush=True);jid=json.loads(r.stdout)['data']['operationID']
for _ in range(60):
 r=cli('operation','show',jid);job=json.loads(r.stdout)['data']
 if job['state'] not in ('queued','validating','running'):break
 time.sleep(.25)
(root/'boot-media-iso-stop-result.json').write_text(r.stdout);print(r.stdout,flush=True);assert job['state']=='succeeded',job
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
print(json.dumps({'nativeVMStopped':True,'explicitHardStop':True,'automaticEscalation':False,'installerInteraction':False,'mediaDeleted':False}),flush=True)
