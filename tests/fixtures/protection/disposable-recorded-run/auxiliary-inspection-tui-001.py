"""Parent-only, single-use CLI/actual-PTY auxiliary inspection observation.

Authoring executes --self-test only. Denial and positive modes consume externally
prepared policy; neither mode changes policy or native resources.
"""
import argparse
import base64
import codecs
import copy
import errno
import fcntl
import hashlib
import json
import os
import pathlib
import pty
import re
import selectors
import sqlite3
import stat
import struct
import subprocess
import sys
import termios
import time
import unicodedata
import unittest
import uuid
import xml.etree.ElementTree as ET

ROOT = pathlib.Path('<test-vm-home>/virmill-tests/run-65930c6-20260907')
NATIVE = ROOT / 'auxiliary-inspection-native-002'
NATIVE_NAME = 'auxiliary-inspection-native-002'
RECIPE = 'auxiliary-inspection-tui-001'
REVISION = '490b88cba5bf6e0837e2cb5780e56211c22a7d27'
DEPLOYMENT_SHA = '5c9945fcfabe2808ff941ef57144f0f7985edba60fe7358b6e18ae2c71bf739c'
CLI_SHA = '9cc64d8aa14e7f5d5992514910364f316d3d12fab784393630d7c160628ac886'
NATIVE_RECIPE_SHA = '6ba5784dee8f014a143d87d4c710fb60ca8b36b0f618c633e51224723407790c'
URI = 'qemu:///system'
ROOT_ID = 'auxiliary-fixture-002'
STATE_ROOT = '/var/lib/virmill-host-helper/auxiliary-fixture-002'
VM_NAME = 'virmill-auxiliary-fixture-002'
POLICY = '/etc/virmill/helper-policy.json'
ACTION = 'vm recovery auxiliary inspect'
MAX_COMMAND, MAX_ANSI, MAX_PAGES = 2 << 20, 4 << 20, 128
TABLES = {
    'plans': 'id,digest,body,input', 'jobs': 'id,plan_id,body',
    'metadata': 'kind,id,body', 'events': 'job_id,seq,body',
    'locks': 'resource,job_id', 'dedup': 'key,request_digest,job_id,created_at',
}
NATIVE_CHECKS = [
 'preflight pins, existing stopped XML, public identity and preservation baseline',
 'original policy and base actor/key/root without auxiliary permission deny inspection',
 'exact metadata-only grant returns three payload members and separate empty TPM lock in JSON/NDJSON',
 'new FIFO refused without blocking or successful inventory; only exact created FIFO removed',
 'unresolved source refused without guessed path; original new definition restored exactly',
 'original public policy restored; old inventory preserved; new domain remains stopped and uninitialized',
]

def require(condition, message):
    if not condition:
        raise RuntimeError(message)

def sha(raw):
    return hashlib.sha256(raw).hexdigest()

def strict_json(raw):
    require(len(raw) <= 8 << 20, 'JSON exceeds bound')

    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'duplicate JSON key')
            result[key] = value
        return result

    def constant(_):
        raise ValueError('non-finite JSON constant')

    return json.loads(raw, object_pairs_hook=pairs, parse_constant=constant)

def canonical_uuid(value):
    require(isinstance(value, str) and str(uuid.UUID(value)) == value and uuid.UUID(value).int != 0,
            'noncanonical UUID')
    return value

def generation(st):
    return st.st_dev, st.st_ino, st.st_size, st.st_mtime_ns, st.st_ctime_ns

def read_regular(path, limit=8 << 20, proc=False):
    flags = os.O_RDONLY | os.O_NONBLOCK | os.O_CLOEXEC
    if not proc:
        flags |= os.O_NOFOLLOW
    with os.fdopen(os.open(path, flags), 'rb') as stream:
        before = os.fstat(stream.fileno())
        require(stat.S_ISREG(before.st_mode) and before.st_size <= limit, 'input is not a bounded regular file')
        raw = stream.read(limit + 1)
        require(len(raw) <= limit and generation(before) == generation(os.fstat(stream.fileno())),
                'input changed during bounded read')
        return raw

def stop_child(process):
    """Only this unreaped Popen child, never a process group/coordinator/guest."""
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=1)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=2)

def collect_child(arguments, env, timeout=30, limit=MAX_COMMAND):
    output = [bytearray(), bytearray()]
    process = subprocess.Popen(arguments, env=env, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               start_new_session=True, close_fds=True)
    selector = selectors.DefaultSelector()
    deadline = time.monotonic() + timeout
    failure = None
    try:
        for number, stream in enumerate((process.stdout, process.stderr)):
            selector.register(stream, selectors.EVENT_READ, number)
        while selector.get_map():
            require(time.monotonic() < deadline, 'child output/EOF timeout')
            for event, _ in selector.select(min(.05, max(0, deadline - time.monotonic()))):
                block = os.read(event.fileobj.fileno(), 65536)
                if not block:
                    selector.unregister(event.fileobj)
                    continue
                remaining = limit - sum(map(len, output))
                output[event.data].extend(block[:remaining])
                require(len(block) < remaining, 'child stdout/stderr ceiling reached')
        process.wait(timeout=max(.001, deadline - time.monotonic()))
    except BaseException as error:
        failure = error
    finally:
        stop_child(process)
        selector.close()
        process.stdout.close()
        process.stderr.close()
    return process.returncode, bytes(output[0]), bytes(output[1]), failure

class Screen:
    """Bounded current screen for the pinned plain Bubble Tea renderer.

    Unknown controls fail closed. Historical text is never searched as a screen.
    Cursor movement and erase operations replace cells, including across chunks.
    """
    def __init__(self):
        self.rows, self.columns = 24, 80
        self.cells = [[' '] * self.columns for _ in range(self.rows)]
        self.row = self.column = 0
        self.wrap_pending = False
        self.decoder = codecs.getincrementaldecoder('utf-8')('strict')
        self.escape = ''
        self.updates = 0

    def clear(self):
        self.cells = [[' '] * self.columns for _ in range(self.rows)]
        self.row = self.column = 0
        self.wrap_pending = False

    def newline(self):
        self.row += 1
        if self.row == self.rows:
            self.cells.pop(0)
            self.cells.append([' '] * self.columns)
            self.row -= 1
        self.wrap_pending = False

    def csi(self, sequence):
        final, raw = sequence[-1], sequence[2:-1]
        require(re.fullmatch(r'\??[0-9;:]*', raw) is not None, 'unsupported CSI parameters')
        if raw.startswith('?'):
            require(final in 'hl', 'unsupported private terminal control')
            for value in raw[1:].split(';'):
                require(value in ('25', '1002', '1003', '1006', '1004', '2004', '1049'),
                        'unsupported private terminal mode')
                if value == '1049':
                    self.clear()
            return
        if final == 'm':
            return  # SGR changes appearance only; text cells stay authoritative.
        values = [int(value or '0') for value in raw.split(';')]
        require(len(values) <= 2 and max(values) <= 10000, 'excessive CSI arguments')
        n = values[0] or 1
        if final in 'Hf':
            self.row = min(self.rows - 1, n - 1)
            self.column = min(self.columns - 1, (values[1] or 1) - 1 if len(values) == 2 else 0)
        elif final == 'A':
            self.row = max(0, self.row - n)
        elif final == 'B':
            self.row = min(self.rows - 1, self.row + n)
        elif final == 'C':
            self.column = min(self.columns - 1, self.column + n)
        elif final == 'D':
            self.column = max(0, self.column - n)
        elif final == 'G':
            self.column = min(self.columns - 1, n - 1)
        elif final == 'K':
            require(values[0] in (0, 1, 2), 'unsupported line erase')
            start = 0 if values[0] in (1, 2) else self.column
            end = self.column + 1 if values[0] == 1 else self.columns
            self.cells[self.row][start:end] = [' '] * (end - start)
        elif final == 'J':
            require(values[0] in (0, 2), 'unsupported screen erase')
            if values[0] == 2:
                self.cells = [[' '] * self.columns for _ in range(self.rows)]
            else:
                self.cells[self.row][self.column:] = [' '] * (self.columns - self.column)
                for row in range(self.row + 1, self.rows):
                    self.cells[row] = [' '] * self.columns
        else:
            raise RuntimeError('unsupported terminal control: ' + repr(sequence))
        self.wrap_pending = False

    def feed(self, raw):
        self.updates += 1
        for char in self.decoder.decode(raw):
            if self.escape:
                self.escape += char
                require(len(self.escape) <= 128, 'terminal escape exceeds bound')
                if len(self.escape) == 2:
                    require(char == '[', 'unsupported non-CSI terminal escape')
                elif '@' <= char <= '~':
                    self.csi(self.escape)
                    self.escape = ''
                continue
            if char == '\x1b':
                self.escape = char
            elif char == '\r':
                self.column, self.wrap_pending = 0, False
            elif char == '\n':
                self.newline()
            elif char == '\b':
                self.column, self.wrap_pending = max(0, self.column - 1), False
            else:
                require(char >= ' ' and char != '\x7f' and unicodedata.category(char)[0] != 'C',
                        'unsupported terminal control character')
                require(not unicodedata.combining(char) and unicodedata.east_asian_width(char) not in 'WF',
                        'unsupported non-single-cell text; observation remains inconclusive')
                if self.wrap_pending:
                    self.column = 0
                    self.newline()
                self.cells[self.row][self.column] = char
                if self.column == self.columns - 1:
                    self.wrap_pending = True
                else:
                    self.column += 1

    def complete(self):
        return not self.escape and not self.decoder.getstate()[0]

    def lines(self):
        return [''.join(row).rstrip() for row in self.cells]

    def text(self):
        return '\n'.join(self.lines())

