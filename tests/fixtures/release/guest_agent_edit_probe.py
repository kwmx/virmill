#!/usr/bin/env python3
"""Parent-operated native existing-VM guest-agent channel edit fixture.

Native mode: --execute-disposable --root PRIVATE_STAGED_DIRECTORY. Requires
binaries.json and adjacent tui_workspace_probe.py. Only the owner-authorized
<test-vm-login> host/UID1000 may execute it. Creates one NEW persistent diskless,
networkless, stopped BIOS VM using an actually advertised i440fx machine.
Virmill previews and applies the opt-in channel edit; native readback proves
normalization and preservation. CLI and TUI observations distinguish a configured
channel from installed software. The guest never boots. No guest-agent package,
agent response, guest OS compatibility or complete acceptance claim is made.

Cleanup rechecks UUID, name, ownership metadata, stopped state and absence of
all disks/NICs before undefine, with no storage-removal or other broad flags.
Existing guests/jobs and source-media metadata must remain unchanged. All evidence
is retained. No downloads, SSH, package installs or host-wide configuration edits.
--self-test checks pure fixture/review/readback logic only; it never calls libvirt.
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
import sys
import unittest
import uuid
import xml.etree.ElementTree as ET

from tui_workspace_probe import (Runner, Terminal, canonical_path, canonical_uuid,
                                 inventory, media_listing, require, strict_json)


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


URI = 'qemu:///system'
NS = 'urn:virmill:guest-agent-edit-fixture:v1'
AGENT = 'org.qemu.guest_agent.0'
ACKS = {'host-mutation', 'exclusive-configuration-writer', 'guest-agent-host-access'}


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def select_machine(raw):
    require(len(raw) <= 2 << 20, 'capability XML exceeds bound')
    root = ET.fromstring(raw)
    require(root.tag == 'capabilities', 'native capabilities root differs')
    candidates = set()
    for guest in root.findall('guest'):
        if guest.findtext('os_type') != 'hvm':
            continue
        for arch in guest.findall('arch'):
            if arch.get('name') != 'x86_64' or arch.find("domain[@type='kvm']") is None:
                continue
            for machine in [*arch.findall('machine'), *arch.findall("domain[@type='kvm']/machine")]:
                name = machine.get('canonical') or machine.text or ''
                if re.fullmatch(r'pc-i440fx-[0-9]+\.[0-9]+', name):
                    candidates.add(name)
    require(candidates, 'no explicit versioned i440fx KVM machine advertised; no fallback guessed')
    return max(candidates, key=lambda name: tuple(map(int, name.rsplit('-', 1)[1].split('.'))))


def xml_for(identity, name, machine):
    canonical_uuid(identity)
    require(name == 'virmill-channel-edit-' + identity[:8] and
            re.fullmatch(r'pc-i440fx-[0-9]+\.[0-9]+', machine), 'fixture identity/machine differs')
    root = ET.Element('domain', {'type': 'kvm'})
    ET.SubElement(root, 'name').text = name
    ET.SubElement(root, 'uuid').text = identity
    metadata = ET.SubElement(root, 'metadata')
    owner = ET.SubElement(metadata, '{' + NS + '}fixture', {'id': identity, 'version': '1'})
    ET.SubElement(owner, '{' + NS + '}opaque', {'retain': 'yes'}).text = 'retain  this opaque value'
    ET.SubElement(root, 'memory', {'unit': 'MiB'}).text = '128'
    ET.SubElement(root, 'vcpu').text = '1'
    os_node = ET.SubElement(root, 'os')
    ET.SubElement(os_node, 'type', {'arch': 'x86_64', 'machine': machine}).text = 'hvm'
    devices = ET.SubElement(root, 'devices')
    ET.SubElement(devices, 'controller', {'type': 'pci', 'index': '0', 'model': 'pci-root'})
    return ET.tostring(root, encoding='unicode')


def owned(raw, identity, name):
    require(len(raw) <= 2 << 20, 'fixture XML exceeds bound')
    root = ET.fromstring(raw)
    marker = root.find('metadata/{' + NS + '}fixture')
    require(root.findtext('uuid') == identity and root.findtext('name') == name and marker is not None and
            marker.attrib == {'id': identity, 'version': '1'} and
            marker.find('{' + NS + '}opaque').text == 'retain  this opaque value',
            'fixture ownership or opaque metadata differs; refuse cleanup')
    require(not root.findall('devices/disk') and not root.findall('devices/interface') and
            not root.findall('devices/hostdev') and not root.findall('devices/filesystem') and
            not root.findall('devices/tpm') and root.find('os/loader') is None and
            root.find('os/nvram') is None, 'fixture acquired storage, host devices or firmware state; refuse cleanup')
    return root


def semantic(node):
    # Indentation is ignorable only in known libvirt container elements. The
    # unknown fixture metadata content remains exact, including internal spaces.
    known = node.tag in ('domain', 'metadata', 'os', 'devices', 'controller', 'channel')
    text = node.text or ''
    if known and not text.strip():
        text = ''
    children = []
    for child in node:
        tail = child.tail or ''
        if known and not tail.strip():
            tail = ''
        children.append((semantic(child), tail))
    return node.tag, tuple(sorted(node.attrib.items())), text, tuple(children)


def verify_edit(before, after, identity, name):
    original = owned(before, identity, name)
    observed = owned(after, identity, name)
    require(not original.findall('devices/channel') and not original.findall("devices/controller[@type='virtio-serial']"),
            'fixture was already configured')
    channels = observed.findall('devices/channel')
    controllers = observed.findall("devices/controller[@type='virtio-serial']")
    require(len(channels) == 1 and len(controllers) == 1, 'exactly one channel/controller addition required')
    channel, controller = channels[0], controllers[0]
    require(channel.attrib == {'type': 'unix'} and len(channel.findall('source')) == 0 and
            len(channel.findall('target')) == 1 and channel.find('target').attrib == {'type': 'virtio', 'name': AGENT} and
            len(channel.findall('address')) == 1 and channel.find('address').attrib ==
            {'type': 'virtio-serial', 'controller': '0', 'bus': '0', 'port': '1'}, 'agent channel differs')
    require(controller.attrib == {'type': 'virtio-serial', 'index': '0', 'model': 'virtio'}, 'agent controller differs')
    for node in channel:
        require(node.tag in ('target', 'address', 'alias') and len(node) == 0, 'opaque channel addition')
        if node.tag == 'alias':
            require(node.attrib == {'name': 'channel0'}, 'agent alias differs')
    require(len(channel.findall('alias')) <= 1, 'duplicate agent alias')
    for node in controller:
        require(node.tag in ('alias', 'address') and len(node) == 0, 'opaque controller addition')
        if node.tag == 'alias':
            require(node.attrib == {'name': 'virtio-serial0'}, 'controller alias differs')
        else:
            require(set(node.attrib) == {'type', 'domain', 'bus', 'slot', 'function'} and node.get('type') == 'pci',
                    'controller placement shape differs')
            for key, maximum in (('domain', 0), ('bus', 255), ('slot', 31), ('function', 0)):
                value = node.get(key, '')
                require(re.fullmatch(r'(?:0x[0-9a-fA-F]+|[0-9]+)', value) and
                        int(value, 16 if value.startswith('0x') else 10) <= maximum, 'controller PCI placement out of bounds')
    require(len(controller.findall('address')) == 1 and len(controller.findall('alias')) <= 1,
            'expected one native controller PCI allocation')
    observed.find('devices').remove(channel)
    observed.find('devices').remove(controller)
    require(semantic(original) == semantic(observed), 'existing native XML changed beyond the two reviewed additions')
    return {'automaticSocket': True, 'target': AGENT, 'controller': 0, 'bus': 0, 'port': 1,
            'opaqueConfigurationPreserved': True, 'guestBooted': False, 'guestAgentInstalled': False}


def apply_arguments(plan, identity):
    canonical_uuid(plan['planID'])
    require(re.fullmatch('[0-9a-f]{64}', plan['planDigest']) and plan['operation'] == 'vm.configure-guest-agent' and
            plan['connectionID'] == URI and plan['actorUID'] == 1000 and
            plan['resourceIDs'] == ['libvirt|' + URI + '|vm|' + identity], 'review identity/operation differs')
    require(set(plan['acknowledgements']) == ACKS and len(plan['acknowledgements']) == len(ACKS),
            'review acknowledgements changed; fixture will not approve unseen risks')
    review = plan['review']
    require(review['vmID'] == identity and review['requested'] == {'enableGuestAgent': True, 'applyMode': 'next-boot'} and
            review['persistentEdit'] is True and review['requiresShutdown'] is True and review['diskDeletion'] is False,
            'review scope differs')
    argv = ['plan', 'apply', plan['planID'], '--digest', plan['planDigest'], '--idempotency-key',
            'guest-agent-edit-' + identity, '--wait', '--timeout', '30s']
    for ack in plan['acknowledgements']:
        argv.extend(['--ack', ack])
    return argv


def tui_inspection(runner, identity, name, configured):
    terminal = Terminal(runner, 'guest-tools-after' if configured else 'guest-tools-before', 80, 24)
    try:
        def wait(label, predicate, key=None):
            return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
        wait('Overview', lambda screen: 'Virtual machines' in screen)
        wait('VM table', lambda screen: 'NAME' in screen and 'STATE' in screen, b'2')
        wait('VM filter focus', lambda screen: 'Enter Keep filter' in screen, b'/')
        wait('Only owned fixture selected', lambda screen: name in screen and '1 of 1 selected' in screen, name.encode())
        wait('Keep fixture filter', lambda screen: '1 of 1 selected' in screen and 'Enter Keep filter' not in screen, b'\r')
        wait('Owned VM details', lambda screen: identity in screen and 'VM details' in screen, b'\r')
        if configured:
            wait('Configured connection opens installation options', lambda screen: 'Install guest tools' in screen and
                 'Guest-agent connection is configured.' in screen, b'g')
        else:
            wait('Missing connection offers explained enable preview', lambda screen: 'The guest-agent connection is missing.' in screen and
                 'Preview enable connection' in screen and 'No software is installed yet.' in screen, b'g')
        wait('Back to owned VM without submitting', lambda screen: identity in screen and 'VM details' in screen, b'\x1b')
        return {'size': [80, 24], 'configured': configured, 'guidanceVisible': True, 'noPlanOrApply': True}
    finally:
        terminal.close()


def execute(root):
    require(authorized_test_host() and os.getuid() == 1000 and os.geteuid() == 1000,
            'wrong authorized host/actor')
    stage = canonical_path(str(root.absolute()))
    require(stage.is_relative_to(Path.home() / 'virmill-tests') and stage != Path.home() / 'virmill-tests' and
            stat.S_ISDIR(stage.lstat().st_mode) and stage.stat().st_uid == 1000 and
            stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private ordinary staged root required')
    manifest = stage / 'binaries.json'
    require(stat.S_ISREG(manifest.lstat().st_mode) and manifest.stat().st_uid == 1000, 'ordinary owned binary manifest required')
    expected = strict_json(manifest.read_bytes())
    require(all(sha('/usr/bin/' + name) == expected[name] for name in ('virmill', 'virmilld')), 'installed binaries differ')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        with os.fdopen(os.dup(fd), 'rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected['virmill'], 'held binary differs')
        out = stage / 'guest-agent-edit'
        out.mkdir(mode=0o700)
        runner = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, fd)
        logs = []
        def native(*arguments, check=True):
            argv = ['/usr/bin/sudo', '-n', '/usr/bin/virsh', '--connect', URI, *arguments]
            result = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, timeout=30,
                                    env=dict(os.environ, LC_ALL='C'))
            require(len(result.stdout) + len(result.stderr) <= 2 << 20, 'native output exceeds bound')
            logs.append({'argv': argv, 'exitCode': result.returncode, 'stdout': result.stdout.decode(),
                         'stderr': result.stderr.decode()})
            runner.save('native-commands.json', logs)
            require(not check or result.returncode == 0, 'native fixture command failed; see native-commands.json')
            return result
        report = {'status': 'failed', 'scope': 'native persistent channel edit and preserved configuration; no guest boot or agent installation',
                  'binaries': expected, 'acceptanceSupport': ['CORE-03', 'GUEST-03', 'UX-01', 'SEC-02', 'SEC-05']}
        identity = str(uuid.uuid4())
        name = 'virmill-channel-edit-' + identity[:8]
        before = prior_jobs = media = None
        creation_attempted = False
        allowed_jobs = set()
        planned_id = None
        try:
            before = inventory(runner.cli('vm', 'list'), URI)
            prior_jobs = runner.cli('operation', 'list')
            require(all(j['state'] in ('succeeded', 'failed', 'partial', 'canceled', 'recovery-required') for j in prior_jobs),
                    'active jobs found; fixture must execute alone')
            media = media_listing(Path.home() / 'images')
            require(identity not in before and all(v['name'] != name for v in before.values()), 'fixture identity collision')
            runner.save('before-vms.json', before)
            runner.save('before-jobs.json', prior_jobs)
            report['version'] = runner.cli('version')
            report['nativeVersion'] = native('version').stdout.decode()
            capabilities = native('capabilities').stdout.decode()
            runner.save('capabilities.xml', capabilities.encode())
            machine = select_machine(capabilities)
            caps_raw = native('domcapabilities', '--virttype', 'kvm', '--arch', 'x86_64', '--machine', machine).stdout.decode()
            runner.save('domain-capabilities.xml', caps_raw.encode())
            caps = ET.fromstring(caps_raw)
            require(caps.tag == 'domainCapabilities' and caps.findtext('domain') == 'kvm' and
                    caps.findtext('machine') == machine and caps.findtext('arch') == 'x86_64',
                    'exact KVM machine not advertised')
            report['machine'] = machine
            definition = out / 'fixture.xml'
            definition.write_text(xml_for(identity, name, machine))
            definition.chmod(0o600)
            report.update(fixtureUUID=identity, fixtureName=name, definitionSHA256=sha(definition))
            creation_attempted = True
            native('define', str(definition), '--validate')
            observed = runner.cli('vm', 'show', identity)
            require(observed['state'] == 'stopped' and observed.get('hasManagedSave') is not True,
                    'fixture must be stopped without saved runtime')
            owned(observed['persistentXML'], identity, name)
            runner.save('before-fixture.json', observed)
            initial = runner.cli('vm', 'guest-agent', 'show', identity)
            runner.save('before-channel.json', initial)
            require(initial['present'] is False and initial['canEnable'] is True and initial['addsController'] is True and
                    initial['controllerIndex'] == 0 and initial['port'] == 1, 'fixture channel allocation differs')
            report['tuiBefore'] = tui_inspection(runner, identity, name, False)
            plan = runner.cli('vm', 'guest-agent', 'enable', identity)
            planned_id = plan['planID']
            runner.save('channel-plan.json', plan)
            reread = runner.cli('plan', 'show', planned_id)
            require(plan == reread, 'reviewed immutable plan changed')
            apply = apply_arguments(reread, identity)
            job = runner.cli(*apply)
            allowed_jobs.add(job['operationID'])
            runner.save('channel-job.json', job)
            require(job['planID'] == planned_id and job['state'] == 'succeeded', 'native channel edit did not succeed')
            current = runner.cli('vm', 'show', identity)
            runner.save('after-fixture.json', current)
            require(current['state'] == 'stopped' and current.get('hasManagedSave') is not True, 'edit started/saved the guest')
            report['nativeChannelProof'] = verify_edit(observed['persistentXML'], current['persistentXML'], identity, name)
            final = runner.cli('vm', 'guest-agent', 'show', identity)
            runner.save('after-channel.json', final)
            require(final['present'] is True and final['canEnable'] is False, 'readonly inspection missed configured channel')
            report['tuiAfter'] = tui_inspection(runner, identity, name, True)
            repeated = runner.launch(['--connection', URI, '--output', 'json', '--non-interactive',
                                      'vm', 'guest-agent', 'enable', identity],
                                     stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            stdout, stderr = repeated.communicate(timeout=30)
            require(len(stdout) + len(stderr) <= 1 << 20, 'refusal output exceeds bound')
            refused = strict_json(stdout)
            runner.save('already-configured-refusal.json', refused)
            require(repeated.returncode != 0 and refused.get('error', {}).get('code') == 'ALREADY_CONFIGURED',
                    'repeated enable did not refuse clearly')
            report['repeatEnableRefused'] = True
            report['status'] = 'passed'
        except BaseException as error:
            report['error'] = repr(error)
        finally:
            try:
                # If apply --wait failed after durable acceptance, find that exact
                # plan's operation instead of pretending no operation exists.
                current_jobs = runner.cli('operation', 'list')
                if planned_id is not None:
                    accepted = [j for j in current_jobs if j['planID'] == planned_id]
                    require(len(accepted) <= 1, 'one channel plan accepted multiple jobs')
                    allowed_jobs.update(j['operationID'] for j in accepted)
                    runner.save('accepted-jobs.json', accepted)
                    require(all(j['state'] in ('succeeded', 'failed', 'canceled') for j in accepted),
                            'uncertain or active effect retained; do not undefine this recovery resource')
                if creation_attempted:
                    raw = native('dumpxml', identity, '--inactive', check=False)
                    if raw.returncode == 0:
                        owned(raw.stdout.decode(), identity, name)
                        require(native('domstate', identity).stdout.decode().strip() == 'shut off',
                                'fixture unexpectedly running; no forced stop attempted')
                        native('undefine', identity)
                        report['ownedFixtureUndefined'] = True
                if before is not None:
                    after = inventory(runner.cli('vm', 'list'), URI)
                    runner.save('after-vms.json', after)
                    require(after == before, 'existing VM state/XML changed or fixture remains')
                    report['existingGuestsPreserved'] = True
                if prior_jobs is not None:
                    indexed = {j['operationID']: j for j in current_jobs}
                    require(all(indexed.get(j['operationID']) == j for j in prior_jobs) and
                            set(indexed) - {j['operationID'] for j in prior_jobs} <= allowed_jobs, 'unrelated jobs changed')
                    report['existingJobsPreserved'] = True
                if media is not None:
                    require(media_listing(Path.home() / 'images') == media, 'owner source-media metadata changed')
                    report['ownerMediaMetadataPreserved'] = True
            except BaseException as error:
                report.update(status='failed', preservationOrCleanupError=repr(error))
            runner.save('report.json', report)
            print(json.dumps(report, sort_keys=True))
        return report['status'] != 'passed'
    finally:
        os.close(fd)


class EditProbeTests(unittest.TestCase):
    identity = '570c4866-5b5e-4583-817c-602538d19b9c'
    name = 'virmill-channel-edit-570c4866'

    def test_capability_machine_is_observed_not_guessed(self):
        raw = '''<capabilities><guest><os_type>hvm</os_type><arch name="x86_64"><machine canonical="pc-i440fx-10.1">pc</machine><machine>pc-i440fx-9.2</machine><domain type="kvm"/></arch></guest></capabilities>'''
        self.assertEqual(select_machine(raw), 'pc-i440fx-10.1')
        with self.assertRaises(RuntimeError):
            select_machine(raw.replace('pc-i440fx-', 'pc-q35-'))

    def test_native_normalization_and_unknown_metadata_preservation(self):
        before = xml_for(self.identity, self.name, 'pc-i440fx-10.1')
        after = before.replace('</devices>', '''<controller type="virtio-serial" index="0" model="virtio"><alias name="virtio-serial0"/><address type="pci" domain="0x0000" bus="0x00" slot="0x04" function="0x0"/></controller><channel type="unix"><target type="virtio" name="org.qemu.guest_agent.0"/><address type="virtio-serial" controller="0" bus="0" port="1"/><alias name="channel0"/></channel></devices>''')
        self.assertTrue(verify_edit(before, after, self.identity, self.name)['opaqueConfigurationPreserved'])
        for changed in (after.replace('retain  this opaque value', 'changed'), after.replace('port="1"', 'port="2"'),
                        after.replace('<channel type="unix">', '<channel type="unix"><source path="/external/socket"/>'),
                        after.replace('</devices>', '<controller type="pci" index="2"/></devices>')):
            with self.assertRaises(RuntimeError):
                verify_edit(before, changed, self.identity, self.name)
        with self.assertRaises(RuntimeError):
            owned(before.replace('</devices>', '<disk device="disk"/></devices>'), self.identity, self.name)

    def test_apply_requires_exact_scope_and_acknowledgements(self):
        plan = {'planID': str(uuid.UUID(int=1)), 'planDigest': 'a' * 64, 'operation': 'vm.configure-guest-agent',
                'connectionID': URI, 'actorUID': 1000, 'resourceIDs': ['libvirt|' + URI + '|vm|' + self.identity],
                'acknowledgements': sorted(ACKS), 'review': {'vmID': self.identity, 'requested': {'enableGuestAgent': True,
                'applyMode': 'next-boot'}, 'persistentEdit': True, 'requiresShutdown': True, 'diskDeletion': False}}
        argv = apply_arguments(plan, self.identity)
        self.assertIn('--wait', argv)
        self.assertEqual(argv.count('--ack'), 3)
        plan['acknowledgements'].append('unreviewed-extra-risk')
        with self.assertRaises(RuntimeError):
            apply_arguments(plan, self.identity)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path)
    parser.add_argument('--execute-disposable', action='store_true')
    parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        require(args.root is None and not args.execute_disposable, 'self-test cannot select a native target')
        return not unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(EditProbeTests)).wasSuccessful()
    require(args.execute_disposable and args.root is not None, 'explicit root and disposable execution required')
    return execute(args.root)


if __name__ == '__main__':
    sys.exit(main())
