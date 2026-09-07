import json,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
name='virmill-policy-c7f8b76'
target='/var/lib/libvirt/images/'+name
assert not pathlib.Path(target).exists()
intent={'name':name,'target':target,'connection':'qemu:///system','createdBy':'external disposable-test setup, not Virmill storage workflow','autostart':False,'deleteExisting':False}
(root/'device-policy-pool-intent.json').write_text(json.dumps(intent,indent=2)+'\n')
subprocess.run(['sudo','-n','install','-d','-o','qemu','-g','qemu','-m','0755',target],check=True)
subprocess.run(['sudo','-n','restorecon',target],check=True)
subprocess.run(['sudo','-n','virsh','-c','qemu:///system','pool-define-as',name,'dir','--target',target],check=True)
subprocess.run(['sudo','-n','virsh','-c','qemu:///system','pool-start',name],check=True)
uid=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','pool-uuid',name],text=True).strip()
intent['uuid']=uid;(root/'device-policy-pool.json').write_text(json.dumps(intent,indent=2)+'\n')
print(json.dumps(intent,indent=2))
subprocess.run(['virsh','-c','qemu:///system','uri'],check=True)
subprocess.run(['virsh','--readonly','-c','qemu:///system','pool-info',name],check=True)
