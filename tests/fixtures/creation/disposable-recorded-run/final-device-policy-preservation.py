import hashlib,json,os,pathlib,sqlite3,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
oldjob='b5983e68-bfcc-42f0-bf4e-3877029b756b';oldvm='ae630461-91d3-4f07-ad88-e6842c3dc3ea';newjob='28701ac3-325d-4596-8103-38e19f74b7cb';newvm='a19bf9ee-cd7f-4921-baac-39ce1694eb35'
def cli(*args,check=True):return subprocess.run(['virmill',*args,'--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=check,timeout=60)
def locks(jid):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute('SELECT resource,job_id FROM locks WHERE job_id=? ORDER BY resource',(jid,)))
def xmlhash(id):return hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id])).hexdigest()
oldlocks=locks(oldjob);assert len(oldlocks)==3 and locks(newjob)==[]
assert xmlhash(oldvm)=='e3200ea179e0cee3258b413baf8668529f2c1fdbed4535115a674d1b4d40d824'
volume='/var/lib/libvirt/images/virmill-qualification-65930c6/virmill-'+oldvm+'-disk-000.qcow2';st=subprocess.check_output(['stat','--format=%d:%i:%s:%Y:%Z',volume],text=True)
r=cli('operation','reconcile',oldjob,check=False);print(r.stdout+r.stderr,flush=True)
assert r.returncode==6 and json.loads(r.stdout)['error']['code']=='RECOVERY_REQUIRED'
assert json.loads(cli('operation','show',oldjob).stdout)['data']['state']=='recovery-required'
assert locks(oldjob)==oldlocks and subprocess.check_output(['stat','--format=%d:%i:%s:%Y:%Z',volume],text=True)==st
assert xmlhash(oldvm)=='e3200ea179e0cee3258b413baf8668529f2c1fdbed4535115a674d1b4d40d824'
assert xmlhash('2ec994ce-2950-498c-8b19-d2f7dbb53a78')=='bbe62f376a3943d795cdac4fd67ff56087efa43024b27cacf0578217a1a44408'
for id in (oldvm,newvm,'2ec994ce-2950-498c-8b19-d2f7dbb53a78'):assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
checks=[(str(pathlib.Path.home()/'images/kali-linux-2026.2-qemu-amd64.7z'),'c7c35588d05277c482c908bf7a136d348f76ffa68700b04ff53c0b217e6bd071'),(str(root/'sources/kali-qemu/kali-linux-2026.2-qemu-amd64.qcow2'),'4e24751faa18753ad5f854053c523481d1bc5efa9a45c3828071b7475d825104'),(volume,'8eb9be4a9592ad8242fa4e99fbac2dd2eef474bb93e99f3699d17617c7d93c70')]
for path,expected in checks:
 got=subprocess.check_output(['sudo','-n','sha256sum',path],text=True).split()[0];assert got==expected;print(json.dumps({'preservedFile':path,'sha256':got}),flush=True)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
r=cli('vm','show',newvm,'--connection','qemu:///system');v=json.loads(r.stdout)['data'];assert v['ownership']=='managed' and v['state']=='stopped' and not v['autostart']
print(json.dumps({'sourcePreserved':True,'unrelatedGuestXMLPreserved':True,'legacyUncertainXMLAndDiskPreserved':True,'legacyResourceLocks':3,'newCreationLocks':0,'newGuestOwnership':'managed','allThreeGuestsStopped':True,'gracefulShutdownVerified':True,'fullReleaseQualified':False}),flush=True)
