import hashlib,json,os,pathlib,sqlite3,subprocess,time,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};vm='a19bf9ee-cd7f-4921-baac-39ce1694eb35';namespace='urn:virmill:qualification:opaque'
def cli(*args,check=True):return subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=check,timeout=60)
def xml():return subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])
def jobs():
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute('SELECT id FROM jobs ORDER BY id'))
def plan(cpus,name):
 request={'vcpus':cpus,'bootOrder':[{'kind':'disk','id':'vda'}],'applyMode':'next-boot'}
 r=cli('vm','set',vm,'--input',json.dumps(request),'--plan');(root/(name+'-plan.json')).write_text(r.stdout);print(r.stdout,flush=True);p=json.loads(r.stdout)['data']
 assert p['operation']=='vm.configure-hardware' and p['review']['requested']==request and p['resourceIDs']==['libvirt|qemu:///system|vm|'+vm]
 assert set(p['acknowledgements'])=={'host-mutation','exclusive-configuration-writer','replace-boot-order'}
 return p
def apply(p,name,check=True):
 args=['plan','apply',p['planID'],'--digest',p['planDigest'],'--idempotency-key',name+'-8a092b4-001','--detach']
 for ack in p['acknowledgements']:args+=['--ack',ack]
 r=cli(*args,check=check);(root/(name+'-accepted.json')).write_text(r.stdout);print(r.stdout+r.stderr,flush=True);return r
def finish(r,name):
 jid=json.loads(r.stdout)['data']['operationID']
 for _ in range(120):
  r=cli('operation','show',jid);j=json.loads(r.stdout)['data']
  if j['state'] in ('succeeded','failed','canceled','partial','recovery-required'):break
  time.sleep(.25)
 (root/(name+'-result.json')).write_text(r.stdout);print(r.stdout,flush=True);assert j['state']=='succeeded',j;return jid
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
before=xml();assert before==(root/'boot-media-ejected.xml').read_bytes();assert namespace.encode() not in before
p=plan(4,'boot-media-stale');beforeJobs=jobs()
subprocess.run(['virsh','-c','qemu:///system','metadata',vm,namespace,'--config','--key','qualification','--set',"<state version='1'><device-policy keep='yes'>unknown extension retained</device-policy></state>"],check=True)
external=xml();assert external!=before;opaque=ET.fromstring(external).find('metadata/{'+namespace+'}state');assert opaque is not None
opaqueXML=ET.tostring(opaque);(root/'boot-media-external-metadata.xml').write_bytes(external)
r=apply(p,'boot-media-stale',check=False);assert r.returncode!=0 and json.loads(r.stdout)['error']['code']=='STALE_PLAN'
assert xml()==external and jobs()==beforeJobs
up=plan(4,'boot-media-opaque-edit');upJob=finish(apply(up,'boot-media-opaque-edit'),'boot-media-opaque-edit');changed=xml();(root/'boot-media-opaque-edited.xml').write_bytes(changed)
assert ET.fromstring(changed).findtext('vcpu')=='4' and ET.tostring(ET.fromstring(changed).find('metadata/{'+namespace+'}state'))==opaqueXML
restore=plan(3,'boot-media-restore-resources');restoreJob=finish(apply(restore,'boot-media-restore-resources'),'boot-media-restore-resources');final=xml();(root/'boot-media-final.xml').write_bytes(final)
assert ET.fromstring(final).findtext('vcpu')=='3' and ET.tostring(ET.fromstring(final).find('metadata/{'+namespace+'}state'))==opaqueXML
assert final==external
print(json.dumps({'stalePlanRefusedBeforeAcceptance':True,'externalChangePreserved':True,'opaqueNamespace':namespace,'unrelatedCPUChanges':[3,4,3],'editOperation':upJob,'restoreOperation':restoreJob,'finalXMLSHA256':hashlib.sha256(final).hexdigest(),'finalConfigurationEqualsPostExternalEdit':True,'atomicExternalWriterExclusionProven':False,'adoptionWorkflowTested':False}),flush=True)
