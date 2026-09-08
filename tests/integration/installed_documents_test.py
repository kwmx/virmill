#!/usr/bin/env python3
"""Check actual staged package bytes and local Markdown targets independently.

Reads only build/package-stage and the explicit public protocol source. Does not
import the transformer, install packages, start services or resolve host paths.
"""
import hashlib
import contextlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import posixpath
import re
import stat
import tempfile
import unittest
from unittest import mock
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[2]


def destinations(text):
    rows, fence = [], None
    for line in text.splitlines(keepends=True):
        match = re.match(r'^ {0,3}(`{3,}|~{3,})(.*)$', line)
        if fence:
            if match and match[1][0] == fence[0] and len(match[1]) >= len(fence) and not match[2].strip():
                fence = None
            rows.append('\n' if line.endswith('\n') else '')
        elif match:
            fence = match[1]
            rows.append('\n' if line.endswith('\n') else '')
        else:
            rows.append(line)
    if fence:
        raise ValueError('Unclosed fenced code block')
    visible = ''.join(rows)
    if re.search(r'(?im)^ {0,3}\[[^\]]+\]:|<(?:a|img)\b[^>]*(?:href|src)\s*=', visible):
        raise ValueError('Unsupported reference/HTML link')
    # This independent audit deliberately supports the actual installed corpus,
    # with conservative refusal if more complex inline syntax is introduced.
    visible = re.sub(r'(`+)([^\n]*?)\1', lambda m: ' ' * len(m[0]), visible)
    found = re.findall(r'\[[^\]]*\]\((<[^>\n]*>|[^()\s]+)\)', visible)
    if len(found) != visible.count(']('):
        raise ValueError('Unsupported or malformed inline destination')
    return [v.removeprefix('<').removesuffix('>') for v in found]


def local_target(origin, url):
    parsed = urlsplit(url)
    if parsed.scheme:
        if (parsed.scheme in ('http', 'https') and parsed.netloc) or (parsed.scheme == 'mailto' and parsed.path):
            return None
        raise ValueError('Unsupported link scheme')
    if parsed.netloc or parsed.path.startswith('/') or parsed.query:
        raise ValueError('Nonportable local destination')
    if not parsed.path:
        return None
    path = unquote(parsed.path, errors='strict')
    if re.search(r'%(?![0-9a-fA-F]{2})', parsed.path) or path.startswith('/') or '\\' in path or any(ord(c)<32 for c in path):
        raise ValueError('Unsafe encoded destination')
    target = posixpath.normpath(posixpath.join(posixpath.dirname(origin), path))
    if target == '..' or target.startswith('../'):
        raise ValueError('Installed target escapes package root')
    return target


class ParserTests(unittest.TestCase):
    def test_code_is_not_a_dependency(self):
        self.assertEqual(destinations('```markdown\n[x](/missing)\n```\n`[x](missing)` [guide](next.md)'), ['next.md'])

    def test_nested_fence_and_multiline_label(self):
        self.assertEqual(destinations('~~~~\n```\n[x](missing)\n~~~~\n[first\nsecond](next.md#part)'), ['next.md#part'])

    def test_unsupported_syntax_refuses(self):
        for text in ('[x](two words.md)', '[x]: next.md\n[x]', '<a href="next.md">x</a>', '```\nunclosed'):
            with self.subTest(text=text), self.assertRaises(ValueError):
                destinations(text)

    def test_resolution_uses_virtual_package_root(self):
        origin = 'usr/share/doc/virmill/guide.md'
        self.assertEqual(local_target(origin, '../../virmill/schemas/a%20b.json#part'), 'usr/share/virmill/schemas/a b.json')
        for url in ('/etc/passwd', '//localhost/etc/passwd', 'file:///etc/passwd', '%2fetc/passwd', '../../../../../../outside'):
            with self.subTest(url=url), self.assertRaises(ValueError):
                local_target(origin, url)

    def test_generated_manifest_with_excluded_log_is_rejected(self):
        # A matching manifest hash must not excuse a forbidden installed log.
        # This is an original tiny package-shaped fixture, not a real package.
        with tempfile.TemporaryDirectory(prefix='virmill-doc-observer-') as directory:
            root = Path(directory)
            protocol_path = 'usr/share/doc/virmill/virmill-v1-spec/docs/12-plugin-protocol.md'
            source = root/'virmill-v1-spec/docs/12-plugin-protocol.md'
            source.parent.mkdir(parents=True)
            source.write_bytes(b'# Generated public protocol fixture\n')
            payloads = {
                'virmill': {
                    protocol_path: source.read_bytes(),
                    'usr/share/doc/virmill/plugin-author-guide.md': b'[protocol](virmill-v1-spec/docs/12-plugin-protocol.md)\n',
                },
                'virmill-host-helper': {'usr/share/doc/virmill-host-helper/fixture.json': b'{"generated":true}\n'},
            }
            def write_stage():
                for package, files in payloads.items():
                    stage = root/'build/package-stage'/package
                    records = []
                    for path, data in files.items():
                        target = stage/path
                        target.parent.mkdir(parents=True, exist_ok=True)
                        target.write_bytes(data); target.chmod(0o644)
                        records.append({'path':path,'mode':0o644,'sha256':hashlib.sha256(data).hexdigest()})
                    (stage/'install-manifest.json').write_text(json.dumps({
                        'package':package,'releaseQualified':False,'files':records}))
            with mock.patch.dict(globals(), ROOT=root), contextlib.redirect_stdout(io.StringIO()):
                write_stage()
                InstalledDocuments().test_staged_payload_integrity_and_document_closure()
                payloads['virmill']['usr/share/doc/virmill/evidence/logs/forbidden.json'] = b'{"generatedLog":true}\n'
                write_stage()
                with self.assertRaises(AssertionError):
                    InstalledDocuments().test_staged_payload_integrity_and_document_closure()


