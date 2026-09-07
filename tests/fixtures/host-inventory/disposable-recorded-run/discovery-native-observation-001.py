import errno,fcntl,hashlib,ipaddress,json,os,pathlib,pty,re,select,sqlite3,struct,subprocess,termios,time,xml.etree.ElementTree as ET
root=pathlib.Path.home()/'virmill-tests/run-65930c6-20260907';env={**os.environ,**json.loads((root/'environment.json').read_text()),'TERM':'xterm-256color'}
def cli(*args,expect_error=False):
 p=subprocess.run(['virmill',*args,'--output','json','--non-interactive'],env=env,text=True,capture_output=True,timeout=45)
 result=json.loads(p.stdout)
 if expect_error:assert p.returncode and result['error'];return result
 if p.returncode:raise AssertionError(result['error'])
 assert result['error'] is None
 return result['data']
def rows(sql):
 with sqlite3.connect((root/'state/virmill/journal.db').as_uri()+'?mode=ro',uri=True) as db:return list(db.execute(sql))
def save(name,data):
 with (root/name).open('x') as f:json.dump(data,f,indent=2)
before={t:rows('SELECT * FROM '+t) for t in ('jobs','plans','events','locks')}
assert len(before['jobs'])==24 and not before['locks']
assert cli('version')['revision'].startswith('def0b10')
pci=cli('host','pci','list');save('discovery-pci-def0b10.json',pci)
names=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','nodedev-list','--cap','pci'],text=True).split()
assert names and {d['name'] for d in pci['devices']}==set(names)
driver_count=0
for d in pci['devices']:
 raw=subprocess.check_output(['virsh','--readonly','-c','qemu:///system','nodedev-dumpxml',d['name']],text=True)
 n=ET.fromstring(raw);cap=n.find("./capability[@type='pci']")
 bdf='%04x:%02x:%02x.%x'%tuple(int(cap.findtext(f)) for f in ('domain','bus','slot','function'))
 assert bdf==d['address']
 assert d['driver']==n.findtext('./driver/name','')
 if d['driver']:driver_count+=1
 assert d['vendorID']==cap.find('vendor').attrib['id'] and d['productID']==cap.find('product').attrib['id']
 assert d['iommuGroup'] is None if cap.find('iommuGroup') is None else d['iommuGroup']==int(cap.find('iommuGroup').attrib['number'])
assert driver_count>0
addresses=json.loads(subprocess.check_output(['ip','-j','-4','address','show'],text=True))
lan=next(str(ipaddress.ip_network(x['local']+'/'+str(x['prefixlen']),strict=False)) for iface in addresses if iface['ifname']!='lo' for x in iface.get('addr_info',[]) if x['scope']=='global')
networks=cli('network','list');save('discovery-networks-def0b10.json',networks)
candidates=[lan,'198.18.249.0/24','fded:249::/64'];network_cases=[]
for n in networks:
 for layer in ('liveXML','persistentXML'):
  if not n[layer]:continue
  tree=ET.fromstring(n[layer])
  for ip in tree.findall('./ip'):
   prefix=str(ipaddress.ip_network(ip.attrib['address']+'/'+ip.attrib.get('prefix',ip.attrib.get('netmask','')),strict=False))
   if prefix not in candidates:candidates.append(prefix)
   network_cases.append((prefix,'network-live' if layer=='liveXML' else 'network-persistent',n['key']['uuid']))
