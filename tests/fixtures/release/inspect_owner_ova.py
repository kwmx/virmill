import hashlib,json,os,socket,subprocess,time
from pathlib import Path
assert socket.gethostname() in ('virmill-test','virmill-test.home') and os.getuid()==1000
root=Path('/home/virmill-test/virmill-tests/import-flow-4945d75')
source=Path('/home/virmill-test/images/DFIR-Win11.ova')
out=root/'owner-ova-inspection';out.mkdir(mode=0o700)
def meta():
 s=source.stat();return [s.st_dev,s.st_ino,s.st_size,s.st_mtime_ns,s.st_ctime_ns]
def observation(*args):
 r=subprocess.run(['/usr/bin/virmill',*args,'--output','json','--non-interactive'],capture_output=True,text=True,timeout=20);assert r.returncode==0,r.stderr
 v=json.loads(r.stdout);assert v['error'] is None;return hashlib.sha256(json.dumps(v['data'],sort_keys=True).encode()).hexdigest()
before=meta();vms=observation('vm','list');jobs=observation('operation','list');start=time.monotonic()
r=subprocess.run(['/usr/bin/virmill','import','inspect',str(source),'--timeout','20m','--output','json','--non-interactive'],capture_output=True,text=True,timeout=1210)
(out/'response.json').write_text(r.stdout);(out/'stderr.txt').write_text(r.stderr)
response=json.loads(r.stdout) if r.stdout else {};data=response.get('data') or {}
report={'status':'passed' if r.returncode==0 and response.get('error') is None else 'failed','exitCode':r.returncode,'elapsedSeconds':round(time.monotonic()-start,3),'error':response.get('error'),'stderr':r.stderr,'sourceMetadataPreserved':meta()==before,'guestInventoryPreserved':observation('vm','list')==vms,'jobsPreserved':observation('operation','list')==jobs,'sourceBytes':before[2],'sourceSHA256':data.get('sha256'),'systems':[{'id':s['id'],'name':s.get('name'),'diskIDs':s['diskIDs']} for s in data.get('systems',[])],'members':len(data.get('members',[])),'integrity':data.get('integrity'),'readiness':data.get('readiness'),'responseSHA256':hashlib.sha256(r.stdout.encode()).hexdigest(),'scope':'actual owner OVA read-only integrity/descriptor inspection; no conversion, VM creation or boot'}
if not all(report[k] for k in ('sourceMetadataPreserved','guestInventoryPreserved','jobsPreserved')):report['status']='failed'
(out/'report.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report,sort_keys=True))
raise SystemExit(report['status']!='passed')
