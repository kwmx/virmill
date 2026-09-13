import json,os,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text())}
r=subprocess.run(['virmill','import','inspect',str(pathlib.Path.home()/'images/<owner-media-1>.ova'),'--output','json','--non-interactive','--timeout','10m'],env=env,text=True,capture_output=True,timeout=650)
(root/'ova-inspection-c9aa310.json').write_text(r.stdout)
print(r.stdout+r.stderr,flush=True)
print('CLI exit code:',r.returncode)
