import hashlib,json,os,pathlib,sqlite3,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text())}
packages={
 'virmill-0.0.0-0.dev.x86_64.rpm':'44da18810be97198213a6a67dd321dd715a97b142d7b9c12c0c8d5abacde114e',
 'virmill-host-helper-0.0.0-0.dev.x86_64.rpm':'c4e68b37288a7792748f15b0f1952ef66554a38d20a7aa01331ea0be599c0286'}
for name,sha in packages.items():assert hashlib.sha256((root/'packages/def0b10'/name).read_bytes()).hexdigest()==sha
def cli(*args):
 p=subprocess.run(['virmill',*args,'--output','json','--non-interactive'],env=env,text=True,capture_output=True,timeout=30)
 if p.returncode:print(p.stdout+p.stderr,flush=True)
 p.check_returncode();return json.loads(p.stdout)['data']
def rows(query):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(query))
before_jobs=rows('SELECT id,body FROM jobs ORDER BY id')
assert all(json.loads(body)['state'] in ('succeeded','failed','canceled','partial') for _,body in before_jobs)
assert not rows('SELECT resource,job_id FROM locks')
xml={}
for vm in ('2ec994ce-2950-498c-8b19-d2f7dbb53a78','ae630461-91d3-4f07-ad88-e6842c3dc3ea','a19bf9ee-cd7f-4921-baac-39ce1694eb35','b9496482-2eeb-40e1-892b-4e291c108c52'):
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
 xml[vm]=hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()
assert cli('version')['revision'].startswith('b7fe053')
loaded=subprocess.check_output(['systemctl','--user','show','virmill-test-b7fe053.service','-p','LoadState','--value'],text=True).strip()
if loaded!='not-found':assert subprocess.check_output(['systemctl','--user','show','virmill-test-b7fe053.service','-p','WorkingDirectory','--value'],text=True).strip()==str(root)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
if loaded!='not-found':subprocess.run(['systemctl','--user','stop','virmill-test-b7fe053.service'],check=True)
subprocess.run(['sudo','-n','rpm','-Uvh','--replacepkgs',*[str(root/'packages/def0b10'/name) for name in packages]],check=True)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
for path,sha in {'/usr/bin/virmill':'1e80e70a1d5fd63bf42f1c8c574c11756601f61a5dcb6d286d0aa8483d53251f','/usr/bin/virmilld':'6972253ae906702339bcdc84116be8c33569a6ff746b9d8fb5e4643eed92f34f','/usr/libexec/virmill-host-helper':'cb87eef14ba0229383e7df6453376241b57e6a1ac35e1240e015754399082c19'}.items():
 assert hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()==sha
args=['systemd-run','--user','--unit=virmill-test-def0b10','--property=RuntimeMaxSec=7200','--property=Restart=no','--property=WorkingDirectory='+str(root)]
for key in ('XDG_STATE_HOME','XDG_RUNTIME_DIR','XDG_CACHE_HOME','XDG_DATA_HOME','XDG_CONFIG_HOME'):args.append('--setenv='+key+'='+env[key])
subprocess.run([*args,'/usr/bin/virmilld'],check=True)
for _ in range(100):
 if pathlib.Path(env['XDG_RUNTIME_DIR'],'virmill/control.sock').exists():break
 time.sleep(.05)
assert cli('version')['revision'].startswith('def0b10')
assert rows('SELECT id,body FROM jobs ORDER BY id')==before_jobs
assert not rows('SELECT resource,job_id FROM locks')
for vm,sha in xml.items():assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()==sha
report={'upgrade':'passed','newUnit':'virmill-test-def0b10.service','existingJobsPreserved':len(before_jobs),'resourceLocks':0,'allFourStoppedVMXMLSHA256':xml,'noGuestStartOrDefinition':True,'SELinux':subprocess.check_output(['getenforce'],text=True).strip()}
with (root/'discovery-upgrade-def0b10.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
