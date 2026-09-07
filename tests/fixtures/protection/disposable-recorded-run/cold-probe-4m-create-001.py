import hashlib,json,os,pathlib,sqlite3,subprocess,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
plan=json.loads((root/'cold-probe-4m-creation-plan.json').read_text());vm='d4c95f21-28bc-428d-9e5f-ceda025d279e'
assert plan['planID']=='4d5cde95-4d3f-481e-8887-e0718f5ca534' and plan['planDigest']=='f4a24fc7f92981d9a9ac57d6785686469bf86ce3a47270d33ecbb78001a48ded'
spec=plan['review']['target']['spec'];assert spec['uuid']==vm and spec['name']=='Virmill UEFI TPM 4M probe' and spec['nics']==[]
assert spec['firmware']=={'mode':'uefi','code':'/usr/share/edk2/ovmf/OVMF_CODE_4M.qcow2','template':'/usr/share/edk2/ovmf/OVMF_VARS_4M.qcow2','format':'qcow2','secureBoot':False,'tpm':True}
assert plan['acknowledgements']==['host-mutation','copy-managed-volumes','new-vm-identity','new-firmware-state','creation-device-policy']
assert len(plan['review']['volumes'])==1 and plan['review']['volumes'][0]['sha256']=='f0eafe1814a7a137def96128e8f5bc15838669c868a25aa808f8d084c4382e51'
before={}
for key in subprocess.check_output(['virsh','--readonly','-c','qemu:///system','list','--all','--uuid'],text=True).split():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',key],text=True).strip()=='shut off'
 before[key]=hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',key])).hexdigest()
assert len(before)==5 and vm not in before
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 jobs=dict(db.execute('SELECT id,body FROM jobs ORDER BY id'));assert len(jobs)==28 and not list(db.execute('SELECT resource,job_id FROM locks'))
def call(*args):
 p=subprocess.run(['virmill',*args,'--output','json','--non-interactive','--timeout','2m'],env=env,capture_output=True,text=True,timeout=150);return p,json.loads(p.stdout)
args=[]
for ack in plan['acknowledgements']:args+=['--ack',ack]
p,response=call('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key','cold-probe-4m-create-'+plan['planID'],*args,'--wait')
with (root/'cold-probe-4m-create-response.json').open('x') as f:json.dump(response,f,indent=2)
print(json.dumps({'revision':'ad608076cd928e6c6c270e6442cbc2d8047e6e0c','exitCode':p.returncode,'response':response}),flush=True)
p.check_returncode();assert response['data']['state']=='succeeded'
p,result=call('vm','creation','result',response['data']['operationID']);p.check_returncode();assert result['data']['complete'] and result['data']['receipt']['defined']
assert all(result['data'][k] is False for k in ('guestBootVerified','setupVerified','connectivityVerified'))
xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);tree=ET.fromstring(xml)
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
assert not tree.findall('./devices/interface') and tree.find('./os/loader').text==spec['firmware']['code'] and tree.find('./os/nvram').attrib['format']=='qcow2'
disk='/var/lib/libvirt/images/virmill-cold-probe-v1/virmill-'+vm+'-disk-000.qcow2';assert subprocess.check_output(['sudo','-n','sha256sum','--',disk],text=True).split()[0]==plan['review']['volumes'][0]['sha256']
for key,sha in before.items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',key],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',key])).hexdigest()==sha
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 now=dict(db.execute('SELECT id,body FROM jobs ORDER BY id'));assert all(now[key]==body for key,body in jobs.items()) and not list(db.execute('SELECT resource,job_id FROM locks'))
with (root/'cold-probe-4m-created.xml').open('xb') as f:f.write(xml)
report={'revision':'ad608076cd928e6c6c270e6442cbc2d8047e6e0c','creation':result['data'],'nativeXML':xml.decode(),'nativeXMLSHA256':hashlib.sha256(xml).hexdigest(),'allFiveEarlierStoppedVMXMLPreserved':before,'previous28JobsPreserved':True,'newVMStopped':True,'noGuestStart':True,'remainingLocks':0,'nativeTPMProtocolVerified':False}
with (root/'cold-probe-4m-created.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
p,start=call('vm','start',vm,'--plan');p.check_returncode()
with (root/'cold-probe-4m-start-plan.json').open('x') as f:json.dump(start['data'],f,indent=2)
print(json.dumps({'startPlan':start['data']}),flush=True)
