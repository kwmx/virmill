import errno,fcntl,hashlib,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,sys,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'}
revision='ad608076cd928e6c6c270e6442cbc2d8047e6e0c';vm='d4c95f21-28bc-428d-9e5f-ceda025d279e'
def rows(sql):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(sql))
def cli(*args):
 p=subprocess.run(['virmill',*args,'--output','json','--non-interactive','--connection','qemu:///system'],env=env,text=True,capture_output=True,timeout=60)
 assert p.returncode==0,p.stdout+p.stderr
 return json.loads(p.stdout)['data']
assert cli('version')['revision']==revision
before_jobs=rows('SELECT id,body FROM jobs ORDER BY id');assert len(before_jobs)==31 and not rows('SELECT resource,job_id FROM locks')
inspection=cli('vm','recovery','inspect',vm)
assert inspection['state']=='stopped' and not inspection['hasManagedSave'] and not inspection['autostart']
assert inspection['layout']['tpm']['sourcePath']=='' and inspection['layout']['tpm']['profile']=='default-v1'
assert 'no path was guessed' in inspection['warnings'][-1]
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
 # Protection is section 6; native recovery inspection is its first action.
 os.write(master,b'\t'*6+b'\r');wait(b'Input:')
 os.write(master,vm.encode()+b'\r');wait(b'"autostart": false')
 for _ in range(35):os.write(master,b'\x1b[6~');drain(.03)
 for marker in (b'default-v1',b'OVMF_CODE_4M.qcow2',b'no path was guessed',inspection['fingerprint'].encode()):assert marker in transcript,marker
 # Explicitly clear the review and leave; never enter apply confirmation.
 os.write(master,b'\x1b');drain(.15);os.write(master,b'q');assert p.wait(timeout=10)==0
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master)
 with (root/'cold-probe-4m-inspection-tui.ansi').open('xb') as f:f.write(transcript)
 with (root/'cold-probe-4m-inspection-tui.txt').open('x') as f:f.write(re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace'))
assert rows('SELECT id,body FROM jobs ORDER BY id')==before_jobs and not rows('SELECT resource,job_id FROM locks')
assert cli('vm','recovery','inspect',vm)==inspection
old=json.loads((root/'cold-probe-4m-created.json').read_text())['allFiveEarlierStoppedVMXMLPreserved']
old[vm]='a10ba2cb370cd1740d42e34a944663420e8b716315e47f21d999a5a64249411e'
for key,sha in old.items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',key],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',key])).hexdigest()==sha
report={'revision':revision,'sharedCLIAndTUIColdInspectionVisible':True,'inspection':inspection,'all31JobsAndSixStoppedGuestXMLPreserved':True,'noPlanOrApplySubmitted':True,'transcriptSHA256':hashlib.sha256(transcript).hexdigest(),'completeCaptureVerified':False,'independentRecoveryVerified':False}
with (root/'cold-probe-4m-inspection-tui.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
