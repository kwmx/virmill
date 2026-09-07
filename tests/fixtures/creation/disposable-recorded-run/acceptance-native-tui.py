import datetime,errno,fcntl,hashlib,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'}
vm='ae630461-91d3-4f07-ad88-e6842c3dc3ea';parent='b5983e68-bfcc-42f0-bf4e-3877029b756b'
disk=pathlib.Path('/var/lib/libvirt/images/virmill-qualification-65930c6/virmill-ae630461-91d3-4f07-ad88-e6842c3dc3ea-disk-000.qcow2')
aclfile=root/'acceptance-original-volume-12d7bba.acl'
policy={'devicePolicy':{'version':1,'chipset':'q35','pciPlacement':'libvirt-auto','usbController':'qemu-xhci','memoryBalloon':'virtio','watchdogAction':'reset','input':'ps2','audio':'none','serial':'isa-serial'}}
def cli(*args,check=True):
 return subprocess.run(['virmill',*args,'--connection','qemu:///system','--timeout','10m','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=check,timeout=660)
def dbrows(sql):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(sql))
def dump(id):return subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id])
version=json.loads(cli('version').stdout)['data'];assert version['revision'].startswith('12d7bba')
jobs_before=dbrows('SELECT id,body FROM jobs ORDER BY id');locks_before=dbrows('SELECT resource,job_id FROM locks ORDER BY resource')
assert len(locks_before)==3 and all(job==parent for _,job in locks_before)
old_parent=json.loads(next(body for id,body in jobs_before if id==parent));assert old_parent['state']=='recovery-required'
plan_id=old_parent['planID']
original_plan=dbrows("SELECT body,input FROM plans WHERE id='"+plan_id+"'")
original_receipt=dbrows("SELECT body FROM metadata WHERE kind='vm-creation' AND id='"+plan_id+"'")
xml_before={id:dump(id) for id in (vm,'a19bf9ee-cd7f-4921-baac-39ce1694eb35','2ec994ce-2950-498c-8b19-d2f7dbb53a78')}
assert hashlib.sha256(xml_before[vm]).hexdigest()=='e3200ea179e0cee3258b413baf8668529f2c1fdbed4535115a674d1b4d40d824'
for id in xml_before:assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
stat_before=disk.stat();assert stat_before.st_uid==0 and stat_before.st_gid==0 and stat_before.st_mode&0o777==0o600
acl=subprocess.check_output(['getfacl','--absolute-names',str(disk)])
with os.fdopen(os.open(aclfile,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600),'wb') as f:f.write(acl)
master=slave=None;p=None;transcript=bytearray();job_id=None
def drain(seconds=.15):
 end=time.monotonic()+seconds
 while time.monotonic()<end:
  if select.select([master],[],[],min(.1,max(0,end-time.monotonic())))[0]:
   try:part=os.read(master,65536)
   except OSError as e:
    if e.errno==errno.EIO:return
    raise
   if not part:return
   transcript.extend(part)
def wait_for(needle,timeout=300):
 end=time.monotonic()+timeout
 while time.monotonic()<end:
  drain()
  if needle in transcript:return
  if p.poll() is not None:break
 raise AssertionError('TUI marker absent: '+repr(needle))
