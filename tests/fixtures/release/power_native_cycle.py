#!/usr/bin/env python3
"""Qualify everyday power actions on one Virmill-created, stopped test VM.

Run only on the owner-authorized disposable test host, with explicit
--execute-disposable, against a VM named on the command line that Virmill
created. Every action is a reviewed CLI plan applied as a detached job, then
read back: start, pause, resume, save, restore-saved, and force off from running
and from paused. A plan made before the VM changed state must be refused.
--graceful adds reboot and graceful stop for a guest that answers ACPI;
--unanswered-stop instead checks that a graceful stop never forces off a guest
that ignores it, and that force off works right after.

The VM ends stopped with no saved state and its persistent definition
unchanged. Other VMs, prior jobs and source media are compared before and
after. A job with an unknown outcome is kept for inspection and never replayed.
"""
import argparse
import hashlib
import json
import os
import socket
import stat
import subprocess
import time
from pathlib import Path

from autostart_tui_probe import TERMINAL_STATES
from tui_workspace_probe import Runner, canonical_path, require, strict_json, media_listing

ACKS = {
    'start': ['host-mutation'], 'pause': ['host-mutation'], 'resume': ['host-mutation'],
    'save': ['host-mutation'], 'restore-saved': ['host-mutation'], 'stop': ['host-mutation'],
    'hard-stop': ['data-loss-hard-stop', 'host-mutation'],
    'reboot': ['exclusive-lifecycle-writer', 'guest-reboot', 'host-mutation'],
}
# The state and saved-state flag each action must leave behind.
AFTER = {
    'start': ('running', False), 'pause': ('paused', False), 'resume': ('running', False),
    'save': ('stopped', True), 'restore-saved': ('running', False), 'reboot': ('running', False),
    'stop': ('stopped', False), 'hard-stop': ('stopped', False),
}
COMMAND = {'hard-stop': ('stop', '--hard')}


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


