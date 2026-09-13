import hashlib,json,os,pathlib,sqlite3,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text())}
vm='b9496482-2eeb-40e1-892b-4e291c108c52'
def cli(*args,check=True):
 p=subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive'],env=env,text=True,capture_output=True,timeout=30)
 if check:p.check_returncode()
 return p
assert json.loads(cli('version').stdout)['data']['revision'].startswith('b7fe053')
denied=cli('storage','access','grant',vm,'--input','{"target":"vda","rootID":"images"}','--plan',check=False)
print(json.dumps({'unconfiguredPreviewExitCode':denied.returncode,'unconfiguredPreview':json.loads(denied.stdout)}),flush=True)
assert denied.returncode!=0
config=pathlib.Path(env['XDG_CONFIG_HOME'])/'virmill'
if not config.exists():config.mkdir(mode=0o700)
assert config.stat().st_uid==os.getuid() and config.stat().st_mode&0o777==0o700
key=config/'helper-key.pem'
with os.fdopen(os.open(key,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600),'wb') as f:
 subprocess.run(['openssl','genpkey','-algorithm','ED25519'],stdout=f,check=True)
 f.flush();os.fsync(f.fileno())
identity=json.loads(cli('host','helper','identity').stdout)['data']
assert identity['actorUID']==1000 and len(identity['keyID'])==64 and len(identity['publicKey'])==64 and identity['policyApproved'] is False
policy={'apiVersion':'virmill/v1','keys':{identity['keyID']:identity['publicKey']},'roots':{'images':'/var/lib/libvirt/images'},'actors':[1000]}
dropin='[Socket]\nSocketGroup=<test-vm-login>\nSocketMode=0660\nDirectoryMode=0755\n'
setup={'policy':policy,'dropin':dropin}
program='''import os,json,pathlib,sys
data=json.load(sys.stdin)
for name in ['/etc/virmill','/etc/systemd/system/virmill-host-helper.socket.d']:
 p=pathlib.Path(name)
 if not p.exists():p.mkdir(mode=0o755)
 st=p.lstat();assert st.st_uid==0 and st.st_mode&0o022==0 and p.is_dir() and not p.is_symlink()
for name,body in [('/etc/virmill/helper-policy.json',json.dumps(data['policy'],sort_keys=True)+'\\n'),('/etc/systemd/system/virmill-host-helper.socket.d/50-virmill-disposable-test.conf',data['dropin'])]:
 with os.fdopen(os.open(name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o644),'w') as f:f.write(body);f.flush();os.fsync(f.fileno())
'''
subprocess.run(['sudo','-n','python3','-c',program],input=json.dumps(setup),text=True,check=True)
subprocess.run(['sudo','-n','systemctl','daemon-reload'],check=True)
subprocess.run(['sudo','-n','systemctl','start','virmill-host-helper.socket'],check=True)
identity=json.loads(cli('host','helper','identity').stdout)['data'];assert identity['policyApproved'] is True
assert pathlib.Path('/run/virmill-host-helper/control.sock').stat().st_uid==0
report={'publicIdentity':identity,'policySHA256':hashlib.sha256(pathlib.Path('/etc/virmill/helper-policy.json').read_bytes()).hexdigest(),'dropInSHA256':hashlib.sha256(dropin.encode()).hexdigest(),'socketStartedNotEnabled':True,'existingGroupUsed':'<test-vm-login>','privateKeyContentsLogged':False,'SELinux':subprocess.check_output(['getenforce'],text=True).strip()}
with (root/'helper-access-setup-b7fe053.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
