#!/usr/bin/env python3
"""Qualify live CPU and memory changes, and boot edits while running (ADR 0068).

Run only on the owner-authorized disposable test host, with explicit
--execute-disposable, against a stopped VM. Memory checks need a virtio memory
balloon in its saved definition; CPU checks need `--cpu-slots` above its boot
count; `--boot-order` needs a second boot device. Each part can be skipped, so
one VM covers what it can and another covers the rest.

1. Fixture, through libvirt's own API: give the saved definition spare CPU slots
   with `virsh setvcpus --config --maximum`, so a live CPU change has room. The
   original maximum is restored at the end. Virmill does not write spare slots
   yet (ADR 0068); it must keep the ones a definition has, which step 2 checks.
2. While the VM is stopped, move the boot CPU count with a reviewed next-boot
   edit: the stored definition must keep the spare slots and match the reviewed
   bytes, which is how the byte-exact readback is proved against libvirt's own
   rendering.
3. Start the VM. Refuse, with no job at all: memory above the running maximum,
   memory under the floor, a CPU count above the running maximum, and a value
   the VM is already running with.
4. Change what it runs with: memory down, memory back up, one CPU in, one CPU
   out. Each is a reviewed plan applied as a detached job; afterwards the live
   definition must hold the reviewed value and the saved definition must be byte
   for byte what it was.
5. With --boot-order, while it still runs, change the boot order: the saved
   definition changes, the running VM keeps its own order, and after a stop and
   start the running order is the new one. The original order is then restored.

Other VMs, prior jobs and source media are compared before and after. Unknown
job outcomes are kept for inspection and never replayed.
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
from power_native_cycle import Cycle, authorized_test_host
from tui_workspace_probe import Runner, canonical_path, require, strict_json, media_listing

LIVE_ACKS = ['exclusive-configuration-writer', 'host-mutation']
PRESSURE = 'guest-resource-pressure'
NEXT_BOOT_ACKS = ['exclusive-configuration-writer', 'host-mutation']


def virsh(uri, *arguments):
    subprocess.run(['virsh', '-c', uri, *arguments], check=True, capture_output=True, timeout=120)


def cpu_element(xml):
    found = re.search(r'<vcpu[^>]*>[0-9]+</vcpu>', xml)
    return found.group(0) if found else ''


class Live(Cycle):
    """Reuses the power probe's plan and apply helpers. A live change must leave
    the saved definition alone, so readback compares it byte for byte."""

    def saved(self):
        return self.r.cli('vm', 'show', self.vm)['persistentXML']

    def live_values(self, label, running=True):
        view = self.r.cli('vm', 'resources', 'show', self.vm)
        self.r.save(label + '-resources.json', view)
        require(not running or view['live'] is not None, label + ': no running values')
        return view

    def refuse(self, label, request):
        code, envelope = self.attempt('vm', 'set', self.vm, '--input', json.dumps(request))
        require(code != 0 and envelope and envelope.get('error'), f'{label}: a request the VM cannot take was planned')
        self.r.save(label + '-refusal.json', envelope['error'])
        return envelope['error']

    def live_change(self, action, request, expect_pressure):
        label = self.label(action)
        before_saved = self.saved()
        plan = self.r.cli('vm', 'set', self.vm, '--input', json.dumps({**request, 'applyMode': 'now'}))
        require(plan['operation'] == 'vm.resources-live-v1' and plan['resourceIDs'] == [self.resource], label + ': unexpected live plan')
        acks = sorted(plan['acknowledgements'])
        require([x for x in acks if x != PRESSURE] == LIVE_ACKS and (PRESSURE in acks) == expect_pressure,
                f'{label}: unexpected acknowledgements {acks}')
        review = plan['review']
        require(review['savedDefinitionUnchanged'] is True and review['nextBootUnchanged'] is True
                and review['memoryBalloon'] == 'virtio', label + ': the review does not say the saved definition is untouched')
        require(plan['estimates']['requiresDowntime'] is False, label + ': a live change claimed downtime')
        job = self.apply(label, plan)
        require(job['state'] == 'succeeded', f'{label}: job {job["state"]}; inspect it, do not replay')
        after = self.live_values(label)
        if 'vcpus' in request:
            require(after['live']['vcpus'] == request['vcpus'], f'{label}: the VM is not running {request["vcpus"]} CPUs')
        if 'memoryMiB' in request:
            require(after['live']['memoryBytes'] == request['memoryMiB'] << 20, f'{label}: running memory is not {request["memoryMiB"]} MiB')
        require(self.saved() == before_saved, label + ': the saved definition changed')
        require(self.r.cli('vm', 'show', self.vm)['state'] == 'running', label + ': the VM stopped')
        self.steps.append({'step': label, 'job': job['operationID'], 'requested': request,
                           'runningAfter': {'vcpus': after['live']['vcpus'], 'memoryBytes': after['live']['memoryBytes']}})
        return after

    def next_boot(self, action, request):
        label = self.label(action)
        plan = self.r.cli('vm', 'set', self.vm, '--input', json.dumps({**request, 'applyMode': 'next-boot'}))
        require(plan['operation'] in ('vm.configure-resources', 'vm.configure-hardware'), label + ': unexpected next-boot plan')
        require(sorted(plan['acknowledgements'])[:2] == NEXT_BOOT_ACKS, label + ': unexpected next-boot acknowledgements')
        job = self.apply(label, plan)
        require(job['state'] == 'succeeded', f'{label}: job {job["state"]}; inspect it, do not replay')
        self.steps.append({'step': label, 'job': job['operationID'], 'requested': request})
        return plan

    def power(self, action, state):
        label = self.label(action)
        plan = self.r.cli('vm', action, self.vm)
        job = self.apply(label, plan)
        require(job['state'] == 'succeeded', f'{label}: job {job["state"]}; inspect it, do not replay')
        require(self.r.cli('vm', 'show', self.vm)['state'] == state, f'{label}: the VM is not {state}')
        self.steps.append({'step': label, 'job': job['operationID']})

    def boot_order(self, layer):
        report = self.r.cli('vm', 'boot', 'show', self.vm)
        view = report[layer]
        require(view is not None, 'no ' + layer + ' boot view')
        chosen = sorted((d for d in view['devices'] if d['order'] > 0), key=lambda d: d['order'])
        return [{'kind': d['kind'], 'id': d['id']} for d in chosen], report


def restore_fixture(a, live, restore_maximum, boot_cpus, original):
    """Take the fixture's spare CPU slots back out of the saved definition."""
    if restore_maximum:
        virsh(a.connection, 'setvcpus', a.vm, str(restore_maximum), '--config', '--maximum')
        virsh(a.connection, 'setvcpus', a.vm, str(boot_cpus), '--config')
        require(cpu_element(live.saved()) == cpu_element(original['persistentXML']),
                'the fixture CPU slots were not removed: ' + cpu_element(live.saved()))
    return 0


