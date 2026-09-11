#!/usr/bin/env python3
"""Parent-operated native serial-console fixture on the authorized disposable VM.

Run --self-test locally for pure fixture-generation/contract checks (no libvirt,
PTY, sudo, guest or network action). Native execution requires --root pointing to
an already staged private ~/virmill-tests/RUN directory containing binaries.json,
plus --execute-disposable. A new console-access child is exclusively created.

Creates one transient 1-vCPU/128-MiB BIOS VM with no NIC, a generated 1-MiB disk,
serial PTY and private Unix VNC. Fixed native virsh commands create the fixture;
actual installed Virmill supplies discovery, CLI and TUI serial attachment, safe
escape and restored TUI focus.
A repeating handcrafted BIOS marker proves bytes crossed the real guest serial
path. It does not prove an OS login, graphical viewer, guest tools, or acceptance
completion. The VM is stopped in finally only after its exact ownership marker,
UUID and disk path are rechecked. Generated source/image/evidence are retained.
Existing guests, operation states and source-media metadata must remain unchanged.
Requires adjacent tui_workspace_probe.py; installed binutils, libvirt/virsh,
QEMU/KVM, sudo and restorecon. No downloads, SSH, network creation or host-wide edits.
"""
import argparse
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pty
import re
import selectors
import signal
import socket
import stat
import struct
import subprocess
import sys
import tempfile
import termios
import time
import unittest
import uuid
import xml.etree.ElementTree as ET

from tui_workspace_probe import (Runner, Terminal, canonical_path, inventory,
                                 media_listing, require, strict_json)

CONNECTION = 'qemu:///system'
BINARY = '/usr/bin/virmill'
VIRSH = '/usr/bin/virsh'
IMAGE_PARENT = Path('/var/lib/libvirt/images')
OWNER_NAMESPACE = 'urn:virmill:console-fixture:v1'
MAX_PTY = 1 << 20


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def assembly(marker):
    require(re.fullmatch(r'VIRMILL CONSOLE PASS [0-9a-f]{32}', marker) is not None,
            'fixed printable fixture marker required')
    # Original fixture code follows the same COM1 setup as multidisk-probe/boot.S.
    # BIOS INT 15h/86h waits approximately 250 ms between repeated markers, so
    # attaching after boot cannot lose the only output. No disk writes or OS code.
    return r'''.code16
.text
.global _start
_start:
    ljmp $0, $start16
start16:
    cli
    xorw %ax, %ax
    movw %ax, %ds
    movw %ax, %es
    movw %ax, %ss
    movw $0x7000, %sp
    cld
    sti
    movw $0x3fb, %dx
    movb $0x80, %al
    outb %al, %dx
    movw $0x3f8, %dx
    movb $3, %al
    outb %al, %dx
    incw %dx
    xorb %al, %al
    outb %al, %dx
    movw $0x3fb, %dx
    movb $3, %al
    outb %al, %dx
    movw $0x3fa, %dx
    movb $0xc7, %al
    outb %al, %dx
    movw $0x3fc, %dx
    movb $0x0b, %al
    outb %al, %dx
repeat:
    movw $message, %si
next:
    lodsb
    testb %al, %al
    jz delay
    movb %al, %bl
    movw $0x3fd, %dx
ready:
    inb %dx, %al
    testb $0x20, %al
    jz ready
    movb %bl, %al
    movw $0x3f8, %dx
    outb %al, %dx
    jmp next
delay:
    movw $0x8600, %ax
    movw $3, %cx
    movw $0xd090, %dx
    int $0x15
    xorw %ax, %ax
    movw %ax, %ds
    jmp repeat
message: .asciz "''' + marker + r'''\r\n"
.org 510
.word 0xaa55
'''


