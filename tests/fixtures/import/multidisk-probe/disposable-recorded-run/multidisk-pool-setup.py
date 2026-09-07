import hashlib,json,pathlib,shutil,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';name='virmill-multidisk-12d7bba';target=pathlib.Path('/var/lib/libvirt/images')/name
expected={'ae630461-91d3-4f07-ad88-e6842c3dc3ea':'e3200ea179e0cee3258b413baf8668529f2c1fdbed4535115a674d1b4d40d824','a19bf9ee-cd7f-4921-baac-39ce1694eb35':'e31e20ae14df1e2809c242416ff31416249b8c4d3a14dab74507afb02e0250d2','2ec994ce-2950-498c-8b19-d2f7dbb53a78':'bbe62f376a3943d795cdac4fd67ff56087efa43024b27cacf0578217a1a44408'}
for vm,digest in expected.items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()==digest
assert not target.exists() and not target.is_symlink();assert shutil.disk_usage(target.parent).free>20<<30
assert name not in subprocess.check_output(['virsh','--readonly','-c','qemu:///system','pool-list','--all','--name'],text=True).splitlines()
intent={'name':name,'target':str(target),'connection':'qemu:///system','createdBy':'external disposable-test setup, not Virmill storage workflow','autostart':False,'deleteExisting':False,'existingStoppedVMXMLSHA256':expected}
with (root/'multidisk-pool-intent.json').open('x') as f:json.dump(intent,f,indent=2)
subprocess.run(['sudo','-n','install','-d','-o','qemu','-g','qemu','-m','0755',str(target)],check=True)
subprocess.run(['sudo','-n','restorecon',str(target)],check=True)
subprocess.run(['sudo','-n','virsh','-c','qemu:///system','pool-define-as',name,'dir','--target',str(target)],check=True)
subprocess.run(['sudo','-n','virsh','-c','qemu:///system','pool-start',name],check=True)
intent['uuid']=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','pool-uuid',name],text=True).strip()
with (root/'multidisk-pool.json').open('x') as f:json.dump(intent,f,indent=2)
print(json.dumps(intent),flush=True);subprocess.run(['virsh','--readonly','-c','qemu:///system','pool-info',name],check=True)
