import errno,fcntl,hashlib,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'};vm='a19bf9ee-cd7f-4921-baac-39ce1694eb35'
def plans():
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return {row[0]:json.loads(row[1]) for row in db.execute('SELECT id,body FROM plans')}
def cli(*args):return subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True,timeout=60)
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
beforePlans=plans();before=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);assert before==(root/'boot-media-fixture-attached.xml').read_bytes()
request={'id':vm,'input':{'bootOrder':[{'kind':'disk','id':'sda'},{'kind':'disk','id':'vda'}],'applyMode':'next-boot'}}
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
def wait_for(needle,start=0,timeout=20):
 end=time.monotonic()+timeout
 while time.monotonic()<end:
  drain()
  if needle in transcript[start:]:return
  if p.poll() is not None:break
 raise AssertionError('TUI expected output absent: '+repr(needle))
try:
 wait_for(b'Development build')
 os.write(master,b'\t'+b'\x1b[B'*2+b'\r');wait_for(b'Input:')
 mark=len(transcript);os.write(master,vm.encode()+b'\r');wait_for(b'"persistent"',mark)
 mark=len(transcript);os.write(master,b'\x1b[B'*12+b'\r');wait_for(b'Input:',mark);assert b'vm set' in transcript[mark:]
 mark=len(transcript);os.write(master,json.dumps(request,separators=(',',':')).encode()+b'\r');wait_for(b'Plan is a preview.',mark)
 afterPlans=plans();new=set(afterPlans)-set(beforePlans);assert len(new)==1,new
 planID=new.pop();r=cli('plan','show',planID);plan=json.loads(r.stdout)['data'];(root/'boot-order-plan.json').write_text(r.stdout);print(r.stdout,flush=True)
 assert plan['operation']=='vm.configure-hardware' and plan['resourceIDs']==['libvirt|qemu:///system|vm|'+vm]
 assert set(plan['acknowledgements'])=={'host-mutation','exclusive-configuration-writer','replace-boot-order'}
 assert plan['review']['requested']==request['input'] and not plan['review']['diskDeletion']
 afterView=plan['review']['afterBoot'];assert {d['id']:d['order'] for d in afterView['devices']}=={'sda':1,'vda':2}
 for _ in range(30):
  if b'"afterBoot"' in transcript[mark:] and b'"beforeBoot"' in transcript[mark:] and b'replace-boot-order' in transcript[mark:]:break
  os.write(master,b'\x1b[6~');drain()
 assert b'"beforeBoot"' in transcript[mark:] and b'"afterBoot"' in transcript[mark:]
 os.write(master,b'a');wait_for(b'Type the full plan digest',mark)
 mark=len(transcript);os.write(master,plan['planDigest'].encode()+b'\r');wait_for(b'"operationID"',mark,timeout=90)
 os.write(master,b'q');assert p.wait(timeout=10)==0
 jobs=[j for j in json.loads(cli('operation','list').stdout)['data'] if j['planID']==planID];assert len(jobs)==1
 jid=jobs[0]['operationID']
 for _ in range(60):
  r=cli('operation','show',jid);job=json.loads(r.stdout)['data']
  if job['state'] not in ('queued','validating','running'):break
  time.sleep(.25)
 (root/'boot-order-result.json').write_text(r.stdout);print(r.stdout,flush=True);assert job['state']=='succeeded',job
 r=cli('vm','boot','show',vm);(root/'boot-order-view.json').write_text(r.stdout);print(r.stdout,flush=True)
 view=json.loads(r.stdout)['data'];assert view['live'] is None and view['state']=='stopped' and not view['guestBootVerified']
 assert {d['id']:d['order'] for d in view['persistent']['devices']}=={'sda':1,'vda':2}
 after=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);(root/'boot-order-applied.xml').write_bytes(after)
 print(json.dumps({'nativePTY':True,'actions':['VMs: vm boot show','VMs: vm set JSON form','review before/after order','acknowledge exact digest','detach','CLI observe job and boot layers'],'vmID':vm,'beforeXMLSHA256':hashlib.sha256(before).hexdigest(),'afterXMLSHA256':hashlib.sha256(after).hexdigest(),'guestBootTested':False}),flush=True)
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master);(root/'boot-order-tui.ansi').write_bytes(transcript)
 (root/'boot-order-tui.txt').write_text(re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace'))
