"""Generated compiler fixtures test orchestration; actual cross builds are separate."""
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
SCRIPT = ROOT / 'scripts/cross-build.sh'
TARGETS = ('darwin/arm64', 'windows/amd64')
PACKAGES = ('internal/domain', 'internal/app/provision', 'internal/wire',
            'internal/validation', 'internal/operations', 'internal/ui/cli',
            'internal/ui/tui')


class CrossBuildContract(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='virmill-cross-contract-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name) / 'source with spaces'
        self.root.mkdir()
        for directory in ('scripts', *PACKAGES):
            (self.root / directory).mkdir(parents=True, exist_ok=True)
        for name in ('go.mod', 'vendor/modules.txt', 'sdk/go/go.mod',
                     'sdk/go/protocol/json.go', 'sdk/go/example/summary.go',
                     'examples/plugins/vm-summary/go.mod',
                     'tests/fixtures/plugins/provider/go.mod',
                     'tests/fixtures/plugins/provider/main.go'):
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('original generated contract input\n')
        shutil.copyfile(SCRIPT, self.root / 'scripts/cross-build.sh')
        self.calls = self.root / 'compiler-calls.jsonl'
        # No compiler, foreign executable, Git operation or network runs here.
        (self.root / 'scripts/go').write_text(f'#!{sys.executable}\n' + r'''
import json, os
from pathlib import Path
import sys
args = sys.argv[1:]
root = Path(__file__).resolve().parents[1]
envkeys = ('GOOS', 'GOARCH', 'CGO_ENABLED', 'GOPROXY', 'GOSUMDB',
           'GOTOOLCHAIN', 'GOENV', 'GOWORK', 'GO111MODULE', 'GOFLAGS',
           'GOEXPERIMENT', 'GOAMD64', 'GOARM64', 'LC_ALL', 'TZ', 'GOROOT',
           'GOTOOLDIR')
with (root / 'compiler-calls.jsonl').open('a') as stream:
    stream.write(json.dumps({'args': args, 'cwd': str(Path.cwd()),
                            'env': {k: os.environ.get(k) for k in envkeys}}) + '\n')
if args == ['tool', 'dist', 'list']:
    print(os.environ.get('FIXTURE_TARGETS', 'darwin/arm64\nwindows/amd64'))
    sys.exit(0)
if args == ['version']:
    print('go version go1.27.1 synthetic-orchestration-only')
    sys.exit(0)
if '-o' not in args:
    raise SystemExit('unexpected compiler command')
path = Path(args[args.index('-o') + 1])
if path.name == os.environ.get('FIXTURE_FAIL_ARTIFACT'):
    raise SystemExit(17)
if path.name == os.environ.get('FIXTURE_OMIT_ARTIFACT'):
    sys.exit(0)
if path.name == os.environ.get('FIXTURE_SYMLINK_ARTIFACT'):
    path.symlink_to(root / 'go.mod')
else:
    path.write_bytes(b'' if path.name == os.environ.get('FIXTURE_EMPTY_ARTIFACT') else
                     b'generated compiler output; never executed\n')
''')
        (self.root / 'scripts/go').chmod(0o700)

    def run_script(self, extra_env=None, args=()):
        env = dict(os.environ)
        env.update(GOOS='unapproved', GOARCH='unapproved', CGO_ENABLED='1',
                   GOPROXY='https://example.invalid', GOSUMDB='unapproved',
                   GOTOOLCHAIN='auto', GOENV='unapproved', GOWORK='unapproved',
                   GOFLAGS='-race -mod=mod', GOEXPERIMENT='unapproved',
                   GOAMD64='v4', GOARM64='v9.5', GOROOT='/unapproved',
                   GOTOOLDIR='/unapproved')
        env.update(extra_env or {})
        result = subprocess.run(['/bin/sh', str(self.root / 'scripts/cross-build.sh'), *args],
                                cwd=self.root.parent, env=env, text=True,
                                capture_output=True, timeout=30)
        return result

    def compiler_calls(self):
        if not self.calls.exists():
            return []
        return [json.loads(line) for line in self.calls.read_text().splitlines()]

    def outputs(self):
        return sorted((self.root / 'build/cross').glob('run.*'))

    def test_complete_exact_matrix_pins_all_build_settings(self):
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn('2 targets, 26 compile-only artifacts', result.stdout)
        calls = self.compiler_calls()
        self.assertEqual(len(calls), 28)  # target check/version plus 26 outputs
        builds = calls[2:]
        self.assertEqual({(c['env']['GOOS'], c['env']['GOARCH']) for c in builds},
                         {tuple(t.split('/')) for t in TARGETS})
        expected = {'CGO_ENABLED': '0', 'GOPROXY': 'off', 'GOSUMDB': 'off',
                    'GOTOOLCHAIN': 'local', 'GOENV': 'off', 'GOWORK': 'off',
                    'GO111MODULE': 'on', 'GOFLAGS': '', 'GOEXPERIMENT': '',
                    'GOAMD64': 'v1', 'GOARM64': 'v8.0', 'LC_ALL': 'C',
                    'TZ': 'UTC', 'GOROOT': None, 'GOTOOLDIR': None}
        for call in calls:
            for key, value in expected.items():
                self.assertEqual(call['env'][key], value, (key, call))
            self.assertEqual(call['cwd'], str(self.root))
        for call in builds:
            for flag in ('-trimpath', '-buildvcs=false', '-ldflags=-buildid='):
                self.assertIn(flag, call['args'])
            self.assertIn('-mod=readonly' if '-C' in call['args'] else '-mod=vendor',
                          call['args'])
            if 'test' in call['args']:
                self.assertIn('-c', call['args'], 'foreign target tests must not execute')
            else:
                self.assertIn('build', call['args'])
        output, = self.outputs()
        artifacts = (output / 'artifacts.txt').read_text().splitlines()
        expected_artifacts = []
        for target in TARGETS:
            expected_artifacts += [f'{target}/core/{p.replace("/", "-")}.a' for p in PACKAGES]
            expected_artifacts += [f'{target}/{p}.a' for p in ('sdk', 'protocol', 'sdk-summary')]
            suffix = '.exe' if target.startswith('windows/') else ''
            expected_artifacts += [f'{target}/sdk.test{suffix}', f'{target}/vm-summary{suffix}',
                                   f'{target}/provider-fixture{suffix}']
        self.assertEqual(artifacts, expected_artifacts)
        checksums = (output / 'artifact-sha256.txt').read_text().splitlines()
        self.assertEqual(len(checksums), 26)
        for line, artifact in zip(checksums, artifacts):
            digest, name = line.split('  ', 1)
            self.assertEqual(name, artifact)
            self.assertEqual(digest, hashlib.sha256((output / artifact).read_bytes()).hexdigest())

    def test_missing_declared_target_fails_before_creating_outputs(self):
        for targets in ('darwin/arm64', 'windows/amd64', 'darwin/arm64-extra\nwindows/amd64', ''):
            with self.subTest(targets=targets):
                result = self.run_script({'FIXTURE_TARGETS': targets})
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('lacks required target', result.stderr)
                self.assertEqual(self.outputs(), [])
        self.assertTrue(all(c['args'] == ['tool', 'dist', 'list'] for c in self.compiler_calls()))

    def test_missing_reference_fixture_fails_before_compiler_or_outputs(self):
        (self.root / 'tests/fixtures/plugins/provider/main.go').unlink()
        result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('required cross-build input', result.stderr)
        self.assertEqual(self.compiler_calls(), [])
        self.assertEqual(self.outputs(), [])

    def test_missing_core_package_fails_before_compiler_or_outputs(self):
        (self.root / 'internal/wire').rmdir()
        result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('required cross-build package', result.stderr)
        self.assertEqual(self.compiler_calls(), [])
        self.assertEqual(self.outputs(), [])

    def test_target_selection_arguments_cannot_skip_matrix(self):
        result = self.run_script(args=('darwin/arm64',))
        self.assertEqual(result.returncode, 2)
        self.assertIn('every declared target is required', result.stderr)
        self.assertEqual(self.compiler_calls(), [])
        self.assertEqual(self.outputs(), [])

    def test_build_failure_preserves_partial_outputs_without_success_checksums(self):
        result = self.run_script({'FIXTURE_FAIL_ARTIFACT': 'provider-fixture'})
        self.assertEqual(result.returncode, 17)
        self.assertNotIn('Cross-build passed', result.stdout)
        self.assertIn('partial outputs retained', result.stderr)
        output, = self.outputs()
        self.assertTrue((output / 'darwin/arm64/vm-summary').is_file())
        self.assertFalse((output / 'artifact-sha256.txt').exists())
        self.assertFalse((output / 'windows').exists())

    def test_compiler_success_without_regular_nonempty_output_is_failure(self):
        for variable in ('FIXTURE_OMIT_ARTIFACT', 'FIXTURE_EMPTY_ARTIFACT', 'FIXTURE_SYMLINK_ARTIFACT'):
            with self.subTest(variable=variable):
                result = self.run_script({variable: 'provider-fixture.exe'})
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn('Cross-build passed', result.stdout)
                self.assertIn('required cross-build artifact', result.stderr)
        self.assertEqual(len(self.outputs()), 3)
        self.assertTrue(all(not (p / 'artifact-sha256.txt').exists() for p in self.outputs()))

    def test_new_run_preserves_previous_outputs_and_cannot_reuse_stale_artifact(self):
        first = self.run_script()
        self.assertEqual(first.returncode, 0, first.stderr)
        old, = self.outputs()
        before = {p.relative_to(old): p.read_bytes() for p in old.rglob('*') if p.is_file()}
        second = self.run_script({'FIXTURE_OMIT_ARTIFACT': 'provider-fixture.exe'})
        self.assertNotEqual(second.returncode, 0)
        self.assertEqual(len(self.outputs()), 2)
        self.assertEqual(before, {p.relative_to(old): p.read_bytes() for p in old.rglob('*') if p.is_file()})


if __name__ == '__main__':
    unittest.main()
