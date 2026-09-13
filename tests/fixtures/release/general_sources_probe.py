#!/usr/bin/env python3
"""Parent-operated disposable source detection and TUI probe; never Plan/Apply.

Creates only new synthetic image fixtures beneath a new ~/virmill-tests output
directory. Reuses tui_workspace_probe.py and import_options_probe.py beside this
file. No SSH, guest creation, package installation or existing-media modification.
"""
import argparse
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

from tui_workspace_probe import (OUTPUT_ROOT, Runner, Terminal, canonical_path,
                                 generation, inventory, jobs, require, sha, strict_json)
from import_options_probe import selected_label, workspace_page


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


FORMATS = {'qcow2': 'qcow2', 'raw': 'raw', 'vmdk': 'vmdk', 'vdi': 'vdi',
           'vpc': 'vhd', 'vhdx': 'vhdx'}


def native_command(runner, argv):
    require(argv[0] in ('/usr/bin/qemu-img', '/usr/bin/xorriso'), 'unapproved fixture executable')
    result = subprocess.run(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=30, check=False)
    require(len(result.stdout) + len(result.stderr) <= 1 << 20, 'fixture output exceeds bound')
    runner.checks.append({'fixtureArgv': argv, 'exitCode': result.returncode,
                         'stdout': result.stdout.decode('utf-8', 'replace'),
                         'stderr': result.stderr.decode('utf-8', 'replace')})
    runner.save('fixture-commands.json', runner.checks)
    require(result.returncode == 0, 'fixture command failed; see fixture-commands.json')
    return result.stdout


def make_fixtures(runner):
    folder = runner.directory / 'disks'
    folder.mkdir(mode=0o700)
    expected = {}
    for format_, extension in FORMATS.items():
        path = folder / ('sample.' + extension)
        require(not path.exists(), 'fixture target already exists')
        native_command(runner, ['/usr/bin/qemu-img', 'create', '-f', format_, str(path), '8M'])
        info = strict_json(native_command(runner, ['/usr/bin/qemu-img', 'info', '--output=json', '-f', format_, str(path)]))
        require(info.get('format') == format_ and 8 << 20 <= info.get('virtual-size', 0) < 9 << 20,
                'created image metadata differs; VHD geometry rounding is bounded')
        expected[path.name] = {'format': format_, 'virtualBytes': info['virtual-size'],
                               'physicalBytes': path.stat().st_size}
    marker = runner.directory / 'iso-content'
    marker.mkdir(mode=0o700)
    with (marker / 'VIRMILL-FIXTURE.txt').open('x') as stream:
        stream.write('Synthetic ISO filesystem only; not a bootable guest installer.\n')
    iso = runner.directory / 'sample.iso'
    native_command(runner, ['/usr/bin/xorriso', '-as', 'mkisofs', '-V', 'VIRMILL_FIXTURE', '-o', str(iso), str(marker)])
    return folder, iso, expected


def verify_description(data, source, expected, kind):
    require(isinstance(data, dict) and data.get('kind') == kind and data.get('source') == str(source),
            'source identity or kind differs')
    require(data.get('appliance') is None and not any(key in data for key in
            ('hardware', 'vcpus', 'memoryMiB', 'firmware', 'os')), 'non-appliance invented VM hardware')
    require(isinstance(data.get('physicalBytes'), int) and data['physicalBytes'] > 0, 'source size missing')
    if kind == 'iso':
        require(not data.get('disks'), 'ISO invented existing disk hardware')
        return
    disks = data.get('disks')
    require(isinstance(disks, list) and len(disks) == len(expected), 'root disk count differs')
    observed = {}
    for disk in disks:
        path = disk.get('path')
        require(path in expected and path not in observed, 'unexpected or duplicate disk root')
        want = expected[path]
        require(disk.get('format') == want['format'] and disk.get('virtualBytes') == want['virtualBytes'] and
                disk.get('physicalBytes') == want['physicalBytes'], 'disk metadata differs from native qemu-img metadata')
        observed[path] = disk
    require(set(data.get('files', [])) == set(expected) and len(data['files']) == len(expected),
            'explicit source file set differs')
    require(data.get('root') == str(source if kind == 'disks' else source.parent), 'disk source root differs')


