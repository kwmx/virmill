#!/usr/bin/env python3
"""Owner-authorized native check of ADR 0054's 2026-09-16 amendment.

Runs on the authorized test VM as the ordinary test user, in a private
qemu:///session that starts with no pools, networks or VMs (see
pool_setup_probe.py), through the installed binaries and a private coordinator.
No guest is ever started.

It defines a pool holding one generated qcow2 disk and a never-booted VM using
it, plus an unrelated never-booted guest whose disks and firmware are plain
files outside every pool, the way many hosts' existing guests are. Then:

1. A guest whose disk is a symlink, outside the pool, to the VM's volume must
   make the removal review refuse with RESOURCE_BUSY. So must a guest whose
   qcow2 image outside the pool has that volume as its backing file.
2. With those two guests undefined, removing the VM with its disk must pass
   review and apply: the VM and its volume are gone, while the unrelated guest,
   its files and the leftover alias files are unchanged.

The private session is emptied at the end and the host's own pools, networks
and VMs must not change. Copy tui_workspace_probe.py, one_approval_import_probe.py
and pool_setup_probe.py alongside.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
from types import SimpleNamespace
import unittest
import uuid
import xml.etree.ElementTree as ET
from tui_workspace_probe import Runner, require, canonical_path, authorized_test_host
from one_approval_import_probe import virsh, domains
from pool_setup_probe import URI, SHORT, STAGED, private_environment, sockets_fit, host_inventory, start_coordinator, stop_coordinator, pool_state

ACKS = {'host-mutation', 'remove-vm-definition', 'data-loss-delete-disks', 'exclusive-lifecycle-writer', 'exclusive-storage-writer'}
FIRMWARE = '/usr/share/edk2/ovmf/OVMF_CODE.fd'


def sha(path):
    with open(path, 'rb') as f:
        return hashlib.file_digest(f, 'sha256').hexdigest()


def guest(name, identity, disks, firmware=None):
    """A never-booted guest. disks holds (path, driver type, device) tuples."""
    root = ET.Element('domain', type='kvm')
    ET.SubElement(root, 'name').text = name
    ET.SubElement(root, 'uuid').text = identity
    ET.SubElement(root, 'memory', unit='MiB').text = '128'
    ET.SubElement(root, 'vcpu').text = '1'
    os_node = ET.SubElement(root, 'os')
    ET.SubElement(os_node, 'type', arch='x86_64', machine='q35' if firmware else 'pc').text = 'hvm'
    if firmware:
        ET.SubElement(os_node, 'loader', readonly='yes', type='pflash', format='raw').text = firmware[0]
        ET.SubElement(os_node, 'nvram', format='raw').text = firmware[1]
    devices = ET.SubElement(root, 'devices')
    for index, (path, driver, device) in enumerate(disks):
        disk = ET.SubElement(devices, 'disk', type='file', device=device)
        ET.SubElement(disk, 'driver', name='qemu', type=driver)
        ET.SubElement(disk, 'source', file=str(path))
        if device == 'cdrom':
            ET.SubElement(disk, 'target', dev='sd' + 'abcdefgh'[index], bus='sata')
            ET.SubElement(disk, 'readonly')
        else:
            ET.SubElement(disk, 'target', dev='vd' + 'abcdefgh'[index], bus='virtio')
    return ET.tostring(root, encoding='unicode')


def cli_raw(*args):
    """The installed CLI's JSON envelope, keeping refusals instead of raising."""
    process = subprocess.run(['/usr/bin/virmill', '--connection', URI, '--output', 'json', '--non-interactive', *args],
                             stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=120)
    return json.loads(process.stdout.splitlines()[0]) if process.stdout.strip() else {'error': {'code': 'NO_OUTPUT', 'message': process.stderr[-400:]}}


def define(out, name, xml):
    path = out / (name + '.xml')
    path.write_text(xml)
    virsh(URI, 'define', str(path))


