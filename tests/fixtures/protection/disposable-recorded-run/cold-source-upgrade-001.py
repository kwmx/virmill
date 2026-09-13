import hashlib,json,os,pathlib,sqlite3,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text())}
packages={
 'virmill-0.0.0-0.dev.x86_64.rpm':'023a5b979aaae958bc3c2c3d2ecdcfb4e7726cb7b93e6167495359c4003fc570',
 'virmill-host-helper-0.0.0-0.dev.x86_64.rpm':'2b933de7e5073748d276244dc9f13004221f555bbce86ac33ea4efc6faf3abad'}
for name,sha in packages.items():assert hashlib.sha256((root/'packages/44a46b6'/name).read_bytes()).hexdigest()==sha
def cli(*args):
 p=subprocess.run(['virmill',*args,'--output','json','--non-interactive'],env=env,text=True,capture_output=True,timeout=30)
 if p.returncode:print(p.stdout+p.stderr,flush=True)
 p.check_returncode();return json.loads(p.stdout)['data']
def rows(query):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(query))
assert subprocess.run(['sudo','-n','true'],check=True).returncode==0
assert subprocess.run(['systemctl','is-active','--quiet','virmill-host-helper.service']).returncode!=0
assert subprocess.run(['systemctl','is-active','--quiet','virmill-host-helper.socket']).returncode!=0
before_jobs=rows('SELECT id,body FROM jobs ORDER BY id')
assert all(json.loads(body)['state'] in ('succeeded','failed','canceled','partial') for _,body in before_jobs)
assert not rows('SELECT resource,job_id FROM locks')
xml={}
for vm in ('2ec994ce-2950-498c-8b19-d2f7dbb53a78','ae630461-91d3-4f07-ad88-e6842c3dc3ea','a19bf9ee-cd7f-4921-baac-39ce1694eb35','b9496482-2eeb-40e1-892b-4e291c108c52'):
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
 xml[vm]=hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()
assert cli('version')['revision'].startswith('def0b10')
loaded=subprocess.check_output(['systemctl','--user','show','<test-vm-login>-def0b10.service','-p','LoadState','--value'],text=True).strip()
if loaded!='not-found':assert subprocess.check_output(['systemctl','--user','show','<test-vm-login>-def0b10.service','-p','WorkingDirectory','--value'],text=True).strip()==str(root)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
if loaded!='not-found':subprocess.run(['systemctl','--user','stop','<test-vm-login>-def0b10.service'],check=True)
subprocess.run(['sudo','-n','rpm','-Uvh','--replacepkgs',*[str(root/'packages/44a46b6'/name) for name in packages]],check=True)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
for path,sha in {'/usr/bin/virmill':'dc1aded072ae761798a2a8ee89fb14a19a8552da77a7c7b24ed6522b758cc217','/usr/bin/virmilld':'08b9d71a4b68c343a4c34fa56e2fdf549c544aeadbd155e9710e4e17bbc35e3e','/usr/libexec/virmill-host-helper':'56692aa05abbf3f3aaf1c1aa3146fcb6cb3135196be9fdd24a46e553c57d095a'}.items():
 assert hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()==sha
args=['systemd-run','--user','--unit=<test-vm-login>-44a46b6','--property=RuntimeMaxSec=7200','--property=Restart=no','--property=WorkingDirectory='+str(root)]
for key in ('XDG_STATE_HOME','XDG_RUNTIME_DIR','XDG_CACHE_HOME','XDG_DATA_HOME','XDG_CONFIG_HOME'):args.append('--setenv='+key+'='+env[key])
subprocess.run([*args,'/usr/bin/virmilld'],check=True)
for _ in range(100):
 if pathlib.Path(env['XDG_RUNTIME_DIR'],'virmill/control.sock').exists():break
 time.sleep(.05)
assert cli('version')['revision'].startswith('44a46b6')
assert rows('SELECT id,body FROM jobs ORDER BY id')==before_jobs
assert not rows('SELECT resource,job_id FROM locks')
for vm,sha in xml.items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()==sha
assert subprocess.run(['systemctl','is-active','--quiet','virmill-host-helper.service']).returncode!=0
assert subprocess.run(['systemctl','is-active','--quiet','virmill-host-helper.socket']).returncode!=0
report={'upgrade':'passed','newUnit':'<test-vm-login>-44a46b6.service','existingJobsPreserved':len(before_jobs),'resourceLocks':0,'allFourStoppedVMXMLSHA256':xml,'noGuestStartOrDefinition':True,'SELinux':subprocess.check_output(['getenforce'],text=True).strip()}
with (root/'cold-source-upgrade-44a46b6.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
