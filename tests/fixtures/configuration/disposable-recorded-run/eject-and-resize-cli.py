import hashlib,json,os,pathlib,subprocess,time,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};vm='a19bf9ee-cd7f-4921-baac-39ce1694eb35'
def cli(*args):return subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True,timeout=60)
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
before=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);assert before==(root/'boot-order-applied.xml').read_bytes();b=ET.fromstring(before)
media='/var/lib/libvirt/images/virmill-policy-c7f8b76/virmill-boot-media-fixture.iso';isoSHA='6dbefacc95e3b556c19c48e8bae39b8b505e2d3a1aba0bfb7ab62b036c3d2ba3'
assert subprocess.check_output(['sudo','-n','sha256sum',media],text=True).split()[0]==isoSHA
requested={'vcpus':3,'memoryMiB':3072,'bootOrder':[{'kind':'disk','id':'vda'}],'ejectMedia':'sda','applyMode':'next-boot'}
r=cli('vm','set',vm,'--input',json.dumps(requested),'--plan');(root/'boot-media-eject-plan.json').write_text(r.stdout);print(r.stdout,flush=True);p=json.loads(r.stdout)['data']
assert p['operation']=='vm.configure-hardware' and p['resourceIDs']==['libvirt|qemu:///system|vm|'+vm]
assert set(p['acknowledgements'])=={'host-mutation','exclusive-configuration-writer','replace-boot-order','eject-retain-media'}
assert p['review']['requested']==requested and not p['review']['diskDeletion']
afterView=p['review']['afterBoot'];cd=next(d for d in afterView['devices'] if d['id']=='sda');assert cd['readOnly'] and not cd['mediaPresent'] and cd['order']==0 and cd['sourceType']=='file'
args=['plan','apply',p['planID'],'--digest',p['planDigest'],'--idempotency-key','boot-media-eject-resize-8a092b4-001','--detach']
for ack in p['acknowledgements']:args+=['--ack',ack]
r=cli(*args);(root/'boot-media-eject-accepted.json').write_text(r.stdout);print(r.stdout,flush=True);jid=json.loads(r.stdout)['data']['operationID']
for _ in range(60):
 r=cli('operation','show',jid);job=json.loads(r.stdout)['data']
 if job['state'] not in ('queued','validating','running'):break
 time.sleep(.25)
(root/'boot-media-eject-result.json').write_text(r.stdout);print(r.stdout,flush=True)
after=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);(root/'boot-media-ejected.xml').write_bytes(after)
assert job['state']=='succeeded',job
t=ET.fromstring(after);assert t.findtext('vcpu')=='3' and t.findtext('memory')=='3145728' and t.findtext('currentMemory')=='3145728'
cd=t.find("devices/disk[@device='cdrom']");assert cd.attrib['type']=='file' and cd.find('source') is None and cd.find('readonly') is not None and cd.find('target').attrib=={'dev':'sda','bus':'sata'} and cd.find('boot') is None
assert t.find("devices/disk[@device='disk']/boot").attrib['order']=='1'
for path in ('metadata','os','cpu','features','devices/watchdog','devices/memballoon','devices/graphics'):
 assert ET.tostring(t.find(path))==ET.tostring(b.find(path)),path
assert t.findall('devices/interface')==[]
assert subprocess.check_output(['sudo','-n','sha256sum',media],text=True).split()[0]==isoSHA
r=cli('vm','boot','show',vm);(root/'boot-media-ejected-view.json').write_text(r.stdout);print(r.stdout,flush=True)
print(json.dumps({'singleDefinitionOperation':jid,'vcpus':3,'memoryMiB':3072,'emptyReadOnlyDriveRetained':'sda','firstBootDevice':'vda','retainedISOSHA256':isoSHA,'opaqueMetadataFirmwareCPUModelAndOtherCheckedDevicesPreserved':True,'beforeXMLSHA256':hashlib.sha256(before).hexdigest(),'afterXMLSHA256':hashlib.sha256(after).hexdigest(),'guestSizingAndBootVerification':'pending'}),flush=True)
