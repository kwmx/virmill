#!/usr/bin/env python3
"""Qualify next-boot CPU/RAM edits on a running VM (ADR 0061) natively.

Run only on the owner-authorized disposable test host, with explicit
--execute-disposable, against a stopped VM that Virmill created and whose guest
answers ACPI. Every change is a reviewed CLI plan applied as a detached job.

1. Start the VM. While it runs, raise next-boot CPU by one and RAM by 512 MiB:
   the running values must stay, the saved ones change.
2. Shut down and start: the new values must now be live.
3. While it runs, plan a restore of the original values and a second, different
   edit. Shut down, then apply the restore: a plan made while running stays
   valid after the VM stops. The second plan must be refused as stale.

The VM ends stopped with its original CPU and RAM. Other VMs, prior jobs and
source media are compared before and after. Unknown job outcomes are kept for
inspection and never replayed.
"""
import argparse
import hashlib
import json
import os
import socket
import stat
import time
from pathlib import Path

from autostart_tui_probe import TERMINAL_STATES
from power_native_cycle import Cycle
from tui_workspace_probe import Runner, canonical_path, require, strict_json, media_listing

SET_ACKS = ['exclusive-configuration-writer', 'host-mutation']
POWER_ACKS = {'start': ['host-mutation'], 'stop': ['host-mutation']}


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


class Edits(Cycle):
    """Reuses the power probe's plan, apply and refusal helpers; the saved
    definition is expected to change here, so readback checks resources."""

    def power(self, action, state):
        label = self.label(action)
        plan = self.r.cli('vm', action, self.vm)
        require(plan['operation'] == 'vm.' + action and sorted(plan['acknowledgements']) == POWER_ACKS[action], label + ': unexpected plan')
        job = self.apply(label, plan)
        require(job['state'] == 'succeeded', f'{label}: job {job["state"]}; inspect it, do not replay')
        require(self.r.cli('vm', 'show', self.vm)['state'] == state, label + ': unexpected state')
        self.steps.append({'step': label, 'job': job['operationID']})

    def plan_set(self, vcpus, memory_mib):
        plan = self.r.cli('vm', 'set', self.vm, '--input', json.dumps({'vcpus': vcpus, 'memoryMiB': memory_mib, 'applyMode': 'next-boot'}))
        require(plan['operation'] == 'vm.configure-resources' and plan['resourceIDs'] == [self.resource], 'unexpected edit plan')
        require(sorted(plan['acknowledgements']) == SET_ACKS, 'unexpected edit acknowledgements')
        require('keeps running' in plan['estimates']['notes'], 'review does not say the VM keeps running')
        return plan

    def resources(self, label):
        view = self.r.cli('vm', 'resources', 'show', self.vm)
        self.r.save(label + '-resources.json', view)
        return view

    def edit(self, label, plan):
        job = self.apply(label, plan)
        require(job['state'] == 'succeeded', f'{label}: job {job["state"]}; inspect it, do not replay')
        self.steps.append({'step': label, 'job': job['operationID']})


