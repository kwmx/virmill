import hashlib,json,os,pathlib,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
package=root/'packages/c7f8b76/virmill-0.0.0-0.dev.x86_64.rpm'
assert hashlib.sha256(package.read_bytes()).hexdigest()=='425a0c6f5a58e6efc889ba74c0506faa6be115f22235e0e62d7972fb458ae670'
def cli(*args):
 r=subprocess.run(['virmill',*args,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=60);print(r.stdout+r.stderr,flush=True);return r
r=cli('operation','list');assert r.returncode==0
jobs=json.loads(r.stdout)['data']
assert all(j['state'] in ('succeeded','failed','canceled','partial') or (j['operationID']=='b5983e68-bfcc-42f0-bf4e-3877029b756b' and j['state']=='recovery-required') for j in jobs)
before={}
for id in ('2ec994ce-2950-498c-8b19-d2f7dbb53a78','ae630461-91d3-4f07-ad88-e6842c3dc3ea'):
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
 before[id]=hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id])).hexdigest()
assert subprocess.check_output(['systemctl','--user','show','<test-vm-login>-c9aa310.service','--property=WorkingDirectory','--value'],text=True).strip()==str(root)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
subprocess.run(['systemctl','--user','stop','<test-vm-login>-c9aa310.service'],check=True)
subprocess.run(['sudo','-n','rpm','-Uvh','--replacepkgs',str(package)],check=True)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
assert hashlib.sha256(pathlib.Path('/usr/bin/virmill').read_bytes()).hexdigest()=='7bcd4e3ccedc465e712542751668628815937a5d8cde6c0506cd1e70ab3950e0'
assert hashlib.sha256(pathlib.Path('/usr/bin/virmilld').read_bytes()).hexdigest()=='913edb24ec8e8165452aa7838a87c58dccf82bd3165f8478ee5d4ba19c5611bb'
args=['systemd-run','--user','--unit=<test-vm-login>-c7f8b76','--property=RuntimeMaxSec=7200','--property=Restart=no','--property=WorkingDirectory='+str(root)]
for key in ('XDG_STATE_HOME','XDG_RUNTIME_DIR','XDG_CACHE_HOME','XDG_DATA_HOME','XDG_CONFIG_HOME'):args.append('--setenv='+key+'='+env[key])
subprocess.run([*args,'/usr/bin/virmilld'],check=True)
for _ in range(100):
 if pathlib.Path(env['XDG_RUNTIME_DIR'],'virmill/control.sock').exists(): break
 time.sleep(.05)
r=cli('version');assert r.returncode==0;(root/'version-c7f8b76.json').write_text(r.stdout)
r=cli('operation','list');assert r.returncode==0
assert [(j['operationID'],j['state']) for j in json.loads(r.stdout)['data']]==[(j['operationID'],j['state']) for j in jobs]
for id,digest in before.items():
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id])).hexdigest()==digest
print(json.dumps({'upgrade':'passed','retainedXMLSHA256':before,'newUnit':'<test-vm-login>-c7f8b76.service','existingJobsPreserved':True,'guestBootTested':False}),flush=True)
