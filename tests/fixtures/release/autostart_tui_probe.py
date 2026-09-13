#!/usr/bin/env python3
"""Installed 80x24 VM automatic-startup preview, without applying any change.

Parent-operated on the authorized disposable host only. Requires private stage
binaries.json and adjacent tui_workspace_probe.py. Selects the retained stopped
Fedora fixture verified in the owner's local host record, reads its actual
automatic-startup value, previews the opposite setting through the TUI and CLI,
compares complete service plans, returns to the retained draft, then cancels.
No daemon, VM, network, or autostart mutation. Durable plan previews are retained.
CORE-02/CORE-05/UX-01/UX-02 partial evidence; no reboot/startup behavior claim.
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

from tui_workspace_probe import Runner, Terminal, canonical_path, inventory, media_listing, require, strict_json


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


URI = 'qemu:///system'
# Verified from .virmill-local/test-host.json verifiedGuestToolsFixture.vmID.
IDENTITY = '71001e0c-e99d-4e26-a886-0553533c5fa0'
NAME = 'virmill-tools-71001e0c'
TERMINAL_STATES = {'succeeded', 'failed', 'partial', 'canceled', 'recovery-required'}


def normalize(plan, old):
    require(plan['operation'] == 'vm.autostart' and plan['actorUID'] == 1000 and
            plan['connectionID'] == URI and plan['resourceIDs'] == ['libvirt|' + URI + '|vm|' + IDENTITY],
            'plan operation or exact target differs')
    r = plan['review']
    require(r['action'] == 'autostart' and r['vmID'] == IDENTITY and r['vmName'] == NAME and
            r['beforeAutostart'] is old and r['afterAutostart'] is (not old) and
            r['requested'] == {'enabled': not old} and r['persistentEdit'] is True and
            r['immediatePowerChange'] is False and r['requiresShutdown'] is False and
            r['diskDeletion'] is False, 'preview scope or before/after values differ')
    result = copy.deepcopy(plan)
    # Everything else, including input digest, fingerprints, steps, risk,
    # grants and acknowledgements, must agree across the two frontends.
    for key in ('planID', 'planDigest', 'createdAt', 'expiresAt'):
        result.pop(key, None)
    return result


def execute(root):
    require(authorized_test_host() and
            os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(root.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stat.S_ISDIR(stage.lstat().st_mode) and
            stage.stat().st_uid == 1000 and stat.S_IMODE(stage.stat().st_mode) == 0o700,
            'private ordinary staged root required')
    manifest = stage / 'binaries.json'; info = manifest.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 1000 and 0 < info.st_size <= 65536,
            'bounded ordinary owned manifest required')
    expected = strict_json(manifest.read_bytes())
    for name in ('virmill', 'virmilld'):
        require(isinstance(expected.get(name), str) and re.fullmatch('[0-9a-f]{64}', expected[name]),
                'invalid binary hash')
        with Path('/usr/bin/' + name).open('rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected[name], 'installed binary differs')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0,
                'frontend is not a trusted ordinary binary')
        with os.fdopen(os.dup(fd), 'rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected['virmill'], 'held binary differs')
        out = stage / 'autostart-tui'; out.mkdir(mode=0o700)
        state = out / 'state'; state.mkdir(mode=0o700)
        runner = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, fd)
        runner.env.update(XDG_STATE_HOME=str(state), VIRMILL_ASCII='1')
        report = {'status': 'failed', 'vmID': IDENTITY, 'binaries': expected,
                  'acceptanceSupport': ['CORE-02', 'CORE-05', 'UX-01', 'UX-02'],
                  'scope': 'actual installed TUI/CLI automatic-startup preview only; no apply or reboot/startup behavior claim'}
        def native_snapshot():
            result = {}
            for args in [('dumpxml', IDENTITY, '--inactive'), ('dumpxml', IDENTITY), ('dominfo', IDENTITY), ('domstate', IDENTITY)]:
                process = subprocess.run(['/usr/bin/virsh', '--connect', URI, *args],
                                         stdin=subprocess.DEVNULL, capture_output=True, timeout=20,
                                         env=dict(os.environ, LC_ALL='C'))
                require(process.returncode == 0 and len(process.stdout) + len(process.stderr) <= 2 << 20,
                        'bounded native read failed')
                result[' '.join(args)] = process.stdout.decode()
            return result
        def snapshot():
            values = runner.cli('vm', 'list')
            return {'vms': inventory(values, URI), 'autostart': {v['key']['resourceUUID']: v['autostart'] for v in values},
                    'jobs': runner.cli('operation', 'list'), 'native': native_snapshot(),
                    'media': media_listing(Path.home() / 'images')}
        before = None; terminal = None
        try:
            before = snapshot(); runner.save('before.json', before)
            require(all(j['state'] in TERMINAL_STATES for j in before['jobs']), 'another operation is active')
            vm = runner.cli('vm', 'show', IDENTITY); runner.save('vm-before.json', vm)
            require(vm['key']['resourceUUID'] == IDENTITY and vm['name'] == NAME and vm['state'] == 'stopped'
                    and vm['persistentXML'] and type(vm['autostart']) is bool, 'retained stopped fixture differs')
            old = vm['autostart']; current, requested = ('On', 'Off') if old else ('Off', 'On')
            native_info = before['native']['dominfo ' + IDENTITY]
            require(re.search(r'^Autostart:\s*' + ('enable' if old else 'disable') + r'\s*$', native_info, re.M),
                    'CLI autostart differs from native libvirt')
            terminal = Terminal(runner, 'autostart-80x24', 80, 24)
            def wait(label, predicate, key=None):
                return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
            wait('Overview', lambda s: 'Virtual machines' in s)
            wait('VM table', lambda s: 'NAME' in s and 'STATE' in s, b'2')
            wait('Filter VM', lambda s: 'Enter Keep filter' in s, b'/')
            wait('Exact retained VM selected', lambda s: NAME in s and re.search(r'row 1 of 1\b', s), NAME.encode())
            wait('Keep exact selection', lambda s: 'Enter Keep filter' not in s, b'\r')
            wait('Exact VM details', lambda s: 'VM details' in s and NAME in s and IDENTITY in s, b'\r')
            wait('More VM tasks', lambda s: 'VMs / More tasks' in s and NAME in s, b'a')
            wait('Task search focused', lambda s: 'Enter Keep matches' in s, b'/')
            wait('Automatic startup action found', lambda s: 'Change VM automatic startup' in s and
                 'No matching tasks' not in s, b'automatic startup')
            wait('Keep matching action', lambda s: 'Enter Keep matches' not in s and 'Change VM automatic startup' in s, b'\r')
            def form(s, value):
                return 'Start automatically' in s and NAME in s and 'Current: ' + current in s and 'Requested: < ' + value + ' >' in s
            wait('Fresh current automatic-startup value', lambda s: form(s, current), b'\r')
            wait('Opposite requested policy', lambda s: form(s, requested), b' ')
            wait('Preview button focus', lambda s: re.search(r'>\s*\[ Preview \]', s) is not None, b'\t')
            wait('Automatic-startup plan review', lambda s: 'Nothing has been applied' in s, b'\r')
            match = None
            for page in range(12):
                match = re.search(r'Plan ID:\s*([0-9a-f-]{36})', terminal.screen.text())
                if match: break
                previous = terminal.screen.text()
                wait('Read plan identity ' + str(page), lambda s: s != previous, b'\x1b[6~')
            require(match, 'plan identity missing')
            tui_plan = runner.cli('plan', 'show', match.group(1)); runner.save('tui-plan.json', tui_plan)
            cli_plan = runner.cli('vm', 'autostart', IDENTITY, '--input', json.dumps({'enabled': not old}))
            runner.save('cli-plan.json', cli_plan)
            require(normalize(tui_plan, old) == normalize(cli_plan, old), 'CLI/TUI plan semantics differ')
            wait('Back retains requested policy', lambda s: form(s, requested), b'\x1b')
            wait('Cancel returns to exact VM details', lambda s: 'VM details' in s and NAME in s and IDENTITY in s
                 and 'Requested:' not in s, b'\x1b')
            terminal.send(b'q'); deadline = time.monotonic() + 10
            while terminal.process.poll() is None and time.monotonic() < deadline: terminal.read(.05)
            require(terminal.process.poll() == 0, 'normal TUI quit failed')
            report.update(status='passed', beforeAutostart=old, requestedAutostart=not old,
                          tuiPlanID=tui_plan['planID'], cliPlanID=cli_plan['planID'], semanticParity=True,
                          backRetainedChoice=True, canceledWithoutApply=True)
        except BaseException as error:
            report['error'] = repr(error)
        finally:
            if terminal is not None: terminal.close()
            try:
                if before is not None:
                    after = snapshot(); runner.save('after.json', after)
                    require(before == after, 'guests, native XML/state/autostart, jobs or media changed')
                    report['preserved'] = True
            except BaseException as error:
                report.update(status='failed', preservationError=repr(error))
            runner.save('report.json', report)
        print(json.dumps(report, indent=2))
        return 0 if report['status'] == 'passed' else 1
    finally:
        os.close(fd)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--execute-disposable', action='store_true')
    args = parser.parse_args()
    require(args.execute_disposable, 'explicit disposable run required')
    sys.exit(execute(args.root))
