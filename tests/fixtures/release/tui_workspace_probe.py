#!/usr/bin/env python3
"""Parent-operated read-only real-PTY workspace walkthrough.

Run --self-test locally; it opens no PTY, service or network connection.
Native execution requires --execute-disposable, UID 1000 and virmill-test(.home).
Use the existing/default coordinator environment. No fake/render-local mode is
provided: passing requires real libvirt-labelled CLI inventory and matching TUI
rows/details. This observes software navigation, not full UX/hardware acceptance.

Example (parent executes only on the authorized disposable machine):
  python3 -B tui_workspace_probe.py --execute-disposable \
    --binary /usr/bin/virmill --binary-sha256 REVIEWED_SHA256 \
    --output /home/virmill-test/virmill-tests/RUN/workspace-001

All output is retained on failure. Never reuse the output directory. The script
only invokes version/vm list/operation list and the TUI. Its key sequence opens
a lifecycle plan and its confirmation, then cancels without applying. Durable
plan-preview records are expected; no guest operation may be created. The file
browser reads existing media directory listings and fills a selected file path in
the import form, then cancels. It never submits import, extracts or changes source
media. Directory metadata is compared before/after.
"""

import argparse
import codecs
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pty
import re
import selectors
import signal
import socket
import stat
import struct
import subprocess
import sys
import termios
import time
import tempfile
import unicodedata
import unittest
from unittest import mock
import uuid

MAX_OUTPUT = 8 << 20
MAX_TRANSCRIPT = 4 << 20
OUTPUT_ROOT = Path('/home/virmill-test/virmill-tests')


def require(ok, message):
    if not ok:
        raise RuntimeError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def strict_json(raw):
    require(len(raw) <= MAX_OUTPUT, 'JSON output exceeds bound')

    def pairs(items):
        out = {}
        for key, value in items:
            require(key not in out, 'duplicate JSON field')
            out[key] = value
        return out

    def invalid(_):
        raise ValueError('non-finite JSON')

    return json.loads(raw, object_pairs_hook=pairs, parse_constant=invalid)


def canonical_uuid(value):
    require(isinstance(value, str) and str(uuid.UUID(value)) == value and
            uuid.UUID(value).int != 0, 'noncanonical UUID')
    return value


def canonical_path(value):
    path = Path(value)
    require(path.is_absolute() and str(path) == value and
            os.path.normpath(value) == value and value != '/' and
            not any(unicodedata.category(c)[0] == 'C' for c in value),
            'canonical absolute nonroot path required')
    for ancestor in reversed(path.parents):
        require(stat.S_ISDIR(ancestor.lstat().st_mode), 'symlink/non-directory path ancestor')
    return path


def generation(st):
    return [st.st_dev, st.st_ino, st.st_size, st.st_mtime_ns, st.st_ctime_ns]


def inventory(data, connection):
    require(isinstance(data, list) and 0 < len(data) <= 1024, 'nonempty bounded native VM list required')
    out = {}
    for vm in data:
        require(isinstance(vm, dict) and isinstance(vm.get('key'), dict), 'native VM shape missing')
        key = vm['key']
        identity = canonical_uuid(key.get('resourceUUID'))
        require(key.get('providerID') == 'libvirt' and key.get('connectionID') == connection and
                key.get('kind') == 'vm' and identity not in out, 'native VM key differs or duplicates')
        require(isinstance(vm.get('name'), str) and vm['name'] and
                isinstance(vm.get('state'), str) and vm['state'] and
                re.fullmatch('[0-9a-f]{64}', vm.get('fingerprint', '')), 'native VM identity fields missing')
        # Do not retain XML/source paths. Compare their exact returned bytes by digest.
        out[identity] = {'name': vm['name'], 'state': vm['state'],
                         'fingerprint': vm['fingerprint'],
                         'persistentXMLSHA256': sha(vm.get('persistentXML', '').encode()),
                         'liveXMLSHA256': sha(vm.get('liveXML', '').encode())}
    return out


def jobs(data):
    require(isinstance(data, list) and len(data) <= 10000, 'bounded operation list required')
    out = {}
    for job in data:
        identity = canonical_uuid(job.get('operationID'))
        require(identity not in out and isinstance(job.get('state'), str), 'invalid/duplicate job')
        out[identity] = job['state']
    return out


def filter_for(identity, vms):
    name = vms[identity]['name']
    require(0 < len(name) <= 255 and name.isascii() and
            all(c.isalnum() or c in ' ._-' for c in name),
            'select a VM with an ASCII printable fixture name for this keyboard probe')
    for length in range(min(3, len(name)), min(32, len(name)) + 1):
        query = name[:length]
        if [key for key, vm in vms.items() if query.casefold() in
                (vm['name'] + ' ' + key + ' ' + vm.get('state', '')).casefold()] == [identity]:
            return query
    raise RuntimeError('selected fixture name lacks a unique bounded name filter')


