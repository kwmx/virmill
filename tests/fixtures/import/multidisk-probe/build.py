#!/usr/bin/env python3
"""Generate a bounded two-disk OVA in a new directory; never launch a guest."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import xml.etree.ElementTree as ET


def digest(path):
    with path.open('rb') as f:
        return hashlib.file_digest(f, 'sha256').hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--destination', type=Path, required=True)
    parser.add_argument('--payload-mib', type=int, default=1024, choices=(16, 256, 1024))
    args = parser.parse_args()
    if os.getuid() == 0:
        raise SystemExit('Run as an ordinary user; no root fixture generation.')
    destination = args.destination.absolute()
    if destination.exists() or destination.is_symlink():
        raise SystemExit('Destination must be new; existing fixtures are preserved.')
    parent = destination.parent
    if parent.resolve() != parent or parent.stat().st_uid != os.getuid() or parent.stat().st_mode & 0o022:
        raise SystemExit('User-owned non-symlink parent without group/other write required.')
    if shutil.disk_usage(parent).free < 16 << 30:
        raise SystemExit('Fixture generation requires at least 16 GiB free.')
    tools = ('as', 'ld', 'objcopy', 'qemu-img', 'bwrap', 'prlimit')
    for name in tools:
        if not Path('/usr/bin', name).is_file():
            raise SystemExit('Missing installed tool: ' + name)
    source = Path(__file__).with_name('boot.S')
    destination.mkdir(mode=0o700)
    raw = destination / 'raw'
    output = destination / 'members'
    raw.mkdir(mode=0o700)
    output.mkdir(mode=0o700)
    env = {'PATH': '/usr/bin', 'LC_ALL': 'C', 'HOME': '/nonexistent'}
    commands = []

    def run(command, timeout=120):
        commands.append(command)
        result = subprocess.run(command, env=env, check=True, capture_output=True, text=True, timeout=timeout)
        if result.stdout or result.stderr:
            print(result.stdout + result.stderr, end='', flush=True)

    run(['/usr/bin/as', '--32', '-o', str(destination / 'boot.o'), str(source)])
    run(['/usr/bin/ld', '-m', 'elf_i386', '-Ttext', '0x7c00', '--entry=_start', '-o', str(destination / 'boot.elf'), str(destination / 'boot.o')])
    run(['/usr/bin/objcopy', '-O', 'binary', '--only-section=.text', str(destination / 'boot.elf'), str(destination / 'boot.bin')])
    boot = (destination / 'boot.bin').read_bytes()
    assert len(boot) == 512 and boot[-2:] == b'\x55\xaa'
    with (raw / 'boot.raw').open('xb') as f:
        f.truncate(16 << 20)
        f.write(boot)
    with (raw / 'data.raw').open('xb') as f:
        f.truncate(3 << 30)
        f.write(b'VIRMILL-DATA-DISK-v1\0')
        f.seek(1 << 20)
        block = hashlib.shake_256(b'virmill-multidisk-upload-fixture-v1').digest(1 << 20)
        for _ in range(args.payload_mib):
            f.write(block)
    for path in raw.iterdir():
        path.chmod(0o400)
    sandbox = ['/usr/bin/prlimit', '--as=4294967296', '--fsize=8589934592', '--cpu=600', '--nofile=128', '--nproc=128', '--',
               '/usr/bin/bwrap', '--die-with-parent', '--unshare-all', '--new-session', '--ro-bind', '/usr', '/usr',
               '--symlink', 'usr/lib', '/lib', '--symlink', 'usr/lib64', '/lib64', '--proc', '/proc', '--dev', '/dev',
               '--tmpfs', '/tmp', '--ro-bind', str(raw), '/source', '--bind', str(output), '/output', '--chdir', '/output']
    for name in ('boot', 'data'):
        run(sandbox + ['/usr/bin/qemu-img', 'convert', '-f', 'raw', '-O', 'vmdk', '-o', 'subformat=twoGbMaxExtentSparse', '/source/' + name + '.raw', '/output/' + name + '.vmdk'], timeout=900)
        run(sandbox + ['/usr/bin/qemu-img', 'compare', '-f', 'raw', '-F', 'vmdk', '/source/' + name + '.raw', '/output/' + name + '.vmdk'], timeout=900)
    assert (output / 'boot-s001.vmdk').is_file()
    assert (output / 'data-s001.vmdk').is_file() and (output / 'data-s002.vmdk').is_file()
    ovf = 'http://schemas.dmtf.org/ovf/envelope/1'
    rasd = 'http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData'
    ET.register_namespace('ovf', ovf)
    ET.register_namespace('rasd', rasd)
    envelope = ET.Element('{'+ovf+'}Envelope')
    references = ET.SubElement(envelope, '{'+ovf+'}References')
    ids = {}
    for i, path in enumerate(sorted(output.iterdir())):
        assert path.is_file() and not path.is_symlink()
        ids[path.name] = 'file' + str(i)
        ET.SubElement(references, '{'+ovf+'}File', {'{'+ovf+'}id': ids[path.name], '{'+ovf+'}href': path.name, '{'+ovf+'}size': str(path.stat().st_size)})
    section = ET.SubElement(envelope, '{'+ovf+'}DiskSection')
    ET.SubElement(section, '{'+ovf+'}Info').text = 'Two independent generated VMDK disks with complete extents'
    for name, capacity in (('boot', 16 << 20), ('data', 3 << 30)):
        ET.SubElement(section, '{'+ovf+'}Disk', {'{'+ovf+'}diskId': name, '{'+ovf+'}fileRef': ids[name+'.vmdk'], '{'+ovf+'}capacity': str(capacity), '{'+ovf+'}capacityAllocationUnits': 'byte'})
    system = ET.SubElement(envelope, '{'+ovf+'}VirtualSystem', {'{'+ovf+'}id': 'virmill-multidisk-probe'})
    ET.SubElement(system, '{'+ovf+'}Name').text = 'Virmill generated BIOS multi-disk probe'
    hardware = ET.SubElement(system, '{'+ovf+'}VirtualHardwareSection')
    ET.SubElement(hardware, '{'+ovf+'}Info').text = 'Generated BIOS probe; target machine and controllers require explicit mapping'
    controller = ET.SubElement(hardware, '{'+ovf+'}Item')
    for key, value in (('ResourceType', '6'), ('InstanceID', 'controller0'), ('ResourceSubType', 'lsilogic')):
        ET.SubElement(controller, '{'+rasd+'}'+key).text = value
    for name, position in (('boot', 0), ('data', 1)):
        item = ET.SubElement(hardware, '{'+ovf+'}Item')
        for key, value in (('ResourceType', '17'), ('InstanceID', str(10+position)), ('Parent', 'controller0'), ('AddressOnParent', str(position)), ('HostResource', 'ovf:/disk/'+name)):
            ET.SubElement(item, '{'+rasd+'}'+key).text = value
    (output / 'probe.ovf').write_bytes(ET.tostring(envelope, encoding='utf-8', xml_declaration=True))
    hashes = {p.name: digest(p) for p in sorted(output.iterdir())}
    (output / 'probe.mf').write_text(''.join('SHA256 ('+name+') = '+value+'\n' for name, value in hashes.items()))
    archive = destination / 'probe.ova'
    with tarfile.open(archive, 'x', format=tarfile.USTAR_FORMAT) as tar:
        for path in sorted(output.iterdir()):
            info = tarfile.TarInfo(path.name)
            info.size, info.mode, info.mtime, info.uid, info.gid = path.stat().st_size, 0o400, 0, 0, 0
            with path.open('rb') as f:
                tar.addfile(info, f)
            path.chmod(0o400)
    archive.chmod(0o400)
    packages = subprocess.check_output(['/usr/bin/rpm', '-q', 'binutils', 'qemu-img', 'bubblewrap', 'util-linux'], text=True).splitlines()
    manifest = {'fixtureVersion': 1, 'class': 'generated BIOS probe, not an OS compatibility fixture', 'assemblySHA256': digest(source),
                'generatorSHA256': digest(Path(__file__)), 'bootSectorSHA256': digest(destination/'boot.bin'), 'payloadMiB': args.payload_mib,
                'archive': str(archive), 'archiveBytes': archive.stat().st_size, 'archiveSHA256': digest(archive), 'memberSHA256': hashes,
                'rawSHA256': {p.name: digest(p) for p in sorted(raw.iterdir())}, 'packages': packages, 'toolSHA256': {name:digest(Path('/usr/bin',name)) for name in tools},
                'commands': commands, 'confinement': 'ordinary-user all-namespace bubblewrap with read-only raw source and private output; no network or host service sockets',
                'guestExecuted': False, 'VMCreated': False, 'requiresExplicitDisposableVMTest': True}
    (destination / 'fixture.json').write_text(json.dumps(manifest, indent=2)+'\n')
    print(json.dumps(manifest, indent=2), flush=True)


if __name__ == '__main__':
    main()
