import hashlib,json,os,pathlib,sqlite3,subprocess,sys
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
upgrade=json.loads((root/'cold-fixes-upgrade.json').read_text());revision=upgrade['revision']
assert revision==sys.argv[1]
vm='f78674f3-bf3a-43e5-81f9-4283e2472024';job_id='c809898c-e1be-45e2-b31d-644ad7bc75f0';plan_id='a7232967-796b-4526-9423-e51a9b4bf9d2'
def rows(query):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(query))
def call(*args):
 p=subprocess.run(['virmill',*args,'--output','json','--non-interactive','--timeout','2m'],env=env,text=True,capture_output=True,timeout=150)
 result=json.loads(p.stdout)
 if p.returncode:print(json.dumps({'command':args,'exitCode':p.returncode,'response':result}),flush=True)
 p.check_returncode();return result['data']
def preserve():
 assert set(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','list','--all','--uuid'],text=True).split())==set(upgrade['allFiveStoppedVMXMLSHA256'])
 for key,sha in upgrade['allFiveStoppedVMXMLSHA256'].items():
  assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',key],text=True).strip()=='shut off'
  assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',key])).hexdigest()==sha
 disk='/var/lib/libvirt/images/virmill-cold-probe-v1/virmill-'+vm+'-disk-000.qcow2'
 assert subprocess.check_output(['sudo','-n','sha256sum','--',disk],text=True).split()[0]==upgrade['probeDiskSHA256']
assert call('version')['revision']==revision
before=dict(rows('SELECT id,body FROM jobs ORDER BY id'));assert len(before)==26 and json.loads(before[job_id])['state']=='recovery-required'
locks=rows('SELECT resource,job_id FROM locks ORDER BY resource');assert locks==[tuple(x) for x in upgrade['locksPreserved']]
plans=rows('SELECT * FROM plans ORDER BY id')
old_receipt=json.loads((root/'cold-probe-mismatch-observation.json').read_text())['creationResult']['response']['data']['receipt'];assert not old_receipt['defined']
preserve()
job=call('operation','reconcile',job_id)
with (root/'cold-fixes-reconcile-response.json').open('x') as f:json.dump(job,f,indent=2)
print(json.dumps({'reconciledJob':job}),flush=True);assert job['state']=='succeeded'
result=call('vm','creation','result',job_id)
assert result['complete'] and result['receipt']['defined'] and result['receipt']['volumesVerified']
expected={**old_receipt,'defined':True};assert result['receipt']==expected
assert all(result[k] is False for k in ('guestBootVerified','setupVerified','connectivityVerified'))
assert not rows('SELECT resource,job_id FROM locks')
assert rows('SELECT * FROM plans ORDER BY id')==plans
now=dict(rows('SELECT id,body FROM jobs ORDER BY id'));assert set(now)==set(before)
assert all(now[key]==body for key,body in before.items() if key!=job_id)
original=call('plan','show',plan_id);assert original['planDigest']=='14ecac25ba06dfdc36ae7f2d75615fb9caf76982cedbf2e4e6dec70492630be1' and original['estimates']['additionalBytes']==0
inspection=call('vm','recovery','inspect',vm)
assert inspection['state']=='stopped' and not inspection['hasManagedSave'] and not inspection['autostart']
assert not any(x['kind']=='runtime-dependency' and x['target']=='domain/element[8]' for x in inspection['source']['externalDependencies'])
assert inspection['layout']['tpm']['sourcePath']==''
assert call('vm','show',vm)['ownership']=='managed'
preserve()
report={'revision':revision,'reconciledCreation':result,'allFiveStoppedGuestXMLAndProbeDiskPreserved':True,'other25JobsUnchanged':True,'allOriginalPlansUnchanged':True,'originalCreationEstimateRemainsZero':True,'remainingLocks':0,'coldInspection':inspection,'noDefinitionOrUploadReplayRequested':True,'guestStarted':False,'completeCaptureVerified':False,'nvramFirstPathBindingVerified':False}
with (root/'cold-fixes-reconcile.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
