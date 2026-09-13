import hashlib,json,os,pathlib,sqlite3,subprocess,time,signal
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
jobid='b5983e68-bfcc-42f0-bf4e-3877029b756b';vmid='ae630461-91d3-4f07-ad88-e6842c3dc3ea'
volume='/var/lib/libvirt/images/virmill-qualification-65930c6/virmill-'+vmid+'-disk-000.qcow2'
def call(args,check=True):return subprocess.run(args,env=(env if args[0]=='virmill' else os.environ),capture_output=True,text=True,check=check,timeout=120)
def job():return json.loads(call(['virmill','operation','show',jobid,'--output','json','--non-interactive']).stdout)['data']
def locks():
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
  return list(db.execute('SELECT resource,job_id FROM locks WHERE job_id=? ORDER BY resource',(jobid,)))
def xml():return call(['virsh','--readonly','--connect','qemu:///system','dumpxml','--inactive',vmid]).stdout
before_job=job();assert before_job['state']=='recovery-required'
before_locks=locks();assert len(before_locks)==3
before_xml=xml();before_stat=call(['stat','--format=%d:%i:%s:%Y:%Z',volume]).stdout
pid=int(call(['systemctl','--user','show','<test-vm-login>-c9aa310.service','--property=MainPID','--value']).stdout)
assert pid>1
pidfd=os.pidfd_open(pid)
try:
 assert pathlib.Path('/proc/'+str(pid)).stat().st_uid==os.getuid()
 assert os.readlink('/proc/'+str(pid)+'/exe')=='/usr/bin/virmilld'
 signal.pidfd_send_signal(pidfd,signal.SIGKILL)
finally:os.close(pidfd)
for _ in range(100):
 if not pathlib.Path('/proc/'+str(pid)).exists():break
 time.sleep(0.05)
assert not pathlib.Path('/proc/'+str(pid)).exists()
call(['systemctl','--user','restart','<test-vm-login>-c9aa310.service'])
for _ in range(100):
 r=call(['virmill','operation','show',jobid,'--output','json','--non-interactive'],check=False)
 if r.returncode==0:break
 time.sleep(0.05)
assert r.returncode==0
assert json.loads(r.stdout)['data']['state']=='recovery-required'
r=call(['virmill','operation','reconcile',jobid,'--output','json','--non-interactive'],check=False)
(root/'creation-reconcile.json').write_text(r.stdout)
assert job()['state']=='recovery-required'
assert locks()==before_locks
assert xml()==before_xml
assert call(['stat','--format=%d:%i:%s:%Y:%Z',volume]).stdout==before_stat
assert call(['virsh','--readonly','--connect','qemu:///system','domstate',vmid]).stdout.strip()=='shut off'
digest=call(['sudo','-n','sha256sum',volume]).stdout.split()[0]
assert digest=='8eb9be4a9592ad8242fa4e99fbac2dd2eef474bb93e99f3699d17617c7d93c70'
result={'test':'already-uncertain native creation across coordinator SIGKILL and explicit reconcile','installedRevision':'c9aa310693c337a8908787f8b616262b3fa53faf','operationID':jobid,'stateAfterRestartAndReconcile':'recovery-required','heldResourceLocks':len(before_locks),'reconcileExitCode':r.returncode,'nativeDefinitionUnchanged':True,'nativeVolumeIdentitySizeTimestampsUnchanged':True,'nativeVolumeSHA256':digest,'vmState':'shut off','allocationOrUploadReplayed':False,'limitation':'Crash occurred after creation was already uncertain, not during active upload or definition. Full crash-boundary qualification remains required.'}
(root/'uncertain-recovery.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result,indent=2))
