import hashlib,json,os,pathlib,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
package=root/'packages/12d7bba/virmill-0.0.0-0.dev.x86_64.rpm'
assert hashlib.sha256(package.read_bytes()).hexdigest()=='4510142395eda74615931e67b5b99f2d9a144f9fe5335ea299c1c8a5df9a63ae'
def cli(*args):
 r=subprocess.run(['virmill',*args,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=60);print(r.stdout+r.stderr,flush=True);return r
r=cli('operation','list');assert r.returncode==0
jobs=json.loads(r.stdout)['data']
assert all(j['state'] in ('succeeded','failed','canceled','partial') or (j['operationID']=='b5983e68-bfcc-42f0-bf4e-3877029b756b' and j['state']=='recovery-required') for j in jobs)
before={}
for id in ('2ec994ce-2950-498c-8b19-d2f7dbb53a78','ae630461-91d3-4f07-ad88-e6842c3dc3ea','a19bf9ee-cd7f-4921-baac-39ce1694eb35'):
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
 before[id]=hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id])).hexdigest()
assert subprocess.check_output(['systemctl','--user','show','<test-vm-login>-11a8174.service','--property=WorkingDirectory','--value'],text=True).strip()==str(root)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
subprocess.run(['systemctl','--user','stop','<test-vm-login>-11a8174.service'],check=True)
subprocess.run(['sudo','-n','rpm','-Uvh','--replacepkgs',str(package)],check=True)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
assert hashlib.sha256(pathlib.Path('/usr/bin/virmill').read_bytes()).hexdigest()=='39510e88c07083d4180e695b76c9d3d3dcb33ce5043703d0bc3c499e0f011145'
assert hashlib.sha256(pathlib.Path('/usr/bin/virmilld').read_bytes()).hexdigest()=='d514cb9348f69779f7aa29016d0f4bdc979b16f5f0534ec2a15521310ed12170'
args=['systemd-run','--user','--unit=<test-vm-login>-12d7bba','--property=RuntimeMaxSec=7200','--property=Restart=no','--property=WorkingDirectory='+str(root)]
for key in ('XDG_STATE_HOME','XDG_RUNTIME_DIR','XDG_CACHE_HOME','XDG_DATA_HOME','XDG_CONFIG_HOME'):args.append('--setenv='+key+'='+env[key])
subprocess.run([*args,'/usr/bin/virmilld'],check=True)
for _ in range(100):
 if pathlib.Path(env['XDG_RUNTIME_DIR'],'virmill/control.sock').exists(): break
 time.sleep(.05)
r=cli('version');assert r.returncode==0;(root/'version-12d7bba.json').write_text(r.stdout)
r=cli('operation','list');assert r.returncode==0
assert [(j['operationID'],j['state']) for j in json.loads(r.stdout)['data']]==[(j['operationID'],j['state']) for j in jobs]
for id,digest in before.items():
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id])).hexdigest()==digest
print(json.dumps({'upgrade':'passed','retainedXMLSHA256':before,'newUnit':'<test-vm-login>-12d7bba.service','existingJobsPreserved':True,'guestBootTested':False}),flush=True)
