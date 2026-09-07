import json,os,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
id=json.loads((root/'creation-accepted-c7f8b76.json').read_text())['data']['operationID']
for label,args in [('creation-status-c7f8b76',['operation','show',id]),('creation-result-c7f8b76',['vm','creation','result',id])]:
 r=subprocess.run(['virmill',*args,'--output','json','--non-interactive','--timeout','60s'],env=env,text=True,capture_output=True,timeout=70)
 (root/(label+'.json')).write_text(r.stdout);print(label,r.stdout+r.stderr,flush=True)
 assert r.returncode==0 or (label=='creation-result-c7f8b76' and json.loads(r.stdout)['error']['code']=='RECOVERY_REQUIRED')