class Cycle:
    def __init__(self, runner, stage, uri, original):
        self.r, self.stage, self.uri, self.original = runner, stage, uri, original
        self.vm = original['key']['resourceUUID']
        self.resource = f'libvirt|{uri}|vm|{self.vm}'
        self.steps, self.jobs, self.count = [], [], 0

    def plan(self, action):
        verb, *flags = COMMAND.get(action, (action,))
        plan = self.r.cli('vm', verb, self.vm, *flags)
        require(plan['operation'] == 'vm.' + action, action + ': unexpected operation')
        require(plan['resourceIDs'] == [self.resource], action + ': plan names other resources')
        require(sorted(plan['acknowledgements']) == ACKS[action], action + ': unexpected acknowledgements')
        return plan

    def label(self, action):
        self.count += 1
        return f'{self.count:02d}-{action}'

    def wait(self, job):
        self.jobs.append(job['operationID'])
        deadline = time.monotonic() + 240
        while job['state'] not in TERMINAL_STATES and time.monotonic() < deadline:
            time.sleep(.5)
            job = self.r.cli('operation', 'show', job['operationID'])
        return job

    def apply(self, label, plan):
        self.r.save(label + '-plan.json', plan)
        acks = [part for ack in plan['acknowledgements'] for part in ('--ack', ack)]
        job = self.r.cli('plan', 'apply', plan['planID'], '--digest', plan['planDigest'], *acks,
                         '--detach', '--idempotency-key', f'{self.stage.name}-{label}')
        job = self.wait(job)
        self.r.save(label + '-job.json', job)
        return job

    def observe(self, label, expected):
        v = self.r.cli('vm', 'show', self.vm)
        self.r.save(label + '-vm.json', v)
        observed = (v['state'], v['hasManagedSave'])
        require(observed == expected, f'{label}: observed {observed}, expected {expected}')
        require(v['persistentXML'] == self.original['persistentXML'] and v['autostart'] is False,
                label + ': definition or automatic startup changed')

    def run(self, action):
        started, label = time.monotonic(), self.label(action)
        job = self.apply(label, self.plan(action))
        require(job['state'] == 'succeeded', f'{label}: job {job["state"]}; inspect it, do not replay')
        self.observe(label, AFTER[action])
        self.steps.append({'step': label, 'job': job['operationID'], 'seconds': round(time.monotonic() - started, 1)})

    def attempt(self, *arguments):
        """Run one CLI call that may fail; return its exit code and JSON envelope."""
        argv = ['--connection', self.uri, '--output', 'json', '--non-interactive', *arguments]
        process = self.r.launch(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            out, err = process.communicate(timeout=60)
        finally:
            if process.poll() is None:
                process.kill()
                process.wait()
        self.r.commands.append({'argv': [self.r.args.binary, *argv], 'exitCode': process.returncode,
                                'stdoutSHA256': hashlib.sha256(out).hexdigest(), 'stderrBytes': len(err)})
        self.r.save('commands.json', self.r.commands)
        try:
            return process.returncode, strict_json(out)
        except Exception:
            return process.returncode, None

    def stale(self):
        """A pause plan made while running must be refused once the VM is paused."""
        held = self.plan('pause')
        self.run('pause')
        label = self.label('stale-pause')
        acks = [part for ack in held['acknowledgements'] for part in ('--ack', ack)]
        code, envelope = self.attempt('plan', 'apply', held['planID'], '--digest', held['planDigest'], *acks,
                                      '--detach', '--idempotency-key', f'{self.stage.name}-{label}')
        self.r.save(label + '.json', {'exitCode': code, 'envelope': envelope})
        if code == 0:
            job = self.wait(envelope['data'])
            self.r.save(label + '-job.json', job)
            require(job['state'] == 'failed', f'{label}: stale plan ran to {job["state"]}')
            error = (job.get('error') or {}).get('code')
        else:
            error = ((envelope or {}).get('error') or {}).get('code')
        require(error in ('STALE_PLAN', 'UNSUPPORTED_CAPABILITY'), f'{label}: stale plan not refused: {error}')
        self.observe(label, ('paused', False))
        self.steps.append({'step': label, 'refusal': error, 'refusedBeforeJob': code != 0})

    def unanswered_stop(self):
        """A guest that ignores the request stays running; the stop forces nothing,
        fails plainly and leaves the VM free for force off."""
        label = self.label('unanswered-stop')
        job = self.apply(label, self.plan('stop'))
        error = (job.get('error') or {}).get('code')
        require(job['state'] == 'failed' and error == 'WAIT_TIMEOUT', f'{label}: ended {job["state"]} ({error})')
        self.observe(label, ('running', False))
        self.steps.append({'step': label, 'job': job['operationID'], 'jobState': job['state'],
                           'error': (job.get('error') or {}).get('code')})


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', required=True, type=Path)
    p.add_argument('--execute-disposable', required=True, action='store_true')
    p.add_argument('--connection', required=True, choices=('qemu:///system', 'qemu:///session'))
    p.add_argument('--vm', required=True, help='UUID of a stopped VM that Virmill created for tests')
    p.add_argument('--name', required=True, help="the same VM's name, as a second check")
    mode = p.add_mutually_exclusive_group()
    mode.add_argument('--graceful', action='store_true', help='the guest answers ACPI: also reboot and shut down')
    mode.add_argument('--unanswered-stop', action='store_true', help='the guest has no OS: a graceful stop must not force it off')
    p.add_argument('--boot-wait', type=int, default=90, help='seconds for the guest to boot before a graceful request')
    a = p.parse_args()
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(a.root.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private stage required')
    os.umask(0o077)
    out = stage / f'power-{a.vm[:8]}-{int(time.time())}'
    out.mkdir(mode=0o700)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    for name in ('virmill', 'virmilld'):
        with Path('/usr/bin/' + name).open('rb') as f:
            require(hashlib.file_digest(f, 'sha256').hexdigest() == expected[name], 'installed binary mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(os.dup(fd), 'rb') as f:
        require(hashlib.file_digest(f, 'sha256').hexdigest() == expected['virmill'], 'held binary mismatch')
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=a.connection), out, fd)
    mode = 'graceful' if a.graceful else 'unanswered-stop' if a.unanswered_stop else 'power-only'
    report = {'status': 'failed', 'connection': a.connection, 'vmID': a.vm, 'mode': mode, 'binaries': expected,
              'scope': 'native reviewed power plans and state readback on one test VM; guest readiness is not checked'}
    cycle = None
    try:
        before = r.cli('vm', 'list')
        jobs = r.cli('operation', 'list')
        media = media_listing(Path.home() / 'images')
        r.save('before-vms.json', before)
        r.save('before-jobs.json', jobs)
        require(all(j['state'] in TERMINAL_STATES for j in jobs), 'active job prevents test')
        original = next(v for v in before if v['key']['resourceUUID'] == a.vm)
        xml = original['persistentXML']
        require(original['name'] == a.name and original['state'] == 'stopped' and not original.get('hasManagedSave')
                and original['autostart'] is False and '<virmill:creation' in xml and 'urn:virmill:v1' in xml,
                'target is not a stopped, Virmill-created test VM with no saved state')
        others = [v for v in before if v['key']['resourceUUID'] != a.vm]
        cycle = Cycle(r, stage, a.connection, original)
        cycle.run('start')
        if a.graceful:
            time.sleep(a.boot_wait)
        cycle.run('pause')
        cycle.run('resume')
        cycle.run('save')
        cycle.run('restore-saved')
        cycle.stale()
        cycle.run('hard-stop')
        cycle.run('start')
        if a.unanswered_stop:
            cycle.unanswered_stop()
        cycle.run('hard-stop')
        if a.graceful:
            cycle.run('start')
            time.sleep(a.boot_wait)
            cycle.run('reboot')
            time.sleep(a.boot_wait)
            cycle.run('stop')
        after = r.cli('vm', 'list')
        r.save('after-vms.json', after)
        require([v for v in after if v['key']['resourceUUID'] != a.vm] == others, 'other VMs changed')
        afterjobs = r.cli('operation', 'list')
        require([j for j in afterjobs if j['operationID'] not in cycle.jobs] == jobs, 'prior job records changed')
        require(media_listing(Path.home() / 'images') == media, 'source media changed')
        report.update(status='passed', otherVMsJobsAndMediaPreserved=True, definitionUnchanged=True)
    except BaseException as e:
        report['error'] = repr(e)
    finally:
        if cycle:
            report.update(steps=cycle.steps, jobs=cycle.jobs)
        try:
            v = r.cli('vm', 'show', a.vm)
            report.update(finalState=v['state'], finalHasManagedSave=v['hasManagedSave'])
        except BaseException as e:
            report['observationError'] = repr(e)
        r.save('report.json', report)
        os.close(fd)
        print(json.dumps(report, sort_keys=True))
    raise SystemExit(report['status'] != 'passed')


if __name__ == '__main__':
    main()
