import errno,fcntl,hashlib,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'}
parent='a3b3f9c8-e474-48eb-a916-60ed534c81cc';original='f7ecbe1b-73f0-491a-adde-7248c960c2f6'
before=json.loads((root/'multidisk-crash-before-restart.json').read_text())
def cli(*args,check=True):
 r=subprocess.run(['virmill',*args,'--connection','qemu:///system','--timeout','10m','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=660)
 if check:assert r.returncode==0,r.stdout+r.stderr
 return r
assert json.loads(cli('operation','show',parent).stdout)['data']['state']=='recovery-required'
def rows(sql,args=()):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(sql,args))
assert [list(x) for x in rows('SELECT resource,job_id FROM locks ORDER BY resource')]==before['locks']
assert hashlib.sha256(rows("SELECT body FROM metadata WHERE kind='vm-creation' AND id=?",(original,))[0][0]).hexdigest()==before['receiptSHA256']
master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',70,180,0,0))
p=subprocess.Popen(['virmill','tui','--connection','qemu:///system','--timeout','10m'],stdin=slave,stdout=slave,stderr=slave,env=env,start_new_session=True);os.close(slave)
transcript=bytearray()
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
def wait_for(marker,timeout=40):
 end=time.monotonic()+timeout
 while time.monotonic()<end:
  drain()
  if marker in transcript:return
  if p.poll() is not None:break
 raise AssertionError('TUI marker absent: '+repr(marker))
try:
 wait_for(b'Development build');os.write(master,b'\t'+b'\x1b[B'*6+b'\r');wait_for(b'Input:')
 os.write(master,json.dumps({'id':parent,'input':{'disposition':'retain'}}).encode()+b'\r');wait_for(b'Plan is a preview.')
 found=rows("SELECT body,input FROM plans WHERE json_extract(body,'$.operation')='vm.create.cleanup' AND json_extract(input,'$.parentOperationID')=?",(parent,));assert len(found)==1
 plan=json.loads(found[0][0]);inp=json.loads(found[0][1]);assert inp['disposition']=='retain' and inp['creationPlanID']==original
 assert set(plan['acknowledgements'])=={'inherit-recovery-resources','abandon-creation','retain-partial-volumes'}
 review=plan['review'];assert review['disposition']=='retain' and review['closesOriginalRecipe'] and not review['createsVM'] and not review['deletesVM']
 assert len(review['volumes'])==2 and all(v['state']=='present' for v in review['volumes'])
 with (root/'multidisk-retain-plan.json').open('x') as f:json.dump(plan,f,indent=2)
 for _ in range(20):os.write(master,b'\x1b[6~');drain(.03)
 assert b'retain-partial-volumes' in transcript and b'closesOriginalRecipe' in transcript
 os.write(master,b'a');wait_for(b'Type the full plan digest');os.write(master,plan['planDigest'].encode()+b'\r')
 job=None;deadline=time.monotonic()+120
 while time.monotonic()<deadline:
  drain();found=rows('SELECT body FROM jobs WHERE plan_id=?',(plan['planID'],))
  if found:assert len(found)==1;job=json.loads(found[0][0]);break
 assert job is not None,'Inspect existing operation; never replay automatically'
 os.write(master,b'q');assert p.wait(timeout=10)==0
 print(json.dumps({'tuiRetentionApproved':True,'planID':plan['planID'],'planDigest':plan['planDigest'],'operationID':job['operationID']}),flush=True)
 deadline=time.monotonic()+120
 while time.monotonic()<deadline:
  job=json.loads(cli('operation','show',job['operationID']).stdout)['data']
  if job['state'] in ('succeeded','failed','canceled','partial','recovery-required'):break
  time.sleep(.1)
 assert job['state']=='succeeded',job
 result=json.loads(cli('vm','creation','result',job['operationID']).stdout)['data']
 assert result['complete'] and result['cleanupProofAvailable'] and result['dispositionAvailable'] and not result['vmCreated'] and not result['guestBootVerified']
 assert result['disposition']['disposition']=='retain' and len(result['disposition']['observation']['volumes'])==2
 assert not any(result['cleanupProof']['deleteIntents']) and not any(result['cleanupProof']['confirmedAbsent'])
 parent_job=json.loads(cli('operation','show',parent).stdout)['data'];assert parent_job['state']=='partial' and parent_job['recoveryOperationID']==job['operationID']
 assert not rows('SELECT resource,job_id FROM locks')
 assert hashlib.sha256(rows("SELECT body FROM metadata WHERE kind='vm-creation' AND id=?",(original,))[0][0]).hexdigest()==before['receiptSHA256']
 for v in before['volumes']:
  s=pathlib.Path(v['path']).stat();assert (s.st_dev,s.st_ino,s.st_size,s.st_blocks*512,s.st_mtime_ns,s.st_ctime_ns)==tuple(v[k] for k in ('device','inode','size','allocatedBytes','mtimeNS','ctimeNS'))
  assert subprocess.check_output(['sudo','-n','sha256sum',v['path']],text=True).split()[0]==v['sha256']
 r=cli('vm','creation','resume',parent,'--plan',check=False);assert r.returncode==6 and json.loads(r.stdout)['error']['code']=='RECOVERY_REQUIRED'
 with (root/'multidisk-retention-result.json').open('x') as f:json.dump(result,f,indent=2)
 print(json.dumps({'retentionResult':result,'parentRemainsPartial':True,'originalReceiptUnchanged':True,'volumesUnchanged':True,'inheritedLocksReleased':True,'closedRecipeResumeRefused':True,'deletionTested':False}),flush=True)
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master);(root/'multidisk-retain-tui.ansi').write_bytes(transcript);(root/'multidisk-retain-tui.txt').write_text(re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace'))
