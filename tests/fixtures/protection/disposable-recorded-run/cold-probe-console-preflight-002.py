import hashlib,json,os,pathlib,sqlite3,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
vm='f78674f3-bf3a-43e5-81f9-4283e2472024';observer=root/'sources/uefi-probe-observer-v2/observe_console.py'
assert hashlib.sha256(observer.read_bytes()).hexdigest()=='4d2232a9570e5089fb5704db1f7f398ec3702a48027526144f5978603a415261'
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()=='36a51eefeb3f4b48f46f1308668b1e4c3cef3b19c03ba8b9fd7da6b70c61c500'
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 jobs=list(db.execute('SELECT id,body FROM jobs ORDER BY id'));assert len(jobs)==26 and not list(db.execute('SELECT resource,job_id FROM locks'))
p=subprocess.run(['python3',str(observer),'--uuid',vm,'--uri','qemu:///system','--expect','SEEDED','--log',str(root/'cold-probe-console-preflight-002.raw'),'--timeout','2','--attempts','1'],text=True,capture_output=True,timeout=5)
observation=json.loads(p.stdout)
with (root/'cold-probe-console-preflight-002.json').open('x') as f:json.dump(observation,f,indent=2)
print(json.dumps({'observerExitCode':p.returncode,'observation':observation}),flush=True)
assert p.returncode==1 and observation['status']=='attempt_limit' and observation['log_bytes']==0
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
p=subprocess.run(['virmill','vm','start',vm,'--plan','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=30)
assert p.returncode==0,p.stdout+p.stderr;plan=json.loads(p.stdout)['data']
assert plan['operation']=='vm.start' and plan['connectionID']=='qemu:///system'
with (root/'cold-probe-start-1-plan.json').open('x') as f:json.dump(plan,f,indent=2)
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 assert list(db.execute('SELECT id,body FROM jobs ORDER BY id'))==jobs and not list(db.execute('SELECT resource,job_id FROM locks'))
print(json.dumps({'revision':'ad608076cd928e6c6c270e6442cbc2d8047e6e0c','consoleStoppedRetryDiagnosticRecognized':True,'nativeGuestUnchanged':True,'startPlan':plan}),flush=True)
