import hashlib,json,os,pathlib,sqlite3,subprocess,time,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
vm='f78674f3-bf3a-43e5-81f9-4283e2472024';observer=root/'sources/uefi-probe-observer-v2/observe_console.py'
assert hashlib.sha256(observer.read_bytes()).hexdigest()=='4d2232a9570e5089fb5704db1f7f398ec3702a48027526144f5978603a415261'
assert hashlib.sha256(pathlib.Path('/usr/bin/virmill').read_bytes()).hexdigest()=='cb22b33ebb70e28d9c0b5ab95934c021f396051e8d24b4f69eae233d103dded4'
plan=json.loads((root/'cold-probe-start-1-plan.json').read_text())
assert plan['planID']=='3cf9b71e-7fc7-433d-b900-aafcd7e6c00e' and plan['planDigest']=='f3b7f629684604c39ab004a0480d127ce3654920a46af3e102dd6c6a27144e7f'
assert plan['operation']=='vm.start' and plan['review']['vmID']==vm and plan['acknowledgements']==['host-mutation']
old=json.loads((root/'cold-source-upgrade-44a46b6.json').read_text())['allFourStoppedVMXMLSHA256']
def state(key):return subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',key],text=True).strip()
def xml(key):return subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',key])
def preserve_old():
 for key,sha in old.items():assert state(key)=='shut off' and hashlib.sha256(xml(key)).hexdigest()==sha
preserve_old();assert state(vm)=='shut off'
before_xml=xml(vm);assert hashlib.sha256(before_xml).hexdigest()=='36a51eefeb3f4b48f46f1308668b1e4c3cef3b19c03ba8b9fd7da6b70c61c500'
tree=ET.fromstring(before_xml);assert not tree.findall('./devices/interface') and tree.attrib['type']=='kvm'
assert tree.find('./devices/tpm/backend').attrib=={'type':'emulator','version':'2.0','persistent_state':'yes'}
disk='/var/lib/libvirt/images/virmill-cold-probe-v1/virmill-'+vm+'-disk-000.qcow2'
assert subprocess.check_output(['sudo','-n','sha256sum','--',disk],text=True).split()[0]=='f0eafe1814a7a137def96128e8f5bc15838669c868a25aa808f8d084c4382e51'
# Point-in-time absence check through held no-symlink parents. No auxiliary bytes.
check="""import os
fd=os.open('/',os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC)
try:
 for part in ('var','lib','libvirt','qemu','nvram'):
  child=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC,dir_fd=fd);os.close(fd);fd=child
 try:os.stat('Virmill UEFI TPM cold probe_VARS.fd',dir_fd=fd,follow_symlinks=False)
 except FileNotFoundError:pass
 else:raise SystemExit('probe NVRAM entry unexpectedly exists before first boot')
finally:os.close(fd)
"""
subprocess.run(['sudo','-n','python3','-c',check],check=True)
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 before_jobs=list(db.execute('SELECT id,body FROM jobs ORDER BY id'));assert len(before_jobs)==26 and not list(db.execute('SELECT resource,job_id FROM locks'))
console=subprocess.Popen(['python3',str(observer),'--uuid',vm,'--uri','qemu:///system','--expect','SEEDED','--log',str(root/'cold-probe-first-boot-console.raw'),'--timeout','60','--attempts','100'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
try:
 deadline=time.monotonic()+2
 while not (root/'cold-probe-first-boot-console.raw').exists() and time.monotonic()<deadline:time.sleep(.01)
 assert (root/'cold-probe-first-boot-console.raw').exists() and console.poll() is None,'console observer ended before start; no apply submitted'
 p=subprocess.run(['virmill','plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key','cold-probe-first-boot-'+plan['planID'],'--ack','host-mutation','--wait','--timeout','2m','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=150)
 response=json.loads(p.stdout)
 with (root/'cold-probe-first-boot-apply.json').open('x') as f:json.dump(response,f,indent=2)
 print(json.dumps({'startExitCode':p.returncode,'startResponse':response}),flush=True)
 out,err=console.communicate(timeout=65);observation=json.loads(out)
 with (root/'cold-probe-first-boot-console.json').open('x') as f:json.dump(observation,f,indent=2)
 print(json.dumps({'consoleExitCode':console.returncode,'consoleObservation':observation}),flush=True)
 deadline=time.monotonic()+30
 while state(vm)!='shut off' and time.monotonic()<deadline:time.sleep(.1)
 after_state=state(vm);preserve_old()
 after_xml=xml(vm)
 with (root/'cold-probe-after-first-boot.xml').open('xb') as f:f.write(after_xml)
 report={'revision':'ad608076cd928e6c6c270e6442cbc2d8047e6e0c','vmID':vm,'nativeState':after_state,'startExitCode':p.returncode,'startResponse':response,'consoleExitCode':console.returncode,'consoleObservation':observation,'persistentXMLSHA256':hashlib.sha256(after_xml).hexdigest(),'otherFourStoppedGuestXMLPreserved':True,'guestNICs':0,'noAuxiliaryBytesReadByCollector':True,'completeCaptureVerified':False,'independentRecoveryVerified':False}
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
  now=dict(db.execute('SELECT id,body FROM jobs ORDER BY id'));assert all(now[key]==body for key,body in before_jobs)
  report['remainingLocks']=list(db.execute('SELECT resource,job_id FROM locks'))
 if after_state=='shut off':report['stoppedProbeDiskSHA256']=subprocess.check_output(['sudo','-n','sha256sum','--',disk],text=True).split()[0]
 with (root/'cold-probe-first-boot.json').open('x') as f:json.dump(report,f,indent=2)
 print(json.dumps(report),flush=True)
 assert p.returncode==0 and response['data']['state']=='succeeded' and after_state=='shut off'
 assert console.returncode==0 and observation['status']=='observed' and observation['result']=='SEEDED'
finally:
 if console.poll() is None:console.terminate();console.wait(timeout=3)
