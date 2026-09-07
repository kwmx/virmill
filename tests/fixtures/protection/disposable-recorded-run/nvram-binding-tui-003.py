"""Parent-operated, single-use installed CLI/PTY observation; no native execution at authoring.

Run --self-test locally for synthetic terminal/child/pool seams only. Normal mode
retains native-002's exact pool-counter assertion failure, independently observes
its already completed operation and never applies or reconciles a plan.
"""

import argparse
import base64
import codecs
import errno
import fcntl
import hashlib
import json
import os
import pathlib
import pty
import re
import selectors
import signal
import sqlite3
import stat
import struct
import subprocess
import sys
import termios
import time
import unicodedata
import uuid
import xml.etree.ElementTree as ET


ROOT = pathlib.Path('/home/virmill-test/virmill-tests/run-65930c6-20260907')
URI = 'qemu:///system'
POOL_ID = '95b94843-0db0-46ac-9bbf-a2fa3c183818'
POOL_NAME = 'virmill-cold-probe-v1'
POOL_PATH = '/var/lib/libvirt/images/' + POOL_NAME
EXISTING_PLAN = '2bd38ffb-6931-493b-bd01-5b56c68ff574'
EXISTING_OPERATION = '8cafa2bf-f148-4189-bf93-34340f1c432a'
EXISTING_VM = '666c692d-da0e-4119-9554-727c4af3c751'
EXISTING_XML_SHA = '12ecd7f3a77ebcaf3b5942a1497bc0bbb32ce5546a3fa156f90ba7e24d432164'
NATIVE_CHECKS = [
    'immutable new v1 plan; matching reviewed pool budget and exact acknowledgement IDs',
    'first A binding durable before failed Defined receipt; resources and locks retained',
    'same-A observation is idempotent: exact binding and historical fingerprint bytes retained',
    'B rejected by durable binding comparison; A/fingerprint/receipt/locks preserved',
]
POOL_BEFORE = {'capacity': 272029974528, 'allocation': 145265324032, 'available': 126764650496}
POOL_AFTER = {'capacity': 272029974528, 'allocation': 145318100992, 'available': 126711873536}
MAX_COMMAND = 2 << 20
MAX_ANSI = 4 << 20
MAX_PAGES = 128
TABLES = {
    'plans': 'id,digest,body,input', 'jobs': 'id,plan_id,body',
    'metadata': 'kind,id,body', 'events': 'job_id,seq,body',
    'locks': 'resource,job_id', 'dedup': 'key,request_digest,job_id,created_at',
}


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


def validate_native_failure(native, revision, deployment_sha256):
    require(native['recipe'] == 'nvram-binding-native-002' and
            native['status'] == 'uncertain-preserve-resources' and
            native['failures'] == ['existing pool XML changed'] and
            native['lastStage'] == 'final A reconciliation' and native['checks'] == NATIVE_CHECKS,
            'native run has a different or additional failure; supplemental exception does not apply')
    require(native['revision'] == revision and native['deploymentManifestSHA256'] == deployment_sha256,
            'native failure belongs to another deployment')
    require(native['planID'] == EXISTING_PLAN and native['operationID'] == EXISTING_OPERATION and
            native['vmID'] == EXISTING_VM and native['finalNativeXMLSHA256'] == EXISTING_XML_SHA,
            'native failure belongs to another operation or definition')
    require(native['remainingLocks'] == [], 'native operation still retains locks')
    for field in ('scopedTriggerRemoved', 'allSixEarlierStoppedXMLPreserved', 'priorJournalRowsPreserved',
                  'priorDeclaredDiskMetadataPreserved', 'selectedProbeSourceAndDiskHashesPreserved'):
        require(native[field] is True, 'native preservation not verified: ' + field)
    for field in ('olderLargeDiskBytesRehashed', 'guestStarted', 'nvramInitializationVerified',
                  'completeCaptureVerified', 'independentRecoveryVerified', 'guestBootVerified', 'nativeCallCountsObserved'):
        require(native[field] is False, 'unexpected native proof claim: ' + field)


def pool_observation(raw):
    """Return exact configuration bytes with only two numeric texts masked.

    Only the observed direct dir-pool statistics are recognized. XML shape,
    attributes, comments and every other byte remain part of the comparison.
    Capacity stays in the configuration comparison and must remain fixed.
    """
    require(isinstance(raw, bytes) and 0 < len(raw) <= 65536, 'pool XML exceeds bound')
    require(b'<!DOCTYPE' not in raw and b'<!ENTITY' not in raw, 'pool XML DTD/entity is unsupported')
    tree = ET.fromstring(raw)
    require(tree.tag == 'pool' and tree.attrib == {'type': 'dir'}, 'pool kind/attributes differ')
    for tag, expected in (('name', POOL_NAME), ('uuid', POOL_ID), ('target/path', POOL_PATH)):
        nodes = tree.findall(tag)
        require(len(nodes) == 1 and nodes[0].text == expected, 'pool identity or target differs')
    statistics, spans = {}, []
    for name in ('capacity', 'allocation', 'available'):
        nodes = tree.findall(name)
        require(len(nodes) == 1 and not list(nodes[0]) and nodes[0].attrib == {'unit': 'bytes'},
                'pool statistic must be one direct byte-valued leaf: ' + name)
        value = nodes[0].text
        require(isinstance(value, str) and re.fullmatch('[0-9]+', value) is not None and len(value) <= 20,
                'pool statistic is not a bounded nonnegative integer')
        statistics[name] = int(value)
        require(statistics[name] <= (1 << 64) - 1, 'pool statistic exceeds uint64')
        pattern = rb'<' + name.encode() + rb'\b[^<>]*>([0-9]+)</' + name.encode() + rb'>'
        matches = list(re.finditer(pattern, raw))
        require(len(matches) == 1 and matches[0][1].decode() == value,
                'pool statistic is duplicated, nested or has unsupported lexical form')
        if name != 'capacity':
            spans.append(matches[0].span(1))
    require(statistics['capacity'] > 0 and statistics['allocation'] <= statistics['capacity'] and
            statistics['available'] <= statistics['capacity'] and
            statistics['allocation'] + statistics['available'] == statistics['capacity'],
            'observed directory-pool statistics have inconsistent bounds or sum')
    masked = raw
    for start, end in sorted(spans, reverse=True):
        masked = masked[:start] + b'OBSERVED_NUMERIC_STAT' + masked[end:]
    return statistics, masked


