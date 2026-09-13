#!/usr/bin/env python3
"""Private-value redaction and commit checks, using generated values only.

Addresses come from the documentation range and names from `.example`; no
real host, path or media name appears here.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import private_values

VALUES = [
    {'match': '203.0.113.7', 'placeholder': '<test-vm-address>', 'wholeWord': True},
    {'match': 'lab-host.example', 'placeholder': '<test-vm-host>', 'wholeWord': True},
    {'match': '/home/labuser', 'placeholder': '<test-vm-home>', 'wholeWord': True},
    {'match': 'labuser', 'placeholder': '<test-vm-login>', 'wholeWord': True},
    {'match': 'Secret-Image', 'placeholder': '<owner-media-1>', 'ignoreCase': True},
]
TOKEN = 'ghp_' + 'A1b2' * 9
URL = 'https://user:' + 'pw123' + '@git.corp-host.net/x'
KEY_DATA = 'b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2'


def git(root, *args):
    env = {k: v for k, v in os.environ.items() if not k.startswith('GIT_')}
    env.update(GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull)
    return subprocess.run(['git', '-c', 'user.name=fixture', '-c', 'user.email=fixture@example.invalid', *args],
                          cwd=root, env=env, capture_output=True, text=True, check=True).stdout


class Redaction(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='virmill-private-values-')
        self.addCleanup(temporary.cleanup)
        self.config = Path(temporary.name) / 'private-values.json'
        self.config.write_text(json.dumps({'values': VALUES}))
        self.rules = private_values.load(self.config, home='/home/developer')

    def test_values_become_placeholders_without_touching_longer_names(self):
        text = ('ssh labuser@lab-host.example 203.0.113.7:22 /home/labuser/runs labusers '
                '203.0.113.70 /home/developer/project secret-image.ova Secret-Image-copy /home/developers')
        redacted, count = private_values.redact(text, self.rules)
        self.assertEqual(redacted, 'ssh <test-vm-login>@<test-vm-host> <test-vm-address>:22 <test-vm-home>/runs '
                                   'labusers 203.0.113.70 <dev-home>/project <owner-media-1>.ova '
                                   '<owner-media-1>-copy /home/developers')
        self.assertEqual(count, 7)

    def test_credentials_are_found_but_rejection_fixtures_are_not(self):
        text = f'token={TOKEN}\n{URL}\n-----BEGIN OPENSSH PRIVATE KEY-----\n{KEY_DATA}\n'
        found = list(private_values.findings(text, []))
        self.assertEqual(found, [(1, '<redacted-github-token>'), (3, '<redacted-private-key>'),
                                 (2, '<redacted-url-credentials>')])
        redacted, _ = private_values.redact(text, [])
        for secret in (TOKEN, 'pw123', KEY_DATA):
            self.assertNotIn(secret, redacted)
        header_only = '"-----BEGIN OPENSSH PRIVATE KEY-----" + secret'
        self.assertEqual(list(private_values.findings(header_only, [])), [])
        for reserved in ('example.invalid', 'example.com', 'repo.example', 'localhost:8080'):
            rejection_fixture = 'https://name:' + 'secret@' + reserved + '/image'
            self.assertEqual(list(private_values.findings(rejection_fixture, [])), [], reserved)

    def test_home_rule_skips_root_directories_and_can_be_disabled(self):
        for home in ('/', '/root'):
            self.assertEqual(private_values.load(Path('/nonexistent'), home=home), [])
        self.assertEqual(private_values.load(Path('/nonexistent'), include_home=False, home='/home/developer'), [])


class Tooling(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='virmill-private-tools-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name) / 'repo'
        (self.root / 'scripts').mkdir(parents=True)
        for name in ('private_values.py', 'check-private.py', 'redact-tracked.py', 'record-evidence.py'):
            shutil.copyfile(ROOT / 'scripts' / name, self.root / 'scripts' / name)
        (self.root / '.virmill-local').mkdir()
        (self.root / '.virmill-local/private-values.json').write_text(json.dumps({'values': VALUES}))
        (self.root / '.gitignore').write_text('/.virmill-local/\n__pycache__/\n')
        (self.root / 'docs/evidence').mkdir(parents=True)
        (self.root / 'docs/evidence/ledger.jsonl').write_text(json.dumps({'id': 'earlier'}) + '\n')
        catalog = self.root / 'virmill-v1-spec/contracts/requirements.json'
        catalog.parent.mkdir(parents=True)
        shutil.copyfile(ROOT / 'virmill-v1-spec/contracts/requirements.json', catalog)
        (self.root / 'internal').mkdir()
        (self.root / 'internal/payload.txt').write_text('generated\n')
        (self.root / 'go.mod').write_text('module fixture.invalid/private\n')
        (self.root / 'go.sum').write_text('')
        git(self.root, 'init', '-q')
        git(self.root, 'add', '.')
        git(self.root, 'commit', '-q', '-m', 'fixture')
        self.env = {k: v for k, v in os.environ.items() if k != 'CI'}

    def run_script(self, *args):
        return subprocess.run([sys.executable, *args], cwd=self.root, env=self.env, capture_output=True, text=True)

    def test_staged_check_refuses_without_printing_values(self):
        (self.root / 'notes.md').write_text('Connect with ssh labuser@lab-host.example\n')
        git(self.root, 'add', 'notes.md')
        result = self.run_script('scripts/check-private.py', '--staged')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('notes.md:1: contains <test-vm-host>', result.stdout)
        self.assertNotIn('lab-host.example', result.stdout + result.stderr)
        self.assertNotIn('labuser@', result.stdout + result.stderr)

    def test_redaction_records_hashes_and_leaves_a_clean_tree(self):
        notes = self.root / 'notes.md'
        notes.write_text('Host lab-host.example stores Secret-Image.ova\n')
        git(self.root, 'add', 'notes.md')
        git(self.root, 'commit', '-q', '-m', 'add notes')
        before = hashlib.sha256(notes.read_bytes()).hexdigest()
        self.assertNotEqual(self.run_script('scripts/check-private.py').returncode, 0)
        result = self.run_script('scripts/redact-tracked.py', '--id', 'redaction-fixture-001')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(notes.read_text(), 'Host <test-vm-host> stores <owner-media-1>.ova\n')
        entry = json.loads((self.root / 'docs/evidence/ledger.jsonl').read_text().splitlines()[-1])
        self.assertEqual(entry['id'], 'redaction-fixture-001')
        self.assertEqual(entry['files']['notes.md']['beforeSHA256'], before)
        self.assertTrue(entry['files']['notes.md']['beforeMatchesRevision'])
        self.assertEqual(entry['files']['notes.md']['afterSHA256'], hashlib.sha256(notes.read_bytes()).hexdigest())
        self.assertEqual(self.run_script('scripts/check-private.py').returncode, 0)
        self.assertNotEqual(self.run_script('scripts/redact-tracked.py', '--id', 'redaction-fixture-001').returncode, 0)

    def test_recorder_redacts_log_and_command(self):
        command = [sys.executable, '-c', 'print("ssh labuser@lab-host.example")']
        result = self.run_script('scripts/record-evidence.py', '--id', 'private-recording-001', '--class', 'unit', '--', *command)
        self.assertEqual(result.returncode, 0, result.stderr)
        log = (self.root / 'docs/evidence/logs/private-recording-001.log').read_text()
        self.assertEqual(log, 'ssh <test-vm-login>@<test-vm-host>\n')
        entry = json.loads((self.root / 'docs/evidence/ledger.jsonl').read_text().splitlines()[-1])
        self.assertEqual(entry['command'][-1], 'print("ssh <test-vm-login>@<test-vm-host>")')
        self.assertEqual(entry['redactedValues'], 4)
        self.assertEqual(entry['logSHA256'], hashlib.sha256(log.encode()).hexdigest())


if __name__ == '__main__':
    unittest.main()