def xml_tree(raw):
    require(isinstance(raw, bytes) and 0 < len(raw) <= 1 << 20, 'XML exceeds bound')
    require(b'<!DOCTYPE' not in raw and b'<!ENTITY' not in raw, 'XML declarations unsupported')
    return ET.fromstring(raw)

def uuid_list(raw):
    values = raw.decode('ascii').split()
    require(len(values) <= 64 and len(values) == len(set(values)), 'UUID inventory ambiguous/excessive')
    return sorted(canonical_uuid(value) for value in values)

def volume_list(raw):
    require(len(raw) <= 65536, 'volume list exceeds bound')
    lines = [line.strip() for line in raw.decode('utf-8').splitlines() if line.strip()]
    require(len(lines) >= 2 and lines[0].split() == ['Name', 'Path'] and
            re.fullmatch('-+', lines[1]), 'unrecognized virsh Name/Path table')
    result = {}
    for line in lines[2:]:
        parts = line.split()
        require(len(parts) == 2 and re.fullmatch('[A-Za-z0-9._-]{1,255}', parts[0]),
                'volume name or column shape unsupported')
        name, path = parts
        require(name not in result and path not in result.values() and path.startswith('/') and
                os.path.normpath(path) == path and len(path) <= 4096, 'duplicate/ambiguous volume path')
        result[name] = path
    require(len(result) <= 128, 'volume count exceeds bound')
    return result

def pool_config(raw):
    """Exact bytes except allocation/available values; capacity remains fixed."""
    tree = xml_tree(raw)
    require(tree.tag == 'pool' and tree.get('type') == 'dir', 'only observed directory pools supported')
    numbers, spans = {}, []
    for name in ('capacity', 'allocation', 'available'):
        nodes = tree.findall(name)
        require(len(nodes) == 1 and nodes[0].attrib == {'unit': 'bytes'} and not list(nodes[0]),
                'one direct byte-valued pool statistic required')
        value = nodes[0].text
        require(value is not None and re.fullmatch('[0-9]{1,20}', value), 'invalid pool statistic')
        numbers[name] = int(value)
        require(numbers[name] <= (1 << 64) - 1, 'pool integer exceeds bound')
        matches = list(re.finditer(rb'<' + name.encode() + rb'\b[^<>]*>([0-9]+)</' + name.encode() + rb'>', raw))
        require(len(matches) == 1 and matches[0][1].decode() == value, 'ambiguous pool statistic bytes')
        if name != 'capacity':
            spans.append(matches[0].span(1))
    require(numbers['capacity'] > 0 and numbers['allocation'] + numbers['available'] == numbers['capacity'],
            'pool accounting bounds/sum differ')
    for start, end in sorted(spans, reverse=True):
        raw = raw[:start] + b'OBSERVED_COUNTER' + raw[end:]
    return raw

def policy_variant(original, identity, vm, state_uid, state_gid, grant):
    p = copy.deepcopy(original)
    require(set(p) <= {'apiVersion', 'keys', 'roots', 'actors', 'auxiliary'} and
            p['apiVersion'] == 'virmill/v1' and isinstance(p['roots'], dict) and
            isinstance(p.get('auxiliary', []), list), 'unknown policy contract')
    require(ROOT_ID not in p['roots'] and STATE_ROOT not in p['roots'].values(), 'fixture root already granted')
    require(identity['actorUID'] in p['actors'] and p['keys'].get(identity['keyID']) == identity['publicKey'],
            'original policy does not already approve this actor/public key')
    require(all(g['resourceID'] != vm and g['rootID'] != ROOT_ID for g in p.get('auxiliary', [])),
            'fixture grant overlaps an existing permission')
    p['roots'][ROOT_ID] = STATE_ROOT
    if grant:
        p.setdefault('auxiliary', []).append({
            'actorUID': identity['actorUID'], 'keyID': identity['keyID'], 'resourceID': vm,
            'rootID': ROOT_ID, 'stateUID': state_uid, 'stateGID': state_gid,
            'maxBytes': 8192, 'maxMembers': 3, 'allowCapture': False,
        })
    return p

def validate_inventory(response, vm, fingerprint, state_uid, state_gid):
    data = response['data']
    require(all(data.get(k) is False for k in
                ('captureVerified', 'independentRestoreVerified', 'guestBootVerified')),
            'metadata promoted to recovery proof')
    observation = data['observation']
    require(observation['version'] == 1 and observation['stage'] == 'inspected' and
            observation.get('artifact') is None, 'unexpected captured artifact/stage')
    canonical_uuid(observation['jobID'])
    require(re.fullmatch('[a-f0-9]{64}', observation['binding']), 'invalid response binding')
    inventory = observation['inventory']
    require(inventory['version'] == 1 and inventory['resource'] == {
        'providerID': 'libvirt', 'connectionID': URI, 'kind': 'vm', 'resourceUUID': vm} and
        inventory['fingerprint'] == fingerprint and inventory['root']['id'] == ROOT_ID and
        inventory['root']['path'] == STATE_ROOT and inventory['root']['state']['uid'] == 0,
        'inventory identity differs')
    require(inventory['layout']['vmID'] == vm and
            inventory['layout']['firmware']['nvram']['path'] == STATE_ROOT + '/nvram.fd' and
            inventory['layout']['tpm']['sourcePath'] == STATE_ROOT + '/tpm' and
            inventory['layout']['tpm']['sourceType'] == 'dir', 'native source mapping differs')
    expected = {'nvram.fd': ('nvram', 4096), 'tpm/empty.state': ('tpm', 0), 'tpm/ordinary.state': ('tpm', 32)}
    members = inventory['members']
    require(len(members) == 3 and len({m['relativePath'] for m in members}) == 3 and
            len({m['id'] for m in members}) == 3 and inventory['totalBytes'] == 4128, 'payload set/count differs')
    for member in members:
        require(member['relativePath'] in expected and
                (member['kind'], member['state']['size']) == expected[member['relativePath']], 'member shape differs')
    lock = inventory['tpmLock']
    require(lock['kind'] == 'tpm-lock' and lock['relativePath'] == 'tpm/.lock' and lock['state']['size'] == 0,
            'empty producer control file was omitted or counted as payload')
    for member in members + [lock]:
        state = member['state']
        require(state['uid'] == state_uid and state['gid'] == state_gid and state['links'] == 1 and
                state['mode'] == stat.S_IFREG | 0o600 and isinstance(state['generation'], str) and
                bool(state['generation']), 'member access/generation metadata differs')
    return inventory


