#!/usr/bin/env python3
"""Read native SPICE definition normalization on one fresh stopped fixture.

This is adapter feasibility evidence, not product creation or viewer evidence.
No disk, network, guest start, source media access or existing-VM edit occurs.
"""
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import uuid
import xml.etree.ElementTree as ET

p = argparse.ArgumentParser()
p.add_argument('--execute-disposable', action='store_true', required=True)
p.add_argument('--root', type=Path, required=True)
a = p.parse_args()
assert socket.gethostname() in ('virmill-test', 'virmill-test.home') and os.getuid() == 1000
root = a.root.resolve(strict=True)
assert root.parent == Path.home() / 'virmill-tests' and root.stat().st_uid == 1000
out = root / 'spice-normalization'
out.mkdir(mode=0o700)
identity = str(uuid.uuid4())
name = 'virmill-spice-normalize-' + identity[:8]
namespace = 'urn:virmill:spice-normalization-fixture:v1'
report = dict(status='failed', fixtureUUID=identity, fixtureName=name, guestStarted=False)
commands = []

def run(args):
    r = subprocess.run(args, capture_output=True, text=True, timeout=30)
    commands.append(dict(argv=args, exitCode=r.returncode, stdout=r.stdout, stderr=r.stderr))
    (out / 'commands.json').write_text(json.dumps(commands, indent=2) + '\n')
    assert r.returncode == 0, (args, r.stderr)
    return r.stdout

def native(*args):
    return run(['/usr/bin/virsh', '-c', 'qemu:///system', *args])

def inventory():
    return sorted(native('list', '--all', '--uuid').split())

before = inventory()
attempted = False
try:
    caps = ET.fromstring(native('domcapabilities', '--virttype', 'kvm', '--arch', 'x86_64', '--machine', 'pc-i440fx-10.2'))
    assert caps.findtext('machine') == 'pc-i440fx-10.2'
    assert caps.find("devices/graphics[@supported='yes']/enum[@name='type']/value[.='spice']") is not None
    xml = f'''<domain type="kvm"><name>{name}</name><uuid>{identity}</uuid>
<metadata><fixture xmlns="{namespace}" id="{identity}"/></metadata>
<memory unit="MiB">128</memory><currentMemory unit="MiB">128</currentMemory><vcpu>1</vcpu>
<os><type arch="x86_64" machine="pc-i440fx-10.2">hvm</type></os>
<devices><controller type="usb" model="none"/><memballoon model="none"/>
<audio id="1" type="none"/><input type="mouse" bus="ps2"/><input type="keyboard" bus="ps2"/>
<graphics type="spice"><listen type="socket"/><clipboard copypaste="no"/><filetransfer enable="no"/></graphics>
<video><model type="vga"/></video></devices></domain>'''
    (out / 'requested.xml').write_text(xml)
    assert identity not in before
    attempted = True
    native('define', '--validate', str(out / 'requested.xml'))
    observed = native('dumpxml', '--inactive', identity)
    (out / 'observed.xml').write_text(observed)
    doc = ET.fromstring(observed)
    assert doc.findtext('uuid') == identity and doc.findtext('name') == name
    assert doc.find(f'metadata/{{{namespace}}}fixture').get('id') == identity
    assert not doc.findall('devices/disk') and not doc.findall('devices/interface') and not doc.findall('devices/hostdev')
    report['graphicsXML'] = ET.tostring(doc.find('devices/graphics'), encoding='unicode')
    report['status'] = 'passed'
except BaseException as error:
    report['error'] = repr(error)
finally:
    try:
        if attempted and identity in inventory():
            doc = ET.fromstring(native('dumpxml', '--inactive', identity))
            assert doc.findtext('uuid') == identity and doc.findtext('name') == name
            assert doc.find(f'metadata/{{{namespace}}}fixture').get('id') == identity
            assert not doc.findall('devices/disk') and not doc.findall('devices/interface') and not doc.findall('devices/hostdev')
            assert native('domstate', identity).strip() == 'shut off'
            native('undefine', identity)
            report['ownedFixtureUndefined'] = True
        assert inventory() == before
        report['existingGuestInventoryPreserved'] = True
    except BaseException as error:
        report['status'] = 'failed'
        report['cleanupError'] = repr(error)
    (out / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report))
raise SystemExit(report['status'] != 'passed')
