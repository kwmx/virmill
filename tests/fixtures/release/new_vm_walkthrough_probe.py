#!/usr/bin/env python3
"""Owner-authorized New VM walkthrough against the ADR 0065 acceptance bar.

Runs on the authorized test VM as the ordinary test user, in a private
qemu:///session that starts with no pools, networks or VMs (see
pool_setup_probe.py), under a private virtual display (Xvfb). Through the real
TUI it creates and starts one VM from each of a generated qcow2 disk image, a
generated ISO and a generated OVA. The first finds no storage pool, so its one
confirmation also sets up libvirt's standard pool.

Each case must: need exactly one confirmation; take at most six keypresses,
not counting choosing the file; show no UUIDs, digests, resource keys or
acknowledgement identifiers on any screen after the file is chosen; never
switch to the Jobs page; and end with the VM running and its viewer process
started. The host's own pools, networks and VMs must not change.

With --system it walks the host's qemu:///system instead, through the
installed user coordinator, where storage pools and the default network already
exist. There each VM is also removed with its disks afterwards through reviewed
CLI plans, and anything left behind is reported.

The viewer must really show the VM: the probe waits for its window on the
private display and saves a PNG screenshot of that display for review.

Every screen is saved. VMs are hard-stopped through reviewed CLI plans and
viewers are closed; the private coordinator and display are stopped at the end.
Copy tui_workspace_probe.py, one_approval_import_probe.py and
pool_setup_probe.py alongside.
"""
import argparse
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import struct
import tarfile
import time
import zlib
from types import SimpleNamespace
import unittest
import uuid
from tui_workspace_probe import Runner, Terminal, require, canonical_path, authorized_test_host
from one_approval_import_probe import virsh, domains
from pool_setup_probe import URI, SHORT, STAGED, private_environment, sockets_fit, host_inventory, start_coordinator, stop_coordinator, pool_state, hard_stop

MAX_KEYS = 6
# What a person should not have to read: UUIDs, digests, resource keys and
# bracketed acknowledgement identifiers.
RAW = re.compile(r'[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-|\b[0-9a-f]{64}\b|libvirt:qemu|disk-source:|\[[a-z]+(?:-[a-z]+)+\]')
OVF = '''<?xml version="1.0" encoding="UTF-8"?>
<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1" xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData">
 <References><File ovf:id="file1" ovf:href="disk1.vmdk"/></References>
 <DiskSection><Info>Disks</Info><Disk ovf:capacity="67108864" ovf:diskId="vmdisk1" ovf:fileRef="file1" ovf:format="http://www.vmware.com/interfaces/specifications/vmdk.html#streamOptimized"/></DiskSection>
 <VirtualSystem ovf:id="NAME">
  <Info>A generated appliance</Info><Name>NAME</Name>
  <VirtualHardwareSection><Info>Hardware</Info>
   <Item><rasd:ElementName>1 CPU</rasd:ElementName><rasd:InstanceID>1</rasd:InstanceID><rasd:ResourceType>3</rasd:ResourceType><rasd:VirtualQuantity>1</rasd:VirtualQuantity></Item>
   <Item><rasd:AllocationUnits>byte * 2^20</rasd:AllocationUnits><rasd:ElementName>256 MB</rasd:ElementName><rasd:InstanceID>2</rasd:InstanceID><rasd:ResourceType>4</rasd:ResourceType><rasd:VirtualQuantity>256</rasd:VirtualQuantity></Item>
   <Item><rasd:AddressOnParent>0</rasd:AddressOnParent><rasd:ElementName>disk1</rasd:ElementName><rasd:HostResource>ovf:/disk/vmdisk1</rasd:HostResource><rasd:InstanceID>3</rasd:InstanceID><rasd:Parent>controller0</rasd:Parent><rasd:ResourceType>17</rasd:ResourceType></Item>
  </VirtualHardwareSection>
 </VirtualSystem>
</Envelope>
'''


def raw_identities(screen):
    return sorted(set(m.group(0) for m in RAW.finditer(screen)))


