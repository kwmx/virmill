import hashlib,json,os,pathlib,sqlite3,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907'
baseline=json.loads((root/'discovery-upgrade-def0b10.json').read_text())
report={'readOnly':True,'guests':{},'firmware':[]}
for vm,digest in baseline['allFourStoppedVMXMLSHA256'].items():
    state=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()
    xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])
    assert state=='shut off' and hashlib.sha256(xml).hexdigest()==digest
    report['guests'][vm]={'state':state,'xmlSHA256':digest}
for p in sorted(pathlib.Path('/usr/share/qemu/firmware').glob('*.json')):
    d=json.loads(p.read_text())
    if 'uefi' in d.get('interface-types',[]) and 'secure-boot' not in d.get('features',[]):
        report['firmware'].append({'descriptor':str(p),'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'content':d})
report['coordinator']=subprocess.run(['systemctl','--user','show','<test-vm-login>-def0b10.service','-p','ActiveState','-p','SubState','-p','ExecMainStatus'],capture_output=True,text=True).stdout
report['helper']=subprocess.run(['systemctl','is-active','virmill-host-helper.socket','virmill-host-helper.service'],capture_output=True,text=True).stdout
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
    report['jobs']=[{'operationID':row[0], 'state':json.loads(row[1])['state']} for row in db.execute('SELECT id,body FROM jobs')]
    report['locks']=list(db.execute('SELECT resource,job_id FROM locks'))
assert not report['locks']
report['installed']={name:hashlib.sha256(pathlib.Path('/usr/bin',name).read_bytes()).hexdigest() for name in ('virmill','virmilld')}
print(json.dumps(report),flush=True)
