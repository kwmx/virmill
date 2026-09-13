#!/usr/bin/env python3
"""Read-only native TUI network-form previews, with CLI/service equivalence.

Only durable plan previews are created. Never applies a plan, installs grants,
creates/starts a network, changes helper policy, or starts a guest. Requires the
authorized UID1000 host, a private staged --root/binaries.json, and adjacent
tui_workspace_probe.py plus guest_tools_fedora_probe.py. All three supported
profiles are exercised at 80x24: form → full plan → Back with values retained →
cancel. CLI inline declarations must produce equivalent plans after validating
and removing only generated identity/time/digest fields. Existing VM/network/
job inventories and owner media metadata remain unchanged. These observations
support UX and planning only, not routing, DNS, packet isolation or acceptance.
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
import unittest
import uuid
import xml.etree.ElementTree as ET
from tui_workspace_probe import Runner, Terminal, canonical_path, inventory, media_listing, require, strict_json
from guest_tools_fedora_probe import network_snapshot


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


URI = 'qemu:///system'
ACKS = {'host-mutation', 'network-host-access', 'network-firewall', 'exclusive-network-writer'}
PURPOSES = {'nat': 'NAT internet', 'lab': 'Isolated lab', 'guest-only': 'Guest-only'}


def sha(path):
    with Path(path).open('rb') as stream: return hashlib.file_digest(stream, 'sha256').hexdigest()


def document(mode, name):
    require(mode in PURPOSES and re.fullmatch('[a-z][a-z0-9-]{0,62}', name), 'invalid fixture declaration')
    spec = {'type': mode, 'hostAccess': 'deny' if mode == 'guest-only' else 'services-only',
            'egress': 'any' if mode == 'nat' else 'none', 'ipv6': {'mode': 'disabled'}}
    if mode != 'guest-only': spec['ipv4'] = {'cidr': 'auto', 'dhcp': {'enabled': True, 'advertiseDefaultRoute': mode == 'nat'}}
    return {'apiVersion': 'virmill/v1', 'kind': 'Network', 'metadata': {'name': name}, 'spec': spec}


def validate_and_normalize(plan, doc):
    p = copy.deepcopy(plan); review = p['review']; d = review['definition']; wanted = doc['spec']
    identity = d['uuid']; require(str(uuid.UUID(identity)) == identity and uuid.UUID(identity).int, 'invalid generated network UUID')
    require(p['operation'] == 'network.create' and p['actorUID'] == 1000 and p['connectionID'] == URI and p['apiVersion'] == 'virmill/v1', 'plan scope differs')
    require(d['name'] == 'virmill-' + identity and d['bridge'] == 'vm' + identity.replace('-', '')[:12], 'generated network names not bound to UUID')
    require(set(p['resourceIDs']) == {'libvirt|' + URI + '|network|' + identity, 'libvirt|' + URI + '|network-allocation|host'}
            and len(p['resourceIDs']) == 2, 'unexpected network resources')
    require(set(p['acknowledgements']) == ACKS and len(p['acknowledgements']) == len(ACKS), 'unexpected network grants/risks')
    require(review['metadata'] == doc['metadata'] and review['autostart'] is False and review['packetVerification'] == 'not-run'
            and review['guestRoutingVerified'] is False and review['physicalUplinkChanges'] is False and review['resumeFrom'] == '', 'plan invents completion or changes source metadata')
    require(d['type'] == wanted['type'] and d['hostAccess'] == wanted['hostAccess'] and d['egress'] == wanted['egress']
            and d['ipv6Mode'] == 'disabled' and d['dhcpEnabled'] == (wanted['type'] != 'guest-only')
            and d['advertiseDefaultRoute'] == (wanted['type'] == 'nat'), 'mode, DHCP, route, host access or IPv6 differs')
    if wanted['type'] == 'guest-only':
        require(d['ipv4CIDR'] == '' and review['allocation'] is None, 'guest-only invented host L3/subnet')
    else:
        require(d['ipv4CIDR'] not in ('', 'auto') and review['allocation']['requestedCIDR'] == 'auto', 'automatic subnet was not reviewed')
    tree = ET.fromstring(review['networkXML'])
    require(tree.findtext('uuid') == identity and tree.findtext('name') == d['name'] and tree.find('bridge').get('name') == d['bridge'], 'XML network identity differs')
    marker = tree.find('metadata/{urn:virmill:v1}networkCreation')
    require(marker is not None and marker.get('apiVersion') == 'virmill/v1' and marker.get('version') == '2', 'protected intent marker missing')
    # Match the documented Go struct order when validating the generated intent
    # digest. The ASCII-only definition is serialized without HTML escape cases.
    ordered = {key: d[key] for key in ('uuid', 'name', 'bridge', 'type', 'ipv4CIDR', 'dhcpEnabled', 'advertiseDefaultRoute', 'ipv6Mode', 'hostAccess', 'egress')}
    intent = {'apiVersion': 'virmill/v1', 'version': 2, 'definition': ordered}
    expected = hashlib.sha256(json.dumps(intent, separators=(',', ':')).encode()).hexdigest()
    require(marker.get('intent') == expected, 'XML intent digest is not bound to full reviewed definition')
    marker.set('intent', '<generated-intent>')
    review['networkXML'] = ET.tostring(tree, encoding='unicode')
    for key in ('planID', 'planDigest', 'inputDigest', 'createdAt', 'expiresAt'):
        require(key in p, 'plan identity field missing'); del p[key]
    replacements = [(d['name'], '<generated-network-name>'), (d['bridge'], '<generated-bridge>'), (identity, '<generated-network-uuid>')]
    def normalize(value):
        if isinstance(value, str):
            for before, after in replacements: value = value.replace(before, after)
            return value
        if isinstance(value, list): return [normalize(item) for item in value]
        if isinstance(value, dict): return {key: normalize(item) for key, item in value.items()}
        return value
    return normalize(p)


def execute(stage):
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(stage.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    os.umask(0o077); expected = strict_json((stage / 'binaries.json').read_bytes())
    require(all(sha('/usr/bin/' + name) == expected[name] for name in ('virmill', 'virmilld')), 'installed binary manifest differs')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(os.dup(fd), 'rb') as f: require(hashlib.file_digest(f, 'sha256').hexdigest() == expected['virmill'], 'held executable differs')
    out = stage / 'network-form-preview'; out.mkdir(mode=0o700)
    runner = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, fd)
    report = {'status': 'failed', 'scope': 'native read-only observations and CLI/TUI previews; no network creation or packet test',
              'acceptanceSupport': ['NET-01', 'NET-03', 'NET-04', 'NET-06', 'UX-01', 'UX-02'], 'binaries': expected,
              'plansApplied': 0, 'profiles': {}}
    before = jobs = networks = media = network_inventory = None; terminal = None
    logs = []
    def virsh(*args):
        result = subprocess.run(['/usr/bin/virsh', '--connect', URI, *args], stdin=subprocess.DEVNULL, capture_output=True, timeout=20,
                                env=dict(os.environ, LC_ALL='C'))
        require(result.returncode == 0 and len(result.stdout) + len(result.stderr) < 2 << 20, 'read-only native network observation failed')
        logs.append({'argv': list(args), 'stdoutSHA256': hashlib.sha256(result.stdout).hexdigest()}); runner.save('native-observations.json', logs)
        return result
    try:
        before = inventory(runner.cli('vm', 'list'), URI); jobs = runner.cli('operation', 'list')
        networks = network_snapshot(virsh); network_inventory = runner.cli('network', 'list'); media = media_listing(Path.home() / 'images')
        runner.save('before-vms.json', before); runner.save('before-jobs.json', jobs); runner.save('before-networks.json', networks)
        terminal = Terminal(runner, 'network-form-80x24', 80, 24)
        def wait(label, predicate, key=None): return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
        def focused(screen, label):
            return any(re.match(r'^>\s*(?:\[\s*)?' + re.escape(label) + r'(?:[:\s\]]|$)', line) for line in screen.splitlines())
        def focus(label):
            for index in range(14):
                if focused(terminal.screen.text(), label): return
                old = terminal.screen.text(); wait('Focus ' + label + ' ' + str(index), lambda screen: screen != old, b'\t')
            raise RuntimeError('unreachable form control ' + label)
        wait('Overview', lambda screen: 'Virtual machines' in screen)
        wait('Networks page', lambda screen: 'Networks' in screen.splitlines()[0] and 'Create network' in screen, b'3')
        for mode, label in PURPOSES.items():
            for index in range(8):
                if re.search(r'>\[', terminal.screen.text()): break
                old = terminal.screen.text(); wait('Focus network actions ' + str(index), lambda screen: screen != old, b'\t')
            for index in range(8):
                if re.search(r'>\[ Create network \]', terminal.screen.text()): break
                old = terminal.screen.text(); wait('Select Create network ' + str(index), lambda screen: screen != old, b'\x1b[C')
            require(re.search(r'>\[ Create network \]', terminal.screen.text()), 'Create network action missing')
            wait('Guided network form', lambda screen: 'Network name' in screen and 'Purpose' in screen and 'Preview network' in screen, b'\r')
            name = 'preview-' + mode + '-' + hashlib.sha256(str(stage).encode()).hexdigest()[:8]
            doc = document(mode, name); runner.save(mode + '-document.json', doc)
            focus('Network name'); terminal.send(b'\x15')
            while terminal.read(.05) and not terminal.screen.complete(): pass
            wait('Set network name', lambda screen: name in screen, name.encode())
            focus('Purpose')
            for index in range(4):
                if '< ' + label + ' >' in terminal.screen.text(): break
                old = terminal.screen.text(); wait('Choose ' + label + ' ' + str(index), lambda screen: screen != old, b'\x1b[C')
            require('< ' + label + ' >' in terminal.screen.text(), 'purpose choice differs')
            form_screen = terminal.screen.text()
            focus('Preview network')
            wait('Network plan preview', lambda screen: 'Nothing has been applied' in screen, b'\r')
            match = None
            for page in range(16):
                match = re.search(r'Plan ID:\s*([0-9a-f-]{36})', terminal.screen.text())
                if match: break
                old = terminal.screen.text(); wait('Read full plan identity ' + str(page), lambda screen: screen != old, b'\x1b[6~')
            require(match, 'network plan identity missing')
            tui_plan = runner.cli('plan', 'show', match.group(1)); runner.save(mode + '-tui-plan.json', tui_plan)
            normalized = validate_and_normalize(tui_plan, doc)
            cli_plan = runner.cli('network', 'create', '--input', json.dumps({'document': doc})); runner.save(mode + '-cli-plan.json', cli_plan)
            require(normalized == validate_and_normalize(cli_plan, doc), 'CLI/TUI network plan semantics differ')
            wait('Back retains network settings', lambda screen: name in screen and '< ' + label + ' >' in screen
                 and 'Preview network' in screen and 'Nothing has been applied' not in screen, b'\x1b')
            require(('auto' in form_screen) == ('auto' in terminal.screen.text()), 'back changed automatic subnet choice')
            wait('Cancel returns to Networks', lambda screen: 'Networks' in screen.splitlines()[0] and 'Create network' in screen
                 and 'Network name' not in screen, b'\x1b')
            report['profiles'][mode] = {'tuiPlanID': tui_plan['planID'], 'cliPlanID': cli_plan['planID'],
                                        'semanticParity': True, 'backRetainedSettings': True, 'canceledWithoutApply': True}
            require(runner.cli('operation', 'list') == jobs and network_snapshot(virsh) == networks, 'preview mutated jobs or native networks')
        # Session management is not silently redirected to system management.
        process = runner.launch(['--connection', 'qemu:///session', '--output', 'json', '--non-interactive',
                                 'network', 'create', '--input', json.dumps({'document': document('nat', 'preview-session-refusal')})],
                                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        stdout, stderr = process.communicate(timeout=30)
        require(len(stdout) + len(stderr) <= 1 << 20, 'session refusal output bound')
        envelope = strict_json(stdout); runner.save('session-refusal.json', envelope)
        require(process.returncode != 0 and envelope.get('error', {}).get('code') in ('INVALID_INPUT', 'UNSUPPORTED_CAPABILITY'), 'unsupported session did not refuse')
        report.update(status='passed', unsupportedSessionRefused=True, noApplyOrHelperApprovalAttempted=True)
    except BaseException as error: report['error'] = repr(error)
    finally:
        if terminal is not None: terminal.close()
        try:
            if before is not None: require(inventory(runner.cli('vm', 'list'), URI) == before, 'VM inventory changed')
            if jobs is not None: require(runner.cli('operation', 'list') == jobs, 'operation journal changed')
            if networks is not None: require(network_snapshot(virsh) == networks and runner.cli('network', 'list') == network_inventory, 'network definitions/state changed')
            if media is not None: require(media_listing(Path.home() / 'images') == media, 'source media metadata changed')
            report['guestsJobsNetworksAndSourceMetadataPreserved'] = True
        except BaseException as error: report.update(status='failed', preservationError=repr(error))
        runner.save('report.json', report); os.close(fd); print(json.dumps(report, sort_keys=True))
    return 0 if report['status'] == 'passed' else 1


class Tests(unittest.TestCase):
    def sample(self, identity='12345678-1234-4234-8234-123456789abc'):
        doc = document('guest-only', 'fixture')
        d = {'uuid': identity, 'name': 'virmill-' + identity, 'bridge': 'vm' + identity.replace('-', '')[:12], 'type': 'guest-only',
             'ipv4CIDR': '', 'dhcpEnabled': False, 'advertiseDefaultRoute': False, 'ipv6Mode': 'disabled', 'hostAccess': 'deny', 'egress': 'none'}
        digest = hashlib.sha256(json.dumps({'apiVersion': 'virmill/v1', 'version': 2, 'definition': d}, separators=(',', ':')).encode()).hexdigest()
        xml = '<network><name>' + d['name'] + '</name><uuid>' + identity + '</uuid><bridge name="' + d['bridge'] + '"/><metadata><v:networkCreation xmlns:v="urn:virmill:v1" apiVersion="virmill/v1" version="2" intent="' + digest + '"/></metadata></network>'
        p = {'apiVersion': 'virmill/v1', 'operation': 'network.create', 'actorUID': 1000, 'connectionID': URI,
             'resourceIDs': ['libvirt|' + URI + '|network|' + identity, 'libvirt|' + URI + '|network-allocation|host'], 'acknowledgements': sorted(ACKS),
             'planID': identity, 'planDigest': 'a' * 64, 'inputDigest': 'b' * 64, 'createdAt': 'one', 'expiresAt': 'two',
             'review': {'definition': d, 'metadata': doc['metadata'], 'networkXML': xml, 'autostart': False, 'packetVerification': 'not-run',
                        'guestRoutingVerified': False, 'physicalUplinkChanges': False, 'resumeFrom': '', 'allocation': None},
             'steps': [{'action': 'network.define'}], 'requiredGrants': [], 'risks': ['isolation unverified'], 'beforeFingerprints': None}
        return p, doc
    def test_generated_identity_only_normalization(self):
        a, doc = self.sample(); b, _ = self.sample('22345678-1234-4234-8234-123456789abc')
        self.assertEqual(validate_and_normalize(a, doc), validate_and_normalize(b, doc))
        for field in ('steps', 'requiredGrants', 'risks'):
            changed = copy.deepcopy(b); changed[field] = ['changed']
            self.assertNotEqual(validate_and_normalize(a, doc), validate_and_normalize(changed, doc))
    def test_scope_changes_and_unbound_xml_refused(self):
        p, doc = self.sample()
        for field, value in [('hostAccess', 'allow'), ('advertiseDefaultRoute', True), ('ipv4CIDR', '192.168.1.0/24')]:
            bad = copy.deepcopy(p); bad['review']['definition'][field] = value
            with self.assertRaises(RuntimeError): validate_and_normalize(bad, doc)
        bad = copy.deepcopy(p); bad['review']['networkXML'] = bad['review']['networkXML'].replace('intent="', 'intent="bad')
        with self.assertRaises(RuntimeError): validate_and_normalize(bad, doc)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__); parser.add_argument('--root', type=Path); parser.add_argument('--execute-disposable', action='store_true'); parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        require(args.root is None and not args.execute_disposable, 'self-test cannot select native actions'); unittest.main(argv=[sys.argv[0]])
    else:
        require(args.root is not None and args.execute_disposable, 'explicit disposable root/run required'); sys.exit(execute(args.root))
