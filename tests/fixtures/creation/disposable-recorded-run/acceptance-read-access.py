import json,os,pathlib,subprocess
p=pathlib.Path('/var/lib/libvirt/images/virmill-qualification-65930c6/virmill-ae630461-91d3-4f07-ad88-e6842c3dc3ea-disk-000.qcow2')
st=p.stat()
print(json.dumps({'retainedDiskReadable':os.access(p,os.R_OK),'fileMode':oct(st.st_mode & 0o777),'fileUID':st.st_uid,'fileGID':st.st_gid,'actorUID':os.getuid(),'actorGroups':os.getgroups(),'bytes':st.st_size}))
print(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate','ae630461-91d3-4f07-ad88-e6842c3dc3ea'],text=True))
print(subprocess.run(['systemctl','--user','is-active','virmill-test-11a8174.service'],capture_output=True,text=True).stdout)