assert network_cases
inp={'candidates':candidates,'planned':[{'id':'explicit-test-v4','cidr':'198.18.249.0/24'},{'id':'explicit-test-v6','cidr':'fded:249::/64'}]}
report=cli('network','cidr','check','--input',json.dumps(inp));save('discovery-cidrs-def0b10.json',report)
items={c['cidr']:c['conflicts'] for c in report['candidates']}
assert any(x['source']=='address' for x in items[lan])
for cidr,source,id in network_cases:assert any(x['source']==source and x['id']==id for x in items[cidr])
for cidr,id in [('198.18.249.0/24','explicit-test-v4'),('fded:249::/64','explicit-test-v6')]:assert any(x['source']=='planned' and x['id']==id for x in items[cidr])
assert not report['reserved'] and not report['isolationVerified'] and report['ignoredDefaultRoutes']>=1
for c in report['candidates']:assert all(not(x['source']=='route' and ipaddress.ip_network(x['cidr']).prefixlen==0) for x in c['conflicts'])
assert cli('network','cidr','check','--input','{"candidates":["10.7.0.1/24"]}',expect_error=True)['error']['code']=='INVALID_INPUT'
assert cli('network','cidr','check','--connection','qemu+ssh://unapproved.invalid/system','--input','{"candidates":["10.7.0.0/24"]}',expect_error=True)['error']['code']=='UNSUPPORTED_CAPABILITY'
# Actual PTYs exercise the installed CLI/TUI against the same coordinator. Raw
# inventories/transcripts remain private on this host; only counts and hashes log.
def tui(section,down,form,markers,name):
 master,slave=pty.openpty();fcntl.ioctl(slave,termios.TIOCSWINSZ,struct.pack('HHHH',70,180,0,0));data=bytearray()
 p=subprocess.Popen(['virmill','tui'],stdin=slave,stdout=slave,stderr=slave,env=env,start_new_session=True);os.close(slave)
 def drain(seconds=.08):
  end=time.monotonic()+seconds
  while time.monotonic()<end:
   if select.select([master],[],[],min(.04,max(0,end-time.monotonic())))[0]:
    try:b=os.read(master,65536)
    except OSError as e:
     if e.errno==errno.EIO:return
     raise
    if not b:return
    data.extend(b)
 def wait(marker,offset=0):
  end=time.monotonic()+20
  while time.monotonic()<end:
   drain()
   if marker in data[offset:]:return
   if p.poll() is not None:break
  raise AssertionError('TUI marker absent: '+repr(marker))
 try:
  wait(b'Overview')
  for _ in range(section):os.write(master,b'\t');drain()
  for _ in range(down):os.write(master,b'\x1b[B');drain()
  start=len(data);os.write(master,b'\r');drain()
  if form:
   wait(b'Input:',start);start=len(data);os.write(master,json.dumps({'input':form}).encode()+b'\r')
  wait(markers[0],start)
  for _ in range(40):os.write(master,b'\x1b[6~');drain(.02)
  for marker in markers:wait(marker,start)
  assert b'"error": null' in data[start:]
  os.write(master,b'q');assert p.wait(timeout=10)==0
 finally:
  if p.poll() is None:p.terminate();p.wait(timeout=10)
  os.close(master)
  with (root/name).open('xb') as f:f.write(data)
 return hashlib.sha256(data).hexdigest()
pci_tui=tui(7,0,None,[b'"devices":',pci['devices'][0]['address'].encode()], 'discovery-pci-tui-def0b10.ansi')
cidr_tui=tui(2,1,{'candidates':['198.18.249.0/24'],'planned':[{'id':'tui-planned','cidr':'198.18.249.0/24'}]},[b'"candidates":',b'"source": "planned"',b'"reserved": false'],'discovery-cidr-tui-def0b10.ansi')
assert {t:rows('SELECT * FROM '+t) for t in before}==before
baseline=json.loads((root/'discovery-upgrade-def0b10.json').read_text())
for vm,sha in baseline['allFourStoppedVMXMLSHA256'].items():
 assert subprocess.check_output(['virsh','--readonly','-c','qemu:///system','domstate',vm],text=True).strip()=='shut off'
 assert hashlib.sha256(subprocess.check_output(['virsh','--readonly','-c','qemu:///system','dumpxml','--inactive',vm])).hexdigest()==sha
assert cli('network','list')==networks
volumes=json.loads((root/'helper-access-final-b7fe053.json').read_text())['allFourTestVolumeSHA256']
for path,sha in volumes.items():assert subprocess.check_output(['sudo','-n','sha256sum',path],text=True).split()[0]==sha
assert subprocess.run(['systemctl','is-active','virmill-host-helper.socket','virmill-host-helper.service'],text=True,capture_output=True).stdout.splitlines()==['inactive','inactive']
summary={'nativePCIcount':len(pci['devices']),'reportedDriversComparedWithNativeXML':driver_count,'nativeIOMMUGroupsObserved':len({d['iommuGroup'] for d in pci['devices'] if d['iommuGroup'] is not None}),'nativeNetworkLayersCompared':len(network_cases),'hostAddressConflictObserved':True,'explicitIPv4IPv6PlannedConflictsObserved':True,'defaultRoutesExcluded':report['ignoredDefaultRoutes'],'invalidCIDRAndRemoteURIRefused':True,'cliAndTUI':'passed','tuiTranscriptSHA256':{'pci':pci_tui,'cidr':cidr_tui},'jobsPlansEventsLocksUnchanged':True,'allFourStoppedVMsAndVolumesPreserved':True,'networkInventoryUnchanged':True,'helperStillStopped':True,'SELinux':subprocess.check_output(['getenforce'],text=True).strip(),'physicalPCIOrPacketIsolationQualified':False}
save('discovery-final-def0b10.json',summary);print(json.dumps(summary),flush=True)
