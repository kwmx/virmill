import errno,fcntl,hashlib,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'};vm='a19bf9ee-cd7f-4921-baac-39ce1694eb35'
def cli(*args):return subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True,timeout=60)
def ids(table):
 assert table in ('plans','jobs')
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return {row[0] for row in db.execute('SELECT id FROM '+table)}
version=json.loads(cli('version').stdout)['data'];assert version['revision'].startswith('15a1f2c')
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
before=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);assert hashlib.sha256(before).hexdigest()=='e31e20ae14df1e2809c242416ff31416249b8c4d3a14dab74507afb02e0250d2'
old=json.loads(cli('plan','show','b688d921-d000-40d9-9814-e12182a11812').stdout)['data'];assert not old['estimates']['requiresDowntime'] and old['planDigest']=='6b727dcf11b6156296c267ce1a25d177557a357b02691bf237c92617ede64660'
request={'id':vm,'input':{'bootOrder':[{'kind':'disk','id':'vda'}],'applyMode':'next-boot'}}
r=cli('vm','set',vm,'--input',json.dumps(request['input']),'--plan');plan=json.loads(r.stdout)['data'];(root/'downtime-cli-plan.json').write_text(r.stdout);print(r.stdout,flush=True)
assert plan['estimates']['requiresDowntime'] and 'already stopped' in plan['estimates']['notes']
beforePlans=ids('plans');beforeJobs=ids('jobs')
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
 raise AssertionError('expected TUI output absent: '+repr(needle))
try:
 wait_for(b'Development build');os.write(master,b'\t'+b'\x1b[B'*14+b'\r');wait_for(b'Input:')
 os.write(master,json.dumps(request).encode()+b'\r');wait_for(b'Plan is a preview.')
 new=ids('plans')-beforePlans;assert len(new)==1
 tuiPlan=json.loads(cli('plan','show',new.pop()).stdout)['data'];assert tuiPlan['estimates']==plan['estimates'] and tuiPlan['review']['requested']==plan['review']['requested']
 for _ in range(30):
  if b'"requiresDowntime": true' in transcript and b'already stopped' in transcript:break
  os.write(master,b'\x1b[6~');drain()
 assert b'"requiresDowntime": true' in transcript and b'already stopped' in transcript
 os.write(master,b'q');assert p.wait(timeout=10)==0
 assert ids('jobs')==beforeJobs and subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])==before
 print(json.dumps({'nativePTY':True,'CLITUIDowntimeEstimatesEqual':True,'requiresDowntime':True,'stoppedPrerequisiteDisplayed':True,'oldPlanDigestAndEstimateUnchanged':True,'plansPreviewedOnly':2,'operationAccepted':False,'VMXMLUnchangedSHA256':hashlib.sha256(before).hexdigest()}),flush=True)
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master);(root/'downtime-preview-tui.ansi').write_bytes(transcript);(root/'downtime-preview-tui.txt').write_text(re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace'))
