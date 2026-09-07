import json,os,pathlib,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
retained=json.loads((root/'multidisk-retention-result.json').read_text());assert retained['complete'] and retained['disposition']['disposition']=='retain'
job=json.loads((root/'multidisk-import-job.json').read_text());assert job['state']=='succeeded'
pool=json.loads((root/'multidisk-pool.json').read_text());assert pool['uuid']=='36ab8b28-60d8-4495-9046-6f2f6bfc23b2'
request={'identityMode':'clone','hardware':{'name':'Virmill BIOS multi-disk probe','poolID':pool['uuid'],'architecture':'x86_64','machine':'pc-q35-10.2','vcpus':1,'memoryMiB':256,'cpu':{'mode':'host-passthrough'},'firmware':{'mode':'bios','secureBoot':False,'tpm':False},'clock':'utc','graphics':'vnc-unix','disks':[{'sourceID':'boot','bus':'virtio','bootOrder':1},{'sourceID':'data','bus':'virtio','bootOrder':2}],'nics':[],'devicePolicy':{'version':1,'chipset':'q35','pciPlacement':'libvirt-auto','usbController':'none','memoryBalloon':'none','watchdogAction':'none','input':'ps2','audio':'none','serial':'isa-serial'}}}
with (root/'multidisk-fresh-input.json').open('x') as f:json.dump(request,f,indent=2)
r=subprocess.run(['virmill','vm','create',job['operationID'],'--input',json.dumps(request),'--plan','--connection','qemu:///system','--output','json','--non-interactive','--timeout','10m'],env=env,capture_output=True,text=True,timeout=660)
with (root/'multidisk-fresh-plan.json').open('x') as f:f.write(r.stdout)
print(r.stdout+r.stderr,flush=True);assert r.returncode==0
