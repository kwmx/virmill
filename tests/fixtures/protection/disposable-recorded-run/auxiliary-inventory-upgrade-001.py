"""Single-use parent-operated development RPM upgrade; never execute at authoring.

Only --self-test is a local action. Normal execution is restricted to the fixed
disposable run and retains every failure; it has no repair/retry/rollback mode.
"""
import argparse
import base64
import hashlib
import json
import os
import pathlib
import pwd
import re
import selectors
import sqlite3
import stat
import subprocess
import sys
import tempfile
import time
import unittest
import xml.etree.ElementTree as ET

ROOT = pathlib.Path('<test-vm-home>/virmill-tests/run-65930c6-20260907')
RECIPE = 'auxiliary-inventory-upgrade-001'
REVISION = '490b88cba5bf6e0837e2cb5780e56211c22a7d27'
DEPLOYMENT = '5c9945fcfabe2808ff941ef57144f0f7985edba60fe7358b6e18ae2c71bf739c'
SOURCE_DIGEST = '26e4f8410395a37606d577073d0bdff424610ae04b83df678e61d618ae38eb06'
SOURCE_INVENTORY = 'f0585986ff6693a306a54e86d48db9eecbed5cddca83941d461d3e2bb2727576'
OLD_REVISION = 'dc2ab1a7af8c023c1485d36d0e858a65264ace3e'
OLD_DEPLOYMENT = 'c0b4ef70f038291e4f4803e7934e660b740654c2e2e660bc3784cb86939914f9'
OLD_UNIT = '<test-vm-login>-dc2ab1a.service'
NEW_UNIT = '<test-vm-login>-490b88c.service'
OLD_PID = '27851'
URI = 'qemu:///system'
EXECUTABLES = {'virmill': '/usr/bin/virmill', 'virmilld': '/usr/bin/virmilld',
               'virmill-host-helper': '/usr/libexec/virmill-host-helper'}
OLD_BINARIES = {
    'virmill': '50228ee2df4b54885aa85cf95740b9a126700ac031d10e40181c599deef1ad9d',
    'virmilld': '75c1390cc0cf259cf2243ac81ebd52b8e276a7e3611105e7d2ecfa577d00e25b',
    'virmill-host-helper': 'f6d7eef0cb186b3b18356604d577b39810b0e879942ea4c4d6b4a4f4f828990a',
}
PACKAGES = ('virmill', 'virmill-host-helper')
RPMS = tuple(name + '-0.0.0-0.dev.x86_64.rpm' for name in PACKAGES)
GUESTS = {
    '2ec994ce-2950-498c-8b19-d2f7dbb53a78': 'bbe62f376a3943d795cdac4fd67ff56087efa43024b27cacf0578217a1a44408',
    '666c692d-da0e-4119-9554-727c4af3c751': '12ecd7f3a77ebcaf3b5942a1497bc0bbb32ce5546a3fa156f90ba7e24d432164',
    'a19bf9ee-cd7f-4921-baac-39ce1694eb35': 'e31e20ae14df1e2809c242416ff31416249b8c4d3a14dab74507afb02e0250d2',
    'ae630461-91d3-4f07-ad88-e6842c3dc3ea': 'e3200ea179e0cee3258b413baf8668529f2c1fdbed4535115a674d1b4d40d824',
    'b9496482-2eeb-40e1-892b-4e291c108c52': '7880991288cfbfb9b757678c3c0c0211500fe1fc3f03274a29f84b03e632076e',
    'd4c95f21-28bc-428d-9e5f-ceda025d279e': 'a10ba2cb370cd1740d42e34a944663420e8b716315e47f21d999a5a64249411e',
    'f78674f3-bf3a-43e5-81f9-4283e2472024': '081324fb849b275fafacfdf7efc6e22581f1ea926e87b75e80af8f0af688b31c',
}
TABLES = {'plans': 'id,digest,body,input', 'jobs': 'id,plan_id,body',
          'metadata': 'kind,id,body', 'events': 'job_id,seq,body',
          'locks': 'resource,job_id', 'dedup': 'key,request_digest,job_id,created_at'}
COUNTS = {'plans': 36, 'jobs': 32, 'metadata': 24, 'events': 177, 'locks': 0, 'dedup': 32}
XDG = ('XDG_STATE_HOME', 'XDG_RUNTIME_DIR', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_CONFIG_HOME')
MAX_COMMAND = 2 << 20


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def strict_json(raw):
    require(len(raw) <= 40 << 20, 'JSON exceeds bound')
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate JSON key')
            result[key] = value
        return result
    def constant(_):
        raise ValueError('non-finite JSON constant')
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=constant)


def canonical_path(value):
    require(isinstance(value, str) and 1 < len(value.encode()) <= 4096 and value.startswith('/')
            and os.path.normpath(value) == value and not value.startswith('//')
            and all(ord(c) >= 32 and ord(c) != 127 for c in value), 'noncanonical path')
    return pathlib.Path(value)


def generation(st):
    return (st.st_dev, st.st_ino, st.st_mode, st.st_uid, st.st_gid,
            st.st_nlink, st.st_size, st.st_mtime_ns, st.st_ctime_ns)