def values(view, side):
    part = view[side] or {}
    return part.get('vcpus'), part.get('memoryBytes')


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', required=True, type=Path)
    p.add_argument('--execute-disposable', required=True, action='store_true')
    p.add_argument('--connection', required=True, choices=('qemu:///system', 'qemu:///session'))
    p.add_argument('--vm', required=True, help='UUID of a stopped VM that Virmill created for tests')
    p.add_argument('--name', required=True, help="the same VM's name, as a second check")
    p.add_argument('--boot-wait', type=int, default=90, help='seconds for the guest to boot before a graceful stop')
    a = p.parse_args()
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(a.root.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private stage required')
    os.umask(0o077)
    out = stage / f'resources-{a.vm[:8]}-{int(time.time())}'
    out.mkdir(mode=0o700)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    for name in ('virmill', 'virmilld'):
        with Path('/usr/bin/' + name).open('rb') as f:
            require(hashlib.file_digest(f, 'sha256').hexdigest() == expected[name], 'installed binary mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=a.connection), out, fd)
    report = {'status': 'failed', 'connection': a.connection, 'vmID': a.vm, 'binaries': expected,
              'scope': 'native next-boot CPU/RAM edits on a running VM; guest readiness is not checked'}
    edits = None
    try:
        before = r.cli('vm', 'list')
        jobs = r.cli('operation', 'list')
        media = media_listing(Path.home() / 'images')
        r.save('before-vms.json', before)
        require(all(j['state'] in TERMINAL_STATES for j in jobs), 'active job prevents test')
        original = next(v for v in before if v['key']['resourceUUID'] == a.vm)
        xml = original['persistentXML']
        require(original['name'] == a.name and original['state'] == 'stopped' and not original.get('hasManagedSave')
                and original['autostart'] is False and '<virmill:creation' in xml and 'urn:virmill:v1' in xml,
                'target is not a stopped, Virmill-created test VM with no saved state')
        others = [v for v in before if v['key']['resourceUUID'] != a.vm]
        edits = Edits(r, stage, a.connection, original)
        first = edits.resources('00-original')
        cpu, memory = values(first, 'persistent')
        require(isinstance(cpu, int) and isinstance(memory, int) and memory % (1 << 20) == 0 and first['canEditCPU'] and first['canEditMemory'], 'original values not editable')
        mib = memory >> 20
        report['original'] = {'vcpus': cpu, 'memoryMiB': mib}

        edits.power('start', 'running')
        time.sleep(a.boot_wait)
        edits.edit(edits.label('raise-while-running'), edits.plan_set(cpu + 1, mib + 512))
        view = edits.resources(f'{edits.count:02d}-after-raise')
        require(values(view, 'live') == (cpu, memory), 'running values changed')
        require(values(view, 'persistent') == (cpu + 1, (mib + 512) << 20), 'saved values not changed')
        require(view['requiresShutdown'] and view['state'] == 'running', 'view does not say a shutdown is needed')
        report['runningValuesKept'] = True

        edits.power('stop', 'stopped')
        edits.power('start', 'running')
        view = edits.resources(f'{edits.count:02d}-after-restart')
        require(values(view, 'live') == (cpu + 1, (mib + 512) << 20), 'new values not live after shutdown and start')
        report['appliedAfterShutdownAndStart'] = True

        time.sleep(a.boot_wait)
        restore = edits.plan_set(cpu, mib)
        other = edits.plan_set(cpu + 2, mib)
        edits.power('stop', 'stopped')
        edits.edit(edits.label('restore-after-stop'), restore)
        report['planValidAfterStop'] = True
        label = edits.label('stale-edit')
        acks = [part for ack in other['acknowledgements'] for part in ('--ack', ack)]
        code, envelope = edits.attempt('plan', 'apply', other['planID'], '--digest', other['planDigest'], *acks,
                                       '--detach', '--idempotency-key', f'{edits.key}-{label}')
        r.save(label + '.json', {'exitCode': code, 'envelope': envelope})
        if code == 0:
            job = edits.wait(envelope['data'])
            error = (job.get('error') or {}).get('code') if job['state'] == 'failed' else job['state']
        else:
            error = ((envelope or {}).get('error') or {}).get('code')
        require(error == 'STALE_PLAN', f'{label}: second plan not refused as stale: {error}')
        edits.steps.append({'step': label, 'refusal': error, 'refusedBeforeJob': code != 0})

        final = edits.resources(f'{edits.count:02d}-final')
        require(values(final, 'persistent') == (cpu, memory) and final['state'] == 'stopped', 'original values not restored')
        shown = r.cli('vm', 'show', a.vm)
        report['definitionIdenticalToOriginal'] = shown['persistentXML'] == xml
        after = r.cli('vm', 'list')
        r.save('after-vms.json', after)
        require([v for v in after if v['key']['resourceUUID'] != a.vm] == others, 'other VMs changed')
        afterjobs = r.cli('operation', 'list')
        require([j for j in afterjobs if j['operationID'] not in edits.jobs] == jobs, 'prior job records changed')
        require(media_listing(Path.home() / 'images') == media, 'source media changed')
        report.update(status='passed', staleRefused=True, otherVMsJobsAndMediaPreserved=True)
    except BaseException as e:
        report['error'] = repr(e)
    finally:
        if edits:
            report.update(steps=edits.steps, jobs=edits.jobs)
        try:
            v = r.cli('vm', 'show', a.vm)
            report['finalState'] = v['state']
        except BaseException as e:
            report['observationError'] = repr(e)
        r.save('report.json', report)
        os.close(fd)
        print(json.dumps(report, sort_keys=True))
    raise SystemExit(report['status'] != 'passed')


if __name__ == '__main__':
    main()
