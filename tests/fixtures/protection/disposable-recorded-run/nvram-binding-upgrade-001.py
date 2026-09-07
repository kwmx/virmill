"""Single-use parent-operated upgrade on the owner-authorized disposable VM.

Only the two verified development RPMs and this run's private coordinator change.
No guest, network, volume, helper policy or auxiliary-state mutation is requested.
"""
import hashlib
import json
import os
import pathlib
import re
import sqlite3
import subprocess
import sys
import time

ROOT = pathlib.Path.home() / 'virmill-tests/run-65930c6-20260907'
OLD_REVISION = '4c5817674e8c036081715237ad1e768869f377c5'
assert len(sys.argv) == 3
revision, manifest_sha = sys.argv[1:]
assert re.fullmatch('[a-f0-9]{40}', revision) and revision != OLD_REVISION
assert re.fullmatch('[a-f0-9]{64}', manifest_sha)
report_path = ROOT / 'nvram-binding-upgrade.json'
assert not report_path.exists(), 'Single-use recipe: prior report exists'
bundle = ROOT / 'packages' / revision[:7]
raw = (bundle / 'deployment.json').read_bytes()
assert hashlib.sha256(raw).hexdigest() == manifest_sha
manifest = json.loads(raw)
assert manifest['revision'] == revision
env = {**os.environ, **json.loads((ROOT / 'environment.json').read_text())}


def run(args, *, private_env=False, timeout=60):
    result = subprocess.run(args, env=env if private_env else None,
                            capture_output=True, timeout=timeout)
    if result.returncode:
        print(result.stdout.decode(errors='replace'), flush=True)
        print(result.stderr.decode(errors='replace'), flush=True)
    result.check_returncode()
    return result.stdout


def digest(path):
    h = hashlib.sha256()
    with pathlib.Path(path).open('rb') as f:
        for block in iter(lambda: f.read(1024 * 1024), b''):
            h.update(block)
    return h.hexdigest()


def cli(*args):
    response = json.loads(run(['virmill', *args, '--connection', 'qemu:///system',
                               '--output', 'json', '--non-interactive'], private_env=True))
    assert not response.get('error'), response
    return response['data']


def journal():
    with sqlite3.connect((ROOT / 'state/virmill/journal.db').as_uri() + '?mode=ro', uri=True) as db:
        return {table: list(db.execute('SELECT * FROM ' + table + ' ORDER BY rowid'))
                for table in ('plans', 'jobs', 'metadata', 'locks', 'events', 'dedup')}


def guests():
    command = ['virsh', '--readonly', '-c', 'qemu:///system']
    ids = run([*command, 'list', '--all', '--uuid']).decode().split()
    observed = {}
    for vm in ids:
        assert run([*command, 'domstate', vm]).decode().strip() == 'shut off'
        observed[vm] = hashlib.sha256(run([*command, 'dumpxml', '--inactive', vm])).hexdigest()
    return observed


def helper_inactive():
    for unit in ('virmill-host-helper.service', 'virmill-host-helper.socket'):
        status = subprocess.run(['systemctl', 'is-active', '--quiet', unit], timeout=15)
        assert status.returncode == 3, (unit, status.returncode)


packages = ['virmill-0.0.0-0.dev.x86_64.rpm', 'virmill-host-helper-0.0.0-0.dev.x86_64.rpm']
for name in packages:
    assert digest(bundle / name) == manifest['artifacts']['dist/' + name]
run(['sudo', '-n', 'true'])
helper_inactive()
assert cli('version')['revision'] == OLD_REVISION
run(['rpm', '-V', 'virmill', 'virmill-host-helper'])
before = journal()
assert before['locks'] == []
# Preserve every prior body, including legacy partial/retained dispositions.
with sqlite3.connect((ROOT / 'state/virmill/journal.db').as_uri() + '?mode=ro', uri=True) as db:
    jobs = dict(db.execute('SELECT id,body FROM jobs'))