class Controls:
    def __init__(self, terminal): self.t = terminal

    def wait(self, label, predicate, key=None):
        return self.t.wait(label, predicate, self.t.send(key) if key is not None else -1)

    def focus(self, label):
        text = self.t.screen.text()
        for step in range(32):
            if selected_label(text, label): return
            previous = text
            text = self.wait('Focus ' + label + ' ' + str(step), lambda now: now != previous, b'\t')
        raise RuntimeError('control not reachable: ' + label)

    def activate(self, label, predicate):
        self.focus(label)
        return self.wait(label, predicate, b'\r')

    def edit(self, label, value):
        self.focus(label)
        self.t.send(b'\x15')
        while self.t.read(.1) and not self.t.screen.complete(): pass
        self.wait('Edit ' + label, lambda text: selected_label(text, label) and value in text, value.encode())


def picker(text):
    return re.search(r'(?:^|[│|])Choose a (?:file|source)[ \t]*$', text, re.MULTILINE) is not None


def select_source(c, path):
    # A saved setup is offered before the browser; Start new never resumes it.
    text = c.wait('Import opens mixed source browser or offers saved setup',
                  lambda text: picker(text) or 'Continue your saved setup?' in text, b'i')
    if not picker(text):
        c.wait('Start new setup focused', lambda text: '> [ Start new setup ]' in text, b'\t')
        c.wait('Start new setup opens mixed source browser', picker, b'\r')
    c.wait('Browser location input', lambda text: picker(text) and 'Path:' in text, b'\x0c')
    c.t.send(b'\x15')
    while c.t.read(.1) and not c.t.screen.complete(): pass
    c.t.send(str(path.parent).encode())
    c.wait('Open fixture directory', lambda text: picker(text) and 'Path:' not in text, b'\r')
    c.wait('Filter source listing', lambda text: picker(text) and 'Enter done' in text, b'/')
    c.wait('Fixture listed', lambda text: picker(text) and path.name in text, path.name.encode())
    c.wait('Filter completed', lambda text: picker(text) and 'Enter open/select' in text, b'\r')
    c.wait('Detected source review', lambda text: 'Review source' in text and 'CPU cores' in text, b'\r')


def source_walkthrough(runner, source, columns, rows):
    terminal = Terminal(runner, f'source-{source.suffix[1:]}-{columns}x{rows}', columns, rows)
    c = Controls(terminal)
    try:
        c.wait('Default Overview', lambda text: 'Virmill' in text and 'Virtual machines' in text)
        c.wait('VM workspace', lambda text: workspace_page(text, 'VMs'), b'2')
        select_source(c, source)
        c.edit('CPU cores', '3')
        c.edit('Memory (MiB)', '3072')
        c.activate('Advanced settings', lambda text: 'Advanced hardware' in text and 'Create a VM' in text)
        c.activate('Done', lambda text: 'Review source' in text)
        c.focus('CPU cores')
        require(re.search(r'CPU cores[^\n]*3', terminal.screen.text()), 'CPU choice lost on Advanced return')
        c.focus('Memory (MiB)')
        require(re.search(r'Memory \(MiB\)[^\n]*3072', terminal.screen.text()), 'memory choice lost on Advanced return')
        c.activate('Continue', lambda text: 'Choose where to save' in text or 'New folder name' in text)
        c.activate('Back', lambda text: 'Review source' in text)
        c.focus('CPU cores')
        require(re.search(r'CPU cores[^\n]*3', terminal.screen.text()), 'CPU choice lost on destination Back')
        c.focus('Memory (MiB)')
        require(re.search(r'Memory \(MiB\)[^\n]*3072', terminal.screen.text()), 'memory choice lost on destination Back')
        runner.checks.append({'sourceTUI': str(source), 'size': [columns, rows], 'advancedBeforeDestination': True,
                              'cpuMemoryPreserved': True, 'planOrApply': False})
    finally:
        terminal.close()


