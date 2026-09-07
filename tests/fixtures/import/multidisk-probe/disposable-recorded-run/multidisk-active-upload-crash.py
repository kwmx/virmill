import datetime,hashlib,json,os,pathlib,select,signal,sqlite3,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
unit='virmill-test-12d7bba.service';dbpath=root/'state/virmill/journal.db';plan=json.loads((root/'multidisk-create-plan.json').read_text())['data']
spec=plan['review']['target']['spec'];pool=json.loads((root/'multidisk-pool.json').read_text());dest=pathlib.Path(pool['target'])
assert plan['operation']=='vm.create.devices-v1' and spec['name']=='Virmill multi-disk interrupted copy' and spec['poolID']==pool['uuid']=='36ab8b28-60d8-4495-9046-6f2f6bfc23b2'
assert spec['nics']==[] and spec['firmware']=={'mode':'bios','secureBoot':False,'tpm':False}
assert spec['disks']==[{'sourceID':'boot','bus':'virtio','bootOrder':1},{'sourceID':'data','bus':'virtio','bootOrder':2}]
assert spec['devicePolicy']=={'version':1,'chipset':'q35','pciPlacement':'libvirt-auto','usbController':'none','memoryBalloon':'none','watchdogAction':'none','input':'ps2','audio':'none','serial':'isa-serial'}
assert set(plan['acknowledgements'])=={'host-mutation','copy-managed-volumes','new-vm-identity','creation-device-policy'}
assert len(plan['review']['volumes'])==2 and plan['review']['volumes'][1]['fileBytes']>1<<30
with sqlite3.connect(dbpath.as_uri()+'?mode=ro',uri=True) as db:
 assert not list(db.execute('SELECT resource,job_id FROM locks'))
 assert not list(db.execute('SELECT id FROM jobs WHERE plan_id=?',(plan['planID'],)))
 before_jobs={id:hashlib.sha256(body).hexdigest() for id,body in db.execute('SELECT id,body FROM jobs')}
