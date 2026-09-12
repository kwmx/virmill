#!/usr/bin/env python3
"""Read-only native80x24 Jobs → Activity → Refresh → Back fixture.

Requires adjacent tui_workspace_probe.py and staged binaries.json. Uses the exact
retained removal job, never creates jobs/plans or executes helper/guest mutations.
CLI operation watch without --follow is a bounded one-shot snapshot. The parent
alone stages/runs --execute-disposable; --self-test performs local decoder tests.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import stat
import time
import unittest

from tui_workspace_probe import Runner, Terminal, canonical_path, inventory, media_listing, require

URI = 'qemu:///system'
JOB_ID = '992fb03c-5056-4c0e-99cf-25140d364b4f'
TERMINAL = {'succeeded', 'failed', 'partial', 'canceled', 'recovery-required'}


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def selected_job(jobs):
    require(isinstance(jobs, list) and 0 < len(jobs) <= 4096, 'bounded existing job inventory required')
    require(all(isinstance(j, dict) and j.get('state') in TERMINAL and isinstance(j.get('createdAt'), str) for j in jobs),
            'active jobs or invalid inventory; coordinate fixture before execution')
    ordered = sorted(jobs, key=lambda j: j['createdAt'], reverse=True)
    found = [(i, j) for i, j in enumerate(ordered) if j.get('operationID') == JOB_ID]
    require(len(found) == 1 and found[0][1].get('planID'), 'exact retained job missing or duplicated')
    return found[0]


def checked_events(events):
    require(isinstance(events, list) and 0 < len(events) < 1000, 'bounded complete nonempty event snapshot required')
    previous = 0
    for event in events:
        require(event.get('apiVersion') == 'virmill/v1' and event.get('operationID') == JOB_ID,
                'event belongs to another job or API version')
        seq = event.get('sequence')
        require(type(seq) is int and seq > previous, 'event sequences duplicate or unordered')
        previous = seq
        for key in ('timestamp', 'phase', 'severity', 'message'):
            value = event.get(key)
            require(isinstance(value, str) and value and not any(ord(c) < 32 for c in value), 'invalid event ' + key)
        stamp = datetime.datetime.fromisoformat(event['timestamp'].replace('Z', '+00:00'))
        require(stamp.utcoffset() == datetime.timedelta(0), 'UTC event timestamp required')
    return events


def execute(stage):
    require(socket.gethostname() in ('virmill-test', 'virmill-test.home') and os.getuid() == os.geteuid() == 1000,
            'wrong authorized host/actor')
    stage = canonical_path(str(stage.absolute()))
    require(stage.parent == Path.home() / 'virmill-tests' and stage.stat().st_uid == 1000
            and stat.S_IMODE(stage.stat().st_mode) == 0o700, 'private staged root required')
    os.umask(0o077)
    from tui_workspace_probe import strict_json
    expected = strict_json((stage / 'binaries.json').read_bytes())
    require(all(sha('/usr/bin/' + name) == expected[name] for name in ('virmill', 'virmilld')), 'binary manifest differs')
    fd = os.open('/usr/bin/virmill', os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        st = os.fstat(fd)
        require(stat.S_ISREG(st.st_mode) and st.st_uid == 0 and st.st_mode & 0o022 == 0, 'trusted installed frontend required')
        with os.fdopen(os.dup(fd), 'rb') as stream:
            require(hashlib.file_digest(stream, 'sha256').hexdigest() == expected['virmill'], 'held executable differs')
        out = stage / 'job-activity-tui'; out.mkdir(mode=0o700)
        state = out / 'state'; state.mkdir(mode=0o700)
        runner = Runner(argparse.Namespace(binary='/usr/bin/virmill', connection=URI), out, fd)
        runner.env['XDG_STATE_HOME'] = str(state)
        report = {'status': 'failed', 'scope': 'native job activity observation/navigation; no plan, apply or host mutation',
                  'acceptanceSupport': ['JOB-02', 'JOB-03', 'UX-01', 'UX-02'], 'jobID': JOB_ID,
                  'binaries': expected, 'fixtureSHA256': sha(__file__), 'applyAttempted': False}
        before = None; terminal = None
        def observe():
            return {'vms': inventory(runner.cli('vm', 'list'), URI), 'jobs': runner.cli('operation', 'list'),
                    'networks': runner.cli('network', 'list'), 'media': media_listing(Path.home() / 'images')}
        try:
            before = observe(); index, job = selected_job(before['jobs'])
            require(runner.cli('operation', 'show', JOB_ID) == job, 'selected job changed between observations')
            events = checked_events(runner.cli('operation', 'watch', JOB_ID, '--after', '0'))
            runner.save('before.json', before); runner.save('cli-events.json', events)
            runner.save('selected-job.json', job)
            terminal = Terminal(runner, 'job-activity-80x24', 80, 24)
            def wait(label, predicate, key=None):
                return terminal.wait(label, predicate, terminal.send(key) if key is not None else -1)
            wait('Overview', lambda s: 'Virtual machines' in s)
            wait('Jobs inventory', lambda s: 'Jobs' in s and 'NAME' in s and 'STATE' in s, b'9')
            for i in range(index):
                old = terminal.screen.text(); wait('Select existing job' + str(i), lambda s: s != old, b'\x1b[B')
            require(any(line.startswith('> ') and JOB_ID in line for line in terminal.screen.text().splitlines()),
                    'selected job row differs from fresh CLI ordering')
            wait('Exact job details', lambda s: 'Job /' in s and JOB_ID in s, b'\r')
            for i in range(10):
                if '>[' in terminal.screen.text(): break
                old = terminal.screen.text(); wait('Job action focus' + str(i), lambda s: s != old, b'\t')
            for i in range(12):
                if '>[ Activity ]' in terminal.screen.text(): break
                old = terminal.screen.text(); wait('Select Activity' + str(i), lambda s: s != old, b'\x1b[C')
            require('>[ Activity ]' in terminal.screen.text(), 'direct Activity button missing')
            wait('Direct job activity', lambda s: 'Job activity' in s and JOB_ID in s, b'\r')
            require('Job ID: ' + JOB_ID in terminal.screen.text() and 'Starting sequence' not in terminal.screen.text(),
                    'Activity opened a generic parameter prompt instead of bound job events')
            first = capture_activity(terminal, events, 'initial')
            runner.save('activity-initial.json', first)
            require('>[ Refresh ]' in terminal.screen.text(), 'Refresh button focus changed unexpectedly')
            checked = re.search(r'Checked \d\d:\d\d:\d\d UTC', terminal.screen.text())
            require(checked is not None, 'last successful activity check is not visible')
            # A no-change refresh can finish within one render frame. Require a
            # changed successful-read time, not transient loading output.
            deadline = time.monotonic() + 1.1
            while time.monotonic() < deadline: terminal.read(.05)
            wait('Refresh same activity', lambda s: 'Job activity' in s and 'Refreshing activity' not in s
                 and 'Reading recorded' not in s and 'Could not read activity' not in s
                 and 'Checked ' in s and checked.group(0) not in s, b'\r')
            refreshed = capture_activity(terminal, events, 'refreshed')
            require(first == refreshed, 'refresh duplicated, omitted or changed retained job activity')
            runner.save('activity-refreshed.json', refreshed)
            wait('Back to same job', lambda s: 'Job /' in s and JOB_ID in s, b'\x1b')
            require('Activity' in terminal.screen.text(), 'Back lost the selected job actions')
            terminal.send(b'\x03')
            deadline = time.monotonic() + 5
            while terminal.process.poll() is None and time.monotonic() < deadline: terminal.read(.05)
            require(terminal.process.poll() == 0, 'normal TUI shutdown failed')
            require(checked_events(runner.cli('operation', 'watch', JOB_ID, '--after', '0')) == events,
                    'read-only activity changed stored events')
            report.update(status='passed', directActivity=True, eventCount=len(events), refreshStable=True, backSameJob=True)
        except BaseException as error:
            report['failure'] = str(error)
            raise
        finally:
            if terminal is not None: terminal.close()
            try:
                if before is not None:
                    after = observe(); runner.save('after.json', after)
                    require(before == after, 'jobs/VMs/networks/media changed during read-only fixture')
                    report['inventoriesAndMediaUnchanged'] = True
            except BaseException as error:
                report['status'] = 'failed'; report['preservationFailure'] = str(error)
                raise
            finally:
                runner.save('report.json', report)
    finally:
        os.close(fd)


# Screen decoder and paginator below are bound to the Activity view's public
# headings. They compare actual native events, not fixture-invented messages.
def screen_window(screen):
    lines = screen.splitlines()
    matches = [(i, re.match(r'^\s*Lines (\d+)[–-](\d+) of (\d+)(?:\s*·.*)?\s*$', line)) for i, line in enumerate(lines)]
    matches = [(i, match) for i, match in matches if match]
    require(len(matches) == 1, 'activity scroll range unavailable or ambiguous')
    index, match = matches[0]
    start, end, total = map(int, match.groups())
    require(1 <= start <= end <= total <= 20000, 'invalid activity line range')
    count = end - start + 1
    headers = [i for i, line in enumerate(lines) if line.strip().startswith('Recorded events')]
    require(len(headers) == 1 and headers[0] + 1 + count <= index, 'activity range exceeds visible rows')
    body = headers[0] + 1
    return start, end, total, lines[body:body+count]


def compare_visible(lines, events):
    headers = []
    for event in events:
        stamp = datetime.datetime.fromisoformat(event['timestamp'].replace('Z', '+00:00'))
        headers.append(stamp.strftime('%Y-%m-%d %H:%M:%S UTC') + ' · ' + event['phase'] + ' · ' + event['severity'])
    positions = []
    for i, line in enumerate(lines):
        if re.match(r'^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d UTC', line.strip()):
            positions.append(i)
    require(len(positions) == len(events), 'visible activity omitted or duplicated event headers')
    result = []
    for index, event in enumerate(events):
        pos = positions[index]
        require(lines[pos].strip() == headers[index], 'visible timestamp/phase/severity differs from CLI event')
        end = positions[index+1] if index+1 < len(positions) else len(lines)
        message = ' '.join(' '.join(lines[pos+1:end]).split())
        require(message == ' '.join(event['message'].split()), 'visible complete event message differs from CLI')
        result.append({'sequence': event['sequence'], 'header': headers[index], 'message': message})
    return result


def capture_activity(terminal, events, label):
    terminal.send(b'\x1b[H')
    terminal.wait(label + ' first activity lines', lambda s: 'Job activity' in s and re.search(r'Lines 1[–-]', s)
                  and 'Reading recorded' not in s and 'Refreshing activity' not in s and 'Could not read activity' not in s)
    collected = {}; total = None
    for page in range(128):
        screen = terminal.screen.text()
        start, end, count, lines = screen_window(screen)
        require(total is None or total == count, 'activity changed during read-only capture')
        total = count
        for number, line in enumerate(lines, start):
            require(number not in collected or collected[number] == line, 'overlapping activity lines changed')
            collected[number] = line
        if end == total: break
        old = screen
        terminal.wait(label + ' page ' + str(page+1), lambda s: s != old,
                      terminal.send(b'\x1b[6~'))
    require(total is not None and len(collected) == total, 'activity scrolling omitted lines or exceeded bound')
    ordered = [collected[i] for i in range(1, total+1)]
    return compare_visible(ordered, events)


class DecoderTests(unittest.TestCase):
    def events(self):
        return [{'apiVersion': 'virmill/v1', 'operationID': JOB_ID, 'sequence': seq,
                 'timestamp': '2026-09-12T01:02:03Z', 'phase': 'running', 'severity': 'info',
                 'message': message} for seq, message in [(1, 'Intent persisted: remove definition'), (2, 'All completion predicates verified')]]
    def test_exact_job_and_event_identity(self):
        checked_events(self.events())
        for events in [self.events()[::-1], self.events() * 2, [dict(self.events()[0], operationID='other')]]:
            with self.assertRaises(Exception): checked_events(events)
        job = {'operationID': JOB_ID, 'createdAt': '2026-09-12T01:02:03Z', 'state': 'succeeded', 'planID': 'plan'}
        self.assertEqual(selected_job([job])[0], 0)
        with self.assertRaises(Exception): selected_job([job, job])
        with self.assertRaises(Exception): selected_job([dict(job, state='running')])
    def test_visible_messages_headers_and_duplicates(self):
        header = '2026-09-12 01:02:03 UTC · running · info'
        lines = [header, 'Intent persisted:', 'remove definition', '', header, 'All completion predicates verified', '']
        self.assertEqual(len(compare_visible(lines, self.events())), 2)
        for wrong in [lines[:-2], lines + [header, 'extra'], [line.replace('running', 'failed') for line in lines]]:
            with self.assertRaises(Exception): compare_visible(wrong, self.events())
    def test_bounded_range_and_exact_body_extraction(self):
        screen = 'Job activity\nJob ID: ' + JOB_ID + '\nRecorded events\nfirst\nsecond\n\n\nLines 3–4 of 7 · PgUp/PgDn · Home/End\n[ Refresh ]'
        self.assertEqual(screen_window(screen), (3, 4, 7, ['first', 'second']))
        for wrong in [screen.replace('3–4', '4–3'), screen.replace(' of 7', ' of 20001'), screen + '\nLines 1–2 of 7']:
            with self.assertRaises(Exception): screen_window(wrong)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--execute-disposable', action='store_true')
    parser.add_argument('--root', type=Path)
    parser.add_argument('--self-test', action='store_true')
    args = parser.parse_args()
    if args.self_test:
        unittest.main(argv=[__file__])
    elif args.execute_disposable and args.root is not None:
        execute(args.root)
    else:
        parser.error('explicit --execute-disposable and --root required')