def build_boot(directory, marker, run):
    source = directory / 'boot.S'
    source.write_text(assembly(marker))
    source.chmod(0o400)
    run('/usr/bin/as', '--32', '-o', str(directory / 'boot.o'), str(source))
    run('/usr/bin/ld', '-m', 'elf_i386', '-Ttext', '0x7c00', '--entry=_start',
        '-o', str(directory / 'boot.elf'), str(directory / 'boot.o'))
    run('/usr/bin/objcopy', '-O', 'binary', '--only-section=.text',
        str(directory / 'boot.elf'), str(directory / 'boot.bin'))
    boot = (directory / 'boot.bin').read_bytes()
    require(len(boot) == 512 and boot[-2:] == b'\x55\xaa' and
            marker.encode() + b'\r\n\0' in boot, 'boot sector construction differs')
    raw = directory / 'boot.raw'
    with raw.open('xb') as stream:
        stream.truncate(1 << 20)
        stream.write(boot)
    raw.chmod(0o400)
    return raw


def domain_xml(identity, name, disk):
    require(str(uuid.UUID(identity)) == identity and uuid.UUID(identity).int != 0,
            'fixture UUID required')
    require(name == 'virmill-console-' + identity[:8] and
            disk == IMAGE_PARENT / ('virmill-console-' + identity) / 'boot.raw',
            'fixed fixture ownership paths required')
    root = ET.Element('domain', {'type': 'kvm'})
    ET.SubElement(root, 'name').text = name
    ET.SubElement(root, 'uuid').text = identity
    meta = ET.SubElement(root, 'metadata')
    ET.SubElement(meta, '{' + OWNER_NAMESPACE + '}fixture', {'id': identity})
    ET.SubElement(root, 'memory', {'unit': 'MiB'}).text = '128'
    ET.SubElement(root, 'vcpu').text = '1'
    os_node = ET.SubElement(root, 'os')
    ET.SubElement(os_node, 'type', {'arch': 'x86_64', 'machine': 'pc'}).text = 'hvm'
    ET.SubElement(os_node, 'boot', {'dev': 'hd'})
    ET.SubElement(root, 'on_poweroff').text = 'destroy'
    ET.SubElement(root, 'on_reboot').text = 'destroy'
    ET.SubElement(root, 'on_crash').text = 'destroy'
    devices = ET.SubElement(root, 'devices')
    image = ET.SubElement(devices, 'disk', {'type': 'file', 'device': 'disk'})
    ET.SubElement(image, 'driver', {'name': 'qemu', 'type': 'raw'})
    ET.SubElement(image, 'source', {'file': str(disk)})
    ET.SubElement(image, 'target', {'dev': 'hda', 'bus': 'ide'})
    serial = ET.SubElement(devices, 'serial', {'type': 'pty'})
    ET.SubElement(serial, 'target', {'port': '0'})
    console = ET.SubElement(devices, 'console', {'type': 'pty'})
    ET.SubElement(console, 'target', {'type': 'serial', 'port': '0'})
    graphics = ET.SubElement(devices, 'graphics', {'type': 'vnc'})
    ET.SubElement(graphics, 'listen', {'type': 'socket'})
    video = ET.SubElement(devices, 'video')
    ET.SubElement(video, 'model', {'type': 'vga'})
    return ET.tostring(root, encoding='unicode')


def owned_xml(raw, identity, name, disk):
    root = ET.fromstring(raw)
    owner = root.find('metadata/{' + OWNER_NAMESPACE + '}fixture')
    images = root.findall('devices/disk')
    require(root.findtext('uuid') == identity and root.findtext('name') == name and
            owner is not None and owner.get('id') == identity and len(images) == 1 and
            images[0].find('source').get('file') == str(disk) and
            not root.findall('devices/interface') and root.find('os/loader') is None,
            'refusing action on a guest without exact fixture ownership')
    return root


