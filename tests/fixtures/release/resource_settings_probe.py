#!/usr/bin/env python3
"""Native current-settings → TUI CPU/RAM edit → native readback fixture.

Requires --execute-disposable --root PRIVATE_STAGED_DIRECTORY on the authorized
UID1000 test host, exact binaries.json and adjacent guest_agent_edit_probe.py and
tui_workspace_probe.py. A fresh marked diskless/networkless persistent guest stays
stopped throughout. Only CPU/RAM change (1/128MiB → 2/256MiB). No guest boot, live
hardware sizing, source conversion, or acceptance completion is claimed.
Cleanup undefines only this owned, stopped fixture after all its jobs settle.
Existing guest XML/state, jobs and owner source-media metadata are preserved.
--self-test checks pure readback/plan preservation rules with no native actions.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import stat
import subprocess
import sys
import time
import unittest
import uuid
import xml.etree.ElementTree as ET
from tui_workspace_probe import Runner, Terminal, canonical_path, inventory, media_listing, require, strict_json
from guest_agent_edit_probe import URI, select_machine, xml_for, owned, semantic


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


ACKS = {'host-mutation', 'exclusive-configuration-writer'}
TERMINAL = {'succeeded', 'failed', 'canceled', 'partial', 'recovery-required'}


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def check_view(view, vm, cpus, mib):
    require(view['resource'] == vm['key'] and view['name'] == vm['name'] and view['fingerprint'] == vm['fingerprint'],
            'resource inspection selected another VM or generation')
    require(view['state'] == vm['state'] == 'stopped' and view['hasManagedSave'] is False and view['live'] is None,
            'stopped fixture falsely reports live resources')
    require(view['persistent']['vcpus'] == cpus and view['persistent']['maximumVcpus'] == cpus
            and view['persistent']['memoryBytes'] == mib << 20 and view['persistent']['maximumMemoryBytes'] == mib << 20,
            'observed persistent resources differ from native definition')
    require(view['canEditCPU'] and view['canEditMemory'] and view['applyModes'] == ['next-boot'],
            'expected stopped resource edits unavailable')



def check_running_view(view, vm):
    require(vm['state'] == view['state'] == 'running' and view['resource'] == vm['key']
            and view['fingerprint'] == vm['fingerprint'] and view['live'] is not None,
            'running resource identity or generation differs')
    for key, xml_key in [('persistent', 'persistentXML'), ('live', 'liveXML')]:
        tree = ET.fromstring(vm[xml_key])
        cpu, maximum, current = tree.find('vcpu'), tree.find('memory'), tree.find('currentMemory')
        require(cpu is not None and maximum is not None and maximum.get('unit') == 'KiB'
                and (current is None or current.get('unit') == 'KiB'), 'running native XML not canonical KiB')
        expected = {'vcpus': int(cpu.get('current', cpu.text)), 'maximumVcpus': int(cpu.text),
                    'memoryBytes': int(current.text if current is not None else maximum.text) * 1024,
                    'maximumMemoryBytes': int(maximum.text) * 1024}
        require(all(view[key].get(field) == value for field, value in expected.items()), 'running ' + key + ' resource values differ')
    require(not view['canEditCPU'] and not view['canEditMemory'], 'running VM offers forbidden basic edit')
    require(view['requiresShutdown'] == (not vm['hasManagedSave'] and 'next-boot' in view['applyModes']),
            'running shutdown guidance differs from available next-boot edit')


def check_plan(plan, identity):
    require(plan['operation'] == 'vm.configure-resources' and plan['connectionID'] == URI and plan['actorUID'] == 1000
            and plan['resourceIDs'] == ['libvirt|' + URI + '|vm|' + identity], 'review target or operation differs')
    require(len(plan['acknowledgements']) == len(ACKS) and set(plan['acknowledgements']) == ACKS,
            'unreviewed acknowledgements')
    review = plan['review']
    require(review['vmID'] == identity and review['requested'] == {'vcpus': 2, 'memoryMiB': 256, 'applyMode': 'next-boot'}
            and review['persistentEdit'] and review['requiresShutdown'] and not review['diskDeletion'], 'review scope differs')


def check_xml(before, after, identity, name):
    a, b = owned(before, identity, name), owned(after, identity, name)
    require(a.findtext('vcpu') == '1' and b.findtext('vcpu') == '2', 'CPU edit differs')
    require(a.findtext('memory') == a.findtext('currentMemory') == '131072'
            and b.findtext('memory') == b.findtext('currentMemory') == '262144', 'memory edit differs')
    for path in ('vcpu', 'memory', 'currentMemory'):
        require(a.find(path).attrib == b.find(path).attrib, 'resource attributes changed')
        a.find(path).text = b.find(path).text = 'REVIEWED'
    require(semantic(a) == semantic(b), 'unrelated native XML changed')


def execute(stage):
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000,
            'wrong authorized test host or actor')
    stage = canonical_path(str(stage.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_ISDIR(stage.lstat().st_mode) and stat.S_IMODE(stage.stat().st_mode) == 0o700,
            'private staged root required')
    os.umask(0o077)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    require(all(digest('/usr/bin/' + name) == expected[name] for name in ('virmill', 'virmilld')), 'installed binary mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    out = stage / 'resource-settings'; out.mkdir(mode=0o700)
    runner = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, fd)
    with os.fdopen(os.dup(fd), 'rb') as stream:
        require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected['virmill'], 'held binary mismatch')
    logs, planned_ids, allowed_jobs = [], set(), set()
    def native(*arguments, check=True):
        argv = ['/usr/bin/virsh', '--connect', URI, *arguments]
        result = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, timeout=30,
                                env=dict(os.environ, LC_ALL='C'))
        require(len(result.stdout) + len(result.stderr) <= 2 << 20, 'native output bound')
        logs.append({'argv': argv, 'exitCode': result.returncode, 'stdout': result.stdout.decode(), 'stderr': result.stderr.decode()})
        runner.save('native-commands.json', logs)
        require(not check or result.returncode == 0, 'native command failed; inspect evidence')
        return result
    identity = str(uuid.uuid4()); name = 'virmill-channel-edit-' + identity[:8]
    report = {'status': 'failed', 'scope': 'stopped resource readback and TUI submission; no boot/live hardware claim',
              'acceptanceSupport': ['CORE-03', 'UX-01', 'UX-02'], 'fixtureUUID': identity, 'fixtureName': name,
              'binaries': expected}
    before = prior_jobs = media = None
    definition_attempted = False
    terminal = None
    try:
        before = inventory(runner.cli('vm', 'list'), URI)
        prior_jobs = runner.cli('operation', 'list')
        require(all(j['state'] in TERMINAL for j in prior_jobs), 'another operation is active')
        require(identity not in before and all(v['name'] != name for v in before.values()), 'fixture identity collision')
        running = next((identity for identity, value in before.items() if value['state'] == 'running'), None)
        if running is not None:
            running_vm = runner.cli('vm', 'show', running)
            running_view = runner.cli('vm', 'resources', 'show', running)
            check_running_view(running_view, running_vm)
            runner.save('running-readonly-resources.json', running_view)
            report['existingRunningVMReadOnly'] = {'vmID': running, 'persistentAndLiveValuesMatchedXML': True,
                                                  'basicEditsUnavailable': True, 'requiresShutdown': running_view['requiresShutdown']}
        else:
            report['existingRunningVMReadOnly'] = {'available': False, 'reason': 'No baseline running guest; none started for this check'}
        media = media_listing(Path.home() / 'images')
        runner.save('before-vms.json', before); runner.save('before-jobs.json', prior_jobs)
        capabilities = native('capabilities').stdout.decode(); machine = select_machine(capabilities)
        runner.save('capabilities.xml', capabilities.encode())
        caps = ET.fromstring(native('domcapabilities', '--virttype', 'kvm', '--arch', 'x86_64', '--machine', machine).stdout)
        require(caps.findtext('machine') == machine and caps.findtext('domain') == 'kvm', 'machine capability changed')
        report['machine'] = machine
        definition = out / 'fixture.xml'; definition.write_text(xml_for(identity, name, machine))
        definition_attempted = True; native('define', str(definition), '--validate')
        original = runner.cli('vm', 'show', identity); owned(original['persistentXML'], identity, name)
        runner.save('before-fixture.json', original)
        initial = runner.cli('vm', 'resources', 'show', identity); runner.save('before-resources.json', initial)
        check_view(initial, original, 1, 128)
        terminal = Terminal(runner, 'resource-settings-80x24', 80, 24)
        def wait(label, predicate, key=None):
            return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
        def focused(text, label):
            return any(re.match(r'^>\s*(?:\[\s*)?' + re.escape(label), line) for line in text.splitlines())
        def focus(label):
            for index in range(12):
                if focused(terminal.screen.text(), label): return
                old = terminal.screen.text(); wait('Focus ' + label + ' ' + str(index), lambda s: s != old, b'\t')
            raise RuntimeError('could not focus ' + label)
        wait('Overview', lambda s: 'Virtual machines' in s)
        wait('VM table', lambda s: 'NAME' in s and 'STATE' in s, b'2')
        wait('Filter focus', lambda s: 'Enter Keep filter' in s, b'/')
        wait('Owned fixture selected', lambda s: name in s and '1 of 1 selected' in s, name.encode())
        wait('Filter kept', lambda s: 'Enter Keep filter' not in s, b'\r')
        wait('Owned VM details', lambda s: 'VM details' in s and name in s, b'\r')
        wait('Current resources loaded', lambda s: 'Requested CPU cores: [1]' in s and 'Requested RAM (MiB): [128]' in s and 'Preview changes' in s, b'e')
        # Labels are frozen with workspace_resources.go; no guessed command text.
        for label, value in [('Requested CPU cores', '2'), ('Requested RAM (MiB)', '256')]:
            focus(label); terminal.send(b'\x15')
            while terminal.read(.05) and not terminal.screen.complete(): pass
            wait('Edit ' + label, lambda s: focused(s, label) and value in s, value.encode())
        focus('Preview changes')
        wait('Resource preview', lambda s: 'Nothing has been applied' in s, b'\r')
        for page in range(12):
            match = re.search(r'Plan ID:\s*([0-9a-f-]{36})', terminal.screen.text())
            if match: break
            old = terminal.screen.text(); wait('Read plan ' + str(page), lambda s: s != old, b'\x1b[6~')
        require(match is not None, 'preview plan identity not visible')
        plan = runner.cli('plan', 'show', match.group(1)); check_plan(plan, identity)
        planned_ids.add(plan['planID']); runner.save('resource-plan.json', plan)
        require(inventory(runner.cli('vm', 'list'), URI)[identity] == inventory([original], URI)[identity], 'preview mutated fixture')
        wait('Confirm reviewed changes', lambda s: 'Confirm reviewed changes' in s and plan['planID'] in s, b'\r')
        for ack in plan['acknowledgements']:
            require(re.search(r'>\s*\[ \]', terminal.screen.text()), 'unchecked acknowledgement not focused')
            wait('Acknowledge ' + ack, lambda s: re.search(r'>\s*\[x\]', s), b' ')
            old = terminal.screen.text(); wait('Next acknowledgement', lambda s: s != old, b'\t')
        focus('Apply reviewed plan')
        wait('Operation accepted', lambda s: 'Operation accepted.' in s or 'Job /' in s, b'\r')
        deadline = time.monotonic() + 90; job = None
        while time.monotonic() < deadline:
            found = [j for j in runner.cli('operation', 'list') if j['planID'] == plan['planID']]
            require(len(found) <= 1, 'review accepted multiple operations')
            if found:
                job = found[0]; allowed_jobs.add(job['operationID'])
                if job['state'] in TERMINAL: break
            terminal.read(.3)
        runner.save('resource-job.json', job)
        require(job is not None and job['state'] == 'succeeded', 'native edit did not complete; retain uncertain fixture')
        terminal.close(); terminal = None
        current = runner.cli('vm', 'show', identity); runner.save('after-fixture.json', current)
        final = runner.cli('vm', 'resources', 'show', identity); runner.save('after-resources.json', final)
        check_view(final, current, 2, 256)
        check_xml(original['persistentXML'], current['persistentXML'], identity, name)
        terminal = Terminal(runner, 'resource-readback-80x24', 80, 24)
        wait('Readback overview', lambda s: 'Virtual machines' in s)
        wait('Readback VM table', lambda s: 'NAME' in s and 'STATE' in s, b'2')
        wait('Readback filter focus', lambda s: 'Enter Keep filter' in s, b'/')
        wait('Readback fixture selected', lambda s: name in s and '1 of 1 selected' in s, name.encode())
        wait('Readback filter kept', lambda s: 'Enter Keep filter' not in s, b'\r')
        wait('Readback VM details', lambda s: 'VM details' in s and name in s, b'\r')
        wait('Updated resource defaults', lambda s: 'Requested CPU cores: [2]' in s and 'Requested RAM (MiB): [256]' in s and 'Preview changes' in s, b'e')
        report.update(status='passed', vcpus=2, memoryMiB=256, nativePersistentXMLPreserved=True,
                      mutationSubmittedThroughTUI=True, currentValuesReadThroughCLIAndTUI=True,
                      guestStarted=False, operationID=job['operationID'])
    except BaseException as error:
        report['error'] = repr(error)
    finally:
        if terminal is not None: terminal.close()
        try:
            current_jobs = runner.cli('operation', 'list')
            accepted = [j for j in current_jobs if j['planID'] in planned_ids]
            allowed_jobs.update(j['operationID'] for j in accepted)
            runner.save('accepted-jobs.json', accepted)
            require(all(j['state'] in ('succeeded', 'failed', 'canceled') for j in accepted), 'active/uncertain effect retained')
            if definition_attempted:
                raw = native('dumpxml', identity, '--inactive', check=False)
                if raw.returncode == 0:
                    owned(raw.stdout.decode(), identity, name)
                    require(native('domstate', identity).stdout.decode().strip() == 'shut off', 'fixture running; refuse cleanup')
                    native('undefine', identity); report['ownedFixtureUndefined'] = True
        except BaseException as error:
            report['status'] = 'failed'; report['cleanupError'] = repr(error)
        try:
            if before is not None:
                after = inventory(runner.cli('vm', 'list'), URI); runner.save('after-vms.json', after)
                require(after == before, 'existing guest state/XML changed or owned fixture retained')
                report['existingGuestsPreserved'] = True
            if prior_jobs is not None:
                indexed = {j['operationID']: j for j in runner.cli('operation', 'list')}
                require(all(indexed.get(j['operationID']) == j for j in prior_jobs)
                        and set(indexed) - {j['operationID'] for j in prior_jobs} <= allowed_jobs, 'unrelated jobs changed')
                report['existingJobsPreserved'] = True
            if media is not None:
                require(media_listing(Path.home() / 'images') == media, 'owner source media changed')
                report['ownerMediaMetadataPreserved'] = True
        except BaseException as error:
            report['status'] = 'failed'; report['preservationError'] = repr(error)
        runner.save('report.json', report); os.close(fd)
        print(json.dumps(report, sort_keys=True))
    return 0 if report['status'] == 'passed' else 1


class Tests(unittest.TestCase):
    identity = '12345678-1234-4234-8234-123456789abc'
    name = 'virmill-channel-edit-12345678'
    def test_xml_checks_only_allow_requested_resource_text(self):
        raw = xml_for(self.identity, self.name, 'pc-i440fx-10.2')
        raw = raw.replace('<memory unit="MiB">128</memory>', '<memory unit="KiB">131072</memory><currentMemory unit="KiB">131072</currentMemory>')
        after = raw.replace('131072', '262144').replace('<vcpu>1</vcpu>', '<vcpu>2</vcpu>')
        check_xml(raw, after, self.identity, self.name)
        for bad in (after.replace('retain  this opaque value', 'changed'), after.replace('pc-i440fx-10.2', 'pc-i440fx-10.1'),
                    after.replace('unit="KiB"', 'unit="bytes"'), after.replace('<vcpu>2', '<vcpu>3')):
            with self.assertRaises(RuntimeError): check_xml(raw, bad, self.identity, self.name)
    def test_running_view_keeps_live_and_persistent_values_distinct(self):
        raw = '<domain><vcpu current="2">4</vcpu><memory unit="KiB">262144</memory><currentMemory unit="KiB">131072</currentMemory></domain>'
        key = {'resourceUUID': self.identity}
        vm = {'state': 'running', 'key': key, 'fingerprint': 'f', 'hasManagedSave': False,
              'persistentXML': raw, 'liveXML': raw.replace('current="2"', 'current="1"')}
        view = {'state': 'running', 'resource': key, 'fingerprint': 'f', 'canEditCPU': False, 'canEditMemory': False,
                'requiresShutdown': True, 'applyModes': ['next-boot'],
                'persistent': {'vcpus': 2, 'maximumVcpus': 4, 'memoryBytes': 128 << 20, 'maximumMemoryBytes': 256 << 20},
                'live': {'vcpus': 1, 'maximumVcpus': 4, 'memoryBytes': 128 << 20, 'maximumMemoryBytes': 256 << 20}}
        check_running_view(view, vm)
        bad = copy.deepcopy(view); bad['live'] = bad['persistent']
        with self.assertRaises(RuntimeError): check_running_view(bad, vm)
        bad = copy.deepcopy(view); bad['canEditCPU'] = True
        with self.assertRaises(RuntimeError): check_running_view(bad, vm)

    def test_plan_refuses_added_scope(self):
        p = {'operation': 'vm.configure-resources', 'connectionID': URI, 'actorUID': 1000,
             'resourceIDs': ['libvirt|' + URI + '|vm|' + self.identity], 'acknowledgements': sorted(ACKS),
             'review': {'vmID': self.identity, 'requested': {'vcpus': 2, 'memoryMiB': 256, 'applyMode': 'next-boot'},
                        'persistentEdit': True, 'requiresShutdown': True, 'diskDeletion': False}}
        check_plan(p, self.identity)
        for change in ('disk', 'cpu', 'ack'):
            bad = copy.deepcopy(p)
            if change == 'disk': bad['review']['diskDeletion'] = True
            elif change == 'cpu': bad['review']['requested']['vcpus'] = 3
            else: bad['acknowledgements'].append('other-risk')
            with self.assertRaises(RuntimeError): check_plan(bad, self.identity)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path)
    parser.add_argument('--execute-disposable', action='store_true')
    parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        require(args.root is None and not args.execute_disposable, 'self-test cannot select native actions')
        unittest.main(argv=[sys.argv[0]])
    else:
        require(args.root is not None and args.execute_disposable, 'explicit disposable run/root required')
        sys.exit(execute(args.root))