def guest_tools_walkthrough(runner, columns, rows):
    terminal = Terminal(runner, f'guest-tools-{columns}x{rows}', columns, rows)
    c = Controls(terminal)
    try:
        c.wait('Default Overview', lambda text: 'Virmill' in text and 'Virtual machines' in text)
        c.wait('VM workspace', lambda text: workspace_page(text, 'VMs') and 'NAME' in text, b'2')
        form = lambda text: 'Install guest tools' in text and 'Guest system' in text
        options = ('Prepare options / Windows help', 'Installation options / Windows help')
        text = c.wait('Selected VM guest tools', lambda text: form(text) or any(o in text for o in options), b'g')
        if not form(text):
            # Readiness guidance comes first; its options button opens the form without a plan.
            c.activate(next(o for o in options if o in text), form)
        c.focus('Guest system')
        c.wait('Guest profile chooser', lambda text: 'Guest system: < Debian >' in text, b'\x1b[C')
        c.activate('Advanced: desktop tools and SSH port', lambda text: 'Hide advanced options' in text)
        c.focus('Desktop tools')
        c.wait('Desktop toggle', lambda text: re.search(r'\[[xX]\] Desktop tools', text) is not None, b' ')
        for field in ('Guest IP address', 'Guest SSH user', 'SSH key', 'Verified host keys', 'SSH port'):
            c.focus(field)
            require(selected_label(terminal.screen.text(), field), 'guest field unavailable: ' + field)
        runner.checks.append({'guestToolsTUI': [columns, rows], 'profileAndToggle': True,
                              'sshFieldsReachable': True, 'planOrApply': False, 'guestPackageInstallationVerified': False})
    finally:
        terminal.close()


def parser():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--self-test', action='store_true')
    p.add_argument('--execute-disposable', action='store_true')
    p.add_argument('--binary', default='/usr/bin/virmill')
    p.add_argument('--binary-sha256')
    p.add_argument('--output')
    p.add_argument('--connection', choices=('qemu:///system', 'qemu:///session'), default='qemu:///system')
    return p