assert len(jobs) == 31
assert all(json.loads(body)['state'] in ('succeeded', 'failed', 'canceled', 'partial') for body in jobs.values())
prior_upgrade = json.loads((ROOT / 'cold-native-upgrade.json').read_text())
before_guests = guests()
assert before_guests == prior_upgrade['allSixStoppedVMXMLSHA256']
probe = 'd4c95f21-28bc-428d-9e5f-ceda025d279e'
probe_disk = '/var/lib/libvirt/images/virmill-cold-probe-v1/virmill-' + probe + '-disk-000.qcow2'
probe_sha = 'f0eafe1814a7a137def96128e8f5bc15838669c868a25aa808f8d084c4382e51'
assert run(['sudo', '-n', 'sha256sum', '--', probe_disk]).decode().split()[0] == probe_sha
old_unit = 'virmill-test-' + OLD_REVISION[:7] + '.service'
assert run(['systemctl', '--user', 'show', old_unit, '-p', 'WorkingDirectory', '--value']).decode().strip() == str(ROOT)

# All preflight checks precede the only authorized mutations in this recipe.
run(['systemctl', '--user', 'stop', old_unit])
run(['sudo', '-n', 'rpm', '-Uvh', '--replacepkgs', *[str(bundle / name) for name in packages]], timeout=120)
run(['rpm', '-V', 'virmill', 'virmill-host-helper'])
executables = {'virmill': '/usr/bin/virmill', 'virmilld': '/usr/bin/virmilld',
               'virmill-host-helper': '/usr/libexec/virmill-host-helper'}
for name, path in executables.items():
    assert digest(path) == manifest['artifacts']['build/bin/' + name]
unit = 'virmill-test-' + revision[:7] + '.service'
args = ['systemd-run', '--user', '--unit=' + unit, '--property=RuntimeMaxSec=7200',
        '--property=Restart=no', '--property=WorkingDirectory=' + str(ROOT)]
for key in ('XDG_STATE_HOME', 'XDG_RUNTIME_DIR', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_CONFIG_HOME'):
    args.append('--setenv=' + key + '=' + env[key])
run([*args, '/usr/bin/virmilld'])
for _ in range(100):
    if pathlib.Path(env['XDG_RUNTIME_DIR'], 'virmill/control.sock').exists():
        break
    time.sleep(.05)
run(['systemctl', '--user', 'is-active', '--quiet', unit])
pid = int(run(['systemctl', '--user', 'show', unit, '-p', 'MainPID', '--value']))
assert pid > 0
assert digest('/proc/' + str(pid) + '/exe') == manifest['artifacts']['build/bin/virmilld']
assert cli('version')['revision'] == revision
legacy = cli('vm', 'creation', 'result', 'ef65c956-b26b-4fce-9d04-7800768570ea')
assert legacy['complete'] is True
assert legacy['nvramDeclarationStatus'] == 'legacy-unbound'
assert legacy['nvramDeclarationBound'] is False and legacy['nvramInitializationVerified'] is False
assert cli('vm', 'recovery', 'inspect', probe)['layout']['tpm']['sourcePath'] == ''
assert journal() == before
assert guests() == before_guests
assert run(['sudo', '-n', 'sha256sum', '--', probe_disk]).decode().split()[0] == probe_sha
helper_inactive()
report = {'upgrade': 'passed', 'revision': revision, 'sourceDigest': manifest['sourceDigest'],
          'deploymentSHA256': manifest_sha, 'newUnit': unit,
          'all31PriorJobsPlansMetadataEventsLocksUnchanged': True,
          'allSixStoppedVMXMLSHA256': before_guests, 'probeDiskSHA256': probe_sha,
          'legacyUEFIResult': {'complete': legacy['complete'], 'nvramDeclarationStatus': legacy['nvramDeclarationStatus'],
                              'nvramDeclarationBound': False, 'nvramInitializationVerified': False},
          'noGuestStartOrDefinitionOrReconciliation': True, 'completeCaptureVerified': False,
          'independentRecoveryVerified': False,
          'SELinux': run(['getenforce']).decode().strip(),
          'nativePackages': run(['rpm', '-q', 'libvirt-daemon-kvm', 'qemu-kvm', 'edk2-ovmf', 'swtpm', 'restic']).decode().splitlines()}
with report_path.open('x') as f:
    json.dump(report, f, indent=2)
print(json.dumps(report), flush=True)
