import hashlib,json,pathlib,shutil,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
print(json.dumps({'freeBytes':shutil.disk_usage(root).free,'tools':{name:shutil.which(name) for name in ('as','objcopy','nasm','qemu-img','bwrap')}}))
for args in (['rpm','-q','binutils','nasm','qemu-img','bubblewrap'],['systemctl','--user','is-active','virmill-test-12d7bba.service']):
 r=subprocess.run(args,capture_output=True,text=True);print(json.dumps({'command':args,'exitCode':r.returncode,'output':r.stdout+r.stderr}))
for id in ('ae630461-91d3-4f07-ad88-e6842c3dc3ea','a19bf9ee-cd7f-4921-baac-39ce1694eb35','2ec994ce-2950-498c-8b19-d2f7dbb53a78'):
 print(json.dumps({'vmID':id,'state':subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip(),'xmlSHA256':hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id])).hexdigest()}))
