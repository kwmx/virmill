import hashlib,json,os,pathlib,sqlite3,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
vm='ae630461-91d3-4f07-ad88-e6842c3dc3ea';parent='b5983e68-bfcc-42f0-bf4e-3877029b756b'
policy={'devicePolicy':{'version':1,'chipset':'q35','pciPlacement':'libvirt-auto','usbController':'qemu-xhci','memoryBalloon':'virtio','watchdogAction':'reset','input':'ps2','audio':'none','serial':'isa-serial'}}
def journal():
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return (list(db.execute('SELECT id,body FROM jobs ORDER BY id')),list(db.execute('SELECT resource,job_id FROM locks ORDER BY resource')),list(db.execute('SELECT id FROM plans ORDER BY id')))
before=journal();assert len(before[1])==3 and all(job==parent for _,job in before[1])
xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);assert hashlib.sha256(xml).hexdigest()=='e3200ea179e0cee3258b413baf8668529f2c1fdbed4535115a674d1b4d40d824'
r=subprocess.run(['virmill','vm','creation','accept',parent,'--input',json.dumps(policy),'--plan','--connection','qemu:///system','--timeout','10m','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=660)
(root/'acceptance-permission-refusal-12d7bba.json').write_text(r.stdout)
print(r.stdout+r.stderr,flush=True)
assert r.returncode==4 and json.loads(r.stdout)['error']['code']=='PERMISSION_DENIED'
assert journal()==before
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])==xml
print(json.dumps({'permissionRefusal':'passed','planJobAndLockStateUnchanged':True,'oldLocksRetained':3,'guestXMLUnchanged':True,'VMRemainsStopped':subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'}),flush=True)
