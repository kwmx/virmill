#!/usr/bin/env python3
"""Native Fedora44 guest-tools installation on one exclusively owned guest.

Parent-operated only: --execute-disposable --root PRIVATE_STAGED_DIRECTORY.
Requires adjacent tui_workspace_probe.py and binaries.json. Copies the exact
previously inspected Fedora source copy; all image parsers run as UID1000.
Offline customization is test scaffolding, not a product first-boot claim.
Only this new guest is modified. Its existing default NAT is never edited.
Virmill reviews/applies start, guest tools, repeat and stop. Source media,
preexisting guests/jobs/network definitions are checked for preservation.
Keys and generated disks remain private for investigation. No forced shutdown,
undefine, deletion, host package install, or source modification. Failed/uncertain
jobs retain the fixture running rather than race a still-running guest mutation.
--self-test performs pure generation/validation checks, without native actions.
"""
import argparse
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import shutil
import socket
import stat
import subprocess
import sys
import time
import unittest
import uuid
import xml.etree.ElementTree as ET
from tui_workspace_probe import Runner, Terminal, canonical_path, inventory, media_listing, require, strict_json


def designated_pool():
    """The dedicated libvirt pool named in ~/.config/virmill-tests/storage-pool on the test host."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/storage-pool')) as f:
            return f.read().strip()
    except OSError:
        return 'unconfigured-pool'  # no such pool exists, so native runs refuse


TEST_POOL = designated_pool()



def authorized_test_host():
    """True only on a host that lists its own name in ~/.config/virmill-tests/authorized-hosts."""
    try:
        with open(os.path.expanduser('~/.config/virmill-tests/authorized-hosts')) as f:
            return socket.gethostname() in f.read().split()
    except OSError:
        return False


URI = 'qemu:///system'
SOURCE_VM = '2ec994ce-2950-498c-8b19-d2f7dbb53a78'
SOURCE = Path('/var/lib/libvirt/images') / TEST_POOL / 'nested-fedora.qcow2'
COPY = Path.home() / 'virmill-tests/guest-tools-fedora-copy-001/fedora-copy.qcow2'
SOURCE_SHA = '3a6b44a4db1299ec7bef95e2921b83fdaa17dc5b74449b588e66fc81d63fae59'
NETWORK = 'e4aa7897-db51-45de-a0dc-a13554eba163'
NS = 'urn:virmill:guest-tools-fixture:v1'
TERMINAL = {'succeeded', 'failed', 'partial', 'canceled', 'recovery-required'}
ACKS = {'guest-execution', 'guest-host-key-binding', 'guest-admin-package-install'}


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def bootstrap(mac):
    require(re.fullmatch(r'52:54:00:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}', mac), 'fixed generated MAC required')
    return '''#!/bin/sh
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
. /etc/os-release
test "$ID" = fedora && test "$VERSION_ID" = 44
for program in /usr/sbin/sshd /usr/bin/sudo /usr/bin/systemctl /usr/bin/nmcli; do test -x "$program"; done
! id virmillprobe >/dev/null 2>&1
useradd --create-home --shell /bin/bash virmillprobe
# Unlock the account with an impossible crypt hash; all password login is disabled.
usermod --password '*' virmillprobe
install -d -m 0700 -o virmillprobe -g virmillprobe /home/virmillprobe/.ssh
install -m 0600 -o virmillprobe -g virmillprobe /var/tmp/virmill-client.pub /home/virmillprobe/.ssh/authorized_keys
printf 'virmillprobe ALL=(ALL) NOPASSWD: ALL\\n' > /etc/sudoers.d/virmill-probe
chmod 0440 /etc/sudoers.d/virmill-probe
visudo -cf /etc/sudoers.d/virmill-probe
rm -f /etc/ssh/ssh_host_*key /etc/ssh/ssh_host_*key.pub
install -m 0600 /var/tmp/virmill-host /etc/ssh/ssh_host_ed25519_key
install -m 0644 /var/tmp/virmill-host.pub /etc/ssh/ssh_host_ed25519_key.pub
cat > /etc/ssh/sshd_config <<'EOF'
Port 22
HostKey /etc/ssh/ssh_host_ed25519_key
PermitRootLogin no
PasswordAuthentication no
KbdInteractiveAuthentication no
PubkeyAuthentication yes
AuthenticationMethods publickey
UsePAM yes
AllowUsers virmillprobe
Subsystem sftp internal-sftp
EOF
mkdir -p /run/sshd
sshd -t
mkdir -p /etc/cloud
touch /etc/cloud/cloud-init.disabled
systemctl disable cloud-init-local.service cloud-init.service cloud-config.service cloud-final.service || true
# These files belong only to the new offline work copy.
find /etc/NetworkManager/system-connections -maxdepth 1 -type f -delete
cat > /etc/NetworkManager/system-connections/virmill-probe.nmconnection <<'EOF'
[connection]
id=virmill-probe
type=ethernet
autoconnect=true
[ethernet]
mac-address=MAC_VALUE
[ipv4]
method=auto
[ipv6]
method=disabled
EOF
chmod 0600 /etc/NetworkManager/system-connections/virmill-probe.nmconnection
rpm -q qemu-guest-agent
rpm -e qemu-guest-agent
if rpm -q qemu-guest-agent; then exit 40; fi
systemctl enable NetworkManager.service sshd.service
touch /.autorelabel
rm -f /var/tmp/virmill-client.pub /var/tmp/virmill-host /var/tmp/virmill-host.pub
'''.replace('MAC_VALUE', mac)


def xml_for(identity, mac):
    require(str(uuid.UUID(identity)) == identity and uuid.UUID(identity).int, 'canonical fixture UUID required')
    require(re.fullmatch(r'52:54:00:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}', mac), 'fixture MAC required')
    root = ET.Element('domain', {'type': 'kvm'})
    ET.SubElement(root, 'name').text = 'virmill-tools-' + identity[:8]
    ET.SubElement(root, 'uuid').text = identity
    meta = ET.SubElement(root, 'metadata')
    ET.SubElement(meta, '{' + NS + '}fixture', {'id': identity})
    ET.SubElement(root, 'memory', {'unit': 'MiB'}).text = '1024'
    ET.SubElement(root, 'vcpu').text = '2'
    osnode = ET.SubElement(root, 'os')
    ET.SubElement(osnode, 'type', {'arch': 'x86_64', 'machine': 'pc-i440fx-10.2'}).text = 'hvm'
    ET.SubElement(osnode, 'boot', {'dev': 'hd'})
    features = ET.SubElement(root, 'features')
    ET.SubElement(features, 'acpi')
    ET.SubElement(features, 'apic')
    devices = ET.SubElement(root, 'devices')
    disk = ET.SubElement(devices, 'disk', {'type': 'file', 'device': 'disk'})
    ET.SubElement(disk, 'driver', {'name': 'qemu', 'type': 'qcow2'})
    ET.SubElement(disk, 'source', {'file': '/var/lib/libvirt/images/virmill-tools-' + identity + '/fedora.qcow2'})
    ET.SubElement(disk, 'target', {'dev': 'vda', 'bus': 'virtio'})
    nic = ET.SubElement(devices, 'interface', {'type': 'network'})
    ET.SubElement(nic, 'mac', {'address': mac})
    ET.SubElement(nic, 'source', {'network': 'default'})
    ET.SubElement(nic, 'model', {'type': 'virtio'})
    ET.SubElement(devices, 'controller', {'type': 'virtio-serial', 'index': '0', 'model': 'virtio'})
    channel = ET.SubElement(devices, 'channel', {'type': 'unix'})
    ET.SubElement(channel, 'target', {'type': 'virtio', 'name': 'org.qemu.guest_agent.0'})
    ET.SubElement(channel, 'address', {'type': 'virtio-serial', 'controller': '0', 'bus': '0', 'port': '1'})
    serial = ET.SubElement(devices, 'serial', {'type': 'pty'})
    ET.SubElement(serial, 'target', {'port': '0'})
    ET.SubElement(devices, 'memballoon', {'model': 'none'})
    return ET.tostring(root, encoding='unicode')


def owned(raw, identity, mac):
    tree = ET.fromstring(raw)
    marker = tree.find('metadata/{' + NS + '}fixture')
    require(tree.findtext('uuid') == identity and tree.findtext('name') == 'virmill-tools-' + identity[:8]
            and marker is not None and marker.get('id') == identity, 'ownership marker differs')
    expected = ET.fromstring(xml_for(identity, mac))
    for selector in ('devices/disk/source', 'devices/interface/mac', 'devices/interface/source'):
        require(tree.find(selector).attrib == expected.find(selector).attrib, 'fixture path/network changed')
    require(len(tree.findall('devices/disk')) == len(tree.findall('devices/interface')) == 1
            and not tree.findall('devices/hostdev') and not tree.findall('devices/filesystem')
            and tree.find('os/loader') is None, 'fixture acquired unreviewed devices')


def network_live_configuration(raw):
    """Retain live configuration except libvirt's root attachment counter.

    A guest attachment changes <network connections='N'>. This is not a
    definition edit. No other attribute, node, address, route, or policy is
    omitted from the comparison.
    """
    tree = ET.fromstring(raw)
    require(tree.tag == 'network', 'unexpected native network XML root')
    count = tree.attrib.pop('connections', None)
    require(count is None or re.fullmatch(r'[0-9]+', count), 'invalid network attachment counter')
    return ET.tostring(tree, encoding='unicode')


def network_snapshot(virsh):
    names = virsh('net-list', '--all', '--uuid').stdout.decode().split()
    result = {}
    for name in names:
        require(str(uuid.UUID(name)) == name and name not in result, 'network UUID duplicate or malformed')
        # Inactive XML binds persistent configuration; live XML separately binds
        # runtime configuration. net-info is retained in full: none of its
        # observed fields has been established as a changing attachment count.
        result[name] = {
            'xml': virsh('net-dumpxml', name, '--inactive').stdout.decode(),
            'liveXML': network_live_configuration(virsh('net-dumpxml', name).stdout),
            'info': virsh('net-info', name).stdout.decode(),
        }
    return result


def inspect_owned_domain(virsh, identity, mac):
    # Ownership belongs to the persistent fixture definition. Libvirt enriches
    # running NIC sources with bridge/port information, so do not compare those
    # runtime attributes with the original persistent source dictionary.
    owned(virsh('dumpxml', identity, '--inactive').stdout, identity, mac)
    live = ET.fromstring(virsh('dumpxml', identity).stdout)
    marker = live.find('metadata/{' + NS + '}fixture')
    require(live.findtext('uuid') == identity and live.findtext('name') == 'virmill-tools-' + identity[:8]
            and marker is not None and marker.get('id') == identity, 'runtime fixture identity differs')


def validate_plan(plan, identity, label, address=None):
    require(plan['connectionID'] == URI and plan['actorUID'] == 1000, 'plan host/actor differs')
    resource = 'libvirt|' + URI + '|vm|' + identity
    require(resource in plan['resourceIDs'], 'plan VM differs')
    require(re.fullmatch('[0-9a-f]{64}', plan['planDigest']), 'plan digest differs')
    if label in ('install', 'repeat'):
        review = plan['review']
        require(plan['operation'] == 'guest.recipe.run' and set(plan['acknowledgements']) == ACKS
                and review['resource']['resourceUUID'] == identity and review['address'] == address
                and review['user'] == 'virmillprobe' and review['port'] == 22
                and review['recipe']['metadata']['name'] == 'virmill-guest-tools-fedora'
                and review['reboot'] == 'never', 'guest recipe review differs')
    else:
        require(plan['operation'] == 'vm.' + label and plan['resourceIDs'] == [resource]
                and plan['acknowledgements'] == ['host-mutation'], 'lifecycle review differs')


def execute(stage):
    require(authorized_test_host() and os.getuid() == os.geteuid() == 1000,
            'wrong authorized host or actor')
    stage = canonical_path(str(stage.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged directory required')
    os.umask(0o077)
    manifest = strict_json((stage / 'binaries.json').read_bytes())
    require(all(sha('/usr/bin/' + n) == manifest[n] for n in ('virmill', 'virmilld')), 'installed manifest mismatch')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW)
    out = stage / 'guest-tools-fedora'
    out.mkdir(mode=0o700)
    r = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, fd)
    report = {'status': 'failed', 'scope': 'Fedora44 CLI tools install and repeat; TUI installation form access',
              'acceptanceSupport': ['GUEST-03', 'UX-01'], 'binarySHA256': manifest['virmill']}
    logs, allowed_jobs = [], set()
    identity = str(uuid.uuid4())
    mac = '52:54:00:' + ':'.join(f'{b:02x}' for b in os.urandom(3))
    image_dir = Path('/var/lib/libvirt/images/virmill-tools-' + identity)
    before = jobs_before = media_before = networks_before = None
    defined = False
    def native(*argv, timeout=30, check=True, input=None):
        p = subprocess.run(argv, input=input, capture_output=True, timeout=timeout,
                           env=dict(os.environ, LC_ALL='C'))
        require(len(p.stdout) + len(p.stderr) < 2 << 20, 'native output bound')
        logs.append({'argv': list(argv), 'exitCode': p.returncode,
                     'stdout': p.stdout.decode(errors='replace'), 'stderr': p.stderr.decode(errors='replace')})
        r.save('native-commands.json', logs)
        require(not check or p.returncode == 0, 'native command failed; inspect native-commands.json')
        return p
    def virsh(*argv, **kwargs):
        return native('/usr/bin/virsh', '--connect', URI, *argv, **kwargs)
    def networks():
        return network_snapshot(virsh)
    def apply(plan, label, address=None):
        validate_plan(plan, identity, label, address)
        r.save(label + '-plan.json', plan)
        args = ['plan', 'apply', plan['planID'], '--digest', plan['planDigest'], '--detach',
                '--idempotency-key', identity + '-' + label]
        for ack in plan['acknowledgements']: args += ['--ack', ack]
        job = r.cli(*args)
        allowed_jobs.add(job['operationID'])
        r.save(label + '-accepted.json', job)
        deadline = time.monotonic() + (380 if label in ('install', 'repeat') else 120)
        while job['state'] not in TERMINAL and time.monotonic() < deadline:
            time.sleep(1)
            job = r.cli('operation', 'show', job['operationID'])
        r.save(label + '-job.json', job)
        require(job['state'] == 'succeeded', label + ' incomplete; do not replay')
        return job
    try:
        before = inventory(r.cli('vm', 'list'), URI)
        require(before[SOURCE_VM]['state'] == 'stopped', 'source VM is not stopped')
        require(identity not in before, 'fixture UUID collision')
        for vm in r.cli('vm', 'list'):
            require(mac not in vm.get('persistentXML', '') + vm.get('liveXML', ''), 'fixture MAC collision')
        jobs_before = r.cli('operation', 'list')
        require(all(j['state'] in TERMINAL for j in jobs_before), 'another operation is active')
        media_before = media_listing(Path.home() / 'images')
        networks_before = networks()
        require(NETWORK in networks_before, 'approved default network missing')
        net = ET.fromstring(networks_before[NETWORK]['xml'])
        require(net.findtext('name') == 'default' and net.find('forward').get('mode') == 'nat'
                and net.find('ip').get('address') == '192.168.122.1', 'default network changed')
        require(re.search(r'^Active:\s+yes$', networks_before[NETWORK]['info'], re.M), 'default network is inactive')
        require(COPY == canonical_path(str(COPY)) and COPY.stat().st_uid == 1000
                and stat.S_ISREG(COPY.lstat().st_mode) and sha(COPY) == SOURCE_SHA and sha(SOURCE) == SOURCE_SHA,
                'reviewed source or private copy differs')
        require(shutil.disk_usage(out).free > 8 << 30, 'need 8 GiB private fixture workspace')
        require(shutil.disk_usage(image_dir.parent).free > 8 << 30, 'need 8 GiB fixture image space')
        r.save('before-vms.json', before)
        r.save('before-jobs.json', jobs_before)
        report.update({'vmID': identity, 'name': 'virmill-tools-' + identity[:8], 'mac': mac,
                       'installedDisk': str(image_dir / 'fedora.qcow2'), 'sourceSHA256': SOURCE_SHA})
        report['toolVersions'] = {tool: native('/usr/bin/' + tool, '--version').stdout.decode().strip()
                                  for tool in ('virt-customize', 'guestfish', 'qemu-img')}
        work = out / 'fedora-work.qcow2'
        with COPY.open('rb') as src, work.open('xb') as dst: shutil.copyfileobj(src, dst)
        require(sha(work) == SOURCE_SHA, 'work copy hash differs')
        for key in ('client', 'host'):
            native('/usr/bin/ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-C', 'virmill-fixture', '-f', str(out / key))
        script = out / 'bootstrap.sh'
        script.write_text(bootstrap(mac)); script.chmod(0o600)
        native('/usr/bin/virt-customize', '--format=qcow2', '-a', str(work), '--no-network',
               '--upload', str(out / 'client.pub') + ':/var/tmp/virmill-client.pub',
               '--upload', str(out / 'host') + ':/var/tmp/virmill-host',
               '--upload', str(out / 'host.pub') + ':/var/tmp/virmill-host.pub', '--run', str(script), timeout=300)
        report['customizedSHA256'] = sha(work)
        native('/usr/bin/sudo', '-n', '/usr/bin/mkdir', '--mode=0750', str(image_dir))
        native('/usr/bin/sudo', '-n', '/usr/bin/chown', 'root:qemu', str(image_dir))
        native('/usr/bin/sudo', '-n', '/usr/bin/install', '--owner=qemu', '--group=qemu', '--mode=0600', str(work), str(image_dir / 'fedora.qcow2'))
        native('/usr/bin/sudo', '-n', '/usr/sbin/restorecon', '-RF', str(image_dir))
        definition = out / 'domain.xml'; definition.write_text(xml_for(identity, mac))
        virsh('define', str(definition)); defined = True
        inspect_owned_domain(virsh, identity, mac)
        apply(r.cli('vm', 'start', identity), 'start')
        deadline = time.monotonic() + 240
        address = None
        while time.monotonic() < deadline:
            leases = virsh('net-dhcp-leases', NETWORK, '--mac', mac).stdout.decode()
            found = re.findall(r'\b192\.168\.122\.\d+/24\b', leases)
            if len(found) == 1:
                address = found[0].split('/')[0]; break
            time.sleep(2)
        require(address is not None and ipaddress.ip_address(address) in ipaddress.ip_network('192.168.122.0/24'), 'no unique own DHCP lease')
        host_public = (out / 'host.pub').read_text().split()
        known = out / 'known_hosts'; known.write_text(address + ' ' + ' '.join(host_public[:2]) + '\n')
        report['address'] = address
        def ssh(script, check=True):
            return native('/usr/bin/ssh', '-F', '/dev/null', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes',
                          '-o', 'IdentitiesOnly=yes', '-o', 'PasswordAuthentication=no', '-o', 'ForwardAgent=no',
                          '-o', 'ConnectTimeout=5', '-o', 'ConnectionAttempts=1', '-o', 'GlobalKnownHostsFile=/dev/null',
                          '-o', 'UserKnownHostsFile=' + str(known), '-i', str(out / 'client'),
                          'virmillprobe@' + address, '/bin/sh', '-s', timeout=15, check=check, input=script.encode())
        booted = False
        while time.monotonic() < deadline:
            probe = ssh('set -eu\ntest "$(id -u)" -ne 0\nsudo -n true\n', check=False)
            if probe.returncode == 0: booted = True; break
            time.sleep(2)
        require(booted, 'guest SSH did not become ready within bounded boot attempt')
        ssh('set -eu\n. /etc/os-release\ntest "$ID" = fedora && test "$VERSION_ID" = 44\nif rpm -q qemu-guest-agent; then exit 40; fi\ntest -c /dev/virtio-ports/org.qemu.guest_agent.0\n')
        report['agentInitiallyAbsent'] = True
        tools = {'profile': 'fedora', 'desktop': False, 'address': address, 'port': 22, 'user': 'virmillprobe',
                 'identityFile': str(out / 'client'), 'knownHostsFile': str(known)}
        for label in ('install', 'repeat'):
            job = apply(r.cli('guest', 'tools', 'install', identity, '--input', json.dumps(tools)), label, address)
            result = r.cli('guest', 'recipe', 'result', job['operationID']); r.save(label + '-result.json', result)
            require(result['complete'] and result['state'] == 'succeeded', 'guest recipe result incomplete')
            stages = {s['stage']: s for s in result['stages']}
            require(stages['check']['receipt']['outcome'] == ('needs-apply' if label == 'install' else 'already-configured'), 'unexpected tools check outcome')
            require(stages['apply']['intent']['runPlanned'] == (label == 'install'), 'repeat did not skip package mutation')
        report['guestPackages'] = ssh('set -eu\nrpm -q qemu-guest-agent systemd openssh-server sudo NetworkManager\nsystemctl is-active qemu-guest-agent.service\n').stdout.decode()
        readiness = r.cli('vm', 'readiness', 'show', identity); r.save('readiness.json', readiness)
        require(readiness['agentResponsive'] and readiness['state'] == 'responsive', 'native guest-agent ping failed')
        report['nativeGuestPing'] = True
        # A real 80x24 frame proves access to the guided form; the actual mutation
        # above is CLI evidence, not a claim that keyboard submission was tested.
        terminal = Terminal(r, 'guest-tools-form', 80, 24)
        try:
            def wait(label, predicate, key=None):
                return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
            wait('Overview', lambda s: 'Virtual machines' in s)
            wait('VM table', lambda s: 'NAME' in s and 'STATE' in s, b'2')
            wait('Filter focus', lambda s: 'Enter Keep filter' in s, b'/')
            wait('Owned VM selected', lambda s: report['name'] in s and re.search(r'row 1 of 1\b', s), report['name'].encode())
            wait('Filter kept', lambda s: 'Enter Keep filter' not in s, b'\r')
            wait('VM details', lambda s: 'VM details' in s and report['name'] in s, b'\r')
            wait('Guest tools form', lambda s: 'Install guest tools' in s and 'Preview installation' in s, b'g')
            report['tuiForm80x24'] = True
        finally: terminal.close()
        report['status'] = 'passed'
    except BaseException as error:
        report['error'] = repr(error)
    finally:
        try:
            if defined:
                inspect_owned_domain(virsh, identity, mac)
                current_jobs = r.cli('operation', 'list')
                require(all(j['state'] == 'succeeded' for j in current_jobs if j['operationID'] in allowed_jobs),
                        'fixture job failed or remains uncertain; retain VM for explicit review')
                vm = r.cli('vm', 'show', identity)
                if vm['state'] == 'running':
                    apply(r.cli('vm', 'stop', identity), 'stop')
                    shutdown_deadline = time.monotonic() + 90
                    while time.monotonic() < shutdown_deadline:
                        vm = r.cli('vm', 'show', identity)
                        if vm['state'] == 'stopped': break
                        time.sleep(1)
                require(vm['state'] == 'stopped', 'fixture has not stopped; retained')
                report['ownedFixtureStopped'] = True
        except BaseException as error:
            report['status'] = 'failed'; report['cleanupError'] = repr(error)
        try:
            if before is not None:
                after = inventory(r.cli('vm', 'list'), URI); after.pop(identity, None)
                require(after == before, 'preexisting guest inventory changed')
                report['existingGuestsPreserved'] = True
            if jobs_before is not None:
                after_jobs = r.cli('operation', 'list')
                require([j for j in after_jobs if j['operationID'] not in allowed_jobs] == jobs_before, 'unrelated jobs changed')
                report['existingJobsPreserved'] = True
            if networks_before is not None: require(networks() == networks_before, 'existing network configuration or state changed')
            if media_before is not None: require(media_listing(Path.home() / 'images') == media_before, 'source media metadata changed')
            require(sha(SOURCE) == sha(COPY) == SOURCE_SHA, 'source or inspected copy changed')
            report['sourcesAndNetworksPreserved'] = True
        except BaseException as error:
            report['status'] = 'failed'; report['preservationOrCleanupError'] = repr(error)
        r.save('report.json', report)
        os.close(fd)
        print(json.dumps(report, sort_keys=True))
    return 0 if report['status'] == 'passed' else 1


class Tests(unittest.TestCase):
    def test_offline_bootstrap(self):
        s = bootstrap('52:54:00:12:34:56')
        self.assertIn('rpm -e qemu-guest-agent', s)
        self.assertIn('test "$ID" = fedora && test "$VERSION_ID" = 44', s)
        self.assertIn('touch /.autorelabel', s)
        self.assertNotIn('dnf', s)
        with self.assertRaises(RuntimeError): bootstrap('52:54:00:12:34:56\nanything')
    def test_owned_definition(self):
        identity = '12345678-1234-4234-8234-123456789abc'; mac = '52:54:00:12:34:56'
        raw = xml_for(identity, mac); owned(raw, identity, mac)
        with self.assertRaises(RuntimeError): owned(raw.replace('fedora.qcow2', 'other.qcow2'), identity, mac)
        self.assertNotIn('firmware=', raw)
        self.assertNotIn('<source mode=', raw)


    def test_persistent_ownership_ignores_runtime_nic_enrichment(self):
        identity = '12345678-1234-4234-8234-123456789abc'; mac = '52:54:00:12:34:56'
        persistent = xml_for(identity, mac)
        live = ET.fromstring(persistent)
        live.find('devices/interface/source').set('bridge', 'virbr0')
        calls = []
        def virsh(*args):
            calls.append(args)
            raw = persistent if '--inactive' in args else ET.tostring(live, encoding='unicode')
            return argparse.Namespace(stdout=raw.encode())
        inspect_owned_domain(virsh, identity, mac)
        self.assertEqual(calls[0], ('dumpxml', identity, '--inactive'))
        live.find('uuid').text = '22345678-1234-4234-8234-123456789abc'
        with self.assertRaises(RuntimeError): inspect_owned_domain(virsh, identity, mac)
        # A persistent network edit still refuses ownership.
        persistent = persistent.replace('network="default"', 'network="other"')
        with self.assertRaises(RuntimeError): inspect_owned_domain(virsh, identity, mac)

    def test_network_counter_is_the_only_ignored_runtime_attribute(self):
        raw = '<network connections="1"><name>default</name><bridge name="virbr0"/><forward mode="nat"/><ip address="192.168.122.1"><dhcp><range start="192.168.122.2" end="192.168.122.254"/></dhcp></ip></network>'
        original = network_live_configuration(raw)
        self.assertEqual(original, network_live_configuration(raw.replace('connections="1"', 'connections="2"')))
        for before, after in [('virbr0', 'virbr1'), ('mode="nat"', 'mode="route"'),
                              ('192.168.122.1', '192.168.123.1'), ('192.168.122.254', '192.168.122.253')]:
            self.assertNotEqual(original, network_live_configuration(raw.replace(before, after)))
        self.assertNotEqual(original, network_live_configuration(raw.replace('<network ', '<network unknown="changed" ')))
        with self.assertRaises(RuntimeError): network_live_configuration(raw.replace('connections="1"', 'connections="bad"'))

    def test_network_snapshot_preserves_persistent_xml_and_state(self):
        identity = '12345678-1234-4234-8234-123456789abc'
        calls = []
        state = {'xml': '<network><name>default</name></network>', 'active': 'yes', 'connections': '1'}
        def virsh(*args):
            calls.append(args)
            if args[0] == 'net-list': raw = identity + '\n'
            elif args[0] == 'net-info': raw = 'Active: yes\nAutostart: no\nBridge: virbr0\n'.replace('Active: yes', 'Active: ' + state['active'])
            elif '--inactive' in args: raw = state['xml']
            else: raw = '<network connections="' + state['connections'] + '"><name>default</name></network>'
            return argparse.Namespace(stdout=raw.encode())
        before = network_snapshot(virsh)
        self.assertIn(('net-dumpxml', identity, '--inactive'), calls)
        state['connections'] = '2'
        self.assertEqual(before, network_snapshot(virsh))
        state['xml'] = state['xml'].replace('default', 'other')
        self.assertNotEqual(before, network_snapshot(virsh))
        state['xml'] = before[identity]['xml']; state['active'] = 'no'
        self.assertNotEqual(before, network_snapshot(virsh))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path)
    parser.add_argument('--execute-disposable', action='store_true')
    parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        require(not args.execute_disposable and args.root is None, 'self-test cannot select native execution')
        unittest.main(argv=[sys.argv[0]])
    else:
        require(args.execute_disposable and args.root is not None, 'explicit native execution/root required')
        sys.exit(execute(args.root))
