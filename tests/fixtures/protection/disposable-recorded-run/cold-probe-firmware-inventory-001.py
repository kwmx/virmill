import hashlib,json,os,pathlib,subprocess,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';vm='f78674f3-bf3a-43e5-81f9-4283e2472024'
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);assert hashlib.sha256(xml).hexdigest()=='081324fb849b275fafacfdf7efc6e22581f1ea926e87b75e80af8f0af688b31c'
files=subprocess.check_output(['rpm','-ql','edk2-ovmf'],text=True).splitlines()
metadata=[]
for name in files:
 p=pathlib.Path(name)
 if str(p.parent)=='/usr/share/qemu/firmware' and p.suffix=='.json' and not p.is_symlink() and p.is_file():
  raw=p.read_bytes();assert len(raw)<=65536;metadata.append({'path':name,'sha256':hashlib.sha256(raw).hexdigest(),'descriptor':json.loads(raw)})
selected={}
for name in ('/usr/share/edk2/ovmf/OVMF_CODE.fd','/usr/share/edk2/ovmf/OVMF_VARS.fd'):
 p=pathlib.Path(name);assert p.is_file() and not p.is_symlink();selected[name]={'bytes':p.stat().st_size,'sha256':hashlib.sha256(p.read_bytes()).hexdigest()}
verify=subprocess.run(['rpm','-V','edk2-ovmf','swtpm'],capture_output=True,text=True)
console=(root/'cold-probe-second-boot-console.raw').read_bytes();assert len(console)==795 and hashlib.sha256(console).hexdigest()=='6aee22d698403d3f1e28c953ddc39b3f32b776eb55d94b901ebbc4831498e1c1'
report={'revision':'ad608076cd928e6c6c270e6442cbc2d8047e6e0c','readOnly':True,'edk2Package':subprocess.check_output(['rpm','-q','edk2-ovmf'],text=True).strip(),'selectedSystemFirmware':selected,'descriptors':metadata,'packageVerificationExitCode':verify.returncode,'packageVerificationOutput':verify.stdout+verify.stderr,'consoleTextEscaped':console.decode('utf-8',errors='replace'),'nativeState':'shut off','nativeXMLSHA256':hashlib.sha256(xml).hexdigest(),'noAuxiliaryBytesRead':True,'noFirmwareSelectionChange':True}
with (root/'cold-probe-firmware-inventory.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report,ensure_ascii=True),flush=True)
