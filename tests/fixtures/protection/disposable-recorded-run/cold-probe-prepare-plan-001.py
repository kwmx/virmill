import hashlib,json,os,pathlib,sqlite3,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text())}
source=root/'sources/uefi-tpm-probe-v1'
assert hashlib.sha256(pathlib.Path('/usr/bin/virmill').read_bytes()).hexdigest()=='dc1aded072ae761798a2a8ee89fb14a19a8552da77a7c7b24ed6522b758cc217'
assert hashlib.sha256((source/'build-manifest.json').read_bytes()).hexdigest()=='275abd707259b46a31d24fd7e5614b5e655319b7f6815e7a45a240b99a0f82ac'
manifest=json.loads((source/'build-manifest.json').read_text())
image=source/'probe-fat.img'
assert image.is_file() and image.stat().st_size==33554432
digest=hashlib.sha256(image.read_bytes()).hexdigest()
# Filled with the parent-reviewed executable/image hashes before execution.
assert digest=='1945000ea87359d92f6fcb3413549fe477884012d631c66a058c71a1dda9e483'
assert hashlib.sha256((source/'BOOTX64.EFI').read_bytes()).hexdigest()=='b50a5ef0a95feb2ee6824b1d5f353b41a4b2ed189c6974c76293592fa3109b33'
assert not (root/'prepared/uefi-tpm-probe-v1').exists()
pool=json.loads((root/'cold-probe-pool.json').read_text())
assert pool['uuid']=='95b94843-0db0-46ac-9bbf-a2fa3c183818'
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
    assert not list(db.execute('SELECT resource,job_id FROM locks'))
def cli(*args):
    p=subprocess.run(['virmill',*args,'--connection','qemu:///system','--timeout','5m','--output','json','--non-interactive'],env=env,text=True,capture_output=True,timeout=330)
    if p.returncode:print(p.stdout+p.stderr,flush=True)
    p.check_returncode();return json.loads(p.stdout)['data']
def save(name,value):
    with (root/name).open('x') as f:json.dump(value,f,indent=2)
request={'offlineSources':True,'destination':str(root/'prepared/uefi-tpm-probe-v1'),'files':[{'path':'probe-fat.img','sha256':digest}],'disks':[{'id':'boot','path':'probe-fat.img','format':'raw','maximumVirtualBytes':33554432}]}
plan=cli('import','prepare-disks',str(source),'--input',json.dumps(request),'--plan')
save('cold-probe-preparation-plan.json',plan)
assert set(plan['acknowledgements'])=={'write-import-artifacts','offline-source-files'}
args=[]
for ack in plan['acknowledgements']:args+=['--ack',ack]
job=cli('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key','cold-probe-prepare-'+plan['planID'],*args,'--wait')
save('cold-probe-preparation-job.json',job)
assert job['state']=='succeeded',job
result=cli('import','result',job['operationID']);save('cold-probe-preparation-result.json',result)
assert hashlib.sha256(image.read_bytes()).hexdigest()==digest
print(json.dumps({'prepared':job,'sourceSHA256':digest,'sourcePreserved':True}),flush=True)
request={'identityMode':'clone','hardware':{'name':'Virmill UEFI TPM cold probe','poolID':pool['uuid'],'architecture':'x86_64','machine':'pc-q35-10.2','vcpus':1,'memoryMiB':512,'cpu':{'mode':'host-passthrough'},'firmware':{'mode':'uefi','code':'/usr/share/edk2/ovmf/OVMF_CODE.fd','template':'/usr/share/edk2/ovmf/OVMF_VARS.fd','format':'raw','secureBoot':False,'tpm':True},'clock':'utc','graphics':'vnc-unix','disks':[{'sourceID':'boot','bus':'sata','bootOrder':1}],'nics':[],'devicePolicy':{'version':1,'chipset':'q35','pciPlacement':'libvirt-auto','usbController':'none','memoryBalloon':'none','watchdogAction':'none','input':'ps2','audio':'none','serial':'isa-serial'}}}
save('cold-probe-creation-input.json',request)
plan=cli('vm','create',job['operationID'],'--input',json.dumps(request),'--plan')
save('cold-probe-creation-plan.json',plan)
print(json.dumps({'creationPlan':plan}),flush=True)