def eligible_native(native):
    require(native['recipe'] == NATIVE_NAME and native['recipeSHA256'] == NATIVE_RECIPE_SHA and
            native['status'] == 'passed' and native['failures'] == [] and native['checks'] == NATIVE_CHECKS and
            native['revision'] == REVISION and native['deploymentManifestSHA256'] == DEPLOYMENT_SHA and
            native['expectedBinarySHA256'] == CLI_SHA and native['policyRestored'] is True,
            'selected native fixture did not pass the exact reviewed recipe/deployment with restored policy')
    canonical_uuid(native['vmID'])
    require(native['vmName'] == VM_NAME and native['fixtureRoot'] == STATE_ROOT and
            re.fullmatch('[a-f0-9]{64}', native['finalFixtureXMLSHA256']), 'native fixture identity differs')
    for name in ('allJournalRowsAndSchemaPreserved', 'allEarlierStoppedXMLPreserved',
                 'allPoolConfigurationAndVolumesPreserved', 'allDeclaredDiskMetadataAndSelectedHashesPreserved',
                 'priorDisposableFileMetadataPreserved', 'suppliedSourceMediaMetadataPreserved',
                 'priorHelperFileMetadataPreserved'):
        require(native[name] is True, 'native preservation incomplete: ' + name)
    for name in ('guestStarted', 'auxiliaryPayloadReadByRecipe', 'validFirmwareOrTPMState', 'captureVerified',
                 'independentRestoreVerified', 'guestBootVerified', 'actualTUIObserved', 'staleNativeRequestInjected',
                 'olderLargeDiskBytesRehashed'):
        require(native[name] is False, 'native result contains an unsupported proof claim: ' + name)


def validate_response(value, mode):
    require(isinstance(value, dict) and set(value) == {'apiVersion', 'data', 'warnings', 'error'} and
            value['apiVersion'] == 'virmill/v1' and value['warnings'] == [], 'unexpected shared envelope')
    if mode == 'denial':
        require(value['data'] is None and isinstance(value['error'], dict) and
                value['error']['code'] == 'PERMISSION_DENIED' and
                value['error']['message'] == 'root ID is not approved by helper policy',
                'expected restored-policy denial was masked or retained successful data')
    else:
        require(value['error'] is None and value['data'] is not None,
                'positive observation failed; no denial fallback is permitted')
    return value


def envelope(raw, code, stderr, mode, machine=True):
    if machine:
        require(raw.endswith(b'\n') and raw.count(b'\n') == 1, 'machine output is not one JSON record')
    value = validate_response(strict_json(raw), mode)
    if mode == 'denial':
        require(code == 4 and stderr == (value['error']['code']+': '+value['error']['message']+'\n').encode(),
                'CLI refusal exit/typed stderr differs')
    else:
        require(code == 0 and stderr == b'', 'positive CLI exit/diagnostics differ')
    return value


def configure_native(name, digest):
    global NATIVE_NAME, NATIVE, NATIVE_RECIPE_SHA, ROOT_ID, STATE_ROOT, VM_NAME
    require(name in ('auxiliary-inspection-native-002', 'auxiliary-inspection-native-003') and
            re.fullmatch('[a-f0-9]{64}', digest or ''), 'explicit allowlisted native recipe and source pin required')
    NATIVE_NAME, NATIVE, NATIVE_RECIPE_SHA = name, ROOT/name, digest
    suffix = name[-3:]
    ROOT_ID = 'auxiliary-fixture-'+suffix
    STATE_ROOT = '/var/lib/virmill-host-helper/'+ROOT_ID
    VM_NAME = 'virmill-'+ROOT_ID


def stable_response(value, mode):
    result = copy.deepcopy(value)
    if mode == 'positive':
        observation = result['data']['observation']
        canonical_uuid(observation['jobID'])
        require(re.fullmatch('[a-f0-9]{64}', observation['binding']), 'invalid typed read binding')
        del observation['jobID'], observation['binding']
    return result


def file_state(s):
    return {'dev': s.st_dev, 'ino': s.st_ino, 'mode': s.st_mode, 'uid': s.st_uid, 'gid': s.st_gid,
            'links': s.st_nlink, 'size': s.st_size, 'mtimeNS': s.st_mtime_ns, 'ctimeNS': s.st_ctime_ns}


def walk_error(error):
    raise error


