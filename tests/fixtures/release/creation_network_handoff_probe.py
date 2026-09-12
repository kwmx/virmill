#!/usr/bin/env python3
"""One authorized native 80x24 creation/network handoff; no VM creation or boot.

Requires private staged --root/binaries.json and adjacent tui_workspace_probe,
network_form_tui_probe and guest_tools_fedora_probe. Uses the existing explicitly
owned prepared operation, isolated frontend draft state, and one new lab network
with explicitly reviewed host access Allow. The network remains on success or
failure. Never retries Apply, cleans up, edits existing networks, or changes media.
Draft snapshots prove unfinished NIC choices that Export correctly cannot accept.
This proves native workflow/preservation only, not guest routing or isolation.
All profiles require the authenticated helper, including this allowed-host lab.
Optional --await-helper-approval pauses for at most 120s at the exact preview.
The parent writes helper-approval.json only after separately installing its exact
version1 networks policy grant. The marker grants no authority itself. Setup and
job observation each have a 180s PTY bound; approval waiting is bounded separately.
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
import time
import unittest
import uuid
import xml.etree.ElementTree as ET

from tui_workspace_probe import Runner, Terminal, canonical_path, inventory, media_listing, require, strict_json
from guest_tools_fedora_probe import network_snapshot
from network_form_tui_probe import document, validate_and_normalize

URI = 'qemu:///system'
SOURCE_ID = '7ab996ee-4ed1-4330-9975-4459bf9286c2'
SOURCE_REL = 'virmill-tests/beta-ux-47d1a8b/walkthrough/prepared'
TERMINAL_STATES = {'succeeded', 'failed', 'partial', 'canceled', 'recovery-required'}


def sha(path):
    with Path(path).open('rb') as stream: return hashlib.file_digest(stream, 'sha256').hexdigest()


def selected_source(sources, home):
    require(isinstance(sources, list) and 0 < len(sources) <= 1024, 'bounded prepared source list required')
    matches = [(i, s) for i, s in enumerate(sources) if s.get('operationID') == SOURCE_ID]
    require(len(matches) == 1, 'exact retained preparation operation is unavailable or duplicated')
    i, source = matches[0]
    require(source.get('destination') == str(home / SOURCE_REL) and source.get('name') == 'hardware-fixture',
            'retained preparation source destination/name differs')
    return i, source


def read_draft(path):
    require(stat.S_ISREG(path.lstat().st_mode) and path.stat().st_uid == 1000
            and stat.S_IMODE(path.stat().st_mode) == 0o600, 'private ordinary draft required')
    envelope = strict_json(path.read_bytes())
    require(envelope.get('apiVersion') == 'virmill/v1' and envelope.get('kind') == 'TUIDraft', 'draft envelope differs')
    d = envelope['document']
    require(d.get('connection') == URI and d.get('state') == 'editing' and d.get('creation', {}).get('OperationID') == SOURCE_ID,
            'network action changed the VM setup submission/source binding')
    return d


def require_same_setup(before, after):
    require(before == after, 'network handoff changed saved VM choices or prepared source binding')
    nics = after['creation']['Spec']['nics']
    require(nics and all(n['networkID'] == '' and n['link'] == 'down' for n in nics), 'network selected or cable connected automatically')


def normalize_allow_plan(plan, doc):
    """Validate real v1 marker, then reuse identity normalization on a copy.

    The shared preview helper validates v2 markers only. Projecting the already
    verified digest to v2 lets that helper check all remaining plan fields;
    no projected XML is submitted, and the actual v1 steps/risks stay unchanged.
    """
    p = copy.deepcopy(plan); d = p['review']['definition']
    require(d['hostAccess'] == 'allow' and d['type'] == 'lab', 'only explicit allowed-host lab fixture permitted')
    tree = ET.fromstring(p['review']['networkXML'])
    marker = tree.find('metadata/{urn:virmill:v1}networkCreation')
    require(marker is not None and marker.get('version') == '1', 'allowed-host intent version differs')
    ordered = {key: d[key] for key in ('uuid', 'name', 'bridge', 'type', 'ipv4CIDR', 'dhcpEnabled', 'advertiseDefaultRoute', 'ipv6Mode', 'hostAccess', 'egress')}
    def digest(version):
        return hashlib.sha256(json.dumps({'apiVersion': 'virmill/v1', 'version': version, 'definition': ordered}, separators=(',', ':')).encode()).hexdigest()
    require(marker.get('intent') == digest(1), 'allowed-host XML intent is not bound to reviewed settings')
    marker.set('version', '2'); marker.set('intent', digest(2))
    p['review']['networkXML'] = ET.tostring(tree, encoding='unicode')
    return validate_and_normalize(p, doc)


def approval_request(plan):
    d = plan['review']['definition']
    require(plan['operation'] == 'network.create' and plan['actorUID'] == 1000 and plan['connectionID'] == URI
            and d['type'] == 'lab' and d['hostAccess'] == 'allow', 'helper approval scope differs')
    return {'apiVersion': 'virmill/v1', 'planID': plan['planID'], 'planDigest': plan['planDigest'],
            'actorUID': 1000, 'connectionID': URI, 'networkUUID': d['uuid'],
            'helperOperation': 'network.ipv6-filter', 'policyVersion': 1, 'policyArray': 'networks'}


def validate_approval(value, request):
    require(value == dict(request, approved=True) and value.get('approved') is True,
            'helper approval marker does not match this exact reviewed plan')


def submission_issue(screen):
    # Root renders full wrapped issues in the plan body; old versions still
    # provide the status Error line. A warning or required grant is not refusal.
    lines = screen.splitlines()
    for i, line in enumerate(lines):
        if re.match(r'^\s*(?:Working\.\.\.\s*)?Error:\s*\S', line): return '\n'.join(lines[i:]).strip()
        if re.match(r'^\s*Issue(?:\s*$|:\s*\S)', line): return '\n'.join(lines[i:]).strip()
    return ''


def execute(stage, await_helper_approval=False):
    require(socket.gethostname() in ('virmill-test', 'virmill-test.home') and os.getuid() == os.geteuid() == 1000,
            'wrong authorized host/actor')
    stage = canonical_path(str(stage.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    os.umask(0o077)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    require(all(sha('/usr/bin/' + n) == expected[n] for n in ('virmill', 'virmilld')), 'installed binary manifest differs')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(os.dup(fd), 'rb') as stream:
        require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected['virmill'], 'held executable differs')
    out = stage / 'creation-network-handoff'; out.mkdir(mode=0o700)
    runner = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, fd)
    state = out / 'state'; state.mkdir(mode=0o700); runner.env['XDG_STATE_HOME'] = str(state)
    draft_path = state / 'virmill/tui-drafts/import.json'
    report = {'status': 'failed', 'scope': 'native creation wizard/network job handoff; no VM mutation or packet verification',
              'acceptanceSupport': ['UX-01', 'UX-02', 'NET-01', 'NET-03', 'NET-04', 'NET-06'],
              'binaries': expected, 'fixtureSHA256': sha(__file__), 'applyAttempted': False, 'networkID': None}
    prior_vms = prior_jobs = prior_networks = prior_media = prior_source = baseline_draft = None
    terminal = None; plan = None; allowed_job = None
    source_path = Path.home() / SOURCE_REL
    def virsh(*args):
        result = subprocess.run(['/usr/bin/virsh', '--connect', URI, *args], stdin=subprocess.DEVNULL,
                                capture_output=True, timeout=20, env=dict(os.environ, LC_ALL='C'))
        require(result.returncode == 0 and len(result.stdout) + len(result.stderr) < 2 << 20, 'native network observation failed')
        return result
    try:
        prior_vms = inventory(runner.cli('vm', 'list'), URI)
        prior_jobs = runner.cli('operation', 'list')
        prior_networks = network_snapshot(virsh)
        prior_media = media_listing(Path.home() / 'images'); prior_source = media_listing(source_path)
        runner.save('before.json', {'vms': prior_vms, 'jobs': prior_jobs, 'networks': prior_networks,
                                    'media': prior_media, 'preparedSource': prior_source})
        source_index, source = selected_source(runner.cli('import', 'sources'), Path.home())
        runner.save('prepared-source.json', source)
        runner.save('version.json', runner.cli('version'))
        terminal = Terminal(runner, 'handoff-80x24', 80, 24)
        def wait(label, predicate, key=None):
            return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
        def focused(screen, label):
            return any(re.match(r'^>\s*(?:\[\s*)?' + re.escape(label) + r'(?:[:\s\]]|$)', line) for line in screen.splitlines())
        def focus(label):
            for index in range(24):
                if focused(terminal.screen.text(), label): return
                old = terminal.screen.text(); wait('Focus ' + label + ' ' + str(index), lambda s: s != old, b'\t')
            raise RuntimeError('unreachable form control ' + label)
        def activate(label, predicate):
            focus(label); return wait(label, predicate, b'\r')
        def fill(label, text):
            focus(label); terminal.send(b'\x15')
            while terminal.read(.02) and not terminal.screen.complete(): pass
            wait('Set ' + label, lambda s: text in s, text.encode())
        def choice(label, text):
            focus(label)
            for i in range(32):
                if re.search(re.escape(label) + r':\s*<\s*' + re.escape(text) + r'\s*>', terminal.screen.text()): return
                old = terminal.screen.text(); wait('Choose ' + label + ' ' + str(i), lambda s: s != old, b'\x1b[C')
            raise RuntimeError('observed choice unavailable: ' + label + ' / ' + text)
        def snapshot(label):
            started = time.monotonic(); deadline = started + 8
            value = None
            while time.monotonic() < deadline:
                terminal.read(.1)
                if draft_path.exists():
                    value = read_draft(draft_path)
                    if value['creation']['Page'] == 2 and value['creation']['CPUText'] == '3' and value['creation']['MemoryText'] == '768':
                        if time.monotonic() - started >= 1 and (baseline_draft is None or value == baseline_draft): break
            require(value is not None and value['creation']['Page'] == 2 and value['creation']['CPUText'] == '3'
                    and value['creation']['MemoryText'] == '768', 'edited VM choices not durably saved')
            runner.save(label + '.json', value)
            return value
        def vm_networks(s): return 'Step 3 of 3' in s and 'Network adapters' in s and 'Preview VM creation' in s
        wait('Overview', lambda s: 'Virtual machines' in s)
        wait('VM table', lambda s: 'NAME' in s and 'STATE' in s, b'2')
        for i in range(10):
            if re.search(r'>\[', terminal.screen.text()): break
            old = terminal.screen.text(); wait('VM action focus ' + str(i), lambda s: s != old, b'\t')
        for i in range(12):
            if re.search(r'>\[ Create VM \]', terminal.screen.text()): break
            old = terminal.screen.text(); wait('Create VM action ' + str(i), lambda s: s != old, b'\x1b[C')
        require(re.search(r'>\[ Create VM \]', terminal.screen.text()), 'Create VM action unavailable')
        wait('Prepared source picker', lambda s: 'Choose prepared images' in s and 'Loading' not in s, b'\r')
        for i in range(source_index):
            old = terminal.screen.text(); wait('Prepared source ' + str(i), lambda s: s != old, b'\x1b[B')
        require('> [ hardware-fixture ]' in terminal.screen.text() and 'beta-ux-47d1a8b' in terminal.screen.text(),
                'selected prepared source row/path differs from exact CLI ordering')
        wait('VM basic settings', lambda s: 'CPU cores' in s and 'Storage pool' in s, b'\r')
        fill('CPU cores', '3'); fill('Memory (MiB)', '768')
        choice('Storage pool', 'virmill-test'); choice('Firmware', 'BIOS')
        activate('Continue to disks', lambda s: 'Controller bus' in s and 'Continue to networks' in s)
        saved = None
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            terminal.read(.1)
            if draft_path.exists():
                saved = read_draft(draft_path)
                if saved['creation']['Page'] == 1: break
        require(saved is not None and 1 <= len(saved['creation']['Spec']['disks']) <= 8
                and not saved['creation']['Spec'].get('media'), 'prepared fixture disk/media shape changed')
        original_nics = copy.deepcopy(saved['creation']['Spec']['nics'])
        runner.save('source-original-adapters.json', original_nics)
        for disk in saved['creation']['Spec']['disks']:
            choice('Device', 'Disk: ' + disk['sourceID'])
            choice('Controller bus', 'SATA')
        activate('Continue to networks', vm_networks)
        if not original_nics:
            activate('Add network adapter', lambda s: vm_networks(s) and 'Adapter:' in s)
            report['addedExplicitDisconnectedFixtureAdapter'] = True
        focus('Create network'); baseline_draft = snapshot('setup-before')
        require_same_setup(baseline_draft, baseline_draft)
        require([n['sourceIndex'] for n in baseline_draft['creation']['Spec']['nics'] if n['sourceIndex'] >= 0]
                == [n['sourceIndex'] for n in original_nics], 'original adapters omitted or reordered')
        activate('Create network', lambda s: 'Network name' in s and 'Preview network' in s)
        wait('Cancel restores VM settings', vm_networks, b'\x1b')
        require_same_setup(baseline_draft, snapshot('setup-after-cancel'))
        focus('Refresh networks'); terminal.send(b'\r')
        wait('Refresh preserves VM settings', lambda s: vm_networks(s) and 'Refreshing network choices' not in s)
        require_same_setup(baseline_draft, snapshot('setup-after-refresh'))
        require(runner.cli('operation', 'list') == prior_jobs and network_snapshot(virsh) == prior_networks, 'cancel/refresh mutated host')
        activate('Create network', lambda s: 'Network name' in s and 'Preview network' in s)
        name = 'handoff-lab-' + uuid.uuid4().hex[:12]
        fill('Network name', name); choice('Purpose', 'Isolated lab')
        activate('Advanced options', lambda s: 'Host access' in s and 'Done' in s)
        choice('Host access', 'Allow')
        activate('Done', lambda s: 'Preview network' in s and 'Network name' in s)
        doc = document('lab', name); doc['spec']['hostAccess'] = 'allow'; runner.save('network-document.json', doc)
        activate('Preview network', lambda s: 'Nothing has been applied' in s)
        match = None
        for page in range(16):
            match = re.search(r'Plan ID:\s*([0-9a-f-]{36})', terminal.screen.text())
            if match: break
            old = terminal.screen.text(); wait('Read plan ' + str(page), lambda s: s != old, b'\x1b[6~')
        require(match, 'network plan identity unavailable')
        plan = runner.cli('plan', 'show', match.group(1)); runner.save('tui-plan.json', plan)
        normalized = normalize_allow_plan(plan, doc)
        require(plan.get('requiredGrants') == [{'operation': 'network.create', 'resourceID': r} for r in plan['resourceIDs']],
                'unexpected grants outside the reviewed new network/allocation resources')
        cli_plan = runner.cli('network', 'create', '--input', json.dumps({'document': doc})); runner.save('cli-plan.json', cli_plan)
        require(normalized == normalize_allow_plan(cli_plan, doc), 'CLI/TUI network plan semantics differ')
        identity = plan['review']['definition']['uuid']; require(identity not in prior_networks, 'network is not new')
        report.update(networkID=identity, tuiPlanID=plan['planID'], cliPlanID=cli_plan['planID'], planParity=True)
        require_same_setup(baseline_draft, snapshot('setup-at-network-review'))
        if await_helper_approval:
            request = approval_request(plan)
            runner.save('helper-approval-request.json', request)
            report['waitingForExactHelperApproval'] = True; runner.save('report.json', report)
            approval = out / 'helper-approval.json'
            started = time.monotonic(); original_start = terminal.started
            terminal.started = started  # Explicit separate bounded approval phase.
            try:
                deadline = started + 120
                while time.monotonic() < deadline:
                    terminal.read(.2)
                    if approval.exists():
                        st = approval.lstat()
                        require(stat.S_ISREG(st.st_mode) and st.st_uid in (0, 1000) and st.st_nlink == 1
                                and stat.S_IMODE(st.st_mode) == 0o600 and st.st_size <= 4096,
                                'approval marker must be private ordinary bounded file')
                        validate_approval(strict_json(approval.read_bytes()), request)
                        break
                else:
                    raise RuntimeError('exact helper approval not received within120s; no apply attempted')
            finally:
                terminal.started = original_start + time.monotonic() - started
            report.update(waitingForExactHelperApproval=False, exactHelperApprovalReceived=True)
            runner.save('report.json', report)
        wait('Confirm reviewed network changes', lambda s: 'Confirm reviewed changes' in s and plan['planID'] in s, b'\r')
        for ack in plan['acknowledgements']:
            require(re.search(r'>\s*\[ \]', terminal.screen.text()), 'unchecked acknowledgement not focused')
            wait('Acknowledge ' + ack, lambda s: re.search(r'>\s*\[x\]', s), b' ')
            old = terminal.screen.text(); wait('Next acknowledgement', lambda s: s != old, b'\t')
        focus('Apply reviewed plan')
        report['applyAttempted'] = True; runner.save('report.json', report)
        terminal.started = time.monotonic()  # Separate bounded native job phase.
        terminal.send(b'\r')  # Exactly once. Never replay after a timeout/error.
        deadline = time.monotonic() + 175; job = None; next_poll = 0
        while time.monotonic() < deadline:
            terminal.read(.1)
            if time.monotonic() < next_poll: continue
            found = [j for j in runner.cli('operation', 'list') if j['planID'] == plan['planID']]
            next_poll = time.monotonic() + 1
            require(len(found) <= 1, 'more than one job accepted for this network plan')
            if found:
                job = found[0]; allowed_job = job['operationID']
                if job['state'] in TERMINAL_STATES: break
            elif terminal.screen.complete() and submission_issue(terminal.screen.text()):
                failure = {'planID': plan['planID'], 'planDigest': plan['planDigest'], 'matchingJobObserved': False,
                           'screen80x24': terminal.screen.text()}
                runner.save('submission-issue.json', failure); runner.save('network-job.json', None)
                report.update(submissionIssueObserved=True, noMatchingJobAtIssue=True)
                runner.save('report.json', report)
                # Capture a diagnostic width as well as the actual80x24 failure
                # so older one-line error renderers do not hide recovery text.
                update = terminal.resize(512, 32)
                terminal.wait('Full submission issue (diagnostic512x32)', lambda s: bool(submission_issue(s)), update)
                failure['diagnostic512x32'] = terminal.screen.text()
                runner.save('submission-issue.json', failure)
                raise RuntimeError('TUI reported a submission issue before any matching job was observed; see submission-issue.json; apply was not retried')
        runner.save('network-job.json', job)
        require(job is not None and job['state'] == 'succeeded', 'network job not durably successful; fixture retained')
        wait('Network completion returns to VM setup', vm_networks)
        require_same_setup(baseline_draft, snapshot('setup-after-job'))
        new_inventory = runner.cli('network', 'list'); runner.save('after-network-inventory.json', new_inventory)
        new = [n for n in new_inventory if n['key']['resourceUUID'] == identity]
        require(len(new) == 1 and new[0]['active'] and not new[0]['autostart'], 'new reviewed network not active without autostart')
        focus('Network')
        require(re.search(r'Network:\s*< Choose… >', terminal.screen.text()), 'new network selected automatically')
        # Explicitly cycle to prove this fresh observed option is reachable,
        # then stop without previewing/creating a VM. Baseline before this edit
        # establishes that neither network selection nor cable-up was automatic.
        choice('Network', new[0]['name'])
        focus('Cable'); require('< Disconnected >' in terminal.screen.text(), 'explicit selection connected adapter cable')
        report.update(status='passed', operationID=allowed_job, canceledChoicesPreserved=True, refreshedChoicesPreserved=True,
                      completedJobReturnedToWizard=True, noAutomaticSelectionOrCableConnection=True, newNetworkReachable=True)
    except BaseException as error:
        report['error'] = repr(error)
    finally:
        if terminal is not None: terminal.close()
        try:
            if prior_vms is not None: require(inventory(runner.cli('vm', 'list'), URI) == prior_vms, 'existing VM inventory changed')
            if prior_jobs is not None:
                current_jobs = runner.cli('operation', 'list')
                if plan is not None:
                    found = [j for j in current_jobs if j['planID'] == plan['planID']]
                    require(len(found) <= 1, 'duplicate submitted job')
                    if found: allowed_job = found[0]['operationID']; report['operationID'] = allowed_job
                require([j for j in current_jobs if j['operationID'] != allowed_job] == prior_jobs, 'unrelated jobs changed')
            if prior_networks is not None:
                current = network_snapshot(virsh); runner.save('after-networks.json', current)
                require({k: v for k, v in current.items() if k != report['networkID']} == prior_networks, 'existing native networks changed')
            if prior_media is not None: require(media_listing(Path.home() / 'images') == prior_media, 'owner media metadata changed')
            if prior_source is not None: require(media_listing(source_path) == prior_source, 'prepared source metadata changed')
            report['unrelatedGuestsJobsNetworksAndMediaPreserved'] = True
        except BaseException as error: report.update(status='failed', preservationError=repr(error))
        runner.save('report.json', report); os.close(fd); print(json.dumps(report, sort_keys=True))
    return 0 if report['status'] == 'passed' else 1


class Tests(unittest.TestCase):
    def test_exact_helper_marker_refuses_other_plan_or_profile(self):
        plan = {'operation': 'network.create', 'actorUID': 1000, 'connectionID': URI, 'planID': str(uuid.uuid4()),
                'planDigest': 'a' * 64, 'review': {'definition': {'type': 'lab', 'hostAccess': 'allow', 'uuid': str(uuid.uuid4())}}}
        request = approval_request(plan); validate_approval(dict(request, approved=True), request)
        for key, value in [('planID', str(uuid.uuid4())), ('planDigest', 'b' * 64), ('actorUID', 0),
                           ('networkUUID', str(uuid.uuid4())), ('policyArray', 'protectedNetworks'), ('approved', False)]:
            marker = dict(request, approved=True); marker[key] = value
            with self.assertRaises(RuntimeError): validate_approval(marker, request)
        plan['review']['definition']['hostAccess'] = 'services-only'
        with self.assertRaises(RuntimeError): approval_request(plan)

    def test_submission_issue_detection_not_review_warning(self):
        for text in ('Confirm reviewed changes\nError: PERMISSION_DENIED: key unavailable',
                     'Issue\nPERMISSION_DENIED: helper key\nAsk an administrator to prepare it.'):
            self.assertTrue(submission_issue(text))
        for text in ('Required grants\nnetwork.ipv6-filter\nConfirm reviewed changes', 'Working... Creating network',
                     'Warning: packet verification not run'):
            self.assertFalse(submission_issue(text))

    def test_allowed_host_v1_marker_and_plan_normalization(self):
        from network_form_tui_probe import Tests as SharedTests
        def sample(identity):
            p, _ = SharedTests().sample(identity)
            doc = document('lab', 'fixture'); doc['spec']['hostAccess'] = 'allow'
            d = p['review']['definition']
            d.update(type='lab', hostAccess='allow', ipv4CIDR='192.168.123.0/24', dhcpEnabled=True)
            p['review']['allocation'] = {'requestedCIDR': 'auto'}
            tree = ET.fromstring(p['review']['networkXML']); marker = tree.find('metadata/{urn:virmill:v1}networkCreation')
            marker.set('version', '1')
            marker.set('intent', hashlib.sha256(json.dumps({'apiVersion': 'virmill/v1', 'version': 1, 'definition': d}, separators=(',', ':')).encode()).hexdigest())
            p['review']['networkXML'] = ET.tostring(tree, encoding='unicode')
            return p, doc
        p, doc = sample('12345678-1234-4234-8234-123456789abc')
        other, _ = sample('22345678-1234-4234-8234-123456789abc')
        self.assertEqual(normalize_allow_plan(p, doc), normalize_allow_plan(other, doc))
        bad = copy.deepcopy(p); bad['review']['networkXML'] = bad['review']['networkXML'].replace('version="1"', 'version="2"')
        with self.assertRaises(RuntimeError): normalize_allow_plan(bad, doc)
        bad = copy.deepcopy(p); bad['review']['definition']['hostAccess'] = 'services-only'
        with self.assertRaises(RuntimeError): normalize_allow_plan(bad, doc)
        bad = copy.deepcopy(other); bad['steps'] = ['different operation']
        self.assertNotEqual(normalize_allow_plan(p, doc), normalize_allow_plan(bad, doc))

    def test_preparation_identity_not_display_name_alone(self):
        home = Path('/home/virmill-test')
        wanted = {'operationID': SOURCE_ID, 'name': 'hardware-fixture', 'destination': str(home / SOURCE_REL)}
        self.assertEqual(selected_source([dict(wanted, operationID=str(uuid.uuid4())), wanted], home)[0], 1)
        for bad in ([wanted, wanted], [dict(wanted, destination='/foreign')], [dict(wanted, name='foreign')]):
            with self.assertRaises(RuntimeError): selected_source(bad, home)

    def test_exact_setup_preservation_and_disconnected_unselected_nics(self):
        before = {'creation': {'Spec': {'nics': [{'networkID': '', 'link': 'down', 'sourceIndex': 0}], 'cpu': {'mode': 'custom'}}, 'CPUText': '3'}}
        require_same_setup(before, copy.deepcopy(before))
        for field, value in [('networkID', str(uuid.uuid4())), ('link', 'up')]:
            bad = copy.deepcopy(before); bad['creation']['Spec']['nics'][0][field] = value
            with self.assertRaises(RuntimeError): require_same_setup(before, bad)
            with self.assertRaises(RuntimeError): require_same_setup(bad, bad)
        bad = copy.deepcopy(before); bad['creation']['CPUText'] = '2'
        with self.assertRaises(RuntimeError): require_same_setup(before, bad)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path); parser.add_argument('--execute-disposable', action='store_true'); parser.add_argument('--self-test', action='store_true')
    parser.add_argument('--await-helper-approval', action='store_true', help='Wait up to120s for an exact helper-approval.json marker after parent-managed policy setup')
    args = parser.parse_args()
    if args.self_test:
        unittest.main(argv=[__file__], exit=False).result.wasSuccessful() or exit(1)
    else:
        require(args.execute_disposable and args.root is not None, '--execute-disposable and --root required')
        raise SystemExit(execute(args.root, args.await_helper_approval))
