#!/usr/bin/env python3
"""Read-only installed 80x24 boot-order editing fixture on the authorized VM.

Requires adjacent tui_workspace_probe.py and a private staged binaries.json.
Uses only an existing exact prepared ISO operation, isolated frontend draft state,
CLI inventory reads and TUI editing. Never selects Preview or Apply, creates a VM,
changes helper policy, stops a guest, or edits source media. The parent stages and
executes this fixture. Local --self-test checks evidence decoders only.
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
import time
import unittest

from tui_workspace_probe import Runner, Terminal, canonical_path, inventory, media_listing, require, strict_json

URI = 'qemu:///system'
SOURCE_ID = '5a464dae-5ff1-4080-a4ed-1b239b697aaa'
SOURCE_REL = 'virmill-tests/spice-creation-a1d6cb6/spice-creation/prepared'
TERMINAL = {'succeeded', 'failed', 'partial', 'canceled', 'recovery-required'}


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def selected_source(sources, home):
    require(isinstance(sources, list) and 0 < len(sources) <= 1024, 'bounded prepared source list required')
    matches = [(i, row) for i, row in enumerate(sources) if row.get('operationID') == SOURCE_ID]
    require(len(matches) == 1, 'exact prepared ISO operation missing or duplicated')
    index, source = matches[0]
    require(source.get('destination') == str(home / SOURCE_REL) and isinstance(source.get('name'), str)
            and source['name'] and not any(ord(c) < 32 for c in source['name']), 'prepared source identity differs')
    return index, source


def decode_draft(raw):
    envelope = strict_json(raw)
    require(envelope.get('apiVersion') == 'virmill/v1' and envelope.get('kind') == 'TUIDraft', 'draft envelope differs')
    d = envelope['document']
    require(d.get('version') == 1 and d.get('connection') == URI and d.get('state') == 'editing'
            and not d.get('operationID') and not d.get('import'), 'draft was submitted or source type changed')
    c = d['creation']; spec = c['Spec']
    require(c['OperationID'] == SOURCE_ID and re.fullmatch('[0-9a-f]{64}', d.get('sourceBinding', '')),
            'draft lost exact prepared-source binding')
    require(len(spec['disks']) == 1 and len(spec.get('media', [])) == 1 and not spec['nics'],
            'existing generated ISO fixture disk/media/NIC shape differs')
    require(spec['disks'][0]['sourceID'] != spec['media'][0]['sourceID'], 'source device identity duplicated')
    return d


def without_boot(d):
    out = copy.deepcopy(d)
    out['creation'].pop('Page', None)
    for entry in out['creation']['Spec']['disks'] + out['creation']['Spec']['media']:
        entry.pop('bootOrder', None)
    return out


def require_order(d, disk, media, baseline=None):
    spec = d['creation']['Spec']
    require(spec['disks'][0]['bootOrder'] == disk and spec['media'][0]['bootOrder'] == media,
            'complete saved boot order differs from selected action')
    orders = [item['bootOrder'] for item in spec['disks'] + spec['media'] if item['bootOrder'] > 0]
    require(sorted(orders) == list(range(1, len(orders) + 1)), 'boot priorities are duplicated or noncontiguous')
    if baseline is not None:
        require(without_boot(d) == without_boot(baseline), 'boot editing changed source, buses or other VM settings')


def read_draft(path):
    st = path.lstat()
    require(stat.S_ISREG(st.st_mode) and st.st_uid == 1000 and st.st_nlink == 1
            and stat.S_IMODE(st.st_mode) == 0o600 and st.st_size <= 1 << 20, 'private bounded draft required')
    return decode_draft(path.read_bytes())


def execute(stage):
    require(socket.gethostname() in ('virmill-test', 'virmill-test.home') and os.getuid() == os.geteuid() == 1000,
            'wrong authorized host or actor')
    stage = canonical_path(str(stage.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    os.umask(0o077)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    require(all(sha('/usr/bin/' + n) == expected[n] for n in ('virmill', 'virmilld')), 'installed binaries differ')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        st = os.fstat(fd)
        require(stat.S_ISREG(st.st_mode) and st.st_uid == 0 and st.st_mode & 0o022 == 0, 'trusted installed binary required')
        with os.fdopen(os.dup(fd), 'rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected['virmill'], 'held executable differs')
        out = stage / 'boot-order-tui'; out.mkdir(mode=0o700)
        state = out / 'state'; state.mkdir(mode=0o700)
        runner = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, fd)
        runner.env['XDG_STATE_HOME'] = str(state)
        draft = state / 'virmill/tui-drafts/import.json'
        report = {'status': 'failed', 'scope': 'native 80x24 editing and draft evidence; no preview, apply, VM creation or guest boot',
                  'acceptanceSupport': ['IMP-01', 'IMP-02', 'IMP-04', 'UX-01', 'UX-02'],
                  'binaries': expected, 'fixtureSHA256': sha(__file__), 'sourceOperationID': SOURCE_ID,
                  'previewAttempted': False, 'applyAttempted': False}
        before = None; terminal = None
        def observe():
            return {'vms': inventory(runner.cli('vm', 'list'), URI), 'jobs': runner.cli('operation', 'list'),
                    'networks': runner.cli('network', 'list'), 'source': media_listing(Path.home() / SOURCE_REL),
                    'media': media_listing(Path.home() / 'images')}
        try:
            before = observe()
            require(all(j.get('state') in TERMINAL for j in before['jobs']), 'active jobs; coordinate fixture first')
            runner.save('before.json', before)
            index, source = selected_source(runner.cli('import', 'sources'), Path.home())
            runner.save('selected-source.json', source)
            terminal = Terminal(runner, 'boot-order-80x24', 80, 24)
            def wait(label, predicate, key=None):
                return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
            def focused(label):
                return any(re.match(r'^>\s*(?:\[\s*)?' + re.escape(label) + r'(?:[:\s\]]|$)', line)
                           for line in terminal.screen.text().splitlines())
            def focus(label):
                for i in range(24):
                    if focused(label): return
                    old = terminal.screen.text(); wait('Focus ' + label + str(i), lambda s: s != old, b'\t')
                raise RuntimeError('unreachable form control: ' + label)
            def activate(label, predicate):
                require('Preview' not in label and 'Apply' not in label, 'fixture cannot activate preview/apply')
                focus(label); return wait(label, predicate, b'\r')
            def fill(label, value):
                focus(label)
                old = terminal.screen.text(); wait('Clear ' + label, lambda s: s != old, b'\x15')
                wait('Set ' + label, lambda s: value in s, value.encode())
            def choice(label, value):
                focus(label)
                for i in range(32):
                    if re.search(re.escape(label) + r':\s*<\s*' + re.escape(value) + r'\s*>', terminal.screen.text()): return
                    old = terminal.screen.text(); wait('Choose ' + label + str(i), lambda s: s != old, b'\x1b[C')
                raise RuntimeError('choice unavailable: ' + label + ' / ' + value)
            def snapshot(label, disk, medium, baseline=None):
                deadline = time.monotonic() + 8
                while time.monotonic() < deadline:
                    terminal.read(.1)
                    if not draft.exists(): continue
                    d = read_draft(draft)
                    if d['creation']['Spec']['disks'][0]['bootOrder'] == disk and d['creation']['Spec']['media'][0]['bootOrder'] == medium:
                        if d['creation']['CPUText'] == '3' and d['creation']['MemoryText'] == '768':
                            require_order(d, disk, medium, baseline); runner.save(label + '.json', d); return d
                raise RuntimeError('edited boot order was not durably saved: ' + label)
            wait('Overview', lambda s: 'Virtual machines' in s)
            wait('VM inventory', lambda s: 'NAME' in s and 'STATE' in s, b'2')
            for i in range(10):
                if re.search(r'>\[', terminal.screen.text()): break
                old = terminal.screen.text(); wait('VM action focus' + str(i), lambda s: s != old, b'\t')
            for i in range(12):
                if '>[' + ' Create VM ]' in terminal.screen.text(): break
                old = terminal.screen.text(); wait('Select Create VM' + str(i), lambda s: s != old, b'\x1b[C')
            require('>[ Create VM ]' in terminal.screen.text(), 'Create VM action unavailable')
            wait('Prepared sources', lambda s: 'Choose prepared images' in s and 'Loading' not in s, b'\r')
            for i in range(index):
                old = terminal.screen.text(); wait('Choose exact prepared source' + str(i), lambda s: s != old, b'\x1b[B')
            require('> [ ' + source['name'] + ' ]' in terminal.screen.text(), 'selected row differs from exact CLI source ordering')
            wait('VM options', lambda s: 'CPU cores' in s and 'Storage pool' in s, b'\r')
            fill('CPU cores', '3'); fill('Memory (MiB)', '768')
            choice('Storage pool', 'virmill-test'); choice('Firmware', 'BIOS')
            activate('Continue to disks', lambda s: 'Controller bus' in s and 'Continue to networks' in s)
            baseline = snapshot('default-installer-first', 2, 1)
            require(all(item['bus'] == 'sata' for item in baseline['creation']['Spec']['disks'] + baseline['creation']['Spec']['media']),
                    'observed-host ISO default did not select SATA; fixture must not repair this default')
            report['initialISOBusesSATA'] = True
            disk = baseline['creation']['Spec']['disks'][0]['sourceID']
            medium = baseline['creation']['Spec']['media'][0]['sourceID']
            choice('Device', 'Installer/media: ' + medium)
            choice('Boot priority', 'Attach only')
            snapshot('attach-only', 1, 0, baseline)
            choice('Device', 'Disk: ' + disk)
            require(re.search(r'Boot priority:\s*<\s*1\s*>', terminal.screen.text()), 'disk was not visibly compacted to first')
            activate('Continue to networks', lambda s: 'Network adapters' in s and 'Step 3 of 3' in s)
            require('unique boot priority' not in terminal.screen.text(), 'ordinary attach-only edit blocked continuation')
            activate('Back', lambda s: 'Controller bus' in s and 'Continue to networks' in s)
            choice('Device', 'Installer/media: ' + medium)
            choice('Boot priority', '1')
            snapshot('reenabled-installer-first', 2, 1, baseline)
            choice('Device', 'Disk: ' + disk)
            choice('Boot priority', '1')
            snapshot('disk-first', 1, 2, baseline)
            require('Boot: 1. ' + disk in terminal.screen.text(), 'ordered device summary missing from disk screen')
            activate('Continue to networks', lambda s: 'Network adapters' in s and 'Step 3 of 3' in s)
            require('unique boot priority' not in terminal.screen.text(), 'ordinary move blocked continuation')
            terminal.send(b'\x03')
            deadline = time.monotonic() + 5
            while terminal.process.poll() is None and time.monotonic() < deadline: terminal.read(.05)
            require(terminal.process.poll() == 0, 'normal TUI shutdown/draft flush failed')
            persisted = read_draft(draft); require_order(persisted, 1, 2, baseline)
            runner.save('after-exit-draft.json', persisted)
            report.update(status='passed', editingSteps=['installer-first', 'attach-only', 'reenable-installer', 'disk-first'],
                          normalExitSaved=True, completeBootOrder=True, unrelatedChoicesPreserved=True)
        except BaseException as error:
            report['failure'] = str(error)
            raise
        finally:
            if terminal is not None: terminal.close()
            try:
                if before is not None:
                    after = observe(); runner.save('after.json', after)
                    require(before == after, 'VMs/jobs/networks/source media changed during read-only fixture')
                    report['inventoriesAndMediaUnchanged'] = True
            except BaseException as error:
                report['status'] = 'failed'; report['preservationFailure'] = str(error)
                raise
            finally:
                runner.save('report.json', report)
    finally:
        os.close(fd)


class EvidenceTests(unittest.TestCase):
    def document(self):
        return {'apiVersion': 'virmill/v1', 'kind': 'TUIDraft', 'document': {'version': 1, 'connection': URI,
                'state': 'editing', 'sourceBinding': 'a' * 64, 'creation': {'OperationID': SOURCE_ID, 'Page': 1,
                'CPUText': '3', 'MemoryText': '768', 'Spec': {'disks': [{'sourceID': 'disk', 'bus': 'sata', 'bootOrder': 2}],
                'media': [{'sourceID': 'installer', 'bus': 'sata', 'bootOrder': 1}], 'nics': []}}}}
    def test_exact_source_and_unsubmitted_draft(self):
        d = self.document(); decode_draft(json.dumps(d).encode())
        for field, value in [('state', 'submitted'), ('connection', 'qemu:///session'), ('sourceBinding', '')]:
            bad = copy.deepcopy(d); bad['document'][field] = value
            with self.assertRaises(Exception): decode_draft(json.dumps(bad).encode())
        bad = copy.deepcopy(d); bad['document']['creation']['OperationID'] = 'another'
        with self.assertRaises(Exception): decode_draft(json.dumps(bad).encode())
    def test_order_projection_preserves_other_hardware_and_source(self):
        before = decode_draft(json.dumps(self.document()).encode()); after = copy.deepcopy(before)
        after['creation']['Spec']['disks'][0]['bootOrder'] = 1
        after['creation']['Spec']['media'][0]['bootOrder'] = 0
        require_order(after, 1, 0, before)
        for key, value in [('bus', 'virtio'), ('sourceID', 'other')]:
            changed = copy.deepcopy(after); changed['creation']['Spec']['disks'][0][key] = value
            with self.assertRaises(Exception): require_order(changed, 1, 0, before)
        after['creation']['Spec']['media'][0]['bootOrder'] = 1
        with self.assertRaises(Exception): require_order(after, 1, 1)
    def test_source_requires_operation_and_destination(self):
        home = Path('/home/virmill-test')
        source = {'operationID': SOURCE_ID, 'destination': str(home / SOURCE_REL), 'name': 'prepared'}
        self.assertEqual(selected_source([source], home)[0], 0)
        for changed in [dict(source, destination='/other/prepared'), dict(source, operationID='another')]:
            with self.assertRaises(Exception): selected_source([changed], home)
        with self.assertRaises(Exception): selected_source([source, source], home)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--execute-disposable', action='store_true')
    parser.add_argument('--root', type=Path)
    parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        unittest.main(argv=[__file__])
    else:
        parser.error('explicit --execute-disposable and --root required') if not args.execute_disposable or args.root is None else execute(args.root)
