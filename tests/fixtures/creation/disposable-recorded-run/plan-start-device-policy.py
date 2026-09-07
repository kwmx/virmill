import hashlib,json,os,pathlib,subprocess,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};id='a19bf9ee-cd7f-4921-baac-39ce1694eb35'
r=subprocess.run(['virmill','vm','creation','result','28701ac3-325d-4596-8103-38e19f74b7cb','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True)
result=json.loads(r.stdout)['data'];assert result['operation']['state']=='succeeded' and result['receipt']['defined'] and result['receipt']['volumesVerified']
assert not result['guestBootVerified']
x=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id],text=True);tree=ET.fromstring(x)
assert tree.findtext('uuid')==id and tree.find('devices').findall('interface')==[]
assert tree.find('devices/watchdog').attrib=={'model':'itco','action':'none'}
assert tree.find('devices/memballoon').attrib=={'model':'none'}
assert tree.find("devices/controller[@type='usb']").attrib['model']=='none'
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
(root/'device-policy-defined.xml').write_text(x)
print(json.dumps({'definedXMLSHA256':hashlib.sha256(x.encode()).hexdigest(),'vmID':id,'noNICs':True,'watchdogAction':'none','usbController':'none','balloon':'none','poweredOff':True}),flush=True)
r=subprocess.run(['virmill','vm','show',id,'--connection','qemu:///system','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True)
(root/'device-policy-vm-before-start.json').write_text(r.stdout);print(r.stdout,flush=True)
r=subprocess.run(['virmill','vm','start',id,'--connection','qemu:///system','--plan','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True)
(root/'device-policy-start-plan.json').write_text(r.stdout);print(r.stdout,flush=True)
