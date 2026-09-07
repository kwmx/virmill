import hashlib,json,os,pathlib,sqlite3,subprocess,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
vm='f78674f3-bf3a-43e5-81f9-4283e2472024'
xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])
assert hashlib.sha256(xml).hexdigest()=='36a51eefeb3f4b48f46f1308668b1e4c3cef3b19c03ba8b9fd7da6b70c61c500'
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
assert ET.fromstring(xml).find('os/nvram').text=='/var/lib/libvirt/qemu/nvram/Virmill UEFI TPM cold probe_VARS.fd'
# Metadata only. Exact path was observed in this new disposable guest's XML.
probe=r'''import json,os,stat
parts=['var','lib','libvirt','qemu','nvram']
fd=os.open('/',os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC)
result=[]
try:
 for part in parts:
  child=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC,dir_fd=fd)
  os.close(fd);fd=child;s=os.fstat(fd)
  result.append(dict(component=part,uid=s.st_uid,gid=s.st_gid,mode=oct(stat.S_IMODE(s.st_mode)),device=s.st_dev,inode=s.st_ino))
 try:os.stat('Virmill UEFI TPM cold probe_VARS.fd',dir_fd=fd,follow_symlinks=False)
 except FileNotFoundError:absent=True
 else:absent=False
 assert absent,'new probe NVRAM directory entry already exists'
 print(json.dumps(dict(ancestorsOpenedWithoutSymlink=True,finalDirectoryEntryAbsent=True,ancestorMetadata=result)))
finally:os.close(fd)
'''
paths=json.loads(subprocess.check_output(['sudo','-n','python3','-c',probe],text=True))
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 locks=list(db.execute('SELECT resource,job_id FROM locks ORDER BY resource'))
assert len(locks)==3 and all(owner=='c809898c-e1be-45e2-b31d-644ad7bc75f0' for _,owner in locks)
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])==xml
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
report={'readOnlyExternalFixtureCheck':True,'vmID':vm,'state':'shut off','xmlSHA256':hashlib.sha256(xml).hexdigest(),'nvramMetadataObservation':paths,'remainingRecoveryLocks':locks,'noAuxiliaryBytesRead':True,'tpmStatePathResolved':False,'durableAuxiliaryBindingVerified':False,'completeCaptureVerified':False}
with (root/'cold-probe-preboot-paths.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