def media_listing(path):
    """Bounded metadata only; never read media bytes or follow symlinks."""
    path = canonical_path(str(path))
    require(stat.S_ISDIR(path.lstat().st_mode), 'media directory must be ordinary')
    out = {}
    with os.scandir(path) as entries:
        for entry in entries:
            require(len(out) < 4096, 'media directory exceeds probe listing bound')
            item = entry.stat(follow_symlinks=False)
            out[entry.name] = {'generation': generation(item), 'mode': item.st_mode}
    return out


def media_folder(entries):
    candidates = [name for name, info in entries.items() if
                  stat.S_ISDIR(info['mode']) and 0 < len(name) <= 40 and
                  name[0] != '.' and name.isascii() and
                  all(c.isalnum() or c in ' ._-' for c in name)]
    return sorted(candidates, key=lambda name: (name.casefold(), name))[0] if candidates else None


def media_file(listings):
    for directory, entries in listings.items():
        candidates = [name for name, info in entries.items() if
                      stat.S_ISREG(info['mode']) and 0 < len(name) <= 64 and
                      name[0] != '.' and name.isascii() and
                      all(c.isalnum() or c in ' ._-' for c in name) and
                      not any(name.casefold() in other.casefold() for other in entries if other != name)]
        if candidates:
            return directory / sorted(candidates, key=lambda name: (len(name) > 40, name.casefold(), name))[0]
    raise RuntimeError('an existing uniquely filterable ordinary media file is required')


