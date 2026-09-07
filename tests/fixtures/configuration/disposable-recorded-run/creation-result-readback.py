import errno,fcntl,hashlib,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'};vm='a19bf9ee-cd7f-4921-baac-39ce1694eb35';success='28701ac3-325d-4596-8103-38e19f74b7cb';uncertain='b5983e68-bfcc-42f0-bf4e-3877029b756b'
def cli(*args,check=True):return subprocess.run(['virmill',*args,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=check,timeout=60)
def journal():
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return (list(db.execute('SELECT id,body FROM jobs ORDER BY id')),list(db.execute('SELECT resource,job_id FROM locks ORDER BY resource')))
assert json.loads(cli('version').stdout)['data']['revision'].startswith('11a8174')
before=journal();assert len(before[1])==3 and all(jid==uncertain for _,jid in before[1])
xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);assert hashlib.sha256(xml).hexdigest()=='e31e20ae14df1e2809c242416ff31416249b8c4d3a14dab74507afb02e0250d2'
r=cli('vm','creation','result',success);(root/'creation-result-success-11a8174.json').write_text(r.stdout);data=json.loads(r.stdout)['data']
assert data['operation']['state']=='succeeded' and data['complete'] and data['receiptAvailable'] and data['receipt']['defined'] and data['receipt']['volumesVerified']
assert not data['guestBootVerified'] and not data['setupVerified'] and not data['connectivityVerified']
print(json.dumps({'operationID':success,'state':'succeeded','complete':True,'receiptAvailable':True,'defined':True,'volumesVerified':True,'guestReadinessFlags':False,'responseSHA256':hashlib.sha256(r.stdout.encode()).hexdigest()}),flush=True)
r=cli('vm','creation','result',uncertain,check=False);(root/'creation-result-uncertain-11a8174.json').write_text(r.stdout);response=json.loads(r.stdout);data=response['data']
assert r.returncode==6 and response['error']['code']=='RECOVERY_REQUIRED' and not data['complete'] and data['receiptAvailable'] and data['operation']['state']=='recovery-required'
print(json.dumps({'operationID':uncertain,'state':'recovery-required','complete':False,'receiptAvailable':True,'errorCode':response['error']['code'],'exitCode':r.returncode,'responseSHA256':hashlib.sha256(r.stdout.encode()).hexdigest()}),flush=True)
master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',70,180,0,0))
p=subprocess.Popen(['virmill','tui','--connection','qemu:///system','--timeout','10m'],stdin=slave,stdout=slave,stderr=slave,env=env,start_new_session=True);os.close(slave);transcript=bytearray()
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
def wait_for(needle,timeout=20):
 end=time.monotonic()+timeout
 while time.monotonic()<end:
  drain()
  if needle in transcript:return
  if p.poll() is not None:break
 raise AssertionError('expected TUI result absent: '+repr(needle))
try:
 wait_for(b'Development build');os.write(master,b'\t'+b'\x1b[B'*4+b'\r');wait_for(b'Input:')
 os.write(master,success.encode()+b'\r');wait_for(b'"complete": true')
 assert b'"guestBootVerified": false' in transcript
 os.write(master,b'q');assert p.wait(timeout=10)==0
 assert journal()==before and subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])==xml
 for id in (vm,'ae630461-91d3-4f07-ad88-e6842c3dc3ea','2ec994ce-2950-498c-8b19-d2f7dbb53a78'):
  assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
 print(json.dumps({'nativePTY':True,'CLIAndTUIReadExistingReceipts':True,'journalJobsAndLocksUnchanged':True,'legacyUncertainLocksRetained':3,'allGuestsStopped':True,'nativeEffectsExecuted':False,'activeNativeUploadExercised':False}),flush=True)
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master);(root/'creation-result-readback.ansi').write_bytes(transcript);(root/'creation-result-readback.txt').write_text(re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace'))
