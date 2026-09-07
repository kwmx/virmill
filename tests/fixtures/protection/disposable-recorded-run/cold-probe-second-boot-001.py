import hashlib,json,os,pathlib,sqlite3,subprocess,time,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
vm='f78674f3-bf3a-43e5-81f9-4283e2472024';observer=root/'sources/uefi-probe-observer-v3/observe_console.py'
assert hashlib.sha256(observer.read_bytes()).hexdigest()=='dc96fbc1ca5dae19be1d1b6f8f9a6a1101ac95d133442e4227d12804a251c5e6'
assert hashlib.sha256(pathlib.Path('/usr/bin/virmill').read_bytes()).hexdigest()=='cb22b33ebb70e28d9c0b5ab95934c021f396051e8d24b4f69eae233d103dded4'
plan=json.loads((root/'cold-probe-start-2-plan.json').read_text())
assert plan['planID']=='087b34ab-9656-4d99-bcbb-071c636e929b' and plan['planDigest']=='693e4983ab0540c478c24a59ccf902c950095a6ea025291ad4b5bc39f83891f5'
assert plan['operation']=='vm.start' and plan['review']['vmID']==vm and plan['acknowledgements']==['host-mutation']
old=json.loads((root/'cold-source-upgrade-44a46b6.json').read_text())['allFourStoppedVMXMLSHA256']
def state(key):return subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',key],text=True).strip()
def xml(key):return subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',key])
def preserve_old():
 for key,sha in old.items():assert state(key)=='shut off' and hashlib.sha256(xml(key)).hexdigest()==sha
preserve_old();assert state(vm)=='shut off'
before_xml=xml(vm);assert hashlib.sha256(before_xml).hexdigest()=='081324fb849b275fafacfdf7efc6e22581f1ea926e87b75e80af8f0af688b31c'
tree=ET.fromstring(before_xml);assert not tree.findall('./devices/interface') and tree.attrib['type']=='kvm'
assert tree.find('./devices/tpm/backend').attrib=={'type':'emulator','version':'2.0','persistent_state':'yes'}
disk='/var/lib/libvirt/images/virmill-cold-probe-v1/virmill-'+vm+'-disk-000.qcow2'
assert subprocess.check_output(['sudo','-n','sha256sum','--',disk],text=True).split()[0]=='f0eafe1814a7a137def96128e8f5bc15838669c868a25aa808f8d084c4382e51'
# Check the same materialized NVRAM metadata through held parents; no auxiliary bytes.
check="""import os,stat
fd=os.open('/',os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC)
try:
 for part in ('var','lib','libvirt','qemu','nvram'):
  child=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC,dir_fd=fd);os.close(fd);fd=child
 s=os.stat('Virmill UEFI TPM cold probe_VARS.fd',dir_fd=fd,follow_symlinks=False)
 assert stat.S_ISREG(s.st_mode) and s.st_nlink==1
 assert (s.st_dev,s.st_ino,s.st_size,s.st_uid,s.st_gid,stat.S_IMODE(s.st_mode),s.st_mtime_ns,s.st_ctime_ns)==(64512,9332206,131072,107,107,0o600,1788812299299985287,1788812300547575588)
finally:os.close(fd)
"""
subprocess.run(['sudo','-n','python3','-c',check],check=True)
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 before_jobs=list(db.execute('SELECT id,body FROM jobs ORDER BY id'));assert len(before_jobs)==27 and not list(db.execute('SELECT resource,job_id FROM locks'))
console=subprocess.Popen(['python3',str(observer),'--uuid',vm,'--uri','qemu:///system','--expect','PRESERVED','--log',str(root/'cold-probe-second-boot-console.raw'),'--timeout','60','--attempts','100'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
try:
 deadline=time.monotonic()+2
 while not (root/'cold-probe-second-boot-console.raw').exists() and time.monotonic()<deadline:time.sleep(.01)
 assert (root/'cold-probe-second-boot-console.raw').exists() and console.poll() is None,'console observer ended before start; no apply submitted'
 p=subprocess.run(['virmill','plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key','cold-probe-second-boot-'+plan['planID'],'--ack','host-mutation','--wait','--timeout','2m','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=150)
 response=json.loads(p.stdout)
 with (root/'cold-probe-second-boot-apply.json').open('x') as f:json.dump(response,f,indent=2)
 print(json.dumps({'startExitCode':p.returncode,'startResponse':response}),flush=True)
 out,err=console.communicate(timeout=65);observation=json.loads(out)
 with (root/'cold-probe-second-boot-console.json').open('x') as f:json.dump(observation,f,indent=2)
 print(json.dumps({'consoleExitCode':console.returncode,'consoleObservation':observation}),flush=True)
 deadline=time.monotonic()+30
 while state(vm)!='shut off' and time.monotonic()<deadline:time.sleep(.1)
 after_state=state(vm);preserve_old()
 after_xml=xml(vm)
 with (root/'cold-probe-after-second-boot.xml').open('xb') as f:f.write(after_xml)
 report={'revision':'ad608076cd928e6c6c270e6442cbc2d8047e6e0c','vmID':vm,'nativeState':after_state,'startExitCode':p.returncode,'startResponse':response,'consoleExitCode':console.returncode,'consoleObservation':observation,'persistentXMLSHA256':hashlib.sha256(after_xml).hexdigest(),'otherFourStoppedGuestXMLPreserved':True,'guestNICs':0,'noAuxiliaryBytesReadByCollector':True,'completeCaptureVerified':False,'independentRecoveryVerified':False,'firstMarkerObserved':False,'expectedThisBoot':'PRESERVED'}
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
  now=dict(db.execute('SELECT id,body FROM jobs ORDER BY id'));assert all(now[key]==body for key,body in before_jobs)
  report['remainingLocks']=list(db.execute('SELECT resource,job_id FROM locks'))
 if after_state=='shut off':report['stoppedProbeDiskSHA256']=subprocess.check_output(['sudo','-n','sha256sum','--',disk],text=True).split()[0]
 with (root/'cold-probe-second-boot.json').open('x') as f:json.dump(report,f,indent=2)
 print(json.dumps(report),flush=True)
 assert p.returncode==0 and response['data']['state']=='succeeded' and after_state=='shut off'
 assert console.returncode==0 and observation['status']=='observed' and observation['result']=='PRESERVED'
finally:
 if console.poll() is None:console.terminate();console.wait(timeout=3)
