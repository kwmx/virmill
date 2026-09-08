#!/usr/bin/env python3
"""Read-only runtime and preserved-resource observation on the authorized VM."""
import datetime
import hashlib
import json
from pathlib import Path
import subprocess

def run(args):
    result = subprocess.run(args, capture_output=True, text=True, timeout=20)
    return {'argv': args, 'exitCode': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr}

report = {'at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'scope': 'read-only disposable-host observation',
          'kernel': run(['uname', '-r']),
          'packages': run(['rpm', '-q', 'libvirt-daemon', 'qemu-kvm', 'firewalld', 'python3-firewall',
                           'nftables', 'NetworkManager', 'iptables-nft', 'python3', 'systemd', 'iproute']),
          'ipv4Forwarding': Path('/proc/sys/net/ipv4/ip_forward').read_text().strip(),
          'helperUnits': [run(['systemctl', 'is-active', unit]) for unit in ('virmill-host-helper.socket', 'virmill-host-helper.service')],
          'namespaces': run(['sudo', '-n', 'ip', 'netns', 'list']),
          'firewall': [run(['sudo', '-n', 'firewall-cmd', *mode, '--direct', '--get-all-rules']) for mode in ([], ['--permanent'])],
          'guests': {}, 'networks': {}}
for kind in ('guest', 'network'):
    prefix = ['virsh', '--readonly', '-c', 'qemu:///system']
    listing = run(prefix + (['list'] if kind == 'guest' else ['net-list']) + ['--all', '--uuid'])
    assert listing['exitCode'] == 0
    for ident in listing['stdout'].split():
        xml = run(prefix + (['dumpxml'] if kind == 'guest' else ['net-dumpxml']) + [ident, '--inactive'])
        assert xml['exitCode'] == 0
        info = run(prefix + (['domstate'] if kind == 'guest' else ['net-info']) + [ident])
        assert info['exitCode'] == 0
        report['guests' if kind == 'guest' else 'networks'][ident] = {
            'inactiveXMLSHA256': hashlib.sha256(xml['stdout'].encode()).hexdigest(), 'state': info['stdout']}
print(json.dumps(report, indent=2))
