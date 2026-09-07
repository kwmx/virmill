import hashlib,json,os,pathlib,sqlite3,subprocess,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
vm='d4c95f21-28bc-428d-9e5f-ceda025d279e'
def state(key):return subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',key],text=True).strip()
def native(key):return subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',key])
old=json.loads((root/'cold-probe-4m-created.json').read_text())['allFiveEarlierStoppedVMXMLPreserved']
for key,digest in old.items():assert state(key)=='shut off' and hashlib.sha256(native(key)).hexdigest()==digest
assert state(vm)=='shut off'
xml=native(vm);assert hashlib.sha256(xml).hexdigest()=='a10ba2cb370cd1740d42e34a944663420e8b716315e47f21d999a5a64249411e'
tree=ET.fromstring(xml);assert not tree.findall('./devices/interface')
nvram=tree.find('./os/nvram');assert nvram.text=='/var/lib/libvirt/qemu/nvram/Virmill UEFI TPM 4M probe_VARS.qcow2'
probe="""import json,os,stat
fd=os.open('/',os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC)
try:
 for part in ('var','lib','libvirt','qemu','nvram'):
  child=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC,dir_fd=fd);os.close(fd);fd=child
 s=os.stat('Virmill UEFI TPM 4M probe_VARS.qcow2',dir_fd=fd,follow_symlinks=False)
 assert stat.S_ISREG(s.st_mode) and s.st_nlink==1
 print(json.dumps(dict(ordinarySingleLink=True,size=s.st_size,uid=s.st_uid,gid=s.st_gid,mode=oct(stat.S_IMODE(s.st_mode)),device=s.st_dev,inode=s.st_ino,mtimeNS=s.st_mtime_ns,ctimeNS=s.st_ctime_ns)))
finally:os.close(fd)
"""
metadata=json.loads(subprocess.check_output(['sudo','-n','python3','-c',probe],text=True))
firmware={}
for name in ('OVMF_CODE_4M.qcow2','OVMF_VARS_4M.qcow2'):
 p=pathlib.Path('/usr/share/edk2/ovmf')/name;data=p.read_bytes();firmware[str(p)]={'bytes':len(data),'sha256':hashlib.sha256(data).hexdigest()}
public=(root/'cold-probe-4m-boot-console.raw').read_bytes();assert len(public)==775 and hashlib.sha256(public).hexdigest()=='147f94086027b300d8d203cf641b89ef7f3c204d4cc3783fff31f1492de4ced9'
inspection=subprocess.run(['virmill','vm','recovery','inspect',vm,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=30);assert inspection.returncode==0,inspection.stdout+inspection.stderr
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 jobs=list(db.execute('SELECT id,body FROM jobs ORDER BY id'));assert len(jobs)==30 and not list(db.execute('SELECT resource,job_id FROM locks'))
p=subprocess.run(['virmill','vm','start',vm,'--plan','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=30);assert p.returncode==0,p.stdout+p.stderr
plan=json.loads(p.stdout)['data'];assert plan['operation']=='vm.start' and plan['review']['vmID']==vm and plan['acknowledgements']==['host-mutation']
with (root/'cold-probe-4m-start-2-plan.json').open('x') as f:json.dump(plan,f,indent=2)
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:assert list(db.execute('SELECT id,body FROM jobs ORDER BY id'))==jobs
assert state(vm)=='shut off' and native(vm)==xml
for key,digest in old.items():assert state(key)=='shut off' and hashlib.sha256(native(key)).hexdigest()==digest
report={'revision':'ad608076cd928e6c6c270e6442cbc2d8047e6e0c','previewOnly':True,'firstMarkerObserved':'SEEDED','nativeXML':xml.decode(),'NVRAMMetadataOnly':metadata,'systemFirmwareFiles':firmware,'publicFirstConsoleText':public.decode('ascii'),'coldInspection':json.loads(inspection.stdout)['data'],'startPlan':plan,'noAuxiliaryBytesReadByCollector':True,'completeCaptureVerified':False,'independentRecoveryVerified':False}
with (root/'cold-probe-4m-second-plan.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
