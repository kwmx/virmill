#!/usr/bin/env python3
"""Qualify closing an interrupted disk addition (ADR 0062) natively.

Run only on the owner-authorized disposable test host, with explicit
--execute-disposable, against a stopped VM that Virmill created, while the
installed user coordinator runs as virmilld.service.

1. Plan one empty disk and apply it detached. After --kill-after-ms the
   coordinator is killed with SIGKILL, as a crash would, and started again.
2. The addition must then be unresolved, and the probe reads what the host holds:
   whether the saved definition names the reviewed disk and whether its volume
   exists. It picks the one disposition that matches: accept a named disk, or
   delete an unnamed one (which closes it with nothing to delete when the volume
   never existed).
3. The disposition must succeed, the addition must read partial, a deleted or
   never-created volume must be absent, and the VM must be free: it starts and
   shuts down (or is forced off with --unanswered-stop).

An addition that finished before the kill is recorded as such and ends the run
without a disposition; rerun with a smaller delay. Other VMs, prior jobs and
source media are compared before and after. Nothing is replayed.
"""
import argparse
import hashlib
import json
import os
import signal
import stat
import subprocess
import time
from pathlib import Path

from autostart_tui_probe import TERMINAL_STATES
from disk_add_native import Adder, authorized_test_host, disk_targets
from power_native_cycle import Cycle
from tui_workspace_probe import Runner, canonical_path, require, strict_json, media_listing

DISPOSE_ACKS = {'accept': ['close-disk-addition', 'inherit-recovery-resources'],
                'delete': ['close-disk-addition', 'data-loss-delete-disks', 'host-mutation', 'inherit-recovery-resources'],
                'close': ['close-disk-addition', 'inherit-recovery-resources']}


def volume_present(uri, pool, volume):
    out = subprocess.run(['virsh', '-c', uri, 'vol-info', '--pool', pool, volume], capture_output=True, text=True, timeout=60)
    return out.returncode == 0