def execute(root):
    require(authorized_test_host() and os.getuid() == 1000 and os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(root.absolute()))
    require(stage.is_relative_to(Path.home() / 'virmill-tests') and stage != Path.home() / 'virmill-tests' and
            stat.S_ISDIR(stage.lstat().st_mode) and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    user_runtime = Path(f'/run/user/{os.getuid()}')
    ident = uuid.uuid4().hex[:8]
    run, short = stage / ('outside-' + ident), user_runtime / ('vo-' + ident)
    require(sockets_fit(short), 'libvirt socket paths would exceed 108 bytes')
    for folder in (run, run / 'pool', run / 'outside', run / 'outside' / 'nested', *(run / f for f in STAGED.values()), short, *(short / f for f in SHORT.values())):
        folder.mkdir(mode=0o700)
    regular = dict(os.environ)
    host_before = host_inventory(regular)
    os.environ.update(private_environment(run, short))
    os.environ.pop('WAYLAND_DISPLAY', None)
    require(domains(URI) == set() and pool_state()['names'] == [], 'the private session is not empty')
    report = {'privateSessionEmptyAtStart': True, 'result': 'failed'}
    coordinator = start_coordinator(run, short)
    pool_id = str(uuid.uuid4())
    try:
        qemu = lambda *args: subprocess.run(['/usr/bin/qemu-img', *args], check=True, capture_output=True, timeout=60)
        volume = run / 'pool' / 'created-disk-000.qcow2'
        qemu('create', '-f', 'qcow2', str(volume), '64M')
        pool = ET.Element('pool', type='dir')
        ET.SubElement(pool, 'name').text = 'virmill-outside-' + ident
        ET.SubElement(pool, 'uuid').text = pool_id
        ET.SubElement(ET.SubElement(pool, 'target'), 'path').text = str(run / 'pool')
        (run / 'pool.xml').write_text(ET.tostring(pool, encoding='unicode'))
        virsh(URI, 'pool-define', str(run / 'pool.xml'))
        virsh(URI, 'pool-start', pool_id)

        outside = run / 'outside'
        other_disk, nested_disk = outside / 'other.qcow2', outside / 'nested' / 'fedora.qcow2'
        qemu('create', '-f', 'qcow2', str(nested_disk), '64M')
        qemu('create', '-f', 'qcow2', '-b', 'nested/fedora.qcow2', '-F', 'qcow2', str(other_disk))
        alias = outside / 'alias.qcow2'
        alias.symlink_to(volume)
        child = outside / 'child.qcow2'
        qemu('create', '-f', 'qcow2', '-b', str(volume), '-F', 'qcow2', str(child))
        kept = {str(p): sha(p) for p in (other_disk, nested_disk, child)}

        target, other, aliased, backed = (str(uuid.uuid4()) for _ in range(4))
        define(run, 'target', guest('virmill-outside-target-' + ident, target, [(volume, 'qcow2', 'disk')]))
        define(run, 'other', guest('virmill-outside-other-' + ident, other,
                                   [(other_disk, 'qcow2', 'disk'), (outside / 'removed-installer.iso', 'raw', 'cdrom')],
                                   (FIRMWARE, str(outside / 'never-started_VARS.fd'))))
        report['unrelatedGuest'] = {'disk': 'qcow2 outside pools with an outside backing file', 'cdrom': 'missing ISO',
                                    'firmware': 'system loader, never-created NVRAM'}

        # 1. Aliases outside the pool must still protect the volume.
        report['refusals'] = {}
        for label, identity, disk in (('symlink', aliased, alias), ('backing file', backed, child)):
            define(run, label.replace(' ', '-'), guest(f'virmill-outside-{label.replace(" ", "-")}-{ident}', identity, [(disk, 'qcow2', 'disk')]))
            refused = cli_raw('vm', 'remove', target, '--delete-disk', 'vda')
            report['refusals'][label] = refused.get('error')
            require((refused.get('error') or {}).get('code') == 'RESOURCE_BUSY', label + ' alias did not refuse deletion: ' + json.dumps(refused.get('error')))
            virsh(URI, 'undefine', identity)

        # 2. The unrelated guest alone must not block removing the VM with its disk.
        fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_CLOEXEC)
        runner = Runner(SimpleNamespace(binary='/usr/bin/virmill', connection=URI), run, fd)
        plan = runner.cli('vm', 'remove', target, '--delete-disk', 'vda')
        report['plan'] = {'operation': plan['operation'], 'acknowledgements': sorted(plan['acknowledgements'])}
        require(plan['operation'] == 'vm.remove-disks-v1' and set(plan['acknowledgements']) <= ACKS and 'data-loss-delete-disks' in plan['acknowledgements'],
                'unexpected removal review')
        argv = ['plan', 'apply', plan['planID'], '--digest', plan['planDigest'], '--idempotency-key', 'outside-remove-' + ident, '--wait', '--timeout', '120s']
        for ack in plan['acknowledgements']:
            argv += ['--ack', ack]
        report['applied'] = runner.cli(*argv)['state']
        virsh(URI, 'pool-refresh', pool_id)
        report['after'] = {
            'vmRemoved': target not in set(virsh(URI, 'list', '--all', '--uuid').split()),
            'volumeFileAbsent': not os.path.lexists(volume),
            'volumeUnlisted': volume.name not in virsh(URI, 'vol-list', pool_id),
            'unrelatedGuestDefined': other in set(virsh(URI, 'list', '--all', '--uuid').split()),
            'unrelatedAndAliasFilesUnchanged': {p: sha(p) for p in kept} == kept,
            'symlinkLeftInPlace': alias.is_symlink(),
        }
        require(report['applied'] == 'succeeded' and all(report['after'].values()), 'removal with disks did not end as expected: ' + json.dumps(report['after']))
        report['result'] = 'passed'
    finally:
        for identity in set(virsh(URI, 'list', '--all', '--uuid').split()):
            virsh(URI, 'undefine', identity, '--nvram') if 'nvram' in virsh(URI, 'dumpxml', identity) else virsh(URI, 'undefine', identity)
        if pool_id in virsh(URI, 'pool-list', '--all', '--uuid').split():
            virsh(URI, 'pool-destroy', pool_id)
            virsh(URI, 'pool-undefine', pool_id)
        stop_coordinator(coordinator)
        report['privateSessionEmptyAtEnd'] = domains(URI) == set() and pool_state()['names'] == []
        os.environ.clear()
        os.environ.update(regular)
        report['hostInventoryUnchanged'] = host_inventory(regular) == host_before
        (run / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
        os.chmod(run / 'report.json', 0o600)
        print(json.dumps(report, indent=2, sort_keys=True))
    require(report['privateSessionEmptyAtEnd'] and report['hostInventoryUnchanged'], 'the private session or the host was not left as found')


class ProbeTests(unittest.TestCase):
    def test_unrelated_guest_declares_outside_files(self):
        tree = ET.fromstring(guest('g', '99999999-9999-4999-8999-999999999999', [('/x/a.qcow2', 'qcow2', 'disk'), ('/x/b.iso', 'raw', 'cdrom')], ('/fw/code.fd', '/x/vars.fd')))
        self.assertEqual([d.find('source').get('file') for d in tree.findall('devices/disk')], ['/x/a.qcow2', '/x/b.iso'])
        self.assertEqual([d.find('target').get('dev') for d in tree.findall('devices/disk')], ['vda', 'sdb'])
        self.assertEqual(tree.findtext('os/nvram'), '/x/vars.fd')
        self.assertIsNotNone(tree.find('devices/disk/readonly'))


def main():
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument('--execute-disposable', action='store_true')
    p.add_argument('--root', type=Path)
    p.add_argument('--self-test', action='store_true')
    a = p.parse_args()
    if a.self_test or not a.execute_disposable:
        unittest.main(argv=['disk_removal_outside_pools_probe'], exit=True)
    require(a.root is not None, '--root is required')
    execute(a.root)


if __name__ == '__main__':
    main()
