#!/usr/bin/env python3
"""Qualify growing a stopped VM's disk (ADR 0062) natively.

Run only on the owner-authorized disposable test host, with explicit
--execute-disposable, against a stopped VM that Virmill created. Every change
is a reviewed CLI plan applied as a detached job.

1. Plan two larger sizes for one disk from its original size. Apply the first;
   the second, made against the original disk, must be refused as stale.
2. Plan the second size again and apply it. Planning the same size once more
   must be refused before any job.
3. After each grow, libvirt's volume XML must report the new capacity, read-only.
4. With --boot-check, start the VM and shut it down gracefully afterwards.

The VM's definition must not change. Other VMs, prior jobs and source media are
compared before and after. Unknown job outcomes are kept, never replayed.
"""
import argparse
import hashlib
import json
import os
import re
import socket
import stat
import subprocess
import time
from pathlib import Path

from autostart_tui_probe import TERMINAL_STATES
from power_native_cycle import Cycle
from tui_workspace_probe import Runner, canonical_path, require, strict_json, media_listing

GROW_ACKS = (['exclusive-storage-writer', 'host-mutation'], ['exclusive-storage-writer', 'host-mutation', 'pool-overcommit'])


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


def volume_capacity(uri, path):
    """Capacity in bytes from libvirt's volume XML; a read-only query."""
    out = subprocess.run(['virsh', '-c', uri, 'vol-dumpxml', path], capture_output=True, text=True, timeout=60, check=True).stdout
    found = re.search(r"<capacity unit='bytes'>(\d+)</capacity>", out)
    require(found is not None, 'volume capacity unreadable')
    return int(found.group(1))


class Grow(Cycle):
    def plan_grow(self, target, size):
        plan = self.r.cli('vm', 'disk', 'grow', self.vm, '--input', json.dumps({'target': target, 'sizeGiB': size}))
        require(plan['operation'] == 'vm.disk.grow-v1' and self.resource in plan['resourceIDs'], 'unexpected grow plan')
        require(sorted(plan['acknowledgements']) in GROW_ACKS, 'unexpected grow acknowledgements')
        review = plan['review']
        require(review['target'] == target and review['afterCapacityBytes'] == size << 30 and review['guestFilesystemsGrown'] is False, 'review differs from the request')
        return plan

    def grow(self, plan, uri):
        label = self.label('grow')
        job = self.apply(label, plan)
        require(job['state'] == 'succeeded', f'{label}: job {job["state"]}; inspect it, do not replay')
        capacity = volume_capacity(uri, plan['review']['path'])
        require(capacity == plan['review']['afterCapacityBytes'], f'{label}: volume reports {capacity} bytes')
        self.observe(label, ('stopped', False))
        self.steps.append({'step': label, 'job': job['operationID'], 'capacityBytes': capacity})

    def refused_apply(self, plan, want):
        label = self.label('stale-grow')
        acks = [part for ack in plan['acknowledgements'] for part in ('--ack', ack)]
        code, envelope = self.attempt('plan', 'apply', plan['planID'], '--digest', plan['planDigest'], *acks,
                                      '--detach', '--idempotency-key', f'{self.key}-{label}')
        self.r.save(label + '.json', {'exitCode': code, 'envelope': envelope})
        if code == 0:
            job = self.wait(envelope['data'])
            error = (job.get('error') or {}).get('code') if job['state'] == 'failed' else job['state']
        else:
            error = ((envelope or {}).get('error') or {}).get('code')
        require(error == want, f'{label}: expected {want}, got {error}')
        self.steps.append({'step': label, 'refusal': error, 'refusedBeforeJob': code != 0})

    def refused_plan(self, target, size):
        label = self.label('same-size')
        code, envelope = self.attempt('vm', 'disk', 'grow', self.vm, '--input', json.dumps({'target': target, 'sizeGiB': size}))
        self.r.save(label + '.json', {'exitCode': code, 'envelope': envelope})
        error = (envelope or {}).get('error') or {}
        require(code != 0 and 'already' in error.get('message', ''), f'{label}: same size was not refused')
        self.steps.append({'step': label, 'refusal': error.get('code')})


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', required=True, type=Path)
    p.add_argument('--execute-disposable', required=True, action='store_true')
    p.add_argument('--connection', required=True, choices=('qemu:///system', 'qemu:///session'))
    p.add_argument('--vm', required=True, help='UUID of a stopped VM that Virmill created for tests')
    p.add_argument('--name', required=True, help="the same VM's name, as a second check")
    p.add_argument('--target', required=True, help='the disk to grow, such as vda or sda')
    p.add_argument('--sizes', required=True, help='two increasing new sizes in GiB, such as 5,6')
    p.add_argument('--boot-check', action='store_true', help='start the VM after growing and shut it down gracefully')
    p.add_argument('--boot-wait', type=int, default=90)
    a = p.parse_args()
    first, second = (int(x) for x in a.sizes.split(','))
    require(0 < first < second, 'sizes must increase')
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(a.root.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private stage required')
    os.umask(0o077)
    out = stage / f'grow-{a.vm[:8]}-{int(time.time())}'
    out.mkdir(mode=0o700)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    for name in ('virmill', 'virmilld'):
        with Path('/usr/bin/' + name).open('rb') as f:
            require(hashlib.file_digest(f, 'sha256').hexdigest() == expected[name], 'installed binary mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=a.connection), out, fd)
    report = {'status': 'failed', 'connection': a.connection, 'vmID': a.vm, 'target': a.target, 'binaries': expected,
              'scope': 'native disk grow on a stopped VM; partitions inside the guest are not checked'}
    grow = None
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
        grow = Grow(r, stage, a.connection, original)

        plan_first = grow.plan_grow(a.target, first)
        plan_second = grow.plan_grow(a.target, second)
        report['originalCapacityBytes'] = plan_first['review']['beforeCapacityBytes']
        require(volume_capacity(a.connection, plan_first['review']['path']) == report['originalCapacityBytes'], 'review and volume disagree')
        grow.grow(plan_first, a.connection)
        grow.refused_apply(plan_second, 'STALE_PLAN')
        grow.grow(grow.plan_grow(a.target, second), a.connection)
        grow.refused_plan(a.target, second)
        report['grownTo'] = second << 30

        if a.boot_check:
            grow.run('start')
            time.sleep(a.boot_wait)
            grow.run('stop')
            report['bootedAfterGrow'] = True

        after = r.cli('vm', 'list')
        r.save('after-vms.json', after)
        require([v for v in after if v['key']['resourceUUID'] != a.vm] == others, 'other VMs changed')
        afterjobs = r.cli('operation', 'list')
        require([j for j in afterjobs if j['operationID'] not in grow.jobs] == jobs, 'prior job records changed')
        require(media_listing(Path.home() / 'images') == media, 'source media changed')
        report.update(status='passed', definitionUnchanged=True, staleRefused=True, otherVMsJobsAndMediaPreserved=True)
    except BaseException as e:
        report['error'] = repr(e)
    finally:
        if grow:
            report.update(steps=grow.steps, jobs=grow.jobs)
        try:
            report['finalState'] = r.cli('vm', 'show', a.vm)['state']
        except BaseException as e:
            report['observationError'] = repr(e)
        r.save('report.json', report)
        os.close(fd)
        print(json.dumps(report, sort_keys=True))
    raise SystemExit(report['status'] != 'passed')


if __name__ == '__main__':
    main()
