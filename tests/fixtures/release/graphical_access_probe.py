#!/usr/bin/env python3
"""Guarded native SPICE viewer probe; root agent is the sole remote operator.

--root is a staged private ~/virmill-tests/RUN with binaries.json. Exclusively
creates graphical-access beneath it, one marked transient diskless BIOS guest,
and a cookie-protected Xvfb display without TCP. No supplied disk, guest, network,
or host configuration is changed. Main+display SPICE channels from the guest's
read-only QMP query and an X11 viewer window prove connection, beyond a process
merely existing. Retains a framebuffer PNG for human review; no OS-login claim.
Only this fixture's marked guest is destroyed in finally. No downloads or SSH.
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
import struct
import subprocess
import time
import uuid
import xml.etree.ElementTree as ET
import zlib

class Blocked(RuntimeError):
    pass


URI = 'qemu:///system'
NS = 'urn:virmill:graphical-fixture:v1'


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def xml_for(identity, name):
    require(str(uuid.UUID(identity)) == identity and uuid.UUID(identity).int != 0, 'canonical UUID required')
    require(name == 'virmill-graphics-' + identity[:8], 'fixture name differs')
    return f'''<domain type='qemu'><name>{name}</name><uuid>{identity}</uuid>
<metadata><fixture xmlns='{NS}' id='{identity}'/></metadata>
<memory unit='MiB'>128</memory><vcpu>1</vcpu>
<os><type arch='x86_64' machine='pc'>hvm</type><boot dev='hd'/></os>
<on_poweroff>destroy</on_poweroff><on_reboot>destroy</on_reboot><on_crash>destroy</on_crash>
<devices><emulator>/usr/bin/qemu-system-x86_64</emulator>
<graphics type='spice' autoport='yes'><listen type='address' address='127.0.0.1'/></graphics>
<video><model type='vga' vram='16384' heads='1' primary='yes'/></video>
<input type='keyboard' bus='ps2'/><input type='mouse' bus='ps2'/>
</devices></domain>'''


def check_owned(raw, identity, name):
    root = ET.fromstring(raw)
    marker = root.find(f'metadata/{{{NS}}}fixture')
    require(root.findtext('uuid') == identity and root.findtext('name') == name and marker is not None and marker.get('id') == identity, 'fixture identity/ownership differs; refuse cleanup')
    require(not root.findall('devices/disk') and not root.findall('devices/interface'), 'fixture unexpectedly contains disks or NICs')


def xauthority(path, display):
    def field(value):
        value = value.encode() if isinstance(value, str) else value
        return struct.pack('>H', len(value)) + value
    cookie = os.urandom(16)
    # FamilyWild avoids hostname aliases; the display number and secret cookie
    # remain exact. File/directory permissions prevent other users reading it.
    data = struct.pack('>H', 65535) + field('') + field(str(display)) + field('MIT-MAGIC-COOKIE-1') + field(cookie)
    with path.open('xb') as stream:
        stream.write(data)
    path.chmod(0o600)


def framebuffer_png(source, destination):
    data = source.read_bytes()
    require(len(data) >= 100, 'missing Xvfb framebuffer header')
    h = struct.unpack('>25I', data[:100])
    header, version, fmt, depth, width, height, offset, order, _, _, _, bpp, stride, _, red, green, blue, _, _, colors, *_ = h
    require(version == 7 and fmt == 2 and depth == 24 and bpp == 32 and offset == 0 and 0 < width <= 2048 and 0 < height <= 2048 and stride >= width * 4, 'unsupported framebuffer layout')
    pixels = data[header + colors * 12:]
    require(len(pixels) >= stride * height, 'incomplete framebuffer')
    def component(value, mask):
        shift = (mask & -mask).bit_length() - 1
        return ((value & mask) >> shift) * 255 // (mask >> shift)
    require(all(mask > 0 for mask in (red, green, blue)), 'invalid pixel masks')
    raw = bytearray()
    for y in range(height):
        raw.append(0)
        for x in range(width):
            pos = y * stride + x * 4
            value = int.from_bytes(pixels[pos:pos + 4], 'little' if order == 0 else 'big')
            raw.extend(component(value, mask) for mask in (red, green, blue))
    def chunk(kind, body):
        return struct.pack('>I', len(body)) + kind + body + struct.pack('>I', zlib.crc32(kind + body))
    destination.write_bytes(b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', width, height, 8, 2, 0, 0, 0)) + chunk(b'IDAT', zlib.compress(raw)) + chunk(b'IEND', b''))
    return {'width': width, 'height': height, 'sha256': sha(destination)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--execute-disposable', action='store_true', required=True)
    parser.add_argument('--root', type=Path, required=True)
    args = parser.parse_args()
    require(socket.gethostname() in ('virmill-test', 'virmill-test.home') and os.getuid() == 1000 and os.geteuid() == 1000, 'wrong authorized host/actor')
    stage = args.root.absolute()
    require(stage == stage.resolve(strict=True) and stage.is_relative_to(Path.home() / 'virmill-tests') and stage != Path.home() / 'virmill-tests', 'canonical staged test root required')
    require(stage.stat().st_uid == os.getuid() and stat.S_IMODE(stage.stat().st_mode) & 0o077 == 0, 'staging root must be private to actor')
    expected = json.loads((stage / 'binaries.json').read_text())
    require(all(sha('/usr/bin/' + name) == expected[name] for name in ('virmill', 'virmilld')), 'installed binaries differ')
    out = stage / 'graphical-access'
    out.mkdir(mode=0o700)
    report = {'status': 'failed', 'scope': 'real SPICE main/display channels, external CLI viewer and framebuffer; no OS login or guest integration claim', 'binaries': expected}
    logs = []
    identity = str(uuid.uuid4())
    name = 'virmill-graphics-' + identity[:8]
    viewer = cli_process = xvfb = None
    master = None
    before = media = jobs = None
    creation_attempted = False
    transcript = bytearray()
    viewer_start = None

    def run(argv, timeout=20, check=True, env=None):
        result = subprocess.run(argv, capture_output=True, text=True, timeout=timeout, env=env)
        logs.append({'argv': argv, 'exitCode': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr})
        (out / 'commands.json').write_text(json.dumps(logs, indent=2) + '\n')
        if check:
            require(result.returncode == 0, f'{argv[0]} failed: {result.stderr[:1000]}')
        return result

    def virsh(*argv, check=True):
        return run(['/usr/bin/sudo', '-n', '/usr/bin/virsh', '--connect', URI, *argv], check=check).stdout

    def cli(*argv):
        result = json.loads(run(['/usr/bin/virmill', *argv, '--output', 'json', '--non-interactive']).stdout)
        require(result.get('error') is None, str(result.get('error')))
        return result['data']

    def guests():
        ids = sorted(virsh('list', '--all', '--uuid').split())
        return {item: {'xml': virsh('dumpxml', item), 'state': virsh('domstate', item).strip()} for item in ids if item != identity}

    def media_state():
        root = Path.home() / 'images'
        result = {}
        if root.exists():
            for entry in [root, *root.rglob('*')]:
                st = entry.lstat()
                result[str(entry.relative_to(root))] = [st.st_dev, st.st_ino, st.st_mode, st.st_size, st.st_mtime_ns, st.st_ctime_ns]
        return result

    def spice():
        return json.loads(virsh('qemu-monitor-command', identity, '{"execute":"query-spice"}'))['return']

    def drain():
        if master is None:
            return
        while select.select([master], [], [], 0)[0]:
            try:
                block = os.read(master, 65536)
            except OSError:
                return
            if not block:
                return
            transcript.extend(block)
            require(len(transcript) <= 1 << 20, 'console output exceeded fixture bound')

    try:
        missing = [path for path in ('/usr/bin/Xvfb', '/usr/bin/xwininfo', '/usr/bin/virt-viewer', '/usr/bin/virsh') if not Path(path).is_file()]
        if missing:
            raise Blocked('Missing administrator-installed prerequisites: ' + ', '.join(missing))
        report['versions'] = run(['/usr/bin/rpm', '-q', 'virt-viewer', 'xorg-x11-server-Xvfb', 'libvirt-daemon-driver-qemu', 'qemu-system-x86-core'], check=False).stdout
        before, media, jobs = guests(), media_state(), cli('operation', 'list')
        require(identity not in before and all(name not in vm['xml'] for vm in before.values()), 'fixture identity collision')
        for display in range(90, 190):
            if not Path(f'/tmp/.X11-unix/X{display}').exists() and not Path(f'/tmp/.X{display}-lock').exists():
                break
        else:
            raise RuntimeError('no unused private X display')
        authority = out / 'Xauthority'
        xauthority(authority, display)
        env = dict(os.environ, DISPLAY=f':{display}', XAUTHORITY=str(authority))
        env.pop('WAYLAND_DISPLAY', None)
        xvfb_log = (out / 'xvfb.log').open('xb')
        xvfb = subprocess.Popen(['/usr/bin/Xvfb', f':{display}', '-screen', '0', '1024x768x24', '-nolisten', 'tcp', '-auth', str(authority), '-fbdir', str(out)], stdout=xvfb_log, stderr=subprocess.STDOUT)
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            require(xvfb.poll() is None, 'private Xvfb failed')
            if run(['/usr/bin/xwininfo', '-root'], check=False, env=env).returncode == 0:
                break
            time.sleep(.2)
        else:
            raise RuntimeError('private Xvfb did not become ready')
        definition = out / 'fixture.xml'
        definition.write_text(xml_for(identity, name))
        report.update(fixtureUUID=identity, fixtureName=name, definitionSHA256=sha(definition), display=f':{display}')
        creation_attempted = True
        result = run(['/usr/bin/sudo', '-n', '/usr/bin/virsh', '--connect', URI, 'create', str(definition), '--validate'], check=False)
        if result.returncode:
            raise Blocked('Native QEMU could not start the diskless BIOS SPICE fixture: ' + result.stderr[:1000])
        check_owned(virsh('dumpxml', identity), identity, name)
        initial = spice()
        require(initial.get('enabled') is True and not initial.get('channels'), 'SPICE was unavailable or already connected before viewer')
        options = cli('vm', 'console', 'show', identity)
        (out / 'console-options.json').write_text(json.dumps(options, indent=2) + '\n')
        choice = next((choice for choice in options['choices'] if choice['id'] == 'graphics:0'), None)
        require(choice is not None and choice['available'] and choice['protocol'] == 'spice', 'fixture console not offered')
        master, slave = pty.openpty()
        cli_process = subprocess.Popen(['/usr/bin/virmill', 'vm', 'console', 'open', identity, '--choice', 'graphics:0'], stdin=slave, stdout=slave, stderr=slave, env=env, start_new_session=True)
        os.close(slave)
        deadline = time.monotonic() + 30
        connected = None
        while time.monotonic() < deadline:
            drain()
            require(cli_process.poll() is None, 'Virmill console exited before viewer connection: ' + transcript.decode(errors='replace'))
            children = Path(f'/proc/{cli_process.pid}/task/{cli_process.pid}/children').read_text().split()
            for child in children:
                try:
                    if Path(f'/proc/{child}/exe').resolve(strict=True) == Path('/usr/bin/virt-viewer'):
                        viewer = int(child)
                        viewer_start = Path(f'/proc/{child}/stat').read_text().split()[21]
                except FileNotFoundError:
                    pass
            observed = spice()
            types = {channel.get('channel-type') for channel in observed.get('channels', [])}
            tree = run(['/usr/bin/xwininfo', '-root', '-tree'], env=env).stdout
            if viewer and {1, 2}.issubset(types) and name in tree:
                connected = observed
                break
            time.sleep(.3)
        require(connected is not None, 'no verified SPICE main+display connection and viewer window')
        report['connectedSPICE'] = connected
        (out / 'viewer-window.txt').write_text(tree)
        time.sleep(.5)
        report['framebuffer'] = framebuffer_png(out / 'Xvfb_screen0', out / 'viewer.png')
        require(Path(f'/proc/{viewer}/exe').resolve(strict=True) == Path('/usr/bin/virt-viewer') and Path(f'/proc/{viewer}/stat').read_text().split()[21] == viewer_start, 'viewer PID changed')
        # virt-viewer registers a SIGINT handler which closes the session through
        # its normal quit path; no QMP monitor ports exist in this fixture.
        os.kill(viewer, signal.SIGINT)
        deadline = time.monotonic() + 15
        while cli_process.poll() is None and time.monotonic() < deadline:
            drain()
            time.sleep(.1)
        require(cli_process.poll() == 0, 'viewer did not return cleanly to CLI')
        require(virsh('domstate', identity).strip() == 'running', 'closing viewer stopped the guest')
        require(not spice().get('channels'), 'viewer connection remained after close')
        report.update(status='passed', viewerExitCode=cli_process.returncode, guestRunningAfterViewerClose=True)
    except Blocked as exc:
        report.update(status='blocked', reason=str(exc))
    except BaseException as exc:
        report.update(status='failed', error=repr(exc))
    finally:
        cleanup_errors = []
        try:
            if cli_process is not None and cli_process.poll() is None:
                os.killpg(cli_process.pid, signal.SIGTERM)
                cli_process.wait(timeout=5)
            drain()
            if master is not None:
                os.close(master)
        except BaseException as exc:
            cleanup_errors.append('console process: ' + repr(exc))
        (out / 'console.pty').write_bytes(transcript)
        try:
            if creation_attempted:
                current = run(['/usr/bin/sudo', '-n', '/usr/bin/virsh', '--connect', URI, 'dumpxml', identity], check=False)
                if current.returncode == 0:
                    check_owned(current.stdout, identity, name)
                    virsh('destroy', identity)
                    require(identity not in virsh('list', '--all', '--uuid').split(), 'transient fixture was not removed')
                    report['fixtureStoppedAndRemoved'] = True
        except BaseException as exc:
            cleanup_errors.append('fixture guest: ' + repr(exc))
        try:
            if xvfb is not None:
                if xvfb.poll() is None:
                    xvfb.terminate()
                xvfb.wait(timeout=5)
        except BaseException as exc:
            cleanup_errors.append('private display: ' + repr(exc))
        try:
            if before is not None:
                report['existingGuestsPreserved'] = guests() == before
                report['sourceMediaMetadataPreserved'] = media_state() == media
                report['operationJournalPreserved'] = cli('operation', 'list') == jobs
                require(all(report[key] for key in ('existingGuestsPreserved', 'sourceMediaMetadataPreserved', 'operationJournalPreserved')), 'existing state changed')
        except BaseException as exc:
            cleanup_errors.append('preservation checks: ' + repr(exc))
        if cleanup_errors:
            report.update(status='failed', cleanupErrors=cleanup_errors)
        (out / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(report, sort_keys=True))
    return 0 if report['status'] == 'passed' else 2 if report['status'] == 'blocked' else 1


if __name__ == '__main__':
    raise SystemExit(main())
