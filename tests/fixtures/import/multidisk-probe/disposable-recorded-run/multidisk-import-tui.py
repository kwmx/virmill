import errno,fcntl,hashlib,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'}
plan=json.loads((root/'multidisk-import-plan.json').read_text())['data']
assert plan['planID']=='dca04c7b-37f8-4fb7-afe5-64106d7bf6dc' and plan['planDigest']=='a79aecd216d95057b8c1fac02b7a5965be5c3a62d546a4924bd3cb597babaac3'
assert plan['review']['system']['id']=='virmill-multidisk-probe'
assert plan['review']['system']['diskIDs']==['boot','data']
assert plan['review']['sourceSHA256']=='1b2b15b998b4879d71cd2e5a1b137253cb41f14808a830ff8ba6ff231c0d88f0'
assert plan['acknowledgements']==['write-import-artifacts']
assert not pathlib.Path(plan['review']['destination']).exists()
def cli(*args):
 r=subprocess.run(['virmill',*args,'--connection','qemu:///system','--timeout','10m','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=660)
 assert r.returncode==0,r.stdout+r.stderr
 return json.loads(r.stdout)['data']
assert not any(j['planID']==plan['planID'] for j in cli('operation','list'))
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
 wait_for(b'Development build');os.write(master,b'\t'*8+b'\x1b[B'*5+b'\r');wait_for(b'Input:')
 os.write(master,plan['planID'].encode()+b'\r');wait_for(b'Plan is a preview.')
 for _ in range(15):os.write(master,b'\x1b[6~');drain(.03)
 assert b'virmill-multidisk-probe' in transcript and b'write-import-artifacts' in transcript
 os.write(master,b'a');wait_for(b'Type the full plan digest');os.write(master,plan['planDigest'].encode()+b'\r')
 job=None;deadline=time.monotonic()+120
 while time.monotonic()<deadline:
  drain();rows=[j for j in cli('operation','list') if j['planID']==plan['planID']]
  if rows:assert len(rows)==1;job=rows[0];break
 assert job is not None,'Inspect current plan and journal; do not replay'
 (root/'multidisk-import-accepted.json').write_text(json.dumps(job,indent=2)+'\n')
 os.write(master,b'q');assert p.wait(timeout=10)==0
 print(json.dumps({'correctedHarnessReviewField':'review.system.id','tuiPlanApproved':True,'planID':plan['planID'],'planDigest':plan['planDigest'],'operationID':job['operationID'],'tuiDetached':True}),flush=True)
 last=None;deadline=time.monotonic()+1500
 while time.monotonic()<deadline:
  job=cli('operation','show',job['operationID'])
  if job['state']!=last:print(json.dumps({'operationID':job['operationID'],'state':job['state']}),flush=True);last=job['state']
  if job['state'] in ('succeeded','failed','canceled','partial','recovery-required'):break
  time.sleep(.5)
 else:raise AssertionError('Observation timeout; inspect current operation without replay')
 (root/'multidisk-import-job.json').write_text(json.dumps(job,indent=2)+'\n');assert job['state']=='succeeded',job
 result=cli('import','result',job['operationID']);(root/'multidisk-import-result.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({'importResult':result}),flush=True)
 verified=cli('import','verify',plan['review']['destination']);(root/'multidisk-import-verified.json').write_text(json.dumps(verified,indent=2)+'\n');print(json.dumps({'artifactVerification':verified}),flush=True)
 with pathlib.Path(plan['review']['source']).open('rb') as f:assert hashlib.file_digest(f,'sha256').hexdigest()==plan['review']['sourceSHA256']
 print(json.dumps({'nativeMultiDiskPreparation':'passed','originalArchiveUnchanged':True,'guestBootTested':False}),flush=True)
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master);(root/'multidisk-import-tui.ansi').write_bytes(transcript);(root/'multidisk-import-tui.txt').write_text(re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace'))
