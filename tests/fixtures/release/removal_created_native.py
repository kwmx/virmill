#!/usr/bin/env python3
"""Qualify removing a VM that Virmill created, keeping its disks (ADR 0063).

Run only on the owner-authorized disposable test host, with explicit
--execute-disposable, against a stopped VM that Virmill created. The reviewed
plan must list every retained source, and for a UEFI VM its firmware settings
file. After one detached job the VM is gone and every retained file is still
there: pool volumes are read back from libvirt, other files by their path.

Other VMs, prior jobs and source media are compared before and after. An
unknown job outcome is kept for inspection and never replayed.
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

REMOVE_ACKS = ['exclusive-lifecycle-writer', 'host-mutation', 'remove-vm-definition']


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


def volume_present(uri, path):
    """True when libvirt still registers a volume at this path; a read-only query."""
    out = subprocess.run(['virsh', '-c', uri, 'vol-dumpxml', path], capture_output=True, text=True, timeout=60)
    return out.returncode == 0 and re.search(r'<capacity[^>]*>\d+</capacity>', out.stdout) is not None


class Removal(Cycle):
    def plan_removal(self):
        plan = self.r.cli('vm', 'remove', self.vm)
        require(plan['operation'] == 'vm.remove-definition-v1' and plan['resourceIDs'] == [self.resource], 'unexpected removal plan')
        require(sorted(plan['acknowledgements']) == REMOVE_ACKS, 'unexpected removal acknowledgements')
        review = plan['review']
        require(review['diskDeletion'] is False and review['configurationRemoved'] is True and review['backupsDeleted'] is False, 'review does not keep storage')
        require(isinstance(review['retainedSources'], list) and review['retainedSources'], 'review lists no retained sources')
        return plan


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', required=True, type=Path)
    p.add_argument('--execute-disposable', required=True, action='store_true')
    p.add_argument('--connection', required=True, choices=('qemu:///system', 'qemu:///session'))
    p.add_argument('--vm', required=True, help='UUID of a stopped VM that Virmill created for tests')
    p.add_argument('--name', required=True, help="the same VM's name, as a second check")
    p.add_argument('--expect-firmware', action='store_true', help='a UEFI VM: its firmware settings file must be kept')
    a = p.parse_args()
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(a.root.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private stage required')
    os.umask(0o077)
    out = stage / f'removal-{a.vm[:8]}-{int(time.time())}'
    out.mkdir(mode=0o700)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    for name in ('virmill', 'virmilld'):
        with Path('/usr/bin/' + name).open('rb') as f:
            require(hashlib.file_digest(f, 'sha256').hexdigest() == expected[name], 'installed binary mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=a.connection), out, fd)
    report = {'status': 'failed', 'connection': a.connection, 'vmID': a.vm, 'binaries': expected,
              'scope': 'native definition-only removal of a Virmill-created VM; no disk deletion'}
    removal = None
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
        require("type='volume'" in xml or 'type="volume"' in xml, 'target does not use pool-volume disks')
        others = [v for v in before if v['key']['resourceUUID'] != a.vm]
        removal = Removal(r, stage, a.connection, original)

        plan = removal.plan_removal()
        review = plan['review']
        sources = review['retainedSources']
        report.update(retainedSources=sources, keptFirmware=review.get('keptFirmware', ''), keptTPMState=review.get('keptTPMState'))
        if a.expect_firmware:
            require(review.get('keptFirmware', '') in sources and review['keptFirmware'] != '', 'UEFI firmware file not listed as kept')
        volumes = [s for s in sources if volume_present(a.connection, s)]
        require(volumes, 'no retained source is a registered volume before removal')
        label = removal.label('remove')
        job = removal.apply(label, plan)
        require(job['state'] == 'succeeded', f'{label}: job {job["state"]}; inspect it, do not replay')

        after = r.cli('vm', 'list')
        r.save('after-vms.json', after)
        require(all(v['key']['resourceUUID'] != a.vm for v in after), 'the VM is still defined')
        require([v for v in after if v['key']['resourceUUID'] != a.vm] == others, 'other VMs changed')
        kept, missing = [], []
        for source in sources:
            if volume_present(a.connection, source):
                kept.append(source)
            elif os.path.exists(source):
                kept.append(source)
            else:
                missing.append(source)
        report.update(keptSources=kept, unreadableSources=missing)
        require(all(v in kept for v in volumes), 'a retained volume disappeared with the VM')
        afterjobs = r.cli('operation', 'list')
        require([j for j in afterjobs if j['operationID'] not in removal.jobs] == jobs, 'prior job records changed')
        require(media_listing(Path.home() / 'images') == media, 'source media changed')
        report.update(status='passed', definitionRemoved=True, otherVMsJobsAndMediaPreserved=True)
    except BaseException as e:
        report['error'] = repr(e)
    finally:
        if removal:
            report.update(steps=removal.steps, jobs=removal.jobs)
        r.save('report.json', report)
        os.close(fd)
        print(json.dumps(report, sort_keys=True))
    raise SystemExit(report['status'] != 'passed')


if __name__ == '__main__':
    main()