def compare_pool_observation(reference, observed):
    before, expected_bytes = pool_observation(reference)
    current, actual_bytes = pool_observation(observed)
    require(expected_bytes == actual_bytes and before['capacity'] == current['capacity'],
            'pool XML changed outside allocation/available numeric texts')
    return {'statistics': current, 'configurationBytesSHA256': sha(actual_bytes),
            'rawXMLSHA256': sha(observed)}


def saved_pool_xml(raw, expected_stage):
    command = strict_json(raw)
    require(set(command) == {'stage', 'arguments', 'exitCode', 'stdout', 'stderr'} and
            command['stage'] == expected_stage and command['exitCode'] == 0 and command['stderr'] == '' and
            command['arguments'] == ['/usr/bin/virsh', '--readonly', '-c', URI, 'pool-dumpxml', POOL_ID] and
            isinstance(command['stdout'], str), 'saved pool observation command does not match reviewed read')
    xml = command['stdout'].encode('utf-8')
    pool_observation(xml)
    return xml


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


class Terminal:
    def __init__(self, recipe, name):
        self.recipe, self.name = recipe, name
        self.screen, self.raw, self.pages = Screen(), bytearray(), []
        self.process = None
        self.master = self.slave = None
        self.selector = selectors.DefaultSelector()
        self.closed = False
        try:
            self.master, self.slave = pty.openpty()
            fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 80, 0, 0))
            self.process = subprocess.Popen(['/usr/bin/virmill', 'tui', '--connection', URI],
                                            stdin=self.slave, stdout=self.slave, stderr=self.slave,
                                            env=recipe.env, start_new_session=True, close_fds=True)
            os.close(self.slave)
            self.slave = None
            self.selector.register(self.master, selectors.EVENT_READ)
        except BaseException:
            self.close()
            raise

    def drain(self, duration=.05):
        end = time.monotonic() + duration
        while not self.closed and time.monotonic() < end:
            self.recipe.budget()
            for _, _ in self.selector.select(min(.02, max(0, end - time.monotonic()))):
                try:
                    block = os.read(self.master, 65536)
                except OSError as error:
                    if error.errno != errno.EIO:
                        raise
                    block = b''
                if not block:
                    self.closed = True
                    return
                remaining = MAX_ANSI - len(self.raw)
                self.raw.extend(block[:remaining])
                require(len(block) < remaining, 'PTY ANSI ceiling reached')
                self.screen.feed(block)

    def wait(self, predicate, label, timeout=10, newer_than=None):
        end, stable, previous = time.monotonic() + timeout, None, None
        while time.monotonic() < end:
            self.drain()
            require(self.process.poll() is None and not self.closed, 'TUI exited during ' + label)
            text = self.screen.text()
            require('Approve plan ' not in text and 'Type the full plan digest' not in text,
                    'unexpected apply confirmation; no further keys sent')
            current = (self.screen.updates, text)
            if self.screen.complete() and (newer_than is None or self.screen.updates > newer_than) and predicate(self.screen):
                if current == previous:
                    stable = stable or time.monotonic()
                    if time.monotonic() - stable >= .15:
                        return
                else:
                    stable = None
            else:
                stable = None
            previous = current
        raise RuntimeError('current TUI screen did not verify: ' + label)

    def send(self, key):
        require(key in (b'\t', b'\x1b[B', b'\r', b'\x1b[6~', b'q'), 'unreviewed TUI navigation key')
        require(not self.closed and self.process.poll() is None, 'cannot write to exited TUI')
        require(os.write(self.master, key) == len(key), 'partial PTY key write')

    def submit_id(self, value):
        canonical_uuid(value)
        require('Input: path/ID' in self.screen.text() and 'Approve plan ' not in self.screen.text(),
                'ID may only be entered in the verified read form')
        raw = value.encode('ascii') + b'\r'
        require(os.write(self.master, raw) == len(raw), 'partial PTY ID write')

    def close(self):
        if self.process is not None:
            stop_child(self.process)
        self.selector.close()
        for fd in (self.slave, self.master):
            if fd is not None:
                os.close(fd)
        self.slave = self.master = None

    def observe(self, command, identifier, table):
        section, tabs, down = ('Jobs', 8, 5) if command == 'plan show' else ('VMs', 1, 4)
        self.wait(lambda screen: 'Development build.' in screen.text() and
                  screen.lines()[0] == 'Virmill | ' + URI + ' | Overview', 'startup')
        for _ in range(tabs):
            self.send(b'\t')
        for _ in range(down):
            self.send(b'\x1b[B')
        self.wait(lambda screen: screen.lines()[0] == 'Virmill | ' + URI + ' | ' + section and
                  any(line.startswith('> ' + command + ' — ') for line in screen.lines()), 'selected read action')
        self.send(b'\r')
        self.wait(lambda screen: 'Input: path/ID' in screen.text(), 'read-only ID form')
        generation_before = self.screen.updates
        self.submit_id(identifier)
        lines = []
        for line in table.decode('utf-8').removesuffix('\n').split('\n'):
            lines.extend([line[i:i+80] for i in range(0, len(line), 80)] or [''])
        require(len(lines) <= 10 * MAX_PAGES, 'review needs more than bounded paging')
        is_plan = command == 'plan show'
        header_rows, available = (9, 14) if is_plan else (11, 12)

        def matches(screen, offset):
            current = screen.lines()
            if current[0] != 'Virmill | ' + URI + ' | ' + section or not any(
                    line.startswith('> ' + command + ' — ') for line in current):
                return False
            preview = 'Plan is a preview. Press a to review authorization.'
            if is_plan and current[8] != preview:
                return False
            if not is_plan and preview in current:
                return False
            expected = [line.rstrip() for line in lines[offset:offset+available]]
            expected += [''] * (24 - header_rows - len(expected))
            return current[header_rows:] == expected

        for page, offset in enumerate(range(0, len(lines), 10)):
            require(page < MAX_PAGES, 'paging bound exceeded')
            self.wait(lambda screen: matches(screen, offset), 'exact %s page %d' % (command, page),
                      newer_than=generation_before)
            self.pages.append({'page': page, 'offset': offset, 'ansiBytesThrough': len(self.raw),
                               'screen': self.screen.lines(), 'exactCLIEnvelopeSlice': True})
            if offset + available >= len(lines):
                break
            generation_before = self.screen.updates
            self.send(b'\x1b[6~')
        require(self.pages and self.pages[-1]['offset'] + available >= len(lines), 'incomplete TUI envelope')
        # q detaches this UI only. No a, confirmation digest, apply or lifecycle key is sent.
        self.send(b'q')
        end = time.monotonic() + 5
        while (not self.closed or self.process.poll() is None) and time.monotonic() < end:
            if self.closed:
                try:
                    self.process.wait(timeout=min(.05, max(.001, end - time.monotonic())))
                except subprocess.TimeoutExpired:
                    pass
            else:
                self.drain()
        require(self.process.poll() == 0 and self.closed, 'TUI did not close with a complete EOF')
        require(self.screen.complete(), 'truncated ANSI/UTF-8 at TUI close')


