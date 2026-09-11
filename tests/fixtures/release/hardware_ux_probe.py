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
report={'status':'failed','scope':'two tiny OVA disks; full TUI source/prepare/approve/create/export/mkdir; native powered-off definition; no boot claim'}
before=inventory(r.cli('vm','list'),a.connection)
prior_jobs=r.cli('operation','list')
owner_media={p.name:generation(p.stat()) for p in (Path.home()/'images').iterdir()}
t=None
try:
    # A reproducible archive with VirtualBox-compatible units and explicit SATA
    # attachments. No supplied owner media is copied or modified.
    name='virmill-hardware-'+hashlib.sha256(str(root).encode()).hexdigest()[:12]
    require(all(vm['name']!=name for vm in before.values()),'fixture guest name already exists')
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
    def picker(screen): return re.search(r'Choose a (?:source|file|folder)\b',screen) is not None
    def browser_path(path):
        wait('Browser location',lambda screen:picker(screen) and 'Path:' in screen,b'\x0c')
        t.send(b'\x15')
        while t.read(.05) and not t.screen.complete():pass
        wait('Fixture location entered',lambda screen:str(path) in screen,str(path).encode())
        return wait('Open fixture location',lambda screen:'Path:' not in screen,b'\r')
    def reviewed_plan(label):
        for page in range(12):
            if re.search(r'Plan ID:\s*([0-9a-f-]{36})',t.screen.text()):break
            old=t.screen.text();wait(label+' review page '+str(page),lambda screen:screen!=old,b'\x1b[6~')
        matched=re.search(r'Plan ID:\s*([0-9a-f-]{36})',t.screen.text())
        require(matched is not None,'plan identity absent')
        plan=r.cli('plan','show',matched.group(1));r.save(label+'-plan.json',plan)
        return plan
    def apply_in_tui(plan,label):
        # Acknowledge only the plan just displayed and read back. CLI calls below
        # observe the accepted job; all plan approval and Apply input uses TUI.
        wait(label+' confirmation',lambda screen:'Confirm reviewed changes' in screen,b'\r')
        require(plan['planID'] in t.screen.text(),'confirmation changed plan identity')
        for ack in plan['acknowledgements']:
            require(re.search(r'>\s*\[ \]',t.screen.text()),'unchecked focused acknowledgement required')
            wait(label+' acknowledge '+ack,lambda screen:re.search(r'>\s*\[x\]',screen) is not None,b' ')
            old=t.screen.text()
            wait(label+' next acknowledgement',lambda screen:screen!=old,b'\t')
        focus('Apply reviewed plan')
        wait(label+' accepted',lambda screen:'Operation accepted.' in screen or 'Job /' in screen or 'VM options' in screen,b'\r')
        end=time.monotonic()+90
        job=None
        while time.monotonic()<end:
            found=[j for j in r.cli('operation','list') if j['planID']==plan['planID']]
            require(len(found)<=1,'one reviewed plan unexpectedly accepted more than once')
            if found:
                job=found[0]
                if job['state'] in ('succeeded','failed','partial','canceled','recovery-required'):break
            t.read(.3)
        require(job is not None,'accepted job not found')
        r.save(label+'-job.json',job)
        require(job['state']=='succeeded',label+' did not succeed: '+json.dumps(job))
        return job
    wait('Unified source browser',picker,b'i')
    browser_path(root)
    wait('Source filename filter',lambda screen:'Enter done' in screen,b'/')
    wait('Tiny source listed',lambda screen:source.name in screen,source.name.encode())
    wait('Source filter complete',lambda screen:'Enter open/select' in screen,b'\r')
    wait('Appliance summary',lambda screen:'Review appliance' in screen and 'CPU cores' in screen,b'\r')
    require('2048' in t.screen.text(),'detected RAM absent from summary')
    edit('VM name',name);edit('CPU cores','3');edit('Memory (MiB)','1024')
    activate('Continue',lambda screen:'New folder name' in screen)
    activate('Save in',picker);browser_path(root)
    if picker(t.screen.text()):
        wait('Destination parent selected',lambda screen:'New folder name' in screen,b'\x13')
    edit('New folder name','prepared')
    activate('Continue',lambda screen:'Preview image preparation' in screen)
    activate('Preview image preparation',lambda screen:'Nothing has been applied' in screen)
    preparation=reviewed_plan('preparation')
    require(preparation['operation']=='import.prepare','different preparation operation')
    require(preparation['review']['destination']==str(root/'prepared'),'preparation destination changed')
    prepared=apply_in_tui(preparation,'prepare');report['preparationOperationID']=prepared['operationID']
    wait('Preparation opens VM setup automatically',lambda screen:'VM options' in screen and 'Memory (MiB)' in screen)
    require('1024' in t.screen.text(),'edited summary RAM did not carry into VM setup')
    edit('VM name',name);edit('CPU cores','3');edit('Memory (MiB)','1024')
    focus('Storage pool')
    for i in range(12):
        if re.search(r'Storage pool:.*< virmill-test >',t.screen.text()):break
        old=t.screen.text();wait('Choose test storage '+str(i),lambda s:s!=old,b'\x1b[C')
    require(re.search(r'Storage pool:.*< virmill-test >',t.screen.text()),'test pool not selected')
    focus('Firmware')
    for i in range(len(choices['firmware'])+1):
        if ('< '+firmware['label']+' >') in t.screen.text():break
        old=t.screen.text();wait('Choose fixture firmware '+str(i),lambda s:s!=old,b'\x1b[C')
    require(('< '+firmware['label']+' >') in t.screen.text(),'explicit firmware choice failed')
    activate('Advanced hardware',lambda s:'Advanced hardware' in s and 'Guest agent channel' in s)
    activate('Done',lambda s:'VM options' in s)
    activate('Continue',lambda s:'Disks and boot' in s)
    require(re.search(r'Controller bus:.*< SATA >',t.screen.text()),'detected SATA bus missing')
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
    activate('Preview VM creation',lambda s:'Nothing has been applied' in s)
    creation=reviewed_plan('creation')
    wait('Back retains hardware options',lambda s:'Network adapters' in s,b'\x1b')
    activate('Preview VM creation',lambda s:'Nothing has been applied' in s)
    creation=reviewed_plan('creation-after-back')
    job=apply_in_tui(creation,'create');report['creationOperationID']=job['operationID']
    wait('Creation job completes without refresh',lambda screen:'Job / Completed' in screen and job['operationID'] in screen)
    receipt=r.cli('vm','creation','result',job['operationID']);r.save('creation-result.json',receipt)
    require(receipt['complete'] is True,'creation receipt incomplete');vmid=receipt['receipt']['vmID'];vm=r.cli('vm','show',vmid);r.save('created-vm.json',vm)
    import xml.etree.ElementTree as E
    xml=E.fromstring(vm['persistentXML'])
    require(xml.findtext('vcpu')=='3' and xml.findtext('memory')=='1048576','native CPU/RAM differ')
    require(len(xml.findall("devices/disk[@device='disk']"))==2 and len(xml.findall('devices/interface'))==0,'disk/NIC definition differs')
    require(vm['state'] in ('stopped','shut off'),'fixture guest unexpectedly running')
    # The only edited VM is the UUID returned by this probe's creation job.
    original_order=[d.find('target').get('dev') for d in sorted(xml.findall("devices/disk[@device='disk']"),key=lambda d:int(d.find('boot').get('order')))]
    require(len(original_order)==2,'two observed boot disks required')
    wait('VM list after creation',lambda screen:'NAME' in screen,b'2')
    wait('Find only new fixture VM',lambda screen:'Search:' in screen and 'Enter Keep filter' in screen,b'/')
    wait('New fixture UUID filter',lambda screen:vmid in screen,vmid.encode())
    wait('Finish new VM filter',lambda screen:name[:16] in screen and '1 of 1 selected' in screen and 'Enter Keep filter' not in screen,b'\r')
    wait('New VM details',lambda screen:name in screen and vmid in screen and 'Boot / installer' in screen,b'\r')
    wait('Detail buttons',lambda screen:'>[' in screen,b'\t')
    for i in range(12):
        if '>[ Boot / installer ]' in t.screen.text():break
        old=t.screen.text();wait('Choose Boot / installer '+str(i),lambda screen:screen!=old,b'\x1b[C')
    require('>[ Boot / installer ]' in t.screen.text(),'boot form action missing')
    wait('Observed boot devices',lambda screen:'Boot order and installer media' in screen and name in screen,b'\r')
    first=original_order[0]
    for i in range(6):
        if re.search(r'>\s*\[1\] disk '+re.escape(first)+r'\b',t.screen.text()):break
        old=t.screen.text();wait('Select first boot disk '+str(i),lambda screen:screen!=old,b'\t')
    require(re.search(r'>\s*\[1\] disk '+re.escape(first)+r'\b',t.screen.text()),'first observed boot disk not selected')
    wait('Move boot disk later',lambda screen:re.search(r'>\s*\[2\] disk '+re.escape(first)+r'\b',screen) is not None,b'\x1b[C')
    activate('Preview changes',lambda screen:'Nothing has been applied' in screen)
    boot=reviewed_plan('boot')
    wait('Back preserves requested boot order',lambda screen:'Boot order and installer media' in screen and '[2] disk '+first in screen,b'\x1b')
    activate('Preview changes',lambda screen:'Nothing has been applied' in screen)
    boot=reviewed_plan('boot-after-back')
    boot_job=apply_in_tui(boot,'boot');report['bootOperationID']=boot_job['operationID']
    wait('Boot edit completion refreshes automatically',lambda screen:'Job / Completed' in screen and boot_job['operationID'] in screen)
    edited=r.cli('vm','show',vmid);r.save('boot-edited-vm.json',edited)
    edited_xml=E.fromstring(edited['persistentXML'])
    edited_order=[d.find('target').get('dev') for d in sorted(edited_xml.findall("devices/disk[@device='disk']"),key=lambda d:int(d.find('boot').get('order')))]
    require(edited_order==list(reversed(original_order)),'native boot order does not match TUI edit')
    require(edited['state'] in ('stopped','shut off'),'boot editor started the VM')
    # Remove only order nodes when comparing; all other persistent settings,
    # disk paths and backend-generated metadata must remain identical.
    for doc in (xml,edited_xml):
        for disk in doc.findall('devices/disk'):
            boot_node=disk.find('boot')
            if boot_node is not None:disk.remove(boot_node)
    def semantic(node):return (node.tag,tuple(sorted(node.attrib.items())),(node.text or '').strip(),tuple(semantic(child) for child in node))
    require(semantic(xml)==semantic(edited_xml),'boot edit changed unrelated persistent XML')
    report['bootOrderBefore']=original_order;report['bootOrderAfter']=edited_order
    t.close();t=None
    require(generation(source.stat())==source_before,'fixture source changed during import')
    report.update(status='passed',vmID=vmid,vcpus=3,memoryMiB=1024,disks=2,guestStarted=False,allMutationApprovalThroughTUI=True,automaticJobCompletionObserved=True,sourceFixturePreserved=generation(source.stat())==source_before)
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