def finish(r, live, report, a, before, jobs, media, original):
    """The VM must end exactly as it started, and nothing else may have moved."""
    require(live.saved() == original['persistentXML'], 'the VM did not end as it started')
    after = r.cli('vm', 'list')
    r.save('after-vms.json', after)
    require([v for v in after if v['key']['resourceUUID'] != a.vm] == [v for v in before if v['key']['resourceUUID'] != a.vm], 'other VMs changed')
    require([j for j in r.cli('operation', 'list') if j['operationID'] not in set(live.jobs)] == jobs, 'prior job records changed')
    require(media_listing(Path.home() / 'images') == media, 'source media changed')
    report.update(status='passed', otherVMsJobsAndMediaPreserved=True, endedAsItStarted=True)


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', required=True, type=Path)
    p.add_argument('--execute-disposable', required=True, action='store_true')
    p.add_argument('--connection', required=True, choices=('qemu:///system', 'qemu:///session'))
    p.add_argument('--vm', required=True, help='UUID of a stopped VM with a virtio memory balloon')
    p.add_argument('--name', required=True, help="the same VM's name, as a second check")
    p.add_argument('--cpu-slots', type=int, default=4, help='maximum CPUs the fixture gives the saved definition; 0 skips CPU checks')
    p.add_argument('--boot-wait', type=int, default=120)
    p.add_argument('--memory-step-mib', type=int, default=512, help='how much memory to take from the running guest; 0 skips memory checks')
    p.add_argument('--boot-order', action='store_true', help='also change the boot order while the VM runs; needs a second boot device')
    a = p.parse_args()
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(a.root.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private stage required')
    os.umask(0o077)
    out = stage / f'live-{a.vm[:8]}-{int(time.time())}'
    out.mkdir(mode=0o700)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    for name in ('virmill', 'virmilld'):
        with Path('/usr/bin/' + name).open('rb') as f:
            require(hashlib.file_digest(f, 'sha256').hexdigest() == expected[name], 'installed binary mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=a.connection), out, fd)
    report = {'status': 'failed', 'connection': a.connection, 'vmID': a.vm, 'binaries': expected,
              'scope': 'native live CPU and memory changes and boot edits while running; what the guest does with '
                       'the change inside itself is not checked'}
    live = None
    restore_maximum = 0
    try:
        before = r.cli('vm', 'list')
        jobs = r.cli('operation', 'list')
        media = media_listing(Path.home() / 'images')
        r.save('before-vms.json', before)
        require(all(j['state'] in TERMINAL_STATES for j in jobs), 'active job prevents test')
        original = next(v for v in before if v['key']['resourceUUID'] == a.vm)
        require(original['name'] == a.name and original['state'] == 'stopped' and not original.get('hasManagedSave')
                and original['autostart'] is False, 'target is not a stopped test VM with no saved state')
        require(a.cpu_slots or a.memory_step_mib or a.boot_order, 'nothing to check')
        require(not a.memory_step_mib or "<memballoon model='virtio'" in original['persistentXML'],
                'the target has no virtio memory balloon')
        live = Live(r, stage, a.connection, original)
        first = live.live_values('00-stopped', running=False)
        boot_cpus = first['persistent']['vcpus']
        maximum_memory_mib = first['persistent']['memoryBytes'] >> 20
        require(first['canChangeLiveCPU'] is False and first['canChangeLiveMemory'] is False,
                'a stopped VM was offered a live change')
        require(not a.cpu_slots or a.cpu_slots > boot_cpus, 'the fixture must add CPU slots above the boot count')
        require(not a.memory_step_mib or maximum_memory_mib > 256 + a.memory_step_mib, 'the target is too small to give memory back')

        if a.cpu_slots:
            # Fixture: libvirt writes the spare CPU slots itself, so the
            # definition carries exactly what libvirt renders.
            restore_maximum = first['persistent']['maximumVcpus']
            virsh(a.connection, 'setvcpus', a.vm, str(a.cpu_slots), '--config', '--maximum')
            virsh(a.connection, 'setvcpus', a.vm, str(boot_cpus), '--config')
            with_slots = live.saved()
            require(f"current='{boot_cpus}'" in cpu_element(with_slots) and f">{a.cpu_slots}<" in cpu_element(with_slots),
                    'the fixture did not give the definition spare CPU slots')
            report['fixture'] = {'cpuElementBefore': cpu_element(original['persistentXML']), 'cpuElementWithSlots': cpu_element(with_slots)}

            # A next-boot CPU edit must keep the slots and match its reviewed bytes.
            live.next_boot('next-boot-cpu', {'vcpus': boot_cpus + 1})
            kept = cpu_element(live.saved())
            require(f"current='{boot_cpus + 1}'" in kept and f">{a.cpu_slots}<" in kept, 'the next-boot edit dropped the spare CPU slots: ' + kept)
            report['nextBootKeptSlots'] = kept
            live.next_boot('next-boot-cpu-back', {'vcpus': boot_cpus})

        live.power('start', 'running')
        time.sleep(a.boot_wait)
        running = live.live_values('01-running')
        if a.cpu_slots:
            require(running['live']['vcpus'] == boot_cpus and running['live']['maximumVcpus'] == a.cpu_slots,
                    'the VM is not running with spare CPU slots')
        require(running['canChangeLiveCPU'] is bool(a.cpu_slots) and running['canChangeLiveMemory'] is bool(a.memory_step_mib),
                'the running VM was offered something other than what it can take')
        require(('now' in running['applyModes']) == bool(a.cpu_slots or a.memory_step_mib), 'unexpected apply modes')
        report['runningBefore'] = {'vcpus': running['live']['vcpus'], 'maximumVcpus': running['live']['maximumVcpus'],
                                   'memoryBytes': running['live']['memoryBytes'], 'maximumMemoryBytes': running['live']['maximumMemoryBytes']}

        refusals = {'memory-above-maximum': {'applyMode': 'now', 'memoryMiB': maximum_memory_mib + 1024}}
        if a.memory_step_mib:
            refusals['memory-under-floor'] = {'applyMode': 'now', 'memoryMiB': 128}
        if a.cpu_slots:
            refusals['cpus-above-maximum'] = {'applyMode': 'now', 'vcpus': a.cpu_slots + 1}
            refusals['cpus-already-running'] = {'applyMode': 'now', 'vcpus': boot_cpus}
        else:
            refusals['cpus-without-slots'] = {'applyMode': 'now', 'vcpus': boot_cpus + 1}
        report['refusals'] = {}
        for name, request in refusals.items():
            error = live.refuse(live.label('refuse-' + name), request)
            report['refusals'][name] = {'code': error['code'], 'message': error['message']}
        require(all(j['state'] in TERMINAL_STATES for j in r.cli('operation', 'list')), 'a refusal started a job')

        if a.memory_step_mib:
            live.live_change('memory-down', {'memoryMiB': maximum_memory_mib - a.memory_step_mib}, True)
            live.live_change('memory-up', {'memoryMiB': maximum_memory_mib}, False)
        if a.cpu_slots:
            live.live_change('cpu-in', {'vcpus': boot_cpus + 1}, False)
            live.live_change('cpu-out', {'vcpus': boot_cpus}, True)
        report['liveChangesApplied'] = bool(a.memory_step_mib or a.cpu_slots)

        if a.boot_order:
            # A boot edit while the VM runs changes the saved definition only. The
            # first device keeps its place, so the guest still boots the same way.
            saved_order, report_before = live.boot_order('persistent')
            running_order, _ = live.boot_order('live')
            if len(saved_order) >= 2:
                changed = saved_order[:-2] + [saved_order[-1], saved_order[-2]]
            else:
                spare = next((d for d in report_before['persistent']['devices']
                              if d['order'] == 0 and d['selectable'] and not (d['kind'] == 'disk' and d['device'] == 'cdrom' and not d['mediaPresent'])), None)
                require(spare is not None, 'the target has no second boot device for this check')
                changed = saved_order + [{'kind': spare['kind'], 'id': spare['id']}]
            before_saved = live.saved()
            plan = live.next_boot('boot-order-while-running', {'bootOrder': changed})
            require('keeps its current boot order' in ' '.join(plan['risks']), 'the review does not say the running VM keeps its order')
            require(live.saved() != before_saved, 'the saved boot order did not change')
            require(live.boot_order('persistent')[0] == changed, 'the saved definition does not hold the new order')
            require(live.boot_order('live')[0] == running_order, 'the running VM lost the order it started with')
            require(r.cli('vm', 'show', a.vm)['state'] == 'running', 'the boot edit stopped the VM')
            report['bootOrderWhileRunning'] = {'saved': changed, 'runningKept': running_order}

            live.power('stop', 'stopped')
            live.power('start', 'running')
            time.sleep(a.boot_wait)
            require(live.boot_order('live')[0] == changed, 'the new boot order did not take effect at the next start')
            report['bootOrderAfterRestart'] = changed
            live.next_boot('boot-order-restore', {'bootOrder': saved_order})
            live.power('stop', 'stopped')
            require(live.boot_order('persistent')[0] == saved_order, 'the original boot order was not restored')
        else:
            live.power('stop', 'stopped')
        restore_maximum = restore_fixture(a, live, restore_maximum, boot_cpus, original)
        finish(r, live, report, a, before, jobs, media, original)
    except BaseException as e:
        report['error'] = repr(e)
        if restore_maximum:
            report['fixtureLeftBehind'] = f'saved CPU maximum {restore_maximum} not restored; inspect before reuse'
    finally:
        if live:
            report['steps'] = live.steps
            report['jobs'] = live.jobs
        r.save('report.json', report)
        os.close(fd)
        print(json.dumps(report, sort_keys=True))
    raise SystemExit(report['status'] != 'passed')


if __name__ == '__main__':
    main()
