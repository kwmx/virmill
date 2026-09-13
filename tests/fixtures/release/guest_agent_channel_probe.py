#!/usr/bin/env python3
"""Parent-operated powered-off guest-agent channel fixture on the disposable VM.

Creates one 1 MiB raw source, one prepared disk, and one new powered-off VM in an
explicit existing test pool. No guest starts, installed guest agents, network
changes, unrelated guest edits, or fabricated journal/crash state. Retains every
artifact on success/failure. Run --self-test without service or host access.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import stat
import sys
import time
import unittest
import xml.etree.ElementTree as ET

from tui_workspace_probe import (Runner, canonical_path, generation, inventory,
                                 media_listing, require, canonical_uuid)


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


AGENT = 'org.qemu.guest_agent.0'
TERMINAL = {'succeeded', 'failed', 'partial', 'canceled', 'recovery-required', 'interrupted'}


def channel_proof(vm):
    require(vm.get('state') in ('stopped', 'shut off'), 'fixture VM must remain powered off')
    root = ET.fromstring(vm['persistentXML'])
    channels = root.findall('devices/channel')
    require(len(channels) == 1, 'expected exactly one guest-agent channel')
    channel = channels[0]
    require(channel.attrib == {'type': 'unix'}, 'channel transport differs')
    require(len(channel.findall('source')) == 0, 'persistent channel must retain libvirt automatic socket policy')
    require(len(channel.findall('target')) == 1 and
            channel.find('target').attrib == {'type': 'virtio', 'name': AGENT}, 'channel target differs')
    require(len(channel.findall('address')) == 1 and channel.find('address').attrib ==
            {'type': 'virtio-serial', 'controller': '0', 'bus': '0', 'port': '1'}, 'channel topology differs')
    for node in channel:
        require(node.tag in ('target', 'address', 'alias'), 'unexpected channel configuration')
        if node.tag == 'alias':
            require(node.attrib == {'name': 'channel0'}, 'channel alias differs')
    require(len(channel.findall('alias')) <= 1, 'duplicate channel aliases')
    controllers = root.findall("devices/controller[@type='virtio-serial']")
    require(len(controllers) == 1 and controllers[0].attrib ==
            {'type': 'virtio-serial', 'index': '0', 'model': 'virtio'}, 'controller differs')
    require(len(root.findall("devices/disk[@device='disk']")) == 1 and
            len(root.findall('devices/interface')) == 0, 'fixture disk/network count differs')
    require(root.findtext('vcpu') == '1' and root.findtext('memory') == '524288', 'bounded fixture resources differ')
    return {'target': AGENT, 'controller': 0, 'bus': 0, 'port': 1,
            'automaticSocket': True, 'poweredOff': True}


def apply_arguments(plan, key):
    canonical_uuid(plan['planID'])
    require(re.fullmatch('[0-9a-f]{64}', plan['planDigest']) is not None, 'invalid reviewed digest')
    require(isinstance(plan['acknowledgements'], list) and
            all(isinstance(x, str) and x for x in plan['acknowledgements']), 'invalid acknowledgements')
    args = ['plan', 'apply', plan['planID'], '--digest', plan['planDigest'],
            '--idempotency-key', key, '--detach']
    for ack in plan['acknowledgements']:
        args.extend(['--ack', ack])
    return args


def execute(args):
    require(args.execute_disposable, 'explicit disposable execution required')
    require(authorized_test_host() and os.getuid() == 1000,
            'wrong test host or actor')
    require(args.connection == 'qemu:///system' and args.binary == '/usr/bin/virmill',
            'installed binary and explicit local system connection required')
    require(args.pool == TEST_POOL, 'only the existing designated test storage pool is authorized')
    root = canonical_path(str(args.output))
    require(root.parent.resolve().is_relative_to(Path.home() / 'virmill-tests') and not root.exists(),
            'choose a new output directory under the authorized test root')
    require(re.fullmatch('[0-9a-f]{64}', args.binary_sha256 or '') is not None, 'pinned binary digest required')
    fd = os.open(args.binary, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        require(stat.S_ISREG(os.fstat(fd).st_mode), 'binary must be ordinary')
        with os.fdopen(os.dup(fd), 'rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == args.binary_sha256,
                    'installed binary differs from reviewed build')
        root.mkdir(mode=0o700)
        r = Runner(args, root, fd)
        report = {'status': 'failed', 'scope': 'native new powered-off VM definition and guest-agent channel only',
                  'acceptanceSupport': ['IMP-01', 'IMP-07', 'GUEST-03'], 'guestBootVerified': False,
                  'guestAgentInstalled': False, 'guestTransportVerified': False,
                  'crashRecoveryVerified': False, 'reconciliation': 'not-needed'}
        before = inventory(r.cli('vm', 'list'), args.connection)
        prior_jobs = r.cli('operation', 'list')
        media = Path.home() / 'images'
        owner_media = media_listing(media)
        fixture_source = None
        fixture_digest = None
        allowed_jobs = set()
        created_id = None
        try:
            choices = r.cli('vm', 'creation', 'options')
            r.save('hardware-options.json', choices)
            require(choices['maxVCPUs'] >= 1 and choices['hostMemoryMiB'] >= 512,
                    'bounded fixture resources unavailable')
            firmware = next((x['firmware'] for x in choices['firmware'] if x['firmware']['mode'] == 'bios'), None)
            require(firmware is not None, 'BIOS fixture needs an advertised BIOS choice; do not infer firmware paths')
            require('sata' in choices['diskBuses'] and 'none' in choices['graphics'], 'fixture device choices unavailable')
            cpu_mode = next((x for x in ('host-model', 'host-passthrough') if x in choices['cpuModes']), None)
            require(cpu_mode is not None, 'supported automatic CPU mode required')
            pools = r.cli('storage', 'pool', 'list')
            selected = [p for p in pools if p['name'] == args.pool and p['active'] and p['type'] == 'dir']
            require(len(selected) == 1, 'dedicated active directory pool unavailable or ambiguous')
            pool_id = canonical_uuid(selected[0]['key']['resourceUUID'])
            r.save('selected-pool.json', selected[0])
            source_dir = root / 'source'
            source_dir.mkdir(mode=0o700)
            fixture_source = source_dir / 'channel-fixture.raw'
            fixture_source.write_bytes(bytes(1 << 20))
            fixture_source.chmod(0o600)
            fixture_digest = hashlib.sha256(fixture_source.read_bytes()).hexdigest()
            marker = {'owner': 'virmill-guest-agent-channel-probe', 'binarySHA256': args.binary_sha256,
                      'sourceSHA256': fixture_digest, 'sourceBytes': 1 << 20}
            r.save('fixture-owner.json', marker)
            source_generation = generation(fixture_source.stat())
            prep_input = {'destination': str(root / 'prepared'), 'offlineSources': True,
                          'files': [{'path': fixture_source.name}],
                          'disks': [{'id': 'boot', 'path': fixture_source.name, 'format': 'raw',
                                     'maximumVirtualBytes': 1 << 20}]}
            prep_plan = r.cli('import', 'prepare-disks', str(source_dir), '--input', json.dumps(prep_input))
            r.save('preparation-plan.json', prep_plan)

            def apply(plan, label):
                argv = apply_arguments(plan, root.name + '-' + label)
                job = r.cli(*argv)
                allowed_jobs.add(job['operationID'])
                r.save(label + '-accepted.json', job)
                deadline = time.monotonic() + 90
                while job['state'] not in TERMINAL and time.monotonic() < deadline:
                    time.sleep(.3)
                    job = r.cli('operation', 'show', job['operationID'])
                r.save(label + '-job.json', job)
                if label == 'create' and job['state'] in ('recovery-required', 'interrupted'):
                    # Observe an actual uncertain result only. Never fabricate a
                    # crash or edit SQLite merely to exercise this branch.
                    report['reconciliation'] = 'attempted-actual-uncertain-operation'
                    job = r.cli('operation', 'reconcile', job['operationID'])
                    r.save('creation-reconciled.json', job)
                    report['reconciliation'] = 'native-observation' if job['state'] == 'succeeded' else 'unresolved'
                require(job['state'] == 'succeeded', label + ' did not succeed; retain all fixture resources')
                return job, argv

            prepared, _ = apply(prep_plan, 'prepare')
            report['preparationOperationID'] = prepared['operationID']
            machine = choices['machine']
            chipset = 'q35' if machine == 'q35' or machine.startswith('pc-q35-') else 'i440fx'
            require(chipset == 'q35' or machine == 'pc' or machine.startswith('pc-i440fx-'), 'unsupported fixture chipset')
            name = 'virmill-channel-' + hashlib.sha256(str(root).encode()).hexdigest()[:12]
            require(all(v['name'] != name for v in before.values()), 'fixture name already exists')
            hardware = {'name': name, 'poolID': pool_id, 'architecture': choices['architecture'],
                        'machine': machine, 'vcpus': 1, 'memoryMiB': 512, 'cpu': {'mode': cpu_mode},
                        'firmware': firmware, 'clock': 'utc', 'graphics': 'none', 'guestAgent': True,
                        'disks': [{'sourceID': 'boot', 'bus': 'sata', 'bootOrder': 1}], 'nics': [],
                        'devicePolicy': {'version': 1, 'chipset': chipset, 'pciPlacement': 'libvirt-auto',
                                         'usbController': 'none', 'memoryBalloon': 'none', 'watchdogAction': 'none',
                                         'input': 'ps2', 'audio': 'none', 'serial': 'isa-serial'}}
            plan = r.cli('vm', 'create', prepared['operationID'], '--input',
                         json.dumps({'identityMode': 'clone', 'hardware': hardware}))
            r.save('creation-input.json', hardware)
            r.save('creation-plan.json', plan)
            created, argv = apply(plan, 'create')
            report['creationOperationID'] = created['operationID']
            result = r.cli('vm', 'creation', 'result', created['operationID'])
            r.save('creation-result.json', result)
            require(result['complete'] and not result['guestBootVerified'] and
                    not result['setupVerified'] and not result['connectivityVerified'], 'creation stages are not truthful')
            created_id = canonical_uuid(result['receipt']['vmID'])
            require(created_id not in before, 'created guest must have a fresh identity')
            report['vmID'] = created_id
            vm = r.cli('vm', 'show', created_id)
            r.save('created-vm.json', vm)
            report['channelProof'] = channel_proof(vm)
            # Idempotent apply is supported for a succeeded job; it must not be
            # confused with crash recovery or an independent native observation.
            replay = r.cli(*argv)
            r.save('idempotent-apply.json', replay)
            require(replay['operationID'] == created['operationID'], 'same reviewed apply created another operation')
            again = r.cli('vm', 'creation', 'result', created['operationID'])
            require(again == result, 'stable completed receipt changed')
            report['idempotentApplyPreserved'] = True
            report['sourceFixturePreserved'] = (generation(fixture_source.stat()) == source_generation and
                                               hashlib.sha256(fixture_source.read_bytes()).hexdigest() == fixture_digest)
            require(report['sourceFixturePreserved'], 'generated source changed')
            report['status'] = 'passed'
        except BaseException as error:
            report['error'] = repr(error)
        finally:
            try:
                after = inventory(r.cli('vm', 'list'), args.connection)
                report['existingGuestsPreserved'] = all(after.get(k) == v for k, v in before.items())
                extras = set(after) - set(before)
                report['newGuestIDs'] = sorted(extras)
                require(len(extras) <= 1 and (created_id is None or extras == {created_id}),
                        'unexpected additional guest appeared')
                current = r.cli('operation', 'list')
                jobs_by_id = {j['operationID']: j for j in current}
                report['existingJobsPreserved'] = all(jobs_by_id.get(j['operationID']) == j for j in prior_jobs)
                require(set(jobs_by_id) - {j['operationID'] for j in prior_jobs} <= allowed_jobs,
                        'unexpected job appeared during exclusive fixture')
                report['ownerMediaDirectoryMetadataPreserved'] = owner_media == media_listing(media)
                require(all(report[k] for k in ('existingGuestsPreserved', 'existingJobsPreserved',
                                                'ownerMediaDirectoryMetadataPreserved')), 'preservation failed')
                if fixture_source is not None and fixture_digest is not None:
                    require(hashlib.sha256(fixture_source.read_bytes()).hexdigest() == fixture_digest,
                            'source fixture bytes changed')
            except BaseException as error:
                report.update(status='failed', preservationError=repr(error))
            r.save('report.json', report)
            print(json.dumps(report, sort_keys=True))
        return report['status'] != 'passed'
    finally:
        os.close(fd)


class ProbeTests(unittest.TestCase):
    def fixture(self):
        return {'state': 'stopped', 'persistentXML': '''<domain><vcpu>1</vcpu><memory>524288</memory><devices>
<disk device="disk"/><controller type="virtio-serial" index="0" model="virtio"/>
<channel type="unix"><target type="virtio" name="org.qemu.guest_agent.0"/>
<address type="virtio-serial" controller="0" bus="0" port="1"/></channel></devices></domain>'''}

    def test_native_readback_predicate(self):
        self.assertTrue(channel_proof(self.fixture())['automaticSocket'])
        for old, new in [('state-unused', 'ignored'), ('port="1"', 'port="2"'),
                         ('<channel type="unix">', '<channel type="unix"><source path="/tmp/socket"/>'),
                         ('</channel>', '<target type="virtio" name="other"/></channel>'),
                         ('</devices>', '<interface/></devices>')]:
            if old == 'state-unused':
                vm = self.fixture(); vm['state'] = 'running'
            else:
                vm = self.fixture(); vm['persistentXML'] = vm['persistentXML'].replace(old, new)
            with self.assertRaises(RuntimeError):
                channel_proof(vm)

    def test_explicit_review_arguments(self):
        plan = {'planID': '11111111-1111-4111-8111-111111111111', 'planDigest': 'a' * 64,
                'acknowledgements': ['write-import-artifacts', 'offline-source-files']}
        argv = apply_arguments(plan, 'fixture-create')
        self.assertEqual(argv.count('--ack'), 2)
        self.assertIn('--detach', argv)
        self.assertEqual(argv[2], plan['planID'])
        plan['planDigest'] = 'not-a-digest'
        with self.assertRaises(RuntimeError):
            apply_arguments(plan, 'fixture-create')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--self-test', action='store_true')
    parser.add_argument('--execute-disposable', action='store_true')
    parser.add_argument('--binary', default='/usr/bin/virmill')
    parser.add_argument('--binary-sha256')
    parser.add_argument('--connection', default='qemu:///system')
    parser.add_argument('--pool', default=TEST_POOL)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    if args.self_test:
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(ProbeTests)
        return not unittest.TextTestRunner(verbosity=2).run(suite).wasSuccessful()
    require(args.output is not None, 'new output directory required')
    return execute(args)


if __name__ == '__main__':
    sys.exit(main())
