import hashlib,json,os,pathlib,subprocess,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
def run(args):
 r=subprocess.run(args,env=env,capture_output=True,text=True,timeout=60)
 assert r.returncode==0,(args,r.stderr)
 return r.stdout
vm='ae630461-91d3-4f07-ad88-e6842c3dc3ea'
xml=run(['virsh','--readonly','--connect','qemu:///system','dumpxml','--inactive',vm])
(root/'created-native.xml').write_text(xml)
x=ET.fromstring(xml);assert not x.findall('./devices/interface')
state=run(['virsh','--readonly','--connect','qemu:///system','domstate',vm]).strip();assert state=='shut off'
old=json.loads((root/'vms.json').read_text())['data'][0]
current=json.loads(run(['virmill','vm','show',old['key']['resourceUUID'],'--connection','qemu:///system','--output','json','--non-interactive']))['data']
assert current['fingerprint']==old['fingerprint']
run(['rpm','-V','virmill','virmill-host-helper'])
job=json.loads(run(['virmill','operation','show','b5983e68-bfcc-42f0-bf4e-3877029b756b','--output','json','--non-interactive']))['data'];assert job['state']=='recovery-required'
(root/'creation-final-job.json').write_text(json.dumps(job,indent=2)+'\n')
with (root/'sources/kali-qemu/<owner-media-5>.qcow2').open('rb') as f:source_hash=hashlib.file_digest(f,'sha256').hexdigest()
assert source_hash=='4e24751faa18753ad5f854053c523481d1bc5efa9a45c3828071b7475d825104'
packages=run(['rpm','-q','libvirt-daemon-kvm','libvirt-libs','qemu-kvm','qemu-img','edk2-ovmf','swtpm','bubblewrap','xorriso','7zip','util-linux','python3','restic','virt-v2v']).splitlines()
result={'hostAlias':'disposable-fedora44-01','kernel':run(['uname','-sr']).strip(),'architecture':run(['uname','-m']).strip(),'virtualization':run(['systemd-detect-virt']).strip(),'selinux':run(['getenforce']).strip(),'nativePackages':packages,'installedRevision':json.loads(run(['virmill','version','--output','json']))['data']['revision'],'installedPackageVerification':'rpm -V passed for core and helper','newVMID':vm,'newVMState':state,'guestBootVerified':False,'networkAdapters':len(x.findall('./devices/interface')),'preexistingGuestUnchanged':True,'originalQCOW2SHA256After':source_hash,'creationState':job['state'],'creationError':job['error']['message'],'nativeXMLSHA256':hashlib.sha256(xml.encode()).hexdigest(),'unexpectedDeviceTypes':[n.tag for n in x.findall('./devices/*') if n.tag in ('controller','input','audio','watchdog','memballoon')],'watchdog':x.find('./devices/watchdog').attrib,'cleanup':'No source media, existing guest, retained disk or uncertain definition deleted.'}
(root/'final-observations.json').write_text(json.dumps(result,indent=2)+'\n')
print(json.dumps(result,indent=2))
