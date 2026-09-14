#!/usr/bin/env python3
"""Owner-authorized disposable walkthrough of the one-approval import (ADR 0057).

Runs on the authorized test VM. Without --source it creates one small blank
qcow2 disk inside its private stage; with --source it imports that existing
disk image, ISO or OVA read-only. Through the real TUI it imports with the
default private folder, chooses an existing active pool when none is
preselected and approves one combined review. It then checks that preparation,
creation and start ran with no further review. The new VM is hard-stopped
through a reviewed CLI plan and left defined, with its volumes, for review. No
other VM, pool, network or file is changed or removed. Client state and data
folders are private to the stage. Copy tui_workspace_probe.py alongside.
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

PREPARE = ('import.prepare', 'import.prepare-disks', 'import.prepare-install')


def virsh(uri, *args):
    process = subprocess.run(['virsh', '-c', uri, *args], stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30)
    require(process.returncode == 0, 'virsh ' + args[0] + ' failed')
    return process.stdout


def domains(uri):
    # VM names may contain spaces; virsh prints one name per line.
    return {name.strip() for name in virsh(uri, 'list', '--all', '--name').splitlines() if name.strip()}


def focus_button(terminal, label, wait_label, limit=40):
    """Tab until the named control is focused; never activates anything."""
    marker = re.compile(r'>\s*\[ ' + re.escape(label) + r' \]')
    screen = terminal.screen.text()
    for _ in range(limit):
        if marker.search(screen):
            return screen
        screen = terminal.wait(wait_label + ': Tab', lambda text: True, terminal.send(b'\t'))
    raise RuntimeError('control not reachable: ' + label)


def focus_row(terminal, label, wait_label, limit=40):
    marker = re.compile(r'^\s*>\s*(?:\[[ x]\]\s*)?' + re.escape(label), re.MULTILINE)
    screen = terminal.screen.text()
    for _ in range(limit):
        if marker.search(screen):
            return screen
        screen = terminal.wait(wait_label + ': Tab', lambda text: True, terminal.send(b'\t'))
    raise RuntimeError('control not reachable: ' + label)


def walkthrough(runner, uri, source, deadline_seconds):
    terminal = Terminal(runner, 'one-approval-import', 120, 36)
    report = {}
    try:
        def wait(label, predicate, key=None):
            return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
        picker = lambda text: re.search(r'(?:^|[│|])Choose a file[ \t]*$', text, re.MULTILINE) is not None

        def choose(entry, label):
            wait(label + ' filter', lambda text: picker(text) and 'Find:' in text, b'/')
            wait(label + ' typed', lambda text: picker(text) and ('Find: ' + entry) in text, entry.encode())
            wait(label + ' kept', lambda text: picker(text) and 'Enter open/select' in text, b'\r')

        wait('Overview', lambda text: 'Virmill' in text and uri in text)
        wait('Import opens the file browser', picker, b'i')
        for part in source.relative_to(Path.home()).parts[:-1]:
            choose(part, 'folder ' + part)
            wait('open ' + part, lambda text, part=part: picker(text) and part in text, b'\r')
        choose(source.name, 'source file')
        terminal.started = time.monotonic()
        summary = wait('Summary after inspection', lambda text: ('Review source' in text or 'Review appliance' in text) and not picker(text), b'\r')
        report['summary'] = 'appliance' if 'Review appliance' in summary else 'source'
        focus_button(terminal, 'Continue', 'summary')
        # The long private path is shown shortened with a leading ellipsis.
        disks = wait('Folder step skipped with the private default', lambda text: 'Saved in:' in text and 'data/virmill/imports/' in text, b'\r')
        report['folderStepSkipped'] = 'Save in:' not in disks
        if 'Source images are not in use' in disks:
            focus_row(terminal, 'Source images are not in use', 'offline')
            wait('Offline confirmed', lambda text: re.search(r'\[x\] Source images are not in use', text) is not None, b' ')
        focus_button(terminal, 'Preview image preparation', 'disks')
        settings = wait('Preview opens VM settings first', lambda text: 'Create a VM' in text and 'Storage pool' in text, b'\r')
        report['poolPreselected'] = 'Storage pool: < Choose' not in settings
        if not report['poolPreselected']:
            focus_row(terminal, 'Storage pool', 'pool')
            wait('Existing active pool chosen', lambda text: 'Storage pool: < Choose' not in text, b'\x1b[C')
        report['firmware'] = (re.search(r'Firmware: < ([^>]+) >', terminal.screen.text()) or [None, None])[1]
        focus_button(terminal, 'Continue to disks', 'settings')
        disks_page = wait('Disks page', lambda text: 'Disks and boot' in text, b'\r')
        report['busPreselected'] = 'Controller bus: < Choose' not in disks_page
        focus_button(terminal, 'Continue to networks', 'disks page')
        networks = wait('Networks page', lambda text: 'Network adapters' in text and 'After creation: < Start the VM >' in text, b'\r')
        report['networkPreselected'] = 'Network: < Choose' not in networks
        focus_button(terminal, 'Review import', 'networks page')
        review = wait('One review covers creation and start', lambda text: 'Nothing has been applied' in text and 'After approval, Virmill also:' in text, b'\r')
        created_line = (re.search(r'- creates VM [^\n]*(?:\n\s+[^\n-][^\n]*)*', review) or [''])[0]
        report['reviewedVM'] = re.sub(r'\s+', ' ', created_line)
        report['reviewListsStart'] = 'starts the VM once it is created' in review
        require(created_line and report['reviewListsStart'], 'combined review lacks later steps')
        wait('Confirmation', lambda text: 'Confirm reviewed changes' in text, b'\r')
        checked = 0
        while '> [ Apply reviewed plan ]' not in terminal.screen.text():
            require(checked < 20, 'confirmation did not reach Apply')
            wait(f'check item {checked + 1}', lambda text: True, b'\r')
            checked += 1
        report['itemsChecked'] = checked
        before = domains(uri)
        # The confirmation stays visible until the coordinator accepts the job.
        wait('Preparation accepted', lambda text: 'Confirm reviewed changes' not in text, b'\r')
        started = time.monotonic()
        created = None
        while time.monotonic() - started < deadline_seconds:
            terminal.started = time.monotonic()  # long copies keep the session open
            terminal.read(1)
            # A chain that stops shows that step's review with this notice.
            require('needs your review' not in terminal.screen.text(), 'a later step stopped for another review')
            new = domains(uri) - before
            if new:
                created = sorted(new)[0]
                if virsh(uri, 'domstate', created).strip() == 'running':
                    break
        require(created is not None and virsh(uri, 'domstate', created).strip() == 'running', 'VM was not created and started')
        report['vm'] = created
        report['seconds'] = round(time.monotonic() - started)
        return report
    finally:
        terminal.close()


def execute(root, uri, source, deadline_seconds):
    require(authorized_test_host() and os.getuid() == 1000 and os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(root.absolute()))
    require(stage.is_relative_to(Path.home() / 'virmill-tests') and stage != Path.home() / 'virmill-tests' and
            stat.S_ISDIR(stage.lstat().st_mode) and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    ident = uuid.uuid4().hex[:8]
    run = stage / ('onestep-' + ident)
    for folder in (run, run / 'state', run / 'data', run / 'src'):
        folder.mkdir(mode=0o700)
    if source is None:
        source = run / 'src' / ('onestep-' + ident + '.qcow2')
        subprocess.run(['qemu-img', 'create', '-q', '-f', 'qcow2', str(source), '64M'], check=True, timeout=30)
    else:
        source = canonical_path(str(source.absolute()))
        require(source.is_relative_to(Path.home()) and stat.S_ISREG(source.lstat().st_mode), 'existing regular source under home required')
    source_before = (source.stat().st_size, source.stat().st_mtime_ns)
    pools_before, domains_before = virsh(uri, 'pool-list', '--all', '--name'), domains(uri)
    networks_before = virsh(uri, 'net-list', '--all', '--name')
    os.environ['XDG_STATE_HOME'], os.environ['XDG_DATA_HOME'] = str(run / 'state'), str(run / 'data')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_CLOEXEC)
    args = SimpleNamespace(binary='/usr/bin/virmill', connection=uri)
    runner = Runner(args, run, fd)
    report = walkthrough(runner, uri, source, deadline_seconds)
    ops = {j.get('operation'): j['state'] for j in reversed(runner.cli('operation', 'list')) if j.get('operation')}
    prepared = [op for op in PREPARE if ops.get(op) == 'succeeded']
    require(prepared and ops.get('vm.create.devices-v1') == 'succeeded' and ops.get('vm.start') == 'succeeded', 'expected prepare, create and start jobs')
    report['succeededOperations'] = [prepared[0], 'vm.create.devices-v1', 'vm.start']
    vm = next(v for v in runner.cli('vm', 'list') if v['name'] == report['vm'])
    plan = runner.cli('vm', 'stop', vm['key']['resourceUUID'], '--hard')
    require(set(plan['acknowledgements']) <= {'host-mutation', 'data-loss-hard-stop'}, 'stop review asks for unexpected consequences')
    argv = ['plan', 'apply', plan['planID'], '--digest', plan['planDigest'], '--idempotency-key', 'onestep-stop-' + ident, '--wait', '--timeout', '60s']
    for ack in plan['acknowledgements']:
        argv += ['--ack', ack]
    require(runner.cli(*argv)['state'] == 'succeeded', 'hard stop failed')
    require(virsh(uri, 'domstate', report['vm']).strip() == 'shut off', 'test VM still running')
    require(virsh(uri, 'pool-list', '--all', '--name') == pools_before and virsh(uri, 'net-list', '--all', '--name') == networks_before and
            domains(uri) == domains_before | {report['vm']}, 'other pools, networks or VMs changed')
    require((source.stat().st_size, source.stat().st_mtime_ns) == source_before, 'source changed')
    report.update({'result': 'passed', 'connection': uri, 'sourceUnchanged': True, 'retainedStoppedVM': report['vm'], 'otherResourcesUnchanged': True})
    runner.save('report.json', report)
    print(json.dumps(report, indent=2, sort_keys=True))


class ProbeTests(unittest.TestCase):
    def test_focus_marker_matches_focused_button_only(self):
        self.assertIsNotNone(re.search(r'>\s*\[ ' + re.escape('Review import') + r' \]', '  > [ Review import ]'))
        self.assertIsNone(re.search(r'>\s*\[ ' + re.escape('Review import') + r' \]', '    [ Review import ]'))


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--execute-disposable', action='store_true')
    p.add_argument('--root', type=Path)
    p.add_argument('--source', type=Path, help='existing disk image, ISO or OVA under home; default: a generated blank disk')
    p.add_argument('--connection', default='qemu:///session', choices=('qemu:///session', 'qemu:///system'))
    p.add_argument('--deadline', type=int, default=150, help='seconds allowed for preparation, creation and start')
    p.add_argument('--self-test', action='store_true')
    a = p.parse_args()
    if a.self_test:
        unittest.main(argv=['one_approval_import_probe'], exit=True)
    require(a.execute_disposable and a.root, '--execute-disposable --root STAGE required')
    execute(a.root, a.connection, a.source, a.deadline)


if __name__ == '__main__':
    main()