class Recipe:
    def __init__(self, arguments):
        self.args, self.started = arguments, time.monotonic()
        self.output = ROOT / 'nvram-binding-tui-003'
        self.native = ROOT / 'nvram-binding-native-002'
        self.host_env = {**os.environ, 'LC_ALL': 'C', 'LANG': 'C', 'TERM': 'xterm-256color',
                         'PATH': '/usr/bin:/usr/sbin:/bin:/sbin'}
        for key in ('VIRSH_DEBUG', 'VIRSH_LOG_FILE'):
            self.host_env.pop(key, None)
        self.env = dict(self.host_env)
        self.sequence, self.command_bytes = 0, 0
        self.baseline = self.guest_before = None
        self.pool_reference = None
        self.report = {'recipe': 'nvram-binding-tui-003', 'revision': arguments.revision,
                       'deploymentManifestSHA256': arguments.deployment_sha256,
                       'status': 'inconclusive', 'failures': [], 'terminal': {'rows': 24, 'columns': 80},
                       'noApplyOrLifecycleSubmitted': True, 'nvramInitializationVerified': False,
                       'completeCaptureVerified': False, 'independentRecoveryVerified': False,
                       'guestBootVerified': False}

    def budget(self):
        require(time.monotonic() - self.started < 240, 'fixture exceeded four-minute budget')

    def save(self, name, value):
        raw = value if isinstance(value, bytes) else (json.dumps(value, indent=2, sort_keys=True) + '\n').encode()
        require(len(raw) <= 96 << 20, 'artifact exceeds bound')
        with (self.output / name).open('xb') as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())

    def command(self, arguments):
        self.budget()
        self.sequence += 1
        require(self.sequence <= 100, 'command count bound exceeded')
        environment = self.env if arguments[0] == '/usr/bin/virmill' else self.host_env
        code, out, err, failure = collect_child(arguments, environment, min(30, max(.01, 240 - (time.monotonic() - self.started))))
        self.command_bytes += len(out) + len(err)
        prefix = 'command-%03d' % self.sequence
        self.save(prefix + '.stdout', out)
        self.save(prefix + '.stderr', err)
        self.save(prefix + '.json', {'arguments': arguments, 'exitCode': code,
                  'stdoutSHA256': sha(out), 'stderrSHA256': sha(err),
                  'collectionError': None if failure is None else str(failure)[:500]})
        if failure is not None:
            raise failure
        require(self.command_bytes <= 32 << 20, 'total command output bound exceeded')
        require(code == 0 and not err, 'read command failed or produced stderr; see command artifact')
        return out

    def cli(self, *arguments, output='json'):
        raw = self.command(['/usr/bin/virmill', *arguments, '--connection', URI, '--output', output,
                            '--non-interactive', '--timeout', '20s'])
        value = strict_json(raw)
        require(set(value) == {'apiVersion', 'data', 'warnings', 'error'} and value['apiVersion'] == 'virmill/v1'
                and value['error'] is None and value['warnings'] == [], 'unexpected CLI envelope')
        if output in ('json', 'ndjson'):
            require(raw.count(b'\n') == 1 and raw.endswith(b'\n'), 'machine output is not one clean envelope')
        return value, raw

    def virsh(self, *arguments):
        return self.command(['/usr/bin/virsh', '--readonly', '-c', URI, *arguments])

    def journal(self):
        self.budget()
        database = ROOT / 'state/virmill/journal.db'
        with sqlite3.connect(database.as_uri() + '?mode=ro', uri=True, timeout=3) as connection:
            deadline = time.monotonic() + 5
            connection.set_progress_handler(lambda: int(time.monotonic() > deadline), 500)
            connection.execute('PRAGMA query_only=ON')
            connection.execute('BEGIN')
            require(not connection.execute("SELECT name,sql FROM sqlite_master WHERE type='trigger'").fetchall(),
                    'unexpected journal trigger remains or appeared')
            snapshot, count, size = {}, 0, 0
            for table, columns in TABLES.items():
                snapshot[table] = []
                for row in connection.execute('SELECT ' + columns + ' FROM ' + table + ' ORDER BY rowid'):
                    count += 1
                    size += sum(len(cell) if isinstance(cell, (str, bytes)) else 8 for cell in row)
                    require(count <= 100000 and size <= 32 << 20, 'journal snapshot exceeds bound')
                    snapshot[table].append(row)
            return snapshot

    def save_journal(self, name, snapshot):
        encoded = {table: [[{'bytesBase64': base64.b64encode(cell).decode()} if isinstance(cell, bytes)
                            else cell for cell in row] for row in rows] for table, rows in snapshot.items()}
        self.save(name, encoded)

    def guests(self):
        ids = self.virsh('list', '--all', '--uuid').decode('ascii').split()
        require(0 < len(ids) <= 32 and len(ids) == len(set(ids)), 'ambiguous guest inventory')
        result = {}
        for identifier in sorted(ids):
            canonical_uuid(identifier)
            state = self.virsh('domstate', identifier)
            require(state.strip() == b'shut off', 'guest is not stopped')
            xml = self.virsh('dumpxml', '--inactive', identifier)
            require(self.virsh('domstate', identifier) == state, 'guest state changed around XML observation')
            result[identifier] = (state, xml)
        return result

    def runtime(self, manifest):
        hashes = {}
        for name in ('virmill', 'virmilld'):
            expected = manifest['artifacts']['build/bin/' + name]
            require(re.fullmatch('[a-f0-9]{64}', expected), 'invalid binary digest')
            hashes[name] = sha(read_regular('/usr/bin/' + name, 128 << 20))
            require(hashes[name] == expected, 'installed binary differs from deployment')
        unit = 'virmill-test-' + self.args.revision[:7] + '.service'

        def prop(name):
            return self.command(['/usr/bin/systemctl', '--user', 'show', unit, '-p', name, '--value']).decode().strip()

        require(prop('ActiveState') == 'active' and prop('WorkingDirectory') == str(ROOT), 'wrong coordinator unit/root')
        pid = prop('MainPID')
        require(re.fullmatch('[1-9][0-9]*', pid), 'invalid coordinator PID')
        require(sha(read_regular('/proc/' + pid + '/exe', 128 << 20, proc=True)) == hashes['virmilld']
                and prop('MainPID') == pid, 'coordinator executable or PID changed')
        require(self.cli('version')[0]['data']['revision'] == self.args.revision, 'installed version differs')
        return {'unit': unit, 'pid': pid, 'binarySHA256': hashes}

    def inspect(self):
        require(os.getuid() != 0 and pathlib.Path.home() == ROOT.parents[1], 'only the designated ordinary-user run root is supported')
        environment = strict_json(read_regular(ROOT / 'environment.json'))
        for key in ('XDG_STATE_HOME', 'XDG_RUNTIME_DIR', 'XDG_CACHE_HOME', 'XDG_DATA_HOME', 'XDG_CONFIG_HOME'):
            path = pathlib.Path(environment[key])
            require(path.is_absolute() and path.is_relative_to(ROOT) and path.resolve() == path, 'environment escapes run root')
            self.env[key] = str(path)
        require(pathlib.Path(self.env['XDG_STATE_HOME']) / 'virmill/journal.db' == ROOT / 'state/virmill/journal.db', 'wrong journal')
        raw = read_regular(ROOT / 'packages' / self.args.revision[:7] / 'deployment.json')
        require(sha(raw) == self.args.deployment_sha256, 'deployment manifest hash differs')
        manifest = strict_json(raw)
        require(manifest['revision'] == self.args.revision, 'deployment revision differs')
        native_report_raw = read_regular(self.native / 'report.json')
        native = strict_json(native_report_raw)
        validate_native_failure(native, self.args.revision, self.args.deployment_sha256)
        self.save('native-failed-report.json', native_report_raw)
        self.report['nativeFixture'] = {'recipe': native['recipe'], 'status': native['status'],
            'failures': native['failures'], 'lastStage': native['lastStage'], 'checks': native['checks'],
            'reportSHA256': sha(native_report_raw), 'failedResultRetained': True}
        command_before = read_regular(self.native / 'command-047.json')
        command_after = read_regular(self.native / 'command-388.json')
        pool_before = saved_pool_xml(command_before, 'preflight')
        pool_after = saved_pool_xml(command_after, 'final A reconciliation')
        require(pool_observation(pool_before)[0] == POOL_BEFORE and pool_observation(pool_after)[0] == POOL_AFTER,
                'saved pool statistics differ from the reviewed native-002 failure')
        comparison = compare_pool_observation(pool_before, pool_after)
        self.pool_reference = pool_before
        self.save('native-pool-command-047.json', command_before)
        self.save('native-pool-command-388.json', command_after)
        self.save('pool-native-before.xml', pool_before)
        self.save('pool-native-after.xml', pool_after)
        self.report['nativePoolDifference'] = {'before': POOL_BEFORE, 'after': POOL_AFTER,
            'allocationDeltaBytes': POOL_AFTER['allocation'] - POOL_BEFORE['allocation'],
            'availableDeltaBytes': POOL_AFTER['available'] - POOL_BEFORE['available'],
            'configurationBytesSHA256': comparison['configurationBytesSHA256'],
            'onlyAllocationAvailableNumericTextsDiffer': True, 'capacityUnchanged': True,
            'deltaAttributedToNewDisk': False}
        self.report.update(nativeReportSHA256=sha(native_report_raw),
                           fixtureSHA256=sha(read_regular(pathlib.Path(__file__))), sourceDigest=manifest['sourceDigest'])
        plan_id, job_id, vm_id = [canonical_uuid(native[key]) for key in ('planID', 'operationID', 'vmID')]
        binding_raw = read_regular(self.native / 'first-binding.json')
        require(sha(binding_raw) == native['firstBindingSHA256'], 'recorded original binding bytes differ')
        binding = strict_json(binding_raw)
        native_plan = strict_json(read_regular(self.native / 'creation-plan-response.json'))
        native_result = strict_json(read_regular(self.native / 'result-complete.json'))
        require(binding['planID'] == plan_id and binding['operationID'] == job_id and
                binding['resource'] == {'providerID': 'libvirt', 'connectionID': URI, 'kind': 'vm', 'resourceUUID': vm_id}
                and binding['firmware']['nvram']['path'] == native['firstPath']
                and binding['observedFingerprint'] == native['firstObservedFingerprint'], 'native binding identity differs')
        self.baseline = self.journal()
        self.save_journal('journal-before.json', self.baseline)
        require(not self.baseline['locks'], 'unexpected unresolved locks before read fixture')
        require(all(strict_json(row[2])['state'] in ('succeeded', 'failed', 'canceled', 'partial')
                    for row in self.baseline['jobs']), 'active/uncertain job exists')
        records = [row[2] for row in self.baseline['metadata'] if row[:2] == ('creation-nvram-declaration', plan_id)]
        require(records == [binding_raw], 'durable binding is not the exact first record')
        self.guest_before = self.guests()
        prior_guests = strict_json(read_regular(self.native / 'baseline-guests.json'))
        require(len(prior_guests) == 6 and len(self.guest_before) == 7 and
                set(self.guest_before) == set(prior_guests) | {vm_id}, 'guest inventory differs from retained native fixture')
        for identifier, expected in prior_guests.items():
            require(sha(self.guest_before[identifier][1]) == expected, 'prior native XML differs')
        require(self.guest_before[vm_id][1] == read_regular(self.native / 'native-A.xml') and
                sha(self.guest_before[vm_id][1]) == EXISTING_XML_SHA, 'restored native A differs')
        for identifier, (state, xml) in self.guest_before.items():
            self.save('before-' + identifier + '.state', state)
            self.save('before-' + identifier + '.xml', xml)
        runtime = self.runtime(manifest)
        require(runtime == native['runtime'], 'installed/running binaries or coordinator changed since native fixture')
        current_pool = self.virsh('pool-dumpxml', POOL_ID)
        self.report['poolBeforeUI'] = compare_pool_observation(pool_before, current_pool)
        self.save('pool-current-before-ui.xml', current_pool)
        operation, operation_raw = self.cli('operation', 'show', job_id)
        require(operation['data'] == native_result['data']['operation'] and operation['data']['state'] == 'succeeded',
                'existing reconciled operation is not the same completed job')
        self.save('operation-response.json', operation_raw)
        envelopes = {}
        for command, identifier, artifact in (('plan show', plan_id, 'plan'), ('vm creation result', job_id, 'result')):
            response, raw = self.cli(*command.split(), identifier)
            table_response, table = self.cli(*command.split(), identifier, output='table')
            ndjson, _ = self.cli(*command.split(), identifier, output='ndjson')
            require(response == table_response == ndjson, 'CLI formats disagree')
            require(response == (native_plan if artifact == 'plan' else native_result), 'stored native response changed')
            self.save(artifact + '-response.json', raw)
            envelopes[artifact] = response
            terminal = Terminal(self, artifact)
            try:
                terminal.observe(command, identifier, table)
            finally:
                terminal.close()
                self.save(artifact + '.ansi', bytes(terminal.raw))
                self.save(artifact + '-pages.json', terminal.pages)
            require(terminal.pages, 'no verified current TUI pages')
            self.report[artifact + 'TUI'] = {'verifiedPages': len(terminal.pages), 'ansiSHA256': sha(bytes(terminal.raw)),
                                            'allPagesMatchCurrentCLIEnvelope': True}
        plan, result = envelopes['plan']['data'], envelopes['result']['data']
        require(plan['planID'] == plan_id and plan['planDigest'] == native['planDigest']
                and plan['review']['nvramDeclarationVersion'] == 1
                and plan['review']['nvramInitializationVerified'] is False
                and plan['inputDigest'] == binding['inputDigest'] and plan['review']['target']['spec']['uuid'] == vm_id,
                'plan version/digest/native identity differs')
        require(result['complete'] is True and result['nvramDeclarationBound'] is True
                and result['nvramDeclarationStatus'] == 'declaration-bound'
                and result['nvramInitializationVerified'] is False and result['nvramDeclaration'] == binding
                and result['operation']['operationID'] == job_id and result['operation']['planID'] == plan_id
                and result['operation']['state'] == 'succeeded' and result['receipt']['vmID'] == vm_id
                and result['receipt']['planID'] == plan_id and result['receipt']['operationID'] == job_id
                and result['receipt']['defined'] is True and result['receipt']['volumesVerified'] is True,
                'creation result conflates or changes declaration/completion identity')
        require(all(result[key] is False for key in ('guestBootVerified', 'setupVerified', 'connectivityVerified')),
                'unexpected guest qualification flags')
        require(self.runtime(manifest) == runtime, 'runtime changed during observation')
        self.report.update(planID=plan_id, planDigest=plan['planDigest'], operationID=job_id, vmID=vm_id,
                           firstPath=native['firstPath'], firstBindingSHA256=sha(binding_raw),
                           firstObservedFingerprint=binding['observedFingerprint'], runtime=runtime,
                           sharedCLIAndCurrentTUIEnvelopeVerified=True, sameExistingOperationObserved=True,
                           noReconcileSubmitted=True)

    def finish(self):
        if self.baseline is not None:
            try:
                after = self.journal()
                self.save_journal('journal-after.json', after)
                require(after == self.baseline, 'journal rows changed during read-only UI observation')
                self.report['allJournalRowsUnchanged'] = True
                self.report['noJournalTriggersObserved'] = True
                self.report['journalCounts'] = {key: len(rows) for key, rows in after.items()}
            except Exception as error:
                self.report['failures'].append('final journal: ' + str(error)[:500])
        if self.guest_before is not None:
            try:
                after = self.guests()
                for identifier, (state, xml) in after.items():
                    self.save('after-' + identifier + '.state', state)
                    self.save('after-' + identifier + '.xml', xml)
                require(after == self.guest_before, 'guest XML/state/inventory changed during UI observation')
                self.report['allStoppedGuestXMLAndStatesUnchanged'] = True
                self.report['guestXMLSHA256'] = {identifier: sha(value[1]) for identifier, value in after.items()}
            except Exception as error:
                self.report['failures'].append('final guest observations: ' + str(error)[:500])
        if self.pool_reference is not None:
            try:
                current = self.virsh('pool-dumpxml', POOL_ID)
                self.report['poolAfterUI'] = compare_pool_observation(self.pool_reference, current)
                self.save('pool-current-after-ui.xml', current)
                self.report['poolConfigurationBytesUnchanged'] = True
            except Exception as error:
                self.report['failures'].append('final pool observation: ' + str(error)[:500])
        self.report['status'] = 'passed' if (not self.report['failures'] and self.report.get('sharedCLIAndCurrentTUIEnvelopeVerified')
            and self.report.get('sameExistingOperationObserved') and self.report.get('poolConfigurationBytesUnchanged')) else 'inconclusive'
        self.save('report.json', self.report)
        print(json.dumps(self.report, sort_keys=True), flush=True)
        return 0 if self.report['status'] == 'passed' else 1


