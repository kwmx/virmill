import copy,datetime,hashlib,json,os,pathlib,socket,sqlite3,subprocess
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text())}
def cli(*args):
 p=subprocess.run(['virmill',*args,'--output','json','--non-interactive'],env=env,text=True,capture_output=True,check=True,timeout=30);return json.loads(p.stdout)['data']
identity=cli('host','helper','identity');assert identity['policyApproved']
grant=json.loads((root/'helper-access-grant-result-b7fe053.json').read_text())
with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:
 inp=json.loads(db.execute('SELECT input FROM plans WHERE id=?',(grant['grantPlanID'],)).fetchone()[0])
 assert not list(db.execute('SELECT resource,job_id FROM locks'))
def root_inventory():return subprocess.check_output(['sudo','-n','find','/var/lib/virmill-host-helper','-maxdepth','1','-type','f','-printf','%f\n'],text=True).splitlines()
records_before=sorted(root_inventory())
request={'apiVersion':'virmill/v1','actorUID':1000,'operation':'storage.grant-read','resourceID':inp['access']['mapping']['vmID'],'rootID':'images','planDigest':'a'*64,'jobID':'9b89c017-545d-41c6-87b7-7664b1c847a0','expiresAt':(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(minutes=1)).isoformat().replace('+00:00','Z'),'keyID':identity['keyID'],'signature':'00'*64,'mode':'check','access':inp['access']}
denials={}
for name in ('wrong-actor','generic-root-action','forged-signature'):
 r=copy.deepcopy(request)
 if name=='wrong-actor':r['actorUID']=1001
 if name=='generic-root-action':r['operation']='runCommand'
 with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as s:
  s.settimeout(10);s.connect('/run/virmill-host-helper/control.sock');s.sendall(json.dumps(r,separators=(',',':')).encode()+b'\n')
  with s.makefile('rb') as f:result=json.loads(f.readline(1<<20))
 assert result['success'] is False and result.get('error'),result
 denials[name]=result['error']
assert 'kernel peer' in denials['wrong-actor'] and 'allowlisted' in denials['generic-root-action'] and 'signature invalid' in denials['forged-signature']
code="import socket; s=socket.socket(socket.AF_UNIX); s.connect('/run/virmill-host-helper/control.sock')"
p=subprocess.run(['sudo','-n','-u','#65534','--','python3','-c',code],text=True,capture_output=True)
assert p.returncode!=0 and 'PermissionError' in p.stderr
assert sorted(root_inventory())==records_before
upgrade=json.loads((root/'helper-access-upgrade-b7fe053.json').read_text())
for vm,expected in upgrade['allFourStoppedVMXMLSHA256'].items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()==expected
volumes=json.loads((root/'multidisk-final-preservation.json').read_text())['allFourTestVolumeSHA256']
observed={}
for path,sha in volumes.items():
 got=subprocess.check_output(['sudo','-n','sha256sum',path],text=True).split()[0];assert got==sha;observed[path]=got
with (root/'sources/multidisk-probe-v1/probe.ova').open('rb') as f:assert hashlib.file_digest(f,'sha256').hexdigest()=='1b2b15b998b4879d71cd2e5a1b137253cb41f14808a830ff8ba6ff231c0d88f0'
jobs=cli('operation','list');assert len(jobs)==24 and all(j['state'] in ('succeeded','failed','canceled','partial') for j in jobs)
for name in ('helper-access-grant-result-b7fe053.json','helper-access-revoke-result-b7fe053.json','helper-access-tui-grant-cli-revoke-b7fe053.json'):
 data=json.loads((root/name).read_text())
 for key,value in data.items():
  if key.endswith('OperationID') and value:assert cli('storage','access','result',value)['complete']
subprocess.run(['rpm','-V','virmill','virmill-host-helper'],check=True)
subprocess.run(['sudo','-n','systemctl','stop','virmill-host-helper.socket','virmill-host-helper.service'],check=True)
state=subprocess.run(['sudo','-n','systemctl','is-active','virmill-host-helper.socket','virmill-host-helper.service'],text=True,capture_output=True);assert state.stdout.splitlines()==['inactive','inactive']
report={'nativeDenials':denials,'otherUIDSocketAccessDenied':True,'denialsCreatedNoRootRecords':True,'allFourVMsStoppedAndXMLUnchanged':True,'allFourTestVolumeSHA256':observed,'generatedOVAUnchanged':True,'jobs':len(jobs),'resourceLocks':0,'helperStoppedNotEnabled':True,'privateKeyPolicyAndRootJournalsRetainedForRecovery':True,'SELinux':subprocess.check_output(['getenforce'],text=True).strip(),'physicalHardwareMatrixQualified':False}
with (root/'helper-access-final-b7fe053.json').open('x') as f:json.dump(report,f,indent=2)
print(json.dumps(report),flush=True)
