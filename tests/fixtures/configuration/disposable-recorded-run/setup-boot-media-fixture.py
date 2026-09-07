import hashlib,json,os,pathlib,sqlite3,subprocess,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
vm='a19bf9ee-cd7f-4921-baac-39ce1694eb35';pool='virmill-policy-c7f8b76'
iso=pathlib.Path.home()/'images/kali-linux-2026.2-installer-amd64.iso';copy=pathlib.Path('/var/lib/libvirt/images')/pool/'virmill-boot-media-fixture.iso'
expected='6dbefacc95e3b556c19c48e8bae39b8b505e2d3a1aba0bfb7ab62b036c3d2ba3'
assert iso.is_file() and not iso.is_symlink() and iso.stat().st_size==4802531328 and not copy.exists() and not copy.is_symlink()
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','pool-uuid',pool],text=True).strip()=='eede6ba7-13a9-48d6-8cba-b8611d36513b'
for id in (vm,'ae630461-91d3-4f07-ad88-e6842c3dc3ea','2ec994ce-2950-498c-8b19-d2f7dbb53a78'):
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 assert list(db.execute('SELECT job_id FROM locks WHERE resource=?',('libvirt|qemu:///system|vm|'+vm,)))==[]
before=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);tree=ET.fromstring(before)
assert tree.findtext('uuid')==vm and tree.findall('devices/interface')==[]
assert tree.findall("devices/disk[@device='cdrom']")==[]
assert tree.find("devices/disk/target").attrib['dev']=='vda'
assert tree.find("devices/controller[@type='sata']").attrib['index']=='0'
(root/'boot-media-before-fixture.xml').write_bytes(before)
assert subprocess.check_output(['sha256sum',str(iso)],text=True).split()[0]==expected
subprocess.run(['sudo','-n','cp','--reflink=never','--no-clobber',str(iso),str(copy)],check=True)
assert subprocess.check_output(['sudo','-n','sha256sum',str(copy)],text=True).split()[0]==expected
subprocess.run(['sudo','-n','chown','qemu:qemu',str(copy)],check=True)
subprocess.run(['sudo','-n','chmod','0444',str(copy)],check=True)
subprocess.run(['sudo','-n','restorecon',str(copy)],check=True)
subprocess.run(['virsh','-c','qemu:///system','pool-refresh',pool],check=True)
fixture=root/'boot-media-cdrom-fixture.xml';assert not fixture.exists()
fixture.write_text("<disk type='volume' device='cdrom'><driver name='qemu' type='raw'/><source pool='virmill-policy-c7f8b76' volume='virmill-boot-media-fixture.iso'/><target dev='sda' bus='sata'/><readonly/><address type='drive' controller='0' bus='0' target='0' unit='0'/></disk>\n")
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])==before
subprocess.run(['virsh','-c','qemu:///system','attach-device',vm,str(fixture),'--config'],check=True)
after=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);tree=ET.fromstring(after)
cd=tree.find("devices/disk[@device='cdrom']");assert cd is not None and cd.attrib['type']=='volume' and cd.find('readonly') is not None and cd.find('target').attrib['dev']=='sda'
(root/'boot-media-fixture-attached.xml').write_bytes(after)
print(json.dumps({'externalFixturePreparation':True,'virmillDiskAttachQualified':False,'vmID':vm,'sourceISOAndIndependentCopySHA256':expected,'copyBytes':copy.stat().st_size,'copyMode':oct(copy.stat().st_mode&0o777),'readOnlyCDROM':'sda','sourceType':'volume','beforeXMLSHA256':hashlib.sha256(before).hexdigest(),'afterXMLSHA256':hashlib.sha256(after).hexdigest()}),flush=True)
