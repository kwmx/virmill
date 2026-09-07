import hashlib,json,os,pathlib,sqlite3,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
vm='b9496482-2eeb-40e1-892b-4e291c108c52'
disk=pathlib.Path('/var/lib/libvirt/images/virmill-multidisk-12d7bba/virmill-'+vm+'-disk-000.qcow2')
def cli(*args,check=True):
 p=subprocess.run(['virmill',*args,'--connection','qemu:///system','--output','json','--non-interactive','--timeout','30s'],env=env,text=True,capture_output=True,timeout=45)
 print(p.stdout+p.stderr,flush=True)
 if check:p.check_returncode()
 return p
def rows(query):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(query))
assert not rows('SELECT resource,job_id FROM locks')
st=disk.stat();assert st.st_uid==0 and st.st_gid==0 and st.st_mode&0o777==0o600 and not os.access(disk,os.R_OK)
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm]);assert hashlib.sha256(xml).hexdigest()=='7880991288cfbfb9b757678c3c0c0211500fe1fc3f03274a29f84b03e632076e'
acl=subprocess.check_output(['getfacl','--absolute-names','--numeric',str(disk)])
before={'device':st.st_dev,'inode':st.st_ino,'size':st.st_size,'mtimeNS':st.st_mtime_ns,'uid':st.st_uid,'gid':st.st_gid,'mode':st.st_mode,'acl':acl.decode(),'xmlSHA256':hashlib.sha256(xml).hexdigest()}
with (root/'helper-access-original-b7fe053.json').open('x') as f:json.dump(before,f,indent=2)
p=json.loads(cli('storage','access','grant',vm,'--input','{"target":"vda","rootID":"images"}','--plan').stdout)['data']
assert p['operation']=='storage.grant-read' and p['review']['volume']['vmID']==vm and p['review']['volume']['path']==str(disk)
assert p['review']['actorUID']==1000 and p['review']['before']['uid']==0 and p['review']['changesDiskBytes'] is False and p['review']['startsVM'] is False
assert set(p['acknowledgements'])=={'host-permission-change','exclusive-offline-volume','persistent-disk-read-access'}
with (root/'helper-access-grant-plan-b7fe053.json').open('x') as f:json.dump(p,f,indent=2)
args=['plan','apply',p['planID'],'--digest',p['planDigest'],'--idempotency-key','helper-access-grant-b7fe053-001','--wait']
for ack in p['acknowledgements']:args+=['--ack',ack]
applied=cli(*args,check=False)
data=json.loads(applied.stdout)['data']
with (root/'helper-access-grant-apply-b7fe053.json').open('x') as f:json.dump(data,f,indent=2)
assert applied.returncode==0 and data['state']=='succeeded'
job=data['operationID'];result=json.loads(cli('storage','access','result',job).stdout)['data'];assert result['complete'] and result['receipt']['complete']
assert os.access(disk,os.R_OK) and not os.access(disk,os.W_OK)
with disk.open('rb') as f:sha=hashlib.file_digest(f,'sha256').hexdigest()
assert sha=='fe1356a1e613740dc51fb799fc1f559a1f4e311bbcae6b15424c2bd01ec6dfd3'
assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])==xml
assert not rows('SELECT resource,job_id FROM locks')
report={'grantOperationID':job,'grantPlanID':p['planID'],'nativeHelperGrant':'passed','ordinaryActorCanRead':True,'ordinaryActorCannotWrite':True,'originalDiskSHA256Unchanged':sha,'VMXMLUnchanged':True,'guestStarted':False,'SELinux':subprocess.check_output(['getenforce'],text=True).strip(),'grantPendingExplicitRevocation':True}
with (root/'helper-access-grant-result-b7fe053.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