def serial_roundtrip(runner, identity, marker):
    master, slave = pty.openpty()
    selector = selectors.DefaultSelector()
    process = None
    transcript = bytearray()
    attached = False
    escaped = False
    started = time.monotonic()
    try:
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 80, 0, 0))
        process = runner.launch(['--connection', CONNECTION, 'vm', 'console', 'open',
                                 identity, '--choice', 'serial:0'],
                                stdin=slave, stdout=slave, stderr=slave)
        os.close(slave)
        slave = -1
        selector.register(master, selectors.EVENT_READ)
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            for _, _ in selector.select(.1):
                try:
                    block = os.read(master, 65536)
                except OSError as error:
                    if error.errno == errno.EIO:
                        block = b''
                    else:
                        raise
                if block:
                    require(len(transcript) + len(block) <= MAX_PTY, 'serial transcript exceeds bound')
                    transcript.extend(block)
                else:
                    break
            if marker.encode() in transcript:
                attached = True
                require(process.poll() is None, 'console exited before escape')
                require(os.write(master, b'\x1d') == 1, 'could not send Ctrl+]')
                escaped = True
                break
            require(process.poll() is None, 'console exited before BIOS serial marker')
        require(attached and escaped, 'real BIOS serial marker not received before deadline')
        process.wait(timeout=10)
        require(process.returncode == 0, 'Ctrl+] did not return a successful console command')
        return {'biosMarkerObserved': True, 'ctrlBracketExited': True,
                'exitCode': process.returncode, 'elapsedSeconds': round(time.monotonic() - started, 3),
                'scope': 'real guest serial bytes and clean client escape; no guest OS/login or viewer claim'}
    finally:
        if process is not None and process.poll() is None:
            # This isolated process group contains only our console CLI/viewer.
            # Never signal QEMU or the coordinator through process-group cleanup.
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=3)
        selector.close()
        os.close(master)
        if slave >= 0:
            os.close(slave)
        runner.save('serial-console.ansi', bytes(transcript))


