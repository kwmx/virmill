#!/usr/bin/env python3
"""Bounded, parent-operated native ISO import-options walkthrough; never Apply.

The parent alone runs this on the explicitly authorized disposable VM. Requires
an explicit existing ISO, a pinned executable and a new output child beneath the
approved test tree. Reuses the current-screen PTY parser and native inventory
checks in tui_workspace_probe.py (copy both scripts together). Export creates a
new settings file in the test output; preview creates a durable plan only. It
never creates a VM or converted media and never deletes anything. This provides
UX-01/UX-02 software evidence and IMP-01 input/plan coverage, not guest boot or
full acceptance. --self-test runs without services, PTYs or network access.
"""
import argparse
import json
import os
from pathlib import Path
import re
import socket
import stat
import sys
import time
import unittest

from tui_workspace_probe import (OUTPUT_ROOT, Runner, Terminal, canonical_path,
                                 canonical_uuid, generation, inventory, jobs, require, sha,
                                 strict_json)


def workspace_page(text, section):
    return re.search(r'\bVirmill\s*/\s*' + re.escape(section) + r'\b', text.split('\n')[0]) is not None


def selected_label(text, label):
    """A focused control, never its explanatory prose or a stale transcript."""
    return any(re.search(r'(?:^|[│|])\s*>\s*(?:\[[ xX]\]\s*|\[\s*)?' + re.escape(label) +
                         r'(?:\s|:|\]|$)', line) for line in text.splitlines())


def verify_export(data, destination):
    require(isinstance(data, dict), 'export must contain a JSON input object')
    require(data.get('offlineSources') is True, 'offline acknowledgement was not exported')
    require(data.get('destination') == str(destination), 'exported destination differs')
    require(data.get('mediaID') == 'install-media', 'media identity differs')
    require(data.get('disks') == [{'id': 'system', 'virtualBytes': 1024 * 1024 * 1024},
                                  {'id': 'data', 'virtualBytes': 2048 * 1024 * 1024}],
            'two disk IDs, order or capacities differ from edited options')
    require(not set(data) - {'offlineSources', 'destination', 'mediaID', 'disks', 'sha256'},
            'export contains unexpected fields')


def verify_plan(plan, export, source):
    require(isinstance(plan, dict) and plan.get('operation') == 'import.prepare-install',
            'service returned a different operation')
    canonical_uuid(plan.get('planID'))
    review = plan.get('review')
    require(isinstance(review, dict) and review.get('sourceKind') == 'installation-media' and
            review.get('mediaID') == export['input']['mediaID'] and
            review.get('blankDisks') == export['input']['disks'] and
            review.get('destination') == export['input']['destination'],
            'actual service plan differs from the exported edited options')
    require(review.get('vmDefined') is False and review.get('guestBootVerified') is False,
            'preview falsely claims VM or guest boot completion')
    files = review.get('files')
    require(isinstance(files, list) and len(files) == 1 and
            files[0].get('path') == source.name and
            re.fullmatch('[0-9a-f]{64}', files[0].get('sha256', '')),
            'source file/digest missing from actual service preview')


def read_export(path, destination):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_uid == 1000 and
                before.st_nlink == 1 and not before.st_mode & 0o077 and before.st_size <= 1 << 20,
                'export must be a bounded private ordinary user file')
        with os.fdopen(os.dup(fd), 'rb') as stream:
            raw = stream.read((1 << 20) + 1)
        require(generation(before) == generation(os.fstat(fd)), 'export changed while read')
        data = strict_json(raw)
        verify_export(data, destination)
        return {'path': str(path), 'sha256': sha(raw), 'bytes': len(raw), 'input': data}
    finally:
        os.close(fd)


