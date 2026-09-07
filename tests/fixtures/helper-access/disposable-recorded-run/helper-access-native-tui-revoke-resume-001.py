import errno,fcntl,hashlib,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'}
grant=json.loads((root/'helper-access-grant-result-b7fe053.json').read_text())['grantOperationID']
before=json.loads((root/'helper-access-original-b7fe053.json').read_text())
disk=pathlib.Path('/var/lib/libvirt/images/virmill-multidisk-12d7bba/virmill-b9496482-2eeb-40e1-892b-4e291c108c52-disk-000.qcow2')
def cli(*args):
 p=subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive','--timeout','30s'],env=env,text=True,capture_output=True,check=True,timeout=45);return json.loads(p.stdout)['data']
def rows(sql):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(sql))
assert os.access(disk,os.R_OK) and not os.access(disk,os.W_OK)
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
 existing=rows("SELECT body FROM plans WHERE json_extract(body,'$.operation')='storage.revoke-read' ORDER BY rowid DESC LIMIT 1")
 assert len(existing)==1;existing_plan=json.loads(existing[0][0]);assert existing_plan['review']['originalGrantOperationID']==grant
 assert not [j for j in cli('operation','list') if j['planID']==existing_plan['planID']]
 wait_for(b'Overview')
 for _ in range(8):os.write(master,b'\t');drain()
 wait_for(b'plan show')
 for _ in range(5):os.write(master,b'\x1b[B');drain()
 os.write(master,b'\r');drain();os.write(master,existing_plan['planID'].encode()+b'\r')
 wait_for(b'Plan is a preview.')
 matches=rows("SELECT body,input FROM plans WHERE json_extract(body,'$.operation')='storage.revoke-read' ORDER BY rowid DESC LIMIT 1")
 assert len(matches)==1;plan=json.loads(matches[0][0]);inp=json.loads(matches[0][1])
 assert inp['access']['originalGrantJobID']==grant and plan['review']['originalGrantOperationID']==grant
 assert set(plan['acknowledgements'])=={'host-permission-change','exclusive-offline-volume'}
 assert plan['review']['desiredAccessACL']=='0200000001000600ffffffff04000000ffffffff20000000ffffffff'
 with (root/'helper-access-revoke-plan-b7fe053.json').open('x') as f:json.dump(plan,f,indent=2)
 for _ in range(12):os.write(master,b'\x1b[6~');drain(.03)
 assert b'host-permission-change' in transcript and b'exclusive-offline-volume' in transcript
 os.write(master,b'a');wait_for(b'Type the full plan digest');os.write(master,plan['planDigest'].encode()+b'\r')
 end=time.monotonic()+30
 while time.monotonic()<end:
  drain();jobs=[j for j in cli('operation','list') if j['planID']==plan['planID']]
  if jobs:job_id=jobs[0]['operationID'];break
 assert job_id is not None
 os.write(master,b'q');assert p.wait(timeout=10)==0
 end=time.monotonic()+30
 while time.monotonic()<end:
  job=cli('operation','show',job_id)
  if job['state'] in ('succeeded','failed','canceled','partial','recovery-required'):break
  time.sleep(.1)
 assert job['state']=='succeeded',job
 result=cli('storage','access','result',job_id);assert result['complete']
 st=disk.stat();assert st.st_dev==before['device'] and st.st_ino==before['inode'] and st.st_size==before['size'] and st.st_mtime_ns==before['mtimeNS']
 assert st.st_mode==before['mode'] and st.st_uid==before['uid'] and st.st_gid==before['gid']
 assert subprocess.check_output(['getfacl','--absolute-names','--numeric',str(disk)]).decode()==before['acl']
 assert not os.access(disk,os.R_OK) and not os.access(disk,os.W_OK)
 assert not rows('SELECT resource,job_id FROM locks')
 report={'tuiRevoke':'passed','originalGrantOperationID':grant,'revokeOperationID':job_id,'revokePlanID':plan['planID'],'exactOriginalAccessRestored':True,'fileGenerationAndByteMetadataUnchanged':True,'ordinaryActorReadRemoved':True,'allResourceLocksReleased':True,'tuiDetached':True,'SELinux':subprocess.check_output(['getenforce'],text=True).strip()}
 with (root/'helper-access-revoke-result-b7fe053.json').open('x') as f:json.dump(report,f,indent=2)
 print(json.dumps(report),flush=True)
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master)
 with (root/'helper-access-revoke-tui-resume-b7fe053.ansi').open('xb') as f:f.write(transcript)
 with (root/'helper-access-revoke-tui-resume-b7fe053.txt').open('x') as f:f.write(re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace'))
 print(json.dumps({'transcriptSHA256':hashlib.sha256(transcript).hexdigest(),'operationID':job_id,'noAutomaticFallbackACLWrite':True}),flush=True)
