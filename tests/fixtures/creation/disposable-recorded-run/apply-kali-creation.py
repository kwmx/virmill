import json,os,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
p=json.loads((root/'creation-plan-c9aa310.json').read_text())['data']
assert p['planID']=='1a386ed9-6d7d-4395-a990-15ac1ffacb46' and p['planDigest']=='1286db190f9a8c00e4521c766ee37c8ff35da3a3635d6fb77cd69c542ff33fb4'
assert p['review']['target']['spec']['nics']==[]
assert set(p['acknowledgements'])=={'host-mutation','copy-managed-volumes','new-vm-identity'}
r=subprocess.run(['virmill','plan','apply',p['planID'],'--digest',p['planDigest'],'--idempotency-key','qualification-kali-create-001','--ack','host-mutation','--ack','copy-managed-volumes','--ack','new-vm-identity','--detach','--output','json','--non-interactive','--timeout','10m'],env=env,text=True,capture_output=True,timeout=650)
(root/'creation-accepted.json').write_text(r.stdout);print(r.stdout+r.stderr,flush=True);assert r.returncode==0
