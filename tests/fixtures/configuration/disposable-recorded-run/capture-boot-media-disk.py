import hashlib,json,pathlib,subprocess,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';id='a19bf9ee-cd7f-4921-baac-39ce1694eb35'
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='running'
x=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml',id],text=True);tree=ET.fromstring(x);assert tree.attrib['type']=='kvm' and tree.find('devices').findall('interface')==[]
(root/'boot-media-disk-running.xml').write_text(x)
r=subprocess.run(['virsh','-c','qemu:///system','qemu-monitor-command',id,'{"execute":"query-kvm"}'],capture_output=True,text=True,check=True)
kvm=json.loads(r.stdout)['return'];assert kvm=={'enabled':True,'present':True},kvm
shot=root/'boot-media-disk-001.png';assert not shot.exists()
r=subprocess.run(['virsh','-c','qemu:///system','screenshot',id,str(shot)],capture_output=True,text=True,check=True)
print(json.dumps({'vmID':id,'nativeState':'running','QMPQueryKVM':kvm,'networkAdapters':0,'screenshotCommandOutput':r.stdout.strip(),'screenshotSHA256':hashlib.sha256(shot.read_bytes()).hexdigest(),'screenshotBytes':shot.stat().st_size,'runningXMLSHA256':hashlib.sha256(x.encode()).hexdigest(),'guestBootVisualReview':'pending','guestReachability':'not-tested'}),flush=True)

for command in ('query-cpus-fast','query-memory-size-summary'):
 result=json.loads(subprocess.check_output(['virsh','-c','qemu:///system','qemu-monitor-command',id,json.dumps({'execute':command})],text=True))
 print(json.dumps({'QMPCommand':command,'result':result,'guestOnlineResourceVerification':False}),flush=True)
 if command=='query-cpus-fast':assert len(result['return'])==3
 else:assert result['return']['base-memory']==3221225472 and result['return'].get('plugged-memory',0)==0