def walkthrough(runner, iso, columns, rows):
    terminal = Terminal(runner, f'import-options-{columns}x{rows}', columns, rows)
    destination = runner.directory / f'prepared-{columns}x{rows}'
    exported = runner.directory / f'options-{columns}x{rows}.json'
    require(not destination.exists() and not exported.exists(), 'preview/export targets must be new')
    try:
        def wait(label, predicate, key=None):
            return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)

        def focus(label):
            text = terminal.screen.text()
            for step in range(24):
                if selected_label(text, label):
                    return
                previous = text
                text = wait(f'Focus {label}: Tab {step + 1}', lambda current: current != previous, b'\t')
            raise RuntimeError('control not reachable by Tab: ' + label)

        def activate(label, predicate, description=None):
            focus(label)
            return wait(description or label, predicate, b'\r')

        def edit(label, value):
            focus(label)
            # Clear only a focused editable control, never arbitrary navigation.
            terminal.send(b'\x15')
            while terminal.read(.1) and not terminal.screen.complete():
                pass
            wait('Edit ' + label, lambda text: selected_label(text, label), value.encode('ascii'))

        def picker(text, kind='file'):
            return re.search(r'(?:^|[│|])Choose a ' + kind + r'[ \t]*$', text, re.MULTILINE) is not None

        def browser_path(path, kind):
            wait('Browser path input', lambda text: picker(text, kind) and 'Path:' in text, b'\x0c')
            terminal.send(b'\x15')
            while terminal.read(.1) and not terminal.screen.complete():
                pass
            wait('Enter explicit probe folder path', lambda text: picker(text, kind) and 'Path:' in text,
                 str(path).encode('ascii'))
            return wait('Browser opens selected folder', lambda text:
                        (picker(text, kind) and 'Path:' not in text) or not picker(text, kind), b'\r')

        wait('Default Overview loads', lambda text: 'Virmill' in text and 'Virtual machines' in text)
        wait('VM workspace opens', lambda text: workspace_page(text, 'VMs'), b'2')
        wait('Source choices', lambda text: 'Import / Choose a source' in text and 'ISO installer' in text, b'i')
        wait('ISO choice selected', lambda text: re.search(r'>.*ISO installer', text) is not None, b'\x1b[B')
        wait('ISO browser opens', lambda text: picker(text), b'\r')
        # The source itself is selected from a directory listing; no typed source
        # filename or fabricated fixture inventory is injected into the form.
        browser_path(iso.parent, 'file')
        wait('Browser filter starts', lambda text: picker(text) and 'Enter done' in text, b'/')
        wait('Existing ISO appears', lambda text: picker(text) and iso.name in text, iso.name.encode('ascii'))
        wait('Browser filter completes', lambda text: picker(text) and 'Enter open/select' in text, b'\r')
        wait('ISO selected into native source options', lambda text: not picker(text) and 'Media name' in text,
             b'\r')
        edit('Media name', 'install-media')
        activate('Next: Destination', lambda text: 'New folder name' in text, 'Destination options open')
        activate('Save in', lambda text: picker(text, 'folder'), 'Destination folder browser opens')
        browser_path(runner.directory, 'folder')
        if picker(terminal.screen.text(), 'folder'):
            wait('Choose test output parent', lambda text: not picker(text, 'folder') and 'New folder name' in text, b'\x13')
        edit('New folder name', destination.name)
        activate('Next: Disks', lambda text: 'Add blank disk' in text, 'Disk options open')
        edit('Disk name', 'system')
        edit('Size (MiB)', '1024')
        activate('Add blank disk', lambda text: 'Disk' in text, 'Second blank disk added')
        edit('Disk name', 'data')
        edit('Size (MiB)', '2048')
        focus('Source images are not in use')
        require('[ ] Source images are not in use' in terminal.screen.text(), 'offline acknowledgement must begin unchecked')
        wait('Offline-source acknowledgement toggled', lambda text: '[x]' in text or '[X]' in text, b' ')
        activate('Export settings', lambda text: 'Export settings' in text and 'File name' in text,
                 'Export destination prompt')
        wait('Export folder picker opens', lambda text: picker(text, 'folder'), b'\x0f')
        browser_path(runner.directory, 'folder')
        if picker(terminal.screen.text(), 'folder'):
            wait('Choose export folder', lambda text: not picker(text, 'folder') and 'File name' in text, b'\x13')
        focus('File name')
        terminal.send(b'\x1b[F')
        terminal.send(b'\x7f' * len('virmill-import-settings.json'))
        while terminal.read(.1) and not terminal.screen.complete():
            pass
        wait('Export filename entered', lambda text: exported.name in text, exported.name.encode('ascii'))
        wait('Export current edited options', lambda text: 'Settings exported:' in text, b'\r')
        export = read_export(exported, destination)
        # Export may either return immediately or offer a Back button.
        if 'Preview' not in terminal.screen.text():
            wait('Back to disk options after export', lambda text: 'Preview' in text, b'\x1b')
        activate('Preview import', lambda text: 'Plan ID' in text and 'Plan digest' in text,
                 'Real service plan opens without Apply')
        plan_text = terminal.screen.text()
        require('install' in plan_text.casefold() or 'iso' in plan_text.casefold(), 'installation plan identity absent')
        matched = re.search(r'Plan ID:\s*([0-9a-f-]{36})', plan_text)
        require(matched is not None, 'complete plan ID absent from visible review')
        plan = runner.cli('plan', 'show', matched.group(1))
        verify_plan(plan, export, iso)
        runner.save(terminal.label + '-plan.json', plan)
        wait('Cancel plan returns to disk options', lambda text: 'Plan ID' not in text and '[Disks]' in text, b'\x1b')
        wait('Esc goes back to destination options', lambda text: '[Destination]' in text and 'New folder name' in text, b'\x1b')
        wait('Esc goes back to source options', lambda text: '[Source]' in text and 'Media name' in text, b'\x1b')
        wait('Esc cancels draft back to source choices', lambda text: 'Import / Choose a source' in text and 'ISO installer' in text, b'\x1b')
        wait('Esc closes source choices to VM workspace', lambda text: workspace_page(text, 'VMs') and 'Choose a source' not in text, b'\x1b')
        terminal.send(b'q')
        deadline = time.monotonic() + 5
        while terminal.process.poll() is None and time.monotonic() < deadline:
            if not terminal.read():
                break
        require(terminal.process.wait(timeout=2) == 0, 'TUI did not exit cleanly')
        require(not destination.exists(), 'preview created destination artifacts')
        runner.checks.append({'case': terminal.label, 'status': 'passed', 'size': [columns, rows],
                              'sourceSelectedFromBrowser': True, 'export': export,
                              'previewCanceled': True, 'planID': plan['planID'], 'sourceSHA256': plan['review']['files'][0]['sha256'],
                              'destinationAbsent': True,
                              'guestMutationSubmitted': False, 'exitCode': 0})
    finally:
        terminal.close()


