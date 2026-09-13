"""Parent-only disposable upgrade preserving a known uncertain creation operation."""
import hashlib,json,os,pathlib,re,sqlite3,subprocess,sys,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
assert len(sys.argv)==3
revision,manifest_sha=sys.argv[1:]
assert re.fullmatch('[a-f0-9]{40}',revision) and re.fullmatch('[a-f0-9]{64}',manifest_sha)
assert revision!='44a46b6222a601661eb993ba905b63d2396a2154'
bundle=root/'packages'/revision[:7];raw=(bundle/'deployment.json').read_bytes()
assert hashlib.sha256(raw).hexdigest()==manifest_sha
manifest=json.loads(raw);assert manifest['revision']==revision
packages=['virmill-0.0.0-0.dev.x86_64.rpm','virmill-host-helper-0.0.0-0.dev.x86_64.rpm']
for name in packages:assert hashlib.sha256((bundle/name).read_bytes()).hexdigest()==manifest['artifacts']['dist/'+name]
env={**os.environ,**json.loads((root/'environment.json').read_text())}
job_id='c809898c-e1be-45e2-b31d-644ad7bc75f0';vm='f78674f3-bf3a-43e5-81f9-4283e2472024'
def rows(query):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(query))
def cli(*args):
 p=subprocess.run(['virmill',*args,'--output','json','--non-interactive'],env=env,text=True,capture_output=True,timeout=30)
 if p.returncode:print(p.stdout+p.stderr,flush=True)
 p.check_returncode();return json.loads(p.stdout)['data']
def helper_inactive():
 for unit in ('virmill-host-helper.service','virmill-host-helper.socket'):
  assert subprocess.run(['systemctl','is-active','--quiet',unit]).returncode!=0
helper_inactive();subprocess.run(['sudo','-n','true'],check=True)
assert cli('version')['revision']=='44a46b6222a601661eb993ba905b63d2396a2154'
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
jobs=dict(rows('SELECT id,body FROM jobs ORDER BY id'));assert len(jobs)==26
assert json.loads(jobs[job_id])['state']=='recovery-required'
assert all(json.loads(body)['state'] in ('succeeded','failed','canceled','partial') for key,body in jobs.items() if key!=job_id)
locks=rows('SELECT resource,job_id FROM locks ORDER BY resource');assert len(locks)==3 and all(owner==job_id for _,owner in locks)
plans=rows('SELECT * FROM plans ORDER BY id');metadata=rows('SELECT * FROM metadata ORDER BY kind,id')
xml=json.loads((root/'cold-source-upgrade-44a46b6.json').read_text())['allFourStoppedVMXMLSHA256']
xml[vm]='36a51eefeb3f4b48f46f1308668b1e4c3cef3b19c03ba8b9fd7da6b70c61c500'
def preserve_guests():
 ids=set(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','list','--all','--uuid'],text=True).split())
 assert ids==set(xml)
 for key,sha in xml.items():
  assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',key],text=True).strip()=='shut off'
  assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',key])).hexdigest()==sha
 disk='/var/lib/libvirt/images/virmill-cold-probe-v1/virmill-'+vm+'-disk-000.qcow2'
 assert subprocess.check_output(['sudo','-n','sha256sum','--',disk],text=True).split()[0]=='f0eafe1814a7a137def96128e8f5bc15838669c868a25aa808f8d084c4382e51'
preserve_guests()
old='<test-vm-login>-44a46b6.service'
assert subprocess.check_output(['systemctl','--user','show',old,'-p','WorkingDirectory','--value'],text=True).strip()==str(root)
subprocess.run(['systemctl','--user','stop',old],check=True)
subprocess.run(['sudo','-n','rpm','-Uvh','--replacepkgs',*[str(bundle/name) for name in packages]],check=True)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
for command,path in {'virmill':'/usr/bin/virmill','virmilld':'/usr/bin/virmilld','virmill-host-helper':'/usr/libexec/virmill-host-helper'}.items():
 assert hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()==manifest['artifacts']['build/bin/'+command]
unit='<test-vm-login>-'+revision[:7]
args=['systemd-run','--user','--unit='+unit,'--property=RuntimeMaxSec=7200','--property=Restart=no','--property=WorkingDirectory='+str(root)]
for key in ('XDG_STATE_HOME','XDG_RUNTIME_DIR','XDG_CACHE_HOME','XDG_DATA_HOME','XDG_CONFIG_HOME'):args.append('--setenv='+key+'='+env[key])
subprocess.run([*args,'/usr/bin/virmilld'],check=True)
for _ in range(100):
 if pathlib.Path(env['XDG_RUNTIME_DIR'],'virmill/control.sock').exists():break
 time.sleep(.05)
assert cli('version')['revision']==revision
assert subprocess.run(['systemctl','--user','is-active','--quiet',unit+'.service']).returncode==0
assert cli('operation','show',job_id)['state']=='recovery-required'
assert rows('SELECT * FROM plans ORDER BY id')==plans
assert rows('SELECT * FROM metadata ORDER BY kind,id')==metadata
assert rows('SELECT resource,job_id FROM locks ORDER BY resource')==locks
now=dict(rows('SELECT id,body FROM jobs ORDER BY id'));assert set(now)==set(jobs)
assert all(now[key]==body for key,body in jobs.items() if key!=job_id)
assert json.loads(now[job_id])['state']=='recovery-required'
preserve_guests();helper_inactive()
report={'upgrade':'passed','revision':revision,'newUnit':unit+'.service','existingTerminalJobsPreserved':25,'knownRecoveryJobID':job_id,'knownRecoveryJobState':'recovery-required','recoveryJobBodyUnchanged':now[job_id]==jobs[job_id],'allPlansAndMetadataUnchanged':True,'locksPreserved':locks,'allFiveStoppedVMXMLSHA256':xml,'probeDiskSHA256':'f0eafe1814a7a137def96128e8f5bc15838669c868a25aa808f8d084c4382e51','noGuestStartOrDefinitionOrReconciliation':True,'SELinux':subprocess.check_output(['getenforce'],text=True).strip(),'nativePackages':subprocess.check_output(['rpm','-q','libvirt-daemon-kvm','qemu-kvm','edk2-ovmf','swtpm','restic'],text=True).splitlines()}
with (root/'cold-fixes-upgrade.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