def checked_path(path):
    path = canonical_path(str(path))
    for current in reversed((path, *path.parents)):
        st = current.lstat()
        require(not stat.S_ISLNK(st.st_mode), 'symlink component: ' + str(current))
        if current != path:
            require(stat.S_ISDIR(st.st_mode), 'non-directory ancestor')
    return path


def read_regular(path, limit=8 << 20, proc=False):
    path = pathlib.Path(path)
    if not proc:
        checked_path(path)
    # O_PATH validates type before the deliberate readable held-inode reopen.
    held = os.open(path, os.O_PATH | os.O_CLOEXEC | (0 if proc else os.O_NOFOLLOW))
    try:
        before = os.fstat(held)
        require(stat.S_ISREG(before.st_mode) and before.st_size <= limit, 'not a bounded regular input')
        with open('/proc/self/fd/' + str(held), 'rb') as stream:
            require(generation(os.fstat(stream.fileno())) == generation(before), 'readable inode differs')
            raw = stream.read(limit + 1)
            require(len(raw) <= limit and generation(before) == generation(os.fstat(held)), 'input changed while read')
        return raw
    finally:
        os.close(held)


def validate_manifest(raw, digest, revision):
    require(re.fullmatch('[a-f0-9]{64}', digest or '') and sha(raw) == digest, 'deployment digest differs')
    value = strict_json(raw)
    require(value['revision'] == revision, 'deployment revision differs')
    for key in [*('build/bin/' + name for name in EXECUTABLES), *('dist/' + name for name in RPMS)]:
        require(re.fullmatch('[a-f0-9]{64}', value['artifacts'][key]), 'missing/invalid artifact digest')
    if revision == OLD_REVISION:
        require(digest == OLD_DEPLOYMENT and all(value['artifacts']['build/bin/' + name] == expected
                for name, expected in OLD_BINARIES.items()), 'old public runtime pins differ')
    else:
        require(revision == REVISION and value['sourceDigest'] == SOURCE_DIGEST
                and value['sourceInventorySHA256'] == SOURCE_INVENTORY, 'new frozen source identity differs')
    return value


def validate_arguments(arguments):
    require(arguments.revision == REVISION, 'exact frozen revision required')
    require(arguments.deployment_sha256 == DEPLOYMENT, 'exact reviewed deployment SHA256 required')


def decode_rows(value):
    require(set(value) == set(TABLES), 'saved table inventory differs')
    rows = {}
    for table, records in value.items():
        require(len(records) == COUNTS[table], 'saved journal count differs')
        rows[table] = []
        for row in records:
            require(len(row) == len(TABLES[table].split(',')), 'saved journal row shape differs')
            decoded = []
            for cell in row:
                if isinstance(cell, dict):
                    require(set(cell) == {'bytesBase64'}, 'unknown saved BLOB encoding')
                    cell = base64.b64decode(cell['bytesBase64'], validate=True)
                require(cell is None or isinstance(cell, (str, bytes, int, float)), 'unsupported saved cell')
                decoded.append(cell)
            rows[table].append(tuple(decoded))
    return rows


def validate_prior_report(value):
    require(value['recipe'] == 'nvram-binding-tui-003' and value['status'] == 'passed' and value['failures'] == []
            and value['revision'] == OLD_REVISION and value['deploymentManifestSHA256'] == OLD_DEPLOYMENT
            and value['journalCounts'] == COUNTS and value['guestXMLSHA256'] == GUESTS,
            'prior observation identity/counts/guests differ')
    require(value['runtime'] == {'unit': OLD_UNIT, 'pid': OLD_PID,
            'binarySHA256': {name: OLD_BINARIES[name] for name in ('virmill', 'virmilld')}}, 'old runtime report differs')
    for field in ('allJournalRowsUnchanged', 'allStoppedGuestXMLAndStatesUnchanged', 'noJournalTriggersObserved'):
        require(value[field] is True, 'prior preservation incomplete')


def start_command(environment):
    return ['/usr/bin/systemd-run', '--user', '--unit=' + NEW_UNIT,
            '--property=RuntimeMaxSec=7200', '--property=Restart=no', '--property=WorkingDirectory=' + str(ROOT),
            *('--setenv=' + key + '=' + environment[key] for key in XDG), '/usr/bin/virmilld']


def package_path_allowed(package, path):
    if package == 'virmill-host-helper':
        return path in ('/usr/libexec/virmill-host-helper', '/usr/lib/systemd/system/virmill-host-helper.service',
                        '/usr/lib/systemd/system/virmill-host-helper.socket',
                        '/usr/share/doc/virmill-host-helper/helper-policy.example.json',
                        '/usr/share/licenses/virmill-host-helper/LICENSE')
    return path in ('/usr/bin/virmill', '/usr/bin/virmilld', '/usr/lib/systemd/user/virmilld.service',
                    '/usr/share/licenses/virmill/LICENSE', '/usr/share/bash-completion/completions/virmill',
                    '/usr/share/zsh/site-functions/_virmill', '/usr/share/fish/vendor_completions.d/virmill.fish') or any(
                    path.startswith(prefix) for prefix in ('/usr/share/doc/virmill/', '/usr/share/virmill/'))