def parser():
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument('--self-test', action='store_true')
    result.add_argument('--execute-disposable', action='store_true')
    result.add_argument('--binary')
    result.add_argument('--binary-sha256')
    result.add_argument('--iso')
    result.add_argument('--output')
    result.add_argument('--connection', choices=('qemu:///system', 'qemu:///session'), default='qemu:///system')
    return result


def main():
    args = parser().parse_args()
    if args.self_test:
        require(not any((args.execute_disposable, args.binary, args.output, args.iso)), 'self-test cannot execute')
        return 0 if unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ProbeTests)).wasSuccessful() else 1
    require(args.execute_disposable and args.binary and args.output and args.iso and
            re.fullmatch('[0-9a-f]{64}', args.binary_sha256 or ''), 'explicit host execution, paths and SHA-256 pin required')
    require(socket.gethostname() in ('virmill-test', 'virmill-test.home') and
            os.getuid() == os.geteuid() == 1000, 'only the authorized ordinary-user disposable host may execute')
    binary, output, iso = map(canonical_path, (args.binary, args.output, args.iso))
    require(output.is_relative_to(OUTPUT_ROOT) and output != OUTPUT_ROOT, 'new approved output child required')
    require(iso.name.isascii() and all(c.isalnum() or c in ' ._-' for c in iso.name), 'bounded printable ISO filename required')
    source_before = iso.lstat()
    require(stat.S_ISREG(source_before.st_mode) and source_before.st_size > 0 and iso.suffix.lower() == '.iso',
            'existing ordinary ISO source required')
    fd = os.open(binary, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid in (0, 1000) and
                before.st_mode & 0o111 and not before.st_mode & 0o7022 and 4 <= before.st_size <= 256 << 20,
                'bounded ordinary owner-controlled executable required')
        with os.fdopen(os.dup(fd), 'rb') as stream:
            raw = stream.read((256 << 20) + 1)
        require(raw.startswith(b'\x7fELF') and len(raw) == before.st_size and
                sha(raw) == args.binary_sha256 and generation(before) == generation(os.fstat(fd)),
                'native executable differs from reviewed pin')
        os.umask(0o077)
        output.mkdir(mode=0o700)
        runner = Runner(args, output, fd)
        report = {'status': 'failed', 'scope': 'native ISO options, export and unapplied service preview; not boot/creation acceptance',
                  'hostname': socket.gethostname(), 'uid': os.getuid(), 'binary': str(binary),
                  'binarySHA256': args.binary_sha256, 'source': str(iso), 'sourceGeneration': generation(source_before),
                  'connection': args.connection, 'pythonVersion': sys.version,
                  'startedUTC': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                  'guestMutationSubmitted': False, 'durablePlanPreviewsOnly': True, 'checks': runner.checks}
        runner.save('intent.json', report)
        baseline = prior_jobs = None
        try:
            report['version'] = runner.cli('version')
            baseline = inventory(runner.cli('vm', 'list'), args.connection)
            prior_jobs = jobs(runner.cli('operation', 'list'))
            runner.save('baseline.json', {'vms': baseline, 'jobs': prior_jobs})
            for size in ((80, 24), (120, 36)):
                walkthrough(runner, iso, *size)
            report['status'] = 'passed'
        except BaseException as error:
            report['error'] = type(error).__name__ + ': ' + str(error)
        finally:
            try:
                after = inventory(runner.cli('vm', 'list'), args.connection)
                after_jobs = jobs(runner.cli('operation', 'list'))
                report['nativeInventoryPreserved'] = baseline is not None and after == baseline
                report['jobStatesPreserved'] = prior_jobs is not None and after_jobs == prior_jobs
                report['noOperationCreated'] = prior_jobs is not None and set(after_jobs) == set(prior_jobs)
                report['sourceMetadataPreserved'] = generation(source_before) == generation(iso.lstat())
                report['binaryPreserved'] = generation(before) == generation(os.fstat(fd)) == generation(binary.lstat())
                runner.save('after.json', {'vms': after, 'jobs': after_jobs})
                require(all(report[k] for k in ('nativeInventoryPreserved', 'jobStatesPreserved', 'noOperationCreated',
                                                'sourceMetadataPreserved', 'binaryPreserved')), 'preservation check failed')
            except BaseException as error:
                report['preservationError'] = type(error).__name__ + ': ' + str(error)
                report['status'] = 'failed'
            report['finishedUTC'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
            runner.save('report.json', report)
            print(json.dumps(report, sort_keys=True))
        return 0 if report['status'] == 'passed' else 1
    finally:
        os.close(fd)


class ProbeTests(unittest.TestCase):
    def test_workspace_header_spacing(self):
        for header in ('Virmill / VMs', 'Virmill  / VMs', 'Virmill  /  VMs  qemu:///system'):
            self.assertTrue(workspace_page(header, 'VMs'))
        self.assertFalse(workspace_page('Virmill / Overview\nVirmill / VMs', 'VMs'))
        self.assertFalse(workspace_page('Virmill / VMsomething', 'VMs'))

    def test_focus_requires_actual_marker(self):
        for value in ('> Disk ID: system', ' 1 Overview        │> [ Next ]', '> [x] Sources are offline'):
            label = 'Sources are offline' if '[x]' in value else ('Next' if 'Next' in value else 'Disk ID')
            self.assertTrue(selected_label(value, label))
        self.assertFalse(selected_label('Helpful text about Next', 'Next'))
        self.assertFalse(selected_label('  [ Next ]', 'Next'))
        self.assertFalse(selected_label('> Next step: destination', 'Next stepper'))

    def test_export_checks_order_capacity_toggle_and_destination(self):
        data = {'offlineSources': True, 'destination': '/tmp/new', 'mediaID': 'install-media',
                'disks': [{'id': 'system', 'virtualBytes': 1073741824}, {'id': 'data', 'virtualBytes': 2147483648}]}
        verify_export(data, Path('/tmp/new'))
        for key, value in (('offlineSources', False), ('disks', list(reversed(data['disks']))),
                           ('destination', '/tmp/other'), ('unreviewed', True)):
            with self.subTest(key=key), self.assertRaises(RuntimeError):
                verify_export(dict(data, **{key: value}), Path('/tmp/new'))

    def test_service_plan_matches_export_without_boot_claim(self):
        options = {'mediaID': 'install-media', 'destination': '/tmp/new',
                   'disks': [{'id': 'system', 'virtualBytes': 1073741824}, {'id': 'data', 'virtualBytes': 2147483648}]}
        plan = {'planID': '11111111-2222-4333-8444-555555555555', 'operation': 'import.prepare-install', 'review': {
            'sourceKind': 'installation-media', 'mediaID': 'install-media',
            'blankDisks': options['disks'], 'destination': '/tmp/new',
            'files': [{'path': 'fixture.iso', 'sha256': 'a' * 64}],
            'vmDefined': False, 'guestBootVerified': False}}
        verify_plan(plan, {'input': options}, Path('/tmp/fixture.iso'))
        for key, value in (('guestBootVerified', True), ('destination', '/tmp/other'),
                           ('blankDisks', []), ('files', [])):
            changed = dict(plan, review=dict(plan['review'], **{key: value}))
            with self.subTest(key=key), self.assertRaises(RuntimeError):
                verify_plan(changed, {'input': options}, Path('/tmp/fixture.iso'))

    def test_duplicate_and_nonfinite_export_refused(self):
        for value in (b'{"disks":[],"disks":[]}', b'{"bytes":NaN}'):
            with self.assertRaises((ValueError, RuntimeError)):
                strict_json(value)

    def test_cli_requires_explicit_scope_arguments(self):
        args = parser().parse_args([])
        self.assertFalse(args.execute_disposable)
        self.assertIsNone(args.binary_sha256)
        self.assertIsNone(args.iso)


if __name__ == '__main__':
    sys.exit(main())
