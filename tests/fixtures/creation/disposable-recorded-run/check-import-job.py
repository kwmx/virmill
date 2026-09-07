import json,os,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
id=json.loads((root/'import-accepted.json').read_text())['data']['operationID']
r=subprocess.run(['virmill','operation','show',id,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=40)
print(r.stdout+r.stderr);(root/'import-status.json').write_text(r.stdout)
assert r.returncode==0
if json.loads(r.stdout)['data']['state']=='succeeded':
 r=subprocess.run(['virmill','import','result',id,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=40)
 (root/'import-result.json').write_text(r.stdout);print(r.stdout+r.stderr)
 assert r.returncode==0