def make_sources(src, ident):
    disk = src / f'disk-{ident}.qcow2'
    subprocess.run(['qemu-img', 'create', '-q', '-f', 'qcow2', str(disk), '64M'], check=True, timeout=30)
    content = src / 'iso-content'
    content.mkdir(mode=0o700)
    (content / 'README.TXT').write_text('Virmill walkthrough installer\n')
    iso = src / f'installer-{ident}.iso'
    subprocess.run(['xorriso', '-as', 'mkisofs', '-quiet', '-V', 'VIRMILL_WALK', '-o', str(iso), str(content)], check=True, timeout=60,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    work = src / 'ova-work'
    work.mkdir(mode=0o700)
    subprocess.run(['qemu-img', 'convert', '-q', '-O', 'vmdk', '-o', 'subformat=streamOptimized', str(disk), str(work / 'disk1.vmdk')], check=True, timeout=60)
    name = f'appliance-{ident}'
    (work / 'appliance.ovf').write_text(OVF.replace('NAME', name))
    ova = src / f'{name}.ova'
    with tarfile.open(ova, 'w', format=tarfile.USTAR_FORMAT) as archive:
        for member in ('appliance.ovf', 'disk1.vmdk'):  # The descriptor comes first.
            archive.add(work / member, arcname=member)
    return {'disk': disk, 'iso': iso, 'ova': ova}


def start_display(run):
    (run / 'framebuffer').mkdir(mode=0o700)
    for number in range(90, 140):
        if not Path(f'/tmp/.X11-unix/X{number}').exists() and not Path(f'/tmp/.X{number}-lock').exists():
            log = open(run / 'xvfb.log', 'wb')
            process = subprocess.Popen(['Xvfb', f':{number}', '-nolisten', 'tcp', '-screen', '0', '1280x800x24', '-fbdir', str(run / 'framebuffer')],
                                       stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
            log.close()
            deadline = time.monotonic() + 10
            while not Path(f'/tmp/.X11-unix/X{number}').exists():
                require(process.poll() is None and time.monotonic() < deadline, 'the private display did not start')
                time.sleep(.1)
            return process, f':{number}'
    raise RuntimeError('no free display number')


def xwd_to_png(raw):
    """Convert Xvfb's 24-bit XWD framebuffer to a PNG, and count its colours."""
    fields = struct.unpack('>25I', raw[:100])
    header, depth, width, height, byte_order, bpp, per_line, colours = fields[0], fields[3], fields[4], fields[5], fields[7], fields[11], fields[12], fields[19]
    require(depth == 24 and bpp == 32, 'unexpected framebuffer format')
    data = raw[header + colours * 12:]
    rows, seen = [], set()
    for y in range(height):
        line = data[y * per_line:y * per_line + width * 4]
        row = bytearray([0])
        for x in range(0, width * 4, 4):
            b, g, r = (line[x], line[x + 1], line[x + 2]) if byte_order == 0 else (line[x + 3], line[x + 2], line[x + 1])
            row += bytes((r, g, b))
            if len(seen) < 64:
                seen.add((r, g, b))
        rows.append(bytes(row))
    chunk = lambda kind, body: struct.pack('>I', len(body)) + kind + body + struct.pack('>I', zlib.crc32(kind + body) & 0xffffffff)
    png = b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', width, height, 8, 2, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(b''.join(rows), 6)) + chunk(b'IEND', b'')
    return png, len(seen)


def viewer_window(display, name, deadline=30):
    """The viewer's mapped top-level window naming the VM, from xwininfo."""
    end = time.monotonic() + deadline
    while time.monotonic() < end:
        tree = subprocess.run(['xwininfo', '-root', '-children', '-display', display], capture_output=True, text=True).stdout
        for line in tree.splitlines():
            if name in line and 'x' in line:
                info = subprocess.run(['xwininfo', '-id', line.split()[0], '-display', display], capture_output=True, text=True).stdout
                if 'Map State: IsViewable' in info:
                    size = re.search(r'Width: (\d+).*?Height: (\d+)', info, re.DOTALL)
                    return {'title': line.split('"')[1] if '"' in line else line.strip(), 'size': [int(size.group(1)), int(size.group(2))] if size else None}
        time.sleep(1)
    return None


def viewers(vm_uuid):
    """This user's virt-viewer processes attached to the VM."""
    out = subprocess.run(['pgrep', '-u', str(os.getuid()), '-a', 'virt-viewer'], capture_output=True, text=True).stdout
    return [line for line in out.splitlines() if vm_uuid in line and '--attach' in line]


def walkthrough(runner, kind, source, imports, deadline_seconds, uri=URI):
    terminal = Terminal(runner, 'new-vm-' + kind, 120, 36)
    report = {'case': kind, 'source': source.name}
    counted = []
    confirmations = 0
    seen = []

    def key(raw, why):
        counted.append(why)
        return terminal.send(raw)

    def wait(label, predicate, fresh=-1, record=True):
        terminal.started = time.monotonic()
        screen = terminal.wait(label, predicate, fresh)
        if record:
            seen.append((label, screen))
        return screen

    try:
        picker = lambda text: re.search(r'(?:^|[│|])Choose a file[ \t]*$', text, re.MULTILINE) is not None

        def choose(entry, label):
            wait(label + ' filter', lambda text: picker(text) and 'Find:' in text, terminal.send(b'/'), False)
            wait(label + ' typed', lambda text: picker(text) and ('Find: ' + entry) in text, terminal.send(entry.encode()), False)
            wait(label + ' kept', lambda text: picker(text) and 'Enter open/select' in text, terminal.send(b'\r'), False)

        wait('Overview', lambda text: 'Virmill' in text and uri in text and 'Loading jobs' not in text, record=False)
        wait('New VM opens the file chooser', picker, key(b'n', 'New VM'), False)
        for part in source.relative_to(Path.home()).parts[:-1]:
            choose(part, 'folder ' + part)
            wait('open ' + part, lambda text, part=part: picker(text) and part in text, terminal.send(b'\r'), False)
        choose(source.name, 'source file')
        terminal.send(b'\r')  # Select the filtered file; choosing it is not counted.
        started = time.monotonic()
        settings = None
        while settings is None:
            require(time.monotonic() - started < 120, 'the settings page did not appear')
            terminal.started = time.monotonic()
            terminal.read(.5)
            text = terminal.screen.text()
            require('Error:' not in text, 'reading the file failed: ' + text[-600:])
            if '> [ Create VM ]' in text and 'New VM' in text:
                settings = wait('Settings page', lambda text: '> [ Create VM ]' in text)
        report['settings'] = {'storage': next((l.strip() for l in settings.splitlines() if 'Storage:' in l), None),
                              'network': next((l.strip() for l in settings.splitlines() if 'Network:' in l), None),
                              'display': next((l.strip() for l in settings.splitlines() if 'Display:' in l), None),
                              'startOn': '[x] Start it and open its display' in settings}
        before = domains(uri)
        key(b'\r', 'Create VM')
        started = time.monotonic()
        while 'By confirming, you agree that:' not in terminal.screen.text():
            require(time.monotonic() - started < deadline_seconds, 'the confirmation did not appear')
            text = terminal.screen.text()
            require('Open Advanced settings' not in text and 'Error:' not in text, 'Create VM was refused: ' + text[-800:])
            terminal.started = time.monotonic()
            terminal.read(.5)
        confirmation = wait('The one confirmation', lambda text: 'By confirming, you agree that:' in text)
        confirmations += 1
        report['confirmationSetsUpPool'] = 'First, Virmill creates storage pool default' in confirmation or 'First, Virmill starts storage pool' in confirmation
        key(b'\r', 'Confirm')
        created, viewer, progress_seen, job_page = None, [], False, False
        started = time.monotonic()
        while time.monotonic() - started < deadline_seconds:
            terminal.started = time.monotonic()
            terminal.read(1)
            text = terminal.screen.text()
            head = text.split('\n')[0]
            job_page = job_page or bool(re.search(r'Virmill\s*/\s*Jobs\b', head)) or 'Job details' in text
            # The confirmation stays up until its jobs are accepted; one that
            # appears after progress is a second confirmation.
            require(not (progress_seen and 'By confirming, you agree that:' in text), 'a second confirmation appeared: ' + text[-800:])
            require('needs attention' not in text, 'a step failed: ' + text[-800:])
            if 'Creating ' in text and '[>]' in text and not progress_seen:
                progress_seen = True
                seen.append(('Progress', text))
            new = domains(uri) - before
            if new:
                created = sorted(new)[0]
                uuid_text = virsh(uri, 'domuuid', created).strip()
                viewer = viewers(uuid_text)
                if virsh(uri, 'domstate', created).strip() == 'running' and viewer and ' is running' in text and '[x] Open its display' in text and not any(imports.iterdir()):
                    seen.append(('Finished', text))
                    break
        require(created is not None, 'no VM was created')
        report['vm'] = created
        report['running'] = virsh(uri, 'domstate', created).strip() == 'running'
        report['viewerStarted'] = bool(viewer)
        report['progressOnSameScreen'] = progress_seen and not job_page
        report['preparedCopyRemoved'] = not any(imports.iterdir())
        report['finishedScreen'] = seen[-1][0] == 'Finished'
        report['keypresses'] = len(counted)
        report['keys'] = counted
        report['confirmations'] = confirmations
        report['rawIdentities'] = {label: found for label, screen in seen if (found := raw_identities(screen))}
        report['seconds'] = round(time.monotonic() - started)
        window = viewer_window(os.environ['DISPLAY'], created)
        report['viewerWindow'] = window
        if window:
            time.sleep(3)  # Let the guest's first frame arrive.
            png, colours = xwd_to_png((runner.directory / 'framebuffer' / 'Xvfb_screen0').read_bytes())
            runner.save(f'viewer-{kind}.png', png)
            report['viewerScreenshot'] = {'file': f'viewer-{kind}.png', 'distinctColours': colours}
        terminal.send(b'\x1b')  # Done, after the VM runs; not counted.
        terminal.read(1)
        return report, uuid_text
    finally:
        terminal.close()


def execute(root, deadline_seconds):
    require(authorized_test_host() and os.getuid() == 1000 and os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(root.absolute()))
    require(stage.is_relative_to(Path.home() / 'virmill-tests') and stage != Path.home() / 'virmill-tests' and
            stat.S_ISDIR(stage.lstat().st_mode) and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    user_runtime = Path(f'/run/user/{os.getuid()}')
    info = user_runtime.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) == 0o700, 'private user runtime folder required')
    ident = uuid.uuid4().hex[:8]
    run, short = stage / ('newvm-' + ident), user_runtime / ('vp-' + ident)
    require(sockets_fit(short), 'libvirt socket paths would exceed 108 bytes')
    for folder in (run, run / 'src', *(run / f for f in STAGED.values()), short, *(short / f for f in SHORT.values())):
        folder.mkdir(mode=0o700)
    regular = dict(os.environ)
    host_before = host_inventory(regular)
    display, display_name = start_display(run)
    os.environ.update(private_environment(run, short))
    os.environ.pop('WAYLAND_DISPLAY', None)
    os.environ['DISPLAY'] = display_name
    require(domains(URI) == set() and pool_state()['names'] == [], 'the private session is not empty')
    report = {'hostInventoryBefore': host_before, 'privateSessionEmptyAtStart': True, 'cases': []}
    coordinator = start_coordinator(run, short)
    viewer_ids = []
    try:
        fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_CLOEXEC)
        runner = Runner(SimpleNamespace(binary='/usr/bin/virmill', connection=URI), run, fd)
        imports = run / 'data' / 'virmill' / 'imports'
        sources = make_sources(run / 'src', ident)
        for kind in ('disk', 'iso', 'ova'):
            case, vm_uuid = walkthrough(runner, kind, sources[kind], imports, deadline_seconds)
            viewer_ids.append(vm_uuid)
            case['passed'] = (case['confirmations'] == 1 and case['keypresses'] <= MAX_KEYS and not case['rawIdentities'] and
                              case['progressOnSameScreen'] and case['running'] and case['viewerStarted'] and case['preparedCopyRemoved'])
            report['cases'].append(case)
            hard_stop(runner, case['vm'], f'newvm-stop-{kind}-{ident}')
            for line in viewers(vm_uuid):
                subprocess.run(['kill', line.split()[0]], check=False)
            require(case['passed'], kind + ' did not meet the acceptance bar: ' + json.dumps(case, sort_keys=True))
        report['firstCaseSetUpPool'] = report['cases'][0]['confirmationSetsUpPool']
        require(report['firstCaseSetUpPool'], 'the first confirmation did not include the storage pool')
        pool = pool_state()
        report['pool'] = {'names': pool['names'], 'active': pool['active'], 'inPrivateDataFolder': pool['path'] == str(run / 'data' / 'libvirt' / 'images')}
        ops = {}
        for job in runner.cli('operation', 'list'):
            ops.setdefault(job.get('operation'), []).append(job['state'])
        expected = {'storage.pool.create': 1, 'import.prepare-disks': 1, 'import.prepare-install': 1, 'import.prepare': 1,
                    'vm.create.devices-v1': 3, 'vm.start': 3, 'import.discard': 3, 'vm.hard-stop': 3}
        report['jobs'] = {op: len(states) for op, states in sorted(ops.items())}
        require(report['jobs'] == expected and all(state == 'succeeded' for states in ops.values() for state in states),
                'expected exactly these succeeded jobs: ' + json.dumps(expected, sort_keys=True))
    finally:
        stop_coordinator(coordinator)
        for name in domains(URI):
            if virsh(URI, 'domstate', name).strip() == 'running':
                virsh(URI, 'destroy', name)  # Only after a failure; the private session is the probe's own.
                report.setdefault('stoppedAfterFailure', []).append(name)
        for vm_uuid in viewer_ids:
            for line in viewers(vm_uuid):
                subprocess.run(['kill', line.split()[0]], check=False)
        stop_coordinator(display)
        (run / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
        os.chmod(run / 'report.json', 0o600)
    report['hostInventoryUnchanged'] = host_inventory(regular) == host_before
    require(report['hostInventoryUnchanged'], 'the host\'s own pools, networks or VMs changed')
    report['result'] = 'passed'
    (run / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
    print(json.dumps({k: v for k, v in report.items() if k != 'hostInventoryBefore'}, indent=2, sort_keys=True))


def cli_apply(runner, plan, key):
    argv = ['plan', 'apply', plan['planID'], '--digest', plan['planDigest'], '--idempotency-key', key, '--wait', '--timeout', '120s']
    for ack in plan['acknowledgements']:
        argv += ['--ack', ack]
    return runner.cli(*argv)


def remove_created(runner, uri, name, vm_uuid, key):
    """Stop, then remove a VM this probe created, with all its disks and media."""
    out = {}
    stop = runner.cli('vm', 'stop', vm_uuid, '--hard')
    require(set(stop['acknowledgements']) <= {'host-mutation', 'data-loss-hard-stop'}, 'stop review asks for unexpected consequences')
    out['stopped'] = cli_apply(runner, stop, key + '-stop')['state']
    targets = [line.split()[2] for line in virsh(uri, 'domblklist', name, '--details').splitlines()[2:] if len(line.split()) >= 4]
    out['targets'] = targets
    try:
        plan = runner.cli('vm', 'remove', vm_uuid, '--delete-disk', ','.join(targets))
        out['removed'] = cli_apply(runner, plan, key + '-remove')['state']
    except RuntimeError as error:
        out['removeRefused'] = str(error)[:400]
    out['stillDefined'] = name in domains(uri)
    return out


def pool_volumes(uri):
    out = {}
    for pool in virsh(uri, 'pool-list', '--name').split():
        # A pool this user cannot read is compared as unreadable, not skipped.
        process = subprocess.run(['virsh', '-c', uri, 'vol-list', pool, '--name'], stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30)
        out[pool] = sorted(process.stdout.split()) if process.returncode == 0 else ['<unreadable>']
    return out


def execute_system(root, deadline_seconds):
    uri = 'qemu:///system'
    require(authorized_test_host() and os.getuid() == 1000 and os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = canonical_path(str(root.absolute()))
    require(stage.is_relative_to(Path.home() / 'virmill-tests') and stage != Path.home() / 'virmill-tests' and
            stat.S_ISDIR(stage.lstat().st_mode) and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    ident = uuid.uuid4().hex[:8]
    run = stage / ('newvm-system-' + ident)
    for folder in (run, run / 'src', *(run / f for f in STAGED.values())):
        folder.mkdir(mode=0o700)
    regular = dict(os.environ)
    host_before, volumes_before = host_inventory(regular), pool_volumes(uri)
    display, display_name = start_display(run)
    # The installed coordinator does the work; the TUI's drafts and prepared
    # copies stay in the stage.
    os.environ.update({key: str(run / folder) for key, folder in STAGED.items()} | {'VIRMILL_UPDATE_CHECK': '0'})
    os.environ.pop('WAYLAND_DISPLAY', None)
    os.environ['DISPLAY'] = display_name
    report = {'connection': uri, 'cases': [], 'cleanup': []}
    viewer_ids = []
    try:
        fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_CLOEXEC)
        runner = Runner(SimpleNamespace(binary='/usr/bin/virmill', connection=uri), run, fd)
        jobs_before = {job['operationID'] for job in runner.cli('operation', 'list')}
        imports = run / 'data' / 'virmill' / 'imports'
        sources = make_sources(run / 'src', ident)
        for kind in ('disk', 'iso', 'ova'):
            case, vm_uuid = walkthrough(runner, kind, sources[kind], imports, deadline_seconds, uri)
            viewer_ids.append(vm_uuid)
            shot = case.get('viewerScreenshot') or {}
            case['passed'] = (case['confirmations'] == 1 and case['keypresses'] <= MAX_KEYS and not case['rawIdentities'] and
                              case['progressOnSameScreen'] and case['running'] and case['viewerStarted'] and case['preparedCopyRemoved'] and
                              case['viewerWindow'] is not None and shot.get('distinctColours', 0) > 1)
            report['cases'].append(case)
            for line in viewers(vm_uuid):
                subprocess.run(['kill', line.split()[0]], check=False)
            report['cleanup'].append(remove_created(runner, uri, case['vm'], vm_uuid, f'newvm-system-{kind}-{ident}'))
            require(case['passed'], kind + ' did not meet the acceptance bar: ' + json.dumps(case, sort_keys=True))
        jobs = [job for job in runner.cli('operation', 'list') if job['operationID'] not in jobs_before]
        report['jobs'] = sorted(f"{job.get('operation')} {job['state']}" for job in jobs)
        require(all(job['state'] == 'succeeded' for job in jobs), 'a job did not succeed')
    finally:
        for vm_uuid in viewer_ids:
            for line in viewers(vm_uuid):
                subprocess.run(['kill', line.split()[0]], check=False)
        stop_coordinator(display)
        (run / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
        os.chmod(run / 'report.json', 0o600)
    after = pool_volumes(uri)
    report['leftVolumes'] = {pool: sorted(set(names) - set(volumes_before.get(pool, []))) for pool, names in after.items() if set(names) - set(volumes_before.get(pool, []))}
    report['hostInventoryUnchanged'] = host_inventory(regular) == host_before
    require(report['hostInventoryUnchanged'], 'the host\'s pools, networks or VMs are not back to where they were')
    report['result'] = 'passed'
    (run / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
    print(json.dumps(report, indent=2, sort_keys=True))


class ProbeTests(unittest.TestCase):
    def test_raw_identities_are_found(self):
        self.assertEqual(raw_identities('Plan: 99999999-9999-4999-8999-999999999993'), ['99999999-9999-4999-'])
        self.assertEqual(raw_identities('  Allow the reviewed changes [host-mutation]'), ['[host-mutation]'])
        self.assertEqual(raw_identities('libvirt:qemu:///session:vm:x'), ['libvirt:qemu'])

    def test_plain_screens_have_none(self):
        self.assertEqual(raw_identities(' Virmill  / New VM   qemu:///session\n> [ Create VM ]\n[x] Start it and open its display'), [])

    def test_framebuffer_becomes_a_png(self):
        header = struct.pack('>25I', 100, 7, 2, 24, 2, 1, 0, 0, 32, 0, 32, 32, 8, 4, 0xff0000, 0xff00, 0xff, 8, 0, 0, 2, 1, 0, 0, 0)
        png, colours = xwd_to_png(header + bytes([0, 0, 255, 0, 255, 0, 0, 0]))
        self.assertTrue(png.startswith(b'\x89PNG') and colours == 2)
        rows = zlib.decompress(png[png.index(b'IDAT') + 4:png.index(b'IEND') - 8])
        self.assertEqual(rows, bytes([0, 255, 0, 0, 0, 0, 255]))

    def test_ovf_names_the_appliance(self):
        self.assertIn('<Name>appliance-x</Name>', OVF.replace('NAME', 'appliance-x'))


def main():
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    p.add_argument('--execute-disposable', action='store_true')
    p.add_argument('--root', type=Path)
    p.add_argument('--deadline', type=int, default=300, help='seconds allowed for each case to reach a running VM')
    p.add_argument('--self-test', action='store_true')
    p.add_argument('--system', action='store_true', help='walk qemu:///system through the installed coordinator')
    a = p.parse_args()
    if a.self_test or not a.execute_disposable:
        unittest.main(argv=['new_vm_walkthrough_probe'], exit=True)
    require(a.root is not None, '--root is required')
    (execute_system if a.system else execute)(a.root, a.deadline)


if __name__ == '__main__':
    main()
