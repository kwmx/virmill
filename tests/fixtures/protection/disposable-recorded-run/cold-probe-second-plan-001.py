import hashlib,json,os,pathlib,sqlite3,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
vm='f78674f3-bf3a-43e5-81f9-4283e2472024';observer=root/'sources/uefi-probe-observer-v3/observe_console.py'
assert hashlib.sha256(observer.read_bytes()).hexdigest()=='dc96fbc1ca5dae19be1d1b6f8f9a6a1101ac95d133442e4227d12804a251c5e6'
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()=='081324fb849b275fafacfdf7efc6e22581f1ea926e87b75e80af8f0af688b31c'
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 jobs=list(db.execute('SELECT id,body FROM jobs ORDER BY id'));assert len(jobs)==27 and not list(db.execute('SELECT resource,job_id FROM locks'))
p=subprocess.run(['virmill','vm','start',vm,'--plan','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=30);assert p.returncode==0,p.stdout+p.stderr
plan=json.loads(p.stdout)['data'];assert plan['operation']=='vm.start' and plan['review']['vmID']==vm
with (root/'cold-probe-start-2-plan.json').open('x') as f:json.dump(plan,f,indent=2)
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:assert list(db.execute('SELECT id,body FROM jobs ORDER BY id'))==jobs
print(json.dumps({'revision':'ad608076cd928e6c6c270e6442cbc2d8047e6e0c','previewOnly':True,'firstMarkerObserved':False,'startPlan':plan}),flush=True)
