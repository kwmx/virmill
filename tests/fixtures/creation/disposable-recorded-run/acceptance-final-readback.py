import errno,fcntl,hashlib,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'};operation='6d4792c7-4e09-40ab-95ef-9b4cc05be8af'
def cli(*args):return subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True,timeout=60)
def journal():
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return (list(db.execute('SELECT id,body FROM jobs ORDER BY id')),list(db.execute('SELECT resource,job_id FROM locks ORDER BY resource')))
before=journal();assert not before[1]
result=json.loads(cli('vm','creation','result',operation).stdout)['data'];assert result['complete'] and result['acceptanceProofAvailable'] and not result['guestBootVerified']
master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',70,180,0,0));p=subprocess.Popen(['virmill','tui','--connection','qemu:///system','--timeout','10m'],stdin=slave,stdout=slave,stderr=slave,env=env,start_new_session=True);os.close(slave);transcript=bytearray()
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
 raise AssertionError('TUI result not observed: '+repr(needle))
try:
 wait_for(b'Development build');os.write(master,b'\t'+b'\x1b[B'*4+b'\r');wait_for(b'Input:');os.write(master,operation.encode()+b'\r');wait_for(b'acceptanceProofAvailable')
 for _ in range(8):os.write(master,b'\x1b[6~');drain(.03)
 assert b'"complete": true' in transcript and b'"guestBootVerified": false' in transcript
 os.write(master,b'q');assert p.wait(timeout=10)==0
 assert journal()==before
 hashes={}
 for id in ('ae630461-91d3-4f07-ad88-e6842c3dc3ea','a19bf9ee-cd7f-4921-baac-39ce1694eb35','2ec994ce-2950-498c-8b19-d2f7dbb53a78'):
  assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
  hashes[id]=hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id])).hexdigest()
 assert hashes['ae630461-91d3-4f07-ad88-e6842c3dc3ea']=='e3200ea179e0cee3258b413baf8668529f2c1fdbed4535115a674d1b4d40d824'
 assert hashes['a19bf9ee-cd7f-4921-baac-39ce1694eb35']=='e31e20ae14df1e2809c242416ff31416249b8c4d3a14dab74507afb02e0250d2'
 assert hashes['2ec994ce-2950-498c-8b19-d2f7dbb53a78']=='bbe62f376a3943d795cdac4fd67ff56087efa43024b27cacf0578217a1a44408'
 disk=pathlib.Path('/var/lib/libvirt/images/virmill-qualification-65930c6/virmill-ae630461-91d3-4f07-ad88-e6842c3dc3ea-disk-000.qcow2')
 assert not os.access(disk,os.R_OK) and disk.stat().st_mode&0o777==0o600
 assert subprocess.check_output(['getfacl','--absolute-names',str(disk)])==(root/'acceptance-original-volume-12d7bba.acl').read_bytes()
 versions=subprocess.check_output(['rpm','-q','--queryformat','%{NAME}-%{VERSION}-%{RELEASE}.%{ARCH} installed=%{INSTALLTIME}\n','acl','libacl'],text=True).splitlines()
 print(json.dumps({'nativePTYResultReadback':True,'completeAcceptance':True,'guestReadinessFlags':False,'jobsAndLocksUnchangedByReadback':True,'locksRemaining':0,'allGuestsStopped':True,'retainedXMLSHA256':hashes,'originalACLRestored':True,'ACLToolPackages':versions,'SELinux':subprocess.check_output(['getenforce'],text=True).strip()}),flush=True)
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master);(root/'acceptance-result-tui-12d7bba.ansi').write_bytes(transcript)
