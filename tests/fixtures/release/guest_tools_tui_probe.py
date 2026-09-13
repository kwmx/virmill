#!/usr/bin/env python3
"""Actual TUI guest-tools approval on the previously qualified Fedora fixture.

Parent-operated only: --execute-disposable --root PRIVATE_NEW_STAGED_DIRECTORY.
Requires adjacent guest_tools_fedora_probe.py and tui_workspace_probe.py, current
binaries.json and the retained original successful wizard-drafts-tools-f371fa8
fixture. Starts only the exact owned stopped UUID below. Uses the previously
verified address and key/trust file references, validates original/new service
hashes, submits through the real 80x24 TUI, and requires already-configured,
skip-apply and verified stage receipts plus a native guest ping. Never reads or
prints private key contents. No guessed address, keyscan, installation bootstrap,
new guest/media, or forced stop. Failed/uncertain jobs retain the guest for review.
Success restores all original guest states and preserves source media/jobs.
--self-test tests pure review/result predicates, without SSH or native actions.
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
from tui_workspace_probe import Runner, Terminal, canonical_path, inventory, media_listing, require, strict_json
from guest_tools_fedora_probe import URI, NETWORK, SOURCE, COPY, SOURCE_SHA, inspect_owned_domain, network_snapshot


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


IDENTITY = '71001e0c-e99d-4e26-a886-0553533c5fa0'
NAME = 'virmill-tools-71001e0c'
MAC = '52:54:00:9d:fd:2f'
ACKS = {'guest-execution', 'guest-host-key-binding', 'guest-admin-package-install'}
TERMINAL = {'succeeded', 'failed', 'partial', 'canceled', 'recovery-required'}


def sha(path):
    with Path(path).open('rb') as stream: return hashlib.file_digest(stream, 'sha256').hexdigest()



def select_owned_vm(terminal):
    def wait(label, predicate, key=None): return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
    wait('Overview', lambda screen: 'Virtual machines' in screen)
    wait('VM table', lambda screen: 'NAME' in screen and 'STATE' in screen, b'2')
    wait('VM filter focus', lambda screen: 'Enter Keep filter' in screen, b'/')
    wait('Owned Fedora selected', lambda screen: NAME in screen and '1 of 1 selected' in screen, NAME.encode())
    wait('Keep filter', lambda screen: 'Enter Keep filter' not in screen, b'\r')
    wait('Owned Fedora details', lambda screen: 'VM details' in screen and NAME in screen and IDENTITY in screen, b'\r')


def stopped_tools_guidance(screen):
    return ('This VM is stopped. Start it before installing guest tools.' in screen and 'Preview start VM' in screen
            and all(field not in screen for field in ('Guest IP address', 'Guest SSH user', 'SSH key', 'Verified host keys', 'Preview installation')))


def completed_guest_job(screen):
    return 'Job / Guest setup completed' in screen and 'Status: succeeded' in screen and '[ Open VM ]' in screen


def check_review(plan, original):
    r, old = plan['review'], original['review']
    require(plan['operation'] == 'guest.recipe.run' and plan['actorUID'] == 1000 and plan['connectionID'] == URI,
            'guest recipe plan operation/actor differs')
    resource = 'libvirt|' + URI + '|vm|' + IDENTITY
    require(set(plan['resourceIDs']) == {resource, 'guest-setup:' + resource}
            and len(plan['resourceIDs']) == 2 and r['resource']['resourceUUID'] == IDENTITY, 'guest target differs')
    require(len(plan['acknowledgements']) == len(ACKS) and set(plan['acknowledgements']) == ACKS, 'unreviewed guest privileges')
    for key in ('address', 'port', 'user', 'identitySHA256', 'knownHostsSHA256', 'recipeSHA256', 'scriptSHA256', 'arguments', 'reboot'):
        require(r[key] == old[key], 'new TUI review differs from verified original ' + key)
    require(r['recipe']['metadata']['name'] == 'virmill-guest-tools-fedora' and r['user'] == 'virmillprobe'
            and r['port'] == 22 and r['arguments'] == [] and r['reboot'] == 'never'
            and not r['nativeAddressBindingVerified'] and not r['rawOutputRetained'], 'unexpected recipe scope or stronger claim')


def check_result(result, plan_id=None):
    require(result['complete'] and result['state'] == 'succeeded' and result['resource']['resourceUUID'] == IDENTITY
            and not result['nativeAddressBindingVerified'] and not result['rawOutputRetained'], 'recipe result scope/completion differs')
    if plan_id is not None: require(result['planID'] == plan_id, 'recipe result belongs to another plan')
    stages = {stage['stage']: stage for stage in result['stages']}
    require(len(stages) == len(result['stages']) == 4 and set(stages) == {'readiness', 'check', 'apply', 'verify'}, 'unexpected recipe stage set')
    for name, outcome in [('readiness', 'ready'), ('check', 'already-configured'), ('apply', 'skip-apply'), ('verify', 'verified')]:
        stage = stages[name]
        require(stage['complete'] and not stage['effectUnknown'] and stage['receipt']['outcome'] == outcome,
                'expected ' + name + ' outcome missing')
        require(stage['intent']['runPlanned'] == (name != 'apply'), 'idempotent run unexpectedly executed package mutation')
    require(stages['readiness']['receipt']['uid'] > 0 and stages['apply']['receipt']['transportCompleted'] is False,
            'guest actor or skipped transport differs')


def execute(stage):
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000,
            'wrong authorized host/actor')
    stage = canonical_path(str(stage.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged directory required')
    os.umask(0o077)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    require(all(sha('/usr/bin/' + name) == expected[name] for name in ('virmill', 'virmilld')), 'installed binary manifest differs')
    prior_dir = Path.home() / 'virmill-tests/wizard-drafts-tools-f371fa8/guest-tools-fedora'
    require(prior_dir == canonical_path(str(prior_dir)), 'original fixture directory differs')
    prior = strict_json((prior_dir / 'report.json').read_bytes())
    original_result = strict_json((prior_dir / 'repeat-result.json').read_bytes())
    original_plan = strict_json((prior_dir / 'repeat-plan.json').read_bytes())
    require(prior['status'] == 'passed' and prior['vmID'] == IDENTITY and prior['name'] == NAME and prior['mac'] == MAC
            and prior['ownedFixtureStopped'] and prior['nativeGuestPing'] and prior['sourcesAndNetworksPreserved']
            and prior['sourceSHA256'] == SOURCE_SHA, 'prior fixture success/ownership differs')
    check_result(original_result); check_review(original_plan, original_plan)
    require(original_result['planID'] == original_plan['planID'] and prior['address'] == original_plan['review']['address'], 'prior address/result binding differs')
    address = prior['address']; identity_file = prior_dir / 'client'; trust_file = prior_dir / 'known_hosts'
    for path in (identity_file, trust_file):
        require(path == canonical_path(str(path)) and stat.S_ISREG(path.lstat().st_mode) and path.stat().st_uid == 1000
                and stat.S_IMODE(path.stat().st_mode) == 0o600, 'original private credential reference unsafe')
    reference_stats = {str(p): [p.stat().st_ino, p.stat().st_size, p.stat().st_mtime_ns, p.stat().st_ctime_ns] for p in (identity_file, trust_file)}
    prior_report_sha = sha(prior_dir / 'report.json')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(os.dup(fd), 'rb') as stream: require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected['virmill'], 'held executable differs')
    out = stage / 'guest-tools-tui'; out.mkdir(mode=0o700)
    runner = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, fd)
    report = {'status': 'failed', 'scope': 'real TUI guest tools approval, idempotent verify and native guest ping; no new package installation',
              'acceptanceSupport': ['GUEST-02', 'GUEST-03', 'UX-01', 'UX-02'], 'vmID': IDENTITY,
              'binaries': expected, 'originalReportSHA256': prior_report_sha, 'addressSource': 'previous verified fixture plan and exact MAC DHCP lease'}
    logs, plans, allowed_jobs = [], set(), set()
    before = prior_jobs = media = networks = None; started = False; terminal = None
    def native(*argv, check=True):
        result = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, timeout=20, env=dict(os.environ, LC_ALL='C'))
        require(len(result.stdout) + len(result.stderr) < 2 << 20, 'native output bound')
        logs.append({'argv': list(argv), 'exitCode': result.returncode, 'stdout': result.stdout.decode(errors='replace'), 'stderr': result.stderr.decode(errors='replace')})
        runner.save('native-commands.json', logs)
        require(not check or result.returncode == 0, 'native observation failed')
        return result
    def virsh(*args, **kwargs): return native('/usr/bin/virsh', '--connect', URI, *args, **kwargs)
    def lifecycle(action):
        plan = runner.cli('vm', action, IDENTITY)
        require(plan['operation'] == 'vm.' + action and plan['connectionID'] == URI and plan['actorUID'] == 1000
                and plan['resourceIDs'] == ['libvirt|' + URI + '|vm|' + IDENTITY] and plan['review']['vmID'] == IDENTITY
                and plan['acknowledgements'] == ['host-mutation'], 'lifecycle scope differs')
        plans.add(plan['planID']); runner.save(action + '-plan.json', plan)
        job = runner.cli('plan', 'apply', plan['planID'], '--digest', plan['planDigest'], '--detach', '--ack', 'host-mutation',
                         '--idempotency-key', stage.name + '-tools-tui-' + action)
        allowed_jobs.add(job['operationID']); runner.save(action + '-accepted.json', job)
        report[action + 'OperationID'] = job['operationID']; deadline = time.monotonic() + 90
        while job['state'] not in TERMINAL and time.monotonic() < deadline:
            time.sleep(.4); job = runner.cli('operation', 'show', job['operationID'])
        runner.save(action + '-job.json', job); require(job['state'] == 'succeeded', action + ' did not complete; no retry')
    try:
        before = inventory(runner.cli('vm', 'list'), URI); prior_jobs = runner.cli('operation', 'list')
        require(before[IDENTITY]['state'] == 'stopped' and all(job['state'] in TERMINAL for job in prior_jobs), 'fixture not stopped or another job active')
        inspect_owned_domain(virsh, IDENTITY, MAC)
        require(runner.cli('guest', 'recipe', 'result', original_result['operationID']) == original_result, 'original successful recipe result changed')
        require(runner.cli('plan', 'show', original_plan['planID']) == original_plan, 'original verified input plan changed')
        media = media_listing(Path.home() / 'images'); networks = network_snapshot(virsh)
        require(sha(SOURCE) == sha(COPY) == SOURCE_SHA, 'original media/copy source changed')
        runner.save('before-vms.json', before); runner.save('before-jobs.json', prior_jobs)
        terminal = Terminal(runner, 'guest-tools-stopped-80x24', 80, 24)
        try:
            select_owned_vm(terminal)
            terminal.wait('Stopped guest offers explained start instead of SSH fields', stopped_tools_guidance, terminal.send(b'g'))
            report['stoppedGuidanceBeforeStart'] = True
        finally:
            terminal.close(); terminal = None
        require(inventory(runner.cli('vm', 'list'), URI) == before and runner.cli('operation', 'list') == prior_jobs,
                'stopped guidance inspection unexpectedly changed guests or accepted a job')
        started = True; lifecycle('start')
        deadline = time.monotonic() + 120; ready = None
        while time.monotonic() < deadline:
            ready = runner.cli('vm', 'readiness', 'show', IDENTITY)
            if ready['state'] == 'responsive' and ready['agentResponsive']: break
            time.sleep(1)
        require(ready is not None and ready['agentResponsive'], 'already-installed agent did not respond after boot')
        runner.save('before-readiness.json', ready)
        leases = virsh('net-dhcp-leases', NETWORK, '--mac', MAC).stdout.decode()
        observed = re.findall(r'\b192\.168\.122\.\d+/24\b', leases)
        require(observed == [address + '/24'], 'current own DHCP lease differs from previously verified address; do not guess or alter host trust')
        terminal = Terminal(runner, 'guest-tools-submit-80x24', 80, 24)
        def wait(label, predicate, key=None): return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
        def focused(text, label):
            return any(re.match(r'^>\s*(?:\[\s*)?' + re.escape(label) + r'(?:[:\s\]]|$)', line) for line in text.splitlines())
        def focus(label):
            for index in range(16):
                if focused(terminal.screen.text(), label): return
                old = terminal.screen.text(); wait('Focus ' + label + ' ' + str(index), lambda screen: screen != old, b'\t')
            raise RuntimeError('unreachable control ' + label)
        select_owned_vm(terminal)
        wait('Installation form', lambda screen: 'Install guest tools' in screen and 'Preview installation' in screen, b'g')
        focus('Guest system')
        for index in range(5):
            if 'Guest system: < Fedora >' in terminal.screen.text(): break
            old = terminal.screen.text(); wait('Select Fedora ' + str(index), lambda screen: screen != old, b'\x1b[C')
        require('Guest system: < Fedora >' in terminal.screen.text(), 'Fedora profile not selected')
        for label, value in [('Guest IP address', address), ('Guest SSH user', 'virmillprobe'), ('SSH key', str(identity_file)), ('Verified host keys', str(trust_file))]:
            focus(label); terminal.send(b'\x15')
            while terminal.read(.05) and not terminal.screen.complete(): pass
            old = terminal.screen.text()
            wait('Set reference ' + label, lambda screen: focused(screen, label) and screen != old, value.encode())
        focus('Preview installation')
        wait('Review guest tools', lambda screen: 'Nothing has been applied' in screen, b'\r')
        match = None
        for page in range(16):
            match = re.search(r'Plan ID:\s*([0-9a-f-]{36})', terminal.screen.text())
            if match: break
            old = terminal.screen.text(); wait('Read exact plan ' + str(page), lambda screen: screen != old, b'\x1b[6~')
        require(match is not None, 'review plan identity missing')
        plan = runner.cli('plan', 'show', match.group(1)); check_review(plan, original_plan)
        plans.add(plan['planID']); runner.save('tools-plan.json', plan)
        wait('Confirm exact reviewed installation', lambda screen: 'Confirm reviewed changes' in screen and plan['planID'] in screen, b'\r')
        for ack in plan['acknowledgements']:
            require(re.search(r'>\s*\[ \]', terminal.screen.text()), 'unchecked acknowledgement not selected')
            wait('Approve ' + ack, lambda screen: re.search(r'>\s*\[x\]', screen), b' ')
            old = terminal.screen.text(); wait('Next acknowledgement', lambda screen: screen != old, b'\t')
        focus('Apply reviewed plan')
        wait('Guest recipe accepted', lambda screen: 'Operation accepted.' in screen or 'Job /' in screen, b'\r')
        job = None; deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            accepted = [j for j in runner.cli('operation', 'list') if j['planID'] == plan['planID']]
            require(len(accepted) <= 1, 'TUI submission created multiple jobs')
            if accepted:
                job = accepted[0]; allowed_jobs.add(job['operationID'])
                if job['state'] in TERMINAL: break
            terminal.read(.3)
        runner.save('tools-job.json', job)
        require(job is not None and job['state'] == 'succeeded', 'TUI guest recipe did not complete; retain uncertain effect')
        report['toolsOperationID'] = job['operationID']
        wait('TUI guest setup result', completed_guest_job)
        for index in range(8):
            if re.search(r'>\[', terminal.screen.text()): break
            old = terminal.screen.text(); wait('Focus job actions ' + str(index), lambda screen: screen != old, b'\t')
        require(re.search(r'>\[', terminal.screen.text()), 'completed job actions not reachable')
        for index in range(8):
            if re.search(r'>\[ Open VM \]', terminal.screen.text()): break
            old = terminal.screen.text(); wait('Select Open VM ' + str(index), lambda screen: screen != old, b'\x1b[C')
        require(re.search(r'>\[ Open VM \]', terminal.screen.text()), 'Open VM action not reachable')
        wait('Open exact verified Fedora VM', lambda screen: 'VM details' in screen and NAME in screen
             and IDENTITY in screen and 'Job /' not in screen, b'\r')
        result = runner.cli('guest', 'recipe', 'result', job['operationID']); runner.save('tools-result.json', result); check_result(result, plan['planID'])
        ready = runner.cli('vm', 'readiness', 'show', IDENTITY); runner.save('after-readiness.json', ready)
        require(ready['agentResponsive'] and ready['state'] == 'responsive', 'native guest ping missing after verification')
        report.update(status='passed', actualTUIApproval=True, alreadyConfigured=True, packageApplySkipped=True,
                      guestRecipeVerified=True, nativeGuestPing=True, tuiCompletedJobVisible=True,
                      guestSetupHeadlineVisible=True, openVMReturnedExactOwnedGuest=True)
    except BaseException as error: report['error'] = repr(error)
    finally:
        if terminal is not None: terminal.close()
        try:
            if started:
                accepted = [job for job in runner.cli('operation', 'list') if job['planID'] in plans]
                allowed_jobs.update(job['operationID'] for job in accepted)
                require(all(job['state'] == 'succeeded' for job in accepted), 'failed or uncertain own job retained for review')
                inspect_owned_domain(virsh, IDENTITY, MAC)
                vm = runner.cli('vm', 'show', IDENTITY)
                if vm['state'] == 'running': lifecycle('stop')
                deadline = time.monotonic() + 90
                while time.monotonic() < deadline:
                    vm = runner.cli('vm', 'show', IDENTITY)
                    if vm['state'] == 'stopped': break
                    time.sleep(1)
                require(vm['state'] == 'stopped', 'guest did not shut down; no forced stop')
                report['ownedFixtureStoppedAndRetained'] = True
        except BaseException as error: report.update(status='failed', cleanupError=repr(error))
        try:
            if before is not None:
                after = inventory(runner.cli('vm', 'list'), URI); runner.save('after-vms.json', after)
                require(after == before, 'guest XML/state not preserved')
                report['allGuestsPreserved'] = True
            if prior_jobs is not None:
                indexed = {job['operationID']: job for job in runner.cli('operation', 'list')}
                require(all(indexed.get(job['operationID']) == job for job in prior_jobs)
                        and set(indexed) - {job['operationID'] for job in prior_jobs} <= allowed_jobs, 'unrelated jobs changed')
                report['existingJobsPreserved'] = True
            if media is not None: require(media_listing(Path.home() / 'images') == media, 'owner media metadata changed')
            if networks is not None: require(network_snapshot(virsh) == networks, 'existing network state/configuration changed')
            require(sha(SOURCE) == sha(COPY) == SOURCE_SHA and sha(prior_dir / 'report.json') == prior_report_sha, 'original sources/evidence changed')
            for path, values in reference_stats.items():
                info = Path(path).stat(); require([info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns] == values, 'credential reference changed')
            report['sourcesNetworksAndCredentialReferencesPreserved'] = True
        except BaseException as error: report.update(status='failed', preservationError=repr(error))
        runner.save('report.json', report); os.close(fd); print(json.dumps(report, sort_keys=True))
    return 0 if report['status'] == 'passed' else 1


class Tests(unittest.TestCase):
    def test_stopped_guidance_requires_action_without_credential_fields(self):
        screen = 'This VM is stopped. Start it before installing guest tools.\n[ Preview start VM ]'
        self.assertTrue(stopped_tools_guidance(screen))
        self.assertFalse(stopped_tools_guidance(screen.replace('Preview start VM', 'Refresh')))
        self.assertFalse(stopped_tools_guidance(screen + '\nSSH key: []'))

    def test_completed_screen_requires_guest_result_and_open_vm(self):
        screen = 'Job / Guest setup completed\nStatus: succeeded\n[ Open VM ]'
        self.assertTrue(completed_guest_job(screen))
        for before, after in [('Guest setup completed', 'Completed'), ('succeeded', 'running'), ('[ Open VM ]', '[ Refresh result ]')]:
            self.assertFalse(completed_guest_job(screen.replace(before, after)))

    def test_review_binds_prior_target_credentials_recipe_and_privilege(self):
        resource = 'libvirt|' + URI + '|vm|' + IDENTITY
        review = {'resource': {'resourceUUID': IDENTITY}, 'address': '192.168.122.246', 'port': 22, 'user': 'virmillprobe',
                  'identitySHA256': 'a' * 64, 'knownHostsSHA256': 'b' * 64, 'recipeSHA256': 'c' * 64,
                  'scriptSHA256': {'apply': 'd' * 64}, 'arguments': [], 'reboot': 'never',
                  'recipe': {'metadata': {'name': 'virmill-guest-tools-fedora'}}, 'nativeAddressBindingVerified': False, 'rawOutputRetained': False}
        plan = {'operation': 'guest.recipe.run', 'actorUID': 1000, 'connectionID': URI, 'resourceIDs': [resource, 'guest-setup:' + resource],
                'acknowledgements': sorted(ACKS), 'review': review}
        check_review(plan, plan)
        for key, value in [('address', '192.168.122.247'), ('user', 'root'), ('knownHostsSHA256', 'f' * 64),
                           ('identitySHA256', 'e' * 64), ('recipeSHA256', 'f' * 64), ('arguments', ['extra'])]:
            bad = copy.deepcopy(plan); bad['review'][key] = value
            with self.assertRaises(RuntimeError): check_review(bad, plan)
        bad = copy.deepcopy(plan); bad['acknowledgements'].append('extra-privilege')
        with self.assertRaises(RuntimeError): check_review(bad, plan)

    def result(self):
        return {'complete': True, 'state': 'succeeded', 'resource': {'resourceUUID': IDENTITY}, 'nativeAddressBindingVerified': False,
                'rawOutputRetained': False, 'stages': [{'stage': name, 'complete': True, 'effectUnknown': False,
                  'intent': {'runPlanned': name != 'apply'}, 'receipt': {'outcome': outcome, 'uid': 1001, 'transportCompleted': name != 'apply'}}
                 for name, outcome in [('readiness', 'ready'), ('check', 'already-configured'), ('apply', 'skip-apply'), ('verify', 'verified')]]}
    def test_result_requires_skipped_apply_and_verified_nonroot_guest(self):
        value = self.result(); check_result(value)
        for index, field, bad in [(1, 'outcome', 'needs-apply'), (2, 'outcome', 'applied'), (3, 'outcome', 'failed'), (0, 'uid', 0)]:
            changed = copy.deepcopy(value); changed['stages'][index]['receipt'][field] = bad
            with self.assertRaises(RuntimeError): check_result(changed)
        changed = copy.deepcopy(value); changed['stages'][2]['intent']['runPlanned'] = True
        with self.assertRaises(RuntimeError): check_result(changed)


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__); p.add_argument('--root', type=Path); p.add_argument('--execute-disposable', action='store_true'); p.add_argument('--self-test', action='store_true')
    a = p.parse_args()
    if a.self_test:
        require(a.root is None and not a.execute_disposable, 'self-test cannot select native actions'); unittest.main(argv=[sys.argv[0]])
    else:
        require(a.root is not None and a.execute_disposable, 'explicit disposable root/run required'); sys.exit(execute(a.root))