for vm,digest in pool['existingStoppedVMXMLSHA256'].items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()==digest
props=dict(line.split('=',1) for line in subprocess.check_output(['systemctl','--user','show',unit,'--property=MainPID,WorkingDirectory,ActiveState,Restart'],text=True).splitlines())
assert props['ActiveState']=='active' and props['WorkingDirectory']==str(root) and props['Restart']=='no';pid=int(props['MainPID']);assert pid>1
pidfd=os.pidfd_open(pid);assert os.readlink('/proc/'+str(pid)+'/exe')=='/usr/bin/virmilld';assert hashlib.sha256(pathlib.Path('/usr/bin/virmilld').read_bytes()).hexdigest()=='d514cb9348f69779f7aa29016d0f4bdc979b16f5f0534ec2a15521310ed12170'
intent={'test':'kill coordinator during native second-volume upload','planID':plan['planID'],'planDigest':plan['planDigest'],'vmID':spec['uuid'],'poolID':pool['uuid'],'coordinatorPID':pid,'coordinatorUnit':unit,'initialJobsSHA256':before_jobs,'signalScope':'pidfd-held ordinary-user disposable coordinator only','requiredObservation':'first disk verified; second allocated but unverified; upload intent latest; allocated bytes increasing and below half the file size'}
with (root/'multidisk-crash-intent.json').open('x') as f:json.dump(intent,f,indent=2)
args=['virmill','plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key','multidisk-upload-crash-'+plan['planID'],'--connection','qemu:///system','--timeout','10m','--output','json','--non-interactive']
for ack in plan['acknowledgements']:args.extend(['--ack',ack])
apply=subprocess.Popen(args,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
stopped=False;killed=False;jobid=None;observation=None;baseline=None;last=None
db=sqlite3.connect(dbpath.as_uri()+'?mode=ro',uri=True,isolation_level=None)
def snapshot():
 db.execute('BEGIN')
 try:
  row=db.execute('SELECT id,body FROM jobs WHERE plan_id=?',(plan['planID'],)).fetchone()
  if row is None:return None
  receipt=db.execute("SELECT body FROM metadata WHERE kind='vm-creation' AND id=?",(plan['planID'],)).fetchone()
  event=db.execute('SELECT body FROM events WHERE job_id=? ORDER BY seq DESC LIMIT 1',(row[0],)).fetchone()
  return {'operationID':row[0],'job':json.loads(row[1]),'receipt':json.loads(receipt[0]) if receipt else None,'event':json.loads(event[0]) if event else None}
 finally:db.execute('COMMIT')
def eligible(s):
 if not s or s['job']['state']!='running' or not s['receipt'] or not s['event']:return False
 r=s['receipt'];return len(r['volumes'])==2 and r['volumes'][0]['verified'] and r['volumes'][1]['allocated'] is not None and not r['volumes'][1]['verified'] and not r['volumesVerified'] and not r['defined'] and s['event']['message']=='Intent persisted: upload verified source artifact data into its new volume'
def filestat(s):
 v=s['receipt']['volumes'][1];path=pathlib.Path(v['allocated']['path']);assert path==dest/v['intent']['name'];assert not path.is_symlink();st=path.stat()
 return {'path':str(path),'device':st.st_dev,'inode':st.st_ino,'size':st.st_size,'allocatedBytes':st.st_blocks*512,'mtimeNS':st.st_mtime_ns,'ctimeNS':st.st_ctime_ns}
try:
 deadline=time.monotonic()+600
 while time.monotonic()<deadline:
  s=snapshot()
  if s:
   jobid=s['operationID'];event=s['event'];key=(s['job']['state'],event['sequence'] if event else None)
   if key!=last:print(json.dumps({'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'jobState':s['job']['state'],'event':event}),flush=True);last=key
   if s['job']['state'] in ('succeeded','failed','canceled','partial','recovery-required'):break
  if eligible(s):
   observed=filestat(s)
   if baseline is None:baseline=observed
   if observed['allocatedBytes']-baseline['allocatedBytes']>=4<<20 and observed['allocatedBytes']<s['receipt']['volumes'][1]['intent']['fileBytes']//2:
    signal.pidfd_send_signal(pidfd,signal.SIGSTOP);stopped=True
    for _ in range(100):
     state=next(line for line in pathlib.Path('/proc/'+str(pid)+'/status').read_text().splitlines() if line.startswith('State:'))
     if 'T (stopped)' in state:break
     time.sleep(.001)
    assert 'T (stopped)' in state
    frozen=snapshot();frozenstat=filestat(frozen) if eligible(frozen) else None
    if frozenstat and frozenstat['allocatedBytes']<frozen['receipt']['volumes'][1]['intent']['fileBytes']//2:
     observation={'snapshot':frozen,'baselineFile':baseline,'preStopFile':observed,'stoppedFile':frozenstat,'signal':'SIGKILL','signalPrecondition':'SIGSTOP and revalidated pending native second-volume upload'}
     with (root/'multidisk-crash-boundary.json').open('x') as f:json.dump(observation,f,indent=2)
     signal.pidfd_send_signal(pidfd,signal.SIGKILL);killed=True;stopped=False
     assert select.select([pidfd],[],[],10)[0],'Killed coordinator did not exit'
     print(json.dumps({'crashInjected':True,'operationID':jobid,'snapshot':observation}),flush=True);break
    signal.pidfd_send_signal(pidfd,signal.SIGCONT);stopped=False
  time.sleep(.002)
 assert killed,'Required active upload boundary was not observed; no crash claim and no replay'
 out,err=apply.communicate(timeout=20);(root/'multidisk-create-apply.json').write_text(out);print(json.dumps({'applyExitCode':apply.returncode,'stdout':out,'stderr':err}),flush=True)
 time.sleep(1)
 post=snapshot();assert eligible(post)
 original_receipt=db.execute("SELECT body FROM metadata WHERE kind='vm-creation' AND id=?",(plan['planID'],)).fetchone()[0]
 locks=list(db.execute('SELECT resource,job_id FROM locks ORDER BY resource'));assert locks and all(owner==jobid for _,owner in locks)
 postfiles=[]
 for v in post['receipt']['volumes']:
  path=pathlib.Path(v['allocated']['path']);assert path==dest/v['intent']['name'];st=path.stat()
  digest=subprocess.check_output(['sudo','-n','sha256sum',str(path)],text=True).split()[0]
  postfiles.append({'path':str(path),'device':st.st_dev,'inode':st.st_ino,'size':st.st_size,'allocatedBytes':st.st_blocks*512,'mtimeNS':st.st_mtime_ns,'ctimeNS':st.st_ctime_ns,'sha256':digest,'expectedSHA256':v['intent']['sha256']})
 assert postfiles[0]['sha256']==postfiles[0]['expectedSHA256'] and postfiles[1]['sha256']!=postfiles[1]['expectedSHA256']
 with (root/'multidisk-crash-before-restart.json').open('x') as f:json.dump({'operationID':jobid,'receiptSHA256':hashlib.sha256(original_receipt).hexdigest(),'locks':locks,'volumes':postfiles},f,indent=2)
 print(json.dumps({'postCrashPartialDiskConfirmed':True,'volumes':postfiles,'heldLocks':locks}),flush=True)
finally:
 if stopped:signal.pidfd_send_signal(pidfd,signal.SIGCONT)
 os.close(pidfd);db.close()
 if killed:
  subprocess.run(['systemctl','--user','start',unit],check=True)
  print(json.dumps({'coordinatorRestartRequested':True,'unit':unit}),flush=True)
 if apply.poll() is None:apply.terminate();apply.wait(timeout=10)
