#!/usr/bin/env python3
"""Beta version policy, using temporary files and a synthetic RPM builder only.

No native package builder, installed package, daemon, Git command or VM is used.
The build-info runtime check uses the pinned Go toolchain with no dependencies.
"""
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import package as packages


class BetaVersion(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='virmill-beta-version-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = b'package buildinfo\n\nconst Version = "1.0.0-beta.2"\n'
        self.write(packages.VERSION_SOURCE, self.source)
        self.files = {
            name: {'usr/share/' + name + '/fixture': (b'original generated fixture', 0o644)}
            for name in packages.PACKAGE_NAMES
        }

    def write(self, relative, data):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        return path

    def build(self, runner):
        with mock.patch.object(packages, 'tracked_inputs', return_value={packages.VERSION_SOURCE: b'100644'}), \
                mock.patch.object(packages, 'collect_package_files', return_value=self.files), \
                mock.patch.object(packages.subprocess, 'run', side_effect=runner), \
                mock.patch.dict(os.environ, {'SOURCE_DATE_EPOCH': '123'}):
            return packages.build_packages(self.root)

    def test_canonical_product_and_exact_package_identities(self):
        self.assertEqual(packages.PRODUCT_VERSION, '1.0.0-beta.2')
        self.assertEqual(packages.DEBIAN_VERSION, '1.0.0~beta.2')
        self.assertEqual((packages.RPM_VERSION, packages.RPM_RELEASE), ('1.0.0', '0.beta.2'))
        self.assertEqual(packages.PACKAGE_ARTIFACTS, (
            'virmill-1.0.0-0.beta.2.x86_64.rpm',
            'virmill-host-helper-1.0.0-0.beta.2.x86_64.rpm',
            'virmill-host-helper_1.0.0~beta.2_amd64.deb',
            'virmill_1.0.0~beta.2_amd64.deb',
        ))
        for name, kind in (('unknown', 'rpm'), ('../virmill', 'deb'), ('virmill', 'tar')):
            with self.subTest(name=name, kind=kind), self.assertRaises(ValueError):
                packages.package_filename(name, kind)

    def test_literal_beta_parser_refuses_ambiguous_or_unreviewed_versions(self):
        invalid = {
            'missing': b'package buildinfo\n',
            'mutable': self.source.replace(b'const Version', b'var Version'),
            'duplicate': self.source + b'const Version = "1.0.0-beta.3"\n',
            'extra variable': self.source + b'var Version = "1.0.0-beta.2"\n',
            'expression': self.source.replace(b'"1.0.0-beta.2"', b'os.Getenv("VERSION")'),
            'concatenation': self.source.replace(b'"1.0.0-beta.2"', b'"1.0.0-" + "beta.2"'),
            'non-UTF8': self.source + b'\xff',
            'oversize': self.source + b' ' * 16384,
        }
        for version in ('1.0.0', '0.0.0-dev', '1.0.0-rc.1', '1.0.0-beta.0',
                        '01.0.0-beta.2', '1.00.0-beta.2', '1.0.0-beta.01',
                        '1.0.0-beta.2+metadata', '1.0.0-beta.2\nRelease: 1',
                        '1.0.0-beta.%{evil}', '1.0.0-beta.9999999999'):
            invalid[version] = self.source.replace(b'1.0.0-beta.2', version.encode())
        for name, data in invalid.items():
            with self.subTest(name=name), self.assertRaises((UnicodeError, ValueError)):
                packages.parse_product_version(data)
        # Later beta numbers derive all native fields from the same literal.
        self.assertEqual(packages.parse_product_version(self.source), '1.0.0-beta.2')
        self.assertEqual(packages.beta_version_parts('2.3.4-beta.12'),
                         ('2.3.4~beta.12', '2.3.4', '0.beta.12'))

    def test_environment_cannot_select_product_or_package_version(self):
        env = {'VERSION': '9.9.9', 'PRODUCT_VERSION': '9.9.9', 'VIRMILL_VERSION': '9.9.9',
               'DEBIAN_VERSION': '9.9.9', 'RPM_VERSION': '9.9.9', 'RPM_RELEASE': '99'}
        with mock.patch.dict(os.environ, env):
            spec = importlib.util.spec_from_file_location('beta_version_package_fixture', ROOT / 'scripts/package.py')
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
        self.assertEqual(module.PRODUCT_VERSION, '1.0.0-beta.2')
        self.assertEqual(module.PACKAGE_ARTIFACTS, packages.PACKAGE_ARTIFACTS)

    def test_source_root_version_refusal_precedes_output_or_builder(self):
        retained = self.write('dist/retained', b'untouched prior artifact')
        variants = {
            'different source': (b'100644', self.source.replace(b'beta.2', b'beta.3')),
            'untracked': (None, self.source),
            'indexed symlink': (b'120000', self.source),
            'malformed': (b'100644', self.source.replace(b'const Version', b'var Version')),
            'missing': (b'100644', None),
        }
        for label, (mode, data) in variants.items():
            with self.subTest(label=label):
                source = self.root / packages.VERSION_SOURCE
                if data is None:
                    source.unlink()
                else:
                    source.write_bytes(data)
                tracked = {packages.VERSION_SOURCE: mode} if mode else {}
                before = {str(p.relative_to(self.root)): p.read_bytes()
                          for p in self.root.rglob('*') if p.is_file()}
                with mock.patch.object(packages, 'tracked_inputs', return_value=tracked), \
                        mock.patch.object(packages, 'collect_package_files') as collect, \
                        mock.patch.object(packages, 'reset_owned_directory') as reset, \
                        mock.patch.object(packages, 'write_regular') as write, \
                        mock.patch.object(packages.subprocess, 'run') as run:
                    with self.assertRaises((ValueError, FileNotFoundError)):
                        packages.build_packages(self.root)
                    for operation in (collect, reset, write, run):
                        operation.assert_not_called()
                self.assertEqual(before, {str(p.relative_to(self.root)): p.read_bytes()
                                          for p in self.root.rglob('*') if p.is_file()})
                self.assertEqual(retained.read_bytes(), b'untouched prior artifact')

    def test_version_source_symlink_is_not_followed(self):
        source = self.root / packages.VERSION_SOURCE
        source.unlink()
        target = self.write('original-version.go', self.source)
        source.symlink_to(target)
        with mock.patch.object(packages, 'tracked_inputs', return_value={packages.VERSION_SOURCE: b'100644'}), \
                mock.patch.object(packages, 'collect_package_files') as collect:
            with self.assertRaises(ValueError):
                packages.build_packages(self.root)
            collect.assert_not_called()
        self.assertEqual(target.read_bytes(), self.source)
        self.assertFalse((self.root / 'dist').exists())

    def test_synthetic_archives_specs_and_receipts_keep_beta_unqualified(self):
        specs = {}

        def rpm_fixture(command, **kwargs):
            self.assertEqual(command[0], 'rpmbuild')
            spec_path = Path(command[-1])
            name = spec_path.stem
            specs[name] = spec_path.read_text()
            filename = packages.package_filename(name, 'rpm')
            self.write(f'build/rpm/{name}/RPMS/x86_64/{filename}', b'synthetic RPM: ' + name.encode())
            self.assertEqual(kwargs['env']['SOURCE_DATE_EPOCH'], '123')
            return subprocess.CompletedProcess(command, 0, 'synthetic builder only\n', '')

        report = self.build(rpm_fixture)
        self.assertEqual(set(specs), set(packages.PACKAGE_NAMES))
        self.assertFalse(report['releaseQualified'])
        self.assertFalse(report['installed'])
        self.assertFalse(report['published'])
        checksums = json.loads((self.root / 'dist/checksums.json').read_text())
        self.assertFalse(checksums['releaseQualified'])
        self.assertFalse(checksums['signed'])
        self.assertEqual(checksums['artifacts'], report['built'])
        self.assertEqual([v['path'] for v in report['built']], list(packages.PACKAGE_ARTIFACTS))
        for record in report['built']:
            self.assertEqual(record['sha256'], hashlib.sha256((self.root / 'dist' / record['path']).read_bytes()).hexdigest())
        for name in packages.PACKAGE_NAMES:
            self.assertIn('\nVersion: 1.0.0\nRelease: 0.beta.2\n', specs[name])
            self.assertIn('Virmill 1.0.0-beta.2 owner-test beta; incomplete and not release-qualified', specs[name])
            deb = (self.root / 'dist' / packages.package_filename(name, 'deb')).read_bytes()
            self.assertEqual(deb[:8], b'!<arch>\n')
            entries = {}
            position = 8
            while position < len(deb):
                header = deb[position:position + 60]
                self.assertEqual(header[-2:], b'`\n')
                size = int(header[48:58])
                entries[header[:16].decode().strip().removesuffix('/')] = deb[position + 60:position + 60 + size]
                position += 60 + size + size % 2
            self.assertEqual(set(entries), {'debian-binary', 'control.tar.xz', 'data.tar.xz'})
            with tarfile.open(fileobj=io.BytesIO(entries['control.tar.xz']), mode='r:xz') as archive:
                control = archive.extractfile('control').read().decode()
            self.assertIn(f'Package: {name}\nVersion: 1.0.0~beta.2\nArchitecture: amd64\n', control)
            self.assertIn('owner-test beta; incomplete and not release-qualified', control)
            with tarfile.open(fileobj=io.BytesIO(entries['data.tar.xz']), mode='r:xz') as archive:
                self.assertEqual(archive.getnames(), list(self.files[name]))
            manifest = json.loads((self.root / f'build/package-stage/{name}/install-manifest.json').read_text())
            self.assertFalse(manifest['releaseQualified'])
            self.assertEqual(manifest['package'], name)

    def test_old_named_rpm_cannot_satisfy_beta_builder_output(self):
        old = self.write('dist/virmill-0.0.0-0.dev.x86_64.rpm', b'preserve historical artifact')

        def stale_fixture(command, **kwargs):
            name = Path(command[-1]).stem
            self.write(f'build/rpm/{name}/RPMS/x86_64/{name}-0.0.0-0.dev.x86_64.rpm', b'wrong output identity')
            return subprocess.CompletedProcess(command, 0, '', '')

        with self.assertRaises(FileNotFoundError):
            self.build(stale_fixture)
        self.assertEqual(old.read_bytes(), b'preserve historical artifact')
        self.assertFalse((self.root / 'dist/checksums.json').exists())
        self.assertFalse((self.root / 'build/package-stage/virmill/install-manifest.json').exists())

    def test_actual_go_build_info_keeps_version_and_protocol_separate(self):
        # Compile the owned production file in a generated module, without touching
        # build/bin or any frozen checkout and without resolving third-party modules.
        fixture = self.root / 'go-fixture'
        fixture.mkdir()
        (fixture / 'go.mod').write_text('module virmill.local/beta-version-fixture\n\ngo 1.27.1\n')
        (fixture / 'version.go').write_bytes((ROOT / packages.VERSION_SOURCE).read_bytes())
        (fixture / 'version_test.go').write_text('''package buildinfo
import "testing"
func TestBetaInfo(t *testing.T) {
    info := Info()
    if Version != "1.0.0-beta.2" || info["version"] != Version { t.Fatal(info) }
    if info["releaseQualified"] != false { t.Fatal(info) }
    if info["apiVersion"] != "virmill/v1" || info["pluginProtocol"] != "1.0" { t.Fatal(info) }
    if info["revision"] != "uncommitted" || info["buildTime"] != "unknown" { t.Fatal(info) }
}
''')
        env = {**os.environ, 'CGO_ENABLED': '0', 'GOPROXY': 'off', 'GOSUMDB': 'off',
               'GOWORK': 'off', 'GOFLAGS': '', 'VERSION': '9.9.9'}
        result = subprocess.run([str(ROOT / 'scripts/go'), 'test', '-mod=mod', '-count=1', '.'],
                                cwd=fixture, env=env, text=True, capture_output=True, timeout=60)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