def tui_roundtrip(runner, identity, name, marker):
    """Real TUI -> virsh terminal handoff -> restored TUI, same running fixture."""
    terminal = Terminal(runner, 'tui-console-80x24', 80, 24)
    launched = False
    restored = False
    started = time.monotonic()
    try:
        def wait(label, predicate, key=None):
            return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
        wait('Initial native Overview', lambda screen: 'Virtual machines' in screen)
        wait('VM table', lambda screen: 'NAME' in screen and 'STATE' in screen, b'2')
        wait('Find fixture VM', lambda screen: 'Search:' in screen and 'Enter Keep filter' in screen, b'/')
        wait('Only newly created fixture selected', lambda screen: name in screen and '1 of 1 selected' in screen,
             name.encode())
        wait('Keep fixture filter', lambda screen: '1 of 1 selected' in screen and 'Enter Keep filter' not in screen,
             b'\r')
        wait('Fixture VM details', lambda screen: identity in screen and 'VM details' in screen and 'Console' in screen,
             b'\r')
        wait('VM action buttons focused', lambda screen: '>[' in screen, b'\t')
        for index in range(10):
            if re.search(r'>\[ Console \]', terminal.screen.text()):
                break
            old = terminal.screen.text()
            wait('Move to Console button ' + str(index), lambda screen: screen != old, b'\x1b[C')
        require(re.search(r'>\[ Console \]', terminal.screen.text()), 'Console action button not reachable')
        wait('Selected VM console choices', lambda screen: 'Open console' in screen and name in screen and
             'Serial console 1' in screen and 'Ctrl+]' in screen, b'\r')
        for index in range(4):
            if re.search(r'>\s*\[ Serial console 1 \]', terminal.screen.text()):
                break
            old = terminal.screen.text()
            wait('Select serial choice ' + str(index), lambda screen: screen != old, b'\t')
        require(re.search(r'>\s*\[ Serial console 1 \]', terminal.screen.text()), 'Serial choice not selected')
        launched = True
        wait('Real BIOS serial bytes after TUI handoff', lambda screen: marker in screen and 'Open console' not in screen,
             b'\r')
        wait('Ctrl+] restores console screen', lambda screen: 'Open console' in screen and name in screen and
             'Console closed.' in screen and 'Console ended:' not in screen, b'\x1d')
        restored = True
        wait('Esc returns to same fixture VM details', lambda screen: identity in screen and 'VM details' in screen and
             'Open console' not in screen, b'\x1b')
        return {'size': [80, 24], 'selectedFixtureByName': True, 'consoleButtonReachable': True,
                'biosMarkerObserved': True, 'ctrlBracketRestoredConsoleScreen': True,
                'escapeRestoredVMDetails': True, 'elapsedSeconds': round(time.monotonic() - started, 3)}
    finally:
        if launched and not restored and terminal.process.poll() is None:
            # Only the process group started by this owned TUI. A failed handoff
            # must not leave its virsh child attached after the TUI is terminated.
            try:
                terminal.send(b'\x1d')
                terminal.read(.2)
            finally:
                if terminal.process.poll() is None:
                    os.killpg(terminal.process.pid, signal.SIGTERM)
        terminal.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path)
    parser.add_argument('--execute-disposable', action='store_true')
    parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        require(not args.execute_disposable and args.root is None, 'self-test cannot select a native target')
        result = unittest.main(argv=[sys.argv[0]], exit=False).result
        if not result.wasSuccessful():
            raise SystemExit(1)
        return
    require(args.execute_disposable and args.root is not None, 'explicit disposable execution and root required')
    require(socket.gethostname() in ('virmill-test', 'virmill-test.home') and os.getuid() == 1000,
            'wrong authorized test host/actor')
    root = canonical_path(str(args.root.absolute()))
    require(root.is_relative_to(Path.home() / 'virmill-tests') and stat.S_ISDIR(root.lstat().st_mode) and
            root.stat().st_uid == 1000 and stat.S_IMODE(root.stat().st_mode) == 0o700,
            'private staged test root required')
    canonical_path(str(IMAGE_PARENT))
    require(stat.S_ISDIR(IMAGE_PARENT.lstat().st_mode) and IMAGE_PARENT.stat().st_uid == 0 and
            not IMAGE_PARENT.stat().st_mode & 0o022, 'ordinary protected libvirt image parent required')
    manifest_path = root / 'binaries.json'
    require(stat.S_ISREG(manifest_path.lstat().st_mode) and manifest_path.stat().st_uid == 1000,
            'ordinary owned binary manifest required')
    manifest = strict_json(manifest_path.read_bytes())
    require(re.fullmatch('[0-9a-f]{64}', manifest.get('virmill', '')) is not None,
            'pinned installed binary hash required')
    binary_fd = os.open(BINARY, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        with os.fdopen(os.dup(binary_fd), 'rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == manifest['virmill'],
                    'installed Virmill binary differs from reviewed hash')
        require(digest(Path('/usr/bin/virmilld')) == manifest['virmilld'], 'daemon binary hash differs')
        out = root / 'console-access'
        out.mkdir(mode=0o700)
        runner_args = argparse.Namespace(binary=BINARY, connection=CONNECTION)
        runner = Runner(runner_args, out, binary_fd)
        native_commands = []
        def native(*argv, allowed_failure=False):
            require(argv[0] in ('/usr/bin/as', '/usr/bin/ld', '/usr/bin/objcopy',
                                '/usr/bin/sudo', VIRSH, '/usr/bin/rpm'), 'unexpected fixture executable')
            result = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True,
                                    timeout=30, env=dict(os.environ, LC_ALL='C'))
            require(len(result.stdout) + len(result.stderr) <= 1 << 20, 'native fixture output exceeds bound')
            native_commands.append({'argv': list(argv), 'exitCode': result.returncode,
                                    'stdout': result.stdout.decode('utf-8', 'replace'),
                                    'stderr': result.stderr.decode('utf-8', 'replace')})
            runner.save('native-commands.json', native_commands)
            require(allowed_failure or result.returncode == 0, 'fixture command failed; see native-commands.json')
            return result
        report = {'status': 'failed', 'scope': __doc__.split('\n\n')[2], 'binarySHA256': manifest['virmill']}
        identity = str(uuid.uuid4())
        name = 'virmill-console-' + identity[:8]
        image_dir = IMAGE_PARENT / ('virmill-console-' + identity)
        image = image_dir / 'boot.raw'
        marker = 'VIRMILL CONSOLE PASS ' + identity.replace('-', '')
        start_attempted = False
        before = None
        before_jobs = None
        source_before = None
        raw = None
        raw_sha = None
        try:
            before = inventory(runner.cli('vm', 'list'), CONNECTION)
            before_jobs = runner.cli('operation', 'list')
            require(all(j['state'] in ('succeeded', 'failed', 'partial', 'canceled', 'recovery-required')
                        for j in before_jobs), 'other jobs are active; console probe must run alone')
            source_before = media_listing(Path.home() / 'images')
            require(identity not in before and all(v['name'] != name for v in before.values()),
                    'fixture identity collision')
            runner.save('before-vms.json', before)
            runner.save('before-jobs.json', before_jobs)
            runner.save('before-media.json', source_before)
            report['version'] = runner.cli('version')
            report['nativeVersions'] = native(VIRSH, '--connect', CONNECTION, 'version').stdout.decode()
            report['toolPackages'] = native('/usr/bin/rpm', '-q', 'binutils', 'libvirt-client',
                                            'qemu-system-x86-core', allowed_failure=True).stdout.decode()
            raw = build_boot(out, marker, native)
            raw_sha = digest(raw)
            report['fixture'] = {'uuid': identity, 'name': name, 'source': str(raw), 'installedDisk': str(image),
                                 'sourceSHA256': raw_sha, 'assemblySHA256': digest(out / 'boot.S'),
                                 'bootSectorSHA256': digest(out / 'boot.bin'), 'marker': marker}
            native('/usr/bin/sudo', '-n', '/usr/bin/mkdir', '--mode=0750', str(image_dir))
            native('/usr/bin/sudo', '-n', '/usr/bin/chown', 'root:qemu', str(image_dir))
            native('/usr/bin/sudo', '-n', '/usr/bin/install', '--owner=qemu', '--group=qemu', '--mode=0600', str(raw), str(image))
            native('/usr/bin/sudo', '-n', '/usr/sbin/restorecon', '-RF', str(image_dir))
            definition = out / 'domain.xml'
            definition.write_text(domain_xml(identity, name, image))
            definition.chmod(0o600)
            # Transient creation avoids persistent auto-start or configuration
            # changes after the exclusively owned fixture is stopped.
            start_attempted = True
            native(VIRSH, '--connect', CONNECTION, 'create', str(definition))
            observed = runner.cli('vm', 'show', identity)
            require(observed['state'] == 'running', 'BIOS fixture did not enter running state')
            tree = owned_xml(observed['liveXML'], identity, name, image)
            report['observedMachine'] = tree.find('os/type').get('machine')
            runner.save('fixture-vm.json', observed)
            choices = runner.cli('vm', 'console', 'show', identity)
            runner.save('console-discovery.json', choices)
            require(choices['resource'] == observed['key'] and choices['name'] == name and
                    choices['state'] == 'running' and choices['configFingerprint'] == observed['fingerprint'],
                    'console discovery selected another VM/configuration')
            serial = [c for c in choices['choices'] if c['id'] == 'serial:0']
            graphics = [c for c in choices['choices'] if c['kind'] == 'graphical']
            require(len(serial) == 1 and serial[0]['available'] and serial[0]['protocol'] == 'serial',
                    'configured serial console unavailable')
            require(len(graphics) == 1 and graphics[0]['available'] and graphics[0]['protocol'] == 'vnc',
                    'private VNC not discovered')
            report['serialRoundtrip'] = serial_roundtrip(runner, identity, marker)
            report['tuiRoundtrip'] = tui_roundtrip(runner, identity, name, marker)
            after_console = runner.cli('vm', 'show', identity)
            require(after_console['state'] == 'running' and after_console['fingerprint'] == observed['fingerprint'],
                    'leaving serial console stopped or changed the fixture VM')
            report['consoleExitPreservedVM'] = True
            report['status'] = 'passed'
        except BaseException as error:
            report['error'] = repr(error)
        finally:
            try:
                if start_attempted:
                    observed = native(VIRSH, '--connect', CONNECTION, 'dumpxml', identity, allowed_failure=True)
                    if observed.returncode == 0:
                        owned_xml(observed.stdout.decode(), identity, name, image)
                        native(VIRSH, '--connect', CONNECTION, 'destroy', identity)
                        report['ownedFixtureStopped'] = True
                    else:
                        report['ownedFixtureNotPresent'] = True
                if before is not None:
                    after = inventory(runner.cli('vm', 'list'), CONNECTION)
                    runner.save('after-vms.json', after)
                    require(after == before, 'existing guest identity/state/XML changed or transient fixture remains')
                    report['existingGuestsPreserved'] = True
                if before_jobs is not None:
                    require(runner.cli('operation', 'list') == before_jobs, 'existing operation journal changed')
                    report['existingJobsPreserved'] = True
                if source_before is not None:
                    require(media_listing(Path.home() / 'images') == source_before, 'owner source-media metadata changed')
                    report['ownerMediaMetadataPreserved'] = True
                if raw_sha is not None:
                    require(digest(raw) == raw_sha, 'generated source image changed')
                    installed = native('/usr/bin/sudo', '-n', '/usr/bin/sha256sum', '--', str(image), allowed_failure=True)
                    require(installed.returncode == 0 and installed.stdout.decode().split()[0] == raw_sha,
                            'installed generated disk differs after fixture boot')
                    report['generatedSourceAndDiskPreserved'] = True
            except BaseException as error:
                report['status'] = 'failed'
                report['cleanupOrPreservationError'] = repr(error)
            runner.save('report.json', report)
            print(json.dumps(report, sort_keys=True))
        if report['status'] != 'passed':
            raise SystemExit(1)
    finally:
        os.close(binary_fd)


