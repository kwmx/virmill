#!/usr/bin/env python3
"""Qualify adding one empty disk to a stopped VM (ADR 0062) natively.

Run only on the owner-authorized disposable test host, with explicit
--execute-disposable, against a stopped VM that Virmill created.

1. Plan the new disk. The review must name the VM's own pool, the new volume,
   its target and bus, and the disk must be empty with no disk deletion.
2. Apply it as one detached job. Afterwards libvirt must register a volume of
   the requested size, read-only, and the definition must name exactly one more
   disk at the reviewed target.
3. Start the VM and shut it down again, so the added disk does not stop it
   booting. With --unanswered-stop the guest has no OS, so it is forced off.

Other VMs, prior jobs and source media are compared before and after. Unknown
job outcomes are kept for inspection and never replayed.
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

ADD_ACKS = ['exclusive-configuration-writer', 'exclusive-storage-writer', 'host-mutation']
ADD_ACKS_OVERCOMMIT = sorted(ADD_ACKS + ['pool-overcommit'])


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


def volume_capacity(uri, pool, volume):
    """Capacity in bytes of a pool volume; a read-only query."""
    out = subprocess.run(['virsh', '-c', uri, 'vol-dumpxml', '--pool', pool, volume],
                         capture_output=True, text=True, timeout=60)
    require(out.returncode == 0, 'the new volume is not registered in its pool')
    found = re.search(r"<capacity unit='bytes'>(\d+)</capacity>", out.stdout)
    require(found is not None, 'volume capacity unreadable')
    return int(found.group(1))


def disk_targets(xml):
    """Guest target names of every disk in a definition."""
    return re.findall(r"<target dev='([^']+)'", xml)


class Adder(Cycle):
    def plan_add(self, size, bus):
        request = {'sizeGiB': size}
        if bus:
            request['bus'] = bus
        plan = self.r.cli('vm', 'disk', 'add', self.vm, '--input', json.dumps(request))
        require(plan['operation'] == 'vm.disk.add-v1' and self.resource in plan['resourceIDs'], 'unexpected add plan')
        require(sorted(plan['acknowledgements']) in (ADD_ACKS, ADD_ACKS_OVERCOMMIT), 'unexpected add acknowledgements')
        review = plan['review']
        require(review['emptyDisk'] is True and review['diskDeletion'] is False and review['requiresStopped'] is True, 'review does not describe an empty new disk')
        require(review['sizeBytes'] == size << 30 and review['volume'] and review['pool'] and review['target'] and review['bus'], 'review is incomplete')
        if bus:
            require(review['bus'] == bus, 'review ignored the requested bus')
        return plan


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', required=True, type=Path)
    p.add_argument('--execute-disposable', required=True, action='store_true')
    p.add_argument('--connection', required=True, choices=('qemu:///system', 'qemu:///session'))
    p.add_argument('--vm', required=True, help='UUID of a stopped VM that Virmill created for tests')
    p.add_argument('--name', required=True, help="the same VM's name, as a second check")
    p.add_argument('--size', type=int, default=1, help='new disk size in GiB')
    p.add_argument('--bus', default='', help='optional explicit bus: sata, scsi or virtio')
    p.add_argument('--unanswered-stop', action='store_true', help='the guest has no OS: force it off instead of shutting down')
    p.add_argument('--boot-wait', type=int, default=90)
    a = p.parse_args()
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(a.root.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private stage required')
    os.umask(0o077)
    out = stage / f'diskadd-{a.vm[:8]}-{int(time.time())}'
    out.mkdir(mode=0o700)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    for name in ('virmill', 'virmilld'):
        with Path('/usr/bin/' + name).open('rb') as f:
            require(hashlib.file_digest(f, 'sha256').hexdigest() == expected[name], 'installed binary mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=a.connection), out, fd)
    report = {'status': 'failed', 'connection': a.connection, 'vmID': a.vm, 'sizeGiB': a.size, 'binaries': expected,
              'scope': 'native empty disk addition to a stopped VM; the guest does not format it'}
    adder = None
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
        targets = disk_targets(xml)
        others = [v for v in before if v['key']['resourceUUID'] != a.vm]
        adder = Adder(r, stage, a.connection, original)

        plan = adder.plan_add(a.size, a.bus)
        review = plan['review']
        label = adder.label('add-disk')
        job = adder.apply(label, plan)
        require(job['state'] == 'succeeded', f'{label}: job {job["state"]}; inspect it, do not replay')
        capacity = volume_capacity(a.connection, review['pool'], review['volume'])
        require(capacity == a.size << 30, f'the new volume reports {capacity} bytes')
        shown = r.cli('vm', 'show', a.vm)
        r.save(label + '-vm.json', shown)
        after_targets = disk_targets(shown['persistentXML'])
        require(after_targets == targets + [review['target']] or sorted(after_targets) == sorted(targets + [review['target']]),
                'the definition did not gain exactly the reviewed disk')
        require(shown['state'] == 'stopped' and review['volume'] in shown['persistentXML'], 'the new disk is not in the saved definition')
        # Later power steps compare against the definition with the new disk.
        adder.original = shown
        report.update(addedTarget=review['target'], volume=review['volume'], capacityBytes=capacity,
                      acknowledgements=sorted(plan['acknowledgements']), steps=adder.steps)

        adder.run('start')
        if a.unanswered_stop:
            time.sleep(5)
            adder.run('hard-stop')
        else:
            time.sleep(a.boot_wait)
            adder.run('stop')
        report['bootedWithNewDisk'] = True

        after = r.cli('vm', 'list')
        r.save('after-vms.json', after)
        require([v for v in after if v['key']['resourceUUID'] != a.vm] == others, 'other VMs changed')
        afterjobs = r.cli('operation', 'list')
        require([j for j in afterjobs if j['operationID'] not in adder.jobs] == jobs, 'prior job records changed')
        require(media_listing(Path.home() / 'images') == media, 'source media changed')
        report.update(status='passed', otherVMsJobsAndMediaPreserved=True)
    except BaseException as e:
        report['error'] = repr(e)
    finally:
        if adder:
            report.update(steps=adder.steps, jobs=adder.jobs)
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
