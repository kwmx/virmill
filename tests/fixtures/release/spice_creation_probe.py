#!/usr/bin/env python3
"""Product ISO preparation → creation → start → private SPICE console fixture.

Root-operated only on the authorized disposable host. Requires private staged
--root/binaries.json, adjacent tui_workspace_probe.py, console_access_probe.py,
and graphical_access_probe.py. All guest creation uses reviewed Virmill services;
there is no external domain definition. One generated El Torito ISO contains only
an original BIOS teletype/COM1 marker, plus one empty 1MiB system disk. The source
is preserved, and created guest/volumes remain identified by the durable receipt.
No supplied media, existing guest or network is changed. Explicit reviewed hard
stop is used only for this fresh no-OS fixture, which cannot honor guest shutdown.
SPICE main/display channels and an Xvfb PNG prove a virtual-desktop connection, not
physical display/input, OS compatibility, clipboard opt-in or release acceptance.
--self-test is pure XML/assembly/receipt validation without native actions.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import socket
import stat
import subprocess
import sys
import time
import unittest
import uuid
import xml.etree.ElementTree as ET
from tui_workspace_probe import Runner, Terminal, canonical_path, inventory, media_listing, require, strict_json
from console_access_probe import assembly, serial_roundtrip
from graphical_access_probe import xauthority, framebuffer_png

URI = 'qemu:///system'
TERMINAL = {'succeeded', 'failed', 'partial', 'canceled', 'recovery-required'}
CREATE_ACKS = {'host-mutation', 'copy-managed-volumes', 'new-vm-identity', 'attach-readonly-media', 'creation-device-policy'}


def sha(path):
    with Path(path).open('rb') as stream: return hashlib.file_digest(stream, 'sha256').hexdigest()


def boot_assembly(marker):
    # Existing original serial fixture also prints once with BIOS teletype so
    # the virtual graphical display contains the same independently known text.
    raw = assembly(marker)
    return raw.replace('    movw $0x3fb, %dx', '''    movw $message, %si
vga_next:
    lodsb
    testb %al, %al
    jz vga_done
    movb $0x0e, %ah
    xorw %bx, %bx
    movb $7, %bl
    int $0x10
    jmp vga_next
vga_done:
    movw $0x3fb, %dx''', 1)


def check_graphics(raw):
    tree = ET.fromstring(raw)
    graphics = tree.findall('devices/graphics')
    require(len(graphics) == 1 and graphics[0].get('type') == 'spice', 'not one SPICE display')
    g = graphics[0]
    listeners = g.findall('listen')
    require(len(listeners) == 1 and listeners[0].get('type') == 'socket', 'SPICE is not a private socket')
    require(g.find('clipboard') is not None and g.find('clipboard').attrib == {'copypaste': 'no'}
            and g.find('filetransfer') is not None and g.find('filetransfer').attrib == {'enable': 'no'},
            'clipboard/file transfer unexpectedly enabled')
    require(not g.findall('channel') and not tree.findall('devices/redirdev'), 'unreviewed SPICE redirection')
    require(not tree.findall('devices/interface') and not tree.findall('devices/hostdev') and not tree.findall('devices/filesystem')
            and not tree.findall('devices/tpm') and tree.find('os/loader') is None, 'unexpected network/host/firmware device')
    return tree


def check_owned(vm, receipt, name, pool):
    identity = receipt['vmID']
    require(vm['key']['resourceUUID'] == identity and vm['name'] == name and not vm['hasManagedSave'], 'fixture VM identity differs')
    tree = check_graphics(vm['persistentXML'])
    marker = tree.find('metadata/{urn:virmill:v1}creation')
    require(tree.findtext('uuid') == identity and tree.findtext('name') == name and marker is not None
            and marker.get('binding') == receipt['binding'], 'product creation binding differs')
    require(receipt['defined'] and receipt['volumesVerified'] and len(receipt['volumes']) == 2, 'incomplete volume receipt')
    volumes = {}
    for item in receipt['volumes']:
        volume = item['allocated']
        require(item['verified'] and volume['intent']['poolID'] == pool
                and volume['intent']['name'].startswith('virmill-' + identity + '-'), 'foreign/unverified managed volume')
        require(Path(volume['path']).is_absolute() and Path(volume['path']).name == volume['intent']['name'], 'managed volume path differs')
        require(volume['intent']['name'] not in volumes, 'duplicate managed volume receipt')
        volumes[volume['intent']['name']] = volume
    disks = tree.findall('devices/disk'); remaining = set(volumes)
    require(len(disks) == 2, 'managed device count differs')
    for disk in disks:
        source = disk.find('source'); require(source is not None, 'managed source missing')
        matching = [name for name, volume in volumes.items()
                    if (disk.get('type') == 'volume' and source.attrib == {'pool': 'virmill-test', 'volume': name})
                    or (disk.get('type') == 'file' and source.attrib == {'file': volume['path']})]
        require(len(matching) == 1 and matching[0] in remaining, 'managed source mapping differs')
        remaining.remove(matching[0])
        medium = volumes[matching[0]]['intent'].get('contentType') == 'cdrom-iso'
        require(disk.get('device') == ('cdrom' if medium else 'disk'), 'managed content/device mapping differs')
    require(not remaining, 'receipt volumes omitted from domain')
    cd = next((d for d in disks if d.get('device') == 'cdrom'), None)
    require(cd is not None and cd.find('readonly') is not None and cd.find('boot').get('order') == '1', 'ISO not retained read-only/first boot')
    return tree


def viewer_roundtrip(runner, native, identity, name):
    out = runner.directory / 'viewer'; out.mkdir(mode=0o700)
    process = xvfb = None; master = None; transcript = bytearray()
    def spice():
        result = native('/usr/bin/virsh', '--connect', URI, 'qemu-monitor-command', identity, '{"execute":"query-spice"}')
        return strict_json(result.stdout)['return']
    def drain():
        if master is None: return
        while select.select([master], [], [], 0)[0]:
            try: block = os.read(master, 65536)
            except OSError: return
            if not block: return
            transcript.extend(block); require(len(transcript) <= 1 << 20, 'viewer transcript bound')
    env = dict(runner.env)
    try:
        for display in range(90, 190):
            if not Path(f'/tmp/.X11-unix/X{display}').exists() and not Path(f'/tmp/.X{display}-lock').exists(): break
        else: raise RuntimeError('no unused Xvfb display')
        authority = out / 'Xauthority'; xauthority(authority, display)
        env.update(DISPLAY=f':{display}', XAUTHORITY=str(authority)); env.pop('WAYLAND_DISPLAY', None)
        with (out / 'xvfb.log').open('xb') as log:
            xvfb = subprocess.Popen(['/usr/bin/Xvfb', f':{display}', '-screen', '0', '1024x768x24', '-nolisten', 'tcp',
                                     '-auth', str(authority), '-fbdir', str(out)], stdout=log, stderr=subprocess.STDOUT)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            require(xvfb.poll() is None, 'Xvfb exited')
            if native('/usr/bin/xwininfo', '-root', env=env, check=False).returncode == 0: break
            time.sleep(.2)
        else: raise RuntimeError('private Xvfb not ready')
        require(not spice().get('channels'), 'unexpected prior viewer connection')
        master, slave = pty.openpty()
        oldenv = runner.env; runner.env = env
        try:
            process = runner.launch(['--connection', URI, 'vm', 'console', 'open', identity, '--choice', 'graphics:0'],
                                    stdin=slave, stdout=slave, stderr=slave)
        finally: runner.env = oldenv; os.close(slave)
        deadline = time.monotonic() + 30; connected = None; viewer = None
        while time.monotonic() < deadline:
            drain(); require(process.poll() is None, 'console exited before viewer connection')
            children = Path(f'/proc/{process.pid}/task/{process.pid}/children').read_text().split()
            for child in children:
                try:
                    if Path(f'/proc/{child}/exe').resolve(strict=True) == Path('/usr/bin/virt-viewer'):
                        viewer = int(child); viewer_start = Path(f'/proc/{child}/stat').read_text().split()[21]
                except FileNotFoundError: pass
            observed = spice(); window = native('/usr/bin/xwininfo', '-root', '-tree', env=env).stdout.decode()
            if viewer and {1, 2}.issubset({c.get('channel-type') for c in observed.get('channels', [])}) and name in window:
                connected = observed; break
            time.sleep(.3)
        require(connected is not None and all(c.get('family') == 'unix' for c in connected['channels']), 'no private main/display connection')
        (out / 'window.txt').write_text(window); time.sleep(.5)
        frame = framebuffer_png(out / 'Xvfb_screen0', out / 'viewer.png')
        require(Path(f'/proc/{viewer}/exe').resolve(strict=True) == Path('/usr/bin/virt-viewer')
                and Path(f'/proc/{viewer}/stat').read_text().split()[21] == viewer_start, 'viewer process identity changed')
        os.kill(viewer, signal.SIGINT)
        deadline = time.monotonic() + 15
        while process.poll() is None and time.monotonic() < deadline: drain(); time.sleep(.1)
        require(process.poll() == 0 and not spice().get('channels'), 'viewer did not close normally')
        require(runner.cli('vm', 'show', identity)['state'] == 'running', 'closing viewer stopped guest')
        return {'mainAndDisplayConnected': connected, 'framebuffer': frame, 'viewerExitCode': 0,
                'guestRunningAfterViewerClose': True, 'scope': 'virtual desktop only'}
    finally:
        if process is not None and process.poll() is None:
            os.killpg(process.pid, signal.SIGTERM); process.wait(timeout=5)
        drain()
        if master is not None: os.close(master)
        (out / 'console.pty').write_bytes(transcript)
        if xvfb is not None:
            if xvfb.poll() is None: xvfb.terminate()
            xvfb.wait(timeout=5)


def execute(stage):
    require(socket.gethostname() in ('virmill-test', 'virmill-test.home') and os.getuid() == os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(stage.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    os.umask(0o077); expected = strict_json((stage / 'binaries.json').read_bytes())
    require(all(sha('/usr/bin/' + n) == expected[n] for n in ('virmill', 'virmilld')), 'installed manifest mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(os.dup(fd), 'rb') as f: require(hashlib.file_digest(f, 'sha256').hexdigest() == expected['virmill'], 'held binary differs')
    out = stage / 'spice-creation'; out.mkdir(mode=0o700)
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, fd)
    logs, allowed_jobs, plans = [], set(), set()
    report = {'status': 'failed', 'scope': 'product ISO preparation/creation/start and virtual-desktop SPICE connection',
              'acceptanceSupport': ['IMP-01', 'IMP-04', 'IMP-07', 'DEV-04', 'UX-01', 'UX-02'], 'binaries': expected}
    def native(*argv, timeout=30, check=True, env=None):
        result = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, timeout=timeout,
                                env=env or dict(os.environ, LC_ALL='C'))
        require(len(result.stdout) + len(result.stderr) < 2 << 20, 'native output bound')
        logs.append({'argv': list(argv), 'exitCode': result.returncode, 'stdout': result.stdout.decode(errors='replace'), 'stderr': result.stderr.decode(errors='replace')})
        r.save('native-commands.json', logs)
        require(not check or result.returncode == 0, 'native command failed; see evidence')
        return result
    def apply(plan, label, acknowledgements):
        require(set(plan['acknowledgements']) == acknowledgements and len(plan['acknowledgements']) == len(acknowledgements), 'unexpected plan risks')
        require(plan['actorUID'] == 1000, 'wrong plan actor')
        plans.add(plan['planID']); r.save(label + '-plan.json', plan)
        argv = ['plan', 'apply', plan['planID'], '--digest', plan['planDigest'], '--detach', '--idempotency-key', out.parent.name + '-' + label]
        for ack in plan['acknowledgements']: argv += ['--ack', ack]
        job = r.cli(*argv); allowed_jobs.add(job['operationID']); r.save(label + '-accepted.json', job)
        deadline = time.monotonic() + 120
        while job['state'] not in TERMINAL and time.monotonic() < deadline:
            time.sleep(.4); job = r.cli('operation', 'show', job['operationID'])
        r.save(label + '-job.json', job); require(job['state'] == 'succeeded', label + ' did not succeed; retain uncertain resources')
        return job
    before = prior_jobs = media = None; receipt = None; identity = pool_id = source = source_sha = None
    name = 'virmill-spice-' + hashlib.sha256(str(stage).encode()).hexdigest()[:12]
    try:
        before = inventory(r.cli('vm', 'list'), URI); prior_jobs = r.cli('operation', 'list'); media = media_listing(Path.home() / 'images')
        require(all(j['state'] in TERMINAL for j in prior_jobs), 'other operation active')
        require(all(v['name'] != name for v in before.values()), 'fixture name collision')
        r.save('before-vms.json', before); r.save('before-jobs.json', prior_jobs)
        report['nativeVersions'] = native('/usr/bin/rpm', '-q', 'virt-viewer', 'xorg-x11-server-Xvfb', 'libvirt-daemon-driver-qemu', 'qemu-system-x86-core', 'xorriso', 'binutils').stdout.decode()
        choices = r.cli('vm', 'creation', 'options'); r.save('creation-options.json', choices)
        require('spice-unix' in choices['graphics'] and 'sata' in choices['diskBuses'], 'SPICE/SATA not advertised')
        firmware = next((x['firmware'] for x in choices['firmware'] if x['firmware']['mode'] == 'bios'), None)
        require(firmware is not None, 'no advertised BIOS fixture choice')
        pools = [p for p in r.cli('storage', 'pool', 'list') if p['name'] == 'virmill-test' and p['active'] and p['type'] == 'dir']
        require(len(pools) == 1, 'dedicated pool missing or ambiguous'); pool_id = pools[0]['key']['resourceUUID']; r.save('selected-pool.json', pools[0])
        source_dir = out / 'source'; source_dir.mkdir(mode=0o700)
        marker = 'VIRMILL CONSOLE PASS ' + uuid.uuid4().hex
        asm = out / 'boot.S'; asm.write_text(boot_assembly(marker))
        native('/usr/bin/as', '--32', '-o', str(out / 'boot.o'), str(asm))
        native('/usr/bin/ld', '-m', 'elf_i386', '-Ttext', '0x7c00', '--entry=_start', '-o', str(out / 'boot.elf'), str(out / 'boot.o'))
        native('/usr/bin/objcopy', '-O', 'binary', '--only-section=.text', str(out / 'boot.elf'), str(out / 'boot.bin'))
        boot = (out / 'boot.bin').read_bytes(); require(len(boot) == 512 and boot[-2:] == b'\x55\xaa', 'invalid generated BIOS sector')
        (source_dir / 'boot.img').write_bytes(boot + bytes(1536))
        source = out / 'fixture.iso'
        native('/usr/bin/xorriso', '-as', 'mkisofs', '-V', 'VIRMILL_FIXTURE', '-b', 'boot.img', '-no-emul-boot', '-boot-load-size', '4', '-o', str(source), str(source_dir))
        source_sha = sha(source); report.update(sourceSHA256=source_sha, marker=marker, assemblySHA256=sha(asm))
        prep = {'offlineSources': True, 'destination': str(out / 'prepared'), 'mediaID': 'installer', 'sha256': source_sha,
                'disks': [{'id': 'system', 'virtualBytes': 1 << 20}]}
        p = r.cli('import', 'prepare-install', str(source), '--input', json.dumps(prep))
        require(p['operation'] == 'import.prepare-install' and p['review']['destination'] == prep['destination']
                and p['review']['blankDisks'] == prep['disks'] and p['review']['files'][0]['sha256'] == source_sha, 'ISO preparation scope differs')
        prepared = apply(p, 'prepare', {'write-import-artifacts', 'offline-source-files'})
        machine = choices['machine']; chipset = 'q35' if machine == 'q35' or machine.startswith('pc-q35-') else 'i440fx'
        cpu = next((c for c in ('host-model', 'host-passthrough') if c in choices['cpuModes']), None); require(cpu, 'no automatic CPU mode')
        hardware = {'name': name, 'poolID': pool_id, 'architecture': choices['architecture'], 'machine': machine, 'vcpus': 1, 'memoryMiB': 512,
                    'cpu': {'mode': cpu}, 'firmware': firmware, 'clock': 'utc', 'graphics': 'spice-unix', 'guestAgent': False,
                    'disks': [{'sourceID': 'system', 'bus': 'sata', 'bootOrder': 2}],
                    'media': [{'sourceID': 'installer', 'bus': 'sata', 'bootOrder': 1}], 'nics': [],
                    'devicePolicy': {'version': 1, 'chipset': chipset, 'pciPlacement': 'libvirt-auto', 'usbController': 'none',
                                     'memoryBalloon': 'none', 'watchdogAction': 'none', 'input': 'ps2', 'audio': 'none', 'serial': 'isa-serial'}}
        p = r.cli('vm', 'create', prepared['operationID'], '--input', json.dumps({'identityMode': 'clone', 'hardware': hardware}))
        require(p['operation'] == 'vm.create.devices-v1' and p['review']['sourceOperationID'] == prepared['operationID'], 'creation source differs')
        spec = dict(p['review']['target']['spec']); planned_identity = spec.pop('uuid'); require(str(uuid.UUID(planned_identity)) == planned_identity, 'invalid planned UUID')
        spec.setdefault('guestAgent', False)
        require(spec == hardware and not p['review']['startsVM'], 'creation hardware differs from requested fixture')
        created = apply(p, 'create', CREATE_ACKS)
        result = r.cli('vm', 'creation', 'result', created['operationID']); r.save('creation-result.json', result)
        require(result['complete'] and not result['guestBootVerified'], 'creation completion claims differ')
        receipt = result['receipt']; identity = receipt['vmID']; require(identity == planned_identity and identity not in before, 'created wrong VM')
        report.update(vmID=identity, fixtureName=name, creationOperationID=created['operationID'])
        vm = r.cli('vm', 'show', identity); check_owned(vm, receipt, name, pool_id); require(vm['state'] == 'stopped', 'creation unexpectedly started VM')
        r.save('created-vm.json', vm)
        p = r.cli('vm', 'start', identity); require(p['operation'] == 'vm.start' and p['review']['vmID'] == identity, 'start target differs')
        apply(p, 'start', {'host-mutation'})
        vm = r.cli('vm', 'show', identity); check_owned(vm, receipt, name, pool_id); require(vm['state'] == 'running', 'start did not run fixture')
        console = r.cli('vm', 'console', 'show', identity); r.save('console-options.json', console)
        graphics = [c for c in console['choices'] if c['id'] == 'graphics:0']
        require(len(graphics) == 1 and graphics[0]['available'] and graphics[0]['protocol'] == 'spice', 'product-created SPICE not available')
        t = Terminal(r, 'created-console-80x24', 80, 24)
        try:
            def wait(label, predicate, key=None): return t.wait(label, predicate, t.send(key) if key is not None else -1)
            wait('Overview', lambda s: 'Virtual machines' in s)
            wait('VM table', lambda s: 'NAME' in s and 'STATE' in s, b'2')
            wait('Filter focus', lambda s: 'Enter Keep filter' in s, b'/')
            wait('Owned fixture selected', lambda s: name in s and '1 of 1 selected' in s, name.encode())
            wait('Filter kept', lambda s: 'Enter Keep filter' not in s, b'\r')
            wait('VM details', lambda s: 'VM details' in s and name in s, b'\r')
            wait('VM action buttons focused', lambda screen: '>[' in screen, b'\t')
            for index in range(12):
                if re.search(r'>\[ Console \]', t.screen.text()): break
                old = t.screen.text(); wait('Move to Console ' + str(index), lambda screen: screen != old, b'\x1b[C')
            require(re.search(r'>\[ Console \]', t.screen.text()), 'Console button unreachable')
            wait('Console choices', lambda screen: 'Open console' in screen and 'Graphical display' in screen, b'\r')
            for index in range(4):
                if re.search(r'>\s*\[ Graphical display \]', t.screen.text()): break
                old = t.screen.text(); wait('Select display ' + str(index), lambda screen: screen != old, b'\t')
            require(re.search(r'>\s*\[ Graphical display \](?! \(unavailable\))', t.screen.text()), 'Graphical display unavailable')
            report['tuiConsoleDiscovery80x24'] = True
        finally: t.close()
        report['bootMarkerSerial'] = serial_roundtrip(r, identity, marker)
        report['viewer'] = viewer_roundtrip(r, native, identity, name)
        report['status'] = 'passed'
    except BaseException as error: report['error'] = repr(error)
    finally:
        try:
            current_jobs = r.cli('operation', 'list'); accepted = [j for j in current_jobs if j['planID'] in plans]
            allowed_jobs.update(j['operationID'] for j in accepted)
            require(all(j['state'] == 'succeeded' for j in accepted), 'failed/uncertain fixture operation retained for review')
            if receipt is not None:
                vm = r.cli('vm', 'show', identity); check_owned(vm, receipt, name, pool_id)
                if vm['state'] == 'running':
                    p = r.cli('vm', 'stop', identity, '--hard')
                    require(p['operation'] == 'vm.hard-stop' and p['review']['vmID'] == identity, 'hard-stop target differs')
                    apply(p, 'stop', {'host-mutation', 'data-loss-hard-stop'})
                require(r.cli('vm', 'show', identity)['state'] == 'stopped', 'fixture not stopped')
                report['ownedFixtureStoppedAndRetained'] = True
        except BaseException as error: report.update(status='failed', cleanupError=repr(error))
        try:
            if before is not None:
                after = inventory(r.cli('vm', 'list'), URI)
                require(all(after.get(k) == v for k, v in before.items()) and set(after) - set(before) <= ({identity} if identity else set()), 'existing or unexpected guests changed')
                report['existingGuestsPreserved'] = True
            if prior_jobs is not None:
                current_jobs = r.cli('operation', 'list'); indexed = {j['operationID']: j for j in current_jobs}
                require(all(indexed.get(j['operationID']) == j for j in prior_jobs)
                        and set(indexed) - {j['operationID'] for j in prior_jobs} <= allowed_jobs, 'unrelated jobs changed')
                report['existingJobsPreserved'] = True
            if media is not None: require(media_listing(Path.home() / 'images') == media, 'owner source media changed')
            if source_sha is not None: require(sha(source) == source_sha, 'generated source ISO changed')
            report['sourcesPreserved'] = True
        except BaseException as error: report.update(status='failed', preservationError=repr(error))
        r.save('report.json', report); os.close(fd); print(json.dumps(report, sort_keys=True))
    return 0 if report['status'] == 'passed' else 1


class Tests(unittest.TestCase):
    def test_boot_marker_prints_serial_and_graphics(self):
        raw = boot_assembly('VIRMILL CONSOLE PASS ' + 'a' * 32)
        self.assertIn('int $0x10', raw); self.assertIn('outb %al, %dx', raw); self.assertIn('.org 510', raw)
        with self.assertRaises(RuntimeError): boot_assembly('invalid')
    def test_creation_ownership_binds_managed_sources(self):
        identity = '12345678-1234-4234-8234-123456789abc'; pool = '32345678-1234-4234-8234-123456789abc'
        names = ['virmill-' + identity + '-disk-000.qcow2', 'virmill-' + identity + '-media-000.iso']
        receipt = {'vmID': identity, 'binding': 'b' * 64, 'defined': True, 'volumesVerified': True,
                   'volumes': [{'verified': True, 'allocated': {'intent': {'name': name, 'poolID': pool,
                                'contentType': 'cdrom-iso' if i else ''}, 'path': '/pool/' + name}} for i, name in enumerate(names)]}
        raw = '<domain><uuid>' + identity + '</uuid><name>fixture</name><metadata><v:creation xmlns:v="urn:virmill:v1" binding="' + 'b' * 64 + '"/></metadata><devices><graphics type="spice"><listen type="socket"/><clipboard copypaste="no"/><filetransfer enable="no"/></graphics>'
        raw += '<disk type="volume" device="disk"><source pool="virmill-test" volume="' + names[0] + '"/></disk>'
        raw += '<disk type="volume" device="cdrom"><source pool="virmill-test" volume="' + names[1] + '"/><readonly/><boot order="1"/></disk></devices></domain>'
        vm = {'key': {'resourceUUID': identity}, 'name': 'fixture', 'hasManagedSave': False, 'persistentXML': raw}
        check_owned(vm, receipt, 'fixture', pool)
        for original, changed in [('pool="virmill-test"', 'pool="other"'), (names[0], 'foreign.qcow2'), ('device="cdrom"', 'device="disk"')]:
            bad = dict(vm, persistentXML=raw.replace(original, changed))
            with self.assertRaises(RuntimeError): check_owned(bad, receipt, 'fixture', pool)

    def test_private_graphics_refuses_enabled_integrations(self):
        raw = '<domain><devices><graphics type="spice"><listen type="socket"/><clipboard copypaste="no"/><filetransfer enable="no"/></graphics></devices></domain>'
        check_graphics(raw)
        for bad in (raw.replace('copypaste="no"', 'copypaste="yes"'), raw.replace('enable="no"', 'enable="yes"'),
                    raw.replace('type="socket"', 'type="address" address="0.0.0.0"'), raw.replace('</devices>', '<interface/></devices>')):
            with self.assertRaises(RuntimeError): check_graphics(bad)


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__); p.add_argument('--root', type=Path); p.add_argument('--execute-disposable', action='store_true'); p.add_argument('--self-test', action='store_true')
    a = p.parse_args()
    if a.self_test:
        require(a.root is None and not a.execute_disposable, 'self-test cannot select native actions'); unittest.main(argv=[sys.argv[0]])
    else:
        require(a.root is not None and a.execute_disposable, 'explicit disposable root/run required'); sys.exit(execute(a.root))
