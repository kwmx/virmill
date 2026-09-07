import hashlib,json,os,pathlib,subprocess,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
vm='f78674f3-bf3a-43e5-81f9-4283e2472024'
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);assert hashlib.sha256(xml).hexdigest()=='081324fb849b275fafacfdf7efc6e22581f1ea926e87b75e80af8f0af688b31c'
nvram=ET.fromstring(xml).find('./os/nvram').text;assert nvram=='/var/lib/libvirt/qemu/nvram/Virmill UEFI TPM cold probe_VARS.fd'
probe="""import json,os,stat
fd=os.open('/',os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC)
try:
 for part in ('var','lib','libvirt','qemu','nvram'):
  child=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC,dir_fd=fd);os.close(fd);fd=child
 s=os.stat('Virmill UEFI TPM cold probe_VARS.fd',dir_fd=fd,follow_symlinks=False)
 assert stat.S_ISREG(s.st_mode) and s.st_nlink==1
 print(json.dumps(dict(ordinarySingleLink=True,size=s.st_size,uid=s.st_uid,gid=s.st_gid,mode=oct(stat.S_IMODE(s.st_mode)),device=s.st_dev,inode=s.st_ino,mtimeNS=s.st_mtime_ns,ctimeNS=s.st_ctime_ns)))
finally:os.close(fd)
"""
metadata=json.loads(subprocess.check_output(['sudo','-n','python3','-c',probe],text=True))
p=subprocess.run(['virmill','vm','recovery','inspect',vm,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=30);assert p.returncode==0,p.stdout+p.stderr
inspection=json.loads(p.stdout)['data'];assert inspection['state']=='stopped'
report={'revision':'ad608076cd928e6c6c270e6442cbc2d8047e6e0c','readOnly':True,'nativeXML':xml.decode(),'nativeXMLSHA256':hashlib.sha256(xml).hexdigest(),'NVRAMMetadataOnly':metadata,'coldInspection':inspection,'noAuxiliaryBytesRead':True,'firstMarkerObserved':False,'completeCaptureVerified':False}
with (root/'cold-probe-after-first-inspect.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
