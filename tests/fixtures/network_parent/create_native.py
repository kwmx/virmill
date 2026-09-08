#!/usr/bin/env python3
"""Owner-authorized disposable network creation; never changes existing guests/media."""
import argparse, hashlib, ipaddress, json, os, pathlib, re, shutil, subprocess, time, uuid, xml.etree.ElementTree as ET

def network_rules(definition):
 bridge=definition['bridge']
 if not re.fullmatch(r'vm[0-9a-f]{12}',bridge):raise ValueError('only generated bridge rules may be cleaned')
 rules=[['eb','filter',chain,'-32768',flag,bridge,'-p','IPv6','-j','DROP'] for chain,flag in [('INPUT','--logical-in'),('OUTPUT','--logical-out'),('FORWARD','--logical-in'),('FORWARD','--logical-out')]]
 if definition['hostAccess']=='allow':return rules
 if definition['type']=='guest-only':
  if definition['hostAccess']!='deny':raise ValueError('guest-only policy differs')
  rules += [['eb','filter',chain,'-32768',flag,bridge,'-p',protocol,'-j','DROP'] for chain,flag in [('INPUT','--logical-in'),('OUTPUT','--logical-out')] for protocol in ('IPv4','ARP')]
 elif definition['type'] in ('nat','lab') and definition['hostAccess']=='services-only':
  if definition['dhcpEnabled']:
   gateway=str(ipaddress.IPv4Network(definition['ipv4CIDR']).network_address+1)+'/32'
   rules += [['ipv4','mangle','INPUT',str(-32767+i),'-d',destination,'-i',bridge,'-p','udp','-m','udp','--sport','68','--dport','67','-j','ACCEPT'] for i,destination in enumerate(('255.255.255.255/32',gateway))]
   rules += [['ipv4','mangle','INPUT',str(-32765+i),'-d',gateway,'-i',bridge,'-p',protocol,'-m',protocol,'--dport','53','-j','ACCEPT'] for i,protocol in enumerate(('udp','tcp'))]
  rules.append(['ipv4','mangle','INPUT','-32763','-i',bridge,'-j','DROP'])
 else:raise ValueError('unsupported reviewed policy')
 return rules