def self_test():
    """Pure local synthetic seams; no native executable, database or PTY used."""
    import unittest

    class Synthetic(unittest.TestCase):
        @staticmethod
        def pool_xml(statistics=POOL_BEFORE):
            return ("<pool type='dir'>\n  <name>" + POOL_NAME + "</name>\n  <uuid>" + POOL_ID + "</uuid>\n" +
                    ''.join("  <" + name + " unit='bytes'>" + str(statistics[name]) + "</" + name + ">\n"
                            for name in ('capacity', 'allocation', 'available')) +
                    "  <source/>\n  <target><path>" + POOL_PATH + "</path><permissions><mode>0755</mode>" +
                    "<owner>107</owner><group>107</group><label>synthetic-label</label></permissions></target>\n</pool>\n").encode()

        @staticmethod
        def native_failure():
            return {'recipe': 'nvram-binding-native-002', 'status': 'uncertain-preserve-resources',
                'failures': ['existing pool XML changed'], 'lastStage': 'final A reconciliation',
                'checks': list(NATIVE_CHECKS), 'revision': 'a' * 40, 'deploymentManifestSHA256': 'b' * 64,
                'planID': EXISTING_PLAN, 'operationID': EXISTING_OPERATION, 'vmID': EXISTING_VM,
                'finalNativeXMLSHA256': EXISTING_XML_SHA, 'remainingLocks': [], 'scopedTriggerRemoved': True,
                'allSixEarlierStoppedXMLPreserved': True, 'priorJournalRowsPreserved': True,
                'priorDeclaredDiskMetadataPreserved': True, 'selectedProbeSourceAndDiskHashesPreserved': True,
                'olderLargeDiskBytesRehashed': False, 'guestStarted': False, 'nvramInitializationVerified': False,
                'completeCaptureVerified': False, 'independentRecoveryVerified': False,
                'guestBootVerified': False, 'nativeCallCountsObserved': False}

        def test_pool_allows_only_numeric_usage_drift(self):
            before, after = self.pool_xml(), self.pool_xml(POOL_AFTER)
            compared = compare_pool_observation(before, after)
            self.assertEqual(compared['statistics'], POOL_AFTER)
            self.assertEqual(compared['configurationBytesSHA256'], sha(pool_observation(before)[1]))
            self.assertNotEqual(sha(before), sha(after))
            self.assertEqual(POOL_AFTER['allocation'] - POOL_BEFORE['allocation'], 52776960)
            current = {**POOL_AFTER, 'allocation': POOL_AFTER['allocation'] + 4096,
                       'available': POOL_AFTER['available'] - 4096}
            self.assertEqual(compare_pool_observation(before, self.pool_xml(current))['statistics'], current)

        def test_pool_rejects_configuration_or_capacity_changes(self):
            original = self.pool_xml()
            altered = {
                'type': original.replace(b"type='dir'", b"type='fs'"),
                'name': original.replace(POOL_NAME.encode(), b'other-pool', 1),
                'uuid': original.replace(POOL_ID.encode(), EXISTING_VM.encode()),
                'path': original.replace(POOL_PATH.encode(), b'/tmp/elsewhere'),
                'permission': original.replace(b'<mode>0755', b'<mode>0777'),
                'owner': original.replace(b'<owner>107', b'<owner>0'),
                'label': original.replace(b'synthetic-label', b'changed-label'),
                'unknown source config': original.replace(b'<source/>', b'<source><dir path="/other"/></source>'),
                'comment': original.replace(b'<source/>', b'<source/><!--different-->'),
                'formatting': original.replace(b'  <source/>', b'    <source/>'),
                'capacity': self.pool_xml({**POOL_BEFORE, 'capacity': POOL_BEFORE['capacity'] + 4096,
                                           'available': POOL_BEFORE['available'] + 4096}),
            }
            for label, changed in altered.items():
                with self.subTest(label=label), self.assertRaises((RuntimeError, ET.ParseError)):
                    compare_pool_observation(original, changed)

        def test_pool_rejects_duplicate_and_malformed_statistics(self):
            original = self.pool_xml()
            allocation = str(POOL_BEFORE['allocation']).encode()
            altered = {
                'duplicate allocation': original.replace(b'<source/>', b"<allocation unit='bytes'>1</allocation><source/>"),
                'duplicate capacity': original.replace(b'<source/>', b"<capacity unit='bytes'>1</capacity><source/>"),
                'nested available': original.replace(b'<source/>', b"<source><available unit='bytes'>1</available></source>"),
                'missing available': re.sub(rb'  <available[^>]*>[0-9]+</available>\n', b'', original),
                'unknown unit': original.replace(b"unit='bytes'", b"unit='KiB'", 1),
                'missing unit': original.replace(b" unit='bytes'", b'', 1),
                'unexpected attribute': original.replace(b"unit='bytes'", b"unit='bytes' extra='x'", 1),
                'negative': original.replace(allocation, b'-1'),
                'float': original.replace(allocation, b'1.5'),
                'exponent': original.replace(allocation, b'1e3'),
                'empty': original.replace(allocation, b''),
                'numeric whitespace': original.replace(allocation, b' ' + allocation),
                'overflow': original.replace(allocation, b'18446744073709551616'),
                'sum': original.replace(allocation, str(POOL_BEFORE['allocation'] + 1).encode()),
                'zero capacity': self.pool_xml({'capacity': 0, 'allocation': 0, 'available': 0}),
                'DTD': b'<!DOCTYPE pool [<!ENTITY x "ignored">]>' + original,
                'truncated': original[:-8],
                'oversized': original + b' ' * 65536,
            }
            for label, changed in altered.items():
                with self.subTest(label=label), self.assertRaises((RuntimeError, ET.ParseError)):
                    compare_pool_observation(original, changed)

        def test_saved_pool_command_requires_exact_read(self):
            command = {'stage': 'preflight', 'arguments': ['/usr/bin/virsh', '--readonly', '-c', URI, 'pool-dumpxml', POOL_ID],
                       'exitCode': 0, 'stdout': self.pool_xml().decode(), 'stderr': ''}
            self.assertEqual(saved_pool_xml(json.dumps(command).encode(), 'preflight'), self.pool_xml())
            altered = {
                'wrong stage': {**command, 'stage': 'unrelated'},
                'wrong action': {**command, 'arguments': ['/usr/bin/virsh', '-c', URI, 'pool-refresh', POOL_ID]},
                'missing readonly': {**command, 'arguments': ['/usr/bin/virsh', '-c', URI, 'pool-dumpxml', POOL_ID]},
                'wrong pool': {**command, 'arguments': command['arguments'][:-1] + [EXISTING_VM]},
                'error code': {**command, 'exitCode': 1},
                'stderr': {**command, 'stderr': 'partial observation'},
                'unknown field': {**command, 'ignored': True},
                'nontext stdout': {**command, 'stdout': {}},
            }
            for label, changed in altered.items():
                with self.subTest(label=label), self.assertRaises((RuntimeError, ET.ParseError)):
                    saved_pool_xml(json.dumps(changed).encode(), 'preflight')

        def test_only_exact_known_native_failure_is_eligible(self):
            native = self.native_failure()
            validate_native_failure(native, 'a' * 40, 'b' * 64)
            altered = {
                'passed instead of retained failure': {**native, 'status': 'passed', 'failures': []},
                'wrong failure': {**native, 'failures': ['first binding missing']},
                'additional failure': {**native, 'failures': native['failures'] + ['cleanup failed']},
                'wrong stage': {**native, 'lastStage': 'injected receipt-publication failure'},
                'missing check': {**native, 'checks': NATIVE_CHECKS[:-1]},
                'reordered checks': {**native, 'checks': list(reversed(NATIVE_CHECKS))},
                'extra check': {**native, 'checks': NATIVE_CHECKS + ['invented completion']},
                'wrong deployment': {**native, 'deploymentManifestSHA256': 'c' * 64},
                'wrong revision': {**native, 'revision': 'c' * 40},
                'wrong operation': {**native, 'operationID': EXISTING_VM},
                'wrong plan': {**native, 'planID': EXISTING_VM},
                'wrong VM': {**native, 'vmID': EXISTING_PLAN},
                'wrong native XML': {**native, 'finalNativeXMLSHA256': 'd' * 64},
                'remaining lock': {**native, 'remainingLocks': [['resource', EXISTING_OPERATION]]},
                'nonboolean preservation': {**native, 'scopedTriggerRemoved': 1},
            }
            for field, value in native.items():
                if isinstance(value, bool):
                    altered['flipped ' + field] = {**native, field: not value}
            for label, changed in altered.items():
                with self.subTest(label=label), self.assertRaises(RuntimeError):
                    validate_native_failure(changed, 'a' * 40, 'b' * 64)
            for field in native:
                changed = dict(native)
                del changed[field]
                with self.subTest(missing=field), self.assertRaises((KeyError, RuntimeError)):
                    validate_native_failure(changed, 'a' * 40, 'b' * 64)

        def test_old_page_cannot_match_current_screen(self):
            screen = Screen()
            screen.feed(b'\x1b[2J\x1b[Hdeclaration-bound\r\nold-path')
            screen.feed(b'\x1b[Hpending\x1b[K\r\nnew-path\x1b[J')
            self.assertNotIn('declaration-bound', screen.text())
            self.assertNotIn('old-path', screen.text())
            self.assertEqual(screen.lines()[:2], ['pending', 'new-path'])

        def test_incremental_sequences_unicode_and_cursor(self):
            screen = Screen()
            for part in (b'\x1b[2', b'J\x1b[1;', b'1Hreview \xe2', b'\x80\x94 yes\x1b[', b'2;1Hpath'):
                screen.feed(part)
            self.assertTrue(screen.complete())
            self.assertEqual(screen.lines()[:2], ['review — yes', 'path'])
            screen.feed(b'\x1b[1A\rno\x1b[K')
            self.assertEqual(screen.lines()[0], 'no')

        def test_unknown_or_incomplete_controls_fail_closed(self):
            for raw in (b'\x1b]0;old-success\x07', b'\x1b[999z', b'\x00', '宽'.encode()):
                with self.assertRaises((RuntimeError, UnicodeError)):
                    Screen().feed(raw)
            screen = Screen()
            screen.feed(b'\x1b[')
            self.assertFalse(screen.complete())

        def test_alt_screen_and_wrapping_replace_cells(self):
            screen = Screen()
            screen.feed(b'x' * 80 + b'\r\nnew')
            self.assertEqual(screen.lines()[:2], ['x' * 80, 'new'])
            screen.feed(b'\x1b[?1049h')
            self.assertFalse(any(screen.lines()))

        def test_duplicate_json_and_uuid_refused(self):
            with self.assertRaises(RuntimeError):
                strict_json(b'{"complete":true,"complete":false}')
            with self.assertRaises((RuntimeError, ValueError)):
                canonical_uuid('00000000-0000-0000-0000-000000000000')

        def test_child_timeout_and_output_limit(self):
            start = time.monotonic()
            code, out, err, failure = collect_child([sys.executable, '-c', 'import time; time.sleep(10)'], os.environ, timeout=.05)
            self.assertIsNotNone(failure)
            self.assertIsNotNone(code)
            self.assertLess(time.monotonic() - start, 4)
            code, out, err, failure = collect_child([sys.executable, '-c', 'import os; os.write(1, b"x"*1000)'], os.environ, limit=100)
            self.assertIsNotNone(failure)
            self.assertEqual(len(out) + len(err), 100)

        def test_child_stderr_and_nonzero_exit_preserved(self):
            code, out, err, failure = collect_child([sys.executable, '-c',
                'import os; os.write(1, b"partial"); os.write(2, b"exact error\\n"); raise SystemExit(7)'], os.environ)
            self.assertIsNone(failure)
            self.assertEqual((code, out, err), (7, b'partial', b'exact error\n'))

        def test_cleanup_signals_only_its_unreaped_child(self):
            from unittest import mock
            child = mock.Mock()
            child.poll.return_value = None
            child.wait.side_effect = [subprocess.TimeoutExpired('synthetic', 1), -9]
            stop_child(child)
            self.assertEqual(child.method_calls, [mock.call.poll(), mock.call.terminate(),
                mock.call.wait(timeout=1), mock.call.kill(), mock.call.wait(timeout=2)])
            child.reset_mock(side_effect=True)
            child.poll.return_value = 0
            stop_child(child)
            self.assertEqual(child.method_calls, [mock.call.poll()])

        def test_screen_rejects_stale_current_page_after_erase(self):
            screen = Screen()
            screen.feed(b'\x1b[H"nvramInitializationVerified": false\x1b[K')
            old = screen.text()
            screen.feed(b'\x1b[HRequest in progress\x1b[J')
            self.assertIn('"nvramInitializationVerified": false', old)
            self.assertNotIn('"nvramInitializationVerified": false', screen.text())

        def test_private_xdg_is_only_for_virmill(self):
            from unittest import mock
            with mock.patch.dict(os.environ, {'XDG_RUNTIME_DIR': '/fixture/host',
                                             'VIRSH_DEBUG': '0', 'VIRSH_LOG_FILE': '/fixture/log'}):
                recipe = Recipe(argparse.Namespace(revision='a' * 40, deployment_sha256='b' * 64))
            recipe.env['XDG_RUNTIME_DIR'] = '/fixture/private'
            recipe.save = lambda *_args: None
            environments = []

            def child(_arguments, environment, _timeout):
                environments.append(dict(environment))
                return 0, b'{}', b'', None

            with mock.patch.dict(globals(), {'collect_child': child}):
                recipe.command(['/usr/bin/virmill', 'version'])
                recipe.command(['/usr/bin/systemctl', '--user', 'show'])
                recipe.command(['/usr/bin/virsh', '--readonly', 'list'])
            self.assertEqual([value['XDG_RUNTIME_DIR'] for value in environments],
                             ['/fixture/private', '/fixture/host', '/fixture/host'])
            for value in environments:
                self.assertNotIn('VIRSH_DEBUG', value)
                self.assertNotIn('VIRSH_LOG_FILE', value)

    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Synthetic))
    return 0 if result.wasSuccessful() else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--revision')
    parser.add_argument('--deployment-sha256')
    parser.add_argument('--self-test', action='store_true')
    arguments = parser.parse_args()
    if arguments.self_test:
        require(arguments.revision is None and arguments.deployment_sha256 is None, 'self-test takes no deployment arguments')
        return self_test()
    require(arguments.revision is not None and re.fullmatch('[a-f0-9]{40}', arguments.revision), 'revision must be 40 lowercase hex characters')
    require(arguments.deployment_sha256 is not None and re.fullmatch('[a-f0-9]{64}', arguments.deployment_sha256), 'deployment digest must be 64 lowercase hex characters')
    require(os.getuid() != 0 and pathlib.Path.home() == ROOT.parents[1], 'only the designated ordinary-user run root is supported')
    os.umask(0o077)
    recipe = Recipe(arguments)
    recipe.output.mkdir(mode=0o700, exist_ok=False)

    def interrupted(_number, _frame):
        raise KeyboardInterrupt('operator interrupted read-only UI observation')

    previous = {number: signal.signal(number, interrupted) for number in (signal.SIGINT, signal.SIGTERM)}
    try:
        recipe.inspect()
    except BaseException as error:
        recipe.report['failures'].append(str(error)[:500] or type(error).__name__)
    finally:
        for number, handler in previous.items():
            signal.signal(number, handler)
    return recipe.finish()


if __name__ == '__main__':
    sys.exit(main())
