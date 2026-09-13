import hashlib,json,os,pathlib,sqlite3,subprocess,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};vm='a19bf9ee-cd7f-4921-baac-39ce1694eb35';oldjob='b5983e68-bfcc-42f0-bf4e-3877029b756b'
for id,expected in {'2ec994ce-2950-498c-8b19-d2f7dbb53a78':'bbe62f376a3943d795cdac4fd67ff56087efa43024b27cacf0578217a1a44408','ae630461-91d3-4f07-ad88-e6842c3dc3ea':'e3200ea179e0cee3258b413baf8668529f2c1fdbed4535115a674d1b4d40d824',vm:'e31e20ae14df1e2809c242416ff31416249b8c4d3a14dab74507afb02e0250d2'}.items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id])).hexdigest()==expected
r=subprocess.run(['virmill','operation','list','--output','json','--non-interactive'],env=env,capture_output=True,text=True,check=True);jobs=json.loads(r.stdout)['data']
assert all(j['state']=='succeeded' or (j['operationID']==oldjob and j['state']=='recovery-required') for j in jobs)
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 locks=list(db.execute('SELECT resource,job_id FROM locks ORDER BY resource'));assert len(locks)==3 and all(jid==oldjob for _,jid in locks)
checks=[(str(pathlib.Path.home()/'images/<owner-media-4>.iso'),'6dbefacc95e3b556c19c48e8bae39b8b505e2d3a1aba0bfb7ab62b036c3d2ba3'),('/var/lib/libvirt/images/virmill-policy-c7f8b76/virmill-boot-media-fixture.iso','6dbefacc95e3b556c19c48e8bae39b8b505e2d3a1aba0bfb7ab62b036c3d2ba3'),(str(pathlib.Path.home()/'images/<owner-media-5>.7z'),'c7c35588d05277c482c908bf7a136d348f76ffa68700b04ff53c0b217e6bd071'),(str(root/'sources/kali-qemu/<owner-media-5>.qcow2'),'4e24751faa18753ad5f854053c523481d1bc5efa9a45c3828071b7475d825104'),('/var/lib/libvirt/images/virmill-qualification-65930c6/virmill-ae630461-91d3-4f07-ad88-e6842c3dc3ea-disk-000.qcow2','8eb9be4a9592ad8242fa4e99fbac2dd2eef474bb93e99f3699d17617c7d93c70')]
for path,expected in checks:
 got=subprocess.check_output(['sudo','-n','sha256sum',path],text=True).split()[0];assert got==expected;print(json.dumps({'preservedFile':path,'sha256':got}),flush=True)
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
print(json.dumps({'allThreeGuestsStopped':True,'allNewJobsSucceeded':True,'legacyUncertainOperation':oldjob,'legacyLocksRetained':3,'newJobLocks':0,'sourceMediaAndUnrelatedGuestPreserved':True,'retainedEmptyCDROM':'sda','vcpus':3,'memoryMiB':3072,'helperServiceEnabled':False,'fullReleaseQualified':False}),flush=True)
print(subprocess.check_output(['df','-B1','--output=size,avail',str(root)],text=True),flush=True)
