#!/usr/bin/env python3
"""Owner-authorized disposable test of storage pool creation (ADR 0056).

Effects happen only on qemu:///session: two pools named virmill-pool-<id> are
created in fresh folders inside the private stage and are left for review. No
other pool, folder, guest or medium is changed or removed. On qemu:///system,
libvirt's default pool is only planned, never applied. The TUI walk opens
Storage, reviews Create storage pool and cancels at the review.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
from types import SimpleNamespace
import unittest
import uuid
import xml.etree.ElementTree as ET
from tui_workspace_probe import Runner, Terminal, require, strict_json, canonical_path, authorized_test_host

SESSION, SYSTEM = 'qemu:///session', 'qemu:///system'
SYSTEM_DEFAULT = '/var/lib/libvirt/images'


def virsh(uri, *args):
    process = subprocess.run(['virsh', '-c', uri, *args], stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30)
    require(process.returncode == 0, 'virsh ' + args[0] + ' failed')
    return process.stdout


def pools(uri):
    out = {}
    for name in virsh(uri, 'pool-list', '--all', '--name').split():
        xml = ET.fromstring(virsh(uri, 'pool-dumpxml', name))
        info = dict(line.split(':', 1) for line in virsh(uri, 'pool-info', name).splitlines() if ':' in line)
        out[name] = {'type': xml.get('type'), 'path': xml.findtext('target/path') or '', 'state': info['State'].strip(),
                     'autostart': info['Autostart'].strip(), 'persistent': info['Persistent'].strip()}
    return out


def overlaps(a, b):
    a, b = os.path.normpath(a), os.path.normpath(b)
    return a == b or b.startswith(a.rstrip('/') + '/') or a.startswith(b.rstrip('/') + '/')


def folder_state(path):
    st = path.stat()
    try:
        label = os.getxattr(path, 'security.selinux').decode().rstrip('\0')
    except OSError:
        label = None
    return {'mode': oct(stat.S_IMODE(st.st_mode)), 'uid': st.st_uid, 'gid': st.st_gid, 'label': label}


def refusal(args, argv, code):
    """An expected refusal exits nonzero with a strict JSON error of that code."""
    process = subprocess.run([args.binary, '--connection', args.connection, '--output', 'json', '--non-interactive', *argv],
                             stdin=subprocess.DEVNULL, capture_output=True, timeout=40)
    require(process.returncode != 0, 'refusal expected')
    envelope = strict_json(process.stdout)
    error = envelope.get('error') if isinstance(envelope, dict) else None
    require(isinstance(error, dict) and error.get('code') == code, 'expected refusal code ' + code)
    return error['message']


def check_plan(plan, uri, name, path, autostart):
    require(plan['operation'] == 'storage.pool.create' and plan['connectionID'] == uri and
            plan['acknowledgements'] == ['host-mutation'], 'plan identity or acknowledgements differ')
    d = plan['review']['definition']
    require(d['name'] == name and d['path'] == str(path) and d['autostart'] is autostart and str(uuid.UUID(d['uuid'])) == d['uuid'],
            'plan definition differs')
    require([s['id'] for s in plan['steps']] == (['define', 'start', 'autostart'] if autostart else ['define', 'start']), 'plan steps differ')
    require(plan['review']['existingFilesChanged'] is False and plan['review']['otherPoolsChanged'] is False, 'plan preservation claims differ')
    return d['uuid']


def apply(runner, plan, key):
    job = runner.cli('plan', 'apply', plan['planID'], '--digest', plan['planDigest'], '--idempotency-key', key,
                     '--ack', 'host-mutation', '--wait', '--timeout', '60s')
    require(job['state'] == 'succeeded' and job['planID'] == plan['planID'], 'pool job did not succeed')
    return job['operationID']


def tui_walk(runner, name):
    terminal = Terminal(runner, 'storage-create-review', 120, 36)
    try:
        def wait(label, predicate, key=None):
            return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
        wait('Overview', lambda screen: 'Virtual machines' in screen)
        wait('Storage lists the new pool', lambda screen: name in screen and 'Create pool' in screen, b'4')
        wait('Storage tasks offer pool creation', lambda screen: 'Create storage pool' in screen, b'a')
        wait('Task search', lambda screen: 'Find task' in screen, b'/')
        wait('Search finds pool creation', lambda screen: 'Find task: Create storage pool' in screen, b'Create storage pool')
        wait('Keep search', lambda screen: 'Create storage pool' in screen, b'\r')
        review = wait('Pool review before any change', lambda screen: 'Libvirt creates the folder' in screen and 'default' in screen, b'\r')
        wait('Esc cancels the review', lambda screen: 'Libvirt creates the folder' not in screen, b'\x1b')
        return {'size': [120, 36], 'reviewShown': True, 'reviewShowsLibvirtImages': 'libvirt/images' in review, 'applied': False}
    finally:
        terminal.close()


def execute(root):
    require(authorized_test_host() and os.getuid() == 1000 and os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(root.absolute()))
    require(stage.is_relative_to(Path.home() / 'virmill-tests') and stage != Path.home() / 'virmill-tests' and
            stat.S_ISDIR(stage.lstat().st_mode) and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    ident = uuid.uuid4().hex[:8]
    evidence = stage / ('pool-run-' + ident)
    evidence.mkdir(mode=0o700)
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_CLOEXEC)
    args = SimpleNamespace(binary='/usr/bin/virmill', connection=SESSION)
    runner = Runner(args, evidence, fd)
    before = {SESSION: pools(SESSION), SYSTEM: pools(SYSTEM)}

    existing = evidence / 'existing-folder'
    existing.mkdir(mode=0o750)
    os.chmod(existing, 0o750)
    keep = existing / 'keep.img'
    keep.write_bytes(b'virmill pool fixture\n' * 64)
    keep_sha, folder_before = hashlib.sha256(keep.read_bytes()).hexdigest(), folder_state(existing)
    first = 'virmill-pool-' + ident
    plan = runner.cli('storage', 'pool', 'create', '--name', first, '--path', str(existing))
    check_plan(plan, SESSION, first, existing, True)
    first_job = apply(runner, plan, 'pool-existing-' + ident)
    observed = pools(SESSION)[first]
    require(observed == {'type': 'dir', 'path': str(existing), 'state': 'running', 'autostart': 'yes', 'persistent': 'yes'},
            'pool is not a running persistent directory pool with autostart')
    require(folder_state(existing) == folder_before and hashlib.sha256(keep.read_bytes()).hexdigest() == keep_sha,
            'existing folder permissions, label or file changed')
    require('keep.img' in virsh(SESSION, 'vol-list', first), 'existing file is not listed as a volume')
    listed = runner.cli('storage', 'pool', 'list')
    require(any(p['name'] == first and p['active'] and p['type'] == 'dir' for p in listed), 'Virmill does not list the new pool')

    refusals = {}
    for label, argv, code in [
        ('same-folder', ['--name', first + 'b', '--path', str(existing)], 'INVALID_STATE'),
        ('same-name', ['--name', first, '--path', str(evidence / 'other-folder')], 'INVALID_STATE'),
        ('nested-folder', ['--name', first + 'c', '--path', str(existing / 'nested')], 'INVALID_STATE'),
        ('system-folder', ['--name', first + 'd', '--path', '/etc/virmill-pool-' + ident], 'INVALID_INPUT'),
    ]:
        refusal(args, ['storage', 'pool', 'create', *argv], code)
        refusals[label] = code
    require(set(pools(SESSION)) == set(before[SESSION]) | {first}, 'a refused plan defined a pool')
    require(not (evidence / 'other-folder').exists() and not (existing / 'nested').exists(), 'a refused plan created a folder')

    missing = evidence / 'new-folder'
    second = first + 'n'
    plan = runner.cli('storage', 'pool', 'create', '--name', second, '--path', str(missing), '--no-autostart')
    check_plan(plan, SESSION, second, missing, False)
    second_job = apply(runner, plan, 'pool-missing-' + ident)
    require(missing.is_dir() and pools(SESSION)[second] == {'type': 'dir', 'path': str(missing), 'state': 'running', 'autostart': 'no', 'persistent': 'yes'},
            'missing folder was not created as a running pool without autostart')

    args.connection = SYSTEM
    system_before = before[SYSTEM]
    blocked = 'default' in system_before or any(p['path'] and overlaps(p['path'], SYSTEM_DEFAULT) for p in system_before.values())
    if blocked:
        refusal(args, ['storage', 'pool', 'create'], 'INVALID_STATE')
        system = {'planned': False, 'refusedForExistingPool': True, 'applied': False}
    else:
        check_plan(runner.cli('storage', 'pool', 'create'), SYSTEM, 'default', SYSTEM_DEFAULT, True)
        system = {'planned': True, 'path': SYSTEM_DEFAULT, 'applied': False}
    require(pools(SYSTEM) == system_before, 'system pools changed')
    args.connection = SESSION

    tui = tui_walk(runner, first)
    require(set(pools(SESSION)) == set(before[SESSION]) | {first, second}, 'TUI review changed pools')
    report = {'result': 'passed', 'existingFolderPool': {'job': first_job, 'autostart': True, 'folderPreserved': True, 'fileListedUnchanged': True},
              'refusals': refusals, 'missingFolderPool': {'job': second_job, 'autostart': False, 'folderCreated': True},
              'system': system, 'tui': tui, 'retainedSessionPools': [first, second],
              'otherPoolsUnchanged': True}
    runner.save('report.json', report)
    print(json.dumps(report, indent=2, sort_keys=True))


class ProbeTests(unittest.TestCase):
    def test_overlaps_matches_equal_and_nested_folders(self):
        self.assertTrue(overlaps('/var/lib/libvirt/images', '/var/lib/libvirt/images/iso'))
        self.assertTrue(overlaps('/var/lib/libvirt', '/var/lib/libvirt/images'))
        self.assertFalse(overlaps('/var/lib/libvirt/images2', '/var/lib/libvirt/images'))

    def test_plan_check_refuses_unseen_acknowledgements(self):
        plan = {'operation': 'storage.pool.create', 'connectionID': SESSION, 'acknowledgements': ['host-mutation', 'other'],
                'review': {'definition': {}}, 'steps': []}
        with self.assertRaises(Exception):
            check_plan(plan, SESSION, 'x', Path('/x'), True)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--execute-disposable', action='store_true')
    p.add_argument('--root', type=Path)
    p.add_argument('--self-test', action='store_true')
    a = p.parse_args()
    if a.self_test:
        unittest.main(argv=['pool_creation_native_probe'], exit=True)
    require(a.execute_disposable and a.root, '--execute-disposable --root STAGE required')
    execute(a.root)


if __name__ == '__main__':
    main()
