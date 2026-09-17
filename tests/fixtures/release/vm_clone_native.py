#!/usr/bin/env python3
"""Qualify a full clone of a stopped VM (ADR 0066) natively.

Run only on the owner-authorized disposable test host, with explicit
--execute-disposable, against a stopped VM whose writable disks are pool
volumes.

1. Plan the clone. The review must name the new name and UUID, a copy for every
   writable disk and the acknowledgements ADR 0066 lists.
2. Apply it as one detached job. Afterwards the original's saved definition must
   be byte for byte what it was, and the clone must exist, stopped, with the
   reviewed name and UUID, new MAC addresses, no creation metadata, and every
   copied disk naming its copy and never the original.
3. Start the clone and shut it down gracefully. With --require-guest-writes,
   libvirt must count reads and writes on the clone's first copied disk while it
   runs, which only a booted operating system makes.
4. With --remove-clone, remove the clone's definition through a reviewed plan
   that keeps disks, then delete exactly its copies once no definition names
   them. The report lists what was removed.

Other VMs, prior jobs and source media are compared before and after.
"""
import argparse
import hashlib
import json
import os
import re
import stat
import subprocess
import time
from pathlib import Path

from autostart_tui_probe import TERMINAL_STATES
from disk_move_native import block_stats
from power_native_cycle import Cycle, authorized_test_host
from tui_workspace_probe import Runner, canonical_path, require, strict_json, media_listing

CLONE_ACKS = ['copy-managed-volumes', 'exclusive-configuration-writer', 'exclusive-storage-writer', 'host-mutation', 'new-vm-identity']


def macs(xml):
    return re.findall(r"<mac address='([0-9a-f:]+)'", xml)


def uses_volume(xml, pool, volume):
    return re.search(r"<source pool='" + re.escape(pool) + r"' volume='" + re.escape(volume) + "'", xml) is not None


