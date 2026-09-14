#!/usr/bin/env python3
"""Owner-authorized disposable walkthrough of the one-approval import (ADR 0057).

Runs on the authorized test VM against qemu:///session. It creates one small
blank qcow2 disk inside its private stage and, through the real TUI, imports it
with the default private folder, chooses an existing active session pool and
approves one combined review. It then checks that preparation, creation and
start ran with no further review. The new VM is hard-stopped through a reviewed
CLI plan and left defined, with its volumes, for review. No other VM, pool,
network or file is changed or removed. Client state and data folders are
private to the stage. Copy tui_workspace_probe.py alongside.
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

URI = 'qemu:///session'


def virsh(*args):
    process = subprocess.run(['virsh', '-c', URI, *args], stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30)
    require(process.returncode == 0, 'virsh ' + args[0] + ' failed')
    return process.stdout


def domains():
    return {name for name in virsh('list', '--all', '--name').split() if name}


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


def walkthrough(runner, stage, source, name):
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

        wait('Overview', lambda text: 'Virmill' in text and URI in text)
        wait('Import opens the file browser', picker, b'i')
        for part in source.relative_to(Path.home()).parts[:-1]:
            choose(part, 'folder ' + part)
            wait('open ' + part, lambda text, part=part: picker(text) and part in text, b'\r')
        choose(source.name, 'disk image')
        wait('Source summary after inspection', lambda text: 'Review source' in text and not picker(text), b'\r')
        focus_button(terminal, 'Continue', 'summary')
        # The long private path is shown shortened with a leading ellipsis.
        disks = wait('Folder step skipped with the private default', lambda text: 'Saved in:' in text and 'data/virmill/imports/' in text and 'Prepare disk images' in text, b'\r')
        report['folderStepSkipped'] = 'Save in:' not in disks
        focus_row(terminal, 'Source images are not in use', 'offline')
        wait('Offline confirmed', lambda text: re.search(r'\[x\] Source images are not in use', text) is not None, b' ')
        focus_button(terminal, 'Preview image preparation', 'disks')
        settings = wait('Preview opens VM settings first', lambda text: 'Create a VM' in text and 'Storage pool' in text, b'\r')
        if 'Storage pool: < Choose' in settings:
            focus_row(terminal, 'Storage pool', 'pool')
            wait('Existing active pool chosen', lambda text: 'Storage pool: < Choose' not in text, b'\x1b[C')
        report['firmwareSuggested'] = 'Suggested: BIOS' in terminal.screen.text() or 'BIOS' in terminal.screen.text()
        focus_button(terminal, 'Continue to disks', 'settings')
        wait('Disks page', lambda text: 'Disks and boot' in text, b'\r')
        focus_button(terminal, 'Continue to networks', 'disks page')
        wait('Networks page', lambda text: 'Network adapters' in text and 'After creation: < Start the VM >' in text, b'\r')
        focus_button(terminal, 'Review import', 'networks page')
        review = wait('One review covers creation and start', lambda text: 'Nothing has been applied' in text and 'After approval, Virmill also:' in text, b'\r')
        report['reviewListsCreation'] = 'creates VM' in review
        report['reviewListsStart'] = 'starts the VM once it is created' in review
        require(report['reviewListsCreation'] and report['reviewListsStart'], 'combined review lacks later steps')
        wait('Confirmation', lambda text: 'Confirm reviewed changes' in text, b'\r')
        checked = 0
        while '> [ Apply reviewed plan ]' not in terminal.screen.text():
            require(checked < 20, 'confirmation did not reach Apply')
            wait(f'check item {checked + 1}', lambda text: True, b'\r')
            checked += 1
        report['itemsChecked'] = checked
        before = domains()
        # The confirmation stays visible until the coordinator accepts the job.
        wait('Preparation accepted', lambda text: 'Confirm reviewed changes' not in text, b'\r')
        deadline = time.monotonic() + 150
        created = None
        while time.monotonic() < deadline:
            terminal.read(.5)
            screen = terminal.screen.text()
            require('needs your review' not in screen and 'Confirm reviewed changes' not in screen, 'a later step stopped for another review')
            new = domains() - before
            if new:
                created = sorted(new)[0]
                if virsh('domstate', created).strip() == 'running':
                    break
        require(created is not None and virsh('domstate', created).strip() == 'running', 'VM was not created and started')
        report['vm'] = created
        report['finalScreen'] = terminal.screen.text()
        return report
    finally:
        terminal.close()


def execute(root):
    require(authorized_test_host() and os.getuid() == 1000 and os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(root.absolute()))
    require(stage.is_relative_to(Path.home() / 'virmill-tests') and stage != Path.home() / 'virmill-tests' and
            stat.S_ISDIR(stage.lstat().st_mode) and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    ident = uuid.uuid4().hex[:8]
    run = stage / ('onestep-' + ident)
    for folder in (run, run / 'state', run / 'data', run / 'src'):
        folder.mkdir(mode=0o700)
    source = run / 'src' / ('onestep-' + ident + '.qcow2')
    subprocess.run(['qemu-img', 'create', '-q', '-f', 'qcow2', str(source), '64M'], check=True, timeout=30)
    pools_before, domains_before = virsh('pool-list', '--all', '--name'), domains()
    os.environ['XDG_STATE_HOME'], os.environ['XDG_DATA_HOME'] = str(run / 'state'), str(run / 'data')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_CLOEXEC)
    args = SimpleNamespace(binary='/usr/bin/virmill', connection=URI)
    runner = Runner(args, run, fd)
    report = walkthrough(runner, run, source, 'onestep-' + ident)
    jobs = runner.cli('operation', 'list')
    ours = [j for j in jobs if j.get('state') == 'succeeded' and j.get('operation') in ('import.prepare-disks', 'vm.create.devices-v1', 'vm.start')]
    report['succeededOperations'] = sorted({j['operation'] for j in ours})
    require(report['succeededOperations'] == ['import.prepare-disks', 'vm.create.devices-v1', 'vm.start'], 'expected prepare, create and start jobs')
    vm = next(v for v in runner.cli('vm', 'list') if v['name'] == report['vm'])
    plan = runner.cli('vm', 'stop', vm['key']['resourceUUID'], '--hard')
    require(set(plan['acknowledgements']) <= {'host-mutation', 'data-loss-hard-stop'}, 'stop review asks for unexpected consequences')
    argv = ['plan', 'apply', plan['planID'], '--digest', plan['planDigest'], '--idempotency-key', 'onestep-stop-' + ident, '--wait', '--timeout', '60s']
    for ack in plan['acknowledgements']:
        argv += ['--ack', ack]
    require(runner.cli(*argv)['state'] == 'succeeded', 'hard stop failed')
    require(virsh('domstate', report['vm']).strip() == 'shut off', 'test VM still running')
    require(virsh('pool-list', '--all', '--name') == pools_before and domains() == domains_before | {report['vm']}, 'other pools or VMs changed')
    report.update({'result': 'passed', 'retainedStoppedVM': report['vm'], 'otherResourcesUnchanged': True})
    report.pop('finalScreen', None)
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
    p.add_argument('--self-test', action='store_true')
    a = p.parse_args()
    if a.self_test:
        unittest.main(argv=['one_approval_import_probe'], exit=True)
    require(a.execute_disposable and a.root, '--execute-disposable --root STAGE required')
    execute(a.root)


if __name__ == '__main__':
    main()