def collect_child(arguments, environment, timeout):
    process = subprocess.Popen(arguments, env=environment, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, close_fds=True)
    chunks = [bytearray(), bytearray()]
    selector = selectors.DefaultSelector()
    deadline, failure = time.monotonic() + timeout, None
    try:
        for number, stream in enumerate((process.stdout, process.stderr)):
            selector.register(stream, selectors.EVENT_READ, number)
        while selector.get_map():
            require(time.monotonic() < deadline, 'command deadline exceeded')
            for event, _ in selector.select(min(.1, max(0, deadline - time.monotonic()))):
                block = os.read(event.fileobj.fileno(), 65536)
                if not block:
                    selector.unregister(event.fileobj)
                    continue
                remaining = MAX_COMMAND - sum(map(len, chunks))
                chunks[event.data].extend(block[:remaining])
                require(len(block) <= remaining, 'command output bound exceeded')
        process.wait(timeout=max(.001, deadline - time.monotonic()))
    except BaseException as error:
        failure = error
    finally:
        if process.poll() is None:
            process.terminate()  # Only this unreaped command; no process group or daemon signal.
            try:
                process.wait(timeout=1)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=2)
        selector.close()
        process.stdout.close()
        process.stderr.close()
    return process.returncode, bytes(chunks[0]), bytes(chunks[1]), failure


# Fixed read-only root observation. State/key files are O_PATH/stat-only.
# Only public policy/drop-in and UUID-named helper journal metadata get hashed.
# No auxiliary state or private-key bytes enter this program's readable opens.
PROTECTED_PROGRAM = r'''
import hashlib,json,os,pathlib,re,stat,sys
root=pathlib.Path('<test-vm-home>/virmill-tests/run-65930c6-20260907')
result={};count=0
def need(v):
 if not v: raise RuntimeError('protected metadata shape/bound violation')
def metadata(p,hash_content=False):
 global count
 count+=1;need(count<=10000)
 for a in reversed((p,*p.parents)):
  s=a.lstat();need(not stat.S_ISLNK(s.st_mode))
 fd=os.open(p,os.O_PATH|os.O_CLOEXEC|os.O_NOFOLLOW)
 try:
  s=os.fstat(fd);need(stat.S_ISDIR(s.st_mode) or stat.S_ISREG(s.st_mode))
  record=[s.st_dev,s.st_ino,s.st_mode,s.st_uid,s.st_gid,s.st_nlink,s.st_size,s.st_mtime_ns,s.st_ctime_ns]
  if hash_content:
   need(stat.S_ISREG(s.st_mode) and s.st_size<=8<<20)
   with open('/proc/self/fd/'+str(fd),'rb') as f:
    data=f.read((8<<20)+1);need(len(data)<=8<<20)
   after=os.fstat(fd);need((s.st_size,s.st_mtime_ns,s.st_ctime_ns)==(after.st_size,after.st_mtime_ns,after.st_ctime_ns))
   record.append(hashlib.sha256(data).hexdigest())
  result[str(p)]=record
 finally: os.close(fd)
 return s
def tree(p):
 s=metadata(p)
 if stat.S_ISDIR(s.st_mode):
  for child in sorted(p.iterdir()): tree(child)
for name in ('sources','prepared'): tree(root/name)
tree(pathlib.Path('<test-vm-home>/images'))
metadata(root/'config/virmill/helper-key.pem')
for name in ('/etc/virmill/helper-policy.json','/etc/systemd/system/virmill-host-helper.socket.d/50-virmill-disposable-test.conf'):
 metadata(pathlib.Path(name),True)
journal=pathlib.Path('/var/lib/virmill-host-helper');metadata(journal)
for p in sorted(journal.iterdir()):
 need(re.fullmatch(r'[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}(?:\.json|\.complete|\.access-intent\.json|\.access-complete\.json)',p.name))
 metadata(p,True)
paths=json.loads(sys.argv[1]);need(isinstance(paths,list) and len(paths)<=32)
for name in paths:
 p=pathlib.Path(name);need(p.is_absolute() and str(p)==os.path.normpath(name) and
   (p.is_relative_to(root) or p.is_relative_to('/var/lib/libvirt/images')))
 s=metadata(p);need(stat.S_ISREG(s.st_mode))
print(json.dumps(result,sort_keys=True))
'''


