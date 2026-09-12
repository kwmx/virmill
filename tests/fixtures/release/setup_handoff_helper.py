#!/usr/bin/env python3
"""Explicit disposable-host helper setup; never edits guests/media or old grants.

Run as UID1000 with --execute-disposable --root PRIVATE_STAGE. Creates an absent
private key only on the authorized VM, registers only its public identity, and
starts the already configured helper socket. --grant REQUEST reads the fixture's
exact reviewed network request and adds only that UUID to the v1 networks array.
Keeps previous policy entries and saves public before/after policy evidence.
The key is never printed, read into this script, copied, or returned to the host.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import socket
import stat
import subprocess
import uuid


def run(*argv):
    p = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=30)
    if p.returncode:
        raise RuntimeError(str(argv[:3]) + ': ' + p.stderr[:1024])
    return p.stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--execute-disposable', action='store_true', required=True)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--grant', type=Path)
    parser.add_argument('--register-root', type=Path)
    a = parser.parse_args()
    os.umask(0o077)
    assert socket.gethostname() in ('virmill-test', 'virmill-test.home')
    root = a.root.resolve(strict=True)
    assert root.parent == Path('/home/virmill-test/virmill-tests')
    assert root.stat().st_uid == 1000 and stat.S_IMODE(root.stat().st_mode) == 0o700
    if a.register_root:
        assert os.getuid() == os.geteuid() == 0
        change = json.loads(a.register_root.read_text())
        identity = change['identity']
        assert identity['actorUID'] == 1000
        public = bytes.fromhex(identity['publicKey'])
        assert len(public) == 32 and hashlib.sha256(public).hexdigest() == identity['keyID']
        policy = Path('/etc/virmill/helper-policy.json')
        st = policy.lstat()
        assert stat.S_ISREG(st.st_mode) and st.st_uid == 0 and st.st_nlink == 1 and st.st_mode & 0o022 == 0
        before = policy.read_bytes(); data = json.loads(before)
        assert len(before) < 65536 and data['apiVersion'] == 'virmill/v1' and 1000 in data['actors']
        assert identity['keyID'] not in data['keys'] or data['keys'][identity['keyID']] == identity['publicKey']
        data['keys'][identity['keyID']] = identity['publicKey']
        if change.get('networkID'):
            network = str(uuid.UUID(change['networkID']))
            assert network == change['networkID'] and int(uuid.UUID(network)) != 0
            grant = {'actorUID': 1000, 'keyID': identity['keyID'], 'resourceID': network}
            if grant not in data.setdefault('networks', []): data['networks'].append(grant)
        evidence = root / ('helper-grant-policy.json' if change.get('networkID') else 'helper-bootstrap-policy.json')
        assert not evidence.exists()
        evidence.write_text(json.dumps({'before': json.loads(before), 'after': data}, indent=2) + '\n')
        os.chown(evidence, 1000, 1000); os.chmod(evidence, 0o600)
        temporary = policy.with_name('helper-policy.handoff-' + uuid.uuid4().hex + '.json')
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o644)
        os.fchmod(fd, stat.S_IMODE(st.st_mode))
        with os.fdopen(fd, 'w') as f:
            f.write(json.dumps(data, sort_keys=True) + '\n'); f.flush(); os.fsync(f.fileno())
        assert policy.read_bytes() == before and policy.stat().st_ino == st.st_ino
        os.replace(temporary, policy)
        fd = os.open(policy.parent, os.O_RDONLY | os.O_DIRECTORY); os.fsync(fd); os.close(fd)
        print(json.dumps({'policyUpdated': True, 'networkID': change.get('networkID')}))
        return
    assert os.getuid() == os.geteuid() == 1000 and Path.home() == Path('/home/virmill-test')
    config = Path.home() / '.config/virmill'; assert config.is_dir() and not config.is_symlink()
    key = config / 'helper-key.pem'
    if not key.exists():
        assert not key.is_symlink() and a.grant is None
        fd = os.open(key, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'wb') as f:
            result = subprocess.run(['/usr/bin/openssl', 'genpkey', '-algorithm', 'ED25519'], stdin=subprocess.DEVNULL, stdout=f, stderr=subprocess.PIPE, timeout=10)
            assert result.returncode == 0
            f.flush(); os.fsync(f.fileno())
    identity = json.loads(run('/usr/bin/virmill', 'host', 'helper', 'identity', '--output', 'json', '--non-interactive'))
    assert identity['error'] is None; identity = identity['data']
    change = {'identity': identity}; approval = None
    if a.grant:
        request = a.grant.resolve(strict=True)
        assert request.parent == root / 'creation-network-handoff'
        approval = json.loads(request.read_text())
        assert approval['actorUID'] == 1000 and approval['connectionID'] == 'qemu:///system'
        plan = json.loads(run('/usr/bin/virmill', 'plan', 'show', approval['planID'], '--output', 'json', '--non-interactive'))['data']
        assert plan['operation'] == 'network.create' and plan['planDigest'] == approval['planDigest']
        d = plan['review']['definition']
        assert d['uuid'] == approval['networkUUID'] and d['type'] == 'lab' and d['hostAccess'] == 'allow'
        assert approval['helperOperation'] == 'network.ipv6-filter' and approval['policyVersion'] == 1 and approval['policyArray'] == 'networks'
        change['networkID'] = d['uuid']
    change_path = root / ('helper-grant-public.json' if a.grant else 'helper-bootstrap-public.json')
    with change_path.open('x') as f: json.dump(change, f)
    run('sudo', '-n', 'python3', str(Path(__file__).resolve()), '--execute-disposable', '--root', str(root), '--register-root', str(change_path))
    unit = run('systemctl', 'cat', 'virmill-host-helper.socket')
    assert 'SocketGroup=virmill-test' in unit and 'DirectoryMode=0755' in unit
    assert run('systemctl', 'is-active', 'firewalld').strip() == 'active'
    run('sudo', '-n', 'systemctl', 'start', 'virmill-host-helper.socket')
    if approval is not None:
        approval['approved'] = True
        marker = root / 'creation-network-handoff/helper-approval.json'
        with marker.open('x') as f: json.dump(approval, f)
    print(json.dumps({'status': 'passed', 'keyID': identity['keyID'], 'networkID': change.get('networkID'), 'privateKeyRetainedOnTestVM': True}))


if __name__ == '__main__': main()