def policy_snapshot():
    # Public administrator policy only. Atime is excluded: its legitimate reads
    # may change atime; content/identity/access metadata may not change.
    parent = pathlib.Path(POLICY).parent
    require(parent.resolve() == parent and parent.lstat().st_uid == 0 and not parent.lstat().st_mode & 0o022,
            'public policy parent is not the protected administrator directory')
    fd = os.open(POLICY, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_uid == 0 and before.st_nlink == 1 and
                not before.st_mode & 0o022 and before.st_size <= 16384, 'unsafe or oversized public policy')
        raw = os.read(fd, 16385)
        names = sorted(os.listxattr(fd))
        require(len(names) <= 32, 'public policy xattr count exceeds bound')
        attrs = {name: os.getxattr(fd, name).hex() for name in names}
        require(sum(len(k) + len(v)//2 for k, v in attrs.items()) <= 8192, 'public policy xattrs exceed bound')
        after = os.fstat(fd)
        require(len(raw) == before.st_size and file_state(before) == file_state(after) == file_state(os.lstat(POLICY)),
                'public policy changed during observation')
        return {'bytes': base64.b64encode(raw).decode(), 'stat': file_state(after), 'xattrs': attrs}
    finally:
        os.close(fd)


def validate_policy(current, original, granted_receipt, mode, expected_sha):
    raw = base64.b64decode(current['bytes'], validate=True)
    expected = original if mode == 'denial' else granted_receipt
    require(sha(raw) == expected_sha and current['bytes'] == expected['bytes'],
            'externally prepared policy is not the exact pinned mode; no setup/fallback')
    require(current['xattrs'] == original['xattrs'] and all(
        current['stat'][name] == original['stat'][name] for name in ('mode', 'uid', 'gid', 'links')),
        'prepared policy changed original ACL/SELinux/access metadata')
    return strict_json(raw)


class PageTemplate:
    """Wrap the exact Go-indented CLI output and mask only typed read nonces.

    Fragments retain their original JSON line boundaries for reconstruction.
    Overlapping current pages must agree byte-for-byte, including nonce values.
    """
    def __init__(self, table, mode):
        self.fragments, self.patterns, self.ends = [], [], []
        self.actual = {}
        seen = set()
        for line in table.decode('utf-8').removesuffix('\n').split('\n'):
            wildcard = {}
            if mode == 'positive':
                match = re.fullmatch(r'(\s+"(jobID|binding)": ")([a-f0-9-]+)(",?)', line)
                if match:
                    name, value = match[2], match[3]
                    require(name not in seen, 'ambiguous nonce field in CLI template')
                    seen.add(name)
                    if name == 'jobID':
                        canonical_uuid(value)
                    else:
                        require(re.fullmatch('[a-f0-9]{64}', value), 'malformed binding in CLI template')
                    start = match.start(3)
                    for i, char in enumerate(value):
                        wildcard[start + i] = re.escape('-') if char == '-' else '[a-f0-9]'
            starts = list(range(0, len(line), 80)) or [0]
            for start in starts:
                fragment = line[start:start+80]
                pattern = ''.join(wildcard.get(i, re.escape(char)) for i, char in enumerate(fragment, start))
                self.fragments.append(fragment)
                self.patterns.append(re.compile(pattern))
                self.ends.append(start + 80 >= len(line))
        require((mode == 'denial' and not seen) or (mode == 'positive' and seen == {'jobID', 'binding'}),
                'CLI template nonce layout differs')
        require(0 < len(self.fragments) <= 10 * MAX_PAGES, 'TUI output exceeds page bound')

    def matches(self, screen, offset):
        current = screen.lines()
        if current[0] != 'Virmill | ' + URI + ' | Protection' or not current[3].startswith('> ' + ACTION + ' — '):
            return False
        if any('Approve plan ' in line or 'Plan is a preview.' in line or 'Request in progress' in line for line in current):
            return False
        visible = current[5:23]  # Three actions + title/help; 18 output rows.
        for index, actual in enumerate(visible, offset):
            if index >= len(self.fragments):
                if actual:
                    return False
            elif not self.patterns[index].fullmatch(actual) or (index in self.actual and self.actual[index] != actual):
                return False
        return current[23] == ''

    def accept(self, screen, offset):
        require(self.matches(screen, offset), 'current TUI page does not match shared CLI fields')
        for index, value in enumerate(screen.lines()[5:23], offset):
            if index < len(self.fragments):
                self.actual[index] = value

    def reconstructed(self):
        require(len(self.actual) == len(self.fragments), 'TUI envelope has missing pages')
        return ''.join(self.actual[i] + ('\n' if end else '') for i, end in enumerate(self.ends)).encode()


class Terminal:
    def __init__(self, recipe):
        self.recipe, self.screen, self.raw, self.pages = recipe, Screen(), bytearray(), []
        self.process = self.master = self.slave = None
        self.selector, self.closed = selectors.DefaultSelector(), False
        try:
            self.master, self.slave = pty.openpty()
            fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 80, 0, 0))
            self.process = subprocess.Popen(['/usr/bin/virmill', 'tui', '--connection', URI],
                stdin=self.slave, stdout=self.slave, stderr=self.slave, env=recipe.env,
                start_new_session=True, close_fds=True)
            os.close(self.slave); self.slave = None
            self.selector.register(self.master, selectors.EVENT_READ)
        except BaseException:
            self.close(); raise

    def drain(self, duration=.05):
        end = time.monotonic() + duration
        while not self.closed and time.monotonic() < end:
            self.recipe.budget()
            for _, _ in self.selector.select(min(.02, max(0, end-time.monotonic()))):
                try:
                    block = os.read(self.master, 65536)
                except OSError as error:
                    if error.errno != errno.EIO:
                        raise
                    block = b''
                if not block:
                    self.closed = True; return
                remaining = MAX_ANSI-len(self.raw)
                self.raw.extend(block[:remaining])
                require(len(block) < remaining, 'bounded PTY output exhausted')
                self.screen.feed(block)

    def wait(self, predicate, label, newer=None):
        end, previous, stable = time.monotonic()+10, None, None
        while time.monotonic() < end:
            self.drain()
            require(self.process.poll() is None and not self.closed, 'TUI exited during ' + label)
            text = self.screen.text()
            require('Approve plan ' not in text and 'Type the full plan digest' not in text,
                    'unexpected authorization prompt; no further input')
            current = self.screen.updates, text
            if self.screen.complete() and (newer is None or self.screen.updates > newer) and predicate(self.screen):
                if previous == current:
                    stable = stable or time.monotonic()
                    if time.monotonic()-stable >= .15:
                        return
                else:
                    stable = None
            else:
                stable = None
            previous = current
        raise RuntimeError('current screen verification timed out: ' + label)

    def send(self, key):
        require(key in (b'\t', b'\x1b[B', b'\r', b'\x1b[6~', b'\x1b[5~', b'q'), 'unreviewed TUI key')
        require(not self.closed and self.process.poll() is None, 'TUI already closed')
        require(os.write(self.master, key) == len(key), 'partial PTY key write')

    def submit_json(self, vm):
        canonical_uuid(vm)
        text = self.screen.text()
        require('Input: path/ID, or JSON' in text and '> ' + ACTION + ' — ' in text and 'Approve plan ' not in text,
                'JSON must enter only the verified auxiliary read form')
        payload = json.dumps({'id': vm, 'input': {'rootID': ROOT_ID}}, separators=(',', ':')).encode() + b'\r'
        require(os.write(self.master, payload) == len(payload), 'partial PTY JSON form write')

    def observe(self, table):
        self.wait(lambda s: s.lines()[0] == 'Virmill | ' + URI + ' | Overview' and 'Development build.' in s.text(), 'startup')
        for _ in range(6):
            self.send(b'\t')
        self.send(b'\x1b[B')
        self.wait(lambda s: s.lines()[0] == 'Virmill | ' + URI + ' | Protection' and
                  s.lines()[3].startswith('> ' + ACTION + ' — '), 'selected auxiliary read action')
        self.send(b'\r')
        self.wait(lambda s: 'Input: path/ID, or JSON' in s.text(), 'JSON form')
        newer = self.screen.updates
        self.submit_json(self.recipe.vm)
        template = PageTemplate(table, self.recipe.args.mode)
        offsets = list(range(0, len(template.fragments), 10))
        last_offset = 0
        for page, offset in enumerate(offsets):
            require(page < MAX_PAGES, 'page count exceeds bound')
            self.wait(lambda s: template.matches(s, offset), 'result page ' + str(page), newer)
            template.accept(self.screen, offset)
            self.pages.append({'offset': offset, 'screen': self.screen.lines(), 'ansiBytesThrough': len(self.raw)})
            last_offset = offset
            if offset+18 >= len(template.fragments):
                break
            newer = self.screen.updates; self.send(b'\x1b[6~')
        # Exercise current-screen navigation in denial mode even when its error
        # envelope fits one screen. Positive mode already traverses its pages.
        if self.recipe.args.mode == 'denial':
            newer = self.screen.updates; self.send(b'\x1b[6~')
            self.wait(lambda s: template.matches(s, last_offset+10), 'denial page down', newer)
            template.accept(self.screen, last_offset+10)
            self.pages.append({'offset': last_offset+10, 'screen': self.screen.lines(), 'ansiBytesThrough': len(self.raw)})
            newer = self.screen.updates; self.send(b'\x1b[5~')
            self.wait(lambda s: template.matches(s, last_offset), 'denial page up', newer)
            template.accept(self.screen, last_offset)
            self.pages.append({'offset': last_offset, 'screen': self.screen.lines(), 'ansiBytesThrough': len(self.raw)})
        reconstructed = template.reconstructed()
        # The TUI successfully renders a shared error and exits normally; it is
        # not a failed Cobra process and has no separate typed stderr record.
        value = validate_response(strict_json(reconstructed), self.recipe.args.mode)
        self.send(b'q')
        end = time.monotonic()+5
        while (not self.closed or self.process.poll() is None) and time.monotonic() < end:
            if self.closed:
                try: self.process.wait(timeout=min(.05, max(.001, end-time.monotonic())))
                except subprocess.TimeoutExpired: pass
            else:
                self.drain()
        require(self.process.poll() == 0 and self.closed and self.screen.complete(), 'TUI detach/EOF incomplete')
        return value, reconstructed

    def close(self):
        if self.process is not None:
            stop_child(self.process)
        self.selector.close()
        for fd in (self.slave, self.master):
            if fd is not None: os.close(fd)
        self.master = self.slave = None


def volume_config(raw):
    """Preserve every byte except the one validated native access timestamp."""
    tree = xml_tree(raw)
    nodes = tree.findall('./target/timestamps/atime')
    require(tree.tag == 'volume' and tree.get('type') == 'file' and
            len(nodes) == len(tree.findall('.//atime')) == 1,
            'one file-volume access timestamp required')
    node = nodes[0]
    require(not node.attrib and not list(node) and isinstance(node.text, str) and
            re.fullmatch('[0-9]{1,20}(?:\\.[0-9]{1,9})?', node.text), 'invalid volume access timestamp')
    matches = list(re.finditer(rb'<atime>([0-9]+(?:\.[0-9]+)?)</atime>', raw))
    require(len(matches) == 1 and matches[0][1].decode() == node.text,
            'ambiguous volume access timestamp bytes')
    start, end = matches[0].span(1)
    return raw[:start] + b'OBSERVED_ACCESS_TIME' + raw[end:], node.text