def volume_present(uri, pool, volume):
    return subprocess.run(['virsh', '-c', uri, 'vol-info', '--pool', pool, volume], capture_output=True, text=True, timeout=60).returncode == 0


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', required=True, type=Path)
    p.add_argument('--execute-disposable', required=True, action='store_true')
    p.add_argument('--connection', required=True, choices=('qemu:///system', 'qemu:///session'))
    p.add_argument('--vm', required=True, help='UUID of a stopped test VM to clone')
    p.add_argument('--name', required=True, help="the same VM's name, as a second check")
    p.add_argument('--clone-name', required=True)
    p.add_argument('--pool', default='', help='optional single pool for every copy')
    p.add_argument('--boot-wait', type=int, default=90)
    p.add_argument('--require-guest-writes', action='store_true')
    p.add_argument('--remove-clone', action='store_true')
    a = p.parse_args()
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(a.root.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private stage required')
    os.umask(0o077)
    out = stage / f'clone-{a.vm[:8]}-{int(time.time())}'
    out.mkdir(mode=0o700)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    for name in ('virmill', 'virmilld'):
        with Path('/usr/bin/' + name).open('rb') as f:
            require(hashlib.file_digest(f, 'sha256').hexdigest() == expected[name], 'installed binary mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=a.connection), out, fd)
    report = {'status': 'failed', 'connection': a.connection, 'vmID': a.vm, 'cloneName': a.clone_name, 'binaries': expected,
              'scope': 'native full clone of a stopped VM; guest identity inside the disks is not changed'}
    source_cycle = clone_cycle = None
    try:
        before = r.cli('vm', 'list')
        jobs = r.cli('operation', 'list')
        media = media_listing(Path.home() / 'images')
        r.save('before-vms.json', before)
        require(all(j['state'] in TERMINAL_STATES for j in jobs), 'active job prevents test')
        original = next(v for v in before if v['key']['resourceUUID'] == a.vm)
        require(original['name'] == a.name and original['state'] == 'stopped' and not original.get('hasManagedSave')
                and original['autostart'] is False, 'target is not a stopped test VM with no saved state')
        require(all(v['name'] != a.clone_name for v in before), 'a VM already uses the clone name')
        source_cycle = Cycle(r, stage, a.connection, original)

        request = {'name': a.clone_name}
        if a.pool:
            request['pool'] = a.pool
        plan = r.cli('vm', 'clone', a.vm, '--input', json.dumps(request))
        require(plan['operation'] == 'vm.clone-v1' and source_cycle.resource in plan['resourceIDs'], 'unexpected clone plan')
        acks = sorted(plan['acknowledgements'])
        require([x for x in acks if x not in ('new-firmware-state', 'pool-overcommit')] == CLONE_ACKS, f'unexpected acknowledgements {acks}')
        review = plan['review']
        require(review['name'] == a.clone_name and review['uuid'] != a.vm and review['changesOriginal'] is False and review['disks'], 'review is incomplete')
        if a.pool:
            require(all(d['toPool'] == a.pool for d in review['disks']), 'a copy is not in the chosen pool')
        label = source_cycle.label('clone')
        job = source_cycle.apply(label, plan)
        require(job['state'] == 'succeeded', f'{label}: job {job["state"]}; inspect it, do not replay')

        after_source = r.cli('vm', 'show', a.vm)
        require(after_source['persistentXML'] == original['persistentXML'] and after_source['state'] == 'stopped', 'the original changed')
        clone = r.cli('vm', 'show', review['uuid'])
        r.save('clone-vm.json', clone)
        xml = clone['persistentXML']
        require(clone['name'] == a.clone_name and clone['state'] == 'stopped' and clone['key']['resourceUUID'] == review['uuid'], 'the clone is not the reviewed stopped VM')
        # Shared read-only media keep their own names, which may mention the
        # original; the clone's identity is its uuid element.
        require('virmill:creation' not in xml and re.search(r'<uuid>' + re.escape(review['uuid']) + '</uuid>', xml) and '<uuid>' + a.vm + '</uuid>' not in xml,
                'the clone carries the original identity or creation metadata')
        original_macs, clone_macs = macs(original['persistentXML']), macs(xml)
        require(len(clone_macs) == len(original_macs) and not set(clone_macs) & set(original_macs), 'the clone does not have its own MAC addresses')
        for d in review['disks']:
            require(uses_volume(xml, d['toPool'], d['volume']) and not uses_volume(xml, d['fromPool'], d['fromVolume']), f'clone disk {d["target"]} does not name its copy alone')
            require(volume_present(a.connection, d['toPool'], d['volume']), f'copy of {d["target"]} is missing')
        report.update(uuid=review['uuid'], copies=[{k: d[k] for k in ('target', 'fromPool', 'toPool', 'volume', 'sizeBytes')} for d in review['disks']],
                      sharedMedia=review['sharedMedia'], freshFirmwareVariables=review['freshFirmwareVariables'], acknowledgements=acks,
                      originalUnchanged=True, newMACAddresses=True)

        clone_cycle = Cycle(r, stage, a.connection, clone)
        clone_cycle.run('start')
        time.sleep(a.boot_wait)
        if a.require_guest_writes:
            first = review['disks'][0]['target']
            stats = block_stats(a.connection, review['uuid'], first)
            report['cloneDiskIOWhileRunning'] = {'target': first, **stats}
            require(stats.get('rd_bytes', 0) > 0 and stats.get('wr_bytes', 0) > 0, 'the clone did not read and write its copied disk')
        clone_cycle.run('stop')
        report['cloneBooted'] = True

        if a.remove_clone:
            removal = r.cli('vm', 'remove', review['uuid'])
            rlabel = clone_cycle.label('remove-clone')
            removed = clone_cycle.apply(rlabel, removal)
            require(removed['state'] == 'succeeded', f'{rlabel}: job {removed["state"]}')
            all_xml = ''.join(v.get('persistentXML', '') for v in r.cli('vm', 'list'))
            deleted = []
            for d in review['disks']:
                require(not uses_volume(all_xml, d['toPool'], d['volume']), f'a definition still names the copy {d["volume"]}')
                subprocess.run(['virsh', '-c', a.connection, 'vol-delete', '--pool', d['toPool'], d['volume']], check=True, capture_output=True, timeout=120)
                deleted.append(d['toPool'] + '/' + d['volume'])
            report['cloneRemoved'] = {'definition': True, 'deletedCopies': deleted}

        after = r.cli('vm', 'list')
        r.save('after-vms.json', after)
        others_before = [v for v in before if v['key']['resourceUUID'] != a.vm]
        others_after = [v for v in after if v['key']['resourceUUID'] not in (a.vm, review['uuid'])]
        require(others_after == others_before, 'other VMs changed')
        ours = set(source_cycle.jobs + clone_cycle.jobs)
        require([j for j in r.cli('operation', 'list') if j['operationID'] not in ours] == jobs, 'prior job records changed')
        require(media_listing(Path.home() / 'images') == media, 'source media changed')
        report.update(status='passed', otherVMsJobsAndMediaPreserved=True)
    except BaseException as e:
        report['error'] = repr(e)
    finally:
        for cycle, key in ((source_cycle, 'sourceSteps'), (clone_cycle, 'cloneSteps')):
            if cycle:
                report[key] = cycle.steps
                report.setdefault('jobs', []).extend(cycle.jobs)
        r.save('report.json', report)
        os.close(fd)
        print(json.dumps(report, sort_keys=True))
    raise SystemExit(report['status'] != 'passed')


if __name__ == '__main__':
    main()
