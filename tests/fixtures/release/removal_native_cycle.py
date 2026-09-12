#!/usr/bin/env python3
"""Remove one newly defined, never-booted BIOS fixture through a durable job.

Requires --execute-disposable --root PRIVATE_STAGE on the authorized test VM.
Creates a unique marked VM and an 8 MiB generated qcow2 disk; removes only that
definition after checking the exact public plan. No disks, existing definitions,
snapshots, networks, media or jobs are deleted. No cleanup or apply retry occurs,
including after failure: retain the fixture and inspect its report. This tests
definition removal and retained disk bytes, not guest boot or disk deletion.
Adjacent tui_workspace_probe.py and guest_agent_edit_probe.py are required.
--self-test runs only pure declaration/review validators.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import socket
import stat
import subprocess
import sys
import time
import unittest
import uuid
import xml.etree.ElementTree as ET

from tui_workspace_probe import Runner, Terminal, canonical_path, canonical_uuid, inventory, media_listing, require, strict_json
from guest_agent_edit_probe import select_machine

URI = 'qemu:///system'
NS = 'urn:virmill:removal-fixture:v1'
ACKS = {'host-mutation', 'remove-vm-definition', 'exclusive-lifecycle-writer'}
TERMINAL = {'succeeded', 'failed', 'canceled', 'partial', 'recovery-required'}


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def fixture_xml(identity, machine, disk):
    canonical_uuid(identity)
    require(re.fullmatch(r'pc-i440fx-[0-9]+\.[0-9]+', machine), 'unobserved machine profile')
    require(disk == Path('/var/lib/libvirt/images') / ('virmill-removal-' + identity) / 'disk.qcow2',
            'disk outside unique fixture directory')
    root = ET.Element('domain', {'type': 'kvm'})
    ET.SubElement(root, 'name').text = 'virmill-removal-' + identity[:8]
    ET.SubElement(root, 'uuid').text = identity
    metadata = ET.SubElement(root, 'metadata')
    ET.SubElement(metadata, '{' + NS + '}fixture', {'id': identity, 'version': '1', 'neverBoot': 'true'})
    ET.SubElement(root, 'memory', {'unit': 'MiB'}).text = '128'
    ET.SubElement(root, 'vcpu').text = '1'
    guest = ET.SubElement(root, 'os')
    ET.SubElement(guest, 'type', {'arch': 'x86_64', 'machine': machine}).text = 'hvm'
    devices = ET.SubElement(root, 'devices')
    ET.SubElement(devices, 'controller', {'type': 'pci', 'index': '0', 'model': 'pci-root'})
    device = ET.SubElement(devices, 'disk', {'type': 'file', 'device': 'disk'})
    ET.SubElement(device, 'driver', {'name': 'qemu', 'type': 'qcow2'})
    ET.SubElement(device, 'source', {'file': str(disk)})
    ET.SubElement(device, 'target', {'dev': 'vda', 'bus': 'virtio'})
    return ET.tostring(root, encoding='unicode')


def check_fixture(vm, identity, name, disk):
    require(vm['key'] == {'providerID': 'libvirt', 'connectionID': URI, 'kind': 'vm', 'resourceUUID': identity}
            and vm['name'] == name and vm['state'] == 'stopped' and vm['autostart'] is False
            and vm['hasManagedSave'] is False and vm['persistentXML'] and not vm.get('liveXML'),
            'fixture identity, stopped state or persistence differs')
    tree = ET.fromstring(vm['persistentXML'])
    marker = tree.find('metadata/{' + NS + '}fixture')
    require(tree.findtext('name') == name and tree.findtext('uuid') == identity and marker is not None
            and marker.attrib == {'id': identity, 'version': '1', 'neverBoot': 'true'}, 'fixture ownership differs')
    require(len(tree.findall('devices/disk')) == 1 and
            [source.get('file') for source in tree.findall('devices/disk/source')] == [str(disk)] and
            not tree.findall('devices/interface') and not tree.findall('devices/tpm') and
            not tree.findall('devices/hostdev') and not tree.findall('devices/filesystem') and
            tree.find('os/loader') is None and tree.find('os/nvram') is None, 'fixture acquired additional storage/device state')


def check_plan(plan, vm, disk):
    key = vm['key']; identity = key['resourceUUID']
    require(plan['operation'] == 'vm.remove-definition-v1' and plan['connectionID'] == URI and plan['actorUID'] == 1000
            and plan['resourceIDs'] == ['libvirt|' + URI + '|vm|' + identity], 'wrong removal target or operation')
    canonical_uuid(plan['planID'])
    require(re.fullmatch(r'[0-9a-f]{64}', plan['planDigest']) and len(plan['acknowledgements']) == len(ACKS)
            and set(plan['acknowledgements']) == ACKS, 'unexpected removal plan grants')
    review = plan['review']
    require(review['action'] == 'remove' and review['resource'] == key and review['vmName'] == vm['name']
            and review['definitionSHA256'] == hashlib.sha256(vm['persistentXML'].encode()).hexdigest()
            and review['retainedSources'] == [str(disk)] and review['diskDeletion'] is False
            and review['backupsDeleted'] is False and review['configurationRemoved'] is True
            and review['backupCreated'] is False and review['requiresStopped'] is True
            and review['automaticStop'] is False, 'review exceeds definition-only retained-disk scope')


def normalized_plan(plan):
    return {key: value for key, value in plan.items() if key not in ('planID', 'planDigest', 'createdAt', 'expiresAt')}


def check_resume_report(report):
    require(report.get('status') == 'failed' and report.get('jobs') == [] and
            report.get('applyAttempted', False) is False and report.get('cleanupPerformed') is False,
            'resume requires a failed fixture with no apply attempt or accepted jobs')
    identity = canonical_uuid(report.get('fixtureUUID'))
    disk = Path('/var/lib/libvirt/images') / ('virmill-removal-' + identity) / 'disk.qcow2'
    require(report.get('fixtureName') == 'virmill-removal-' + identity[:8] and report.get('retainedDisk') == str(disk),
            'retained fixture name/path does not match its identity')
    for field in ('initialDiskSHA256', 'sourceRawSHA256', 'sourceDefinitionSHA256'):
        require(isinstance(report.get(field), str) and re.fullmatch(r'[0-9a-f]{64}', report[field]), 'missing retained source hashes')
    require(report.get('observedRetainedDiskSHA256') == report['initialDiskSHA256'] and
            report.get('unrelatedFinalObservationMatches') is True, 'failed fixture did not preserve its disk and unrelated inventory')
    return identity, disk


def load_resume(path, new_stage):
    path = canonical_path(str(path.absolute()))
    previous = path.parent
    require(path.name == 'report.json' and previous.name == 'removal-cycle' and
            previous.parent.parent == Path.home() / 'virmill-tests' and previous.parent != new_stage,
            'resume must reference another private staged removal-cycle report')
    for directory in (previous, previous.parent):
        require(stat.S_ISDIR(directory.lstat().st_mode) and directory.stat().st_uid == 1000 and
                stat.S_IMODE(directory.stat().st_mode) == 0o700, 'prior stage is not private and owned')
    artifacts = {}
    for name, limit in [('report.json', 1 << 20), ('defined-fixture.json', 2 << 20), ('final-observation.json', 8 << 20),
                        ('fixture.xml', 2 << 20), ('generated-source.raw', 8 << 20)]:
        item = previous / name
        info = item.lstat()
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 1000 and 0 < info.st_size <= limit and
                stat.S_IMODE(info.st_mode) & 0o077 == 0, 'prior artifact is not bounded, private and owned')
        artifacts[name] = {'path': str(item), 'sha256': sha(item)}
    report = strict_json(path.read_bytes())
    identity, disk = check_resume_report(report)
    require(artifacts['fixture.xml']['sha256'] == report['sourceDefinitionSHA256'] and
            artifacts['generated-source.raw']['sha256'] == report['sourceRawSHA256'], 'retained source bytes changed')
    vm = strict_json((previous / 'defined-fixture.json').read_bytes())
    check_fixture(vm, identity, report['fixtureName'], disk)
    require(not (previous / 'accepted-job.json').exists() and not (previous / 'final-job.json').exists(),
            'job evidence exists despite a no-apply report')
    return report, vm, artifacts


def tui_preview(runner, vm, disk):
    identity, name = vm['key']['resourceUUID'], vm['name']
    terminal = Terminal(runner, 'removal-80x24', 80, 24)
    try:
        def wait(label, predicate, key=None):
            return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
        wait('Overview', lambda s: 'Virtual machines' in s)
        wait('VM table', lambda s: 'NAME' in s and 'STATE' in s, b'2')
        wait('Filter focus', lambda s: 'Enter Keep filter' in s, b'/')
        wait('Exact new fixture selected', lambda s: name in s and '1 of 1 selected' in s, name.encode())
        wait('Keep exact fixture selection', lambda s: 'Enter Keep filter' not in s, b'\r')
        wait('Exact fixture details', lambda s: 'VM details' in s and name in s and identity in s, b'\r')
        wait('More VM tasks', lambda s: 'VMs / More tasks' in s and name in s, b'a')
        wait('Task search focused', lambda s: 'Enter Keep matches' in s, b'/')
        wait('Removal action found', lambda s: 'Remove VM, keep disks' in s and 'No matching tasks' not in s, b'Remove VM')
        wait('Keep matching task', lambda s: 'Enter Keep matches' not in s and 'Remove VM, keep disks' in s, b'\r')
        def form(s):
            return 'Type VM name:' in s and name in s and identity in s and 'Disks and backups are kept' in s
        wait('Fresh stopped definition removal form', lambda s: form(s) and 'State: stopped' in s, b'\r')
        wait('Exact literal name entered', lambda s: form(s) and
             any('Type VM name:' in line and name in line for line in s.splitlines()), name.encode())
        wait('Separate Preview focus', lambda s: form(s) and re.search(r'>\s*\[ Preview \]', s) is not None, b'\r')
        wait('Removal plan review without apply', lambda s: 'Nothing has been applied' in s, b'\r')
        match = None
        for page in range(12):
            match = re.search(r'Plan ID:\s*([0-9a-f-]{36})', terminal.screen.text())
            if match: break
            old = terminal.screen.text()
            wait('Read removal plan identity ' + str(page), lambda s: s != old, b'\x1b[6~')
        require(match, 'TUI removal plan identity unavailable')
        plan = runner.cli('plan', 'show', match.group(1)); check_plan(plan, vm, disk)
        runner.save('tui-removal-plan.json', plan)
        wait('Back retains exact typed name', lambda s: form(s) and
             any('Type VM name:' in line and name in line for line in s.splitlines()), b'\x1b')
        wait('Cancel returns to the same VM details', lambda s: 'VM details' in s and identity in s and
             name in s and 'Type VM name:' not in s, b'\x1b')
        terminal.send(b'q'); deadline = time.monotonic() + 10
        while terminal.process.poll() is None and time.monotonic() < deadline: terminal.read(.05)
        require(terminal.process.poll() == 0, 'normal TUI exit failed')
        return plan
    finally:
        terminal.close()


def execute(stage, resume_path=None):
    require(socket.gethostname() in ('virmill-test', 'virmill-test.home') and os.getuid() == os.geteuid() == 1000,
            'wrong authorized disposable host/actor')
    stage = canonical_path(str(stage.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stat.S_ISDIR(stage.lstat().st_mode)
            and stage.stat().st_uid == 1000 and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private stage required')
    os.umask(0o077)
    manifest = stage / 'binaries.json'
    require(stat.S_ISREG(manifest.lstat().st_mode) and manifest.stat().st_uid == 1000
            and manifest.stat().st_size <= 65536, 'ordinary bounded binary manifest required')
    expected = strict_json(manifest.read_bytes())
    for name in ('virmill', 'virmilld'):
        require(re.fullmatch(r'[0-9a-f]{64}', expected[name]) and sha('/usr/bin/' + name) == expected[name],
                'installed binary mismatch')
    resume = load_resume(resume_path, stage) if resume_path is not None else None
    out = stage / 'removal-cycle'; out.mkdir(mode=0o700)
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(os.dup(fd), 'rb') as stream:
        require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected['virmill'], 'held binary mismatch')
    runner = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, fd)
    state = out / 'state'; state.mkdir(mode=0o700)
    runner.env.update(XDG_STATE_HOME=str(state), VIRMILL_ASCII='1')
    identity = resume[0]['fixtureUUID'] if resume else str(uuid.uuid4())
    name = 'virmill-removal-' + identity[:8]
    directory = Path('/var/lib/libvirt/images') / ('virmill-removal-' + identity)
    disk = directory / 'disk.qcow2'
    report = {'status': 'failed', 'acceptanceSupport': ['CORE-02', 'UX-01', 'UX-02'], 'fixtureUUID': identity,
              'fixtureName': name, 'retainedDisk': str(disk), 'binaries': expected,
              'scope': 'actual 80x24 TUI preview/back and CLI plan parity, then durable definition removal of a never-booted BIOS fixture; full retained disk SHA verified; no disk-deletion or boot claim',
              'fixtureScriptSHA256': sha(__file__), 'jobs': [], 'cleanupPerformed': False}
    commands = []; before = None; baseline = None; planned = None

    def native(program, *arguments):
        require(program in ('virsh', 'qemu-img', 'mkdir', 'sha256sum', 'stat'), 'unexpected native program')
        argv = ['/usr/bin/sudo', '-n', '--', '/usr/bin/' + program]
        if program == 'virsh':
            argv.extend(['--connect', URI])
        argv.extend(map(str, arguments))
        result = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, timeout=30,
                                env=dict(os.environ, LC_ALL='C'))
        require(len(result.stdout) + len(result.stderr) <= 2 << 20, 'native output exceeded bound')
        commands.append({'argv': argv, 'exitCode': result.returncode, 'stdout': result.stdout.decode(), 'stderr': result.stderr.decode()})
        runner.save('native-commands.json', commands)
        require(result.returncode == 0, 'native command failed; retain fixture and inspect native-commands.json')
        return result.stdout.decode()

    def lines(command, identity=None):
        args = [command]
        if identity is not None: args.append(identity)
        args.extend(['--name'] if identity else ['--all', '--uuid'])
        values = native('virsh', *args).splitlines()
        values = [value for value in values if value]
        require(len(values) <= 1024 and len(values) == len(set(values)) and
                all(len(value) <= 256 and value.isprintable() for value in values), 'ambiguous native list')
        return sorted(values)

    def snapshot():
        vms = runner.cli('vm', 'list')
        values = inventory(vms, URI)
        native_vms = {}
        for current in lines('list'):
            canonical_uuid(current)
            require(current in values, 'native/service inventory mismatch')
            vm = next(v for v in vms if v['key']['resourceUUID'] == current)
            inactive = native('virsh', 'dumpxml', current, '--inactive') if vm.get('persistentXML') else ''
            item = {'state': native('virsh', 'domstate', current),
                    'definition': hashlib.sha256(inactive.encode()).hexdigest()}
            for kind in ('snapshot', 'checkpoint'):
                records = {}
                for label in lines(kind + '-list', current):
                    raw = native('virsh', kind + '-dumpxml', current, label)
                    records[label] = hashlib.sha256(raw.encode()).hexdigest()
                item[kind] = records
            item['autostart'], item['hasManagedSave'] = vm['autostart'], vm['hasManagedSave']
            native_vms[current] = item
        require(set(native_vms) == set(values), 'native/service VM set differs')
        networks = {}
        for current in lines('net-list'):
            canonical_uuid(current)
            networks[current] = {'xml': hashlib.sha256(native('virsh', 'net-dumpxml', current).encode()).hexdigest(),
                                 'info': native('virsh', 'net-info', current)}
        return {'vms': values, 'nativeVMs': native_vms, 'networks': networks,
                'recoverySets': runner.cli('snapshot', 'list'), 'jobs': runner.cli('operation', 'list'),
                'sourceMediaMetadata': media_listing(Path.home() / 'images')}

    def disk_hash():
        require(native('stat', '--format=%F|%u', '--', disk).strip() == 'regular file|0',
                'retained disk is not an ordinary root-owned fixture file')
        result = native('sha256sum', '--', disk).strip()
        require(re.fullmatch(r'[0-9a-f]{64}  ' + re.escape(str(disk)), result), 'ambiguous disk hash output')
        return result[:64]

    try:
        before = snapshot(); runner.save('before.json', before)
        baseline = dict(before)
        for key in ('vms', 'nativeVMs'):
            baseline[key] = dict(before[key])
            if resume: baseline[key].pop(identity, None)
        require(all(job['state'] in TERMINAL for job in before['jobs']), 'active job prevents coordinated removal test')
        if resume:
            require(identity in before['vms'] and before['vms'][identity]['name'] == name and
                    [key for key, vm in before['vms'].items() if vm['name'] == name] == [identity], 'retained fixture identity differs')
        else:
            require(identity not in before['vms'] and all(v['name'] != name for v in before['vms'].values()), 'fixture identity collision')
        report['versions'] = {'virmill': runner.cli('version'), 'libvirt': native('virsh', 'version'),
                              'qemuImg': native('qemu-img', '--version')}
        parent = canonical_path(str(directory.parent))
        require(stat.S_ISDIR(parent.lstat().st_mode) and parent.stat().st_uid == 0, 'image parent differs')
        source = out / 'generated-source.raw'
        definition = out / 'fixture.xml'
        if resume:
            previous, original_vm, artifacts = resume
            vm = runner.cli('vm', 'show', identity)
            require(vm == original_vm, 'retained native VM or original observation changed')
            check_fixture(vm, identity, name, disk)
            failed_observation = strict_json(Path(artifacts['final-observation.json']['path']).read_bytes())
            require(before['nativeVMs'][identity] == failed_observation['nativeVMs'][identity],
                    'native XML, stopped state or snapshot metadata differs from immutable failed observation')
            require(disk_hash() == previous['initialDiskSHA256'] and
                    int(native('stat', '--format=%s', '--', disk).strip()) == previous['diskBytes'], 'retained disk changed')
            for filename in ('generated-source.raw', 'fixture.xml'):
                with (out / filename).open('xb') as target, Path(artifacts[filename]['path']).open('rb') as origin:
                    shutil.copyfileobj(origin, target); target.flush(); os.fsync(target.fileno())
            report.update(resumedFixture=True, resumeEvidence=artifacts, machine=previous['machine'])
        else:
            require(not directory.exists() and not directory.is_symlink(), 'unique fixture directory already exists')
            capabilities = native('virsh', 'capabilities'); machine = select_machine(capabilities)
            runner.save('capabilities.xml', capabilities.encode()); report['machine'] = machine
            with source.open('xb') as stream:
                stream.write(('Virmill definition-removal fixture ' + identity + '\n').encode())
                stream.seek((8 << 20) - 1); stream.write(b'\0'); stream.flush(); os.fsync(stream.fileno())
            native('mkdir', '--mode=0700', '--', directory)
            native('qemu-img', 'convert', '-f', 'raw', '-O', 'qcow2', str(source), str(disk))
            definition.write_text(fixture_xml(identity, machine, disk))
        report['sourceRawSHA256'] = sha(source)
        require(native('stat', '--format=%F|%u|%a', '--', directory).strip() == 'directory|0|700', 'fixture directory ownership differs')
        info = strict_json(native('qemu-img', 'info', '--output=json', str(disk)).encode())
        require(info['format'] == 'qcow2' and info['virtual-size'] == 8 << 20 and
                not info.get('backing-filename') and not info.get('snapshots'), 'generated disk format or dependencies differ')
        initial_hash = disk_hash(); report['initialDiskSHA256'] = initial_hash
        report['diskBytes'] = int(native('stat', '--format=%s', '--', disk).strip())
        report['sourceDefinitionSHA256'] = sha(definition)
        runner.save('report.json', report)
        if not resume: native('virsh', 'define', str(definition), '--validate')
        vm = runner.cli('vm', 'show', identity); check_fixture(vm, identity, name, disk)
        require(not lines('snapshot-list', identity) and not lines('checkpoint-list', identity), 'new fixture has unexpected native metadata')
        runner.save('defined-fixture.json', vm)
        tui_plan = tui_preview(runner, vm, disk)
        require(runner.cli('vm', 'show', identity) == vm and runner.cli('operation', 'list') == before['jobs'],
                'TUI preview/back changed the fixture or prior durable jobs')
        planned = runner.cli('vm', 'remove', identity, '--plan')
        check_plan(planned, vm, disk); runner.save('removal-plan.json', planned)
        require(normalized_plan(tui_plan) == normalized_plan(planned), 'TUI and CLI removal plan semantics differ')
        report.update(tuiPlanID=tui_plan['planID'],tuiCLIPlanParity=True,tuiBackRetainedConfirmation=True,tuiCanceledWithoutApply=True)
        # Recheck exact stopped identity and source bytes before the one apply.
        check_fixture(runner.cli('vm', 'show', identity), identity, name, disk)
        require(disk_hash() == initial_hash, 'disk changed before removal apply')
        if resume:
            require(all(sha(item['path']) == item['sha256'] for item in resume[2].values()),
                    'immutable failed-fixture evidence changed before apply')
        argv = ['plan', 'apply', planned['planID'], '--digest', planned['planDigest']]
        for ack in sorted(ACKS): argv.extend(['--ack', ack])
        argv.extend(['--detach', '--idempotency-key', 'removal-' + identity])
        report['planID'], report['applyAttempted'] = planned['planID'], True
        runner.save('report.json', report)
        job = runner.cli(*argv)
        canonical_uuid(job['operationID']); report['jobs'].append(job['operationID'])
        job_id = job['operationID']
        runner.save('accepted-job.json', job); runner.save('report.json', report)
        deadline = time.monotonic() + 40
        while job['state'] not in TERMINAL and time.monotonic() < deadline:
            time.sleep(.2); job = runner.cli('operation', 'show', job_id)
        runner.save('final-job.json', job)
        require(job['operationID'] == job_id and job['state'] == 'succeeded' and job['planID'] == planned['planID'],
                'removal incomplete/uncertain; do not replay')
        require(identity not in lines('list'), 'removed UUID remains in native inventory')
        report['finalDiskSHA256'] = disk_hash()
        require(report['finalDiskSHA256'] == initial_hash and sha(source) == report['sourceRawSHA256'], 'retained disk or generated source bytes changed')
        after = snapshot(); runner.save('after.json', after)
        remaining_jobs = [j for j in after['jobs'] if j['operationID'] not in report['jobs']]
        require(remaining_jobs == before['jobs'], 'prior durable jobs changed')
        after['jobs'] = remaining_jobs
        require(after == baseline, 'unrelated guests, networks, snapshots, recovery sets or source media changed')
        report.update(status='passed',durableRemovalSucceeded=True,exactUUIDAbsent=True,diskBytesRetained=True,
                      unrelatedGuestsNetworksSnapshotsJobsAndMediaPreserved=True,fixtureNeverBooted=True)
    except BaseException as error:
        report['error'] = repr(error)
    finally:
        try:
            report['finalNativeUUIDs'] = lines('list')
            if before is not None:
                final = snapshot(); runner.save('final-observation.json', final)
                for key in ('vms', 'nativeVMs'):
                    final[key].pop(identity, None)
                report['unrelatedFinalObservationMatches'] = all(final[key] == baseline[key] for key in baseline if key != 'jobs')
                if report['status'] == 'passed':
                    require(report['unrelatedFinalObservationMatches'], 'final preservation observation changed')
            if 'initialDiskSHA256' in report:
                report['observedRetainedDiskSHA256'] = disk_hash()
        except BaseException as error:
            report['finalObservationError'] = repr(error)
            report['status'] = 'failed'
        runner.save('report.json', report); os.close(fd); print(json.dumps(report, sort_keys=True))
    return report['status'] == 'passed'


class PureRules(unittest.TestCase):
    def fixture(self):
        identity = '77345678-1234-4234-8234-123456789abc'
        disk = Path('/var/lib/libvirt/images') / ('virmill-removal-' + identity) / 'disk.qcow2'
        raw = fixture_xml(identity, 'pc-i440fx-10.1', disk)
        vm = {'key': {'providerID': 'libvirt', 'connectionID': URI, 'kind': 'vm', 'resourceUUID': identity},
              'name': 'virmill-removal-' + identity[:8], 'state': 'stopped', 'autostart': False,
              'hasManagedSave': False, 'persistentXML': raw}
        return identity, disk, vm

    def test_fixture_has_only_owned_disk_and_no_runtime_devices(self):
        identity, disk, vm = self.fixture(); check_fixture(vm, identity, vm['name'], disk)
        for change in ('state', 'autostart', 'hasManagedSave'):
            altered = dict(vm); altered[change] = 'running' if change == 'state' else True
            with self.assertRaises(Exception): check_fixture(altered, identity, vm['name'], disk)

    def test_generated_path_cannot_target_existing_media(self):
        identity, _, _ = self.fixture()
        with self.assertRaises(Exception): fixture_xml(identity, 'pc-i440fx-10.1', Path('/home/virmill-test/images/source.qcow2'))

    def test_resume_refuses_every_uncertain_apply_or_changed_binding(self):
        identity, disk, vm = self.fixture()
        report = {'status': 'failed', 'jobs': [], 'cleanupPerformed': False,
                  'fixtureUUID': identity, 'fixtureName': vm['name'], 'retainedDisk': str(disk),
                  'initialDiskSHA256': 'a' * 64, 'observedRetainedDiskSHA256': 'a' * 64,
                  'sourceRawSHA256': 'b' * 64, 'sourceDefinitionSHA256': 'c' * 64,
                  'unrelatedFinalObservationMatches': True}
        self.assertEqual(check_resume_report(report), (identity, disk))
        for field, value in [('applyAttempted', True), ('applyAttempted', 'false'), ('status', 'passed'),
                             ('jobs', [identity]), ('jobs', None), ('cleanupPerformed', True),
                             ('fixtureName', 'another VM'), ('retainedDisk', '/home/virmill-test/images/source.qcow2'),
                             ('observedRetainedDiskSHA256', 'd' * 64), ('sourceRawSHA256', ''),
                             ('unrelatedFinalObservationMatches', False)]:
            with self.subTest(field=field, value=value):
                with self.assertRaises(Exception): check_resume_report(dict(report, **{field: value}))

    def test_removal_review_refuses_disk_deletion_or_different_vm(self):
        identity, disk, vm = self.fixture()
        plan = {'operation': 'vm.remove-definition-v1', 'connectionID': URI, 'actorUID': 1000,
                'resourceIDs': ['libvirt|' + URI + '|vm|' + identity], 'planID': identity, 'planDigest': 'a' * 64,
                'acknowledgements': sorted(ACKS), 'review': {'action': 'remove', 'resource': vm['key'], 'vmName': vm['name'],
                'definitionSHA256': hashlib.sha256(vm['persistentXML'].encode()).hexdigest(), 'retainedSources': [str(disk)],
                'diskDeletion': False, 'backupsDeleted': False, 'configurationRemoved': True,
                'backupCreated': False, 'requiresStopped': True, 'automaticStop': False}}
        check_plan(plan, vm, disk)
        for field, value in [('diskDeletion', True), ('backupsDeleted', True), ('automaticStop', True), ('retainedSources', []), ('vmName', 'other')]:
            changed = dict(plan); changed['review'] = dict(plan['review'], **{field: value})
            with self.assertRaises(Exception): check_plan(changed, vm, disk)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--self-test', action='store_true')
    parser.add_argument('--root', type=Path)
    parser.add_argument('--execute-disposable', action='store_true')
    parser.add_argument('--resume-fixture', type=Path, help='Prior failed report with no apply attempt; revalidate and reuse its exact fixture')
    args = parser.parse_args()
    if args.self_test:
        require(not args.execute_disposable and args.root is None and args.resume_fixture is None, 'self-test cannot authorize execution')
        return not unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(PureRules)).wasSuccessful()
    require(args.execute_disposable and args.root is not None, 'explicit disposable execution and private stage are required')
    return not execute(args.root, args.resume_fixture)


if __name__ == '__main__':
    sys.exit(main())
