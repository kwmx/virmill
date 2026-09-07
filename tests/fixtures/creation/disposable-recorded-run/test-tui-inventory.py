import errno,fcntl,json,os,pathlib,pty,re,select,struct,subprocess,termios,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'}
master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',60,160,0,0))
p=subprocess.Popen(['virmill','tui','--connection','qemu:///system'],stdin=slave,stdout=slave,stderr=slave,env=env,start_new_session=True)
os.close(slave)
transcript=bytearray()
def wait_for(needle,timeout=15):
 end=time.monotonic()+timeout
 while time.monotonic()<end:
  if select.select([master],[],[],0.1)[0]:
   try:part=os.read(master,65536)
   except OSError as e:
    if e.errno==errno.EIO:break
    raise
   if not part:break
   transcript.extend(part)
   if needle in transcript:return
  if p.poll() is not None:break
 raise AssertionError('TUI expected output not received: '+repr(needle))
try:
 wait_for(b'Development build')
 os.write(master,b'\t\r')
 wait_for(b'virmill-nested-fedora')
 os.write(master,b'\t'*7+b'\r')
 wait_for(b'9b0fafe3-fc03-42bb-88fe-6ca70247a177')
 os.write(master,b'q')
 code=p.wait(timeout=10);assert code==0,code
 (root/'tui-inventory.ansi').write_bytes(transcript)
 text=re.sub(rb'\x1b\[[0-?]*[ -/]*[@-~]',b'',transcript).decode(errors='replace')
 (root/'tui-inventory.txt').write_text(text)
 print(json.dumps({'exitCode':code,'nativePTY':True,'actions':['VMs: vm list','Jobs: operation list'],'existingGuestVisible':True,'realImportJobVisible':True,'mutationSubmitted':False,'fullTUIAcceptance':False}))
finally:
 if p.poll() is None:p.terminate();p.wait(timeout=10)
 os.close(master)
 (root/'tui-inventory.ansi').write_bytes(transcript)