class Recipe:
    def __init__(self, arguments):
        self.args, self.started = arguments, time.monotonic()
        self.output, self.bundle = ROOT / RECIPE, ROOT / 'packages/490b88c'
        self.host_env = {**os.environ, 'LC_ALL': 'C', 'LANG': 'C', 'PATH': '/usr/bin:/usr/sbin:/bin:/sbin'}
        for key in ('VIRSH_DEBUG', 'VIRSH_LOG_FILE', 'VIRMILL_SDK_DIRECTORY'):
            self.host_env.pop(key, None)
        self.env = dict(self.host_env)
        self.sequence, self.output_bytes = 0, 0
        self.before = self.protected_before = None
        self.disk_paths = set()
        self.report = {'recipe': RECIPE, 'revision': REVISION, 'previousRevision': OLD_REVISION,
                       'deploymentSHA256': arguments.deployment_sha256, 'status': 'inconclusive',
                       'stage': 'preflight', 'failures': [], 'installAttempted': False,
                       'noGuestOrPolicyMutationRequested': True, 'completeCaptureVerified': False,
                       'independentRecoveryVerified': False, 'releaseQualified': False}

    def save(self, name, value):
        raw = value if isinstance(value, bytes) else (json.dumps(value, indent=2, sort_keys=True) + '\n').encode()
        require(len(raw) <= 48 << 20, 'artifact exceeds bound')
        with (self.output / name).open('xb') as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        fd = os.open(self.output, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)

    def command(self, arguments, timeout=30, allowed=(0,), private=False, stderr=False):
        require(time.monotonic() - self.started < 600, 'recipe exceeded ten-minute budget')
        self.sequence += 1
        require(self.sequence <= 180, 'command count exceeds bound')
        code, out, err, failure = collect_child(arguments, self.env if private else self.host_env,
                                               min(timeout, max(.01, 600 - (time.monotonic() - self.started))))
        self.output_bytes += len(out) + len(err)
        self.save('command-%03d.json' % self.sequence,
                  {'stage': self.report['stage'], 'arguments': arguments, 'exitCode': code,
                   'stdout': out.decode(errors='replace'), 'stderr': err.decode(errors='replace'),
                   'collectionError': None if failure is None else str(failure)[:500]})
        require(self.output_bytes <= 32 << 20, 'total command output exceeds bound')
        if failure is not None:
            raise failure
        require(code in allowed and (stderr or not err), 'command failed; see retained command record')
        return out

    def prop(self, unit, field):
        return self.command(['/usr/bin/systemctl', '--user', 'show', unit, '-p', field, '--value']).decode().strip()

    def helper_inactive(self):
        for unit in ('virmill-host-helper.service', 'virmill-host-helper.socket'):
            state = self.command(['/usr/bin/systemctl', 'show', unit, '-p', 'ActiveState', '--value']).strip()
            require(state == b'inactive', 'helper unit must remain inactive')

    def cli_version(self, revision):
        raw = self.command(['/usr/bin/virmill', 'version', '--connection', URI, '--output', 'json',
                            '--non-interactive', '--timeout', '20s'], private=True)
        value = strict_json(raw)
        require(set(value) == {'apiVersion', 'data', 'warnings', 'error'} and value['apiVersion'] == 'virmill/v1'
                and value['error'] is None and value['warnings'] == [] and value['data']['revision'] == revision,
                'installed CLI version differs')
        return value['data']

    def journal(self):
        database = checked_path(ROOT / 'state/virmill/journal.db')
        with sqlite3.connect(database.as_uri() + '?mode=ro', uri=True, timeout=3) as connection:
            deadline = time.monotonic() + 5
            connection.set_progress_handler(lambda: int(time.monotonic() > deadline), 500)
            connection.execute('PRAGMA query_only=ON')
            connection.execute('BEGIN')
            schema = connection.execute('SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY type,name').fetchall()
            require(not any(row[0] == 'trigger' for row in schema), 'journal has a trigger')
            require({row[1] for row in schema if row[0] == 'table'} == set(TABLES), 'journal has unknown tables')
            require(connection.execute('PRAGMA user_version').fetchone() == (3,), 'unexpected journal schema version')
            rows, cells = {}, 0
            for table, columns in TABLES.items():
                require([row[1] for row in connection.execute('PRAGMA table_info(' + table + ')')]
                        == columns.split(','), 'journal has unknown or reordered columns')
                rows[table] = []
                for row in connection.execute('SELECT ' + columns + ' FROM ' + table + ' ORDER BY rowid'):
                    cells += sum(len(cell) if isinstance(cell, (str, bytes)) else 8 for cell in row)
                    rows[table].append(row)
                    require(cells <= 32 << 20 and len(rows[table]) <= COUNTS[table], 'journal exceeds exact expected bounds')
            require({key: len(value) for key, value in rows.items()} == COUNTS, 'journal counts differ')
            require(all(strict_json(row[2])['state'] in ('succeeded', 'failed', 'canceled', 'partial')
                        for row in rows['jobs']), 'active or uncertain job exists')
            return rows, schema

    def save_journal(self, name, value):
        rows, schema = value
        encoded = {key: [[{'bytesBase64': base64.b64encode(cell).decode()} if isinstance(cell, bytes)
                         else cell for cell in row] for row in records] for key, records in rows.items()}
        self.save(name, {'rows': encoded, 'schema': schema, 'counts': COUNTS, 'triggers': 0, 'userVersion': 3})

    def virsh(self, *arguments):
        return self.command(['/usr/bin/virsh', '--readonly', '-c', URI, *arguments])

    def guests(self, stage):
        ids = self.virsh('list', '--all', '--uuid').decode().split()
        require(len(ids) == 7 and set(ids) == set(GUESTS), 'expected exact seven guest UUIDs')
        for identifier in sorted(GUESTS):
            require(self.virsh('domstate', identifier).strip() == b'shut off', 'existing guest is active')
            raw = self.virsh('dumpxml', '--inactive', identifier)
            require(sha(raw) == GUESTS[identifier], 'existing inactive XML differs')
            require(self.virsh('domstate', identifier).strip() == b'shut off', 'guest state changed around XML')
            self.save(stage + '-' + identifier + '.xml', raw)
            require(b'<!DOCTYPE' not in raw and b'<!ENTITY' not in raw, 'XML declarations unsupported')
            for disk in ET.fromstring(raw).findall('./devices/disk'):
                source = disk.find('source')
                if source is None:
                    continue
                require(disk.get('device') in ('disk', 'cdrom'), 'unsupported existing disk class')
                if disk.get('type') == 'file':
                    require(set(source.attrib) <= {'file', 'index'} and source.get('file'), 'ambiguous disk source')
                    path = source.get('file')
                elif disk.get('type') == 'volume':
                    require(set(source.attrib) <= {'pool', 'volume', 'index'}, 'ambiguous volume source')
                    path = self.virsh('vol-path', '--pool', source.attrib['pool'], source.attrib['volume']).decode().strip()
                else:
                    raise RuntimeError('unsupported disk storage')
                path = canonical_path(path)
                require(path.is_relative_to(ROOT) or path.is_relative_to('/var/lib/libvirt/images'), 'disk outside run storage')
                self.disk_paths.add(str(path))

    def protected(self):
        return strict_json(self.command(['/usr/bin/sudo', '-n', '/usr/bin/python3', '-c',
                                        PROTECTED_PROGRAM, json.dumps(sorted(self.disk_paths))]))

    def installed(self, manifest):
        self.command(['/usr/bin/rpm', '-V', *PACKAGES])
        for package in PACKAGES:
            expected = (package + '|0.0.0|0.dev|x86_64\n').encode()
            require(self.command(['/usr/bin/rpm', '-q', '--qf', '%{NAME}|%{VERSION}|%{RELEASE}|%{ARCH}\n', package]) == expected,
                    'installed package identity differs')
        for name, path in EXECUTABLES.items():
            require(sha(read_regular(path, 128 << 20)) == manifest['artifacts']['build/bin/' + name], 'installed executable differs')

    def runtime(self, unit, manifest, expected_pid=None):
        require(self.prop(unit, 'ActiveState') == 'active' and self.prop(unit, 'SubState') == 'running'
                and self.prop(unit, 'WorkingDirectory') == str(ROOT), 'wrong coordinator state/root')
        pid = self.prop(unit, 'MainPID')
        require(re.fullmatch('[1-9][0-9]*', pid) and (expected_pid is None or pid == expected_pid), 'coordinator PID differs')
        process = pathlib.Path('/proc') / pid
        require(process.stat().st_uid == os.getuid() and os.readlink(process / 'cwd') == str(ROOT), 'coordinator owner/cwd differs')
        require(read_regular(process / 'cmdline', 4096) == b'/usr/bin/virmilld\x00', 'coordinator command differs')
        before = read_regular(process / 'stat', 4096).split(b') ', 1)[1].split()[19]
        require(sha(read_regular(process / 'exe', 128 << 20, proc=True)) == manifest['artifacts']['build/bin/virmilld'],
                'actual coordinator executable differs')
        require(self.prop(unit, 'MainPID') == pid and read_regular(process / 'stat', 4096).split(b') ', 1)[1].split()[19] == before,
                'coordinator process changed during observation')
        return {'unit': unit, 'pid': pid, 'startTimeTicks': before.decode(), 'daemonSHA256': manifest['artifacts']['build/bin/virmilld']}

    def package_inputs(self, manifest):
        require(set(path.name for path in checked_path(self.bundle).iterdir()) == {'deployment.json', *RPMS}, 'bundle must contain deployment and exactly two RPMs')
        for name, package in zip(RPMS, PACKAGES):
            path = self.bundle / name
            require(sha(read_regular(path, 256 << 20)) == manifest['artifacts']['dist/' + name], 'RPM hash differs')
            require(self.command(['/usr/bin/rpm', '-qp', '--qf', '%{NAME}|%{VERSION}|%{RELEASE}|%{ARCH}\n', str(path)])
                    == (package + '|0.0.0|0.dev|x86_64\n').encode(), 'input package identity differs')
            for query in ('--scripts', '--triggers'):
                require(not self.command(['/usr/bin/rpm', '-qp', query, str(path)]), 'package unexpectedly contains scriptlets/triggers')
            paths = self.command(['/usr/bin/rpm', '-qpl', str(path)]).decode().splitlines()
            require(paths and len(paths) == len(set(paths)) <= 4096, 'package file inventory differs')
            for target in paths:
                canonical_path(target)
                require(package_path_allowed(package, target), 'package would install outside fixed program paths')

    def execute(self):
        environment = strict_json(read_regular(ROOT / 'environment.json'))
        for key in XDG:
            path = checked_path(canonical_path(environment[key]))
            require(path.is_relative_to(ROOT) and path.is_dir(), 'private environment escapes run')
            self.env[key] = str(path)
        require(self.env['XDG_STATE_HOME'] == str(ROOT / 'state') and self.env['XDG_CONFIG_HOME'] == str(ROOT / 'config'), 'wrong private state/config root')
        old = validate_manifest(read_regular(ROOT / 'packages/dc2ab1a/deployment.json'), OLD_DEPLOYMENT, OLD_REVISION)
        new = validate_manifest(read_regular(self.bundle / 'deployment.json'), self.args.deployment_sha256, REVISION)
        prior = strict_json(read_regular(ROOT / 'nvram-binding-tui-003/report.json'))
        validate_prior_report(prior)
        prior_rows = decode_rows(strict_json(read_regular(ROOT / 'nvram-binding-tui-003/journal-after.json', 40 << 20)))
        self.save('prior-observation.json', prior)
        self.report['fixtureSHA256'] = sha(read_regular(pathlib.Path(__file__).absolute()))
        self.report['sourceDigest'], self.report['sourceInventorySHA256'] = SOURCE_DIGEST, SOURCE_INVENTORY
        self.package_inputs(new)
        self.command(['/usr/bin/sudo', '-n', 'true'])
        self.helper_inactive()
        self.installed(old)
        self.report['oldRuntime'] = self.runtime(OLD_UNIT, old, OLD_PID)
        self.cli_version(OLD_REVISION)
        require(self.prop(NEW_UNIT, 'LoadState') == 'not-found', 'new unit already exists; never reuse it')
        self.before = self.journal()
        require(self.before[0] == prior_rows, 'durable rows differ from prior TUI observation')
        self.save_journal('journal-before.json', self.before)
        self.guests('before')
        self.protected_before = self.protected()
        self.save('protected-before.json', self.protected_before)
        # Persist exact intent before stopping even this private coordinator.
        self.save('upgrade-intent.json', {'revision': REVISION, 'oldRevision': OLD_REVISION,
                  'deploymentSHA256': self.args.deployment_sha256, 'oldDeploymentSHA256': OLD_DEPLOYMENT,
                  'oldRuntime': self.report['oldRuntime'], 'oldUnit': OLD_UNIT, 'newUnit': NEW_UNIT,
                  'packages': {name: new['artifacts']['dist/' + name] for name in RPMS},
                  'journalCounts': COUNTS, 'guestXMLSHA256': GUESTS, 'status': 'intent-persisted'})
        self.report['stage'] = 'stop old coordinator'
        require(self.runtime(OLD_UNIT, old, OLD_PID) == self.report['oldRuntime'], 'old process changed before stop')
        self.command(['/usr/bin/systemctl', '--user', 'stop', OLD_UNIT])
        require(self.prop(OLD_UNIT, 'ActiveState') == 'inactive' and self.prop(OLD_UNIT, 'MainPID') == '0', 'old unit did not stop')
        require(not pathlib.Path('/proc', OLD_PID).exists(), 'old PID remains or was reused; inspect manually')
        require(self.journal() == self.before, 'journal changed while stopping old coordinator')
        # Re-read immutable bundle inputs immediately before the one install attempt.
        self.package_inputs(new)
        self.report['stage'], self.report['installAttempted'] = 'install two development RPMs', True
        self.save('install-attempt.json', {'status': 'about-to-invoke', 'packages': list(RPMS)})
        self.command(['/usr/bin/sudo', '-n', '/usr/bin/rpm', '-Uvh', '--replacepkgs',
                      *(str(self.bundle / name) for name in RPMS)], timeout=120, stderr=True)
        self.installed(new)
        self.helper_inactive()
        self.report['stage'] = 'start new coordinator'
        require(self.prop(NEW_UNIT, 'LoadState') == 'not-found', 'new unit appeared before start')
        self.command(start_command(self.env), stderr=True)
        socket = pathlib.Path(self.env['XDG_RUNTIME_DIR']) / 'virmill/control.sock'
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if socket.exists() and self.prop(NEW_UNIT, 'ActiveState') == 'active':
                break
            time.sleep(.1)
        self.report['newRuntime'] = self.runtime(NEW_UNIT, new)
        require(self.report['newRuntime']['pid'] != OLD_PID, 'new coordinator reused old PID')
        require(self.prop(NEW_UNIT, 'RuntimeMaxUSec') == '2h' and self.prop(NEW_UNIT, 'Restart') == 'no', 'new unit lifetime/restart differs')
        self.report['installedVersion'] = self.cli_version(REVISION)
        help_text = self.command(['/usr/bin/virmill', 'vm', 'recovery', 'auxiliary', 'inspect', '--help'], private=True)
        require(b'virmill vm recovery auxiliary inspect ID' in help_text, 'installed command help missing')
        reference = read_regular('/usr/share/doc/virmill/cli-reference.md')
        require(b'## `virmill vm recovery auxiliary inspect`' in reference and
                b'virmill vm recovery auxiliary inspect ID [flags]' in reference, 'installed generated reference lacks command')
        self.report['generatedReferenceSHA256'] = sha(reference)
        self.report['stage'] = 'final preservation'
        self.preserve()
        require(self.runtime(NEW_UNIT, new) == self.report['newRuntime'], 'new runtime changed during checks')
        require(self.prop(OLD_UNIT, 'ActiveState') == 'inactive', 'old coordinator restarted')
        self.report['status'] = 'passed'

    def preserve(self):
        after = self.journal()
        self.save_journal('journal-after.json', after)
        require(after == self.before, 'durable journal rows/schema changed')
        self.guests('after')
        protected_after = self.protected()
        self.save('protected-after.json', protected_after)
        require(protected_after == self.protected_before, 'source/media/key/policy/helper journal observation changed')
        self.helper_inactive()
        self.report.update(allDurableRowsAndSchemaUnchanged=True, journalCounts=COUNTS, triggers=0,
                           allSevenStoppedXMLSHA256=GUESTS, protectedMetadataAndPublicJournalHashesUnchanged=True,
                           privateKeyOrAuxiliaryContentRead=False, sourceMediaContentRehashed=False)

    def main(self):
        require(os.getuid() != 0 and pwd.getpwuid(os.getuid()).pw_name == '<test-vm-login>'
                and pathlib.Path.home() == ROOT.parents[1], 'only the fixed ordinary disposable user may execute')
        require(checked_path(ROOT).stat().st_uid == os.getuid(), 'run root owner differs')
        self.output.mkdir(mode=0o700)  # Existing file/directory/symlink is a permanent single-use refusal.
        parent_fd = os.open(ROOT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(parent_fd)
        finally:
            os.close(parent_fd)
        try:
            self.execute()
        except BaseException as error:
            self.report['failures'].append(str(error)[:500])
            self.report['status'] = 'uncertain-preserve-resources' if self.report['installAttempted'] else 'stopped-preserve-resources'
            self.report['nextAction'] = 'Do not rerun or rollback automatically. Review intent, command logs, package state and the two fixed units; preserve all guests and journals.'
            # Do not signal a daemon, reinstall, restart the old runtime or alter a guest on failure.
            if self.before is not None:
                try:
                    after = self.journal()
                    self.save_journal('failure-journal.json', after)
                    self.report['failureJournalUnchanged'] = after == self.before
                except Exception as observation:
                    self.report['failures'].append('failure journal observation: ' + str(observation)[:300])
            if self.protected_before is not None:
                for name, observer in (('guests', lambda: self.guests('failure')),
                                       ('protected', self.protected)):
                    try:
                        observed = observer()
                        if name == 'protected':
                            self.save('failure-protected.json', observed)
                            self.report['failureProtectedUnchanged'] = observed == self.protected_before
                        else:
                            self.report['failureGuestsUnchanged'] = True
                    except Exception as observation:
                        self.report['failures'].append('failure ' + name + ' observation: ' + str(observation)[:300])
        finally:
            self.save('report.json', self.report)
            print(json.dumps(self.report, sort_keys=True), flush=True)
        return 0 if self.report['status'] == 'passed' else 1


def self_test():
    class Checks(unittest.TestCase):
        def test_new_revision_exact(self):
            validate_arguments(argparse.Namespace(revision=REVISION, deployment_sha256=DEPLOYMENT))
            for revision, deployment in (('490b88c', DEPLOYMENT), (OLD_REVISION, DEPLOYMENT),
                                         ('490b88c' + 'a' * 33, DEPLOYMENT), (REVISION, 'a' * 64), (REVISION, None)):
                with self.subTest(revision=revision, deployment=deployment), self.assertRaises(RuntimeError):
                    validate_arguments(argparse.Namespace(revision=revision, deployment_sha256=deployment))

        def test_deployment_hash_and_identity(self):
            value = {'revision': REVISION, 'sourceDigest': SOURCE_DIGEST, 'sourceInventorySHA256': SOURCE_INVENTORY,
                     'artifacts': {key: 'a' * 64 for key in [*('build/bin/' + name for name in EXECUTABLES),
                                                          *('dist/' + name for name in RPMS)]}}
            raw = json.dumps(value).encode()
            self.assertEqual(validate_manifest(raw, sha(raw), REVISION), value)
            for change in ({'revision': OLD_REVISION}, {'sourceDigest': 'b' * 64}, {'sourceInventorySHA256': 'b' * 64}, {'artifacts': {}}):
                altered = json.dumps({**value, **change}).encode()
                with self.subTest(change=change), self.assertRaises((RuntimeError, KeyError)):
                    validate_manifest(altered, sha(altered), REVISION)
            with self.assertRaises(RuntimeError):
                validate_manifest(raw, 'b' * 64, REVISION)

        def test_duplicate_and_nonfinite_json(self):
            for raw in (b'{"revision":1,"revision":1}', b'{"value":NaN}'):
                with self.subTest(raw=raw), self.assertRaises((RuntimeError, ValueError)):
                    strict_json(raw)

        def test_path_rules(self):
            self.assertEqual(str(canonical_path('/ordinary name/file')), '/ordinary name/file')
            for value in ('/', '//root/file', '/root/../file', '/root//file', '/root/', 'relative', '/root\x00file', '/root\nfile'):
                with self.subTest(value=value), self.assertRaises(RuntimeError):
                    canonical_path(value)

        def test_start_scope(self):
            env = {key: str(ROOT / key) for key in XDG}
            command = start_command(env)
            self.assertEqual(command[:3], ['/usr/bin/systemd-run', '--user', '--unit=' + NEW_UNIT])
            self.assertIn('--property=RuntimeMaxSec=7200', command)
            self.assertIn('--property=Restart=no', command)
            self.assertEqual(command[-1], '/usr/bin/virmilld')
            self.assertEqual(len([part for part in command if part.startswith('--setenv=')]), 5)

        def test_package_boundaries(self):
            self.assertTrue(package_path_allowed('virmill', '/usr/share/doc/virmill/cli-reference.md'))
            self.assertTrue(package_path_allowed('virmill-host-helper', '/usr/libexec/virmill-host-helper'))
            for package in PACKAGES:
                for path in ('/etc/virmill/helper-policy.json', '/var/lib/virmill-host-helper/job.json',
                             str(ROOT / 'state/virmill/journal.db'), str(ROOT / 'config/virmill/helper-key.pem')):
                    self.assertFalse(package_path_allowed(package, path))

        def test_prior_identity(self):
            valid = {'recipe': 'nvram-binding-tui-003', 'status': 'passed', 'failures': [], 'revision': OLD_REVISION,
                     'deploymentManifestSHA256': OLD_DEPLOYMENT, 'journalCounts': COUNTS, 'guestXMLSHA256': GUESTS,
                     'runtime': {'unit': OLD_UNIT, 'pid': OLD_PID, 'binarySHA256': {name: OLD_BINARIES[name] for name in ('virmill', 'virmilld')}},
                     'allJournalRowsUnchanged': True, 'allStoppedGuestXMLAndStatesUnchanged': True, 'noJournalTriggersObserved': True}
            validate_prior_report(valid)
            for change in ({'status': 'inconclusive'}, {'failures': ['retained']}, {'guestXMLSHA256': {}}, {'journalCounts': {}},
                           {'runtime': {}}, {'deploymentManifestSHA256': 'b' * 64}):
                with self.subTest(change=change), self.assertRaises(RuntimeError):
                    validate_prior_report({**valid, **change})

        def test_saved_journal_exact_cells(self):
            value = {table: [[None] * len(columns.split(',')) for _ in range(COUNTS[table])] for table, columns in TABLES.items()}
            value['metadata'][0][2] = {'bytesBase64': 'YWJjAA=='}
            self.assertEqual(decode_rows(value)['metadata'][0][2], b'abc\x00')
            value['jobs'].pop()
            with self.assertRaises(RuntimeError):
                decode_rows(value)

        def test_regular_input_and_symlink_refusal(self):
            with tempfile.TemporaryDirectory(prefix='virmill-upgrade-recipe-') as temporary:
                root = pathlib.Path(temporary)
                ordinary = root / 'ordinary file'
                ordinary.write_bytes(b'original generated fixture')
                self.assertEqual(read_regular(ordinary), b'original generated fixture')
                alias = root / 'alias'
                alias.symlink_to(ordinary)
                with self.assertRaises(RuntimeError):
                    read_regular(alias)
                directory = root / 'directory'
                directory.mkdir()
                (directory / 'file').write_bytes(b'fixture')
                parent_alias = root / 'parent-alias'
                parent_alias.symlink_to(directory, target_is_directory=True)
                with self.assertRaises(RuntimeError):
                    read_regular(parent_alias / 'file')
                with self.assertRaises(RuntimeError):
                    read_regular(ordinary, 1)

        def test_single_use_output(self):
            with tempfile.TemporaryDirectory(prefix='virmill-upgrade-single-use-') as temporary:
                output = pathlib.Path(temporary) / RECIPE
                output.mkdir(mode=0o700)
                (output / 'intent.json').write_bytes(b'preserve this failed intent')
                with self.assertRaises(FileExistsError):
                    output.mkdir(mode=0o700)
                self.assertEqual((output / 'intent.json').read_bytes(), b'preserve this failed intent')

        def test_no_deployment_commands_in_self_test(self):
            # This suite only validates pure data/argv helpers; executable recipes are not imported.
            self.assertEqual(len(GUESTS), 7)
            self.assertEqual(sum(COUNTS.values()), 301)
            compile(PROTECTED_PROGRAM, '<fixed-stat-only-program>', 'exec')
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Checks))
    return 0 if result.wasSuccessful() else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--self-test', action='store_true')
    parser.add_argument('--revision')
    parser.add_argument('--deployment-sha256')
    arguments = parser.parse_args()
    if arguments.self_test:
        require(arguments.revision is None and arguments.deployment_sha256 is None, 'self-test takes no deployment arguments')
        return self_test()
    validate_arguments(arguments)
    return Recipe(arguments).main()


if __name__ == '__main__':
    sys.exit(main())
