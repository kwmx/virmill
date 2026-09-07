import errno,fcntl,hashlib,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'}
vm='b9496482-2eeb-40e1-892b-4e291c108c52'
before=json.loads((root/'helper-access-original-b7fe053.json').read_text())
disk=pathlib.Path('/var/lib/libvirt/images/virmill-multidisk-12d7bba/virmill-b9496482-2eeb-40e1-892b-4e291c108c52-disk-000.qcow2')
def cli(*args):
 p=subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive','--timeout','30s'],env=env,text=True,capture_output=True,check=True,timeout=45);return json.loads(p.stdout)['data']
def rows(sql):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(sql))
assert not os.access(disk,os.R_OK) and not os.access(disk,os.W_OK)
master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',70,180,0,0));transcript=bytearray();job_id=None
p=subprocess.Popen(['virmill','tui','--connection','qemu:///system'],stdin=slave,stdout=slave,stderr=slave,env=env,start_new_session=True);os.close(slave)
def drain(seconds=.12):
 end=time.monotonic()+seconds
 while time.monotonic()<end:
  if select.select([master],[],[],min(.05,max(0,end-time.monotonic())))[0]:
   try:part=os.read(master,65536)
   except OSError as e:
    if e.errno==errno.EIO:return
    raise
   if not part:return
   transcript.extend(part)
def wait_for(needle,timeout=30):
 end=time.monotonic()+timeout
 while time.monotonic()<end:
  drain()
  if needle in transcript:return
  if p.poll() is not None:break
 raise AssertionError('TUI marker absent: '+repr(needle))
try:
 wait_for(b'Overview')
 for _ in range(3):os.write(master,b'\t');drain()
 wait_for(b'storage access grant')
 for _ in range(2):os.write(master,b'\x1b[B');drain()
 os.write(master,b'\r');drain();os.write(master,json.dumps({'id':vm,'input':{'target':'vda','rootID':'images'}}).encode()+b'\r')
 wait_for(b'Plan is a preview.')
 matches=rows("SELECT body,input FROM plans WHERE json_extract(body,'$.operation')='storage.grant-read' ORDER BY rowid DESC LIMIT 1")
 assert len(matches)==1;plan=json.loads(matches[0][0]);inp=json.loads(matches[0][1])
 assert inp['access']['mapping']['vmID']==vm and inp['access']['mapping']['diskTarget']=='vda'
 assert set(plan['acknowledgements'])=={'host-permission-change','exclusive-offline-volume','persistent-disk-read-access'}
 assert plan['review']['before']['acl']=='' and plan['review']['before']['file']['mode']==before['mode']
 for _ in range(12):os.write(master,b'\x1b[6~');drain(.03)
 assert b'persistent-disk-read-access' in transcript
 os.write(master,b'a');wait_for(b'Type the full plan digest');os.write(master,plan['planDigest'].encode()+b'\r')
 end=time.monotonic()+30
 while time.monotonic()<end:
  drain();jobs=[j for j in cli('operation','list') if j['planID']==plan['planID']]
  if jobs:job_id=jobs[0]['operationID'];break
 assert job_id is not None
 with (root/'helper-access-tui-grant-operation-b7fe053.json').open('x') as f:json.dump({'operationID':job_id,'plan':plan},f,indent=2)
 os.write(master,b'q');assert p.wait(timeout=10)==0
 end=time.monotonic()+30
 while time.monotonic()<end:
  job=cli('operation','show',job_id)
  if job['state'] in ('succeeded','failed','canceled','partial','recovery-required'):break
  time.sleep(.1)
 assert job['state']=='succeeded',job
 assert os.access(disk,os.R_OK) and not os.access(disk,os.W_OK)
 with disk.open('rb') as f:sha=hashlib.file_digest(f,'sha256').hexdigest()
 assert sha=='fe1356a1e613740dc51fb799fc1f559a1f4e311bbcae6b15424c2bd01ec6dfd3'
 print(json.dumps({'tuiGrant':'passed','operationID':job_id,'sourceDiskSHA256':sha}),flush=True)
 revoke=cli('storage','access','revoke',job_id,'--plan')
 assert revoke['review']['originalGrantOperationID']==job_id and revoke['review']['desiredAccessACL']=='0200000001000600ffffffff04000000ffffffff20000000ffffffff'
 print(json.dumps({'cliRevokePlan':revoke}),flush=True)
 args=['plan','apply',revoke['planID'],'--digest',revoke['planDigest'],'--idempotency-key','helper-access-cli-revoke-b7fe053-001','--wait']
 for ack in revoke['acknowledgements']:args+=['--ack',ack]
 restored=cli(*args);assert restored['state']=='succeeded',restored
 result=cli('storage','access','result',restored['operationID']);assert result['complete']
 st=disk.stat();assert st.st_dev==before['device'] and st.st_ino==before['inode'] and st.st_size==before['size'] and st.st_mtime_ns==before['mtimeNS']
 assert st.st_mode==before['mode'] and st.st_uid==before['uid'] and st.st_gid==before['gid']
 assert subprocess.check_output(['getfacl','--absolute-names','--numeric',str(disk)]).decode()==before['acl']
 assert not os.access(disk,os.R_OK) and not os.access(disk,os.W_OK)
 assert not rows('SELECT resource,job_id FROM locks')
 report={'tuiGrant':'passed','cliRevoke':'passed','grantOperationID':job_id,'revokeOperationID':restored['operationID'],'exactOriginalAccessRestored':True,'fileGenerationAndByteMetadataUnchanged':True,'ordinaryActorReadRemoved':True,'allResourceLocksReleased':True,'tuiDetached':True,'SELinux':subprocess.check_output(['getenforce'],text=True).strip()}
 with (root/'helper-access-tui-grant-cli-revoke-b7fe053.json').open('x') as f:json.dump(report,f,indent=2)
 print(json.dumps(report),flush=True)
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master)
 with (root/'helper-access-grant-tui-b7fe053.ansi').open('xb') as f:f.write(transcript)
 with (root/'helper-access-grant-tui-b7fe053.txt').open('x') as f:f.write(re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace'))
 print(json.dumps({'transcriptSHA256':hashlib.sha256(transcript).hexdigest(),'operationID':job_id,'noAutomaticFallbackACLWrite':True}),flush=True)
