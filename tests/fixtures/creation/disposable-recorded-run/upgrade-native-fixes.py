import hashlib,json,os,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
package=root/'packages/c9aa310/virmill-0.0.0-0.dev.x86_64.rpm'
assert hashlib.sha256(package.read_bytes()).hexdigest()=='6a47d010cdffb446ea430aa787c552cb2a8106b35f6c31e6c86af9730a6dd92c'
r=subprocess.run(['virmill','operation','list','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True)
response=json.loads(r.stdout);assert response['error'] is None
assert all(j['state'] in ('succeeded','failed','canceled','partial') for j in response['data']),response
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
subprocess.run(['systemctl','--user','stop','<test-vm-login>-65930c6.service'],check=True)
subprocess.run(['sudo','-n','rpm','-Uvh','--replacepkgs',str(package)],check=True)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
args=['systemd-run','--user','--unit=<test-vm-login>-c9aa310','--property=RuntimeMaxSec=7200','--property=Restart=no','--property=WorkingDirectory='+str(root)]
for key in ('XDG_STATE_HOME','XDG_RUNTIME_DIR','XDG_CACHE_HOME','XDG_DATA_HOME','XDG_CONFIG_HOME'):args.append('--setenv='+key+'='+env[key])
subprocess.run([*args,'/usr/bin/virmilld'],check=True)
r=subprocess.run(['virmill','version','--output','json'],capture_output=True,text=True,check=True);print(r.stdout)
(root/'version-c9aa310.json').write_text(r.stdout)
