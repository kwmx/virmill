#!/usr/bin/env python3
"""Actual private coordinator NDJSON streams for generated SDK scaffold jobs."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest

ROOT = Path(os.environ.get('VIRMILL_TEST_BUILD_ROOT', Path(__file__).resolve().parents[2])).resolve()


class CLIStreaming(unittest.TestCase):
    def test_wait_follow_and_cursor_resume_on_durable_scaffold_job(self):
        if os.environ.get('VIRMILL_TEST_REQUIRE_IPC') != '1':
            self.skipTest('explicit private IPC test execution required')
        with tempfile.TemporaryDirectory(prefix='virmill-stream-') as temp:
            root = Path(temp)
            env = dict(os.environ)
            for variable, name in [('XDG_STATE_HOME', 'state'), ('XDG_RUNTIME_DIR', 'runtime'),
                                   ('XDG_CACHE_HOME', 'cache'), ('XDG_DATA_HOME', 'data'),
                                   ('XDG_CONFIG_HOME', 'config')]:
                directory = root / name
                directory.mkdir(mode=0o700)
                env[variable] = str(directory)
            with (root / 'daemon.log').open('w+') as log:
                daemon = subprocess.Popen([str(ROOT / 'build/bin/virmilld')], env=env, stdout=log, stderr=log)
                try:
                    socket = root / 'runtime/virmill/control.sock'
                    deadline = time.monotonic() + 5
                    while not socket.exists() and time.monotonic() < deadline:
                        self.assertIsNone(daemon.poll(), 'private coordinator exited')
                        time.sleep(.02)
                    self.assertTrue(socket.exists())

                    def command(*args, output='json'):
                        result = subprocess.run([str(ROOT / 'build/bin/virmill'), *args,
                                                 '--output', output, '--non-interactive', '--timeout', '20s'],
                                                env=env, cwd=root, capture_output=True, text=True,
                                                stdin=subprocess.DEVNULL, timeout=25)
                        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                        self.assertEqual(result.stderr, '')
                        self.assertTrue(result.stdout.endswith('\n'))
                        frames = [json.loads(line) for line in result.stdout.splitlines()]
                        for frame in frames:
                            self.assertEqual(set(frame), {'apiVersion', 'data', 'warnings', 'error'})
                            self.assertEqual(frame['apiVersion'], 'virmill/v1')
                            self.assertIsNone(frame['error'])
                        if output == 'json':
                            self.assertEqual(len(frames), 1)
                        return frames

                    plan = command('plugin', 'new', str(root / 'generated-plugin'), '--id',
                                   'example.virmill.stream-fixture', '--sdk-directory', str(ROOT / 'sdk/go'))[0]['data']
                    args = ['plan', 'apply', plan['planID'], '--digest', plan['planDigest'],
                            '--idempotency-key', 'stream-fixture-apply', '--wait']
                    for acknowledgement in plan['acknowledgements']:
                        args += ['--ack', acknowledgement]
                    frames = command(*args, output='ndjson')
                    job = frames[-1]['data']
                    self.assertEqual(job['state'], 'succeeded')
                    operation = job['operationID']
                    self.assertEqual(frames[0]['data']['operationID'], operation)
                    events = [f['data'] for f in frames if 'sequence' in f['data']]
                    self.assertGreater(len(events), 1)
                    self.assertEqual([e['sequence'] for e in events], list(range(1, len(events) + 1)))
                    self.assertTrue(all(e['operationID'] == operation for e in events))
                    self.assertTrue(all(e['timestamp'].endswith('Z') for e in events))
                    replay = command('operation', 'watch', operation, '--follow', output='ndjson')
                    self.assertEqual([f['data'] for f in replay if 'sequence' in f['data']], events)
                    cursor = events[-1]['sequence']
                    resumed = command('operation', 'watch', operation, '--follow', '--after', str(cursor), output='ndjson')
                    self.assertEqual(len(resumed), 1)
                    self.assertEqual(resumed[0]['data'], job)
                    ordinary = command('operation', 'watch', operation)[0]['data']
                    self.assertEqual(ordinary, events)
                    self.assertFalse(command('operation', 'show', operation)[0]['data']['cancelRequested'])
                    self.assertTrue((root / 'generated-plugin/main.go').is_file())
                    print(json.dumps({'source': 'actual private coordinator and generated scaffold job',
                                      'operationID': operation, 'events': len(events), 'cursor': cursor,
                                      'terminalState': job['state'], 'resumeEvents': 0,
                                      'hiddenPrompts': False, 'hostOrGuestEffects': False}))
                finally:
                    daemon.terminate()
                    daemon.wait(timeout=5)


if __name__ == '__main__':
    unittest.main(verbosity=2)
