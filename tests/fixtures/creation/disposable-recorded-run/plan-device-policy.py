import json,os,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
job=json.loads((root/'import-status.json').read_text())['data'];assert job['state']=='succeeded'
pool=json.loads((root/'device-policy-pool.json').read_text())
request={'identityMode':'clone','hardware':{'name':'Virmill policy Kali QCOW2','poolID':pool['uuid'],'architecture':'x86_64','machine':'pc-q35-10.2','vcpus':2,'memoryMiB':2048,'cpu':{'mode':'host-passthrough'},'firmware':{'mode':'bios','secureBoot':False,'tpm':False},'clock':'utc','graphics':'vnc-unix','disks':[{'sourceID':'boot','bus':'virtio','bootOrder':1}],'nics':[]}}
request['hardware']['devicePolicy']={'version':1,'chipset':'q35','pciPlacement':'libvirt-auto','usbController':'none','memoryBalloon':'none','watchdogAction':'none','input':'ps2','audio':'none','serial':'isa-serial'}
(root/'creation-input-c7f8b76.json').write_text(json.dumps(request,indent=2)+'\n')
r=subprocess.run(['virmill','vm','create',job['operationID'],'--input',json.dumps(request),'--plan','--connection','qemu:///system','--output','json','--non-interactive','--timeout','10m'],env=env,text=True,capture_output=True,timeout=650)
(root/'creation-plan-c7f8b76.json').write_text(r.stdout)
print(r.stdout+r.stderr,flush=True)
assert r.returncode==0
