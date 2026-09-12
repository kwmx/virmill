#!/usr/bin/env python3
"""Owner-authorized disposable session test: shared refusal, CLI and TUI deletion.

Requires empty qemu:///session inventory and a fresh private stage. Generates
three small disks and two never-booted BIOS guests. Deletes only those exact
generated disks through reviewed Virmill plans. Existing system guests, source
media and prior jobs are preserved. Failure retains all remaining fixture state;
never rerun or clean up an uncertain deletion automatically.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import stat
import subprocess
import time
import unittest
import uuid
import xml.etree.ElementTree as ET
from tui_workspace_probe import Runner, Terminal, require, media_listing, strict_json
from guest_agent_edit_probe import select_machine

URI = 'qemu:///session'
ACKS = {'host-mutation', 'remove-vm-definition', 'data-loss-delete-disks', 'exclusive-lifecycle-writer', 'exclusive-storage-writer'}
NS = 'urn:virmill:disk-removal-fixture:v1'

def sha(path):
    with Path(path).open('rb') as f: return hashlib.file_digest(f, 'sha256').hexdigest()

def declaration(identity, machine, paths):
    require(str(uuid.UUID(identity)) == identity and re.fullmatch(r'pc-i440fx-[0-9]+\.[0-9]+', machine), 'exact fixture identity/machine required')
    require(1 <= len(paths) <= 2 and all(p.is_absolute() for p in paths), 'explicit generated disk paths required')
    root = ET.Element('domain', type='kvm')
    ET.SubElement(root, 'name').text = 'virmill-delete-' + identity[:8]
    ET.SubElement(root, 'uuid').text = identity
    ET.SubElement(ET.SubElement(root, 'metadata'), '{'+NS+'}fixture', id=identity, neverBoot='true')
    ET.SubElement(root, 'memory', unit='MiB').text = '128'
    ET.SubElement(root, 'vcpu').text = '1'
    ET.SubElement(ET.SubElement(root, 'os'), 'type', arch='x86_64', machine=machine).text = 'hvm'
    devices = ET.SubElement(root, 'devices')
    ET.SubElement(devices, 'controller', type='pci', index='0', model='pci-root')
    for target, path in zip(('vda', 'vdb'), paths):
        disk = ET.SubElement(devices, 'disk', type='file', device='disk')
        ET.SubElement(disk, 'driver', name='qemu', type='qcow2')
        ET.SubElement(disk, 'source', file=str(path))
        ET.SubElement(disk, 'target', dev=target, bus='virtio')
    return ET.tostring(root, encoding='unicode')

def check_plan(plan, vm, paths):
    require(plan['operation'] == 'vm.remove-disks-v1' and plan['connectionID'] == URI and plan['actorUID'] == 1000, 'wrong deletion operation')
    require(set(plan['acknowledgements']) == ACKS and len(plan['acknowledgements']) == len(ACKS), 'wrong deletion authority')
    review = plan['review']
    require(review['resource'] == vm['key'] and review['vmName'] == vm['name'] and review['diskDeletion'] is True
            and review['backupsDeleted'] is False and review['configurationRemoved'] is True and review['backupCreated'] is False,
            'wrong removal scope')
    require(review['deleteDisks'] == list(('vda', 'vdb')[:len(paths)])
            and [d['path'] for d in review['disks']] == [str(p) for p in paths]
            and review['retainedSources'] == [], 'deletion extends beyond selected fixture disks')
    require(all(d['generation'].startswith('linux-statx-v1:') and re.fullmatch('[0-9a-f]{64}', d['fingerprint'])
                for d in review['disks']), 'missing disk generations')

def normalize(plan):
    return {k:v for k,v in plan.items() if k not in ('planID', 'planDigest', 'createdAt', 'expiresAt')}

def tui_delete(runner, vm, paths, reference_plan):
    terminal = Terminal(runner, 'delete-disks-80x24', 80, 24)
    name, identity = vm['name'], vm['key']['resourceUUID']
    try:
        def wait(label, predicate, key=None):
            return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
        wait('Overview', lambda s: 'Virtual machines' in s)
        wait('VM inventory', lambda s: 'NAME' in s and 'STATE' in s, b'2')
        wait('Filter', lambda s: 'Enter Keep filter' in s, b'/')
        wait('Exact generated VM', lambda s: name in s and '1 of 1 selected' in s, name.encode())
        wait('Keep filter', lambda s: 'Enter Keep filter' not in s, b'\r')
        wait('Details', lambda s: 'VM details' in s and identity in s, b'\r')
        def open_form():
            wait('More tasks', lambda s: 'VMs / More tasks' in s, b'a')
            wait('Find task', lambda s: 'Enter Keep matches' in s, b'/')
            wait('Remove VM action', lambda s: 'Remove VM' in s and 'No matching tasks' not in s, b'Remove VM')
            wait('Keep task matches', lambda s: 'Enter Keep matches' not in s, b'\r')
            wait('Disk removal choices', lambda s: 'Type VM name:' in s and name in s and '0 of 2 disks' in s, b'\r')
        open_form()
        wait('Cancel keeps VM', lambda s: 'VM details' in s and identity in s, b'\x1b')
        require(runner.cli('vm', 'show', identity) == vm, 'cancel changed VM')
        open_form()
        wait('Exact name', lambda s: any('Type VM name:' in l and name in l for l in s.splitlines()), name.encode())
        wait('First disk focused', lambda s: '> [ ] Keep vda' in s, b'\t')
        wait('Select first disk', lambda s: '> [x] DELETE vda' in s and '1 of 2 disks' in s, b' ')
        wait('Second disk focused', lambda s: '> [ ] Keep vdb' in s, b'\t')
        wait('Select second disk', lambda s: '> [x] DELETE vdb' in s and '2 of 2 disks' in s, b' ')
        wait('Preview focus', lambda s: '> [ Preview ]' in s, b'\t')
        wait('Separate review', lambda s: 'Nothing has been applied' in s, b'\r')
        match = None
        for i in range(40):
            match = re.search(r'Plan ID:\s*([0-9a-f-]{36})', terminal.screen.text())
            if match: break
            old = terminal.screen.text()
            wait('Read plan '+str(i), lambda s: s != old, b'\x1b[6~')
        require(match is not None, 'plan ID unavailable')
        plan = runner.cli('plan', 'show', match.group(1)); check_plan(plan, vm, paths)
        require(normalize(plan) == normalize(reference_plan), 'CLI and TUI removal plans differ')
        runner.save('tui-plan.json', plan)
        wait('Explicit confirmation', lambda s: 'Confirm reviewed changes' in s, b'\r')
        for ack in plan['acknowledgements']:
            require(ack in terminal.screen.text(), 'acknowledgement not visible: '+ack)
            old = terminal.screen.text()
            wait('Acknowledge '+ack, lambda s: s != old, b'\r')
        wait('Apply accepted', lambda s: 'Operation ID:' in s and ('Job /' in s or 'Status:' in s), b'\r')
        match = re.search(r'Operation ID:\s*([0-9a-f-]{36})', terminal.screen.text())
        require(match is not None, 'accepted operation identity unavailable')
        terminal.send(b'\x03')
        deadline=time.monotonic()+5
        while terminal.process.poll() is None and time.monotonic()<deadline:terminal.read(.05)
        require(terminal.process.poll()==0,'TUI did not exit normally')
        return match.group(1)
    finally: terminal.close()

def execute(stage):
    require(socket.gethostname() in ('virmill-test','virmill-test.home') and os.getuid()==os.geteuid()==1000,'wrong authorized host/actor')
    stage=stage.absolute();require(stage.resolve()==stage and stage.parent==Path.home()/'virmill-tests'
        and stage.stat().st_uid==1000 and stat.S_IMODE(stage.stat().st_mode)==0o700,'private stage required')
    os.umask(0o077)
    expected=strict_json((stage/'binaries.json').read_bytes())
    require(all(sha('/usr/bin/'+n)==expected[n] for n in ('virmill','virmilld')),'installed beta differs')
    out=stage/'disk-removal-cycle';out.mkdir(mode=0o700)
    fd=os.open('/usr/bin/virmill',os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
    r=Runner(argparse.Namespace(binary='/usr/bin/virmill',connection=URI),out,fd)
    state=out/'state';state.mkdir(mode=0o700);r.env['XDG_STATE_HOME']=str(state)
    report={'status':'failed','scope':'generated never-booted BIOS session guests only; selected disk deletion via CLI and TUI',
        'acceptanceSupport':['CORE-02','STO-03','UX-01','UX-02'],'binaries':expected,'fixtureSHA256':sha(__file__),'jobs':[],'cleanupPerformed':False}
    commands=[];before=None
    def run(argv):
        p=subprocess.run(argv,capture_output=True,text=True,timeout=90)
        commands.append({'argv':argv,'returncode':p.returncode,'stdout':p.stdout,'stderr':p.stderr});r.save('native-commands.json',commands)
        require(p.returncode==0,'native command failed; inspect native-commands.json');return p.stdout
    def virsh(*args):return run(['/usr/bin/virsh','-c',URI,*args])
    def system_observation():
        observed=strict_json(run(['/usr/bin/virmill','--connection','qemu:///system','--output','json','--non-interactive','vm','list']).encode())
        require(observed.get('error') is None,'system inventory unavailable')
        return {'domains':observed['data'],
                'media':media_listing(Path.home()/'images')}
    def finish(job_id):
        for i in range(100):
            job=r.cli('operation','show',job_id)
            if job['state'] in ('succeeded','failed','partial','canceled','recovery-required'):break
            time.sleep(.1)
        r.save('job-'+job_id+'.json',job);require(job['state']=='succeeded','deletion job did not succeed; retain remaining fixture files')
        report['jobs'].append(job_id)
    try:
        require(not virsh('list','--all','--uuid').strip() and not virsh('pool-list','--all','--uuid').strip()
                and not virsh('net-list','--all','--uuid').strip(),'session must be empty; do not disturb existing session resources')
        before=system_observation();old_jobs=r.cli('operation','list');r.save('prior-jobs.json',old_jobs)
        require(all(j['state'] in ('succeeded','failed','partial','canceled','recovery-required') for j in old_jobs),'active jobs prevent test')
        machine=select_machine(virsh('capabilities'));report['machine']=machine
        pool=out/'pool';pool.mkdir(mode=0o700);pool_id=str(uuid.uuid4());pool_name='virmill-delete-'+pool_id[:8]
        paths=[pool/(n+'.qcow2') for n in ('disk-a','disk-b','disk-c')];sources=[]
        for i,path in enumerate(paths):
            source=out/('source-'+str(i)+'.raw')
            with source.open('xb') as f:f.write(('Virmill generated deletion fixture '+str(i)+'\n').encode());f.truncate(8<<20)
            sources.append({'path':str(source),'sha256':sha(source)})
            run(['/usr/bin/qemu-img','convert','-f','raw','-O','qcow2',str(source),str(path)])
        report['sourceFiles']=sources;report['generatedDiskPaths']=[str(p) for p in paths];report['initialDiskHashes']={str(p):sha(p) for p in paths}
        poolxml=ET.Element('pool',type='dir');ET.SubElement(poolxml,'name').text=pool_name;ET.SubElement(poolxml,'uuid').text=pool_id;ET.SubElement(ET.SubElement(poolxml,'target'),'path').text=str(pool)
        px=out/'pool.xml';px.write_text(ET.tostring(poolxml,encoding='unicode'));virsh('pool-define',str(px));virsh('pool-start',pool_id);virsh('pool-refresh',pool_id)
        target,other=str(uuid.uuid4()),str(uuid.uuid4());report.update(targetUUID=target,otherUUID=other,poolUUID=pool_id)
        tx=out/'target.xml';tx.write_text(declaration(target,machine,paths[:2]));virsh('define',str(tx))
        ox=out/'other.xml';ox.write_text(declaration(other,machine,[paths[2],paths[0]]));virsh('define',str(ox))
        target_vm=r.cli('vm','show',target)
        keep=r.cli('vm','remove',target);require(keep['review']['diskDeletion'] is False and keep['review']['retainedSources']==[str(p) for p in paths[:2]],'default removal does not keep disks')
        result=subprocess.run(['/usr/bin/virmill','--connection',URI,'--output','json','--non-interactive','vm','remove',target,'--delete-disk','vda,vdb'],capture_output=True,text=True,timeout=90)
        rejected=strict_json(result.stdout.encode());r.save('shared-refusal.json',rejected)
        require(result.returncode!=0 and rejected['error']['code']=='RESOURCE_BUSY','shared disk was not refused')
        require(r.cli('vm','show',target)==target_vm and all(sha(p)==report['initialDiskHashes'][str(p)] for p in paths),'refusal changed fixture bytes/VM')
        # Restore only our exact never-booted secondary definition to its own disk.
        old=r.cli('vm','show',other);tree=ET.fromstring(old['persistentXML'])
        require(old['state']=='stopped' and not old['autostart'] and tree.find('metadata/{'+NS+'}fixture').get('id')==other
            and [n.get('file') for n in tree.findall('devices/disk/source')]==[str(paths[2]),str(paths[0])],'secondary fixture changed')
        ox2=out/'other-unshared.xml';ox2.write_text(declaration(other,machine,[paths[2]]));virsh('define',str(ox2))
        other_vm=r.cli('vm','show',other);plan=r.cli('vm','remove',other,'--delete-disk','vda');check_plan(plan,other_vm,[paths[2]])
        r.save('cli-plan.json',plan)
        job=r.cli('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--ack',','.join(sorted(ACKS)),
            '--idempotency-key',stage.name+'-cli','--detach');finish(job['operationID'])
        require(not paths[2].exists() and all(sha(p)==report['initialDiskHashes'][str(p)] for p in paths[:2]),'CLI deletion affected the wrong disks')
        plan=r.cli('vm','remove',target,'--delete-disk','vda,vdb');check_plan(plan,target_vm,paths[:2])
        finish(tui_delete(r,target_vm,paths[:2],plan))
        require(not virsh('list','--all','--uuid').strip() and all(not p.exists() for p in paths),'selected definitions/disks remain')
        require(not virsh('vol-list',pool_id,'--name').strip(),'native selected volume entries remain')
        require(all(sha(s['path'])==s['sha256'] for s in sources),'generated source media changed')
        after_jobs=r.cli('operation','list');prior={j['operationID']:j for j in old_jobs};after={j['operationID']:j for j in after_jobs}
        require(all(after.get(k)==v for k,v in prior.items()) and set(after)-set(prior)==set(report['jobs']),'unrelated jobs changed')
        report.update(status='passed',sharedReferenceRefused=True,defaultKeptDisks=True,cliDeletion=True,tuiDeletion=True,sourceMediaPreserved=True)
    except BaseException as error:
        report['failure']=str(error);raise
    finally:
        try:
            if before is not None:
                require(before==system_observation(),'existing system inventory or supplied media changed')
                report['systemInventoryAndMediaPreserved']=True
        except BaseException as error:report['status']='failed';report['preservationFailure']=str(error);raise
        finally:r.save('report.json',report);os.close(fd)

class Tests(unittest.TestCase):
    def test_definition_uses_only_explicit_disks(self):
        identity='11111111-2222-4333-8444-555555555555';paths=[Path('/fixture/a'),Path('/fixture/b')]
        tree=ET.fromstring(declaration(identity,'pc-i440fx-10.2',paths))
        self.assertEqual([n.get('file') for n in tree.findall('devices/disk/source')],list(map(str,paths)))
        self.assertFalse(tree.findall('devices/interface'));self.assertIsNone(tree.find('os/nvram'))
    def test_plan_comparison_excludes_only_ephemeral_fields(self):
        self.assertEqual(normalize({'planID':'one','review':{'deleteDisks':['vda']}}),normalize({'planID':'two','review':{'deleteDisks':['vda']}}))
        self.assertNotEqual(normalize({'review':{'deleteDisks':['vda']}}),normalize({'review':{'deleteDisks':['vdb']}}))

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--execute-disposable',action='store_true');p.add_argument('--root',type=Path);p.add_argument('--self-test',action='store_true');a=p.parse_args()
    if a.self_test:unittest.main(argv=[__file__])
    elif a.execute_disposable and a.root:execute(a.root)
    else:p.error('explicit disposable execution and private root required')
