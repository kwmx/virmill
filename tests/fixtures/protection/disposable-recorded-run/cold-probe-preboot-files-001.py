import hashlib,json,os,pathlib,stat,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
vm='f78674f3-bf3a-43e5-81f9-4283e2472024'
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
# This exact path belongs to the newly generated public probe, never user media.
disk='/var/lib/libvirt/images/virmill-cold-probe-v1/virmill-'+vm+'-disk-000.qcow2'
expected='f0eafe1814a7a137def96128e8f5bc15838669c868a25aa808f8d084c4382e51'
sha=subprocess.check_output(['sudo','-n','sha256sum','--',disk],text=True).split()[0];assert sha==expected
nvram='/var/lib/libvirt/qemu/nvram/Virmill UEFI TPM cold probe_VARS.fd'
# Explicit path is from captured native XML. Existence metadata only, no NVRAM bytes.
check=subprocess.run(['sudo','-n','test','-e',nvram]);assert check.returncode in (0,1)
report={'readOnlyExternalFixtureCheck':True,'vmID':vm,'state':'shut off','probeDiskSHA256':sha,'probeDiskMatchesPreparedArtifact':True,'nvramPathFromNativeXML':nvram,'nvramExistsBeforeFirstStart':check.returncode==0,'tpmStatePathResolved':False,'noAuxiliaryBytesRead':True,'completeCaptureVerified':False}
with (root/'cold-probe-preboot-files.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
