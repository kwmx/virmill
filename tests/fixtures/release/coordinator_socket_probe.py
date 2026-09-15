#!/usr/bin/env python3
"""Check that read-only coordinator calls leave no client sockets open.

Every call that reads libvirt opens and closes one connection. The probe counts
the coordinator's client-side Unix sockets whose peer is gone, repeats each
read-only command, and fails if that count grows. It changes nothing: it runs
only read-only CLI commands, with private client state and no update check.
Run it as the coordinator's user on the authorized test host; `ss` needs
`sudo -n` to name socket owners.
"""
import argparse
import json
import os
import subprocess
import sys
import tempfile

COMMANDS = [('vm', 'list'), ('storage', 'pool', 'list'), ('network', 'list'), ('host', 'capabilities'),
            ('doctor',), ('device', 'usb', 'list'), ('operation', 'list')]


def coordinator():
    pid = subprocess.run(['pgrep', '-o', '-u', str(os.getuid()), '-x', 'virmilld'],
                         capture_output=True, text=True, timeout=10).stdout.strip()
    if not pid:
        raise SystemExit('no coordinator is running for this user')
    return pid


def orphaned(pid):
    """Client-side sockets of pid whose peer inode no longer exists."""
    lines = subprocess.run(['sudo', '-n', 'ss', '-xpn'], capture_output=True, text=True, check=True, timeout=30).stdout.splitlines()[1:]
    inodes, mine = set(), []
    for line in lines:
        fields = line.split()
        if len(fields) < 8:
            continue
        inodes.add(fields[5])
        if f'pid={pid},' in line:
            mine.append((fields[4], fields[7]))
    return sum(1 for local, peer in mine if local == '*' and peer not in inodes)


def descriptors(pid):
    return len(os.listdir(f'/proc/{pid}/fd'))


def main():
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument('--binary', default='/usr/bin/virmill')
    p.add_argument('--repeat', type=int, default=5)
    a = p.parse_args()
    pid = coordinator()
    env = dict(os.environ, VIRMILL_UPDATE_CHECK='0', XDG_STATE_HOME=tempfile.mkdtemp(), XDG_DATA_HOME=tempfile.mkdtemp())
    uptime = subprocess.run(['ps', '-o', 'etimes=', '-p', pid], capture_output=True, text=True, timeout=10).stdout.strip()
    report = {'coordinatorUptimeSeconds': int(uptime or 0), 'repeat': a.repeat,
              'orphanedSocketsAtStart': orphaned(pid), 'descriptorsAtStart': descriptors(pid), 'commands': {}}
    for args in COMMANDS:
        sockets, fds = orphaned(pid), descriptors(pid)
        codes = [subprocess.run([a.binary, *args, '--output', 'json', '--non-interactive'], env=env,
                                capture_output=True, timeout=120).returncode for _ in range(a.repeat)]
        report['commands'][' '.join(args)] = {'exitCodes': codes, 'orphanedSocketsAdded': orphaned(pid) - sockets,
                                               'descriptorsAdded': descriptors(pid) - fds}
    if coordinator() != pid:
        raise SystemExit('the coordinator restarted during the probe')
    leaked = sorted(name for name, r in report['commands'].items() if r['orphanedSocketsAdded'] > 0)
    failed = sorted(name for name, r in report['commands'].items() if any(r['exitCodes']))
    report.update({'leakingCommands': leaked, 'failedCommands': failed, 'result': 'failed' if leaked or failed else 'passed'})
    print(json.dumps(report, indent=2, sort_keys=True))
    return 1 if leaked or failed else 0


if __name__ == '__main__':
    sys.exit(main())
