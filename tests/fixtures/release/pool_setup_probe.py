#!/usr/bin/env python3
"""Owner-authorized walkthrough of Create and Start storage pool inside VM setup.

Runs on the authorized test VM as the ordinary test user. It needs a host with
no usable storage pool, and makes one without touching the host's own: every
XDG folder points to private folders, so qemu:///session starts private libvirt
daemons with no pools, networks or VMs, and the probe runs its own coordinator
there. The host's own pools, networks and VMs are listed before and after, and
must not change.

Libvirt keeps session sockets (the QEMU probe, each VM's monitor) under
XDG_CONFIG_HOME, and a Unix socket path must stay under 108 bytes. So the
runtime and libvirt configuration folders live in a short private folder in
the user's own /run/user/UID; images, cache and client state stay in the stage.

Through the real TUI it imports two generated blank qcow2 disks:

1. create: VM setup has no pool and offers Create storage pool. One short review
   creates libvirt's standard pool while the form stays open, and the pool is
   selected when ready. One approval then prepares, creates and starts the VM
   and removes the prepared copy.
2. start: the probe stops that pool in the private session. VM setup offers
   Start pool default, and the rest runs the same way.

Both VMs are hard-stopped through reviewed CLI plans. The private coordinator is
stopped at the end; the private folders are kept for review. Copy
tui_workspace_probe.py and one_approval_import_probe.py alongside.
"""
import argparse
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import time
from types import SimpleNamespace
import unittest
import uuid
from tui_workspace_probe import Runner, Terminal, require, canonical_path, authorized_test_host
from one_approval_import_probe import virsh, domains, focus_button, focus_row

URI = 'qemu:///session'
# Socket-bearing folders go in the short private folder; the rest in the stage.
SHORT = {'XDG_RUNTIME_DIR': 'run', 'XDG_CONFIG_HOME': 'config'}
STAGED = {'XDG_DATA_HOME': 'data', 'XDG_CACHE_HOME': 'cache', 'XDG_STATE_HOME': 'state'}
# The longest socket libvirt makes there: a monitor with a 20-character short name.
LONGEST_SOCKET = 'libvirt/qemu/lib/domain-9999-' + 'x' * 20 + '/monitor.sock'
BUTTONS = {'create': ('Create storage pool', 'Creating'), 'start': ('Start pool default', 'Starting')}


def private_environment(run, short):
    """The variables that give the TUI, CLI, coordinator and libvirt a private session."""
    return ({key: str(short / folder) for key, folder in SHORT.items()} |
            {key: str(run / folder) for key, folder in STAGED.items()} | {'VIRMILL_UPDATE_CHECK': '0'})


def sockets_fit(short):
    return len(str(short / SHORT['XDG_CONFIG_HOME'] / LONGEST_SOCKET).encode()) < 108


def host_inventory(env):
    """Names of the host's own pools, networks and VMs, read with its regular environment."""
    out = {}
    for uri in ('qemu:///system', 'qemu:///session'):
        for kind, args in (('pools', ('pool-list', '--all', '--name')), ('networks', ('net-list', '--all', '--name')),
                           ('vms', ('list', '--all', '--name'))):
            process = subprocess.run(['virsh', '-c', uri, *args], env=env, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30)
            require(process.returncode == 0, 'virsh ' + args[0] + ' failed on the host connection')
            out[uri + ' ' + kind] = sorted(name.strip() for name in process.stdout.splitlines() if name.strip())
    return out


