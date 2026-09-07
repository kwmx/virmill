import hashlib,json,os,pathlib,sqlite3,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
env={**os.environ,**json.loads((root/'environment.json').read_text())}
vm='f78674f3-bf3a-43e5-81f9-4283e2472024';job='c809898c-e1be-45e2-b31d-644ad7bc75f0'
xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])
state=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip();assert state=='shut off'
with (root/'cold-probe-mismatch.xml').open('xb') as f:f.write(xml)
def cli(*args):
 p=subprocess.run(['virmill',*args,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=30)
 return {'exitCode':p.returncode,'response':json.loads(p.stdout)}
report={'readOnly':True,'vmID':vm,'operationID':job,'state':state,'nativeXML':xml.decode(),'xmlSHA256':hashlib.sha256(xml).hexdigest(),'operation':cli('operation','show',job),'inspection':cli('vm','recovery','inspect',vm),'creationResult':cli('vm','creation','result',job)}
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 report['locks']=list(db.execute('SELECT resource,job_id FROM locks'))
 report['metadataKeys']=[r[0] for r in db.execute('SELECT key FROM metadata')]
for old,sha in json.loads((root/'cold-source-upgrade-44a46b6.json').read_text())['allFourStoppedVMXMLSHA256'].items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',old],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',old])).hexdigest()==sha
report['allFourExistingStoppedXMLPreserved']=True
with (root/'cold-probe-mismatch-observation.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
