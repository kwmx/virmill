import hashlib,json,os,pathlib,sqlite3,subprocess,time,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
plan=json.loads((root/'multidisk-fresh-plan.json').read_text())['data'];spec=plan['review']['target']['spec']
assert plan['planID']=='ced514d2-b970-47cd-a008-116466090195' and plan['planDigest']=='9c38227a95f86dfe8c6daa9feabe49e8541e553d264e8beb6de9c77eda73fc1a'
assert spec['uuid']=='b9496482-2eeb-40e1-892b-4e291c108c52' and spec['name']=='Virmill BIOS multi-disk probe' and spec['nics']==[] and spec['firmware']=={'mode':'bios','secureBoot':False,'tpm':False}
assert spec['disks']==[{'sourceID':'boot','bus':'virtio','bootOrder':1},{'sourceID':'data','bus':'virtio','bootOrder':2}]
assert spec['devicePolicy']=={'version':1,'chipset':'q35','pciPlacement':'libvirt-auto','usbController':'none','memoryBalloon':'none','watchdogAction':'none','input':'ps2','audio':'none','serial':'isa-serial'}
assert set(plan['acknowledgements'])=={'host-mutation','copy-managed-volumes','new-vm-identity','creation-device-policy'}
expected=['fe1356a1e613740dc51fb799fc1f559a1f4e311bbcae6b15424c2bd01ec6dfd3','35f221cffb8db678c4a657aeb94429efe5cd580022387d339d2cfee055aec8e7'];assert [v['sha256'] for v in plan['review']['volumes']]==expected
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 assert not list(db.execute('SELECT resource,job_id FROM locks'))
 assert not list(db.execute('SELECT id FROM jobs WHERE plan_id=?',(plan['planID'],)))
def cli(*args):
 r=subprocess.run(['virmill',*args,'--connection','qemu:///system','--timeout','10m','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=660);assert r.returncode==0,r.stdout+r.stderr;return json.loads(r.stdout)['data']
acks=[]
for ack in plan['acknowledgements']:acks.extend(['--ack',ack])
job=cli('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key','multidisk-fresh-'+plan['planID'],*acks)
with (root/'multidisk-fresh-accepted.json').open('x') as f:json.dump(job,f,indent=2)
print(json.dumps({'newCreationAccepted':job}),flush=True);last=None;deadline=time.monotonic()+1200
while time.monotonic()<deadline:
 job=cli('operation','show',job['operationID'])
 if job['state']!=last:print(json.dumps({'operationID':job['operationID'],'state':job['state']}),flush=True);last=job['state']
 if job['state'] in ('succeeded','failed','canceled','partial','recovery-required'):break
 time.sleep(.25)
assert job['state']=='succeeded',job
result=cli('vm','creation','result',job['operationID']);assert result['complete'] and result['receipt']['defined'] and result['receipt']['volumesVerified'] and all(v['verified'] for v in result['receipt']['volumes'])
assert not any(result[k] for k in ('guestBootVerified','setupVerified','connectivityVerified'))
with (root/'multidisk-fresh-result.json').open('x') as f:json.dump(result,f,indent=2)
xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',spec['uuid']]);tree=ET.fromstring(xml)
assert tree.attrib['type']=='kvm' and not tree.findall('./devices/interface') and tree.find('./os/loader') is None and not tree.findall('./devices/tpm')
disks=tree.findall('./devices/disk');assert len(disks)==2
assert [(d.find('target').get('dev'),d.find('target').get('bus'),d.find('boot').get('order')) for d in disks]==[('vda','virtio','1'),('vdb','virtio','2')]
for i,v in enumerate(result['receipt']['volumes']):assert subprocess.check_output(['sudo','-n','sha256sum',v['allocated']['path']],text=True).split()[0]==expected[i]
assert cli('vm','show',spec['uuid'])['ownership']=='managed'
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',spec['uuid']],text=True).strip()=='shut off'
(root/'multidisk-fresh-defined.xml').write_bytes(xml)
print(json.dumps({'nativeFreshCreation':'passed','result':result,'definedXMLSHA256':hashlib.sha256(xml).hexdigest(),'orderedDisks':['vda:boot:1','vdb:data:2'],'guestStarted':False}),flush=True)
start=cli('vm','start',spec['uuid'],'--plan')
with (root/'multidisk-start-plan.json').open('x') as f:json.dump(start,f,indent=2)
print(json.dumps({'startPlan':start}),flush=True)
