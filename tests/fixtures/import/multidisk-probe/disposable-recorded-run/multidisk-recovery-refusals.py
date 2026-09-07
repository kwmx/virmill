import hashlib,json,os,pathlib,sqlite3,subprocess,time
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
intent=json.loads((root/'multidisk-crash-intent.json').read_text());before=json.loads((root/'multidisk-crash-before-restart.json').read_text());plan=json.loads((root/'multidisk-create-plan.json').read_text())['data'];jid=before['operationID']
def cli(*args):
 r=subprocess.run(['virmill',*args,'--connection','qemu:///system','--timeout','10m','--output','json','--non-interactive'],env=env,capture_output=True,text=True,timeout=660)
 print(json.dumps({'command':list(args),'exitCode':r.returncode,'stdout':r.stdout,'stderr':r.stderr}),flush=True);return r
r=cli('operation','show',jid);assert r.returncode==0 and json.loads(r.stdout)['data']['state']=='recovery-required'
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 plans_before=list(db.execute('SELECT id,digest FROM plans ORDER BY id'))
 original_recipe=db.execute('SELECT body,input FROM plans WHERE id=?',(plan['planID'],)).fetchone()
r=cli('operation','reconcile',jid);(root/'multidisk-reconcile.json').write_text(r.stdout)
r=cli('operation','show',jid);job=json.loads(r.stdout)['data'];assert job['state']=='recovery-required'
for args in [('vm','creation','resume',jid,'--plan'),('vm','creation','result',jid)]:
 r=cli(*args);assert r.returncode==6 and json.loads(r.stdout)['error']['code']=='RECOVERY_REQUIRED'
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 assert list(db.execute('SELECT id,digest FROM plans ORDER BY id'))==plans_before
 assert db.execute('SELECT body,input FROM plans WHERE id=?',(plan['planID'],)).fetchone()==original_recipe
 receipt=db.execute("SELECT body FROM metadata WHERE kind='vm-creation' AND id=?",(plan['planID'],)).fetchone()[0]
 assert hashlib.sha256(receipt).hexdigest()==before['receiptSHA256']
 assert [list(x) for x in db.execute('SELECT resource,job_id FROM locks ORDER BY resource')]==before['locks']
 for id,digest in intent['initialJobsSHA256'].items():assert hashlib.sha256(db.execute('SELECT body FROM jobs WHERE id=?',(id,)).fetchone()[0]).hexdigest()==digest
 events=[json.loads(r[0]) for r in db.execute('SELECT body FROM events WHERE job_id=? ORDER BY seq',(jid,))]
 assert len([e for e in events if e['message'].startswith('Intent persisted: allocate new managed volume')])==2
 assert not any('define the new VM' in e['message'] for e in events)
 print(json.dumps({'recoveryEvents':events}),flush=True)
for v in before['volumes']:
 p=pathlib.Path(v['path']);s=p.stat()
 assert (s.st_dev,s.st_ino,s.st_size,s.st_blocks*512,s.st_mtime_ns,s.st_ctime_ns)==tuple(v[k] for k in ('device','inode','size','allocatedBytes','mtimeNS','ctimeNS'))
 assert subprocess.check_output(['sudo','-n','sha256sum',str(p)],text=True).split()[0]==v['sha256']
all_vms=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','list','--all','--uuid'],text=True).split();assert intent['vmID'] not in all_vms
pool=json.loads((root/'multidisk-pool.json').read_text())
assert set(pathlib.Path(pool['target']).iterdir())=={pathlib.Path(v['path']) for v in before['volumes']}
for vm,digest in pool['existingStoppedVMXMLSHA256'].items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()==digest
result={'activeUploadCrashRecovery':'passed','operationID':jid,'state':'recovery-required','receiptUnchanged':True,'retainedVolumesUnchanged':True,'heldLocks':len(before['locks']),'allocationUploadDefinitionReplayed':False,'definitionResumeRefused':True,'vmDefined':False,'existingJobsAndGuestsUnchanged':True,'limitation':'One observed coordinator SIGKILL during a BIOS two-disk stream; not host power loss or every crash boundary'}
with (root/'multidisk-recovery.json').open('x') as f:json.dump(result,f,indent=2)
print(json.dumps(result),flush=True)