def start_coordinator(run, short):
    socket = short / SHORT['XDG_RUNTIME_DIR'] / 'virmill' / 'control.sock'
    log = open(run / 'coordinator.log', 'wb')
    os.chmod(run / 'coordinator.log', 0o600)
    process = subprocess.Popen(['/usr/bin/virmilld'], stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
    log.close()
    deadline = time.monotonic() + 15
    while not socket.exists():
        require(process.poll() is None, 'the private coordinator exited; see coordinator.log')
        require(time.monotonic() < deadline, 'the private coordinator did not open its socket')
        time.sleep(.1)
    return process


def stop_coordinator(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=5)


def pool_state():
    names = [name.strip() for name in virsh(URI, 'pool-list', '--all', '--name').splitlines() if name.strip()]
    info = virsh(URI, 'pool-info', 'default') if names == ['default'] else ''
    path = re.search(r'<path>([^<]+)</path>', virsh(URI, 'pool-dumpxml', 'default')) if names == ['default'] else None
    return {'names': names, 'active': bool(re.search(r'^State:\s+running', info, re.MULTILINE)),
            'autostart': bool(re.search(r'^Autostart:\s+yes', info, re.MULTILINE)), 'path': path.group(1) if path else None}


def walkthrough(runner, source, imports, mode, deadline_seconds):
    terminal = Terminal(runner, 'pool-setup-' + mode, 120, 36)
    report = {'mode': mode}
    try:
        def wait(label, predicate, key=None):
            return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
        picker = lambda text: re.search(r'(?:^|[│|])Choose a file[ \t]*$', text, re.MULTILINE) is not None

        def choose(entry, label):
            wait(label + ' filter', lambda text: picker(text) and 'Find:' in text, b'/')
            wait(label + ' typed', lambda text: picker(text) and ('Find: ' + entry) in text, entry.encode())
            wait(label + ' kept', lambda text: picker(text) and 'Enter open/select' in text, b'\r')

        def confirm(label):
            """From a review, open the confirmation, check every item and apply once."""
            wait(label + ' confirmation', lambda text: 'Confirm reviewed changes' in text, b'\r')
            checked = 0
            while '> [ Apply reviewed plan ]' not in terminal.screen.text():
                require(checked < 20, label + ' confirmation did not reach Apply')
                wait(f'{label} item {checked + 1}', lambda text: True, b'\r')
                checked += 1
            # The confirmation stays visible until the coordinator accepts the job.
            wait(label + ' accepted', lambda text: 'Confirm reviewed changes' not in text, b'\r')
            return checked

        def refused(screen):
            return next((line.strip() for line in screen.splitlines() if 'Error:' in line), None)

        # Saved-setup choices depend on the job list, so wait until it has loaded.
        wait('Overview', lambda text: 'Virmill' in text and URI in text and 'Loading jobs' not in text)
        wait('Import opens the file browser', picker, b'i')
        for part in source.relative_to(Path.home()).parts[:-1]:
            choose(part, 'folder ' + part)
            wait('open ' + part, lambda text, part=part: picker(text) and part in text, b'\r')
        choose(source.name, 'source file')
        terminal.started = time.monotonic()
        wait('Summary after inspection', lambda text: 'Review source' in text and not picker(text), b'\r')
        focus_button(terminal, 'Continue', 'summary')
        disks = wait('Folder step skipped', lambda text: 'Saved in:' in text and 'data/virmill/imports/' in text, b'\r')
        if 'Source images are not in use' in disks:
            focus_row(terminal, 'Source images are not in use', 'offline')
            wait('Offline confirmed', lambda text: re.search(r'\[x\] Source images are not in use', text) is not None, b' ')
        focus_button(terminal, 'Preview image preparation', 'disks')
        settings = wait('VM settings', lambda text: ('Create a VM' in text and 'Storage pool' in text) or refused(text) is not None, b'\r')
        require(refused(settings) is None, 'VM setup refused: ' + str(refused(settings)))
        button, verb = BUTTONS[mode]
        require('Storage pool: < Choose' in settings, 'a usable pool was already selected')
        require('[ ' + button + ' ]' in settings, 'VM setup did not offer ' + button)
        report['alsoOffersCreate'] = '[ Create storage pool ]' in settings
        require(report['alsoOffersCreate'], 'Create storage pool not offered')
        focus_row(terminal, 'Storage pool', 'pool')
        report['poolHelp'] = next((line for line in ('No storage pool yet', 'Your storage pools are stopped')
                                   if line in terminal.screen.text()), None)
        focus_button(terminal, button, 'pool button')
        review = wait('Pool review', lambda text: 'Nothing has been applied' in text, b'\r')
        report['poolReviewNamesDefault'] = 'default' in review
        report['poolItemsChecked'] = confirm('Pool')
        # VM setup stays open while the pool job runs, then selects the pool.
        started = time.monotonic()
        report['progressShown'] = False
        while 'Storage pool: < default >' not in terminal.screen.text():
            screen = terminal.screen.text()
            require(time.monotonic() - started < 60, 'the pool was not selected')
            require(refused(screen) is None and 'was not created' not in screen, 'the pool step failed: ' + str(refused(screen)))
            report['progressShown'] = report['progressShown'] or (verb + ' storage pool default') in screen
            terminal.read(.5)
        require('Create a VM' in terminal.screen.text(), 'VM setup did not stay open')
        report['readyNoticeShown'] = 'Storage pool default is ready and selected' in terminal.screen.text()
        focus_button(terminal, 'Continue to disks', 'settings')
        wait('Disks page', lambda text: 'Disks and boot' in text, b'\r')
        focus_button(terminal, 'Continue to networks', 'disks page')
        wait('Networks page', lambda text: 'Network adapters' in text and 'After creation: < Start the VM >' in text, b'\r')
        focus_button(terminal, 'Review import', 'networks page')
        terminal.send(b'\r')
        started = time.monotonic()
        while 'After approval, Virmill also:' not in terminal.screen.text():
            require(time.monotonic() - started < deadline_seconds, 'combined review did not appear')
            require('Your VM settings are kept' not in terminal.screen.text(), 'import was refused: ' + terminal.screen.text()[-400:])
            terminal.started = time.monotonic()
            terminal.read(1)
        terminal.wait('One review covers creation and start', lambda text: 'Nothing has been applied' in text and 'After approval, Virmill also:' in text)
        before = domains(URI)
        report['itemsChecked'] = confirm('Import')
        created = None
        started = time.monotonic()
        while time.monotonic() - started < deadline_seconds:
            terminal.started = time.monotonic()
            terminal.read(1)
            screen = terminal.screen.text()
            require('needs your review' not in screen, 'a later step stopped for another review')
            require(refused(screen) is None, 'a step was refused: ' + str(refused(screen)))
            new = domains(URI) - before
            if new:
                created = sorted(new)[0]
                if virsh(URI, 'domstate', created).strip() == 'running' and imports.is_dir() and not any(imports.iterdir()):
                    break
        require(created is not None and virsh(URI, 'domstate', created).strip() == 'running', 'VM was not created and started')
        require(not any(imports.iterdir()), 'prepared copy was not removed')
        report['vm'] = created
        report['seconds'] = round(time.monotonic() - started)
        return report
    finally:
        terminal.close()


def hard_stop(runner, name, key):
    vm = next(v for v in runner.cli('vm', 'list') if v['name'] == name)
    plan = runner.cli('vm', 'stop', vm['key']['resourceUUID'], '--hard')
    require(set(plan['acknowledgements']) <= {'host-mutation', 'data-loss-hard-stop'}, 'stop review asks for unexpected consequences')
    argv = ['plan', 'apply', plan['planID'], '--digest', plan['planDigest'], '--idempotency-key', key, '--wait', '--timeout', '60s']
    for ack in plan['acknowledgements']:
        argv += ['--ack', ack]
    require(runner.cli(*argv)['state'] == 'succeeded', 'hard stop failed')
    require(virsh(URI, 'domstate', name).strip() == 'shut off', 'test VM still running')


def execute(root, deadline_seconds):
    require(authorized_test_host() and os.getuid() == 1000 and os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(root.absolute()))
    require(stage.is_relative_to(Path.home() / 'virmill-tests') and stage != Path.home() / 'virmill-tests' and
            stat.S_ISDIR(stage.lstat().st_mode) and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    user_runtime = Path(f'/run/user/{os.getuid()}')
    info = user_runtime.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) == 0o700, 'private user runtime folder required')
    ident = uuid.uuid4().hex[:8]
    run, short = stage / ('poolsetup-' + ident), user_runtime / ('vp-' + ident)
    require(sockets_fit(short), 'libvirt socket paths would exceed 108 bytes')
    for folder in (run, run / 'src', *(run / f for f in STAGED.values()), short, *(short / f for f in SHORT.values())):
        folder.mkdir(mode=0o700)
    regular = dict(os.environ)
    host_before = host_inventory(regular)
    os.environ.update(private_environment(run, short))
    require(domains(URI) == set() and pool_state()['names'] == [], 'the private session is not empty')
    report = {'hostInventoryBefore': host_before, 'privateSessionEmptyAtStart': True, 'privateRuntime': str(short), 'scenarios': []}
    coordinator = start_coordinator(run, short)
    try:
        fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_CLOEXEC)
        runner = Runner(SimpleNamespace(binary='/usr/bin/virmill', connection=URI), run, fd)
        imports = run / 'data' / 'virmill' / 'imports'
        for mode in ('create', 'start'):
            source = run / 'src' / f'pool{mode}-{ident}.qcow2'
            subprocess.run(['qemu-img', 'create', '-q', '-f', 'qcow2', str(source), '64M'], check=True, timeout=30)
            if mode == 'start':
                virsh(URI, 'pool-destroy', 'default')
                require(not pool_state()['active'], 'the pool did not stop')
            scenario = walkthrough(runner, source, imports, mode, deadline_seconds)
            pool = pool_state()
            scenario['pool'] = {'names': pool['names'], 'active': pool['active'], 'autostart': pool['autostart'],
                                'inPrivateDataFolder': pool['path'] == str(run / 'data' / 'libvirt' / 'images')}
            require(pool['names'] == ['default'] and pool['active'] and pool['autostart'] and scenario['pool']['inPrivateDataFolder'],
                    'the pool is not libvirt\'s standard active pool')
            hard_stop(runner, scenario['vm'], f'poolsetup-stop-{mode}-{ident}')
            report['scenarios'].append(scenario)
        ops = {}
        for job in runner.cli('operation', 'list'):
            ops.setdefault(job.get('operation'), []).append(job['state'])
        expected = {'storage.pool.create': 1, 'storage.pool.start': 1, 'import.prepare-disks': 2, 'vm.create.devices-v1': 2,
                    'vm.start': 2, 'import.discard': 2, 'vm.hard-stop': 2}
        report['jobs'] = {op: len(states) for op, states in sorted(ops.items())}
        require(report['jobs'] == expected and all(state == 'succeeded' for states in ops.values() for state in states),
                'expected exactly these succeeded jobs: ' + json.dumps(expected, sort_keys=True))
    finally:
        stop_coordinator(coordinator)
        for name in domains(URI):
            if virsh(URI, 'domstate', name).strip() == 'running':
                virsh(URI, 'destroy', name)  # Only after a failure; the private session is the probe's own.
                report.setdefault('stoppedAfterFailure', []).append(name)
        (run / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
        os.chmod(run / 'report.json', 0o600)
    report['hostInventoryUnchanged'] = host_inventory(regular) == host_before
    require(report['hostInventoryUnchanged'], 'the host\'s own pools, networks or VMs changed')
    report['result'] = 'passed'
    (run / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
    print(json.dumps({k: v for k, v in report.items() if k != 'hostInventoryBefore'}, indent=2, sort_keys=True))


class ProbeTests(unittest.TestCase):
    def test_private_environment_splits_short_and_staged_folders(self):
        run, short = Path('/tmp/stage/poolsetup-0'), Path('/run/user/1000/vp-0')
        env = private_environment(run, short)
        self.assertEqual(set(env), set(SHORT) | set(STAGED) | {'VIRMILL_UPDATE_CHECK'})
        self.assertTrue(all(Path(env[key]).parent == short for key in SHORT))
        self.assertTrue(all(Path(env[key]).parent == run for key in STAGED))
        self.assertEqual(env['VIRMILL_UPDATE_CHECK'], '0')

    def test_socket_paths_must_fit(self):
        self.assertTrue(sockets_fit(Path('/run/user/1000/vp-0123abcd')))
        # The first native run kept libvirt's folders in a deep stage; its probe socket did not fit.
        self.assertFalse(sockets_fit(Path('/home/tester/virmill-tests/poolsetup-b2d5662/poolsetup-bae016b4')))

    def test_each_scenario_names_its_setup_button(self):
        self.assertEqual(BUTTONS['create'][0], 'Create storage pool')
        self.assertEqual(BUTTONS['start'][0], 'Start pool default')


def main():
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument('--execute-disposable', action='store_true')
    p.add_argument('--root', type=Path)
    p.add_argument('--deadline', type=int, default=300, help='seconds allowed for each import to reach a running VM')
    p.add_argument('--self-test', action='store_true')
    a = p.parse_args()
    if a.self_test or not a.execute_disposable:
        unittest.main(argv=['pool_setup_probe'], exit=True)
    require(a.root is not None, '--root is required')
    execute(a.root, a.deadline)


if __name__ == '__main__':
    main()