def main():
 p=argparse.ArgumentParser();p.add_argument('--execute-disposable',action='store_true',required=True);p.add_argument('--root',type=pathlib.Path,required=True);p.add_argument('--lab-cidr',required=True);p.add_argument('--nat-cidr',required=True);p.add_argument('--profile',choices=('allow','protected'),default='allow');p.add_argument('--guest-cidr');p.add_argument('--host4-target');p.add_argument('--dhcp',choices=('on','off'),default='on');p.add_argument('--kinds',nargs='+',choices=('lab','nat','guest-only'));a=p.parse_args()
 if a.profile=='protected':assert a.guest_cidr and a.host4_target
 else:assert not a.guest_cidr and not a.host4_target
 profiles=[('lab',a.lab_cidr),('nat',a.nat_cidr)]+([('guest-only',a.guest_cidr)] if a.profile=='protected' else [])
 if a.kinds:
  assert len(set(a.kinds))==len(a.kinds) and set(a.kinds).issubset({kind for kind,_ in profiles})
  profiles=[item for item in profiles if item[0] in a.kinds]
 networks=[ipaddress.IPv4Network(c) for _,c in profiles];assert all(n.is_private and n.prefixlen==24 for n in networks) and all(not left.overlaps(right) for i,left in enumerate(networks) for right in networks[i+1:])
 root=a.root.resolve(strict=True);assert root.is_relative_to(pathlib.Path.home()/'virmill-tests') and os.getuid()!=0
 run_id=uuid.uuid4().hex;results=root/'results';results.mkdir(mode=0o700)
 env=dict(os.environ);events=[];created=[];daemon=None;policy_before=None;dropin=None;dropin_installed=False;current_policy_sha=None;helper_root='/run/virmill-network-fixture-'+run_id
 report={'status':'failed','runID':run_id,'scope':'native owned network definition, declared policy rules and namespace packets; not real-guest or release qualification','profile':a.profile,'selectedKinds':[kind for kind,_ in profiles],'created':created}
 def save(): (results/'report.json').write_text(json.dumps(report,indent=2)+'\n');(results/'commands.json').write_text(json.dumps(events,indent=2)+'\n')
 def run(argv,timeout=45,check=True):
  start=time.monotonic();out=subprocess.run(argv,capture_output=True,text=True,env=env,timeout=timeout)
  assert len(out.stdout)+len(out.stderr)<4<<20
  events.append({'argv':argv,'exitCode':out.returncode,'seconds':round(time.monotonic()-start,3),'stdout':out.stdout,'stderr':out.stderr});save()
  if check and out.returncode:raise RuntimeError('command failed: '+repr(argv))
  return out
 def virsh(*args):return run(['virsh','--readonly','-c','qemu:///system',*args]).stdout
 def cli(*args,check=True):
  out=run([str(root/'bin/virmill'),*args,'--output','json','--non-interactive','--timeout','600s'],check=check,timeout=630)
  value=json.loads(out.stdout)
  if check:assert value['error'] is None,value
  return value
 def snapshots():
  guests={};nets={}
  for ident in virsh('list','--all','--uuid').split():guests[ident]={'state':virsh('domstate',ident).strip(),'xmlSHA256':hashlib.sha256(virsh('dumpxml',ident,'--inactive').encode()).hexdigest()}
  for ident in virsh('net-list','--all','--uuid').split():
   if any(n['uuid']==ident for n in created):continue
   nets[ident]={'xmlSHA256':hashlib.sha256(virsh('net-dumpxml',ident,'--inactive').encode()).hexdigest(),'info':virsh('net-info',ident)}
  return {'guests':guests,'networks':nets}
 def firewall(permanent=False):return run(['sudo','-n','firewall-cmd',*(['--permanent'] if permanent else []),'--direct','--get-all-rules']).stdout
 def install_policy(value):
  nonlocal current_policy_sha
  assert run(['sudo','-n','sha256sum','/etc/virmill/helper-policy.json']).stdout.split()[0]==current_policy_sha
  path=root/'policy-next.json';path.write_text(json.dumps(value,indent=2)+'\n');os.chmod(path,0o600)
  run(['sudo','-n','install','-o','root','-g','root','-m','0644',str(path),'/etc/virmill/helper-policy.json'])
  current_policy_sha=hashlib.sha256(path.read_bytes()).hexdigest()
 try:
  manifest=json.loads((root/'binaries.json').read_text());assert set(manifest)=={'virmill','virmilld','virmill-host-helper'}
  for name,digest in manifest.items():assert hashlib.sha256((root/'bin'/name).read_bytes()).hexdigest()==digest
  report['binaries']=manifest;before=snapshots();report['preservationBefore']=before
  report['firewallBefore']={'runtime':firewall(),'permanent':firewall(True),'zones':run(['sudo','-n','firewall-cmd','--get-active-zones']).stdout}
  for unit in ('virmill-host-helper.service','virmill-host-helper.socket'):
   assert run(['systemctl','is-active',unit],check=False).stdout.strip()=='inactive'
  policy_before=run(['sudo','-n','cat','/etc/virmill/helper-policy.json']).stdout
  (root/'policy-original.json').write_text(policy_before);os.chmod(root/'policy-original.json',0o600)
  report['originalPolicySHA256']=hashlib.sha256(policy_before.encode()).hexdigest();current_policy_sha=report['originalPolicySHA256']
  assert run(['sudo','-n','stat','-c','%a:%u:%g','/etc/virmill/helper-policy.json']).stdout.strip()=='644:0:0'
  for name in ('state','runtime','cache','data','config'):
   target=root/name;target.mkdir(mode=0o700);env['XDG_'+('RUNTIME_DIR' if name=='runtime' else name.upper()+'_HOME')]=str(target)
  config=root/'config/virmill';config.mkdir(mode=0o700)
  run(['openssl','genpkey','-algorithm','ED25519','-out',str(config/'helper-key.pem')]);os.chmod(config/'helper-key.pem',0o600)
  log=(results/'daemon.log').open('w');daemon=subprocess.Popen([str(root/'bin/virmilld')],env=env,stdout=log,stderr=subprocess.STDOUT)
  for _ in range(100):
   if (root/'runtime/virmill/control.sock').exists():break
   if daemon.poll() is not None:raise RuntimeError('coordinator exited')
   time.sleep(0.05)
  identity=cli('host','helper','identity')['data'];report['helperIdentity']={k:identity[k] for k in ('actorUID','keyID','publicKey')}
  # The root helper executable is copied to a fresh root-owned runtime directory.
  run(['sudo','-n','mkdir','-m','0700',helper_root]);run(['sudo','-n','install','-o','root','-g','root','-m','0755',str(root/'bin/virmill-host-helper'),helper_root+'/virmill-host-helper'])
  assert run(['sudo','-n','sha256sum',helper_root+'/virmill-host-helper']).stdout.split()[0]==manifest['virmill-host-helper']
  dropin='/run/systemd/system/virmill-host-helper.service.d/60-virmill-network-fixture.conf'
  assert run(['sudo','-n','test','-e',dropin],check=False).returncode==1
  (root/'helper-override.conf').write_text('[Service]\nExecStart=\nExecStart='+helper_root+'/virmill-host-helper\n')
  run(['sudo','-n','mkdir','-p','/run/systemd/system/virmill-host-helper.service.d']);run(['sudo','-n','install','-o','root','-g','root','-m','0644',str(root/'helper-override.conf'),dropin]);dropin_installed=True;run(['sudo','-n','systemctl','daemon-reload'])
  run(['sudo','-n','systemctl','start','virmill-host-helper.socket'])
  packet_source=root/'network_packet_fixture.py';packet_sha=hashlib.sha256(packet_source.read_bytes()).hexdigest()
  run(['sudo','-n','install','-o','root','-g','root','-m','0644',str(packet_source),helper_root+'/network_packet_fixture.py'])
  assert run(['sudo','-n','sha256sum',helper_root+'/network_packet_fixture.py']).stdout.split()[0]==packet_sha
  policy=json.loads(policy_before);policy.setdefault('keys',{})[identity['keyID']]=identity['publicKey'];policy.setdefault('actors',[])
  if identity['actorUID'] not in policy['actors']:policy['actors'].append(identity['actorUID'])
  for kind,cidr in profiles:
   host_access='deny' if kind=='guest-only' else ('services-only' if a.profile=='protected' else 'allow');dhcp=kind!='guest-only' and a.dhcp=='on'
   doc={'apiVersion':'virmill/v1','kind':'Network','metadata':{'name':'native-'+kind,'tags':['disposable-fixture']},'spec':{'type':kind,'hostAccess':host_access,'egress':'any' if kind=='nat' else 'none','ipv4':{'cidr':cidr,'dhcp':{'enabled':dhcp,'advertiseDefaultRoute':dhcp and kind=='nat'}},'ipv6':{'mode':'disabled'}}}
   path=root/(kind+'.json');path.write_text(json.dumps(doc)+'\n')
   plan=cli('network','create',str(path),'--plan')['data'];d=plan['review']['definition'];assert d['type']==kind and d['hostAccess']==host_access and d['dhcpEnabled']==dhcp and re.fullmatch(r'vm[0-9a-f]{12}',d['bridge'])
   item={'uuid':d['uuid'],'bridge':d['bridge'],'kind':kind,'plan':plan,'definition':d,'cleanupRules':network_rules(d),'ruleCleanupNeeded':True};created.append(item);save()
   # First apply must fail without the independent network permission and before definition.
   denied=cli('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key',str(uuid.uuid4()),*[part for ack in plan['acknowledgements'] for part in ('--ack',ack)],check=False)
   assert denied['error'] is not None;item['unapprovedApply']=denied
   policy.setdefault('protectedNetworks' if a.profile=='protected' else 'networks',[]).append({'actorUID':identity['actorUID'],'keyID':identity['keyID'],'resourceID':d['uuid']});install_policy(policy)
   applied=cli('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key',str(uuid.uuid4()),*[part for ack in plan['acknowledgements'] for part in ('--ack',ack)],'--wait',check=False)
   item['apply']=applied
   assert applied['error'] is None and applied['data']['state']=='succeeded',applied
   item['result']=cli('network','creation','result',applied['data']['operationID'])
   xml=virsh('net-dumpxml',d['uuid']);item['liveXMLSHA256']=hashlib.sha256(xml.encode()).hexdigest();(results/(kind+'-network.xml')).write_text(xml)
   item['bridgeIPv6Disable']=run(['cat','/proc/sys/net/ipv6/conf/'+d['bridge']+'/disable_ipv6']).stdout.strip()
   packet_id=str(uuid.uuid4());packet_output=root/('network-packet-'+packet_id)
   static_args=[] if dhcp else ['--static-a4',str(ipaddress.IPv4Network(cidr).network_address+10),'--static-b4',str(ipaddress.IPv4Network(cidr).network_address+11)]
   if kind=='guest-only':static_args += ['--static-cidr4',cidr,'--host4-target',a.host4_target]
   packet=run(['sudo','-n','python3','-I',helper_root+'/network_packet_fixture.py','run','--root',str(root),'--execute-reviewed','--confirm-new-unused-network','--run-id',packet_id,'--recipe-sha256',packet_sha,'--network-id',d['uuid'],'--network-xml-sha256',item['liveXMLSHA256'],'--bridge',d['bridge'],'--kind',kind,'--host-access',host_access,'--dhcp','on' if dhcp else 'off',*static_args,'--forward4-target','1.1.1.1','--forward4-port','443',*(['--dns-own-lease'] if dhcp and kind=='lab' and host_access=='services-only' else ['--dns-name','example.com'] if dhcp and kind=='nat' else []),'--output',str(packet_output)],timeout=270,check=False)
   item['packetExitCode']=packet.returncode;item['packetReport']=json.loads(run(['sudo','-n','cat',str(packet_output/'report.json')]).stdout)
   if packet.returncode!=0:assert item['packetReport'].get('cleanup',{}).get('status')=='verified',item['packetReport']
  report['status']='passed-native-creation' if all(item['packetExitCode']==0 for item in created) else 'failed-packet-qualification';save()
 finally:
  # Stop every test writer before any cleanup, including after a detached client.
  if daemon is not None:daemon.terminate();daemon.wait(timeout=20);daemon=None
  if dropin_installed:run(['sudo','-n','systemctl','stop','virmill-host-helper.socket','virmill-host-helper.service'],check=False)
  # Shut down only networks named by this run. Definitions, reservations and journals remain.
  for item in reversed(created):
   net=run(['virsh','--readonly','-c','qemu:///system','net-info',item['uuid']],check=False)
   if net.returncode==0 and re.search(r'^Active:\s+yes$',net.stdout,re.M):run(['virsh','-c','qemu:///system','net-destroy',item['uuid']],check=False)
   # Remove only this run's exact definition-bound test rules. No chains/tables.
   assert item['cleanupRules']==network_rules(item['definition'])
   for permanent in (False,True):
    for rule in item['cleanupRules']:
     base=['sudo','-n','firewall-cmd',*(['--permanent'] if permanent else []),'--direct']
     if run([*base,'--query-rule',*rule],check=False).returncode==0:run([*base,'--remove-rule',*rule],check=False)
  if daemon is not None:daemon.terminate();daemon.wait(timeout=15)
  if dropin_installed:
   run(['sudo','-n','systemctl','stop','virmill-host-helper.socket','virmill-host-helper.service'],check=False)
   run(['sudo','-n','rm','--',dropin],check=False);run(['sudo','-n','systemctl','daemon-reload'],check=False)
  if policy_before is not None:
   assert run(['sudo','-n','sha256sum','/etc/virmill/helper-policy.json']).stdout.split()[0]==current_policy_sha
   run(['sudo','-n','install','-o','root','-g','root','-m','0644',str(root/'policy-original.json'),'/etc/virmill/helper-policy.json'])
   report['policyRestored']=run(['sudo','-n','sha256sum','/etc/virmill/helper-policy.json']).stdout.split()[0]==report['originalPolicySHA256']
  if 'preservationBefore' in report:report['preservationAfter']=snapshots();report['existingResourcesPreserved']=report['preservationBefore']==report['preservationAfter']
  if 'firewallBefore' in report:report['firewallAfter']={'runtime':firewall(),'permanent':firewall(True),'zones':run(['sudo','-n','firewall-cmd','--get-active-zones']).stdout};report['existingFirewallPreserved']=report['firewallBefore']==report['firewallAfter']
  save();print(json.dumps(report,indent=2))
 if report['status']!='passed-native-creation':raise RuntimeError('one or more packet profiles failed qualification')
 if not report.get('existingResourcesPreserved') or not report.get('existingFirewallPreserved') or not report.get('policyRestored'):raise RuntimeError('preservation verification failed')

if __name__=='__main__':main()
