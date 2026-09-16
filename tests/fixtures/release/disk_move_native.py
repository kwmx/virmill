#!/usr/bin/env python3
"""Qualify moving one disk of a stopped VM to another pool (ADR 0062) natively.

Run only on the owner-authorized disposable test host, with explicit
--execute-disposable, against a stopped VM that Virmill created.

1. Plan the move. The review must name the disk, both pools, the copy and the
   decision about the original, and must ask for the data-loss acknowledgement
   unless the original is kept.
2. Apply it as one detached job. Afterwards the copy must exist in the
   destination pool with the disk's capacity, the saved definition must name the
   copy at the same target with the same bus and drive port, and the original
   must be gone unless it was kept.
3. Start the VM and shut it down again, so the moved disk does not stop it
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

MOVE_ACKS = ['exclusive-configuration-writer', 'exclusive-storage-writer', 'host-mutation']


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


def volume_names(uri, pool):
    """Names registered in a pool; a read-only query."""
    out = subprocess.run(['virsh', '-c', uri, 'vol-list', '--pool', pool],
                         capture_output=True, text=True, timeout=60)
    require(out.returncode == 0, 'the pool is not registered: ' + pool)
    names = []
    for line in out.stdout.splitlines()[2:]:
        parts = line.split()
        if parts:
            names.append(parts[0])
    return names


def volume_capacity(uri, pool, volume):
    """Capacity in bytes of a pool volume; a read-only query."""
    out = subprocess.run(['virsh', '-c', uri, 'vol-dumpxml', '--pool', pool, volume],
                         capture_output=True, text=True, timeout=60)
    require(out.returncode == 0, 'the copy is not registered in its pool')
    found = re.search(r"<capacity unit='bytes'>(\d+)</capacity>", out.stdout)
    require(found is not None, 'copy capacity unreadable')
    return int(found.group(1))


def disk_declaration(xml, target):
    """The pool, volume, bus and drive unit a definition declares for one disk."""
    for block in re.findall(r'<disk .*?</disk>', xml, re.S):
        dev = re.search(r"<target dev='([^']+)'", block)
        if not dev or dev.group(1) != target:
            continue
        pool = re.search(r"<source pool='([^']+)' volume='([^']+)'", block)
        bus = re.search(r"bus='([^']+)'", block)
        unit = re.search(r"<address type='drive'[^/]*unit='(\d+)'", block)
        return {'pool': pool.group(1) if pool else '', 'volume': pool.group(2) if pool else '',
                'bus': bus.group(1) if bus else '', 'unit': unit.group(1) if unit else ''}
    return {}


class Mover(Cycle):
    def plan_move(self, target, pool, keep):
        request = {'target': target, 'pool': pool}
        if keep:
            request['keepOldCopy'] = True
        plan = self.r.cli('vm', 'disk', 'move', self.vm, '--input', json.dumps(request))
        require(plan['operation'] == 'vm.disk.move-v1' and self.resource in plan['resourceIDs'], 'unexpected move plan')
        wanted = sorted(MOVE_ACKS if keep else MOVE_ACKS + ['data-loss-delete-old-copy'])
        acks = sorted(a for a in plan['acknowledgements'] if a != 'pool-overcommit')
        require(acks == wanted, 'unexpected move acknowledgements: ' + json.dumps(plan['acknowledgements']))
        review = plan['review']
        require(review['target'] == target and review['toPool'] == pool and review['fromPool'] and review['volume'],
                'review does not name the disk and both pools')
        require(review['requiresStopped'] is True and review['keepOldCopy'] is keep and review['diskDeletion'] is (not keep),
                'review does not describe the decision about the original')
        require(review['fromVolume'] and review['fromVolume'] != review['volume'], 'review reuses the original name')
        return plan


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', required=True, type=Path)
    p.add_argument('--execute-disposable', required=True, action='store_true')
    p.add_argument('--connection', required=True, choices=('qemu:///system', 'qemu:///session'))
    p.add_argument('--vm', required=True, help='UUID of a stopped VM that Virmill created for tests')
    p.add_argument('--name', required=True, help="the same VM's name, as a second check")
    p.add_argument('--target', required=True, help='the disk to move, such as sdb')
    p.add_argument('--pool', required=True, help='destination storage pool name')
    p.add_argument('--keep-old-copy', action='store_true', help='keep the original instead of deleting it')
    p.add_argument('--unanswered-stop', action='store_true', help='the guest has no OS: force it off instead of shutting down')
    p.add_argument('--boot-wait', type=int, default=90)
    a = p.parse_args()
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(a.root.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private stage required')
    os.umask(0o077)
    out = stage / f'diskmove-{a.vm[:8]}-{int(time.time())}'
    out.mkdir(mode=0o700)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    for name in ('virmill', 'virmilld'):
        with Path('/usr/bin/' + name).open('rb') as f:
            require(hashlib.file_digest(f, 'sha256').hexdigest() == expected[name], 'installed binary mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=a.connection), out, fd)
    report = {'status': 'failed', 'connection': a.connection, 'vmID': a.vm, 'target': a.target,
              'destinationPool': a.pool, 'keepOldCopy': bool(a.keep_old_copy), 'binaries': expected,
              'scope': 'native move of one stopped VM disk between storage pools; guest contents are not inspected'}
    mover = None
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
        was = disk_declaration(xml, a.target)
        require(was.get('pool') and was.get('volume'), 'the selected disk is not a pool volume')
        require(was['pool'] != a.pool, 'the disk already lives in the destination pool')
        others = [v for v in before if v['key']['resourceUUID'] != a.vm]
        sourceNames = volume_names(a.connection, was['pool'])
        mover = Mover(r, stage, a.connection, original)

        plan = mover.plan_move(a.target, a.pool, bool(a.keep_old_copy))
        review = plan['review']
        label = mover.label('move-disk')
        job = mover.apply(label, plan)
        require(job['state'] == 'succeeded', f'{label}: job {job["state"]}; inspect it, do not replay')

        capacity = volume_capacity(a.connection, a.pool, review['volume'])
        require(capacity == review['capacityBytes'], f'the copy reports {capacity} bytes')
        shown = r.cli('vm', 'show', a.vm)
        r.save(label + '-vm.json', shown)
        now = disk_declaration(shown['persistentXML'], a.target)
        require(now.get('pool') == a.pool and now.get('volume') == review['volume'],
                'the definition does not name the copy at ' + a.target)
        require(now.get('bus') == was.get('bus') and now.get('unit') == was.get('unit'),
                'the moved disk changed its bus or drive port')
        # A bare volume name is not unique across pools: moving a disk back into
        # a pool can give the copy a name another disk already uses elsewhere.
        # The original is identified by its pool and volume together.
        still = [d for d in (disk_declaration(shown['persistentXML'], t) for t in
                             re.findall(r"<target dev='([^']+)'", shown['persistentXML']))
                 if d.get('pool') == was['pool'] and d.get('volume') == was['volume']]
        require(not still, 'the definition still names the original')
        remaining = volume_names(a.connection, was['pool'])
        if a.keep_old_copy:
            require(was['volume'] in remaining, 'the original was deleted although it was kept')
        else:
            require(was['volume'] not in remaining, 'the original is still registered in its pool')
        require([n for n in remaining if n != was['volume']] == [n for n in sourceNames if n != was['volume']],
                'other volumes in the source pool changed')
        require(shown['state'] == 'stopped', 'the VM is no longer stopped after the move')
        report.update(fromPool=was['pool'], fromVolume=was['volume'], copy=review['volume'],
                      capacityBytes=capacity, acknowledgements=sorted(plan['acknowledgements']), steps=mover.steps)

        # The move changed the definition on purpose, so the power cycle
        # compares against the moved VM rather than the one captured before it.
        mover.original = shown
        mover.run('start')
        if a.unanswered_stop:
            time.sleep(5)
            mover.run('hard-stop')
        else:
            time.sleep(a.boot_wait)
            mover.run('stop')
        report['bootedWithMovedDisk'] = True

        after = r.cli('vm', 'list')
        r.save('after-vms.json', after)
        require([v for v in after if v['key']['resourceUUID'] != a.vm] == others, 'other VMs changed')
        afterjobs = r.cli('operation', 'list')
        require([j for j in afterjobs if j['operationID'] not in mover.jobs] == jobs, 'prior job records changed')
        require(media_listing(Path.home() / 'images') == media, 'source media changed')
        report.update(status='passed', otherVMsJobsAndMediaPreserved=True)
    except BaseException as e:
        report['error'] = repr(e)
    finally:
        if mover:
            report.update(steps=mover.steps, jobs=mover.jobs)
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
