import json,os,pathlib,subprocess,time,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};vm='a19bf9ee-cd7f-4921-baac-39ce1694eb35'
def cli(*args):return subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True,timeout=60)
x=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);assert x==(root/'boot-order-applied.xml').read_bytes();t=ET.fromstring(x)
assert t.findall('devices/interface')==[] and t.find("devices/disk[@device='cdrom']/boot").attrib['order']=='1'
r=cli('vm','start',vm,'--plan');(root/'boot-media-iso-start-plan.json').write_text(r.stdout);print(r.stdout,flush=True);p=json.loads(r.stdout)['data']
assert p['operation']=='vm.start' and p['resourceIDs']==['libvirt|qemu:///system|vm|'+vm] and p['acknowledgements']==['host-mutation']
r=cli('plan','apply',p['planID'],'--digest',p['planDigest'],'--idempotency-key','boot-media-iso-start-8a092b4-001','--ack','host-mutation','--detach');(root/'boot-media-iso-start-accepted.json').write_text(r.stdout);print(r.stdout,flush=True);jid=json.loads(r.stdout)['data']['operationID']
for _ in range(60):
 r=cli('operation','show',jid);job=json.loads(r.stdout)['data']
 if job['state'] not in ('queued','validating','running'):break
 time.sleep(.25)
(root/'boot-media-iso-start-result.json').write_text(r.stdout);print(r.stdout,flush=True);assert job['state']=='succeeded',job
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='running'
print(json.dumps({'nativeVMRunning':True,'expectedFirstDevice':'CD-ROM sda','actualScreenReview':'pending','installerInteraction':False}),flush=True)
