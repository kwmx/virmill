"""Generated-repository recorder checks; no product runtime or host qualification."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parents[2] / 'scripts/record-evidence.py'


class EvidenceSourceRoot(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='virmill-evidence-recorder-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name) / 'recorder'
        self.frozen = self.root / 'frozen'
        self.revisions = {}
        for root, payload in [(self.root, 'parent source'), (self.frozen, 'frozen source')]:
            (root / 'internal').mkdir(parents=True)
            (root / 'docs/evidence').mkdir(parents=True)
            (root / 'go.mod').write_text('module generated.recorder.fixture\n')
            (root / 'go.sum').write_text('')
            (root / 'internal/payload.txt').write_text(payload)
            self.git(root, 'init', '--quiet')
            self.git(root, 'add', 'go.mod', 'go.sum', 'internal/payload.txt')
            self.git(root, '-c', 'user.name=Generated Fixture', '-c', 'user.email=fixture@example.invalid',
                     '-c', 'commit.gpgsign=false', 'commit', '--quiet', '-m', payload)
            self.revisions[root] = self.git(root, 'rev-parse', 'HEAD').strip()
        (self.root / 'scripts').mkdir()
        shutil.copyfile(SOURCE, self.root / 'scripts/record-evidence.py')

    def git(self, root, *args):
        return subprocess.check_output(['git', '-c', 'core.hooksPath=/dev/null', *args], cwd=root,
                                       env={**os.environ, 'GIT_CONFIG_NOSYSTEM': '1'}, text=True)

    def run_record(self, identity, source=None, cwd='frozen', command=None):
        args = [sys.executable, str(self.root / 'scripts/record-evidence.py'), '--id', identity,
                '--class', 'generated-recorder-self-test', '--cwd', cwd]
        if source is not None:
            args += ['--source-root', source]
        args += ['--', *(command or [sys.executable, '-c',
                                    'from pathlib import Path; print(Path("internal/payload.txt").read_text())'])]
        return subprocess.run(args, cwd=self.root, capture_output=True, text=True, timeout=10)

    def entries(self):
        return [json.loads(line) for line in (self.root / 'docs/evidence/ledger.jsonl').read_text().splitlines()]

    def test_selected_checkout_and_revision_exclude_unrelated_parent_edits(self):
        first = self.run_record('first', 'frozen')
        self.assertEqual(first.returncode, 0, first.stderr)
        (self.root / 'internal/payload.txt').write_text('unrelated parent edit')
        second = self.run_record('second', 'frozen')
        self.assertEqual(second.returncode, 0, second.stderr)
        a, b = self.entries()
        self.assertEqual(a['sourceDigest'], b['sourceDigest'])
        self.assertEqual(a['revision'], self.revisions[self.frozen])
        self.assertNotEqual(a['revision'], self.revisions[self.root])
        self.assertEqual(a['recorderRevision'], self.revisions[self.root])
        self.assertEqual(a['sourceRoot'], 'frozen')
        self.assertEqual(a['cwd'], 'frozen')
        log = (self.root / 'docs/evidence' / a['log']).read_bytes()
        self.assertEqual(log, b'frozen source\n')
        self.assertEqual(a['logSHA256'], hashlib.sha256(log).hexdigest())
        (self.frozen / 'internal/payload.txt').write_text('changed frozen input')
        self.assertEqual(self.run_record('third', 'frozen').returncode, 0)
        self.assertNotEqual(a['sourceDigest'], self.entries()[-1]['sourceDigest'])

    def test_default_source_scope_preserves_legacy_recorder_behavior(self):
        result = self.run_record('legacy')
        self.assertEqual(result.returncode, 0, result.stderr)
        entry = self.entries()[0]
        self.assertNotIn('sourceRoot', entry)
        self.assertNotIn('recorderRevision', entry)
        self.assertEqual(entry['revision'], self.revisions[self.root])
        self.assertEqual(entry['cwd'], 'frozen')

    def test_outside_or_non_checkout_source_is_refused_before_command(self):
        for source in ('../', 'internal', 'missing'):
            with self.subTest(source=source):
                result = self.run_record('bad', source, command=[sys.executable, '-c',
                                                                'from pathlib import Path; Path("ran").touch()'])
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse((self.frozen / 'ran').exists())
                self.assertFalse((self.root / 'docs/evidence/ledger.jsonl').exists())

    def test_duplicate_evidence_is_never_replaced_or_reexecuted(self):
        self.assertEqual(self.run_record('fixed', 'frozen').returncode, 0)
        before = (self.root / 'docs/evidence/ledger.jsonl').read_bytes()
        again = self.run_record('fixed', 'frozen', command=[sys.executable, '-c',
                                                            'from pathlib import Path; Path("ran").touch()'])
        self.assertNotEqual(again.returncode, 0)
        self.assertFalse((self.frozen / 'ran').exists())
        self.assertEqual((self.root / 'docs/evidence/ledger.jsonl').read_bytes(), before)

    def test_nested_go_directory_cannot_inherit_parent_revision(self):
        nested = self.root / 'fake-checkout'
        (nested / 'internal').mkdir(parents=True)
        (nested / 'go.mod').write_text('module generated.noncheckout.fixture\n')
        (nested / 'go.sum').write_text('')
        (nested / 'internal/payload.txt').write_text('not a separate checkout')
        result = self.run_record('inherited', 'fake-checkout', command=[sys.executable, '-c',
                                'from pathlib import Path; Path("ran").touch()'])
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertFalse((self.frozen / 'ran').exists())
        self.assertFalse((self.root / 'docs/evidence/ledger.jsonl').exists())

    def test_actual_worktree_with_git_file_is_supported(self):
        self.git(self.frozen, 'worktree', 'add', '--quiet', '--detach', str(self.root / 'worktree'), 'HEAD')
        self.assertTrue((self.root / 'worktree/.git').is_file())
        result = self.run_record('worktree', 'worktree', cwd='worktree')
        self.assertEqual(result.returncode, 0, result.stderr)
        entry = self.entries()[0]
        self.assertEqual(entry['sourceRoot'], 'worktree')
        self.assertEqual(entry['revision'], self.revisions[self.frozen])


if __name__ == '__main__':
    unittest.main()
