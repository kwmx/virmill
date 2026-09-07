import hashlib,json,os,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text())}
plan=json.loads((root/'cold-probe-creation-plan.json').read_text())
assert plan['planID']=='a7232967-796b-4526-9423-e51a9b4bf9d2'
assert plan['planDigest']=='14ecac25ba06dfdc36ae7f2d75615fb9caf76982cedbf2e4e6dec70492630be1'
assert plan['operation']=='vm.create.devices-v1' and plan['review']['startsVM'] is False
spec=plan['review']['target']['spec'];vm=spec['uuid']
assert vm=='f78674f3-bf3a-43e5-81f9-4283e2472024'
assert spec['poolID']=='95b94843-0db0-46ac-9bbf-a2fa3c183818' and spec['nics']==[]
assert spec['firmware']['tpm'] and spec['firmware']['mode']=='uefi' and not spec['firmware']['secureBoot']
assert set(plan['acknowledgements'])=={'host-mutation','copy-managed-volumes','new-vm-identity','new-firmware-state','creation-device-policy'}
assert hashlib.sha256(pathlib.Path('/usr/bin/virmill').read_bytes()).hexdigest()=='dc1aded072ae761798a2a8ee89fb14a19a8552da77a7c7b24ed6522b758cc217'
def save(name,value):
 with (root/name).open('x') as f:json.dump(value,f,indent=2)
def call(*args):
 p=subprocess.run(['virmill',*args,'--output','json','--non-interactive','--timeout','5m'],env=env,capture_output=True,text=True,timeout=330)
 return p,json.loads(p.stdout)
args=[]
for ack in plan['acknowledgements']:args+=['--ack',ack]
p,result=call('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key','cold-probe-create-'+plan['planID'],*args,'--wait')
save('cold-probe-creation-response.json',result)
print(json.dumps({'exitCode':p.returncode,'creationResponse':result}),flush=True)
p.check_returncode()
job=result['data'];assert job['state']=='succeeded',job
p,result=call('vm','creation','result',job['operationID']);save('cold-probe-created-result.json',result);p.check_returncode()
state=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip();assert state=='shut off'
xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])
with (root/'cold-probe-created.xml').open('xb') as f:f.write(xml)
p,inspection=call('vm','recovery','inspect',vm);save('cold-probe-initial-inspection.json',inspection)
print(json.dumps({'vmID':vm,'creationResult':result,'state':state,'xmlSHA256':hashlib.sha256(xml).hexdigest(),'inspectionExitCode':p.returncode,'inspection':inspection}),flush=True)
p.check_returncode()