class Recipe:
    def __init__(self, args):
        self.args, self.started, self.final_started = args, time.monotonic(), None
        self.output = ROOT / (RECIPE + '-' + args.mode)
        self.host_env = {**os.environ, 'LC_ALL': 'C', 'LANG': 'C', 'TERM': 'xterm-256color',
                         'PATH': '/usr/bin:/usr/sbin:/bin:/sbin'}
        for key in ('VIRSH_DEBUG', 'VIRSH_LOG_FILE'):
            self.host_env.pop(key, None)
        self.env = dict(self.host_env)
        self.sequence = self.command_bytes = 0
        self.baseline = self.guest_before = self.pool_before = self.file_before = self.media_before = self.policy_before = None
        self.report = {'recipe': RECIPE, 'mode': args.mode, 'revision': REVISION,
            'deploymentManifestSHA256': DEPLOYMENT_SHA, 'recipeSHA256': args.recipe_sha256,
            'nativeReportSHA256': args.native_report_sha256, 'preparedPolicySHA256': args.policy_sha256,
            'status': 'inconclusive', 'failures': [], 'checks': [], 'terminal': {'rows': 24, 'columns': 80},
            'policyChangedByRecipe': False, 'noApplyOrLifecycleSubmitted': True, 'captureVerified': False,
            'independentRestoreVerified': False, 'guestBootVerified': False,
            'auxiliaryPayloadBytesRead': False, 'mediaPayloadBytesRead': False,
            'historicalMediaHashesRechecked': False, 'freshRootHelperExecutableHashVerified': False,
            'positivePolicyRestorationOwnedByParent': args.mode == 'positive'}

    def budget(self):
        start, limit = (self.final_started, 180) if self.final_started is not None else (self.started, 360)
        require(time.monotonic()-start < limit, 'bounded observation time exhausted')

    def save(self, name, value):
        raw = value if isinstance(value, bytes) else (json.dumps(value, sort_keys=True, indent=2)+'\n').encode()
        require(len(raw) <= 48 << 20, 'output artifact exceeds bound')
        with (self.output / name).open('xb') as f:
            f.write(raw); f.flush(); os.fsync(f.fileno())

    def command(self, arguments):
        self.budget()
        require(arguments[0] in ('/usr/bin/virmill', '/usr/bin/systemctl', '/usr/bin/virsh'),
                'native observer executable is not allowlisted')
        if arguments[0] == '/usr/bin/virsh':
            require(arguments[1:4] == ['--readonly', '-c', URI] and arguments[4] in
                    ('list', 'domstate', 'dominfo', 'dumpxml', 'pool-list', 'pool-dumpxml', 'vol-list', 'vol-dumpxml'),
                    'virsh command is not a reviewed read')
        elif arguments[0] == '/usr/bin/systemctl':
            require('show' in arguments and arguments[-1] == '--value', 'systemctl command is not a property read')
        else:
            require(arguments[1:2] == ['version'] or arguments[1:4] == ['host', 'helper', 'identity'] or
                    arguments[1:4] == ['vm', 'recovery', 'inspect'] or
                    arguments[1:5] == ['vm', 'recovery', 'auxiliary', 'inspect'], 'CLI command is not a reviewed read')
        self.sequence += 1
        require(self.sequence <= 512, 'command count exceeds bound')
        env = self.env if arguments[0] == '/usr/bin/virmill' else self.host_env
        code, out, err, failure = collect_child(arguments, env, 30)
        self.command_bytes += len(out)+len(err)
        prefix = 'command-%03d' % self.sequence
        self.save(prefix+'.stdout', out); self.save(prefix+'.stderr', err)
        self.save(prefix+'.json', {'arguments': arguments, 'exitCode': code, 'stdoutSHA256': sha(out),
            'stderrSHA256': sha(err), 'collectionError': str(failure) if failure else None})
        require(self.command_bytes <= 32 << 20, 'aggregate command output exceeds bound')
        if failure is not None:
            raise failure
        return code, out, err

    def checked(self, arguments):
        code, out, err = self.command(arguments)
        require(code == 0 and not err, 'read command failed; inspect command-%03d artifacts' % self.sequence)
        return out

    def virsh(self, *arguments):
        return self.checked(['/usr/bin/virsh', '--readonly', '-c', URI, *arguments])

    def cli(self, arguments, mode='positive', output='json'):
        code, out, err = self.command(['/usr/bin/virmill', *arguments, '--connection', URI,
            '--output', output, '--non-interactive', '--timeout', '20s'])
        return envelope(out, code, err, mode, output in ('json', 'ndjson')), out

    def auxiliary(self, output):
        return self.cli(['vm', 'recovery', 'auxiliary', 'inspect', self.vm, '--input',
                         json.dumps({'rootID': ROOT_ID}, separators=(',', ':'))], self.args.mode, output)

    def property(self, unit, key, user=False):
        return self.checked(['/usr/bin/systemctl', *(['--user'] if user else []), 'show', unit,
                              '-p', key, '--value']).decode().strip()

    def runtime(self):
        raw = read_regular(ROOT / 'packages/490b88c/deployment.json')
        require(sha(raw) == DEPLOYMENT_SHA, 'deployment metadata differs')
        manifest = strict_json(raw)
        require(manifest['revision'] == REVISION, 'deployment revision differs')
        hashes = {}
        for name, path in {'virmill': '/usr/bin/virmill', 'virmilld': '/usr/bin/virmilld',
                           'virmill-host-helper': '/usr/libexec/virmill-host-helper'}.items():
            hashes[name] = sha(read_regular(path, 128 << 20))
            require(hashes[name] == manifest['artifacts']['build/bin/'+name], 'installed binary differs: '+name)
        require(hashes['virmill'] == CLI_SHA, 'explicit CLI pin differs')
        unit = '<test-vm-login>-490b88c.service'
        require(self.property(unit, 'ActiveState', True) == 'active' and
                self.property(unit, 'WorkingDirectory', True) == str(ROOT), 'wrong private coordinator')
        pid = self.property(unit, 'MainPID', True)
        require(re.fullmatch('[1-9][0-9]*', pid), 'invalid coordinator PID')
        require(sha(read_regular('/proc/'+pid+'/exe', 128 << 20, proc=True)) == hashes['virmilld'] and
                self.property(unit, 'MainPID', True) == pid, 'running coordinator identity changed')
        require(self.cli(['version'])[0]['data']['revision'] == REVISION, 'CLI revision differs')
        helper = 'virmill-host-helper.service'
        require(self.property(helper, 'ActiveState') == 'active' and
                self.property('virmill-host-helper.socket', 'ActiveState') == 'active', 'parent-prepared helper is inactive')
        helper_pid = self.property(helper, 'MainPID')
        require(re.fullmatch('[1-9][0-9]*', helper_pid), 'invalid helper PID')
        start = self.property(helper, 'ExecMainStartTimestampMonotonic')
        require(re.fullmatch('[1-9][0-9]*', start), 'helper process start identity unavailable')
        current = {'unit': unit, 'pid': pid, 'helperPID': helper_pid,
                   'helperStartMonotonic': start, 'binarySHA256': hashes}
        historical = self.native_report['runtime']
        require(all(current[key] == historical[key] for key in ('unit', 'pid', 'helperPID', 'binarySHA256')),
                'native success belongs to another runtime/process selection')
        require('runtime' not in self.report or self.report['runtime'] == current, 'runtime changed during UI observation')
        self.report.update(runtime=current, sourceDigest=manifest['sourceDigest'], sourceInventorySHA256=manifest['sourceInventorySHA256'])

    def journal(self):
        self.budget()
        db = ROOT / 'state/virmill/journal.db'
        require(db.resolve() == db and stat.S_ISREG(db.lstat().st_mode), 'journal path differs')
        connection = sqlite3.connect(db.as_uri()+'?mode=ro', uri=True, timeout=2)
        deadline, total, size, result = time.monotonic()+5, 0, 0, {}
        try:
            connection.set_progress_handler(lambda: int(time.monotonic() > deadline), 500)
            connection.execute('PRAGMA query_only=ON'); connection.execute('BEGIN')
            for table, columns in TABLES.items():
                result[table] = []
                for row in connection.execute('SELECT '+columns+' FROM '+table+' ORDER BY '+columns):
                    encoded = [{'bytesHex': value.hex()} if isinstance(value, bytes) else value for value in row]
                    payload = json.dumps(encoded, separators=(',', ':')).encode()
                    total += 1; size += len(payload)
                    require(total <= 100000 and size <= 32 << 20, 'journal row/byte bound')
                    result[table].append(sha(payload))
            result['schema'] = [list(row) for row in connection.execute('SELECT type,name,tbl_name,sql FROM sqlite_master ORDER BY type,name')]
            require(len(result['schema']) <= 256 and not result['locks'] and
                    not any(row[0] == 'trigger' for row in result['schema']), 'journal locks/triggers/schema differ')
            return result
        finally:
            connection.close()

    def guests(self):
        result = {}
        for vm in uuid_list(self.virsh('list', '--all', '--uuid')):
            state = self.virsh('domstate', vm)
            require(state.strip() == b'shut off', 'guest is not stopped: '+vm)
            info = {}
            for line in self.virsh('dominfo', vm).decode().splitlines():
                if ':' in line:
                    key, value = line.split(':', 1)
                    require(key not in info, 'duplicate dominfo field')
                    info[key] = value.strip()
            require(info['Persistent'] == 'yes' and info['Autostart'] == 'disable' and info['Managed save'] == 'no',
                    'guest persistence/save/autostart differs')
            raw = self.virsh('dumpxml', '--inactive', vm)
            require(xml_tree(raw).findtext('uuid') == vm and self.virsh('domstate', vm) == state,
                    'guest state/identity changed around XML observation')
            result[vm] = raw
        return result

    def pools(self):
        result, total = {}, 0
        for pool in uuid_list(self.virsh('pool-list', '--all', '--uuid')):
            raw = self.virsh('pool-dumpxml', pool)
            require(xml_tree(raw).findtext('uuid') == pool, 'pool XML identity differs')
            names = volume_list(self.virsh('vol-list', '--pool', pool))
            total += len(names); require(total <= 128, 'volume inventory bound')
            volumes, times = {}, {}
            for name in sorted(names):
                volumes[name], times[name] = volume_config(self.virsh('vol-dumpxml', '--pool', pool, name))
            self.report.setdefault('volumeAccessTimeObservations', []).append({'poolID': pool, 'atime': times})
            result[pool] = {'configurationSHA256': sha(pool_config(raw)),
                           'names': names, 'volumes': {name: sha(value) for name, value in volumes.items()}}
        return result

    def media(self):
        result = {}
        for path, historical in self.native_media.items():
            target = pathlib.Path(path)
            require(target.is_absolute() and str(target) == os.path.normpath(path) and target.resolve() == target and
                    any(target.is_relative_to(base) for base in (ROOT, pathlib.Path('<test-vm-home>/images'),
                        pathlib.Path('/var/lib/libvirt/images'))), 'media path outside observed approved roots')
            state = file_state(target.lstat())
            require(stat.S_ISREG(state['mode']) and state['links'] == 1, 'media no longer one regular file')
            expected = {key: value for key, value in historical['stat'].items() if key != 'atimeNS'}
            require(state == expected, 'native media metadata changed or is unreadable: '+path)
            result[path] = state
        return result

    def files(self):
        result = {}
        for base, prefix in ((ROOT, 'run/'), (pathlib.Path('<test-vm-home>/images'), 'source-media/')):
            require(base.is_dir() and base.resolve() == base, 'preserved ordinary media root unavailable')
            for directory, dirs, files in os.walk(base, followlinks=False, onerror=walk_error):
                dirs[:] = sorted(name for name in dirs if pathlib.Path(directory, name) != self.output)
                require(len(dirs)+len(files) <= 4096, 'file directory bound')
                for name in sorted(dirs+files):
                    path = pathlib.Path(directory, name)
                    if path.parent == ROOT/'state/virmill' and path.name in ('journal.db', 'journal.db-wal', 'journal.db-shm'):
                        continue
                    s = path.lstat()
                    result[prefix+str(path.relative_to(base))] = [s.st_dev, s.st_ino, s.st_mode, s.st_uid, s.st_gid,
                                                               s.st_size, s.st_mtime_ns, s.st_ctime_ns]
                    require(len(result) <= 20000, 'prior file inventory bound')
        return result

    def check_read_result(self, result):
        if self.args.mode == 'positive':
            ownership = self.native_report['fixtureStateAccount']
            inventory = validate_inventory(result, self.vm, self.expected_inventory['fingerprint'], ownership['uid'], ownership['gid'])
            require(inventory == self.expected_inventory, 'metadata differs from selected native restored observation')

    def inspect(self):
        require(os.getuid() != 0 and pathlib.Path.home() == pathlib.Path('<test-vm-home>'), 'designated ordinary actor required')
        environment = strict_json(read_regular(ROOT/'environment.json'))
        for key in ('XDG_STATE_HOME', 'XDG_RUNTIME_DIR', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_CONFIG_HOME'):
            path = pathlib.Path(environment[key])
            require(path.is_absolute() and path.is_relative_to(ROOT) and path.resolve() == path and path.is_dir(), 'private environment differs')
            self.env[key] = str(path)
        require(pathlib.Path(self.env['XDG_STATE_HOME']) == ROOT/'state', 'coordinator database selection differs')
        native_raw = read_regular(NATIVE/'report.json')
        require(sha(native_raw) == self.args.native_report_sha256, 'native report pin differs')
        self.native_report = strict_json(native_raw); eligible_native(self.native_report)
        self.vm = self.native_report['vmID']
        self.save('selected-native-report.json', native_raw)
        self.report['nativeFixture'] = {'recipe': self.native_report['recipe'], 'status': self.native_report['status'],
                                     'recipeSHA256': NATIVE_RECIPE_SHA, 'reportSHA256': sha(native_raw), 'vmID': self.vm}
        self.report['priorNativeFixtures'] = []
        for suffix in range(1, int(NATIVE_NAME[-3:])):
            name = 'auxiliary-inspection-native-%03d' % suffix
            failed_raw = read_regular(ROOT/name/'report.json')
            failed = strict_json(failed_raw)
            require(failed['recipe'] == name and failed['status'] != 'passed' and bool(failed['failures']),
                    'prior native failure history missing: '+name)
            self.save('prior-failed-'+name+'-report.json', failed_raw)
            self.report['priorNativeFixtures'].append({'recipe': name, 'status': failed['status'],
                'failures': failed['failures'], 'reportSHA256': sha(failed_raw)})
        self.runtime()
        identity = self.cli(['host', 'helper', 'identity'])[0]['data']
        original_identity = strict_json(read_regular(NATIVE/'public-helper-identity.json'))
        require(identity['policyApproved'] is True and identity['actorUID'] == os.getuid() and
                all(identity[key] == original_identity[key] for key in ('actorUID', 'keyID', 'publicKey', 'policyPath', 'socketPath')),
                'current helper public identity differs')
        original_policy = strict_json(read_regular(NATIVE/'original-public-policy.json'))
        grant = strict_json(read_regular(NATIVE/'policy-grant-receipt.json'))
        self.policy_before = policy_snapshot()
        policy = validate_policy(self.policy_before, original_policy, grant, self.args.mode, self.args.policy_sha256)
        expected_policy = strict_json(base64.b64decode(original_policy['bytes'], validate=True))
        if self.args.mode == 'positive':
            ownership = self.native_report['fixtureStateAccount']
            expected_policy = policy_variant(expected_policy, identity, self.vm, ownership['uid'], ownership['gid'], True)
        require(policy == expected_policy, 'externally prepared policy broadens native grant scope')
        self.save('policy-before.json', self.policy_before)
        self.expected_inventory = strict_json(read_regular(NATIVE/'inspection-restored.json'))['data']['observation']['inventory']
        self.baseline = self.journal()
        require(self.baseline == strict_json(read_regular(NATIVE/'baseline-journal.json')), 'native coordinator rows changed before TUI')
        self.save('journal-before.json', self.baseline)
        expected_guests = strict_json(read_regular(NATIVE/'baseline-guests.json'))
        expected_guests[self.vm] = self.native_report['finalFixtureXMLSHA256']
        self.guest_before = self.guests()
        require({vm: sha(raw) for vm, raw in self.guest_before.items()} == expected_guests, 'native stopped guest set/XML differs')
        self.save('guests-before.json', expected_guests)
        for vm, raw in self.guest_before.items(): self.save('before-'+vm+'.xml', raw)
        self.pool_before = self.pools()
        require(self.pool_before == strict_json(read_regular(NATIVE/'baseline-pools.json')), 'native pool configuration/volumes differ')
        self.save('pools-before.json', self.pool_before)
        self.native_media = strict_json(read_regular(NATIVE/'baseline-media.json'))
        self.media_before = self.media(); self.save('media-before.json', self.media_before)
        self.report['historicalMediaHashes'] = {path: row['sha256'] for path, row in self.native_media.items() if 'sha256' in row}
        self.file_before = self.files(); self.save('files-before.json', self.file_before)
        results, seen_ids, seen_bindings, table = [], set(), set(), None
        for output in ('json', 'ndjson', 'table'):
            result, raw = self.auxiliary(output)
            self.check_read_result(result)
            self.save('cli-'+output+'.txt', raw)
            if self.args.mode == 'positive':
                observed = result['data']['observation']
                require(observed['jobID'] not in seen_ids and observed['binding'] not in seen_bindings, 'CLI read correlation reused')
                seen_ids.add(observed['jobID']); seen_bindings.add(observed['binding'])
            results.append(stable_response(result, self.args.mode))
            if output == 'table': table = raw
        require(results[0] == results[1] == results[2], 'CLI JSON/NDJSON/table fields differ')
        terminal = None
        try:
            terminal = Terminal(self)
            tui, raw = terminal.observe(table)
            self.check_read_result(tui)
            require(stable_response(tui, self.args.mode) == results[0], 'current TUI envelope differs from shared CLI fields')
            if self.args.mode == 'positive':
                observed = tui['data']['observation']
                require(observed['jobID'] not in seen_ids and observed['binding'] not in seen_bindings, 'TUI read correlation reused')
            self.save('tui-reconstructed.json', raw)
            self.report['actualTUIObserved'] = True
            self.report['pageCount'] = len(terminal.pages)
            self.report['checks'].append('current 80x24 TUI pages preserve CLI fields and typed fresh read correlation; no apply/proof promotion')
        finally:
            if terminal is not None:
                terminal.close()
                self.save('tui.ansi', bytes(terminal.raw)); self.save('tui-pages.json', terminal.pages)
        self.runtime()
        self.report['checks'].append('pinned CLI JSON/NDJSON/table and actual TUI '+self.args.mode+' observation')

    def finish(self):
        self.final_started = time.monotonic()
        for label, before, observe in (
            ('policy', self.policy_before, policy_snapshot), ('journal', self.baseline, self.journal),
            ('guests', self.guest_before, self.guests), ('pools', self.pool_before, self.pools),
            ('media', self.media_before, self.media), ('files', self.file_before, self.files)):
            if before is None:
                continue
            try:
                after = observe()
                require(after == before, label+' changed during UI observation')
                self.report[label+'Preserved'] = True
                self.save(label+'-after.json', {vm: sha(raw) for vm, raw in after.items()} if label == 'guests' else after)
            except BaseException as error:
                self.report['failures'].append('final '+label+' observation: '+str(error))
        self.report['commandCount'] = self.sequence
        if not self.report['failures'] and self.report.get('actualTUIObserved') is True:
            self.report['status'] = 'passed'
        self.save('report.json', self.report)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--self-test', action='store_true')
    parser.add_argument('--mode', choices=('denial', 'positive'))
    parser.add_argument('--native-recipe', choices=('auxiliary-inspection-native-002', 'auxiliary-inspection-native-003'))
    for flag in ('revision', 'deployment-sha256', 'binary-sha256', 'recipe-sha256', 'native-report-sha256', 'native-recipe-sha256', 'policy-sha256'):
        parser.add_argument('--'+flag)
    parser.add_argument('--prepared-positive-policy', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        require(all(value in (None, False) for key, value in vars(args).items() if key != 'self_test'), 'self-test takes no native arguments')
        result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(SelfTests))
        return 0 if result.wasSuccessful() else 1
    require(args.mode is not None and args.revision == REVISION and args.deployment_sha256 == DEPLOYMENT_SHA and
            args.binary_sha256 == CLI_SHA, 'exact frozen runtime and explicit mode required')
    require(args.prepared_positive_policy == (args.mode == 'positive'), 'positive requires separate explicit preparation; no fallback')
    configure_native(args.native_recipe, args.native_recipe_sha256)
    for value in (args.recipe_sha256, args.native_report_sha256, args.policy_sha256):
        require(re.fullmatch('[a-f0-9]{64}', value or ''), 'exact fixture/report/policy SHA256 required')
    require(sha(read_regular(pathlib.Path(__file__), 1 << 20)) == args.recipe_sha256, 'uploaded recipe pin differs')
    require(os.getuid() != 0 and ROOT.is_dir() and ROOT.resolve() == ROOT, 'approved ordinary disposable root required')
    os.umask(0o077)
    recipe = Recipe(args); recipe.output.mkdir(mode=0o700, exist_ok=False)
    try:
        recipe.inspect()
    except BaseException as error:
        recipe.report['failures'].append(type(error).__name__+': '+str(error))
    finally:
        recipe.finish()
    print(json.dumps(recipe.report, sort_keys=True))
    return 0 if recipe.report['status'] == 'passed' else 1


class SelfTests(unittest.TestCase):
    @staticmethod
    def response(mode='positive'):
        if mode == 'denial':
            return {'apiVersion': 'virmill/v1', 'data': None, 'warnings': [],
                    'error': {'code': 'PERMISSION_DENIED', 'message': 'root ID is not approved by helper policy', 'details': {}}}
        return {'apiVersion': 'virmill/v1', 'data': {'apiVersion': 'virmill/v1',
            'observation': {'version': 1, 'jobID': '12345678-1234-4234-8234-123456789abc', 'binding': 'a'*64,
                'stage': 'inspected', 'inventory': {'generation': 'opaque-generation-'*30, 'members': [
                    {'id': 'members/%03d' % i, 'kind': 'tpm', 'state': {'size': i, 'generation': 'abcd'*60}}
                    for i in range(4)]}},
            'captureVerified': False, 'independentRestoreVerified': False, 'guestBootVerified': False},
            'warnings': [], 'error': None}

    @staticmethod
    def table(value):
        return (json.dumps(value, ensure_ascii=False, indent=2)+'\n').encode()

    @staticmethod
    def screen(fragments, offset):
        screen = Screen()
        lines = ['Virmill | '+URI+' | Protection', 'Tab: section  Enter: action',
                 '  vm recovery inspect — Inspect configuration', '> '+ACTION+' — Inspect metadata',
                 '  backup verify-manifest — Check declarations']
        lines += fragments[offset:offset+18]
        lines += ['']*(24-len(lines))
        screen.feed(('\x1b[2J\x1b[H'+'\r\n'.join(lines)).encode())
        return screen

    @staticmethod
    def native():
        value = {'recipe': NATIVE_NAME, 'recipeSHA256': NATIVE_RECIPE_SHA, 'status': 'passed', 'failures': [],
            'checks': NATIVE_CHECKS.copy(), 'revision': REVISION, 'deploymentManifestSHA256': DEPLOYMENT_SHA,
            'expectedBinarySHA256': CLI_SHA, 'policyRestored': True, 'vmID': '12345678-1234-4234-8234-123456789abc',
            'vmName': VM_NAME, 'fixtureRoot': STATE_ROOT, 'finalFixtureXMLSHA256': 'a'*64}
        for key in ('allJournalRowsAndSchemaPreserved', 'allEarlierStoppedXMLPreserved', 'allPoolConfigurationAndVolumesPreserved',
                    'allDeclaredDiskMetadataAndSelectedHashesPreserved', 'priorDisposableFileMetadataPreserved',
                    'suppliedSourceMediaMetadataPreserved', 'priorHelperFileMetadataPreserved'):
            value[key] = True
        for key in ('guestStarted', 'auxiliaryPayloadReadByRecipe', 'validFirmwareOrTPMState', 'captureVerified',
                    'independentRestoreVerified', 'guestBootVerified', 'actualTUIObserved', 'staleNativeRequestInjected',
                    'olderLargeDiskBytesRehashed'):
            value[key] = False
        return value

    def test_exact_successful_native_prerequisite_only(self):
        native = self.native(); eligible_native(native)
        variants = [dict(native, status='uncertain-preserve-resources'), dict(native, failures=['preflight failure']),
                    dict(native, recipeSHA256='0'*64), dict(native, checks=NATIVE_CHECKS[:-1]),
                    dict(native, recipe='auxiliary-inspection-native-001')]
        variants += [dict(native, **{key: not value}) for key, value in native.items() if type(value) is bool]
        for changed in variants:
            with self.subTest(changed=changed), self.assertRaises(RuntimeError): eligible_native(changed)

    def test_modes_require_exact_prepared_public_policy(self):
        original = {'bytes': base64.b64encode(b'{"roots":{}}').decode(),
                    'stat': {'mode': 0o100644, 'uid': 0, 'gid': 0, 'links': 1},
                    'xattrs': {'security.selinux': '6162', 'system.posix_acl_access': '6364'}}
        grant = copy.deepcopy(original); grant['bytes'] = base64.b64encode(b'{"roots":{"fixture":"/exact"}}').decode()
        validate_policy(original, original, grant, 'denial', sha(base64.b64decode(original['bytes'])))
        validate_policy(grant, original, grant, 'positive', sha(base64.b64decode(grant['bytes'])))
        for mode, current in (('positive', original), ('denial', grant)):
            with self.subTest(mode=mode), self.assertRaises(RuntimeError):
                validate_policy(current, original, grant, mode, sha(base64.b64decode(current['bytes'])))
        bad = copy.deepcopy(grant); bad['xattrs']['security.selinux'] = '00'
        with self.assertRaises(RuntimeError): validate_policy(bad, original, grant, 'positive', sha(base64.b64decode(bad['bytes'])))

    def test_cli_typed_denial_stderr_is_required(self):
        value = self.response('denial'); raw = (json.dumps(value)+'\n').encode()
        diagnostic = b'PERMISSION_DENIED: root ID is not approved by helper policy\n'
        self.assertEqual(envelope(raw, 4, diagnostic, 'denial'), value)
        self.assertEqual(validate_response(value, 'denial'), value)
        for code, stderr in ((0, diagnostic), (4, b''), (4, diagnostic+b'extra\n'), (1, diagnostic)):
            with self.subTest(code=code, stderr=stderr), self.assertRaises(RuntimeError): envelope(raw, code, stderr, 'denial')
        bad = dict(value, data={'observation': {}})
        with self.assertRaises(RuntimeError): validate_response(bad, 'denial')
        with self.assertRaises(RuntimeError): envelope(raw+b'\n', 4, diagnostic, 'denial')

    def test_positive_does_not_fall_back_to_denial(self):
        with self.assertRaises(RuntimeError): validate_response(self.response('denial'), 'positive')
        value = self.response()
        self.assertEqual(envelope((json.dumps(value)+'\n').encode(), 0, b'', 'positive'), value)
        with self.assertRaises(RuntimeError): envelope((json.dumps(value)+'\n').encode(), 0, b'unexpected', 'positive')

    def test_positive_pages_reconstruct_with_only_new_nonce_values(self):
        expected = self.response()
        actual = copy.deepcopy(expected)
        actual['data']['observation']['jobID'] = '87654321-4321-4321-8321-cba987654321'
        actual['data']['observation']['binding'] = 'f'*64
        template = PageTemplate(self.table(expected), 'positive')
        actual_fragments = PageTemplate(self.table(actual), 'positive').fragments
        self.assertGreater(len(template.fragments), 18)
        for offset in range(0, len(template.fragments), 10):
            screen = self.screen(actual_fragments, offset)
            template.accept(screen, offset)
            if offset+18 >= len(template.fragments): break
        reconstructed = strict_json(template.reconstructed())
        self.assertEqual(reconstructed, actual)
        self.assertEqual(stable_response(reconstructed, 'positive'), stable_response(expected, 'positive'))

    def test_missing_changed_and_revisited_pages_fail(self):
        expected = self.response(); template = PageTemplate(self.table(expected), 'positive')
        screen = self.screen(template.fragments, 0); template.accept(screen, 0)
        with self.assertRaises(RuntimeError): template.reconstructed()
        changed = copy.deepcopy(expected); changed['data']['observation']['binding'] = 'b'*64
        changed_fragments = PageTemplate(self.table(changed), 'positive').fragments
        self.assertFalse(template.matches(self.screen(changed_fragments, 0), 0))
        changed = copy.deepcopy(expected); changed['data']['captureVerified'] = True
        changed_fragments = PageTemplate(self.table(changed), 'positive').fragments
        offset = next(i for i, line in enumerate(template.fragments) if 'captureVerified' in line)
        self.assertFalse(template.matches(self.screen(changed_fragments, offset), offset))

    def test_denial_current_page_navigation_and_no_old_success(self):
        template = PageTemplate(self.table(self.response('denial')), 'denial')
        for offset in (0, 10, 0): template.accept(self.screen(template.fragments, offset), offset)
        self.assertEqual(strict_json(template.reconstructed()), self.response('denial'))
        screen = Screen(); screen.feed(b'\x1b[Hsuccessful old inventory\x1b[2J\x1b[HPERMISSION_DENIED')
        self.assertNotIn('successful', screen.text())
        self.assertFalse(template.matches(screen, 0))

    def test_screen_incremental_cursor_and_unsupported_controls(self):
        screen = Screen()
        for part in (b'\x1b[2', b'J\x1b[1;', b'1Hreview \xe2', b'\x80\x94 metadata'):
            screen.feed(part)
        self.assertTrue(screen.complete()); self.assertEqual(screen.lines()[0], 'review — metadata')
        screen.feed(b'\x1b[Hdenied\x1b[K'); self.assertEqual(screen.lines()[0], 'denied')
        for raw in (b'\x1b]0;false\x07', b'\x1b[999z', b'\x00', '宽'.encode()):
            with self.subTest(raw=raw), self.assertRaises((RuntimeError, UnicodeError)): Screen().feed(raw)
        screen.feed(b'\x1b['); self.assertFalse(screen.complete())

    def test_volume_only_access_time_can_change(self):
        raw = b"<volume type='file'><name>old</name><capacity>4</capacity><allocation>2</allocation><physical>2</physical><target><timestamps><atime>1788824533.293235434</atime><mtime>1</mtime><ctime>2</ctime></timestamps><path>/old</path></target></volume>"
        changed = raw.replace(b'1788824533.293235434', b'1788824999.123')
        self.assertEqual(volume_config(raw)[0], volume_config(changed)[0])
        self.assertNotEqual(volume_config(raw)[1], volume_config(changed)[1])
        for old, new in ((b'/old', b'/new'), (b'<physical>2', b'<physical>3'), (b'<allocation>2', b'<allocation>3'),
                         (b'<capacity>4', b'<capacity>5'), (b'<mtime>1', b'<mtime>9')):
            with self.subTest(old=old): self.assertNotEqual(volume_config(raw)[0], volume_config(raw.replace(old, new))[0])
        for bad in (raw.replace(b'1788824533.293235434', b'-1'), raw.replace(b'1788824533.293235434', b'1.1234567890'),
                    raw.replace(b'</volume>', b'<atime>1</atime></volume>'), raw.replace(b'<atime>', b'<atime extra="1">'),
                    raw.replace(b'1788824533.293235434', b'1e3')):
            with self.subTest(raw=bad), self.assertRaises(RuntimeError): volume_config(bad)

    def test_pool_parser_preserves_configuration(self):
        raw = b"<pool type='dir'><capacity unit='bytes'>100</capacity><allocation unit='bytes'>40</allocation><available unit='bytes'>60</available><target><path>/old</path></target></pool>"
        self.assertEqual(pool_config(raw), pool_config(raw.replace(b'>40<', b'>41<').replace(b'>60<', b'>59<')))
        self.assertNotEqual(pool_config(raw), pool_config(raw.replace(b'/old', b'/new')))
        with self.assertRaises(RuntimeError): pool_config(raw.replace(b'>60<', b'>61<'))
        with self.assertRaises(RuntimeError): volume_list(b'Name Path\n----\nname /a\nname /b\n')

    def test_offline_strict_parsers(self):
        for raw in (b'{"id":1,"id":2}', b'{"n":NaN}', b'{}{}'):
            with self.subTest(raw=raw), self.assertRaises((RuntimeError, ValueError)): strict_json(raw)
        with self.assertRaises((RuntimeError, ValueError)): canonical_uuid('00000000-0000-0000-0000-000000000000')
        with self.assertRaises(RuntimeError): configure_native('auxiliary-inspection-native-001', 'a'*64)

    def test_child_output_timeout_and_own_child_cleanup(self):
        from unittest import mock
        code, out, err, failure = collect_child([sys.executable, '-c', 'import time; time.sleep(10)'], os.environ, timeout=.05)
        self.assertIsNotNone(failure); self.assertIsNotNone(code)
        code, out, err, failure = collect_child([sys.executable, '-c', 'import os; os.write(1,b"x"*1000)'], os.environ, limit=100)
        self.assertIsNotNone(failure); self.assertEqual(len(out)+len(err), 100)
        child = mock.Mock(); child.poll.return_value = None
        child.wait.side_effect = [subprocess.TimeoutExpired('own child', 1), -9]
        stop_child(child)
        self.assertEqual(child.method_calls, [mock.call.poll(), mock.call.terminate(), mock.call.wait(timeout=1),
                                             mock.call.kill(), mock.call.wait(timeout=2)])

    def test_host_environment_and_mutation_refusal(self):
        from unittest import mock
        args = argparse.Namespace(mode='denial', recipe_sha256='a'*64, native_report_sha256='b'*64, policy_sha256='c'*64)
        with mock.patch.dict(os.environ, {'XDG_RUNTIME_DIR': '/host', 'VIRSH_DEBUG': '1', 'VIRSH_LOG_FILE': '/log'}):
            recipe = Recipe(args)
        recipe.env['XDG_RUNTIME_DIR'] = '/private'; recipe.save = lambda *_: None
        observed = []
        def child(argv, env, timeout):
            observed.append(env.copy()); return 0, b'{}', b'', None
        with mock.patch.dict(globals(), {'collect_child': child}):
            recipe.command(['/usr/bin/virmill', 'version'])
            recipe.command(['/usr/bin/systemctl', '--user', 'show', 'unit', '-p', 'MainPID', '--value'])
            recipe.command(['/usr/bin/virsh', '--readonly', '-c', URI, 'list'])
            for argv in (['/usr/bin/sudo', '-n', 'true'], ['/usr/bin/virmill', 'plan', 'apply'],
                         ['/usr/bin/systemctl', 'start', 'unit'], ['/usr/bin/virsh', '-c', URI, 'define', 'x']):
                with self.subTest(argv=argv), self.assertRaises(RuntimeError): recipe.command(argv)
        self.assertEqual([env['XDG_RUNTIME_DIR'] for env in observed], ['/private', '/host', '/host'])
        self.assertTrue(all('VIRSH_DEBUG' not in env and 'VIRSH_LOG_FILE' not in env for env in observed))


if __name__ == '__main__':
    sys.exit(main())
