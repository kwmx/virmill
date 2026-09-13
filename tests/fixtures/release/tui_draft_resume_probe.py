#!/usr/bin/env python3
"""Parent-operated installed-TUI draft restart probe (UX-01/UX-02 partial).

Requires --execute-disposable --root PRIVATE_STAGED_DIRECTORY and binaries.json,
plus adjacent tui_workspace_probe.py. Runs only as UID1000 on the authorized
<test-vm-login> host. Uses three real80x24 PTYs and a private XDG_STATE_HOME below
its new output directory, leaving the owner's actual drafts untouched.

The probe edits an ISO wizard's Media name before choosing any source, exits
normally, resumes that value in a new TUI, then explicitly starts a new setup.
It never inspects/parses images, opens a plan, applies an operation or creates a
guest. CLI VM/job observations and source-media metadata must remain unchanged.
All output/state is retained on failure. --self-test is pure validation only.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import stat
import sys
import time
import unittest

from tui_workspace_probe import (Runner, Terminal, canonical_path, inventory,
                                 jobs, media_listing, require, strict_json)


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


URI = 'qemu:///system'
SENTINEL = 'draft-resume-installer'


def digest(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def check_document(envelope, media, kind):
    require(isinstance(envelope, dict) and envelope.get('apiVersion') == 'virmill/v1' and
            envelope.get('kind') == 'TUIDraft' and
            re.fullmatch('[0-9a-f]{32}', envelope.get('generation', '')) is not None,
            'invalid persisted draft envelope')
    document = envelope.get('document')
    require(isinstance(document, dict) and document.get('version') == 1 and
            document.get('connection') == URI and document.get('state') == 'editing',
            'draft is not an ordinary unsubmitted setup')
    require(set(document) <= {'version', 'connection', 'state', 'operationID',
                              'sourceBinding', 'import', 'creation'} and
            not document.get('creation') and not document.get('sourceBinding') and
            not document.get('operationID'), 'unexpected inspected/submitted settings')
    values = document.get('import')
    require(isinstance(values, dict) and values.get('Kind') == kind and
            values.get('MediaID') == media and not values.get('Source') and
            not values.get('SelectedSource'), 'edited/default import values differ')
    require(set(values) <= {'Kind', 'Source', 'SelectedSource', 'DestinationParent',
                           'DestinationName', 'SystemID', 'MediaID', 'SHA256',
                           'VMName', 'VCPUs', 'MemoryMiB', 'Offline', 'Disks', 'Files', 'Page'},
            'unexpected persisted observations or authority')
    return document


def read_draft(state, media, kind):
    path = state / 'virmill' / 'tui-drafts' / 'import.json'
    for parent in (state, path.parent.parent, path.parent):
        info = parent.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 1000 and
                stat.S_IMODE(info.st_mode) == 0o700, 'draft directory privacy differs')
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 1000 and info.st_nlink == 1 and
            stat.S_IMODE(info.st_mode) == 0o600 and 0 < info.st_size <= 1 << 20,
            'draft file privacy or size differs')
    raw = path.read_bytes()
    document = check_document(strict_json(raw), media, kind)
    return {'path': str(path), 'sha256': hashlib.sha256(raw).hexdigest(),
            'document': document}


def graceful_quit(terminal):
    terminal.send(b'\x03')
    deadline = time.monotonic() + 10
    while terminal.process.poll() is None and time.monotonic() < deadline:
        terminal.read(.05)
    require(terminal.process.poll() == 0, 'normal TUI exit/draft flush failed')


def overview(terminal):
    terminal.wait('installed default TUI overview', lambda text:
                  re.search(r'Virmill\s*/\s*Overview', text) is not None and
                  URI in text and 'Virtual machines' in text)


def picker(text):
    return 'Choose a file' in text and 'Esc' in text


def iso_form(text):
    return 'Prepare installation media' in text and 'Media name' in text


def open_iso_form(terminal):
    terminal.wait('All tools opened', lambda text: 'All tools / All sections' in text,
                  terminal.send(b':'))
    terminal.wait('task search focused', lambda text: 'Find task:' in text and
                  'Enter Keep matches' in text, terminal.send(b'/'))
    terminal.wait('ISO preparation action found', lambda text:
                  'Find task: import prepare-install' in text and 'No matching tasks' not in text,
                  terminal.send(b'import prepare-install'))
    terminal.wait('task match kept', lambda text: 'Find task: import prepare-install' in text and
                  'Enter Keep matches' not in text, terminal.send(b'\r'))
    terminal.wait('source browser opens', picker, terminal.send(b'\r'))
    terminal.wait('uninspected ISO settings', iso_form, terminal.send(b'\x1b'))


def execute(root):
    require(authorized_test_host() and
            os.getuid() == 1000 and os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(root.absolute()))
    require(stage.is_relative_to(Path.home() / 'virmill-tests') and
            stage != Path.home() / 'virmill-tests' and stat.S_ISDIR(stage.lstat().st_mode) and
            stage.stat().st_uid == 1000 and stat.S_IMODE(stage.stat().st_mode) == 0o700,
            'private ordinary staged root required')
    manifest = stage / 'binaries.json'
    info = manifest.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 1000 and info.st_size <= 65536,
            'bounded ordinary owned binary manifest required')
    expected = strict_json(manifest.read_bytes())
    for name in ('virmill', 'virmilld'):
        require(re.fullmatch('[0-9a-f]{64}', expected.get(name, '')) is not None and
                digest('/usr/bin/' + name) == expected[name], 'installed binary differs: ' + name)
    binary_fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(binary_fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0,
                'installed frontend is not a trusted ordinary binary')
        with os.fdopen(os.dup(binary_fd), 'rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected['virmill'],
                    'held frontend hash differs')
        out = stage / 'tui-draft-resume'
        out.mkdir(mode=0o700)
        state = out / 'state'
        state.mkdir(mode=0o700)
        runner = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, binary_fd)
        # CLI observation retains the actual coordinator environment. Only the
        # TUI child receives isolated frontend state; its IPC socket is unchanged.
        report = {'status': 'failed', 'acceptanceSupport': ['UX-01', 'UX-02'],
                  'scope': 'installed80x24 TUI editing, normal-exit persistence, restart resume and explicit Start new; no source parsing or guest operation',
                  'binaries': expected, 'xdgStateHome': str(state)}
        before = previous_jobs = media = None
        try:
            before = inventory(runner.cli('vm', 'list'), URI)
            previous_jobs = jobs(runner.cli('operation', 'list'))
            require(all(value in ('succeeded', 'failed', 'partial', 'canceled', 'recovery-required')
                        for value in previous_jobs.values()), 'active jobs; coordinate fixture execution first')
            media = media_listing(Path.home() / 'images')
            runner.save('before.json', {'vms': before, 'jobs': previous_jobs, 'media': media})
            runner.env['XDG_STATE_HOME'] = str(state)
            runner.env['VIRMILL_ASCII'] = '1'
            first = Terminal(runner, 'draft-edit', 80, 24)
            try:
                overview(first)
                open_iso_form(first)
                first.wait('Media name focus', lambda text: iso_form(text) and
                           re.search(r'>\s+Media name:', text) is not None, first.send(b'\t'))
                first.wait('Media name cleared', lambda text: iso_form(text) and
                           re.search(r'Media name:\s*\[\|?\]', text) is not None, first.send(b'\x15'))
                first.wait('Edited media name remains visible', lambda text: iso_form(text) and
                           SENTINEL in text, first.send(SENTINEL.encode('ascii')))
                graceful_quit(first)
            finally:
                first.close()
            report['saved'] = read_draft(state, SENTINEL, 'iso')
            second = Terminal(runner, 'draft-resume', 80, 24)
            try:
                overview(second)
                second.wait('Resume choice after new TUI process', lambda text:
                            'Resume saved setup' in text and 'Start new setup' in text and
                            'Enter Choose' in text and 'Tab Next option' in text and
                            'Enter Details' not in text,
                            second.send(b'i'))
                second.wait('Resume preserves blank-source browser', picker, second.send(b'\r'))
                second.wait('Saved media name restored in actual form', lambda text:
                            iso_form(text) and SENTINEL in text, second.send(b'\x1b'))
                graceful_quit(second)
            finally:
                second.close()
            report['resumed'] = read_draft(state, SENTINEL, 'iso')
            third = Terminal(runner, 'draft-start-new', 80, 24)
            try:
                overview(third)
                third.wait('Explicit new-setup choice available', lambda text:
                           'Resume saved setup' in text and 'Start new setup' in text, third.send(b'i'))
                third.wait('Start new selected', lambda text:
                           re.search(r'>\s*\[ Start new setup \]', text) is not None, third.send(b'\t'))
                third.wait('New setup opens blank browser', picker, third.send(b'\r'))
                third.wait('New import contains no old media name', lambda text:
                           'Import images' in text and 'File or folder:' in text and SENTINEL not in text,
                           third.send(b'\x1b'))
                graceful_quit(third)
            finally:
                third.close()
            report['startedNew'] = read_draft(state, '', 'auto')
            report['status'] = 'passed'
        except BaseException as error:
            report['error'] = str(error)
            raise
        finally:
            try:
                if before is not None:
                    after = inventory(runner.cli('vm', 'list'), URI)
                    after_jobs = jobs(runner.cli('operation', 'list'))
                    after_media = media_listing(Path.home() / 'images')
                    runner.save('after.json', {'vms': after, 'jobs': after_jobs, 'media': after_media})
                    require(after == before and after_jobs == previous_jobs and after_media == media,
                            'preexisting VM/jobs/source media changed')
                    report['preservation'] = 'all observed guests, jobs and source-media metadata unchanged'
            except BaseException as error:
                report['status'] = 'failed'
                report['preservationError'] = str(error)
                raise
            finally:
                runner.save('report.json', report)
        print(json.dumps(report, indent=2))
        return 0
    finally:
        os.close(binary_fd)


class DraftProbeTests(unittest.TestCase):
    def envelope(self):
        return {'apiVersion': 'virmill/v1', 'kind': 'TUIDraft', 'generation': 'a' * 32,
                'document': {'version': 1, 'connection': URI, 'state': 'editing',
                             'import': {'Kind': 'iso', 'MediaID': SENTINEL, 'Source': '', 'SelectedSource': ''}}}

    def test_ordinary_uninspected_values(self):
        check_document(self.envelope(), SENTINEL, 'iso')

    def test_submitted_or_inspected_not_claimed_as_pure_edit(self):
        for key, value in [('state', 'submitted'), ('operationID', 'a'), ('sourceBinding', 'f' * 64)]:
            envelope = self.envelope()
            envelope['document'][key] = value
            with self.assertRaises(RuntimeError):
                check_document(envelope, SENTINEL, 'iso')

    def test_cached_observations_refused(self):
        envelope = self.envelope()
        envelope['document']['import']['Report'] = {'source': '/unwanted'}
        with self.assertRaises(RuntimeError):
            check_document(envelope, SENTINEL, 'iso')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path)
    parser.add_argument('--execute-disposable', action='store_true')
    parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        require(args.root is None and not args.execute_disposable, 'self-test cannot choose native target')
        return not unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(DraftProbeTests)).wasSuccessful()
    require(args.execute_disposable and args.root is not None, 'explicit disposable execution/root required')
    return execute(args.root)


if __name__ == '__main__':
    sys.exit(main())