class InstalledDocuments(unittest.TestCase):
    def test_staged_payload_integrity_and_document_closure(self):
        budget = 512 << 20
        def read(path, limit=128 << 20):
            nonlocal budget
            self.assertTrue(all(not p.is_symlink() for p in (path, *path.parents)))
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
            with os.fdopen(fd, 'rb') as stream:
                before = os.fstat(stream.fileno())
                self.assertTrue(stat.S_ISREG(before.st_mode))
                self.assertLessEqual(before.st_size, limit)
                data = stream.read(limit+1)
                after = os.fstat(stream.fileno())
                signature = lambda s: (s.st_dev,s.st_ino,s.st_mode,s.st_nlink,s.st_size,s.st_mtime_ns,s.st_ctime_ns)
                self.assertEqual(signature(before), signature(after))
                self.assertEqual(signature(after), signature(path.lstat()))
            self.assertEqual(len(data), before.st_size)
            budget -= len(data); self.assertGreaterEqual(budget, 0)
            return data, before.st_mode & 0o7777
        installed, markdown, manifest_hashes = set(), {}, {}
        for package in ('virmill', 'virmill-host-helper'):
            stage = ROOT/'build/package-stage'/package
            raw, _ = read(stage/'install-manifest.json', 1 << 20)
            manifest_hashes[package] = hashlib.sha256(raw).hexdigest()
            manifest = json.loads(raw)
            self.assertEqual(manifest['package'], package)
            self.assertIs(manifest['releaseQualified'], False)
            self.assertLessEqual(len(manifest['files']), 4096)
            expected = {'install-manifest.json'}
            for entry in manifest['files']:
                path = entry['path']; parts = PurePosixPath(path)
                self.assertFalse(parts.is_absolute())
                self.assertEqual(str(parts), path)
                self.assertNotIn('..', parts.parts)
                self.assertNotIn(path, installed)
                self.assertNotIn(path, expected)
                data, mode = read(stage/path)
                self.assertEqual(hashlib.sha256(data).hexdigest(), entry['sha256'], path)
                self.assertEqual(mode, entry['mode'], path)
                installed.add(path); expected.add(path)
                if path.endswith('.md'):
                    self.assertLessEqual(len(data), 8 << 20)
                    markdown[path] = data.decode('utf-8')
            actual = set(); count = 0
            for base, dirs, files in os.walk(stage, followlinks=False):
                count += len(dirs)+len(files); self.assertLessEqual(count, 16384)
                self.assertTrue(all(not (Path(base)/p).is_symlink() for p in dirs+files))
                actual.update((Path(base)/p).relative_to(stage).as_posix() for p in files)
            self.assertEqual(actual, expected)
        nodes = installed | {str(p) for path in installed for p in PurePosixPath(path).parents if str(p)!='.'}
        self.assertTrue(markdown)
        local = remote = references = 0
        for origin, text in markdown.items():
            links = destinations(text)
            self.assertLessEqual(len(links), 20000)
            references += text.count('(source checkout: ')
            for url in links:
                target = local_target(origin, url)
                if target is None:
                    remote += 1
                else:
                    local += 1
                    self.assertIn(target, nodes, origin+' -> '+url)
        protocol = 'usr/share/doc/virmill/virmill-v1-spec/docs/12-plugin-protocol.md'
        source, _ = read(ROOT/'virmill-v1-spec/docs/12-plugin-protocol.md', 1 << 20)
        self.assertEqual(markdown[protocol].encode(), source)
        author = 'usr/share/doc/virmill/plugin-author-guide.md'
        self.assertIn(protocol, [local_target(author, v) for v in destinations(markdown[author])])
        self.assertGreater(local, 0)
        self.assertFalse(any(path.startswith('usr/share/doc/virmill/evidence/logs/') for path in installed))
        print(json.dumps({'status':'passed','manifestSHA256':manifest_hashes,'payloadFiles':len(installed),
              'markdownFiles':len(markdown),'resolvedLocalLinks':local,'externalOrFragmentLinks':remote,
              'explicitSourceReferences':references,'missingLocalTargets':0,'essentialProtocolInstalled':True,
              'scope':'staged hashes/modes/inventory and local file targets; no fragment, URL availability, tutorial, renderer or host installation qualification'},sort_keys=True))


if __name__ == '__main__':
    unittest.main()
