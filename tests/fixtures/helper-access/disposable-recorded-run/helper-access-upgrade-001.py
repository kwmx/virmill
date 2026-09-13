import hashlib,json,os,pathlib,sqlite3,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text())}
packages={
 'virmill-0.0.0-0.dev.x86_64.rpm':'5202794c47870ef3eb6a1f145c15a0a8c92c871b31a7aed89bd6a3113aaadadc',
 'virmill-host-helper-0.0.0-0.dev.x86_64.rpm':'ae6f01ba6121fd0a3ca6937bf153eaa96f7b74224e227ac578cf8868f792b93d'}
for name,sha in packages.items():assert hashlib.sha256((root/'packages/b7fe053'/name).read_bytes()).hexdigest()==sha
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
assert cli('version')['revision'].startswith('12d7bba')
assert subprocess.check_output(['systemctl','--user','show','<test-vm-login>-12d7bba.service','-p','WorkingDirectory','--value'],text=True).strip()==str(root)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
subprocess.run(['systemctl','--user','stop','<test-vm-login>-12d7bba.service'],check=True)
subprocess.run(['sudo','-n','rpm','-Uvh','--replacepkgs',*[str(root/'packages/b7fe053'/name) for name in packages]],check=True)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
for path,sha in {'/usr/bin/virmill':'4b9fbe8afbe286850add098a6c510d32dee23dd1a314885292dae857a22148a1','/usr/bin/virmilld':'8faf488b974a02108e35f00ea083bbadf0737e7913546b9cfdee210df1b8185c','/usr/libexec/virmill-host-helper':'4611ca879e17b35d2d0cc40bc6d6faea38a12d318910c165116d2633e840e4f8'}.items():
 assert hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()==sha
args=['systemd-run','--user','--unit=<test-vm-login>-b7fe053','--property=RuntimeMaxSec=7200','--property=Restart=no','--property=WorkingDirectory='+str(root)]
for key in ('XDG_STATE_HOME','XDG_RUNTIME_DIR','XDG_CACHE_HOME','XDG_DATA_HOME','XDG_CONFIG_HOME'):args.append('--setenv='+key+'='+env[key])
subprocess.run([*args,'/usr/bin/virmilld'],check=True)
for _ in range(100):
 if pathlib.Path(env['XDG_RUNTIME_DIR'],'virmill/control.sock').exists():break
 time.sleep(.05)
assert cli('version')['revision'].startswith('b7fe053')
assert rows('SELECT id,body FROM jobs ORDER BY id')==before_jobs
assert not rows('SELECT resource,job_id FROM locks')
for vm,sha in xml.items():assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()==sha
report={'upgrade':'passed','newUnit':'<test-vm-login>-b7fe053.service','existingJobsPreserved':len(before_jobs),'resourceLocks':0,'allFourStoppedVMXMLSHA256':xml,'noGuestStartOrDefinition':True,'SELinux':subprocess.check_output(['getenforce'],text=True).strip()}
with (root/'helper-access-upgrade-b7fe053.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
