#!/usr/bin/env python3
"""Private coordinator, CLI and real PTY access to confined JSON-only provider tests."""
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import struct
import subprocess
import tempfile
import termios
import time
import unittest

ROOT = Path(__file__).resolve().parents[2]


class ProviderConformanceUI(unittest.TestCase):
    def test_cli_and_real_pty_use_private_confined_provider_workspace(self):
        if os.environ.get('VIRMILL_TEST_REQUIRE_IPC') != '1':
            self.skipTest('explicit private IPC and confined generated-fixture execution required')
        fixture = ROOT / 'build/provider-conformance'
        self.assertTrue((fixture / 'manifest.json').is_file())
        with tempfile.TemporaryDirectory(prefix='virmill-provider-ui-') as temp:
            root = Path(temp)
            env = dict(os.environ, TERM='xterm-256color', NO_COLOR='1')
            for key, name in [('XDG_STATE_HOME', 'state'), ('XDG_CONFIG_HOME', 'config'),
                              ('XDG_DATA_HOME', 'data'), ('XDG_CACHE_HOME', 'cache'),
                              ('XDG_RUNTIME_DIR', 'runtime')]:
                (root / name).mkdir(mode=0o700)
                env[key] = str(root / name)
            with (root / 'daemon.log').open('w+') as log:
                daemon = subprocess.Popen([str(ROOT / 'build/bin/virmilld')], env=env, stdout=log, stderr=log)
                try:
                    socket = root / 'runtime/virmill/control.sock'
                    deadline = time.monotonic() + 5
                    while not socket.exists() and time.monotonic() < deadline:
                        self.assertIsNone(daemon.poll(), 'private coordinator exited')
                        time.sleep(.02)
                    self.assertTrue(socket.exists())
                    result = subprocess.run([str(ROOT / 'build/bin/virmill'), 'plugin', 'test', str(fixture),
                                             '--output', 'json', '--non-interactive', '--timeout', '90s'],
                                            env=env, capture_output=True, text=True, timeout=100)
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    response = json.loads(result.stdout)
                    self.assertIsNone(response['error'])
                    self.assertEqual(response['data']['pluginID'], 'example.virmill.provider-fixture')
                    self.assertTrue(response['data']['confined'])
                    self.assertGreaterEqual(len(response['data']['checks']), 8)
                    print('CLI actual confined checks:', json.dumps(response['data']))
                    master, slave = pty.openpty()
                    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack('HHHH', 24, 80, 0, 0))
                    client = subprocess.Popen([str(ROOT / 'build/bin/virmill'), 'tui'], env=env,
                                              stdin=slave, stdout=slave, stderr=slave, start_new_session=True)
                    os.close(slave)
                    received = bytearray()
                    def until(expected, seconds=10):
                        deadline = time.monotonic() + seconds
                        while expected not in received and time.monotonic() < deadline:
                            self.assertIsNone(client.poll(), received.decode(errors='replace'))
                            if select.select([master], [], [], .1)[0]:
                                received.extend(os.read(master, 65536))
                                self.assertLess(len(received), 2 << 20)
                        self.assertIn(expected, received, received.decode(errors='replace'))
                    try:
                        until(b'Virmill')
                        received.clear()
                        os.write(master, b'\t' * 9)
                        until(b'Plugins')
                        os.write(master, b'/plugin test\r')
                        time.sleep(.1)
                        received.clear()
                        os.write(master, b'\r')
                        until(b'Input:')
                        received.clear()
                        os.write(master, str(fixture).encode() + b'\r')
                        until(b'provider-fixture', 90)
                        for _ in range(30):
                            if b'confined' in received:
                                break
                            os.write(master, b'\x1b[6~')
                            if select.select([master], [], [], .2)[0]:
                                received.extend(os.read(master, 65536))
                        self.assertIn(b'confined', received, received.decode(errors='replace'))
                        self.assertIn(b'true', received)
                        print('Actual 80x24 PTY selected Plugins > plugin test and displayed the confined result.')
                        os.write(master, b'q')
                        client.wait(timeout=5)
                        self.assertEqual(client.returncode, 0)
                    finally:
                        if client.poll() is None:
                            client.terminate()
                            client.wait(timeout=5)
                        os.close(master)
                    self.assertFalse(list((root / 'cache').rglob('provider-state.json')),
                                     'conformance workspace was not discarded')
                finally:
                    daemon.terminate()
                    daemon.wait(timeout=5)


if __name__ == '__main__':
    unittest.main(verbosity=2)
