import argparse,hashlib,json,os,socket,sqlite3,subprocess,time
from pathlib import Path


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False

p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--execute-disposable',action='store_true',required=True);a=p.parse_args()
assert authorized_test_host() and os.getuid()==1000
root=a.root.resolve(strict=True);assert root.is_relative_to(Path.home()/'virmill-tests')
out=root/'restart';out.mkdir(mode=0o700)
report={'status':'failed','scope':'restart idle ordinary-user coordinator into verified installed binary; no guest operations'}
def run(*args):
 r=subprocess.run(args,capture_output=True,text=True,timeout=30);assert r.returncode==0,(args,r.stderr);return r.stdout

def sha(b):return hashlib.sha256(b).hexdigest()
def cli(*args):
 d=json.loads(run('/usr/bin/virmill',*args,'--output','json','--non-interactive'));assert d['error'] is None;return d['data']
def journal():
 path=Path.home()/'.local/state/virmill/journal.db'
 with sqlite3.connect(path.as_uri()+'?mode=ro',uri=True) as c:
  assert c.execute('PRAGMA user_version').fetchone()[0]==3
  states=[json.loads(row[0])['state'] for row in c.execute('SELECT body FROM jobs')]
  assert all(s in ('succeeded','failed','partial','canceled','recovery-required') for s in states),states
  return {'jobCount':len(states),'tables':{table:sha(json.dumps(c.execute('SELECT * FROM '+table+' ORDER BY rowid').fetchall(),default=lambda x:x.hex(),sort_keys=True).encode()) for table in ('jobs','locks','events','plans')}}
def media():
 return {p.name:[p.stat().st_ino,p.stat().st_size,p.stat().st_mtime_ns,p.stat().st_ctime_ns] for p in (Path.home()/'images').iterdir()}
def vms():return sha(json.dumps(cli('vm','list'),sort_keys=True).encode())
try:
 expected=json.loads((root/'binaries.json').read_text());assert sha(Path('/usr/bin/virmilld').read_bytes())==expected['virmilld']
 unit=Path('/usr/lib/systemd/user/virmilld.service').read_text();assert 'PrivateTmp=no' in unit and 'NoNewPrivileges=yes' in unit and 'UMask=0077' in unit
 before={'journal':journal(),'vms':vms(),'media':media()};report['before']=before
 pid=run('systemctl','--user','show','virmilld.service','-p','MainPID','--value').strip();report['oldPID']=pid;report['oldUIDMap']=Path('/proc/'+pid+'/uid_map').read_text()
 run('systemctl','--user','daemon-reload');run('systemctl','--user','restart','virmilld.service')
 for _ in range(50):
  try: cli('operation','list','--timeout','1s');break
  except (AssertionError,FileNotFoundError):time.sleep(.1)
 newpid=run('systemctl','--user','show','virmilld.service','-p','MainPID','--value').strip();assert int(newpid)>1 and newpid!=pid
 report['newPID']=newpid;report['newUIDMap']=Path('/proc/'+newpid+'/uid_map').read_text()
 assert report['newUIDMap']==Path('/proc/self/uid_map').read_text()
 assert sha(Path('/proc/'+newpid+'/exe').read_bytes())==expected['virmilld']
 after={'journal':journal(),'vms':vms(),'media':media()};report['after']=after;assert before==after
 report['version']=cli('version');report['status']='passed';report['guestsJobsMediaPreserved']=True
except BaseException as e:report['error']=repr(e)
finally:(out/'report.json').write_text(json.dumps(report,indent=2)+'\n');print(json.dumps(report,sort_keys=True))
raise SystemExit(report['status']!='passed')
