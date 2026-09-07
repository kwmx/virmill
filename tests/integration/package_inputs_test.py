#!/usr/bin/env python3
"""Package input/output boundary tests using only generated temporary checkouts.

No native package tools, VMs, installed services or repository-index edits occur.
"""
import hashlib
import io
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import package as packages
import reproducibility


class PackageInputs(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='virmill-package-inputs-')
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.root = self.base / 'checkout'
        self.root.mkdir()
        self.git_env = {key: value for key, value in os.environ.items() if not key.startswith('GIT_')}
        self.git_env.update(GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL=os.devnull)
        self.env = mock.patch.dict(os.environ, self.git_env, clear=True)
        self.env.start()
        self.addCleanup(self.env.stop)
        self.git('init', '-q')
        self.files = {
            '.gitignore': b'build/\ncredentials/\n.env.*\n',
            'LICENSE': b'original fixture license\n',
            'packaging/systemd/virmilld.service': b'fixture user unit\n',
            'packaging/systemd/virmill-host-helper.service': b'fixture helper unit\n',
            'packaging/systemd/virmill-host-helper.socket': b'fixture helper socket\n',
            'packaging/policy/helper-policy.example.json': b'{"fixture":true}\n',
            'packaging/completions/virmill.bash': b'fixture bash\n',
            'packaging/completions/virmill.zsh': b'fixture zsh\n',
            'packaging/completions/virmill.fish': b'fixture fish\n',
            'docs/guide.md': b'# Original fixture documentation\n',
            'docs/evidence/logs/diagnostic.json': b'{"intentionallyExcluded":true}\n',
            'schemas/fixture.json': b'{"type":"object"}\n',
            'sdk/go/sdk.go': b'package sdk\n',
            'sdk/go/go.mod': b'module fixture.invalid/sdk\n',
            'examples/fixture.py': b'print("original fixture")\n',
            'examples/fixture.yaml': b'fixture: true\n',
            'examples/unsupported.txt': b'outside existing suffix contract\n',
        }
        for path, data in self.files.items():
            self.write(path, data)
        self.git('add', '--all')
        for path in packages.BINARY_PATHS:
            self.write(path, b'generated test-only binary bytes: ' + path.encode())

    def git(self, *args):
        return subprocess.run(['git', '-c', 'core.hooksPath=/dev/null', *args], cwd=self.root,
                              env=self.git_env, check=True, capture_output=True, timeout=15)

    def write(self, relative, data=b'generated fixture\n'):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        return path

    def assert_refused(self, action):
        with self.assertRaises((OSError, ValueError)):
            action()

    def test_tracked_source_contract_excludes_untracked_and_ignored_siblings(self):
        omitted = ('docs/untracked.md', 'docs/credentials/token.json', 'docs/.env.json',
                   'schemas/untracked.json', 'sdk/go/untracked.go', 'examples/untracked.py',
                   'build/bin/unrelated')
        for path in omitted:
            self.write(path, b'not an approved package source\n')
        # Generated reference/completion bytes may differ from the index; paths
        # remain reviewed and tracked. Packaging intentionally reads these bytes.
        self.write('docs/guide.md', b'# Regenerated tracked documentation\n')
        actual = packages.collect_package_files(self.root)
        core = actual['virmill']
        self.assertEqual(core['usr/share/doc/virmill/guide.md'], (b'# Regenerated tracked documentation\n', 0o644))
        self.assertEqual(core['usr/share/fish/vendor_completions.d/virmill.fish'], (self.files['packaging/completions/virmill.fish'], 0o644))
        self.assertEqual(core['usr/bin/virmill'][1], 0o755)
        self.assertEqual(actual['virmill-host-helper']['usr/libexec/virmill-host-helper'][1], 0o755)
        expected_sources = {
            'usr/share/virmill/schemas/fixture.json', 'usr/share/virmill/sdk/go/sdk.go',
            'usr/share/virmill/sdk/go/go.mod', 'usr/share/virmill/examples/fixture.py',
            'usr/share/virmill/examples/fixture.yaml',
        }
        self.assertTrue(expected_sources <= core.keys())
        all_paths = set(core) | set(actual['virmill-host-helper'])
        self.assertFalse(any('untracked' in path or 'credentials' in path or '.env' in path or 'unrelated' in path for path in all_paths))
        self.assertFalse(any('evidence/logs' in path or 'unsupported.txt' in path for path in all_paths))
        self.assertEqual(len(core), 13)
        self.assertEqual(len(actual['virmill-host-helper']), 5)

    def test_static_source_must_also_be_tracked(self):
        self.git('rm', '--cached', 'LICENSE')
        with self.assertRaisesRegex(ValueError, 'indexed regular file: LICENSE'):
            packages.collect_package_files(self.root)

    def test_exact_checkout_root_required(self):
        nested = self.root / 'nested'
        nested.mkdir()
        with self.assertRaisesRegex(ValueError, 'exact Git checkout root'):
            packages.tracked_inputs(nested)
        with self.assertRaises(subprocess.CalledProcessError):
            packages.tracked_inputs(self.base)

    def test_linked_worktree_is_an_exact_checkout_root(self):
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid',
                 '-c', 'commit.gpgsign=false', 'commit', '-qm', 'original generated fixture')
        worktree = self.base / 'worktree'
        self.git('worktree', 'add', '--detach', str(worktree), 'HEAD')
        self.assertTrue((worktree / '.git').is_file())
        self.assertEqual(packages.tracked_inputs(worktree), packages.tracked_inputs(self.root))

    def test_selected_source_symlink_is_refused(self):
        outside = self.base / 'outside.md'
        outside.write_bytes(b'outside fixture must stay untouched')
        selected = self.root / 'docs/guide.md'
        selected.unlink()
        selected.symlink_to(outside)
        self.assert_refused(lambda: packages.collect_package_files(self.root))
        self.assertEqual(outside.read_bytes(), b'outside fixture must stay untouched')

    def test_selected_source_symlink_ancestor_is_refused(self):
        outside = self.base / 'outside-docs'
        (self.root / 'docs').rename(outside)
        (self.root / 'docs').symlink_to(outside, target_is_directory=True)
        self.assert_refused(lambda: packages.collect_package_files(self.root))
        self.assertEqual((outside / 'guide.md').read_bytes(), self.files['docs/guide.md'])

    def test_index_symlink_mode_is_refused_even_if_working_file_is_regular(self):
        selected = self.root / 'docs/guide.md'
        selected.unlink()
        selected.symlink_to('../LICENSE')
        self.git('add', 'docs/guide.md')
        selected.unlink()
        selected.write_bytes(b'now regular but not indexed as regular\n')
        with self.assertRaisesRegex(ValueError, 'indexed regular file: docs/guide.md'):
            packages.collect_package_files(self.root)

    def test_generated_binary_symlink_is_refused(self):
        selected = self.root / packages.BINARY_PATHS[0]
        selected.unlink()
        selected.symlink_to(self.root / 'LICENSE')
        self.assert_refused(lambda: packages.collect_package_files(self.root))

    def test_generated_binary_symlink_ancestor_is_refused(self):
        outside = self.base / 'outside-bin'
        (self.root / 'build/bin').rename(outside)
        (self.root / 'build/bin').symlink_to(outside, target_is_directory=True)
        self.assert_refused(lambda: packages.collect_package_files(self.root))

    def test_selected_fifo_directory_and_missing_file_are_refused(self):
        selected = self.root / 'docs/guide.md'
        selected.unlink()
        os.mkfifo(selected)
        self.assert_refused(lambda: packages.collect_package_files(self.root))
        selected.unlink()
        selected.mkdir()
        self.assert_refused(lambda: packages.collect_package_files(self.root))
        selected.rmdir()
        self.assert_refused(lambda: packages.collect_package_files(self.root))

    def test_reset_removes_only_owned_output_and_preserves_link_targets(self):
        self.write('build/rpm/virmill/RPMS/old.rpm', b'stale owned output')
        sibling = self.write('build/rpm/unrelated/keep', b'keep sibling')
        outside = self.base / 'outside-output'
        outside.mkdir()
        (outside / 'keep').write_bytes(b'keep target')
        (self.root / 'build/rpm/virmill/link').symlink_to(outside, target_is_directory=True)
        output = packages.reset_owned_directory(self.root, 'build/rpm/virmill')
        self.assertEqual(list(output.iterdir()), [])
        self.assertEqual(sibling.read_bytes(), b'keep sibling')
        self.assertEqual((outside / 'keep').read_bytes(), b'keep target')
        with self.assertRaisesRegex(ValueError, 'unowned'):
            packages.reset_owned_directory(self.root, 'build/rpm/unrelated')

    def test_reset_refuses_symlink_root_and_ancestor(self):
        outside = self.base / 'outside-output'
        outside.mkdir()
        (outside / 'keep').write_bytes(b'keep target')
        (self.root / 'build/rpm').mkdir()
        (self.root / 'build/rpm/virmill').symlink_to(outside, target_is_directory=True)
        self.assert_refused(lambda: packages.reset_owned_directory(self.root, 'build/rpm/virmill'))
        (self.root / 'build/rpm/virmill').unlink()
        (self.root / 'build/rpm').rmdir()
        (self.root / 'build/rpm').symlink_to(outside, target_is_directory=True)
        self.assert_refused(lambda: packages.reset_owned_directory(self.root, 'build/rpm/virmill'))
        self.assertEqual((outside / 'keep').read_bytes(), b'keep target')

    def test_exact_rpm_copy_excludes_stale_siblings_and_requires_expected_output(self):
        expected = 'virmill-0.0.0-0.dev.x86_64.rpm'
        source = self.write('build/rpm/virmill/RPMS/x86_64/' + expected, b'expected rpm fixture')
        self.write('build/rpm/virmill/RPMS/other/old.rpm', b'stale rpm fixture')
        retained = self.write('dist/retained.rpm', b'unrelated retained output')
        packages.copy_expected_rpm(self.root, 'virmill')
        self.assertEqual((self.root / 'dist' / expected).read_bytes(), b'expected rpm fixture')
        self.assertFalse((self.root / 'dist/old.rpm').exists())
        self.assertEqual(retained.read_bytes(), b'unrelated retained output')
        source.unlink()
        # An existing destination and an unrelated RPM cannot mask missing output.
        with self.assertRaises(FileNotFoundError):
            packages.copy_expected_rpm(self.root, 'virmill')

    def test_exact_output_inventory_excludes_stale_and_refuses_any_missing_output(self):
        for filename in packages.PACKAGE_ARTIFACTS:
            self.write('dist/' + filename, filename.encode())
        stale = self.write('dist/stale.rpm', b'stale output')
        self.write('dist/stale.deb', b'stale output')
        self.write('build/bin/stale', b'stale output')
        self.assertEqual([item['path'] for item in packages.package_artifacts(self.root)], list(packages.PACKAGE_ARTIFACTS))
        expected = set(packages.BINARY_PATHS) | {'dist/' + name for name in packages.PACKAGE_ARTIFACTS}
        self.assertEqual(set(reproducibility.collect_outputs(self.root)), expected)
        for path in sorted(expected):
            with self.subTest(missing=path):
                data = (self.root / path).read_bytes()
                (self.root / path).unlink()
                with self.assertRaises(FileNotFoundError):
                    reproducibility.collect_outputs(self.root)
                if path.startswith('dist/'):
                    with self.assertRaises(FileNotFoundError):
                        packages.package_artifacts(self.root)
                self.write(path, data)
        self.assertEqual(stale.read_bytes(), b'stale output')

    def test_output_file_and_directory_symlinks_are_refused(self):
        outside = self.base / 'outside-package'
        outside.write_bytes(b'preserve outside output')
        (self.root / 'dist').mkdir()
        (self.root / 'dist/checksums.json').symlink_to(outside)
        self.assert_refused(lambda: packages.write_regular(self.root, 'dist/checksums.json', b'no'))
        (self.root / 'dist/checksums.json').unlink()
        (self.root / 'dist').rmdir()
        (self.root / 'dist').symlink_to(self.base, target_is_directory=True)
        self.assert_refused(lambda: packages.write_regular(self.root, 'dist/checksums.json', b'no'))
        self.assertEqual(outside.read_bytes(), b'preserve outside output')

    def test_package_control_archive_retains_original_layout(self):
        files = {'usr/bin/fixture': (b'original fixture bytes', 0o755)}
        data = packages.tar_bytes(files, 123)
        with tarfile.open(fileobj=io.BytesIO(data), mode='r:xz') as archive:
            self.assertEqual(archive.getnames(), ['usr/bin/fixture'])
            entry = archive.getmember('usr/bin/fixture')
            self.assertEqual((entry.mode, entry.mtime), (0o755, 123))
            self.assertEqual(archive.extractfile(entry).read(), b'original fixture bytes')
        ar = packages.ar_bytes([('debian-binary', b'2.0\n'), ('data.tar.xz', data)], 123)
        self.assertTrue(ar.startswith(b'!<arch>\ndebian-binary/'))

    def test_package_orchestration_reports_only_four_new_expected_artifacts(self):
        stale = self.write('dist/retained.rpm', b'preserve unrelated artifact')
        for name in packages.PACKAGE_NAMES:
            self.write(f'build/rpm/{name}/RPMS/x86_64/old.rpm', b'old output')
            self.write(f'build/package-stage/{name}/old', b'old stage')
        original_run = subprocess.run
        invoked = []

        def fixture_rpmbuild(command, **kwargs):
            if command[0] != 'rpmbuild':
                return original_run(command, **kwargs)
            name = Path(command[-1]).stem
            invoked.append(name)
            self.assertFalse((self.root / f'build/rpm/{name}/RPMS/x86_64/old.rpm').exists())
            self.assertFalse((self.root / f'build/package-stage/{name}/old').exists())
            self.write(f'build/rpm/{name}/RPMS/x86_64/{name}-0.0.0-0.dev.x86_64.rpm', b'original fake RPM output: ' + name.encode())
            self.write(f'build/rpm/{name}/RPMS/other/unexpected.rpm', b'not selected')
            return subprocess.CompletedProcess(command, 0, 'test-only rpmbuild transport seam\n', '')

        # Only the rpmbuild invocation is replaced. Actual input selection,
        # temporary staging, DEB assembly, manifests and checksums all execute.
        with mock.patch.object(packages.subprocess, 'run', side_effect=fixture_rpmbuild):
            report = packages.build_packages(self.root)
        self.assertEqual(invoked, list(packages.PACKAGE_NAMES))
        checksums = json.loads((self.root / 'dist/checksums.json').read_text())
        self.assertEqual(checksums['artifacts'], report['built'])
        self.assertEqual([item['path'] for item in report['built']], list(packages.PACKAGE_ARTIFACTS))
        self.assertFalse(report['releaseQualified'])
        self.assertFalse(report['installed'])
        self.assertFalse(report['published'])
        self.assertFalse(checksums['signed'])
        self.assertEqual(stale.read_bytes(), b'preserve unrelated artifact')
        self.assertFalse((self.root / 'dist/unexpected.rpm').exists())
        for item in report['built']:
            self.assertEqual(item['sha256'], hashlib.sha256((self.root / 'dist' / item['path']).read_bytes()).hexdigest())
        for name in packages.PACKAGE_NAMES:
            stage = self.root / f'build/package-stage/{name}'
            manifest = json.loads((stage / 'install-manifest.json').read_text())
            self.assertFalse(manifest['releaseQualified'])
            for item in manifest['files']:
                self.assertEqual(item['sha256'], hashlib.sha256((stage / item['path']).read_bytes()).hexdigest())

    def test_successful_rpmbuild_exit_without_expected_output_is_failure(self):
        expected = 'virmill-0.0.0-0.dev.x86_64.rpm'
        self.write('build/rpm/virmill/RPMS/x86_64/' + expected, b'old source RPM')
        retained = self.write('dist/' + expected, b'old destination RPM')
        original_run = subprocess.run

        def missing_rpmbuild(command, **kwargs):
            if command[0] != 'rpmbuild':
                return original_run(command, **kwargs)
            return subprocess.CompletedProcess(command, 0, 'no expected output\n', '')

        with mock.patch.object(packages.subprocess, 'run', side_effect=missing_rpmbuild):
            with self.assertRaises(FileNotFoundError):
                packages.build_packages(self.root)
        self.assertFalse((self.root / 'dist/checksums.json').exists())
        self.assertFalse((self.root / 'build/package-stage/virmill/install-manifest.json').exists())
        self.assertEqual(retained.read_bytes(), b'old destination RPM')

    def test_rpm_literals_preserve_spaces_and_shell_metacharacters(self):
        stage = self.root / "a space ' quote $(literal) `literal`;end"
        lines = packages.rpm_install_lines(stage).splitlines()
        self.assertEqual(lines[0], 'mkdir -p "$RPM_BUILD_ROOT"')
        self.assertEqual(shlex.split(lines[1]), ['cp', '-a', '--', str(stage) + '/.', '$RPM_BUILD_ROOT/'])
        self.assertEqual(packages.rpm_file_path('/usr/share/a "quote" \\ file'),
                         '"/usr/share/a \\"quote\\" \\\\ file"')
        self.write('docs/a space.md', b'ordinary named fixture')
        self.git('add', 'docs/a space.md')
        self.assertIn('usr/share/doc/virmill/a space.md', packages.collect_package_files(self.root)['virmill'])

    def test_rpm_macro_and_control_paths_are_refused_before_packaging(self):
        for bad in ('percent%name', 'macro%{literal}', 'new\nline', 'carriage\rreturn', 'tab\tname', 'delete\x7fname'):
            with self.subTest(path=bad):
                with self.assertRaisesRegex(ValueError, 'RPM macro/control'):
                    packages.rpm_file_path('/usr/share/' + bad)
                with self.assertRaisesRegex(ValueError, 'RPM macro/control'):
                    packages.rpm_install_lines(self.root / bad)
                with self.assertRaisesRegex(ValueError, 'RPM macro/control'):
                    packages.collect_package_files(self.root / bad)
        self.write('docs/macro%{literal}.md')
        self.git('add', 'docs/macro%{literal}.md')
        with self.assertRaisesRegex(ValueError, 'RPM macro/control'):
            packages.collect_package_files(self.root)


if __name__ == '__main__':
    unittest.main(verbosity=2)