class Screen:
    """Current VT text cells; never match old transcript content as a screen.

    Supports the bounded CSI subset emitted by Bubble Tea. Unknown terminal
    controls and ambiguous-width text fail rather than fabricate a screen.
    """
    def __init__(self, columns, rows):
        self.decoder = codecs.getincrementaldecoder('utf-8')('strict')
        self.escape = ''
        self.updates = 0
        self.resize(columns, rows)

    def resize(self, columns, rows):
        require((columns, rows) in ((80, 24), (120, 36)), 'unsupported probe size')
        self.columns, self.rows = columns, rows
        self.clear()

    def clear(self):
        self.cells = [[' '] * self.columns for _ in range(self.rows)]
        self.row = self.column = 0
        self.wrap = False

    def newline(self):
        self.row += 1
        if self.row >= self.rows:
            self.cells.pop(0)
            self.cells.append([' '] * self.columns)
            self.row = self.rows - 1
        self.wrap = False

    def csi(self, sequence):
        final, raw = sequence[-1], sequence[2:-1]
        require(re.fullmatch(r'\??[0-9;:]*', raw) is not None, 'unsupported CSI parameters')
        if raw.startswith('?'):
            require(final in 'hl', 'unsupported private CSI')
            for value in raw[1:].split(';'):
                require(value in ('25', '1002', '1003', '1004', '1006', '2004', '1049'),
                        'unknown private terminal mode')
                if value == '1049':
                    self.clear()
            return
        if final == 'm':
            return
        values = [int(v or '0') for v in raw.split(';')]
        require(len(values) <= 2 and max(values) <= 10000, 'excessive CSI arguments')
        n = values[0] or 1
        if final in 'Hf':
            self.row = min(self.rows - 1, n - 1)
            self.column = min(self.columns - 1, (values[1] or 1) - 1 if len(values) == 2 else 0)
        elif final in 'ABCD':
            if final == 'A': self.row = max(0, self.row - n)
            if final == 'B': self.row = min(self.rows - 1, self.row + n)
            if final == 'C': self.column = min(self.columns - 1, self.column + n)
            if final == 'D': self.column = max(0, self.column - n)
        elif final == 'G':
            self.column = min(self.columns - 1, n - 1)
        elif final == 'K':
            require(values[0] in (0, 1, 2), 'unsupported erase line')
            start = 0 if values[0] in (1, 2) else self.column
            end = self.column + 1 if values[0] == 1 else self.columns
            self.cells[self.row][start:end] = [' '] * (end - start)
        elif final == 'J':
            require(values[0] in (0, 2), 'unsupported erase display')
            if values[0] == 2:
                self.cells = [[' '] * self.columns for _ in range(self.rows)]
            else:
                self.cells[self.row][self.column:] = [' '] * (self.columns - self.column)
                for row in range(self.row + 1, self.rows):
                    self.cells[row] = [' '] * self.columns
        else:
            raise RuntimeError('unsupported terminal CSI ' + repr(sequence))
        self.wrap = False

    def feed(self, raw):
        self.updates += 1
        for char in self.decoder.decode(raw):
            if self.escape:
                self.escape += char
                require(len(self.escape) <= 128, 'terminal escape exceeds bound')
                if len(self.escape) == 2:
                    require(char == '[', 'unsupported non-CSI escape')
                elif '@' <= char <= '~':
                    self.csi(self.escape)
                    self.escape = ''
                continue
            if char == '\x1b': self.escape = char
            elif char == '\r': self.column, self.wrap = 0, False
            elif char == '\n': self.newline()
            elif char == '\b': self.column, self.wrap = max(0, self.column - 1), False
            elif char == '\t': self.column, self.wrap = min(self.columns - 1, (self.column // 8 + 1) * 8), False
            else:
                require(unicodedata.category(char)[0] != 'C' and not unicodedata.combining(char) and
                        unicodedata.east_asian_width(char) not in 'WF', 'unsupported control/ambiguous cell width')
                if self.wrap:
                    self.column = 0
                    self.newline()
                self.cells[self.row][self.column] = char
                if self.column == self.columns - 1: self.wrap = True
                else: self.column += 1

    def text(self):
        return '\n'.join(''.join(row).rstrip() for row in self.cells)

    def complete(self):
        return not self.escape and not self.decoder.getstate()[0]


def stop_child(process):
    # Only the owned unreaped CLI/TUI child; never a coordinator or VM.
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=2)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait(timeout=3)


class Runner:
    def __init__(self, args, directory, binary_fd):
        self.args, self.directory, self.binary_fd = args, directory, binary_fd
        self.commands, self.checks = [], []
        self.env = dict(os.environ, TERM='xterm-256color', NO_COLOR='1')

    def save(self, name, value):
        require('/' not in name and name not in ('.', '..'), 'invalid evidence name')
        path = self.directory / name
        data = value if isinstance(value, bytes) else (json.dumps(value, indent=2, ensure_ascii=True) + '\n').encode()
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        with os.fdopen(fd, 'wb') as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())

    def launch(self, arguments, **kwargs):
        return subprocess.Popen([self.args.binary, *arguments], executable=f'/proc/self/fd/{self.binary_fd}',
                                pass_fds=(self.binary_fd,), env=self.env, start_new_session=True, **kwargs)

    def cli(self, *arguments):
        argv = ['--connection', self.args.connection, '--output', 'json', '--non-interactive', *arguments]
        process = self.launch(argv, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        chunks = [bytearray(), bytearray()]
        selector = selectors.DefaultSelector()
        started = time.monotonic()
        failure = None
        try:
            for number, stream in enumerate((process.stdout, process.stderr)):
                selector.register(stream, selectors.EVENT_READ, number)
            while selector.get_map():
                require(time.monotonic() - started < 40, 'CLI output deadline')
                for event, _ in selector.select(.1):
                    block = os.read(event.fileobj.fileno(), 65536)
                    if not block:
                        selector.unregister(event.fileobj)
                    else:
                        require(sum(map(len, chunks)) + len(block) <= MAX_OUTPUT, 'CLI output bound')
                        chunks[event.data].extend(block)
            process.wait(timeout=2)
        except BaseException as error:
            failure = str(error)
            raise
        finally:
            stop_child(process)
            selector.close()
            process.stdout.close()
            process.stderr.close()
            self.commands.append({'argv': [self.args.binary, *argv], 'exitCode': process.returncode,
                                  'stdoutBytes': len(chunks[0]), 'stdoutSHA256': sha(chunks[0]),
                                  'stderrBytes': len(chunks[1]), 'stderrSHA256': sha(chunks[1]), 'failure': failure})
            self.save('commands.json', self.commands)
        require(process.returncode == 0 and not chunks[1], 'CLI failed or emitted diagnostics; raw diagnostics withheld')
        envelope = strict_json(chunks[0])
        require(isinstance(envelope, dict) and envelope.get('apiVersion') == 'virmill/v1' and
                envelope.get('error') is None and 'data' in envelope, 'CLI did not return successful service data')
        return envelope['data']


class Terminal:
    def __init__(self, runner, label, columns, rows):
        self.runner, self.label = runner, label
        self.screen, self.transcript, self.steps = Screen(columns, rows), bytearray(), []
        self.master, slave = pty.openpty()
        self.started = time.monotonic()
        self.process = None
        try:
            fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', rows, columns, 0, 0))
            self.process = runner.launch(['--connection', runner.args.connection], stdin=slave, stdout=slave, stderr=slave)
        except BaseException:
            os.close(self.master)
            raise
        finally:
            os.close(slave)
        self.selector = selectors.DefaultSelector()
        self.selector.register(self.master, selectors.EVENT_READ)

    def read(self, delay=.05):
        require(time.monotonic() - self.started < 180, 'PTY session deadline')
        for _, _ in self.selector.select(delay):
            try:
                block = os.read(self.master, 65536)
            except OSError as error:
                if error.errno == errno.EIO: return False
                raise
            if not block: return False
            require(len(self.transcript) + len(block) <= MAX_TRANSCRIPT, 'PTY transcript bound')
            self.transcript.extend(block)
            self.screen.feed(block)
        return True

    def wait(self, label, predicate, fresh=-1):
        deadline = time.monotonic() + 20
        matching_since = None
        while time.monotonic() < deadline:
            require(self.process.poll() is None, 'TUI exited before ' + label)
            require(self.read(), 'PTY closed before ' + label)
            if self.screen.updates > fresh and self.screen.complete() and predicate(self.screen.text()):
                matching_since = matching_since or time.monotonic()
                if time.monotonic() - matching_since >= .15:
                    self.steps.append({'label': label, 'size': [self.screen.columns, self.screen.rows],
                                       'screen': self.screen.text(), 'transcriptBytes': len(self.transcript)})
                    self.runner.save(self.label + '-screens.json', self.steps)
                    return self.screen.text()
            else:
                matching_since = None
        self.steps.append({'label': label + ' FAILED', 'screen': self.screen.text()})
        raise RuntimeError('current screen did not satisfy ' + label)

    def send(self, key):
        require(self.process.poll() is None, 'TUI is no longer running')
        previous = self.screen.updates
        require(os.write(self.master, key) == len(key), 'partial PTY key write')
        self.steps.append({'keysHex': key.hex()})
        return previous

    def resize(self, columns, rows):
        self.screen.resize(columns, rows)
        previous = self.screen.updates
        fcntl.ioctl(self.master, termios.TIOCSWINSZ, struct.pack('HHHH', rows, columns, 0, 0))
        self.process.send_signal(signal.SIGWINCH)
        return previous

    def close(self):
        stop_child(self.process)
        self.selector.close()
        os.close(self.master)
        self.runner.save(self.label + '.ansi', bytes(self.transcript))
        self.runner.save(self.label + '-screens.json', self.steps)


def walkthrough(runner, vms, selected, columns, rows, media_root, folder, selected_file):
    label = f'workspace-{columns}x{rows}'
    terminal = Terminal(runner, label, columns, rows)
    try:
        def page(text, title):
            return bool(re.search(r'\bVirmill\s*/\s*' + re.escape(title) + r'\b', text.split('\n')[0])) and runner.args.connection in text

        overview = lambda text: page(text, 'Overview') and 'Virtual machines' in text and any(vm['name'][:16] in text for vm in vms.values())
        vm_table = lambda text: page(text, 'VMs') and 'VM details' not in text and 'NAME' in text and 'STATE' in text and any(vm['name'][:16] in text for vm in vms.values())
        detail_page = lambda text: page(text, 'VMs') and 'VM details' in text and 'UUID:' in text
        terminal.wait('initial Overview and native VM summary', overview)
        terminal.wait('native VM table', vm_table, terminal.send(b'2'))
        # Separate writes model distinct operator keys; coalesced ESC sequences
        # and '/' + query must not accidentally become a different key event.
        require(len(vms) >= 2, 'two actual native VMs required to prove arrow selection movement')
        terminal.wait('Down selects second observed row', lambda text: vm_table(text) and f'2 of {len(vms)} selected' in text,
                      terminal.send(b'\x1b[B'))
        terminal.wait('Up restores first observed row', lambda text: vm_table(text) and f'1 of {len(vms)} selected' in text,
                      terminal.send(b'\x1b[A'))
        detail = terminal.wait('selected full native UUID', lambda text: detail_page(text) and any(identity in text for identity in vms), terminal.send(b'\r'))
        observed = [identity for identity in vms if identity in detail]
        require(len(observed) == 1, 'details did not select exactly one native VM')
        terminal.wait('Esc returns to native table', vm_table, terminal.send(b'\x1b'))
        query = filter_for(selected, vms)
        terminal.wait('search input has focus before typing', lambda text: vm_table(text) and 'Search:' in text and
                      'Enter Keep filter' in text,
                      terminal.send(b'/'))
        terminal.wait('name filter displays only selected native row', lambda text: vm_table(text) and
                      ('Search: ' + query) in text and vms[selected]['name'][:16] in text and '1 of 1 selected' in text,
                      terminal.send(query.encode('ascii')))
        # A visible focus change must occur before Enter can request details.
        terminal.wait('Enter keeps filter and returns row focus', lambda text: vm_table(text) and
                      '1 of 1 selected' in text and 'Enter Details' in text and 'Enter Keep filter' not in text,
                      terminal.send(b'\r'))
        terminal.wait('filtered native UUID details', lambda text: detail_page(text) and selected in text, terminal.send(b'\r'))
        terminal.wait('CPU and memory opens a labeled form for selected VM', lambda text:
                      'CPU and RAM' in text and 'Requested CPU cores' in text and 'Next boot' in text and vms[selected]['name'] in text,
                      terminal.send(b'e'))
        terminal.wait('Esc cancels labeled form without submitting', lambda text: detail_page(text) and selected in text,
                      terminal.send(b'\x1b'))
        # The review opens at its summary; exact plan identities follow below it.
        terminal.wait('Start creates only a review plan for the selected VM', lambda text:
                      'Nothing has been applied' in text and 'Enter Review & apply' in text and selected in text,
                      terminal.send(b's'))
        terminal.wait('Enter opens explicit confirmation without applying', lambda text:
                      'Confirm reviewed changes' in text,
                      terminal.send(b'\r'))
        terminal.wait('Esc returns to the complete review', lambda text:
                      'Enter Review & apply' in text and 'Confirm reviewed changes' not in text,
                      terminal.send(b'\x1b'))
        terminal.wait('Esc dismisses the unapplied plan', lambda text: detail_page(text) and selected in text,
                      terminal.send(b'\x1b'))
        terminal.wait('Esc leaves filtered details', vm_table, terminal.send(b'\x1b'))
        focused = terminal.wait('Tab focuses Details button for selected VM', lambda text:
                               vm_table(text) and '>[ Enter Details ]' in text,
                               terminal.send(b'\t'))
        # Walk the actual visible buttons, never activate a lifecycle button.
        # Their state-sensitive number/order may differ for a running VM.
        for index in range(8):
            if '>[ a More ]' in focused:
                break
            prior_button = re.search(r'>\[ [^\n]*? \]', focused)
            require(prior_button is not None, 'focused action button not visible')
            prior_button = prior_button.group(0)
            focused = terminal.wait(f'Right moves button focus {index + 1}', lambda text:
                                    vm_table(text) and '>[' in text and prior_button not in text,
                                    terminal.send(b'\x1b[C'))
        require('>[ a More ]' in focused, 'More button not reachable by keyboard')
        more_menu = lambda text: 'VMs / More tasks' in text and vms[selected]['name'] in text
        terminal.wait('Enter opens selected VM More menu with task groups', lambda text:
                      more_menu(text) and 'Power' in text and 'Advanced tools...' in text and
                      'Recovery and troubleshooting' not in text,
                      terminal.send(b'\r'))
        terminal.wait('Task search has focus before typing', lambda text:
                      more_menu(text) and re.search(r'Find (?:task|action):', text) is not None,
                      terminal.send(b'/'))
        terminal.wait('Plain CPU search finds the resource editor', lambda text:
                      more_menu(text) and re.search(r'Find (?:task|action): CPU', text) is not None and
                      re.search(r'CPU (?:and|&) (?:RAM|memory)', text, re.IGNORECASE) is not None,
                      terminal.send(b'CPU'))
        terminal.wait('First Esc clears task search and restores groups', lambda text:
                      more_menu(text) and 'Power' in text and
                      re.search(r'Find (?:task|action): CPU', text) is None,
                      terminal.send(b'\x1b'))
        terminal.wait('A opens clearly separated advanced tools', lambda text:
                      'VMs / Advanced tools' in text and vms[selected]['name'] in text,
                      terminal.send(b'A'))
        terminal.wait('Esc returns from advanced to common tasks', lambda text:
                      more_menu(text) and 'Advanced tools...' in text and
                      'VMs / Advanced tools' not in text,
                      terminal.send(b'\x1b'))
        terminal.wait('Second Esc closes More and restores selected row', lambda text:
                      vm_table(text) and vms[selected]['name'][:16] in text and '1 of 1 selected' in text,
                      terminal.send(b'\x1b'))
        terminal.wait('Esc clears filter and restores observed rows', lambda text: vm_table(text) and 'Search:' not in text and
                      f'1 of {len(vms)} selected' in text, terminal.send(b'\x1b'))
        picker = lambda text: re.search(r'(?:^|[│|])Choose a file[ \t]*$', text, re.MULTILINE) is not None and 'Esc' in text
        def choose_browser_entry(name, description):
            terminal.wait(description + ' filter focus', lambda text:
                          picker(text) and 'Find:' in text and 'Enter done' in text,
                          terminal.send(b'/'))
            terminal.wait(description + ' observed name filter', lambda text:
                          picker(text) and ('Find: ' + name) in text,
                          terminal.send(name.encode('ascii')))
            terminal.wait(description + ' filter finished', lambda text:
                          picker(text) and 'Enter open/select' in text and 'Enter done' not in text,
                          terminal.send(b'\r'))

        import_form = lambda text: re.search(r'Virmill\s+/ Import', text) is not None and not picker(text)
        # Import opens the file browser directly. A saved setup is offered first;
        # Start new never resumes or submits it.
        opened = terminal.wait('Import opens browser at home, or offers saved setup', lambda text:
                               'Continue your saved setup?' in text or
                               (picker(text) and str(media_root.parent) in text and str(media_root) not in text),
                               terminal.send(b'i'))
        if 'Continue your saved setup?' in opened:
            terminal.wait('Start new setup focused', lambda text: '> [ Start new setup ]' in text, terminal.send(b'\t'))
            terminal.wait('Start new setup opens browser at home, not fixture media', lambda text:
                          picker(text) and str(media_root.parent) in text and str(media_root) not in text,
                          terminal.send(b'\r'))
        choose_browser_entry(media_root.name, 'Choose test media folder explicitly')
        terminal.wait('Explicitly enter existing test media folder', lambda text:
                      picker(text) and str(media_root) in text, terminal.send(b'\r'))
        traversal_parent = media_root if folder else media_root.parent
        traversal_name = folder or media_root.name
        traversal_path = traversal_parent / traversal_name
        if folder is None:
            terminal.wait('Backspace opens observed home containing images folder', lambda text:
                          picker(text) and str(media_root.parent) in text and str(media_root) not in text and
                          'images/' in text, terminal.send(b'\x7f'))
        terminal.wait('Browser search receives focus', lambda text:
                      picker(text) and 'Find:' in text and 'Enter done' in text,
                      terminal.send(b'/'))
        terminal.wait('Browser filters an actual existing media folder', lambda text:
                      picker(text) and ('Find: ' + traversal_name) in text and traversal_name in text,
                      terminal.send(traversal_name.encode('ascii')))
        terminal.wait('Enter finishes browser filter before directory navigation', lambda text:
                      picker(text) and ('Find: ' + traversal_name) in text and 'Enter open/select' in text and
                      'Enter done' not in text, terminal.send(b'\r'))
        terminal.wait('Enter traverses actual media folder without choosing an image', lambda text:
                      picker(text) and str(traversal_path) in text,
                      terminal.send(b'\r'))
        terminal.wait('Backspace returns to observed parent directory', lambda text:
                      picker(text) and str(traversal_parent) in text and str(traversal_path) not in text,
                      terminal.send(b'\x7f'))
        # Choosing a file starts a read-only source description; general_sources_probe covers it.
        terminal.wait('Esc cancels browser and returns to import form', import_form, terminal.send(b'\x1b'))
        terminal.wait('Ctrl+O reopens browser from source field', picker, terminal.send(b'\x0f'))
        terminal.wait('Esc closes reopened browser without choosing a file', import_form, terminal.send(b'\x1b'))
        # An unused import form returns to the selected VM's task menu.
        terminal.wait('Esc returns from import form to VM tasks', more_menu, terminal.send(b'\x1b'))
        terminal.wait('Esc closes VM tasks back to VM workspace', vm_table,
                      terminal.send(b'\x1b'))
        terminal.wait('Jobs workspace has loaded observations', lambda text: page(text, 'Jobs') and
                      ('NAME' in text and 'STATE' in text or 'No resources to display.' in text) and
                      'Could not load' not in text, terminal.send(b'9'))
        terminal.wait('Overview after Jobs', overview, terminal.send(b'1'))
        new_size = (120, 36) if columns == 80 else (80, 24)
        terminal.wait('Overview redraw after resize', overview, terminal.resize(*new_size))
        terminal.wait('All tools exposes Enter and Back navigation', lambda text: 'All tools' in text and
                      'Enter' in text and 'Back' in text and 'Virtual machines' not in text and
                      'Advanced commands' not in text,
                      terminal.send(b':'))
        terminal.wait('Esc returns from All tools to Overview', overview, terminal.send(b'\x1b'))
        terminal.send(b'q')
        deadline = time.monotonic() + 5
        while terminal.process.poll() is None and time.monotonic() < deadline:
            if not terminal.read(): break
        require(terminal.process.wait(timeout=2) == 0, 'TUI did not exit cleanly')
        runner.checks.append({'case': label, 'status': 'passed', 'selectedVM': selected,
                              'arrowSelectedVM': observed[0], 'exitCode': 0, 'resizedTo': list(new_size),
                              'mediaFolderTraversed': str(traversal_path), 'sourceSelected': False,
                              'importSubmitted': False,
                              'advancedToolsSeparated': True})
    finally:
        terminal.close()


def parser():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument('--self-test', action='store_true')
    p.add_argument('--execute-disposable', action='store_true')
    p.add_argument('--binary')
    p.add_argument('--binary-sha256')
    p.add_argument('--output')
    p.add_argument('--connection', choices=('qemu:///system', 'qemu:///session'), default='qemu:///system')
    p.add_argument('--expect-vm')
    return p


def main():
    args = parser().parse_args()
    if args.self_test:
        require(not args.execute_disposable and not args.binary and not args.output,
                'self-test cannot be mixed with execution inputs')
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(ProbeTests)
        return 0 if unittest.TextTestRunner(verbosity=2).run(suite).wasSuccessful() else 1
    require(args.execute_disposable and args.binary and args.output, 'explicit disposable guard, binary and new output are required')
    require(socket.gethostname() in ('virmill-test', 'virmill-test.home') and
            os.getuid() == os.geteuid() == 1000, 'only the authorized ordinary-user disposable host may execute')
    binary, output = canonical_path(args.binary), canonical_path(args.output)
    require(output.is_relative_to(OUTPUT_ROOT) and output != OUTPUT_ROOT, 'output must be a new child of the approved test tree')
    fd = os.open(binary, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_uid in (0, 1000) and
                before.st_mode & 0o111 and not before.st_mode & 0o7022 and 4 <= before.st_size <= 256 << 20,
                'selected binary must be a bounded ordinary owner-controlled executable')
        with os.fdopen(os.dup(fd), 'rb') as stream:
            raw = stream.read((256 << 20) + 1)
        require(raw.startswith(b'\x7fELF') and len(raw) == before.st_size and
                generation(before) == generation(os.fstat(fd)), 'binary changed or is not native ELF')
        digest = sha(raw)
        if args.binary_sha256:
            require(re.fullmatch('[0-9a-f]{64}', args.binary_sha256) and digest == args.binary_sha256,
                    'binary SHA-256 differs from the explicit pin')
        os.umask(0o077)
        output.mkdir(mode=0o700)  # Exclusive; never remove or reuse earlier evidence.
        runner = Runner(args, output, fd)
        report = {'status': 'failed', 'scope': 'real PTY navigation and durable plan preview/cancel; no guest mutation, full UX or hardware claim',
                  'hostname': socket.gethostname(), 'uid': os.getuid(), 'binary': str(binary), 'binarySHA256': digest,
                  'binaryGeneration': generation(before), 'connection': args.connection,
                  'pythonVersion': sys.version, 'startedUTC': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
                  'guestMutationSubmitted': False, 'durablePlanPreviewsOnly': True, 'checks': runner.checks}
        runner.save('intent.json', report)
        baseline = prior_jobs = None
        media_before = {}
        try:
            report['version'] = runner.cli('version')
            baseline = inventory(runner.cli('vm', 'list'), args.connection)
            prior_jobs = jobs(runner.cli('operation', 'list'))
            runner.save('baseline.json', {'vms': baseline, 'jobs': prior_jobs})
            if args.expect_vm:
                selected = canonical_uuid(args.expect_vm)
                require(selected in baseline, 'explicit expected VM is absent from actual inventory')
                filter_for(selected, baseline)
            else:
                candidates = []
                for identity in sorted(baseline, key=lambda key: (baseline[key]['name'].casefold(), key)):
                    try:
                        filter_for(identity, baseline)
                        candidates.append(identity)
                    except RuntimeError:
                        pass
                require(candidates, 'no uniquely filterable actual native VM; supply --expect-vm after reviewing inventory')
                selected = candidates[0]
            media_root = Path.home() / 'images'
            root_entries = media_listing(media_root)
            media_before = {media_root: root_entries}
            folder = media_folder(root_entries)
            if folder:
                media_before[media_root / folder] = media_listing(media_root / folder)
            selected_file = media_file(media_before)
            report['mediaListingsBefore'] = {str(path): {'entries': len(entries),
                'metadataSHA256': sha(json.dumps(entries, sort_keys=True).encode())}
                for path, entries in media_before.items()}
            for size in ((80, 24), (120, 36)):
                walkthrough(runner, baseline, selected, *size, media_root, folder, selected_file)
            report['status'] = 'passed'
        except BaseException as error:
            report['error'] = type(error).__name__ + ': ' + str(error)
        finally:
            try:
                after = inventory(runner.cli('vm', 'list'), args.connection)
                after_jobs = jobs(runner.cli('operation', 'list'))
                report['nativeInventoryPreserved'] = baseline is not None and after == baseline
                report['noOperationCreated'] = prior_jobs is not None and set(after_jobs) == set(prior_jobs)
                report['jobStatesPreserved'] = prior_jobs is not None and after_jobs == prior_jobs
                report['mediaMetadataPreserved'] = all(
                    media_listing(path) == entries for path, entries in media_before.items()) if media_before else None
                report['binaryPreserved'] = generation(before) == generation(os.fstat(fd)) == generation(binary.lstat())
                runner.save('after.json', {'vms': after, 'jobs': after_jobs})
                failed_preservation = [k for k in ('nativeInventoryPreserved', 'noOperationCreated', 'jobStatesPreserved', 'binaryPreserved', 'mediaMetadataPreserved') if report[k] is False]
                require(not failed_preservation, 'preservation check failed: ' + ', '.join(failed_preservation))
            except BaseException as error:
                report['preservationError'] = type(error).__name__ + ': ' + str(error)
                report['status'] = 'failed'
            report['finishedUTC'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
            runner.save('report.json', report)
            print(json.dumps(report, sort_keys=True, ensure_ascii=True))
        return 0 if report['status'] == 'passed' else 1
    finally:
        os.close(fd)


class ProbeTests(unittest.TestCase):
    def test_picker_title_is_distinct_from_help_in_both_layouts(self):
        pattern = r'(?:^|[│|])Choose a file[ \t]*$'
        for screen in ('Choose a file\n/home/test/images', ' 1 Overview        │Choose a file\n 2 VMs             │/home/test/images', '                   |Choose a file   '):
            self.assertIsNotNone(re.search(pattern, screen, re.MULTILINE))
        self.assertIsNone(re.search(pattern, 'Ctrl+O Browse | Choose a file or type its path', re.MULTILINE))

    def test_current_screen_erases_historical_success(self):
        screen = Screen(80, 24)
        screen.feed(b'Overview Virtual machines\r\n11111111-2222-4333-8444-555555555555')
        self.assertIn('Virtual machines', screen.text())
        screen.feed(b'\x1b[H\x1b[2JFailure')
        self.assertNotIn('Virtual machines', screen.text())
        self.assertNotIn('11111111', screen.text())

    def test_fragmented_ansi_resize_and_sgr(self):
        screen = Screen(80, 24)
        for part in (b'\x1b[?104', b'9h\x1b[3;4H\x1b[32mVM', b's\x1b[0m'):
            screen.feed(part)
        self.assertEqual(''.join(screen.cells[2][3:6]), 'VMs')
        self.assertTrue(screen.complete())
        screen.resize(120, 36)
        self.assertNotIn('VMs', screen.text())
        self.assertEqual((len(screen.cells), len(screen.cells[0])), (36, 120))

    def test_unknown_terminal_control_refused(self):
        for value in (b'\x1b]52;c;clipboard\a', b'\x1b[?999h', b'\x1b[999999A', b'\x00'):
            with self.subTest(value=value), self.assertRaises(RuntimeError):
                Screen(80, 24).feed(value)

    def test_strict_json_and_native_provider_binding(self):
        for value in (b'{"data":[],"data":[]}', b'{"x":NaN}'):
            with self.assertRaises((ValueError, RuntimeError)):
                strict_json(value)
        vm = {'key': {'resourceUUID': '11111111-2222-4333-8444-555555555555', 'providerID': 'libvirt',
                      'connectionID': 'qemu:///system', 'kind': 'vm'}, 'name': 'probe', 'state': 'stopped',
              'fingerprint': 'a' * 64, 'persistentXML': '<domain/>'}
        self.assertEqual(len(inventory([vm], 'qemu:///system')), 1)
        vm['key']['providerID'] = 'mock'
        with self.assertRaises(RuntimeError): inventory([vm], 'qemu:///system')

    def test_filter_does_not_choose_shared_prefix(self):
        values = {'a': {'name': 'probe-one'}, 'b': {'name': 'probe-two'}}
        self.assertEqual(filter_for('a', values), 'probe-o')
        with self.assertRaises(RuntimeError): filter_for('a', {'a': {'name': 'same'}, 'b': {'name': 'same'}})

    def test_media_listing_does_not_follow_or_read_sources(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'samples').mkdir()
            (root / 'media.ova').write_bytes(b'fixture source')
            (root / 'link').symlink_to(root / 'samples', target_is_directory=True)
            entries = media_listing(root)
            self.assertEqual(media_folder(entries), 'samples')
            self.assertEqual(media_file({root: entries}), root / 'media.ova')
            self.assertTrue(stat.S_ISLNK(entries['link']['mode']))
            self.assertEqual(entries, media_listing(root))
            (root / 'media.ova').write_bytes(b'changed source')
            self.assertNotEqual(entries, media_listing(root))
            with self.assertRaises(RuntimeError):
                media_listing(root / 'link')
        with self.assertRaises(RuntimeError):
            media_file({Path('/tmp'): {'source': {'mode': stat.S_IFREG}, 'source-copy': {'mode': stat.S_IFLNK}}})
        self.assertIsNone(media_folder({'link': {'mode': stat.S_IFLNK}, '.hidden': {'mode': stat.S_IFDIR}}))

    def test_no_execution_without_guard(self):
        args = parser().parse_args(['--binary', '/usr/bin/virmill', '--output', '/tmp/output'])
        self.assertFalse(args.execute_disposable)
        with self.assertRaises(RuntimeError): canonical_path('/tmp/a/../b')

    def test_wrong_host_or_uid_refuses_before_files_or_processes(self):
        argv = ['probe', '--execute-disposable', '--binary', '/usr/bin/virmill',
                '--output', '/home/virmill-test/virmill-tests/new-output']
        for host, uid, euid in [('developer-host', 1000, 1000), ('virmill-test', 0, 0),
                                ('virmill-test', 1000, 0)]:
            with self.subTest(host=host, uid=uid, euid=euid), \
                    mock.patch.object(sys, 'argv', argv), \
                    mock.patch.object(socket, 'gethostname', return_value=host), \
                    mock.patch.object(os, 'getuid', return_value=uid), \
                    mock.patch.object(os, 'geteuid', return_value=euid), \
                    mock.patch.object(os, 'open', side_effect=AssertionError('opened filesystem')), \
                    mock.patch.object(subprocess, 'Popen', side_effect=AssertionError('started process')):
                with self.assertRaisesRegex(RuntimeError, 'authorized ordinary-user disposable host'):
                    main()


if __name__ == '__main__':
    raise SystemExit(main())
