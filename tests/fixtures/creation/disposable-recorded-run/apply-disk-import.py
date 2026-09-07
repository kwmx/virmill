import json,os,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text())}
p=json.loads((root/'import-plan.json').read_text())['data']
assert p['planID']=='4aac67c5-2603-4a6c-87ec-edde57ef6cc6'
assert p['planDigest']=='dc0fa30f5881a0bf2a725f6d6b05a48db9e7d1b04caadf930f142c1ac7e91e8e'
assert set(p['acknowledgements'])=={'write-import-artifacts','offline-source-files'}
r=subprocess.run(['virmill','plan','apply',p['planID'],'--digest',p['planDigest'],'--idempotency-key','qualification-kali-qemu-import-001','--ack','write-import-artifacts','--ack','offline-source-files','--detach','--output','json','--non-interactive','--timeout','10m'],env=env,capture_output=True,text=True,timeout=650)
(root/'import-accepted.json').write_text(r.stdout)
print(r.stdout+r.stderr,flush=True)
assert r.returncode==0
