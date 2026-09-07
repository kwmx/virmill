import json,os,pathlib,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
p=json.loads((root/'device-policy-start-plan.json').read_text())['data']
assert p['planID']=='d95d0e4b-2909-4904-b146-0faf39c35f54' and p['planDigest']=='3e62bf1b23a1b93b81b354cbf80aa3e23d3152fed188135dfa278b9b266eb86b'
assert p['operation']=='vm.start' and p['resourceIDs']==['libvirt|qemu:///system|vm|a19bf9ee-cd7f-4921-baac-39ce1694eb35'] and p['acknowledgements']==['host-mutation']
r=subprocess.run(['virmill','plan','apply',p['planID'],'--digest',p['planDigest'],'--idempotency-key','device-policy-kali-start-c7f8b76-001','--ack','host-mutation','--detach','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True)
(root/'device-policy-start-accepted.json').write_text(r.stdout);print(r.stdout,flush=True);id=json.loads(r.stdout)['data']['operationID']
for _ in range(20):
 r=subprocess.run(['virmill','operation','show',id,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True)
 j=json.loads(r.stdout)['data']
 if j['state'] not in ('queued','validating','running'):break
 time.sleep(.5)
print(r.stdout,flush=True);(root/'device-policy-start-result.json').write_text(r.stdout)
assert j['state']=='succeeded',j
print(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate','a19bf9ee-cd7f-4921-baac-39ce1694eb35'],text=True))
