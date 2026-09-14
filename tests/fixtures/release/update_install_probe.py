#!/usr/bin/env python3
"""Run `virmill update` for real on the authorized disposable VM.

Requires --execute-disposable --root PRIVATE_STAGE on the authorized UID 1000
test host, an installed Virmill older than the newest GitHub release, and the
expected version, revision and binaries.json of that release. Runs
/usr/bin/virmill update --yes in a terminal with a private cache and config,
answers dnf's confirmation, and never answers a password prompt. Then checks the
installed packages, binary hashes, version, revision and coordinator restart.
Guest and network definitions, source-media metadata, jobs and the inactive
helper policy must be unchanged. No guest is started.
--self-test checks the prompt handling and version mapping only.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import pty
import re
import select
import socket
import subprocess
import sys
import time
import unittest


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def rpm_version(version):
    match = re.fullmatch(r'(\d+\.\d+\.\d+)-beta\.([1-9]\d*)', version)
    require(match is not None, 'expected a beta version, got ' + repr(version))
    return f'{match.group(1)}-0.beta.{match.group(2)}'


class Prompts:
    """Answers each dnf confirmation once; refuses any password prompt."""

    def __init__(self):
        self.seen, self.answered = '', 0

    def feed(self, text):
        self.seen += text
        lowered = self.seen.lower()
        require(not re.search(r'\[sudo\] password|password for |password:', lowered),
                'a password was requested; this fixture never enters credentials')
        replies = []
        while self.answered < len(re.findall(r'is this ok \[y/n\]', lowered)):
            self.answered += 1
            replies.append(b'y\n')
        return replies


def run(args, env=None, check=True):
    r = subprocess.run(args, capture_output=True, text=True, timeout=300, env=env)
    if check and r.returncode:
        raise RuntimeError(f'{args!r} failed: {r.stderr.strip()[:500]}')
    return r


def sha(path):
    with open(path, 'rb') as f:
        return hashlib.file_digest(f, 'sha256').hexdigest()


def inventory():
    out = {}
    for kind, listing in [('domain', ['list', '--all', '--uuid']), ('network', ['net-list', '--all', '--uuid'])]:
        for identity in run(['virsh', '-c', 'qemu:///system', *listing]).stdout.split():
            verb = 'dumpxml' if kind == 'domain' else 'net-dumpxml'
            out[kind + ':' + identity] = hashlib.sha256(run(['virsh', '-c', 'qemu:///system', verb, identity, '--inactive']).stdout.encode()).hexdigest()
            if kind == 'domain':
                require(run(['virsh', '-c', 'qemu:///system', 'domstate', identity]).stdout.strip() == 'shut off', 'a guest is running')
    return out


def media():
    base = Path.home() / 'images'
    return {str(f.relative_to(base)): [f.stat().st_ino, f.stat().st_size, f.stat().st_mtime_ns] for f in base.rglob('*') if f.is_file()}


def helper():
    policy = run(['sudo', '-n', 'sha256sum', '/etc/virmill/helper-policy.json']).stdout.split()[0]
    units = run(['systemctl', 'is-active', 'virmill-host-helper.socket', 'virmill-host-helper.service'], check=False).stdout.split()
    require(units == ['inactive', 'inactive'], 'host helper is active')
    return policy


def run_update(env, transcript_path):
    """Runs virmill update --yes in a PTY and returns (exit code, output, answers)."""
    master, slave = pty.openpty()
    proc = subprocess.Popen(['/usr/bin/virmill', 'update', '--yes'], stdin=slave, stdout=slave, stderr=slave, env=env, start_new_session=True)
    os.close(slave)
    prompts, output = Prompts(), bytearray()
    deadline = time.monotonic() + 1800
    try:
        while True:
            require(time.monotonic() < deadline, 'virmill update did not finish within 30 minutes')
            if select.select([master], [], [], 0.2)[0]:
                try:
                    data = os.read(master, 65536)
                except OSError:
                    data = b''
                if data:
                    output.extend(data)
                    require(len(output) < 4 << 20, 'update output is too large')
                    for reply in prompts.feed(data.decode('utf-8', 'replace')):
                        os.write(master, reply)
                    continue
            if proc.poll() is not None:
                break
    finally:
        if proc.poll() is None:
            proc.terminate()
            proc.wait(timeout=30)
        os.close(master)
        Path(transcript_path).write_bytes(bytes(output))
    return proc.returncode, output.decode('utf-8', 'replace'), prompts.answered


def execute(a):
    require(authorized_test_host() and os.getuid() == 1000, 'not the authorized test host')
    root = a.root.resolve(strict=True)
    require(root.is_relative_to(Path.home() / 'virmill-tests'), 'stage must be under ~/virmill-tests')
    out = root / 'update-install'
    out.mkdir(mode=0o700)
    env = dict(os.environ, XDG_CACHE_HOME=str(out / 'cache'), XDG_CONFIG_HOME=str(out / 'config'), VIRMILL_UPDATE_CHECK='1', TERM='xterm')
    expected = json.loads(Path(a.expect_binaries).read_text())
    report = {'status': 'failed', 'scope': 'virmill update from an older test build to a published GitHub release; no guest started',
              'fromVersion': a.from_version, 'expectVersion': a.expect_version, 'expectRevision': a.expect_revision}

    def cli(*args):
        value = json.loads(run(['/usr/bin/virmill', *args, '--output', 'json', '--non-interactive'], env=env).stdout)
        require(value['error'] is None, f'{args!r}: {value["error"]}')
        return value['data']

    def jobs():
        return sorted((j['operationID'], j['state']) for j in cli('operation', 'list'))

    def coordinator():
        return run(['systemctl', '--user', 'show', 'virmilld.service', '-p', 'ActiveState', '-p', 'MainPID', '--value']).stdout.split()

    baseline = None
    try:
        baseline = {'inventory': inventory(), 'media': media(), 'helperPolicy': helper(), 'jobs': jobs()}
        require(all(state in ('succeeded', 'failed', 'partial', 'canceled', 'recovery-required') for _, state in baseline['jobs']), 'a job is unfinished')
        before = cli('version')
        require(before['version'] == a.from_version, f'installed version is {before["version"]}, not {a.from_version}')
        report['packagesBefore'] = run(['rpm', '-q', 'virmill', 'virmill-host-helper']).stdout.split()
        check = cli('update', 'check')
        require(check.get('latest', {}).get('version') == a.expect_version, f'GitHub offered {check.get("latest")}')
        report['releasePage'] = check['latest'].get('page')
        state_before = coordinator()
        report['coordinatorBefore'] = state_before
        code, output, answers = run_update(env, out / 'update-transcript.txt')
        report.update(exitCode=code, dnfConfirmations=answers)
        require(code == 0, f'virmill update exited with {code}: {output[-800:]}')
        require('Verified: each file matches SHA256SUMS' in output and f'Virmill {a.expect_version} is installed.' in output, 'update did not report verification and installation')
        report['packagesAfter'] = run(['rpm', '-q', 'virmill', 'virmill-host-helper']).stdout.split()
        wanted = rpm_version(a.expect_version)
        require(report['packagesAfter'] == [f'virmill-{wanted}.x86_64', f'virmill-host-helper-{wanted}.x86_64'], 'installed packages differ')
        paths = {'virmill': '/usr/bin/virmill', 'virmilld': '/usr/bin/virmilld', 'virmill-host-helper': '/usr/libexec/virmill-host-helper'}
        report['installedBinarySHA256'] = {name: sha(path) for name, path in paths.items()}
        require(report['installedBinarySHA256'] == expected, 'installed binaries differ from the release')
        for _ in range(100):
            if Path(os.environ.get('XDG_RUNTIME_DIR', '/run/user/1000'), 'virmill', 'control.sock').exists():
                break
            time.sleep(0.1)
        after = cli('version')
        require(after['version'] == a.expect_version and after['revision'] == a.expect_revision, f'coordinator reports {after}')
        report['coordinatorAfter'] = coordinator()
        require(report['coordinatorAfter'][0] == 'active' and report['coordinatorAfter'][1] != state_before[1], 'coordinator was not restarted')
        report['downloadedFiles'] = sorted(p.name for p in (out / 'cache' / 'virmill' / 'updates' / a.expect_version).iterdir())
        report['status'] = 'passed'
    except BaseException as error:
        report['failure'] = str(error)
    finally:
        try:
            report['guestAndNetworkDefinitionsPreserved'] = baseline is not None and inventory() == baseline['inventory']
            report['sourceMediaMetadataPreserved'] = baseline is not None and media() == baseline['media']
            report['helperPolicyPreservedAndInactive'] = baseline is not None and helper() == baseline['helperPolicy']
            report['jobsPreserved'] = baseline is not None and jobs() == baseline['jobs']
            if not all(report[k] for k in ('guestAndNetworkDefinitionsPreserved', 'sourceMediaMetadataPreserved', 'helperPolicyPreservedAndInactive', 'jobsPreserved')):
                report['status'] = 'failed'
        except BaseException as error:
            report['preservationError'] = str(error)
            report['status'] = 'failed'
        (out / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(report, sort_keys=True))
    return report['status'] == 'passed'


class ProbeTests(unittest.TestCase):
    def test_answers_each_confirmation_once(self):
        p = Prompts()
        self.assertEqual(p.feed('Transaction Summary\nIs this ok [y/N]: '), [b'y\n'])
        self.assertEqual(p.feed('more output'), [])
        self.assertEqual(p.feed('\nIs this ok [y/N]: '), [b'y\n'])

    def test_refuses_password_prompts(self):
        with self.assertRaises(RuntimeError):
            Prompts().feed('[sudo] password for tester: ')

    def test_rpm_version(self):
        self.assertEqual(rpm_version('1.0.0-beta.3'), '1.0.0-0.beta.3')
        with self.assertRaises(RuntimeError):
            rpm_version('1.0.0')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--self-test', action='store_true')
    parser.add_argument('--execute-disposable', action='store_true')
    parser.add_argument('--root', type=Path)
    parser.add_argument('--from-version')
    parser.add_argument('--expect-version')
    parser.add_argument('--expect-revision')
    parser.add_argument('--expect-binaries', type=Path)
    a = parser.parse_args()
    if a.self_test:
        unittest.main(argv=[sys.argv[0]])
    elif a.execute_disposable and a.root and a.from_version and a.expect_version and a.expect_revision and a.expect_binaries:
        raise SystemExit(0 if execute(a) else 1)
    else:
        parser.error('explicit disposable execution, a private root and the expected versions are required')