def restart_coordinator(runner):
    """SIGKILL the user coordinator, as a crash would, and start it again."""
    subprocess.run(['systemctl', '--user', 'kill', '--signal=SIGKILL', 'virmilld.service'], check=True, timeout=30)
    deadline = time.monotonic() + 30
    while subprocess.run(['systemctl', '--user', 'is-active', '--quiet', 'virmilld.service']).returncode == 0:
        require(time.monotonic() < deadline, 'the coordinator did not stop')
        time.sleep(.2)
    subprocess.run(['systemctl', '--user', 'reset-failed', 'virmilld.service'], timeout=30)
    subprocess.run(['systemctl', '--user', 'start', 'virmilld.service'], check=True, timeout=60)
    deadline = time.monotonic() + 60
    while True:
        try:
            return runner.cli('operation', 'list')
        except RuntimeError:
            require(time.monotonic() < deadline, 'the coordinator did not come back')
            time.sleep(1)


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', required=True, type=Path)
    p.add_argument('--execute-disposable', required=True, action='store_true')
    p.add_argument('--connection', required=True, choices=('qemu:///system', 'qemu:///session'))
    p.add_argument('--vm', required=True, help='UUID of a stopped VM that Virmill created for tests')
    p.add_argument('--name', required=True, help="the same VM's name, as a second check")
    p.add_argument('--kill-after-ms', type=int, default=0, help='delay between the accepted apply and the kill')
    p.add_argument('--unanswered-stop', action='store_true', help='the guest has no OS: force it off instead of shutting down')
    p.add_argument('--boot-wait', type=int, default=90)
    a = p.parse_args()
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(a.root.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private stage required')
    os.umask(0o077)
    out = stage / f'diskdispose-{a.vm[:8]}-{int(time.time())}'
    out.mkdir(mode=0o700)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    for name in ('virmill', 'virmilld'):
        with Path('/usr/bin/' + name).open('rb') as f:
            require(hashlib.file_digest(f, 'sha256').hexdigest() == expected[name], 'installed binary mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=a.connection), out, fd)
    report = {'status': 'failed', 'connection': a.connection, 'vmID': a.vm, 'killAfterMs': a.kill_after_ms, 'binaries': expected,
              'scope': 'native disposition of a disk addition interrupted by a coordinator crash'}
    adder = None
    try:
        before = r.cli('vm', 'list')
        jobs = r.cli('operation', 'list')
        media = media_listing(Path.home() / 'images')
        require(all(j['state'] in TERMINAL_STATES for j in jobs), 'active job prevents test')
        original = next(v for v in before if v['key']['resourceUUID'] == a.vm)
        xml = original['persistentXML']
        require(original['name'] == a.name and original['state'] == 'stopped' and not original.get('hasManagedSave')
                and original['autostart'] is False and '<virmill:creation' in xml, 'target is not a stopped, Virmill-created test VM')
        others = [v for v in before if v['key']['resourceUUID'] != a.vm]
        adder = Adder(r, stage, a.connection, original)

        plan = adder.plan_add(1, '')
        review = plan['review']
        label = adder.label('add-disk')
        r.save(label + '-plan.json', plan)
        acks = [part for ack in plan['acknowledgements'] for part in ('--ack', ack)]
        job = r.cli('plan', 'apply', plan['planID'], '--digest', plan['planDigest'], *acks,
                    '--detach', '--idempotency-key', f'{adder.key}-{label}')
        adder.jobs.append(job['operationID'])
        time.sleep(a.kill_after_ms / 1000)
        restart_coordinator(r)
        job = r.cli('operation', 'show', job['operationID'])
        r.save(label + '-after-crash.json', job)
        report['additionAfterCrash'] = job['state']
        referenced = review['volume'] in r.cli('vm', 'show', a.vm)['persistentXML']
        present = volume_present(a.connection, review['pool'], review['volume'])
        report['hostAfterCrash'] = {'definitionNamesDisk': referenced, 'volumePresent': present}
        if job['state'] == 'succeeded':
            report['status'] = 'not-interrupted'
            raise SystemExit('the addition finished before the kill; rerun with a smaller --kill-after-ms')
        require(job['state'] in ('recovery-required', 'interrupted'), f'the interrupted addition reads {job["state"]}')

        choice = 'accept' if referenced else 'delete'
        dispose = r.cli('operation', 'dispose-disk-addition', job['operationID'], '--input', json.dumps({'disposition': choice}))
        r.save('dispose-plan.json', dispose)
        kind = choice if referenced or present else 'close'
        require(sorted(dispose['acknowledgements']) == DISPOSE_ACKS[kind], f'{kind}: unexpected acknowledgements {dispose["acknowledgements"]}')
        require(dispose['review']['diskDeletion'] is (kind == 'delete'), 'the review misstates deletion')
        dlabel = adder.label('dispose-' + kind)
        closed = adder.apply(dlabel, dispose)
        require(closed['state'] == 'succeeded', f'{dlabel}: job {closed["state"]}; inspect it, do not replay')
        parent = r.cli('operation', 'show', job['operationID'])
        require(parent['state'] == 'partial', f'the closed addition reads {parent["state"]}')
        shown = r.cli('vm', 'show', a.vm)
        if kind == 'accept':
            require(review['volume'] in shown['persistentXML'] and volume_present(a.connection, review['pool'], review['volume']), 'the accepted disk is gone')
        else:
            require(review['volume'] not in shown['persistentXML'] and not volume_present(a.connection, review['pool'], review['volume']), 'the unused volume remains')
        report.update(disposition=kind, acknowledgements=sorted(dispose['acknowledgements']), additionAfterDisposition=parent['state'],
                      targets=disk_targets(shown['persistentXML']))

        # The VM and its pool are free again.
        adder.original = shown
        adder.run('start')
        if a.unanswered_stop:
            time.sleep(5)
            adder.run('hard-stop')
        else:
            time.sleep(a.boot_wait)
            adder.run('stop')
        report['vmFreeAfterDisposition'] = True

        after = r.cli('vm', 'list')
        require([v for v in after if v['key']['resourceUUID'] != a.vm] == others, 'other VMs changed')
        afterjobs = r.cli('operation', 'list')
        require([j for j in afterjobs if j['operationID'] not in adder.jobs] == jobs, 'prior job records changed')
        require(media_listing(Path.home() / 'images') == media, 'source media changed')
        report.update(status='passed', otherVMsJobsAndMediaPreserved=True)
    except BaseException as e:
        if report['status'] != 'not-interrupted':
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
