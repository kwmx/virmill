#!/usr/bin/env python3
"""Authorized disposable host: encrypted backup, fresh-profile recovery and boot.

Every repository, credential, profile and restored VM is new. Original source
captures, guest definitions and media are retained. No existing guest is started.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import time
import uuid


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--execute-disposable', action='store_true', required=True)
    p.add_argument('--root', type=Path, required=True)
    p.add_argument('--source-data', type=Path, required=True)
    p.add_argument('--capture-id', required=True)
    p.add_argument('--pool', required=True)
    p.add_argument('--recover-from', type=Path, help='Retained successful backup proof; skip init, backup and check')
    a = p.parse_args()
    assert socket.gethostname() in ('virmill-test', 'virmill-test.home') and os.getuid() == 1000
    root = a.root.resolve(strict=True)
    assert root.is_relative_to(Path.home()/'virmill-tests')
    assert a.source_data.resolve(strict=True).is_relative_to(Path.home()/'virmill-tests')
    assert str(uuid.UUID(a.capture_id)) == a.capture_id and str(uuid.UUID(a.pool)) == a.pool
    results = root/'results'; results.mkdir(mode=0o700)
    events = []; env = dict(os.environ); daemon = None; new_id = None; baseline = None
    report = {'status': 'failed', 'scope': 'encrypted two-disk BIOS capture backup, complete fresh-profile recovery, new disconnected VM and boot; no physical USB or firmware/TPM restoration claim'}
    source = a.source_data/'virmill/captures'/a.capture_id
    before = None

    def run(argv, check=True, timeout=600):
        r = subprocess.run(argv, env=env, capture_output=True, text=True, timeout=timeout)
        events.append({'argv': argv, 'exitCode': r.returncode, 'stdout': r.stdout, 'stderr': r.stderr})
        (results/'commands.json').write_text(json.dumps(events, indent=2)+'\n')
        if check and r.returncode: raise RuntimeError('command failed: '+repr(argv))
        return r

    def native(*argv): return run(['virsh', '-c', 'qemu:///system', *argv]).stdout
    def inventory():
        out = {}
        for vmid in native('list', '--all', '--uuid').split():
            if vmid == new_id: continue
            assert native('domstate', vmid).strip() == 'shut off'
            out[vmid] = hashlib.sha256(native('dumpxml', vmid, '--inactive').encode()).hexdigest()
        return out
    def source_hashes():
        out = {}
        for f in source.rglob('*'):
            if f.is_file():
                with f.open('rb') as stream: out[str(f.relative_to(source))] = hashlib.file_digest(stream, 'sha256').hexdigest()
        return out
    def cli(*argv, check=True):
        r = run([str(root/'bin/virmill'), *argv, '--output', 'json', '--non-interactive'], check=check)
        value = json.loads(r.stdout)
        if check: assert value['error'] is None, value
        return value
    def apply(plan):
        argv = ['plan', 'apply', plan['planID'], '--digest', plan['planDigest'], '--idempotency-key', str(uuid.uuid4()), '--wait', '--timeout', '10m']
        for ack in plan['acknowledgements']: argv += ['--ack', ack]
        value = cli(*argv, check=False)
        report.setdefault('operations', []).append(value)
        assert value['error'] is None and value['data']['state'] == 'succeeded', value
        return value['data']['operationID']
    def stop():
        nonlocal daemon
        if daemon is not None:
            daemon.terminate(); daemon.wait(timeout=15); daemon = None
    def profile(name, data=None):
        nonlocal daemon
        stop()
        base = root/name; base.mkdir(mode=0o700)
        for var, part in [('XDG_STATE_HOME', 'state'), ('XDG_RUNTIME_DIR', 'runtime'), ('XDG_CACHE_HOME', 'cache'), ('XDG_CONFIG_HOME', 'config'), ('XDG_DATA_HOME', 'data')]:
            d = base/part; d.mkdir(mode=0o700); env[var] = str(d)
        if data is not None: env['XDG_DATA_HOME'] = str(data)
        with (results/(name+'-daemon.log')).open('w') as log:
            daemon = subprocess.Popen([str(root/'bin/virmilld')], env=env, stdout=log, stderr=log)
        for _ in range(100):
            if (base/'runtime/virmill/control.sock').exists(): return base
            if daemon.poll() is not None: raise RuntimeError('coordinator exited; inspect '+name+'-daemon.log')
            time.sleep(.1)
        raise RuntimeError('coordinator did not become ready')
    try:
        report['binaries'] = json.loads((root/'binaries.json').read_text())
        for name, wanted in report['binaries'].items():
            assert name in ('virmill', 'virmilld') and hashlib.sha256((root/'bin'/name).read_bytes()).hexdigest() == wanted
        baseline = inventory(); before = source_hashes()
        if a.recover_from is None:
            profile('backup-profile', a.source_data)
            report['version'] = cli('version')['data']
            credential = root/'repository-credential'; credential.write_bytes(os.urandom(48).hex().encode()+b'\n'); credential.chmod(0o600)
            repo = str(root/'repository')
            common = {'repository': repo, 'passwordFile': str(credential)}
            init = cli('backup', 'repository', 'init', repo, '--input', json.dumps({'passwordFile': str(credential)}), '--plan')['data']
            report['init'] = cli('backup', 'result', apply(init))['data']
            backup = cli('backup', 'create', a.capture_id, '--input', json.dumps(common), '--plan')['data']
            backup_proof = cli('backup', 'result', apply(backup))['data']; report['backup'] = backup_proof
            assert backup_proof['roundtripVerified'] and not backup_proof['guestBootVerified']
            check = cli('backup', 'repository', 'check', repo, '--input', json.dumps({'passwordFile': str(credential)}), '--plan')['data']
            report['repositoryCheck'] = cli('backup', 'result', apply(check))['data']
            assert report['repositoryCheck']['repositoryDataChecked']
        else:
            previous = a.recover_from.resolve(strict=True)
            assert previous.is_relative_to(Path.home()/'virmill-tests') and previous != root
            proof_file = previous/'results/report.json'
            proof_bytes = proof_file.read_bytes()
            proof = json.loads(proof_bytes)
            assert proof['binaries'] == report['binaries']
            assert proof['sourceCapturePreserved'] and proof['priorDefinitionsPreserved']
            backup_proof = proof['backup']
            assert backup_proof['roundtripVerified'] and proof['repositoryCheck']['repositoryDataChecked']
            assert backup_proof['captureID'] == a.capture_id
            credential = previous/'repository-credential'
            repo = str(previous/'repository')
            common = {'repository': repo, 'passwordFile': str(credential)}
            report['retainedBackupProofSHA256'] = hashlib.sha256(proof_bytes).hexdigest()
            report['retainedBackupRoot'] = str(previous)
            report['backup'] = backup_proof
        fresh = profile('fresh')
        assert cli('operation', 'list')['data'] == []
        report['freshDatabaseInitiallyEmpty'] = True
        args = dict(common, captureID=a.capture_id, manifestSHA256=backup_proof['manifestSHA256'])
        plan = cli('backup', 'restore', backup_proof['snapshotID'], '--input', json.dumps(args), '--plan')['data']
        report['recovery'] = cli('backup', 'result', apply(plan))['data']
        assert report['recovery']['recoveredSetVerified']
        capture = cli('snapshot', 'show', a.capture_id)['data']
        assert capture['manifestSHA256'] == backup_proof['manifestSHA256']
        duplicate = cli('backup', 'restore', backup_proof['snapshotID'], '--input', json.dumps(args), '--plan', check=False)
        assert duplicate['error'] is not None; report['existingCaptureRefused'] = True
        plan = cli('snapshot', 'restore', a.capture_id, '--input', json.dumps({'name': 'Virmill encrypted recovered BIOS '+a.capture_id[:8], 'poolID': a.pool}), '--plan')['data']
        new_id = plan['review']['newVMID']; report['restoredVM'] = new_id
        report['vmRestoreOperationID'] = apply(plan)
        report['vmStartOperationID'] = apply(cli('vm', 'start', new_id, '--plan')['data'])
        assert native('domstate', new_id).strip() == 'running'
        time.sleep(4)
        native('screenshot', new_id, str(results/'recovered-screen.png'))
        report['nativeGuestRunning'] = True; report['bootMarkerVerified'] = False
        report['freshProfile'] = str(fresh); report['status'] = 'passed'
    except BaseException as exc: report['error'] = repr(exc)
    finally:
        if new_id:
            try:
                state = run(['virsh', '-c', 'qemu:///system', 'domstate', new_id], check=False)
                if state.returncode == 0 and state.stdout.strip() != 'shut off': native('destroy', new_id)
            except BaseException as exc: report['cleanupError'] = repr(exc); report['status'] = 'failed'
        try: stop()
        except BaseException as exc: report['daemonStopError'] = repr(exc); report['status'] = 'failed'
        try:
            report['sourceCapturePreserved'] = before is not None and source_hashes() == before
            report['priorDefinitionsPreserved'] = baseline is not None and inventory() == baseline
            if not report['sourceCapturePreserved'] or not report['priorDefinitionsPreserved']: report['status'] = 'failed'
        except BaseException as exc: report['preservationError'] = repr(exc); report['status'] = 'failed'
        (results/'report.json').write_text(json.dumps(report, indent=2)+'\n')
        print(json.dumps(report, sort_keys=True))
    raise SystemExit(report['status'] != 'passed')


if __name__ == '__main__': main()
