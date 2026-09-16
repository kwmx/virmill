#!/usr/bin/env python3
"""Qualify starting session VMs with no manual libvirt step (ADR 0064) natively.

Run only on the owner-authorized disposable test host, with explicit
--execute-disposable, against a stopped VM on qemu:///session that Virmill
created.

1. The packaged per-user socket units are installed and the socket is active
   with the coordinator.
2. No libvirt daemon of this user is running: the probe lets the daemon exit on
   its own idle timeout, and stops it only if that does not happen in time,
   recording which.
3. With the coordinator restarted, the VM is started through Virmill. The daemon
   that served it must run in virmill-virtqemud.service with no-new-privileges
   off, and none may run inside virmilld.service.
4. The host check reports libvirt ready, and the VM is forced off again.

Other VMs, prior jobs and source media are compared before and after.
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
from power_native_cycle import Cycle
from tui_workspace_probe import Runner, canonical_path, require, strict_json, media_listing

UNITS = ('virmill-virtqemud.socket', 'virmill-virtqemud.service')


def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


def systemctl(*args):
    r = subprocess.run(['systemctl', '--user', *args], capture_output=True, text=True, timeout=60)
    return r.returncode, r.stdout.strip()


def user_daemons():
    """This user's virtqemud processes: pid, no-new-privileges and cgroup unit."""
    r = subprocess.run(['pgrep', '-u', str(os.getuid()), '-x', 'virtqemud'], capture_output=True, text=True, timeout=30)
    out = []
    for pid in r.stdout.split():
        try:
            status = Path(f'/proc/{pid}/status').read_text()
            cgroup = Path(f'/proc/{pid}/cgroup').read_text().strip().splitlines()[-1]
        except OSError:
            continue
        nnp = next((line.split()[1] for line in status.splitlines() if line.startswith('NoNewPrivs:')), '?')
        out.append({'pid': int(pid), 'noNewPrivs': nnp, 'unit': cgroup.rsplit('/', 1)[-1]})
    return out


def wait_for_coordinator(timeout=30):
    sock = Path(f'/run/user/{os.getuid()}/virmill/control.sock')
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if sock.exists():
            return True
        time.sleep(.25)
    return False


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--root', required=True, type=Path)
    p.add_argument('--execute-disposable', required=True, action='store_true')
    p.add_argument('--vm', required=True, help='UUID of a stopped qemu:///session VM that Virmill created for tests')
    p.add_argument('--name', required=True, help="the same VM's name, as a second check")
    p.add_argument('--idle-wait', type=int, default=200, help='seconds to wait for the daemon to exit on its own')
    a = p.parse_args()
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(a.root.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private stage required')
    os.umask(0o077)
    out = stage / f'sessionsocket-{a.vm[:8]}-{int(time.time())}'
    out.mkdir(mode=0o700)
    expected = strict_json((stage / 'binaries.json').read_bytes())
    for name in ('virmill', 'virmilld'):
        with Path('/usr/bin/' + name).open('rb') as f:
            require(hashlib.file_digest(f, 'sha256').hexdigest() == expected[name], 'installed binary mismatch')
    uri = 'qemu:///session'
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=uri), out, fd)
    report = {'status': 'failed', 'connection': uri, 'vmID': a.vm, 'binaries': expected,
              'scope': 'native start of a session VM with no manual libvirt step; the guest has no OS and is forced off'}
    cycle = None
    try:
        for unit in UNITS:
            require(Path('/usr/lib/systemd/user/' + unit).is_file(), unit + ' is not installed')
        systemctl('daemon-reload')
        require(systemctl('is-active', 'virmilld.service')[1] == 'active', 'the coordinator is not running')
        require(systemctl('is-active', 'virmill-virtqemud.socket')[1] == 'active',
                'the per-user libvirt socket did not start with the coordinator')
        report['socketActiveWithCoordinator'] = True

        before = r.cli('vm', 'list')
        jobs = r.cli('operation', 'list')
        media = media_listing(Path.home() / 'images')
        r.save('before-vms.json', before)
        require(all(j['state'] in TERMINAL_STATES for j in jobs), 'active job prevents test')
        original = next(v for v in before if v['key']['resourceUUID'] == a.vm)
        require(original['name'] == a.name and original['state'] == 'stopped' and not original.get('hasManagedSave')
                and original['autostart'] is False and '<virmill:creation' in original['persistentXML'],
                'target is not a stopped, Virmill-created test VM with no saved state')
        others = [v for v in before if v['key']['resourceUUID'] != a.vm]

        # The daemon's own idle timeout is what used to leave users stuck, so
        # wait for it rather than simulating it, within a bound.
        started = time.monotonic()
        while user_daemons() and time.monotonic() - started < a.idle_wait:
            time.sleep(5)
        report['idleExitObserved'] = not user_daemons()
        report['idleWaitSeconds'] = round(time.monotonic() - started)
        if user_daemons():
            systemctl('stop', 'virmill-virtqemud.service')
            time.sleep(2)
        require(not user_daemons(), 'a libvirt daemon of this user is still running before the start')
        require(systemctl('is-active', 'virmill-virtqemud.socket')[1] == 'active', 'the socket stopped with its daemon')

        # A restarted coordinator decides at startup whether it may start VMs.
        systemctl('restart', 'virmilld.service')
        require(wait_for_coordinator(), 'the coordinator did not come back')
        time.sleep(2)
        require(systemctl('is-active', 'virmill-virtqemud.socket')[1] == 'active', 'the socket is not active after the restart')

        cycle = Cycle(r, stage, uri, original)
        cycle.run('start')
        served = user_daemons()
        r.save('daemons-after-start.json', served)
        require(served, 'no libvirt daemon of this user is running after the start')
        require(all(d['unit'] == 'virmill-virtqemud.service' for d in served),
                'a daemon ran outside the packaged service: ' + json.dumps(served))
        require(all(d['noNewPrivs'] == '0' for d in served), 'the serving daemon has no-new-privileges set')
        require(not any(d['unit'] == 'virmilld.service' for d in served), 'a daemon ran inside the coordinator')
        report['servingDaemons'] = served

        doctor = subprocess.run(['/usr/bin/virmill', '--non-interactive', 'doctor'], capture_output=True, text=True, timeout=120)
        (out / 'doctor.txt').write_text(doctor.stdout)
        attention = doctor.stdout.split('Ready')[0]
        require('libvirt socket is not' not in attention and 'LIBVIRT_SESSION_UNACTIVATED' not in doctor.stdout,
                'the host check still reports the per-user libvirt socket missing')
        report['doctorLibvirtReady'] = True

        cycle.run('hard-stop')

        after = r.cli('vm', 'list')
        r.save('after-vms.json', after)
        require([v for v in after if v['key']['resourceUUID'] != a.vm] == others, 'other VMs changed')
        afterjobs = r.cli('operation', 'list')
        require([j for j in afterjobs if j['operationID'] not in cycle.jobs] == jobs, 'prior job records changed')
        require(media_listing(Path.home() / 'images') == media, 'source media changed')
        report.update(status='passed', otherVMsJobsAndMediaPreserved=True)
    except BaseException as e:
        report['error'] = repr(e)
    finally:
        if cycle:
            report.update(steps=cycle.steps, jobs=cycle.jobs)
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
