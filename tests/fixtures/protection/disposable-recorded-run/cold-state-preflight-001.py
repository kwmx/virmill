import hashlib,json,os,pathlib,shutil,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
report={'readOnly':True,'packages':{},'tools':{},'guestsStopped':{}}
for package in ('libvirt-libs','qemu-system-x86-core','edk2-ovmf','swtpm','swtpm-tools','clang','lld','gcc','binutils','dosfstools','mtools','python3'):
 p=subprocess.run(['rpm','-q',package],text=True,capture_output=True);report['packages'][package]=p.stdout.strip()
for tool in ('clang','lld-link','ld.lld','gcc','ld','objcopy','mkfs.fat','mcopy','mformat','qemu-img','swtpm'):
 p=shutil.which(tool)
 if p:
  f=pathlib.Path(p).resolve();report['tools'][tool]={'path':str(f),'sha256':hashlib.sha256(f.read_bytes()).hexdigest()}
 else:report['tools'][tool]=None
baseline=json.loads((root/'discovery-upgrade-def0b10.json').read_text())
for vm,sha in baseline['allFourStoppedVMXMLSHA256'].items():
 state=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip();assert state=='shut off'
 xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);assert hashlib.sha256(xml).hexdigest()==sha
 report['guestsStopped'][vm]=True
caps=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domcapabilities','--virttype','kvm','--arch','x86_64','--machine','q35'])
with (root/'cold-state-domcaps-539642a.xml').open('xb') as f:f.write(caps)
report['domcapsSHA256']=hashlib.sha256(caps).hexdigest()
report['SELinux']=subprocess.check_output(['getenforce'],text=True).strip()
with (root/'cold-state-preflight-539642a.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
