import errno,fcntl,hashlib,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,sys,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'}
assert sys.argv[1]==json.loads((root/'cold-fixes-upgrade.json').read_text())['revision']
def rows(sql):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(sql))
def cli(*args):
 p=subprocess.run(['virmill',*args,'--output','json','--non-interactive','--connection','qemu:///system'],env=env,text=True,capture_output=True,timeout=60)
 assert p.returncode==0,p.stdout+p.stderr
 return json.loads(p.stdout)['data']
assert cli('version')['revision']==sys.argv[1]
before_jobs=rows('SELECT id,body FROM jobs ORDER BY id');assert len(before_jobs)==26 and not rows('SELECT resource,job_id FROM locks')
request=json.loads((root/'cold-probe-creation-input.json').read_text());request['hardware']['name']='Virmill UEFI TPM estimate preview'
plan=cli('vm','create','39078e3f-672b-4451-90dd-7f1fe1824e5a','--input',json.dumps(request),'--plan')
with (root/'cold-fixes-estimate-plan.json').open('x') as f:json.dump(plan,f,indent=2)
assert plan['estimates']['additionalBytes']==125829120 and plan['review']['requiredFreeBytes']==125829120
assert not plan['estimates']['requiresDowntime'] and 'Copied volume payload: 458752 bytes' in plan['estimates']['notes']
assert cli('plan','show',plan['planID'])==plan
master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',70,180,0,0))
p=subprocess.Popen(['virmill','tui','--connection','qemu:///system'],stdin=slave,stdout=slave,stderr=slave,env=env,start_new_session=True);os.close(slave)
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
   transcript.extend(part);assert len(transcript)<=2<<20

def wait(marker,timeout=30):
 end=time.monotonic()+timeout
 while time.monotonic()<end:
  drain()
  if marker in transcript:return
  if p.poll() is not None:break
 raise AssertionError('TUI marker absent: '+repr(marker))
try:
 wait(b'Development build')
 # Jobs is section 8; plan show is its sixth action in this pinned registry.
 os.write(master,b'\t'*8+b'\x1b[B'*5+b'\r');wait(b'Input:')
 os.write(master,plan['planID'].encode()+b'\r');wait(b'Plan is a preview.')
 for _ in range(35):os.write(master,b'\x1b[6~');drain(.03)
 for marker in (b'125829120',b'Copied volume payload: 458752 bytes',b'Initial physical allocation',plan['planDigest'].encode()):assert marker in transcript,marker
 # Explicitly clear the review and leave; never enter apply confirmation.
 os.write(master,b'\x1b');drain(.15);os.write(master,b'q');assert p.wait(timeout=10)==0
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master)
 with (root/'cold-fixes-estimate-tui.ansi').open('xb') as f:f.write(transcript)
 with (root/'cold-fixes-estimate-tui.txt').open('x') as f:f.write(re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace'))
assert rows('SELECT id,body FROM jobs ORDER BY id')==before_jobs and not rows('SELECT resource,job_id FROM locks')
assert cli('plan','show',plan['planID'])==plan
for vm,sha in json.loads((root/'cold-fixes-upgrade.json').read_text())['allFiveStoppedVMXMLSHA256'].items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()==sha
report={'revision':sys.argv[1],'sharedCLIAndTUIEstimateVisible':True,'plan':plan,'samePlanAndDigestRetained':True,'all26JobsAndFiveStoppedGuestXMLPreserved':True,'noApplySubmitted':True,'transcriptSHA256':hashlib.sha256(transcript).hexdigest(),'completeCaptureVerified':False}
with (root/'cold-fixes-estimate-tui.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
