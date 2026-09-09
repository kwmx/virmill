#!/usr/bin/env python3
"""Guarded parent-operated native hardware wizard probe on the owner test VM.

Creates only a new bounded fixture OVA, two prepared disks and one powered-off
guest. Never starts a guest or changes an existing guest/network/source. Tests
actual TUI settings -> shared plan -> native definition; does not claim boot.
"""
import argparse, hashlib, io, json, os, re, socket, tarfile, time
from pathlib import Path
from tui_workspace_probe import Runner, Terminal, inventory, generation, require
from import_options_probe import selected_label

p=argparse.ArgumentParser()
p.add_argument('--execute-disposable',action='store_true',required=True)
p.add_argument('--binary',default='/usr/bin/virmill')
p.add_argument('--binary-sha256',required=True)
p.add_argument('--connection',default='qemu:///system')
p.add_argument('--output',type=Path,required=True)
a=p.parse_args()
require(socket.gethostname() in ('virmill-test','virmill-test.home') and os.getuid()==1000,'wrong test host/actor')
require(a.connection=='qemu:///system' and a.binary=='/usr/bin/virmill','explicit installed system connection required')
root=a.output.absolute(); require(root.parent.resolve().is_relative_to(Path.home()/'virmill-tests') and not root.exists(),'new test output required')
root.mkdir(mode=0o700)
fd=os.open(a.binary,os.O_RDONLY|os.O_NOFOLLOW)
with os.fdopen(os.dup(fd),'rb') as f: require(hashlib.file_digest(f,'sha256').hexdigest()==a.binary_sha256,'wrong binary')
r=Runner(a,root,fd)
report={'status':'failed','scope':'two tiny OVA disks; TUI hardware/export/mkdir; native powered-off definition; no boot claim'}
before=inventory(r.cli('vm','list'),a.connection)
prior_jobs=r.cli('operation','list')
owner_media={p.name:generation(p.stat()) for p in (Path.home()/'images').iterdir()}
t=None
try:
    # A reproducible archive with VirtualBox-compatible units and explicit SATA
    # attachments. No supplied owner media is copied or modified.
    name='virmill-hardware-'+root.parent.name[-8:]
    ovf='''<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1" xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData" xmlns:vssd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_VirtualSystemSettingData">
<References><File ovf:id="f1" ovf:href="boot.raw"/><File ovf:id="f2" ovf:href="data.raw"/></References>
<DiskSection><Disk ovf:diskId="boot" ovf:fileRef="f1" ovf:capacity="1048576" ovf:format="raw"/><Disk ovf:diskId="data" ovf:fileRef="f2" ovf:capacity="1048576" ovf:format="raw"/></DiskSection>
<VirtualSystem ovf:id="hardware-fixture"><Name>hardware-fixture</Name><VirtualHardwareSection><System><vssd:VirtualSystemType>virtualbox-2.2</vssd:VirtualSystemType></System>
<Item><rasd:InstanceID>1</rasd:InstanceID><rasd:ResourceType>3</rasd:ResourceType><rasd:VirtualQuantity>2</rasd:VirtualQuantity></Item>
<Item><rasd:InstanceID>2</rasd:InstanceID><rasd:ResourceType>4</rasd:ResourceType><rasd:AllocationUnits>MegaBytes</rasd:AllocationUnits><rasd:VirtualQuantity>2048</rasd:VirtualQuantity></Item>
<Item><rasd:InstanceID>3</rasd:InstanceID><rasd:ResourceType>20</rasd:ResourceType></Item>
<Item><rasd:InstanceID>4</rasd:InstanceID><rasd:ResourceType>17</rasd:ResourceType><rasd:Parent>3</rasd:Parent><rasd:HostResource>ovf:/disk/boot</rasd:HostResource></Item>
<Item><rasd:InstanceID>5</rasd:InstanceID><rasd:ResourceType>17</rasd:ResourceType><rasd:Parent>3</rasd:Parent><rasd:HostResource>ovf:/disk/data</rasd:HostResource></Item>
</VirtualHardwareSection></VirtualSystem></Envelope>'''.encode()
    source=root/'fixture.ova'
    with tarfile.open(source,'w') as archive:
        for filename,raw in [('fixture.ovf',ovf),('boot.raw',bytes(1<<20)),('data.raw',bytes(1<<20))]:
            member=tarfile.TarInfo(filename);member.size=len(raw);member.mode=0o600;archive.addfile(member,io.BytesIO(raw))
    source_before=generation(source.stat())
    inspected=r.cli('import','inspect',str(source));r.save('inspection.json',inspected)
    require([d['capacityBytes'] for d in inspected['disks']]==[1<<20,1<<20],'disk defaults lost')
    require(next(i for i in inspected['systems'][0]['hardware'] if i['resourceType']=='4')['memoryMiB']==2048,'RAM units lost')
    choices=r.cli('vm','creation','options');r.save('hardware-options.json',choices)
    require(choices['maxVCPUs']>=3 and choices['hostMemoryMiB']>=1024,'insufficient fixture hardware')
    firmware=next((f for f in choices['firmware'] if f['firmware']['mode']=='bios'),None)
    if firmware is None:firmware=next((f for f in choices['firmware'] if f['firmware']['mode']=='uefi' and not f['firmware']['secureBoot'] and not f['firmware']['tpm'] and f['firmware']['format']=='raw'),None)
    require(firmware is not None,'no suitable firmware advertised for blank fixture')
    report['selectedFirmware']=firmware
    inp={'destination':str(root/'prepared'),'systemID':'hardware-fixture','disks':[{'id':d,'format':'raw','maximumVirtualBytes':1<<20} for d in ('boot','data')]}
    plan=r.cli('import','prepare',str(source),'--input',json.dumps(inp));r.save('preparation-plan.json',plan)
    def apply(plan,label):
        args=['plan','apply',plan['planID'],'--digest',plan['planDigest'],'--idempotency-key',root.name+'-'+label,'--detach']
        for ack in plan['acknowledgements']:args.extend(['--ack',ack])
        job=r.cli(*args);r.save(label+'-accepted.json',job)
        end=time.monotonic()+90
        while time.monotonic()<end:
            job=r.cli('operation','show',job['operationID'])
            if job['state'] in ('succeeded','failed','partial','canceled','recovery-required'):break
            time.sleep(.3)
        r.save(label+'-job.json',job)
        require(job['state']=='succeeded',label+' did not succeed: '+json.dumps(job))
        return job
    prepared=apply(plan,'prepare');report['preparationOperationID']=prepared['operationID']
    t=Terminal(r,'hardware-wizard-80x24',80,24)
    def wait(label,predicate,key=None):return t.wait(label,predicate,t.send(key) if key is not None else -1)
    def focus(label):
        for i in range(30):
            text=t.screen.text()
            if selected_label(text,label):return
            wait('Focus '+label+' '+str(i),lambda s:s!=text,b'\t')
        raise RuntimeError('unreachable control '+label)
    def activate(label,predicate):focus(label);return wait(label,predicate,b'\r')
    def edit(label,value):
        focus(label);t.send(b'\x15')
        while t.read(.05) and not t.screen.complete():pass
        wait('Edit '+label,lambda s:value in s and selected_label(s,label),value.encode())
    wait('Overview',lambda s:'Virtual machines' in s)
    wait('Buttons',lambda s:'>[' in s,b'\t')
    for i in range(8):
        if '>[ Create VM ]' in t.screen.text():break
        old=t.screen.text();wait('Choose Create VM '+str(i),lambda s:s!=old,b'\x1b[C')
    require('>[ Create VM ]' in t.screen.text(),'Create VM button missing')
    wait('Prepared image chooser',lambda s:'hardware-fixture' in s and 'Choose prepared images' in s,b'\r')
    for i in range(50):
        if selected_label(t.screen.text(),'hardware-fixture'):break
        old=t.screen.text();wait('Select prepared source '+str(i),lambda s:s!=old,b'\x1b[B')
    wait('Detected hardware fields',lambda s:'CPU cores' in s and 'Memory (MiB)' in s,b'\r')
    require('2048' in t.screen.text(),'detected RAM absent from screen')
    edit('VM name',name);edit('CPU cores','3');edit('Memory (MiB)','1024')
    focus('Storage pool')
    for i in range(12):
        if re.search(r'Storage pool:.*< virmill-test >',t.screen.text()):break
        old=t.screen.text();wait('Choose test storage '+str(i),lambda s:s!=old,b'\x1b[C')
    require(re.search(r'Storage pool:.*< virmill-test >',t.screen.text()),'test pool not selected')
    activate('Hardware options',lambda s:'Advanced hardware' in s)
    focus('Firmware')
    for i in range(len(choices['firmware'])+1):
        if ('< '+firmware['label']+' >') in t.screen.text():break
        old=t.screen.text();wait('Choose fixture firmware '+str(i),lambda s:s!=old,b'\x1b[C')
    require(('< '+firmware['label']+' >') in t.screen.text(),'explicit firmware choice failed')
    activate('Done',lambda s:'VM options' in s)
    activate('Continue',lambda s:'Disks and boot' in s)
    require(re.search(r'Controller bus:.*< sata >',t.screen.text()),'detected SATA bus missing')
    activate('Continue',lambda s:'Network adapters' in s)
    activate('Export settings',lambda s:'File name' in s and 'Export settings' in s)
    wait('Export folder explorer',lambda s:'Choose a folder' in s,b'\x0f')
    wait('Folder location',lambda s:'Path:' in s,b'\x0c');t.send(b'\x15')
    while t.read(.05) and not t.screen.complete():pass
    wait('Fixture output location',lambda s:str(root) in s,str(root).encode())
    wait('Select fixture folder',lambda s:'Path:' not in s and ('Choose a folder' in s or 'File name' in s),b'\r')
    if 'Choose a folder' not in t.screen.text():wait('Reopen selected parent',lambda s:'Choose a folder' in s,b'\x0f')
    wait('New folder prompt',lambda s:'New folder' in s and 'Name:' in s,b'\x0e')
    wait('New folder name',lambda s:'exported' in s,b'exported')
    wait('Create folder',lambda s:'Folder created.' in s,b'\r')
    require((root/'exported').is_dir() and (root/'exported').stat().st_mode&0o777==0o700,'folder was not private')
    wait('Open new folder',lambda s:'exported' in s and 'Folder created.' not in s,b'\r')
    wait('Select new folder',lambda s:'File name' in s,b'\x13')
    wait('Export hardware settings',lambda s:'Settings exported:' in s,b'\r')
    exported=json.loads((root/'exported/virmill-vm-settings.json').read_text());r.save('export-readback.json',exported)
    require(exported['hardware']['vcpus']==3 and exported['hardware']['memoryMiB']==1024,'exported values differ')
    activate('Preview VM creation',lambda s:'Plan ID:' in s and 'Plan digest:' in s)
    matched=re.search(r'Plan ID:\s*([0-9a-f-]{36})',t.screen.text());require(matched is not None,'plan identity absent')
    creation=r.cli('plan','show',matched.group(1));r.save('creation-plan.json',creation)
    wait('Back retains hardware options',lambda s:'Network adapters' in s,b'\x1b')
    t.close();t=None
    job=apply(creation,'create');report['creationOperationID']=job['operationID']
    receipt=r.cli('vm','creation','result',job['operationID']);r.save('creation-result.json',receipt)
    require(receipt['complete'] is True,'creation receipt incomplete');vmid=receipt['receipt']['vmID'];vm=r.cli('vm','show',vmid);r.save('created-vm.json',vm)
    import xml.etree.ElementTree as E
    xml=E.fromstring(vm['persistentXML'])
    require(xml.findtext('vcpu')=='3' and xml.findtext('memory')=='1048576','native CPU/RAM differ')
    require(len(xml.findall("devices/disk[@device='disk']"))==2 and len(xml.findall('devices/interface'))==0,'disk/NIC definition differs')
    require(vm['state'] in ('stopped','shut off'),'fixture guest unexpectedly running')
    report.update(status='passed',vmID=vmid,vcpus=3,memoryMiB=1024,disks=2,guestStarted=False,sourceFixturePreserved=generation(source.stat())==source_before)
except BaseException as error:report['error']=repr(error)
finally:
    if t is not None:t.close()
    try:
        after=inventory(r.cli('vm','list'),a.connection)
        report['existingGuestsPreserved']=all(after.get(k)==v for k,v in before.items())
        current={j['operationID']:j for j in r.cli('operation','list')}
        report['existingJobsPreserved']=all(current.get(j['operationID'])==j for j in prior_jobs)
        report['ownerMediaPreserved']=owner_media=={p.name:generation(p.stat()) for p in (Path.home()/'images').iterdir()}
        require(all(report[k] for k in ('existingGuestsPreserved','existingJobsPreserved','ownerMediaPreserved')),'preservation failed')
    except BaseException as error:report.update(status='failed',preservationError=repr(error))
    r.save('report.json',report);print(json.dumps(report,sort_keys=True));os.close(fd)
raise SystemExit(report['status']!='passed')
