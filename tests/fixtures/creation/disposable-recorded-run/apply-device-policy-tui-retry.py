import errno,fcntl,json,os,pathlib,pty,re,select,struct,subprocess,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'}
plan=json.loads((root/'creation-plan-c7f8b76.json').read_text())['data']
assert plan['planID']=='bb894a45-a59a-410d-8fac-51e516c3979e' and plan['planDigest']=='9cb358387a94be2fb1253d7760a78457bcd7a3bda6fdd0402e9a5dbd0c372bef'
assert plan['operation']=='vm.create.devices-v1'
assert set(plan['acknowledgements'])=={'host-mutation','copy-managed-volumes','new-vm-identity','creation-device-policy'}
spec=plan['review']['target']['spec']
assert spec['uuid']=='a19bf9ee-cd7f-4921-baac-39ce1694eb35' and spec['poolID']=='eede6ba7-13a9-48d6-8cba-b8611d36513b' and spec['nics']==[]
assert spec['devicePolicy']=={'audio':'none','chipset':'q35','input':'ps2','memoryBalloon':'none','pciPlacement':'libvirt-auto','serial':'isa-serial','usbController':'none','version':1,'watchdogAction':'none'}
assert len(plan['review']['volumes'])==1 and plan['review']['volumes'][0]['sha256']=='8eb9be4a9592ad8242fa4e99fbac2dd2eef474bb93e99f3699d17617c7d93c70'
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
def wait_for(needle,timeout=15):
 end=time.monotonic()+timeout
 while time.monotonic()<end:
  drain()
  if needle in transcript:return
  if p.poll() is not None:break
 raise AssertionError('TUI expected output not received: '+repr(needle))
try:
 wait_for(b'Development build')
 os.write(master,b'\t'*8+b'\x1b[B'*5+b'\r');wait_for(b'Input:')
 os.write(master,plan['planID'].encode()+b'\r');wait_for(b'Plan is a preview.')
 for _ in range(40):
  if b'watchdogAction' in transcript:break
  os.write(master,b'\x1b[6~');drain()
 assert b'watchdogAction' in transcript and b'creation-device-policy' in transcript
 os.write(master,b'a');wait_for(b'Type the full plan digest')
 os.write(master,plan['planDigest'].encode()+b'\r');wait_for(b'"operationID"',timeout=650)
 os.write(master,b'q');assert p.wait(timeout=10)==0
 r=subprocess.run(['virmill','operation','list','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True)
 jobs=[j for j in json.loads(r.stdout)['data'] if j['planID']==plan['planID']];assert len(jobs)==1,jobs
 (root/'creation-accepted-c7f8b76.json').write_text(json.dumps({'data':jobs[0]},indent=2)+'\n')
 print(json.dumps({'nativePTY':True,'actions':['Jobs: plan show','page through reviewed device policy','acknowledge exact plan digest','detach TUI'],'operation':jobs[0],'mutationSubmitted':True,'guestBootTested':False}),flush=True)
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master)
 (root/'tui-device-policy-retry.ansi').write_bytes(transcript)
 text=re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace')
 (root/'tui-device-policy-retry.txt').write_text(text)