def main():
    args = parser().parse_args()
    if args.self_test:
        require(not args.execute_disposable and not args.output and not args.binary_sha256, 'self-test cannot execute')
        return 0 if unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ProbeTests)).wasSuccessful() else 1
    require(args.execute_disposable and args.output and re.fullmatch('[0-9a-f]{64}', args.binary_sha256 or ''),
            'explicit disposable execution, output and executable SHA256 required')
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000,
            'only the authorized ordinary-user disposable host may execute')
    require(args.binary == '/usr/bin/virmill', 'installed /usr/bin/virmill required')
    binary, output = canonical_path(args.binary), canonical_path(args.output)
    require(output.is_relative_to(OUTPUT_ROOT) and output != OUTPUT_ROOT, 'new approved output child required')
    fd = os.open(binary, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_uid == 0 and before.st_nlink == 1 and
                before.st_mode & 0o111 and not before.st_mode & 0o7022 and 4 <= before.st_size <= 256 << 20,
                'installed ordinary root-owned executable required')
        with os.fdopen(os.dup(fd), 'rb') as stream: raw = stream.read((256 << 20) + 1)
        require(raw.startswith(b'\x7fELF') and len(raw) == before.st_size and sha(raw) == args.binary_sha256 and
                generation(before) == generation(os.fstat(fd)), 'executable pin differs')
        os.umask(0o077)
        output.mkdir(mode=0o700)
        runner = Runner(args, output, fd)
        report = {'status': 'failed', 'scope': 'synthetic source metadata and real TUI navigation only',
                  'binarySHA256': args.binary_sha256, 'hostname': socket.gethostname(), 'uid': os.getuid(),
                  'guestMutationSubmitted': False, 'planSubmitted': False, 'guestPackageInstallationVerified': False,
                  'startedUTC': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'checks': runner.checks}
        runner.save('intent.json', report)
        baseline = prior_jobs = None
        fixture_state = {}
        try:
            report['version'] = runner.cli('version')
            baseline = inventory(runner.cli('vm', 'list'), args.connection)
            prior_jobs = jobs(runner.cli('operation', 'list'))
            runner.save('baseline.json', {'vms': baseline, 'jobs': prior_jobs})
            folder, iso, expected = make_fixtures(runner)
            fixture_state = {p: generation(p.lstat()) for p in [*folder.iterdir(), iso]}
            for name, metadata in expected.items():
                source = folder / name
                result = runner.cli('import', 'source', 'describe', str(source))
                verify_description(result, source, {name: metadata}, 'disk')
                runner.save('describe-' + name + '.json', result)
            result = runner.cli('import', 'source', 'describe', str(folder))
            verify_description(result, folder, expected, 'disks')
            runner.save('describe-folder.json', result)
            result = runner.cli('import', 'source', 'describe', str(iso))
            verify_description(result, iso, {}, 'iso')
            runner.save('describe-iso.json', result)
            catalog = runner.cli('guest', 'tools', 'catalog')
            require(isinstance(catalog, list), 'guest tools catalog missing')
            entries = {entry.get('id'): entry for entry in catalog}
            require(entries.get('linux-auto', {}).get('automatic') is True and
                    entries.get('windows', {}).get('automatic') is False and
                    entries['windows'].get('manualSteps'), 'automatic Linux/manual Windows catalog differs')
            runner.save('guest-tools-catalog.json', catalog)
            for size in ((80, 24), (120, 36)):
                source_walkthrough(runner, folder / 'sample.qcow2', *size)
                source_walkthrough(runner, iso, *size)
                guest_tools_walkthrough(runner, *size)
            report['status'] = 'passed'
        except BaseException as error:
            report['error'] = type(error).__name__ + ': ' + str(error)
        finally:
            try:
                after = inventory(runner.cli('vm', 'list'), args.connection)
                after_jobs = jobs(runner.cli('operation', 'list'))
                report['nativeInventoryPreserved'] = baseline is not None and baseline == after
                report['jobsPreserved'] = prior_jobs is not None and prior_jobs == after_jobs
                report['fixtureMetadataPreserved'] = bool(fixture_state) and all(generation(p.lstat()) == old for p, old in fixture_state.items())
                report['binaryPreserved'] = generation(before) == generation(os.fstat(fd)) == generation(binary.lstat())
                runner.save('after.json', {'vms': after, 'jobs': after_jobs})
                require(all(report[k] for k in ('nativeInventoryPreserved', 'jobsPreserved', 'fixtureMetadataPreserved', 'binaryPreserved')),
                        'preservation check failed')
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
    def test_metadata_matches_and_rejects_hardware_guesses(self):
        p = Path('/fixture/sample.qcow2')
        wanted = {'sample.qcow2': {'format': 'qcow2', 'virtualBytes': 8 << 20, 'physicalBytes': 196608}}
        data = {'source': str(p), 'root': str(p.parent), 'kind': 'disk', 'physicalBytes': 196608,
                'files': list(wanted), 'disks': [{'path': p.name, **wanted[p.name]}]}
        verify_description(data, p, wanted, 'disk')
        data['memoryMiB'] = 2048
        with self.assertRaises(RuntimeError): verify_description(data, p, wanted, 'disk')

    def test_browser_and_controls_match_current_screen(self):
        self.assertTrue(picker('Choose a source\n'))
        self.assertTrue(picker('Choose a file\n'))
        self.assertTrue(selected_label('> [x] Desktop tools', 'Desktop tools'))
        self.assertFalse(selected_label('Desktop tools help', 'Desktop tools'))


if __name__ == '__main__':
    sys.exit(main())
