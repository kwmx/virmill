import hashlib,json,os,pathlib,sqlite3,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
assert hashlib.sha256(pathlib.Path('/usr/bin/virmill').read_bytes()).hexdigest()=='cb22b33ebb70e28d9c0b5ab95934c021f396051e8d24b4f69eae233d103dded4'
request=json.loads((root/'cold-probe-creation-input.json').read_text());request['hardware']['name']='Virmill UEFI TPM 4M probe'
request['hardware']['firmware'].update(code='/usr/share/edk2/ovmf/OVMF_CODE_4M.qcow2',template='/usr/share/edk2/ovmf/OVMF_VARS_4M.qcow2',format='qcow2')
expected=json.loads((root/'cold-fixes-upgrade.json').read_text())['allFiveStoppedVMXMLSHA256'];expected['f78674f3-bf3a-43e5-81f9-4283e2472024']='081324fb849b275fafacfdf7efc6e22581f1ea926e87b75e80af8f0af688b31c'
for vm,sha in expected.items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()==sha
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 jobs=list(db.execute('SELECT id,body FROM jobs ORDER BY id'));assert len(jobs)==28 and not list(db.execute('SELECT resource,job_id FROM locks'))
p=subprocess.run(['virmill','vm','create','39078e3f-672b-4451-90dd-7f1fe1824e5a','--input',json.dumps(request),'--plan','--output','json','--non-interactive'],env=env,text=True,capture_output=True,timeout=60)
assert p.returncode==0,p.stdout+p.stderr
plan=json.loads(p.stdout)['data'];assert plan['review']['target']['spec']['uuid'] not in expected
with (root/'cold-probe-4m-creation-input.json').open('x') as f:json.dump(request,f,indent=2)
with (root/'cold-probe-4m-creation-plan.json').open('x') as f:json.dump(plan,f,indent=2)
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:assert list(db.execute('SELECT id,body FROM jobs ORDER BY id'))==jobs
print(json.dumps({'revision':'ad608076cd928e6c6c270e6442cbc2d8047e6e0c','previewOnly':True,'allFiveExistingStoppedXMLPreserved':expected,'creationPlan':plan}),flush=True)