try:
 subprocess.run(['sudo','-n','setfacl','-m','u:1000:r--',str(disk)],check=True)
 assert os.access(disk,os.R_OK)
 print(json.dumps({'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'fixtureReadACLGranted':True,'scope':'one retained disposable volume','originalMode':'0600','SELinux':subprocess.check_output(['getenforce'],text=True).strip()}),flush=True)
 master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',70,180,0,0))
 p=subprocess.Popen(['virmill','tui','--connection','qemu:///system','--timeout','10m'],stdin=slave,stdout=slave,stderr=slave,env=env,start_new_session=True);os.close(slave);slave=None
 wait_for(b'Development build');os.write(master,b'\t'+b'\x1b[B'*7+b'\r');wait_for(b'Input:')
 os.write(master,json.dumps({'id':parent,'input':policy}).encode()+b'\r');wait_for(b'Plan is a preview.')
 rows=dbrows("SELECT body,input FROM plans WHERE json_extract(body,'$.operation')='vm.create.accept-devices-v1' ORDER BY rowid DESC LIMIT 1")
 assert len(rows)==1;plan=json.loads(rows[0][0]);inp=json.loads(rows[0][1])
 assert plan['operation']=='vm.create.accept-devices-v1' and inp['parentOperationID']==parent and inp['devicePolicy']==policy['devicePolicy']
 assert plan['estimates']['requiresDowntime'] and set(plan['acknowledgements'])=={'inherit-recovery-resources','accept-observed-devices','exclusive-external-writer','watchdog-reset'}
 assert plan['review']['changesVM'] is False and plan['review']['reverifyRetainedBytes'] is True
 (root/'acceptance-plan-12d7bba.json').write_text(json.dumps(plan,indent=2)+'\n')
 for _ in range(18):os.write(master,b'\x1b[6~');drain(.03)
 assert b'watchdog-reset' in transcript and b'accept-observed-devices' in transcript
 os.write(master,b'a');wait_for(b'Type the full plan digest');os.write(master,plan['planDigest'].encode()+b'\r')
 end=time.monotonic()+120
 while time.monotonic()<end:
  drain()
  jobs=json.loads(cli('operation','list').stdout)['data']
  current=[j for j in jobs if j['planID']==plan['planID']]
  if current:job_id=current[0]['operationID'];break
 assert job_id is not None
 os.write(master,b'q');assert p.wait(timeout=10)==0
 print(json.dumps({'tuiPlanApproved':True,'planID':plan['planID'],'planDigest':plan['planDigest'],'operationID':job_id,'tuiDetached':True}),flush=True)
 last=None;deadline=time.monotonic()+1200
 while time.monotonic()<deadline:
  job=json.loads(cli('operation','show',job_id).stdout)['data']
  if job['state']!=last:print(json.dumps({'operationID':job_id,'state':job['state']}),flush=True);last=job['state']
  if job['state'] in ('succeeded','failed','canceled','partial','recovery-required'):break
  time.sleep(.5)
 else:raise AssertionError('observation timeout: inspect existing job, do not replay')
 (root/'acceptance-job-12d7bba.json').write_text(json.dumps(job,indent=2)+'\n')
 assert job['state']=='succeeded',job
 result=json.loads(cli('vm','creation','result',job_id).stdout)['data']
 assert result['complete'] and result['acceptanceProofAvailable'] and not any(result[k] for k in ('guestBootVerified','setupVerified','connectivityVerified'))
 (root/'acceptance-result-12d7bba.json').write_text(json.dumps(result,indent=2)+'\n')
 current_parent=json.loads(cli('operation','show',parent).stdout)['data'];assert current_parent['state']=='partial' and current_parent['recoveryOperationID']==job_id
 r=cli('vm','creation','result',parent,check=False);assert r.returncode==6 and json.loads(r.stdout)['data']['complete'] is False
 assert dbrows("SELECT body,input FROM plans WHERE id='"+plan_id+"'")==original_plan
 assert dbrows("SELECT body FROM metadata WHERE kind='vm-creation' AND id='"+plan_id+"'")==original_receipt
 assert not dbrows('SELECT resource,job_id FROM locks')
 managed=json.loads(cli('vm','show',vm).stdout)['data'];assert managed['ownership']=='managed' and managed['state']=='stopped'
 for id,x in xml_before.items():assert dump(id)==x
 for id,body in jobs_before:
  if id!=parent:assert dbrows("SELECT body FROM jobs WHERE id='"+id+"'")[0][0]==body
 assert hashlib.file_digest(disk.open('rb'),'sha256').hexdigest()=='8eb9be4a9592ad8242fa4e99fbac2dd2eef474bb93e99f3699d17617c7d93c70'
 print(json.dumps({'acceptance':'passed','originalCreationRemainsPartial':True,'originalPlanAndReceiptUnchanged':True,'managedOwnershipObserved':True,'allInheritedLocksReleased':True,'allGuestXMLUnchanged':True,'retainedDiskSHA256':'8eb9be4a9592ad8242fa4e99fbac2dd2eef474bb93e99f3699d17617c7d93c70','noGuestStartOrDefinition':True}),flush=True)
finally:
 if p is not None and p.poll() is None:p.terminate();p.wait(timeout=10)
 if master is not None:os.close(master)
 if slave is not None:os.close(slave)
 (root/'acceptance-tui-12d7bba.ansi').write_bytes(transcript)
 (root/'acceptance-tui-12d7bba.txt').write_text(re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace'))
 subprocess.run(['sudo','-n','setfacl','--restore='+str(aclfile)],check=True)
 assert subprocess.check_output(['getfacl','--absolute-names',str(disk)])==acl
 restored=disk.stat();assert restored.st_mode&0o777==0o600 and restored.st_uid==stat_before.st_uid and restored.st_gid==stat_before.st_gid
 print(json.dumps({'temporaryACLRestoredExactly':True,'mode':'0600','coordinatorFileReadAccessRemoved':not os.access(disk,os.R_OK),'operationID':job_id}),flush=True)