class FixtureTests(unittest.TestCase):
    def test_definition_has_exact_owned_minimal_resources(self):
        identity = '570c4866-5b5e-4583-817c-602538d19b9c'
        name = 'virmill-console-' + identity[:8]
        disk = IMAGE_PARENT / ('virmill-console-' + identity) / 'boot.raw'
        raw = domain_xml(identity, name, disk)
        tree = owned_xml(raw, identity, name, disk)
        self.assertEqual(tree.findtext('memory'), '128')
        self.assertEqual(tree.findtext('vcpu'), '1')
        self.assertEqual(tree.find('devices/graphics/listen').attrib, {'type': 'socket'})
        self.assertEqual(tree.find('devices/console').get('type'), 'pty')
        self.assertEqual(tree.find('os/type').get('machine'), 'pc')
        with self.assertRaises(RuntimeError):
            owned_xml(raw.replace(name, 'another-guest', 1), identity, name, disk)
        with self.assertRaises(RuntimeError):
            owned_xml(raw.replace(str(disk), '/other/source.raw'), identity, name, disk)

    def test_boot_sector_builds_without_host_vm_actions(self):
        with tempfile.TemporaryDirectory() as temporary:
            folder = Path(temporary)
            marker = 'VIRMILL CONSOLE PASS ' + '1' * 32
            calls = []
            def compile_only(*argv):
                self.assertIn(argv[0], ('/usr/bin/as', '/usr/bin/ld', '/usr/bin/objcopy'))
                calls.append(argv)
                subprocess.run(argv, check=True, capture_output=True, timeout=10)
            source = build_boot(folder, marker, compile_only)
            self.assertEqual(len(calls), 3)
            self.assertEqual(source.stat().st_size, 1 << 20)
            self.assertEqual(source.read_bytes()[512:], bytes((1 << 20) - 512))
            self.assertIn(marker.encode(), source.read_bytes()[:512])
            self.assertEqual(source.stat().st_mode & 0o777, 0o400)

    def test_injected_marker_is_refused(self):
        with self.assertRaises(RuntimeError):
            assembly('VIRMILL CONSOLE PASS "; arbitrary assembler')


if __name__ == '__main__':
    main()
