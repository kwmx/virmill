#!/usr/bin/env python3
"""Owner-authorized disposable session test: shared refusal, CLI and TUI deletion.

Requires empty qemu:///session inventory and a fresh private stage. An explicit
--resume-from report permits reuse only of fully retained, verified pre-apply
fixtures in a separate fresh stage; it never retries a deletion. The bounded
--finish-tui-from mode continues the known confirmation-render failure only
after verifying the completed CLI job and that no TUI job was submitted. Generates
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
import tempfile
import unittest
import uuid
import xml.etree.ElementTree as ET
from tui_workspace_probe import Runner, Terminal, require, media_listing, strict_json
from guest_agent_edit_probe import select_machine


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


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

def tui_delete(runner, vm, paths, reference_plan, before_apply, accepted):
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
        before_apply()
        wait('Apply accepted', lambda s: 'Operation ID:' in s and ('Job /' in s or 'Status:' in s), b'\r')
        match = re.search(r'Operation ID:\s*([0-9a-f-]{36})', terminal.screen.text())
        require(match is not None, 'accepted operation identity unavailable')
        accepted(match.group(1))
        terminal.send(b'\x03')
        deadline=time.monotonic()+5
        while terminal.process.poll() is None and time.monotonic()<deadline:terminal.read(.05)
        require(terminal.process.poll()==0,'TUI did not exit normally')
        return match.group(1)
    finally: terminal.close()

def resume_report(path, current_jobs):
    """Accept only a fully retained, pre-apply fixture; never resume an effect."""
    path = path.absolute()
    require(path.resolve() == path and path.name == 'report.json', 'exact prior report path required')
    old = strict_json(path.read_bytes())
    require(old.get('status') == 'failed' and old.get('jobs') == []
            and old.get('cleanupPerformed') is False and old.get('systemInventoryAndMediaPreserved') is True,
            'prior test is not a preserved pre-apply failure')
    root = path.parent
    require(not any((root / n).exists() for n in ('cli-plan.json', 'tui-plan.json', 'other-unshared.xml')),
            'prior fixture progressed beyond the shared-refusal stage')
    require(not list(root.glob('job-*.json')), 'prior job output prevents resume')
    previous_jobs = strict_json((root / 'prior-jobs.json').read_bytes())
    require({j['operationID']: j for j in previous_jobs} == {j['operationID']: j for j in current_jobs},
            'job journal changed since the prior fixture; resume refused')
    if 'applyAttempted' not in old:
        refused = strict_json((root / 'shared-refusal.json').read_bytes())
        require(old.get('fixtureSHA256') == '276082fb05b51967edf2ad167e2fabea71d20eb7d0cd463c6f5ecf8a28dafcaf'
                and old.get('failure') == 'shared disk was not refused'
                and refused.get('error', {}).get('code') == 'UNSUPPORTED_CAPABILITY'
                and refused.get('error', {}).get('message') == 'unknown or encrypted volume target requires retention',
                'legacy report is not the known pre-apply compatibility refusal')
    else:
        require(old['applyAttempted'] is False, 'an apply was attempted; automatic resume forbidden')
    require(old.get('generatedDiskPaths') == [str(root / 'pool' / (n + '.qcow2')) for n in ('disk-a', 'disk-b', 'disk-c')],
            'prior disks are outside the exact fixture')
    require(set(old.get('initialDiskHashes', {})) == set(old['generatedDiskPaths']), 'incomplete original disk hashes')
    require([s['path'] for s in old.get('sourceFiles', [])] == [str(root / ('source-' + str(i) + '.raw')) for i in range(3)],
            'prior source files are outside the exact fixture')
    for item in list(old['initialDiskHashes'].items()) + [(s['path'], s['sha256']) for s in old['sourceFiles']]:
        file, digest = Path(item[0]), item[1]
        require(file.resolve() == file and stat.S_ISREG(file.lstat().st_mode)
                and file.lstat().st_nlink == 1 and re.fullmatch('[0-9a-f]{64}', digest)
                and sha(file) == digest, 'retained fixture file changed: ' + str(file))
    identities = [old.get(k) for k in ('targetUUID', 'otherUUID', 'poolUUID')]
    require(len(set(identities)) == 3 and all(str(uuid.UUID(i)) == i for i in identities), 'invalid prior identities')
    require(re.fullmatch(r'pc-i440fx-[0-9]+\.[0-9]+', old.get('machine', '')), 'invalid prior machine')
    return old


def finish_tui_report(path, current_jobs):
    """Continue the known confirmation-render failure after a proven CLI success."""
    path = path.absolute()
    require(path.resolve() == path and path.name == 'report.json', 'exact prior report path required')
    old = strict_json(path.read_bytes()); root = path.parent
    require(old.get('status') == 'failed' and old.get('applyAttempted') is True
            and old.get('cleanupPerformed') is False and old.get('systemInventoryAndMediaPreserved') is True
            and old.get('fixtureSHA256') == '1d0c7281c8382cfafb71dfb3ddd3fc7875d4019f93a29de4ed93280c874890b0'
            and old.get('failure') == 'acknowledgement not visible: data-loss-delete-disks'
            and len(old.get('jobs', [])) == 1, 'not the known pre-TUI-apply confirmation failure')
    initial = Path(old['resumedFrom']['path'])
    require(initial.resolve() == initial and initial.name == 'report.json'
            and initial.parent.name == 'disk-removal-cycle' and initial.parent.parent.parent == root.parent.parent
            and sha(initial) == old['resumedFrom']['sha256'], 'original fixture report changed')
    original = strict_json(initial.read_bytes())
    for key in ('machine', 'sourceFiles', 'generatedDiskPaths', 'initialDiskHashes', 'targetUUID', 'otherUUID', 'poolUUID'):
        require(old[key] == original[key], 'continued fixture differs from original: ' + key)
    paths = list(map(Path, old['generatedDiskPaths']))
    require(paths == [initial.parent / 'pool' / (n + '.qcow2') for n in ('disk-a', 'disk-b', 'disk-c')],
            'unexpected fixture disk paths')
    require(not os.path.lexists(paths[2]), 'completed CLI disk reappeared')
    for file, digest in [(p, old['initialDiskHashes'][str(p)]) for p in paths[:2]] + [(Path(s['path']), s['sha256']) for s in old['sourceFiles']]:
        require(file.resolve() == file and stat.S_ISREG(file.lstat().st_mode) and file.lstat().st_nlink == 1
                and sha(file) == digest, 'retained fixture bytes changed: ' + str(file))
    cli_id = old['jobs'][0]
    saved_job = strict_json((root / ('job-' + cli_id + '.json')).read_bytes())
    cli_plan = strict_json((root / 'cli-plan.json').read_bytes())
    other = {'key': {'providerID': 'libvirt', 'connectionID': URI, 'kind': 'vm', 'resourceUUID': old['otherUUID']},
             'name': 'virmill-delete-' + old['otherUUID'][:8]}
    check_plan(cli_plan, other, [paths[2]])
    require(saved_job.get('operationID') == cli_id and saved_job.get('state') == 'succeeded'
            and saved_job.get('planID') == cli_plan['planID'], 'CLI job did not complete the exact selected removal')
    prior = strict_json((root / 'prior-jobs.json').read_bytes())
    expected = {j['operationID']: j for j in prior}
    require(cli_id not in expected, 'CLI operation predates the fixture')
    expected[cli_id] = saved_job
    require(expected == {j['operationID']: j for j in current_jobs}, 'additional or changed jobs prevent TUI continuation')
    require(strict_json((root / 'shared-refusal.json').read_bytes()).get('error', {}).get('code') == 'RESOURCE_BUSY',
            'shared-reference refusal evidence missing')
    require((root / 'tui-plan.json').is_file() and (root / 'other-unshared.xml').is_file(), 'prior stage evidence incomplete')
    return old, saved_job, strict_json((root / 'tui-plan.json').read_bytes())


def execute(stage, resume_from=None, finish_tui_from=None):
    require(authorized_test_host() and os.getuid()==os.geteuid()==1000,'wrong authorized host/actor')
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
        'acceptanceSupport':['CORE-02','STO-03','UX-01','UX-02'],'binaries':expected,'fixtureSHA256':sha(__file__),'jobs':[],'applyAttempted':False,'cleanupPerformed':False}
    commands=[];before=None
    def run(argv):
        p=subprocess.run(argv,capture_output=True,text=True,timeout=90,env=dict(os.environ, LC_ALL="C"))
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
    def before_apply():
        report['applyAttempted'] = True
        r.save('report.json', report)
    def accepted(job_id):
        require(str(uuid.UUID(job_id)) == job_id and job_id not in report['jobs'], 'invalid or duplicated accepted operation')
        report['jobs'].append(job_id)
        r.save('report.json', report)
    try:
        before=system_observation();old_jobs=r.cli('operation','list');r.save('prior-jobs.json',old_jobs)
        require(all(j['state'] in ('succeeded','failed','partial','canceled','recovery-required') for j in old_jobs),'active jobs prevent test')
        previous_tui_plan = None
        if resume_from is not None or finish_tui_from is not None:
            resume_from = (resume_from or finish_tui_from).absolute()
            require(resume_from.parent.parent.parent == Path.home()/'virmill-tests'
                    and resume_from.parent.name == 'disk-removal-cycle'
                    and resume_from.parent != out, 'resume must name a separate prior test stage')
            if finish_tui_from is not None:
                old, cli_job, previous_tui_plan = finish_tui_report(resume_from, old_jobs)
                require(r.cli('operation', 'show', cli_job['operationID']) == cli_job, 'completed CLI job changed')
                report['inheritedCLIJob'] = cli_job
            else:
                old = resume_report(resume_from, old_jobs)
            for key in ('machine', 'sourceFiles', 'generatedDiskPaths', 'initialDiskHashes', 'targetUUID', 'otherUUID', 'poolUUID'):
                report[key] = old[key]
            report['resumedFrom'] = {'path': str(resume_from), 'sha256': sha(resume_from)}
            machine, sources = old['machine'], old['sourceFiles']
            target, other, pool_id = old['targetUUID'], old['otherUUID'], old['poolUUID']
            pool_name = 'virmill-delete-' + pool_id[:8]
            paths = list(map(Path, old['generatedDiskPaths'])); pool = paths[0].parent
            require(set(virsh('list','--all','--uuid').split()) == ({target} if finish_tui_from else {target, other})
                    and virsh('pool-list','--all','--uuid').split() == [pool_id]
                    and not virsh('net-list','--all','--uuid').strip(), 'session inventory differs from exact retained fixtures')
            px = ET.fromstring(virsh('pool-dumpxml', pool_id))
            info = dict(line.split(':', 1) for line in virsh('pool-info', pool_id).splitlines() if ':' in line)
            require(px.get('type') == 'dir' and px.findtext('uuid') == pool_id and px.findtext('name') == pool_name
                    and px.findtext('target/path') == str(pool) and info.get('State','').strip() == 'running'
                    and info.get('Autostart','').strip() == 'no', 'retained pool changed')
            retained_paths = paths[:2] if finish_tui_from else paths
            volumes = virsh('vol-list', pool_id).strip().splitlines()
            require(len(volumes) == len(retained_paths) + 2 and volumes[0].split() == ['Name', 'Path']
                    and {tuple(line.split()) for line in volumes[2:]} == {(p.name, str(p)) for p in retained_paths}, 'retained volume inventory changed')
            retained_domains = [(target, paths[:2])] if finish_tui_from else [(target, paths[:2]), (other, [paths[2], paths[0]])]
            for identity, disks in retained_domains:
                vm = r.cli('vm', 'show', identity); tree = ET.fromstring(vm['persistentXML'])
                marker = tree.find('metadata/{'+NS+'}fixture')
                info = dict(line.split(':', 1) for line in virsh('dominfo', identity).splitlines() if ':' in line)
                require(vm['state'] == 'stopped' and not vm['autostart'] and vm['name'] == 'virmill-delete-'+identity[:8]
                        and tree.findtext('uuid') == identity and marker is not None
                        and marker.attrib == {'id': identity, 'neverBoot': 'true'}
                        and tree.find('os/type').get('machine') == machine
                        and [n.get('file') for n in tree.findall('devices/disk/source')] == list(map(str, disks))
                        and [n.get('dev') for n in tree.findall('devices/disk/target')] == ['vda', 'vdb']
                        and info.get('Persistent','').strip() == 'yes' and info.get('Managed save','').strip() == 'no'
                        and not virsh('snapshot-list', identity, '--name').strip()
                        and not virsh('checkpoint-list', identity, '--name').strip(), 'retained never-booted definition changed')
            r.save('report.json', report)
        else:
            require(not virsh('list','--all','--uuid').strip() and not virsh('pool-list','--all','--uuid').strip()
                    and not virsh('net-list','--all','--uuid').strip(),'session must be empty; do not disturb existing session resources')
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
        if finish_tui_from is None:
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
            before_apply()
            job=r.cli('plan','apply',plan['planID'],'--digest',plan['planDigest'],'--ack',','.join(sorted(ACKS)),
                '--idempotency-key',stage.name+'-cli','--detach');accepted(job['operationID']);finish(job['operationID'])
            require(not paths[2].exists() and all(sha(p)==report['initialDiskHashes'][str(p)] for p in paths[:2]),'CLI deletion affected the wrong disks')
        plan=r.cli('vm','remove',target,'--delete-disk','vda,vdb');check_plan(plan,target_vm,paths[:2])
        if previous_tui_plan is not None:
            check_plan(previous_tui_plan, target_vm, paths[:2])
            require(normalize(plan) == normalize(previous_tui_plan), 'retained TUI plan differs from fresh reviewed plan')
        finish(tui_delete(r,target_vm,paths[:2],plan,before_apply,accepted))
        require(not virsh('list','--all','--uuid').strip() and all(not p.exists() for p in paths),'selected definitions/disks remain')
        volume_lines=virsh('vol-list',pool_id).strip().splitlines()
        require(len(volume_lines)==2 and volume_lines[0].split()==['Name','Path']
                and set(volume_lines[1].strip())=={'-'},'native selected volume entries remain')
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
        finally:r.save('report.json',report);print(json.dumps(report,sort_keys=True));os.close(fd)

class Tests(unittest.TestCase):
    def retained_report(self, root):
        pool = root / 'pool'; pool.mkdir()
        paths = [pool / (name + '.qcow2') for name in ('disk-a', 'disk-b', 'disk-c')]
        sources = [root / ('source-' + str(i) + '.raw') for i in range(3)]
        for i, path in enumerate(paths + sources): path.write_bytes(('fixture ' + str(i)).encode())
        old = {'status': 'failed', 'jobs': [], 'cleanupPerformed': False,
               'systemInventoryAndMediaPreserved': True, 'applyAttempted': False,
               'generatedDiskPaths': list(map(str, paths)), 'initialDiskHashes': {str(p): sha(p) for p in paths},
               'sourceFiles': [{'path': str(p), 'sha256': sha(p)} for p in sources],
               'machine': 'pc-i440fx-10.2', 'targetUUID': str(uuid.uuid4()),
               'otherUUID': str(uuid.uuid4()), 'poolUUID': str(uuid.uuid4())}
        (root / 'prior-jobs.json').write_text('[]')
        (root / 'report.json').write_text(json.dumps(old))
        return old

    def test_resume_only_before_any_apply_and_preserves_original_files(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); old = self.retained_report(root)
            self.assertEqual(resume_report(root / 'report.json', []), old)
            self.assertEqual(sha(old['sourceFiles'][0]['path']), old['sourceFiles'][0]['sha256'])
            for field, value in (('applyAttempted', True), ('jobs', [str(uuid.uuid4())]), ('cleanupPerformed', True)):
                changed = dict(old); changed[field] = value
                (root / 'report.json').write_text(json.dumps(changed))
                with self.assertRaises(RuntimeError): resume_report(root / 'report.json', [])
            (root / 'report.json').write_text(json.dumps(old))
            with self.assertRaises(RuntimeError): resume_report(root / 'report.json', [{'operationID': 'new'}])
            for filename in ('cli-plan.json', 'tui-plan.json', 'other-unshared.xml'):
                artifact = root / filename; artifact.write_text('{}')
                with self.assertRaises(RuntimeError): resume_report(root / 'report.json', [])
                artifact.unlink()
            Path(old['generatedDiskPaths'][0]).write_bytes(b'changed')
            with self.assertRaises(RuntimeError): resume_report(root / 'report.json', [])

    def test_legacy_resume_requires_exact_known_refusal(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp); old = self.retained_report(root); del old['applyAttempted']
            old.update(fixtureSHA256='276082fb05b51967edf2ad167e2fabea71d20eb7d0cd463c6f5ecf8a28dafcaf',
                       failure='shared disk was not refused')
            (root / 'report.json').write_text(json.dumps(old))
            (root / 'shared-refusal.json').write_text(json.dumps({'error': {'code': 'UNSUPPORTED_CAPABILITY',
                       'message': 'unknown or encrypted volume target requires retention'}}))
            self.assertEqual(resume_report(root / 'report.json', []), old)
            (root / 'shared-refusal.json').write_text(json.dumps({'error': {'code': 'RESOURCE_BUSY'}}))
            with self.assertRaises(RuntimeError): resume_report(root / 'report.json', [])

    def test_finish_tui_requires_exact_successful_cli_and_no_extra_jobs(self):
        with tempfile.TemporaryDirectory() as temp:
            original_root = Path(temp) / 'original' / 'disk-removal-cycle'; original_root.mkdir(parents=True)
            original = self.retained_report(original_root)
            root = Path(temp) / 'continued' / 'disk-removal-cycle'; root.mkdir(parents=True)
            cli_id = str(uuid.uuid4()); plan_id = str(uuid.uuid4())
            job = {'operationID': cli_id, 'planID': plan_id, 'state': 'succeeded'}
            old = dict(original)
            old.update(jobs=[cli_id], applyAttempted=True,
                       fixtureSHA256='1d0c7281c8382cfafb71dfb3ddd3fc7875d4019f93a29de4ed93280c874890b0',
                       failure='acknowledgement not visible: data-loss-delete-disks',
                       resumedFrom={'path': str(original_root / 'report.json'), 'sha256': sha(original_root / 'report.json')})
            disk_c = Path(old['generatedDiskPaths'][2]); disk_c.unlink()
            plan = {'operation': 'vm.remove-disks-v1', 'connectionID': URI, 'actorUID': 1000,
                    'acknowledgements': sorted(ACKS), 'planID': plan_id,
                    'review': {'resource': {'providerID': 'libvirt', 'connectionID': URI, 'kind': 'vm', 'resourceUUID': old['otherUUID']},
                               'vmName': 'virmill-delete-' + old['otherUUID'][:8], 'diskDeletion': True,
                               'backupsDeleted': False, 'configurationRemoved': True, 'backupCreated': False,
                               'deleteDisks': ['vda'], 'retainedSources': [],
                               'disks': [{'path': str(disk_c), 'generation': 'linux-statx-v1:test', 'fingerprint': 'a'*64}]}}
            for name, value in [('report.json', old), ('prior-jobs.json', []), ('job-'+cli_id+'.json', job),
                                ('cli-plan.json', plan), ('tui-plan.json', {}),
                                ('shared-refusal.json', {'error': {'code': 'RESOURCE_BUSY'}})]:
                (root / name).write_text(json.dumps(value))
            (root / 'other-unshared.xml').write_text('<domain/>')
            self.assertEqual(finish_tui_report(root / 'report.json', [job])[1], job)
            with self.assertRaises(RuntimeError): finish_tui_report(root / 'report.json', [job, {'operationID': 'extra'}])
            for wrong in ('failed', 'recovery-required'):
                changed = dict(job, state=wrong)
                (root / ('job-'+cli_id+'.json')).write_text(json.dumps(changed))
                with self.assertRaises(RuntimeError): finish_tui_report(root / 'report.json', [changed])
            (root / ('job-'+cli_id+'.json')).write_text(json.dumps(job))
            disk_c.write_bytes(b'replacement')
            with self.assertRaises(RuntimeError): finish_tui_report(root / 'report.json', [job])

    def test_definition_uses_only_explicit_disks(self):
        identity='11111111-2222-4333-8444-555555555555';paths=[Path('/fixture/a'),Path('/fixture/b')]
        tree=ET.fromstring(declaration(identity,'pc-i440fx-10.2',paths))
        self.assertEqual([n.get('file') for n in tree.findall('devices/disk/source')],list(map(str,paths)))
        self.assertFalse(tree.findall('devices/interface'));self.assertIsNone(tree.find('os/nvram'))
    def test_plan_comparison_excludes_only_ephemeral_fields(self):
        self.assertEqual(normalize({'planID':'one','review':{'deleteDisks':['vda']}}),normalize({'planID':'two','review':{'deleteDisks':['vda']}}))
        self.assertNotEqual(normalize({'review':{'deleteDisks':['vda']}}),normalize({'review':{'deleteDisks':['vdb']}}))

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--execute-disposable',action='store_true');p.add_argument('--root',type=Path);p.add_argument('--self-test',action='store_true');continuation=p.add_mutually_exclusive_group();continuation.add_argument('--resume-from',type=Path);continuation.add_argument('--finish-tui-from',type=Path);a=p.parse_args()
    if a.self_test:unittest.main(argv=[__file__])
    elif a.execute_disposable and a.root:execute(a.root,a.resume_from,a.finish_tui_from)
    else:p.error('explicit disposable execution and private root required')
