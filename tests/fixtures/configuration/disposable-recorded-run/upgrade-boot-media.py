import hashlib,json,os,pathlib,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
package=root/'packages/8a092b4/virmill-0.0.0-0.dev.x86_64.rpm'
assert hashlib.sha256(package.read_bytes()).hexdigest()=='095497d774af42f2829b4d792d894a23d925d78bdf9ca72378e5fa687ae4fe5d'
def cli(*args):
 r=subprocess.run(['virmill',*args,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=60);print(r.stdout+r.stderr,flush=True);return r
r=cli('operation','list');assert r.returncode==0
jobs=json.loads(r.stdout)['data']
assert all(j['state'] in ('succeeded','failed','canceled','partial') or (j['operationID']=='b5983e68-bfcc-42f0-bf4e-3877029b756b' and j['state']=='recovery-required') for j in jobs)
before={}
for id in ('2ec994ce-2950-498c-8b19-d2f7dbb53a78','ae630461-91d3-4f07-ad88-e6842c3dc3ea','a19bf9ee-cd7f-4921-baac-39ce1694eb35'):
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
 before[id]=hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id])).hexdigest()
assert subprocess.check_output(['systemctl','--user','show','<test-vm-login>-c7f8b76.service','--property=WorkingDirectory','--value'],text=True).strip()==str(root)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
subprocess.run(['systemctl','--user','stop','<test-vm-login>-c7f8b76.service'],check=True)
subprocess.run(['sudo','-n','rpm','-Uvh','--replacepkgs',str(package)],check=True)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
assert hashlib.sha256(pathlib.Path('/usr/bin/virmill').read_bytes()).hexdigest()=='f99abf89ab4893aeee576b450b0ca6adedb94f45cfffb84166448ca18640c699'
assert hashlib.sha256(pathlib.Path('/usr/bin/virmilld').read_bytes()).hexdigest()=='58aa6bada39c3780b8456f8972d942ef766f5470d5c9eaebb4f27b500043b41b'
args=['systemd-run','--user','--unit=<test-vm-login>-8a092b4','--property=RuntimeMaxSec=7200','--property=Restart=no','--property=WorkingDirectory='+str(root)]
for key in ('XDG_STATE_HOME','XDG_RUNTIME_DIR','XDG_CACHE_HOME','XDG_DATA_HOME','XDG_CONFIG_HOME'):args.append('--setenv='+key+'='+env[key])
subprocess.run([*args,'/usr/bin/virmilld'],check=True)
for _ in range(100):
 if pathlib.Path(env['XDG_RUNTIME_DIR'],'virmill/control.sock').exists(): break
 time.sleep(.05)
r=cli('version');assert r.returncode==0;(root/'version-8a092b4.json').write_text(r.stdout)
r=cli('operation','list');assert r.returncode==0
assert [(j['operationID'],j['state']) for j in json.loads(r.stdout)['data']]==[(j['operationID'],j['state']) for j in jobs]
for id,digest in before.items():
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id])).hexdigest()==digest
print(json.dumps({'upgrade':'passed','retainedXMLSHA256':before,'newUnit':'<test-vm-login>-8a092b4.service','existingJobsPreserved':True,'guestBootTested':False}),flush=True)
