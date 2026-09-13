#!/usr/bin/env python3
"""Parent-operated missing-coordinator TUI probe (UX-01/UX-02 partial).

Requires --execute-disposable --root PRIVATE_STAGE containing binaries.json,
and adjacent tui_workspace_probe.py. Runs only as UID1000 on the authorized
<test-vm-login> host. Uses the installed frontend in a real 80x24 PTY with a fresh
private XDG_RUNTIME_DIR and XDG_STATE_HOME. No daemon is started/stopped, no
configuration is changed, and no host/guest operation is submitted. The missing
socket is intentional. This proves recovery guidance, not native virtualization
or successful service recovery. All screenshots and state remain on failure.
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

from tui_workspace_probe import Runner, Terminal, canonical_path, require, strict_json


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


URI = 'qemu:///system'
START = 'systemctl --user start virmilld.service'
ENABLE = 'systemctl --user enable virmilld.service'
STATUS = 'systemctl --user status virmilld.service'


def offline(text, section):
    return (re.search(r'Virmill\s*/\s*' + re.escape(section) + r'\b', text) is not None
            and URI in text and 'Background service unavailable' in text
            and 'VMs may still be running' in text and START in text
            and 'Retry connection' in text)


def honest(text):
    require(not re.search(r'\b0 virtual machines\b|Loading your virtual machines|\b0 VMs\b', text,
                          re.IGNORECASE), 'offline screen invented inventory or stayed loading')


def focused(text, label):
    shortcut = r'(?:r\s+)?' if label == 'Retry connection' else ''
    return re.search(r'>\s*\[\s*' + shortcut + re.escape(label) + r'\s*\]', text) is not None


def select_button(terminal, label):
    # The recovery card has exactly two buttons; use real keyboard focus.
    if not any(focused(terminal.screen.text(), name) for name in ('Retry connection', 'More help', 'Less help')):
        terminal.wait(label + ' button focus', lambda text: focused(text, 'Retry connection') or
                      focused(text, 'More help') or focused(text, 'Less help'), terminal.send(b'\t'))
    if not focused(terminal.screen.text(), label):
        terminal.wait(label + ' selected', lambda text: focused(text, label),
                      terminal.send(b'\x1b[C'))


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
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 1000 and 0 < info.st_size <= 65536,
            'bounded ordinary owned manifest required')
    expected = strict_json(manifest.read_bytes())
    require(isinstance(expected, dict), 'manifest object required')
    for name in ('virmill', 'virmilld'):
        require(isinstance(expected.get(name), str) and
                re.fullmatch('[0-9a-f]{64}', expected[name]) is not None, 'invalid binary SHA256')
        with Path('/usr/bin/' + name).open('rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected[name],
                    'installed binary differs: ' + name)
    binary_fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(binary_fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0,
                'frontend is not a trusted ordinary binary')
        with os.fdopen(os.dup(binary_fd), 'rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected['virmill'],
                    'held frontend hash differs')
        out = stage / 'coordinator-recovery'
        out.mkdir(mode=0o700)
        runtime, state = out / 'r', out / 's'
        runtime.mkdir(mode=0o700)
        state.mkdir(mode=0o700)
        missing_socket = runtime / 'virmill' / 'control.sock'
        require(len(os.fsencode(missing_socket)) <= 107, 'staged runtime socket path exceeds Unix limit')
        require(not os.path.lexists(missing_socket), 'isolated socket unexpectedly exists')
        runner = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, binary_fd)
        runner.env.update(XDG_RUNTIME_DIR=str(runtime), XDG_STATE_HOME=str(state), VIRMILL_ASCII='1')
        report = {'status': 'failed', 'acceptanceSupport': ['UX-01', 'UX-02'], 'binaries': expected,
                  'scope': 'installed 80x24 TUI missing-socket guidance, retry, help and Settings; no service or virtualization action',
                  'isolatedSocket': str(missing_socket)}
        terminal = None
        try:
            terminal = Terminal(runner, 'coordinator-offline-80x24', 80, 24)
            honest(terminal.wait('Overview explains missing coordinator', lambda text: offline(text, 'Overview')))
            select_button(terminal, 'Retry connection')
            terminal.send(b'\r')
            # An unchanged offline result may not redraw: do not require a new
            # frame merely to observe that retry still reports the same failure.
            honest(terminal.wait('Retry still reports unavailable service', lambda text: offline(text, 'Overview')))
            select_button(terminal, 'More help')
            help_screen = terminal.wait('Optional enable command and bounded startup help', lambda text:
                                        'Less help' in text and ENABLE in text and 'future sign-ins' in text,
                                        terminal.send(b'\r'))
            honest(help_screen)
            # Help can scroll in a narrow terminal. Do not mistake a visible
            # enable command for proof that the lower status guidance fits.
            help_text = help_screen
            if STATUS not in help_text or 'does not enable guest autostart' not in help_text:
                help_text += '\n' + terminal.wait('Remaining help is reachable by Page Down', lambda text:
                                                  STATUS in text and 'does not enable guest autostart' in text,
                                                  terminal.send(b'\x1b[6~'))
            require(STATUS in help_text and 'does not enable guest autostart' in help_text,
                    'help omitted service status or guest autostart boundary')
            select_button(terminal, 'Less help')
            honest(terminal.wait('Short guidance restored', lambda text: offline(text, 'Overview') and
                                 'More help' in text and ENABLE not in text, terminal.send(b'\r')))
            honest(terminal.wait('Settings also explains unavailable service', lambda text: offline(text, 'Settings'),
                                 terminal.send(b',')))
            terminal.send(b'q')
            deadline = time.monotonic() + 10
            while terminal.process.poll() is None and time.monotonic() < deadline:
                terminal.read(.05)
            require(terminal.process.poll() == 0, 'normal TUI quit failed')
            report.update(status='passed', overviewOffline=True, retryRemainsOffline=True,
                          helpCommands=[START, ENABLE, STATUS], settingsOffline=True, normalQuit=True)
        except BaseException as error:
            report['error'] = str(error)
            raise
        finally:
            if terminal is not None:
                terminal.close()
            report['socketStillAbsent'] = not os.path.lexists(missing_socket)
            if not report['socketStillAbsent']:
                report['status'] = 'failed'
            runner.save('report.json', report)
            require(report['socketStillAbsent'], 'unexpected coordinator socket created')
        print(json.dumps(report, indent=2))
        return 0
    finally:
        os.close(binary_fd)


class RecoveryProbeTests(unittest.TestCase):
    def test_offline_guidance_and_false_inventory(self):
        sample = 'Virmill / Overview ' + URI + '\nBackground service unavailable\nVMs may still be running\n' + START + '\nRetry connection'
        self.assertTrue(offline(sample, 'Overview'))
        self.assertFalse(offline(sample, 'Settings'))
        honest(sample)
        for misleading in ('0 virtual machines', 'Loading your virtual machines', '0 VMs'):
            with self.assertRaises(RuntimeError): honest(sample + '\n' + misleading)
        self.assertTrue(focused('>[ More help ]', 'More help'))
        self.assertFalse(focused('[ More help ]', 'More help'))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path)
    parser.add_argument('--execute-disposable', action='store_true')
    parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        require(args.root is None and not args.execute_disposable, 'self-test cannot select native actions')
        unittest.main(argv=[sys.argv[0]])
    else:
        require(args.root is not None and args.execute_disposable, 'explicit disposable root/run required')
        sys.exit(execute(args.root))
