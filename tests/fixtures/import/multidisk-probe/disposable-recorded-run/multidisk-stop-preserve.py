import hashlib,json,os,pathlib,sqlite3,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())};vm='b9496482-2eeb-40e1-892b-4e291c108c52'
assert hashlib.sha256((root/'multidisk-boot-001.png').read_bytes()).hexdigest()=='2dc4dd5e8014e0613e7e494abd88f31e9f74f854a340d05feefe52d6f83022a7'
def cli(*args,check=True):
 r=subprocess.run(['virmill',*args,'--connection','qemu:///system','--timeout','90s','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=100)
 if check:assert r.returncode==0,r.stdout+r.stderr
 return r
plan=json.loads(cli('vm','stop',vm,'--hard','--plan').stdout)['data'];assert plan['operation']=='vm.hard-stop' and plan['review']['vmID']==vm and plan['review']['action']=='hard-stop' and not plan['review']['diskDeletion']
assert set(plan['acknowledgements'])=={'host-mutation','data-loss-hard-stop'}
with (root/'multidisk-stop-plan.json').open('x') as f:json.dump(plan,f,indent=2)
print(json.dumps({'reviewedHardStop':plan,'reason':'Generated BIOS probe halts without an OS or ACPI shutdown handler; both volumes must be retained'}),flush=True)
job=json.loads(cli('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key','multidisk-probe-hard-stop-'+plan['planID'],'--ack','host-mutation','--ack','data-loss-hard-stop').stdout)['data']
with (root/'multidisk-stop-accepted.json').open('x') as f:json.dump(job,f,indent=2)
end=time.monotonic()+90
while time.monotonic()<end:
 job=json.loads(cli('operation','show',job['operationID']).stdout)['data']
 if job['state'] in ('succeeded','failed','canceled','partial','recovery-required'):break
 time.sleep(.1)
assert job['state']=='succeeded',job
pool=json.loads((root/'multidisk-pool.json').read_text());xmlhashes={}
for id,digest in {**pool['existingStoppedVMXMLSHA256'],vm:'7880991288cfbfb9b757678c3c0c0211500fe1fc3f03274a29f84b03e632076e'}.items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',id],text=True).strip()=='shut off'
 xml=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',id]);xmlhashes[id]=hashlib.sha256(xml).hexdigest();assert xmlhashes[id]==digest
result=json.loads((root/'multidisk-fresh-result.json').read_text());hashes={}
for v in result['receipt']['volumes']:
 path=v['allocated']['path'];hashes[path]=subprocess.check_output(['sudo','-n','sha256sum',path],text=True).split()[0];assert hashes[path]==v['intent']['sha256']
partial=json.loads((root/'multidisk-crash-before-restart.json').read_text())
for v in partial['volumes']:
 path=v['path'];s=pathlib.Path(path).stat();assert (s.st_dev,s.st_ino,s.st_size,s.st_blocks*512,s.st_mtime_ns,s.st_ctime_ns)==tuple(v[k] for k in ('device','inode','size','allocatedBytes','mtimeNS','ctimeNS'))
 hashes[path]=subprocess.check_output(['sudo','-n','sha256sum',path],text=True).split()[0];assert hashes[path]==v['sha256']
assert set(str(p) for p in pathlib.Path(pool['target']).iterdir())==set(hashes)
with (root/'sources/multidisk-probe-v1/probe.ova').open('rb') as f:assert hashlib.file_digest(f,'sha256').hexdigest()=='1b2b15b998b4879d71cd2e5a1b137253cb41f14808a830ff8ba6ff231c0d88f0'
verified=json.loads(cli('import','verify',str(root/'prepared/multidisk-probe-v1')).stdout)['data'];assert [v['sha256'] for v in verified['disks']]==[v['intent']['sha256'] for v in result['receipt']['volumes']]
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 assert not list(db.execute('SELECT resource,job_id FROM locks'))
 retained=json.loads(db.execute("SELECT body FROM metadata WHERE kind='vm-creation-disposition' AND id='f7ecbe1b-73f0-491a-adde-7248c960c2f6'").fetchone()[0]);assert retained['disposition']=='retain'
 counts={'jobs':db.execute('SELECT COUNT(*) FROM jobs').fetchone()[0],'locks':db.execute('SELECT COUNT(*) FROM locks').fetchone()[0]}
report={'nativeHardStop':'passed','stopOperationID':job['operationID'],'allFourGuestsStopped':True,'guestXMLSHA256':xmlhashes,'allFourTestVolumeSHA256':hashes,'probeVolumeBytesUnchangedAfterBoot':True,'pinnedPartialVolumesUnchanged':True,'originalArchiveAndPreparationUnchanged':True,'retentionPinsStillDurable':True,'journal':counts,'SELinux':subprocess.check_output(['getenforce'],text=True).strip(),'visualReview':'Model inspected the captured screenshot and observed VIRMILL SECOND DISK PASS','screenshotSHA256':'2dc4dd5e8014e0613e7e494abd88f31e9f74f854a340d05feefe52d6f83022a7','OSCompatibilityOrReadinessTested':False,'physicalHardwareMatrixQualified':False}
with (root/'multidisk-final-preservation.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
