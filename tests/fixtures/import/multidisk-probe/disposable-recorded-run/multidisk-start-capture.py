import hashlib,json,os,pathlib,subprocess,time,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};vm='b9496482-2eeb-40e1-892b-4e291c108c52'
plan=json.loads((root/'multidisk-start-plan.json').read_text());assert plan['planID']=='9cbaa38c-cfeb-4246-b39b-2d213cb3c414' and plan['planDigest']=='839b8cd88059a6620d3d54c365b8cc65f6e5348381351eb73e01908e2906e98c'
assert plan['operation']=='vm.start' and plan['resourceIDs']==['libvirt|qemu:///system|vm|'+vm] and plan['acknowledgements']==['host-mutation']
assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()=='7880991288cfbfb9b757678c3c0c0211500fe1fc3f03274a29f84b03e632076e'
def cli(*args):
 r=subprocess.run(['virmill',*args,'--connection','qemu:///system','--timeout','90s','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=100);assert r.returncode==0,r.stdout+r.stderr;return json.loads(r.stdout)['data']
job=cli('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key','multidisk-probe-start-'+plan['planID'],'--ack','host-mutation')
with (root/'multidisk-start-accepted.json').open('x') as f:json.dump(job,f,indent=2)
end=time.monotonic()+90
while time.monotonic()<end:
 job=cli('operation','show',job['operationID'])
 if job['state'] in ('succeeded','failed','canceled','partial','recovery-required'):break
 time.sleep(.1)
assert job['state']=='succeeded';assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='running'
time.sleep(2)
xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml',vm]);tree=ET.fromstring(xml);assert tree.attrib['type']=='kvm' and not tree.findall('./devices/interface');(root/'multidisk-running.xml').write_bytes(xml)
kvm=json.loads(subprocess.check_output(['virsh','-c','qemu:///system','qemu-monitor-command',vm,'{"execute":"query-kvm"}'],text=True))['return'];assert kvm=={'enabled':True,'present':True}
shot=root/'multidisk-boot-001.png';assert not shot.exists()
output=subprocess.check_output(['virsh','-c','qemu:///system','screenshot',vm,str(shot)],text=True)
result={'vmID':vm,'startOperationID':job['operationID'],'nativeState':'running','QMPQueryKVM':kvm,'networkAdapters':0,'screenshotCommandOutput':output.strip(),'screenshotSHA256':hashlib.sha256(shot.read_bytes()).hexdigest(),'screenshotBytes':shot.stat().st_size,'runningXMLSHA256':hashlib.sha256(xml).hexdigest(),'visualReview':'pending','OSCompatibilityOrReadinessTested':False,'consoleCapture':'external virsh; not Virmill console qualification'}
with (root/'multidisk-boot-capture.json').open('x') as f:json.dump(result,f,indent=2)
print(json.dumps(result),flush=True)
