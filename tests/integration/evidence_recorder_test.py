"""Acceptance-attribution tests using generated files and mocked Git reads only."""
import contextlib
import hashlib
import io
import json
from pathlib import Path
import runpy
import shutil
import sys
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[2]
RECORDER = ROOT / 'scripts/record-evidence.py'
CATALOG = ROOT / 'virmill-v1-spec/contracts/requirements.json'


class EvidenceRecorderRequirements(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='virmill-attribution-fixture-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name) / 'recorder'
        self.frozen = self.root / 'isolated-source'
        self.catalog = self.root / 'virmill-v1-spec/contracts/requirements.json'
        self.catalog.parent.mkdir(parents=True)
        shutil.copyfile(CATALOG, self.catalog)
        (self.root / 'scripts').mkdir()
        self.script = self.root / 'scripts/record-evidence.py'
        shutil.copyfile(RECORDER, self.script)
        shutil.copyfile(RECORDER.with_name('private_values.py'), self.root / 'scripts/private_values.py')
        self.evidence = self.root / 'docs/evidence'
        self.evidence.mkdir(parents=True)
        self.ledger = self.evidence / 'ledger.jsonl'
        self.revisions = {self.root: 'a' * 40, self.frozen: 'b' * 40}
        for root in self.revisions:
            (root / 'internal').mkdir(parents=True)
            (root / 'go.mod').write_text('module original.generated.fixture\n')
            (root / 'go.sum').write_text('')
            (root / 'internal/payload.txt').write_text(root.name + '\n')
        self.git_reads = []

    def git_read(self, args, *, cwd, text, **kwargs):
        """No Git is executed and these synthetic values are not provenance proof."""
        self.assertTrue(text)
        root = Path(cwd)
        self.assertIn(root, self.revisions)
        self.git_reads.append((tuple(args), root))
        if args == ['git', 'rev-parse', '--show-toplevel']:
            return str(root) + '\n'
        self.assertEqual(args, ['git', 'rev-parse', 'HEAD'])
        return self.revisions[root] + '\n'

    def record(self, requirements=None, *, identity='generated-check', source=None,
               cwd='.', command=None):
        args = [str(self.script), '--id', identity, '--class', 'unit', '--cwd', cwd]
        if requirements is not None:
            args += ['--requirements', requirements]
        if source is not None:
            args += ['--source-root', source]
        if command is None:
            command = [sys.executable, '-c',
                       'from pathlib import Path; Path("command-ran").touch(); '
                       'print(Path("internal/payload.txt").read_text(), end="")']
        args += ['--', *command]
        stdout, stderr = io.StringIO(), io.StringIO()
        with mock.patch.object(sys, 'argv', args), \
                mock.patch('subprocess.check_output', side_effect=self.git_read), \
                contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
            with self.assertRaises(SystemExit) as stopped:
                runpy.run_path(str(self.script), run_name='__main__')
        return stopped.exception.code, stdout.getvalue(), stderr.getvalue()

    def evidence_bytes(self):
        return {path.relative_to(self.evidence).as_posix(): path.read_bytes()
                for path in self.evidence.rglob('*') if path.is_file()}

    def assert_rejected_without_side_effect(self, requirements):
        before = self.evidence_bytes()
        calls = list(self.git_reads)
        code, stdout, stderr = self.record(requirements)
        self.assertEqual(code, 2, stdout + stderr)
        self.assertIn('requirement', stderr)
        self.assertEqual(self.git_reads, calls, 'validation ran after a command/Git read')
        self.assertFalse((self.root / 'command-ran').exists())
        self.assertFalse((self.frozen / 'command-ran').exists())
        self.assertEqual(self.evidence_bytes(), before)
        self.assertFalse((self.evidence / 'logs/generated-check.log').exists())

    def test_unknown_ids_and_mixed_lists_never_execute_or_create_evidence(self):
        for ids in ('IO-01', 'NET-00', 'NET-99', 'REL-05', 'core-01', 'CORE-01 ',
                    ' CORE-01', ' ', 'CORE-01,IO-01,REL-04'):
            with self.subTest(requirements=ids):
                self.assert_rejected_without_side_effect(ids)
                self.assertFalse((self.evidence / 'logs').exists())
                self.assertFalse(self.ledger.exists())

    def test_duplicate_ids_are_refused_including_nonadjacent_and_empty_separators(self):
        for ids in ('CORE-01,CORE-01', 'NET-01,REL-04,NET-01', ',REL-03,,REL-03,',
                    'UNKNOWN-01,UNKNOWN-01'):
            with self.subTest(requirements=ids):
                self.assert_rejected_without_side_effect(ids)

    def test_invalid_attribution_preserves_existing_legacy_bytes(self):
        # Historical attribution is retained exactly, including old mistakes;
        # this validator constrains only new entries.
        self.ledger.write_bytes(b'{"id":"historical","requirements":["IO-01"]}\n')
        (self.evidence / 'logs').mkdir()
        (self.evidence / 'logs/historical.log').write_bytes(b'original historical bytes\n')
        self.assert_rejected_without_side_effect('IO-02')
        self.assert_rejected_without_side_effect('REL-03,REL-03')

    def test_all_71_canonical_ids_keep_input_order_and_ledger_shape(self):
        identifiers = [row['id'] for row in json.loads(CATALOG.read_bytes())['requirements']]
        self.assertEqual(len(identifiers), 71)
        self.assertEqual(len(set(identifiers)), 71)
        identifiers.reverse()
        code, stdout, stderr = self.record(','.join(identifiers))
        self.assertEqual(code, 0, stderr)
        entry = json.loads(self.ledger.read_text())
        self.assertEqual(entry['requirements'], identifiers)
        self.assertEqual(entry['revision'], self.revisions[self.root])
        self.assertEqual(entry['result'], 'passed')
        self.assertEqual(entry['exitCode'], 0)
        self.assertEqual(entry['evidenceClass'], 'unit')
        self.assertEqual(entry['environment'], 'environment.json')
        self.assertNotIn('sourceRoot', entry)
        self.assertNotIn('recorderRevision', entry)
        self.assertNotIn('status', entry)
        log = (self.evidence / entry['log']).read_bytes()
        self.assertEqual(log, b'recorder\n')
        self.assertEqual(entry['logSHA256'], hashlib.sha256(log).hexdigest())
        self.assertTrue((self.root / 'command-ran').is_file())
        self.assertIn('"evidenceID": "generated-check"', stdout)

    def test_empty_default_semantics_stay_compatible(self):
        # No attribution needs no catalog read. This also keeps existing
        # generated recorder-only fixtures and their empty default working.
        self.catalog.unlink()
        for i, requirements in enumerate((None, '', ',,')):
            with self.subTest(requirements=requirements):
                code, stdout, stderr = self.record(requirements, identity=f'empty-{i}')
                self.assertEqual(code, 0, stdout + stderr)
        entries = [json.loads(line) for line in self.ledger.read_text().splitlines()]
        self.assertEqual([entry['requirements'] for entry in entries], [[], [], []])

    def test_empty_separators_do_not_change_valid_attribution(self):
        code, stdout, stderr = self.record(',NET-01,,REL-04,')
        self.assertEqual(code, 0, stdout + stderr)
        self.assertEqual(json.loads(self.ledger.read_text())['requirements'], ['NET-01', 'REL-04'])

    def test_selected_source_cannot_redefine_acceptance_ids(self):
        selected_catalog = self.frozen / 'virmill-v1-spec/contracts/requirements.json'
        selected_catalog.parent.mkdir(parents=True)
        selected_catalog.write_text('{"requirements":[{"id":"IO-01"}]}')
        before = self.evidence_bytes()
        code, stdout, stderr = self.record('IO-01', source='isolated-source', cwd='isolated-source')
        self.assertEqual(code, 2, stdout + stderr)
        self.assertFalse((self.frozen / 'command-ran').exists())
        self.assertEqual(self.evidence_bytes(), before)
        self.assertEqual(self.git_reads, [])
        code, stdout, stderr = self.record('UX-01,REL-03', source='isolated-source', cwd='isolated-source')
        self.assertEqual(code, 0, stdout + stderr)
        self.assertEqual(json.loads(self.ledger.read_text())['requirements'], ['UX-01', 'REL-03'])

    def test_valid_selected_source_preserves_digest_revision_and_recorder_attribution(self):
        code, stdout, stderr = self.record('REL-04,REL-03', identity='first',
                                           source='isolated-source', cwd='isolated-source')
        self.assertEqual(code, 0, stdout + stderr)
        (self.root / 'internal/payload.txt').write_text('unrelated parent source changed')
        code, stdout, stderr = self.record('REL-04,REL-03', identity='second',
                                           source='isolated-source', cwd='isolated-source')
        self.assertEqual(code, 0, stdout + stderr)
        first, second = [json.loads(line) for line in self.ledger.read_text().splitlines()]
        for entry in (first, second):
            self.assertEqual(entry['requirements'], ['REL-04', 'REL-03'])
            self.assertEqual(entry['sourceRoot'], 'isolated-source')
            self.assertEqual(entry['revision'], self.revisions[self.frozen])
            self.assertEqual(entry['recorderRevision'], self.revisions[self.root])
            self.assertEqual(entry['cwd'], 'isolated-source')
            self.assertEqual((self.evidence / entry['log']).read_bytes(), b'isolated-source\n')
        self.assertEqual(first['sourceDigest'], second['sourceDigest'])
        (self.frozen / 'internal/payload.txt').write_text('selected implementation changed')
        self.assertEqual(self.record('REL-03', identity='third', source='isolated-source',
                                     cwd='isolated-source')[0], 0)
        third = json.loads(self.ledger.read_text().splitlines()[-1])
        self.assertNotEqual(first['sourceDigest'], third['sourceDigest'])

    def test_unavailable_or_invalid_catalog_refuses_before_command(self):
        valid = self.catalog.read_bytes()
        malformed = json.loads(valid)
        malformed['requirements'][1]['id'] = malformed['requirements'][0]['id']
        for data in (None, b'not json', b'[]', b'{"requirements":null}',
                     b'{"requirements":[]}', json.dumps(malformed).encode(),
                     b' ' * ((256 << 10) + 1), b'\xff'):
            with self.subTest(data=None if data is None else len(data)):
                if data is None:
                    self.catalog.unlink()
                else:
                    self.catalog.write_bytes(data)
                self.assert_rejected_without_side_effect('REL-04')
                self.catalog.write_bytes(valid)

    def test_valid_failed_check_records_failure_without_promoting_requirement(self):
        code, stdout, stderr = self.record('REL-04', command=[sys.executable, '-c',
                                                            'print("synthetic failure"); raise SystemExit(9)'])
        self.assertEqual(code, 9, stdout + stderr)
        entry = json.loads(self.ledger.read_text())
        self.assertEqual(entry['requirements'], ['REL-04'])
        self.assertEqual(entry['result'], 'failed')
        self.assertEqual(entry['exitCode'], 9)
        self.assertNotIn('status', entry)


if __name__ == '__main__':
    unittest.main()
