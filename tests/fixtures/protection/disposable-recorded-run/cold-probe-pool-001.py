import hashlib,json,pathlib,shutil,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
name='virmill-cold-probe-v1';target=pathlib.Path('/var/lib/libvirt/images')/name
baseline=json.loads((root/'discovery-upgrade-def0b10.json').read_text())['allFourStoppedVMXMLSHA256']
for vm,digest in baseline.items():
    assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
    assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()==digest
assert not target.exists() and not target.is_symlink()
assert shutil.disk_usage(target.parent).free>5<<30
assert name not in subprocess.check_output(['virsh','--readonly','-c','qemu:///system','pool-list','--all','--name'],text=True).splitlines()
intent={'name':name,'target':str(target),'connection':'qemu:///system','createdBy':'external disposable fixture setup; not Virmill storage workflow','autostart':False,'preservedGuestXML':baseline,'noGuestStart':True}
with (root/'cold-probe-pool-intent.json').open('x') as f:json.dump(intent,f,indent=2)
subprocess.run(['sudo','-n','install','-d','-o','qemu','-g','qemu','-m','0755',str(target)],check=True)
subprocess.run(['sudo','-n','restorecon',str(target)],check=True)
subprocess.run(['sudo','-n','virsh','-c','qemu:///system','pool-define-as',name,'dir','--target',str(target)],check=True)
subprocess.run(['sudo','-n','virsh','-c','qemu:///system','pool-start',name],check=True)
intent['uuid']=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','pool-uuid',name],text=True).strip()
intent['poolXMLSHA256']=hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','pool-dumpxml',name])).hexdigest()
with (root/'cold-probe-pool.json').open('x') as f:json.dump(intent,f,indent=2)
print(json.dumps(intent),flush=True)
subprocess.run(['virsh','--readonly','-c','qemu:///system','pool-info',name],check=True)
