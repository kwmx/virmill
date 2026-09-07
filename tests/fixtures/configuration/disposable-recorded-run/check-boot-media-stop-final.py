import json,os,pathlib,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};vm='a19bf9ee-cd7f-4921-baac-39ce1694eb35';jid='4e342132-fc81-42e0-9e84-9ca002a51bd7'
for _ in range(120):
 r=subprocess.run(['virmill','operation','show',jid,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True,timeout=60);j=json.loads(r.stdout)['data']
 if j['state'] in ('succeeded','failed','canceled','partial','recovery-required'):break
 time.sleep(.5)
(root/'boot-media-disk-stop-final.json').write_text(r.stdout);print(r.stdout,flush=True)
state=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip();print(json.dumps({'nativeState':state,'observationOnly':True,'mutationReplayed':False}),flush=True)
assert j['state']=='succeeded' and state=='shut off'
