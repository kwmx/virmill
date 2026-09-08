#!/usr/bin/env python3
"""Pure installed-document transformations; no packages or host operations."""
from pathlib import Path
import sys
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import package_docs


class PackageDocuments(unittest.TestCase):
    def setUp(self):
        self.mapping = {
            'docs/guide.md': 'usr/share/doc/virmill/guide.md',
            'docs/reviews/review.md': 'usr/share/doc/virmill/reviews/review.md',
            'docs/adr/0020-cold-recovery-boundary.md': 'usr/share/doc/virmill/adr/0020-cold-recovery-boundary.md',
            'examples/creation/prepared-disks.json': 'usr/share/virmill/examples/creation/prepared-disks.json',
            'schemas/auxiliary-inspect-input.schema.json': 'usr/share/virmill/schemas/auxiliary-inspect-input.schema.json',
            'virmill-v1-spec/docs/12-plugin-protocol.md': 'usr/share/doc/virmill/virmill-v1-spec/docs/12-plugin-protocol.md',
        }
        self.tracked = set(self.mapping) | {
            'docs/evidence/logs/native-001.log',
            'internal/creating/service_linux.go',
            'tests/fixtures/protection/probe.py',
            'vendor/libvirt.org/go/libvirt/domain.go',
            'contracts/dependencies.lock.json',
            'go.mod',
        }

    def rewrite(self, text, source='docs/guide.md'):
        data = text.encode() if isinstance(text, str) else text
        return package_docs.rewrite_markdown(source, self.mapping[source], data,
                                             self.mapping, self.tracked).decode()

    def test_observed_example_relocation_preserves_input_and_inventory(self):
        original = b'Copy [the disk-only example](../examples/creation/prepared-disks.json).\n'
        mapping, tracked = dict(self.mapping), set(self.tracked)
        expected = 'Copy [the disk-only example](../../virmill/examples/creation/prepared-disks.json).\n'
        self.assertEqual(self.rewrite(original), expected)
        self.assertEqual(self.rewrite(original), expected)
        self.assertEqual(original, b'Copy [the disk-only example](../examples/creation/prepared-disks.json).\n')
        self.assertEqual(self.mapping, mapping)
        self.assertEqual(self.tracked, tracked)

    def test_observed_schema_and_source_root_document_detour(self):
        source = 'docs/reviews/review.md'
        cases = (
            ('[schema](../../schemas/auxiliary-inspect-input.schema.json)',
             '[schema](../../../virmill/schemas/auxiliary-inspect-input.schema.json)'),
            ('[ADR](../../docs/adr/0020-cold-recovery-boundary.md#stopped)',
             '[ADR](../adr/0020-cold-recovery-boundary.md#stopped)'),
        )
        for original, expected in cases:
            with self.subTest(original=original):
                self.assertEqual(self.rewrite(original, source), expected)

    def test_explicit_installed_protocol_dependency(self):
        self.assertEqual(self.rewrite('[protocol](../virmill-v1-spec/docs/12-plugin-protocol.md#transport)'),
                         '[protocol](virmill-v1-spec/docs/12-plugin-protocol.md#transport)')

    def test_omitted_tracked_artifacts_are_explicit_nonlinks(self):
        cases = (
            ('evidence/logs/native-001.log', 'docs/evidence/logs/native-001.log'),
            ('../internal/creating/service_linux.go', 'internal/creating/service_linux.go'),
            ('../tests/fixtures/protection/probe.py', 'tests/fixtures/protection/probe.py'),
            ('../vendor/libvirt.org/go/libvirt/domain.go', 'vendor/libvirt.org/go/libvirt/domain.go'),
            ('../contracts/dependencies.lock.json', 'contracts/dependencies.lock.json'),
            ('../go.mod', 'go.mod'),
        )
        for destination, reference in cases:
            with self.subTest(destination=destination):
                self.assertEqual(self.rewrite('[original `label`](' + destination + ')'),
                                 'original `label` (source checkout: `' + reference + '`)')

    def test_observed_source_line_suffix_keeps_line_visible(self):
        self.assertEqual(self.rewrite('[Review](../internal/creating/service_linux.go:649)'),
                         'Review (source checkout: `internal/creating/service_linux.go:649`)')
        self.assertEqual(self.rewrite('[ADR](adr/0020-cold-recovery-boundary.md:12#capture)'),
                         '[ADR](adr/0020-cold-recovery-boundary.md#capture) (line 12)')

    def test_exact_colon_filename_is_not_guessed_to_be_a_line(self):
        self.tracked.add('docs/file:12')
        self.assertEqual(self.rewrite('[source](./file:12)'), 'source (source checkout: `docs/file:12`)')

    def test_unknown_or_invalid_line_cannot_become_a_source_reference(self):
        for destination in ('../internal/missing.go:12', '../internal/creating/service_linux.go:0',
                            '../internal/creating/service_linux.go:01', '../internal/creating/service_linux.go:2147483648'):
            with self.subTest(destination=destination):
                with self.assertRaises(ValueError):
                    self.rewrite('[source](' + destination + ')')

    def test_absolute_frozen_checkout_link_has_no_alias(self):
        for destination in ('/home/faisal/project/virmill/docs/guide.md', '/usr/share/doc/virmill/guide.md',
                            '//localhost/etc/passwd', 'file:///etc/passwd', 'C:/docs/guide.md'):
            with self.subTest(destination=destination):
                with self.assertRaises(ValueError):
                    self.rewrite('[target](' + destination + ')')

    def test_missing_and_escaping_source_targets_fail(self):
        for destination in ('missing.md', '../../outside.md', '../%2e%2e/outside.md',
                            '../unknown/root', './%2Fetc/passwd', '%2Fetc/passwd'):
            with self.subTest(destination=destination):
                with self.assertRaises(ValueError):
                    self.rewrite('[target](' + destination + ')')

    def test_url_decoding_and_parentheses_are_canonicalized(self):
        self.mapping['examples/a (copy).json'] = 'usr/share/virmill/examples/a (copy).json'
        self.tracked.add('examples/a (copy).json')
        for destination in ('<../examples/a (copy).json>', '../examples/a%20%28copy%29.json'):
            with self.subTest(destination=destination):
                angle = destination.startswith('<')
                target = '../../virmill/examples/a%20%28copy%29.json'
                expected = '[file](<' + target + '>)' if angle else '[file](' + target + ')'
                self.assertEqual(self.rewrite('[file](' + destination + ')'), expected)
        self.mapping['examples/f(x).json'] = 'usr/share/virmill/examples/f(x).json'
        self.tracked.add('examples/f(x).json')
        self.assertEqual(self.rewrite(r'[file](../examples/f\(x\).json)'),
                         '[file](../../virmill/examples/f%28x%29.json)')
        self.assertEqual(self.rewrite('[file](../examples/f(x).json)'),
                         '[file](../../virmill/examples/f%28x%29.json)')

    def test_malformed_and_control_url_escapes_fail(self):
        for destination in ('bad%name.md', 'bad%zz.md', 'bad%ff.md', 'bad%00.md', 'bad%0a.md', 'bad%5cname.md'):
            with self.subTest(destination=destination):
                with self.assertRaises(ValueError):
                    self.rewrite('[target](' + destination + ')')

    def test_source_and_installed_directory_references(self):
        self.assertEqual(self.rewrite('[fixtures](../tests/fixtures/protection)'),
                         'fixtures (source checkout: `tests/fixtures/protection`)')
        self.assertEqual(self.rewrite('[examples](../examples/creation)'),
                         '[examples](../../virmill/examples/creation)')

    def test_ambiguous_directory_mapping_does_not_invent_a_target(self):
        self.mapping['examples/elsewhere.md'] = 'usr/share/doc/virmill/elsewhere.md'
        self.tracked.add('examples/elsewhere.md')
        self.assertEqual(self.rewrite('[examples](../examples)'), 'examples (source checkout: `examples`)')

    def test_remote_and_fragment_links_are_byte_preserved(self):
        original = ('[official](https://libvirt.org/formatdomain.html#tpm-device) '
                    '[http](http://example.invalid/a) [mail](mailto:fixture@example.invalid) '
                    '[section](#stopped) [same]()')
        self.assertEqual(self.rewrite(original), original)

    def test_unsupported_scheme_and_local_query_fail(self):
        for destination in ('javascript:alert(1)', 'https:relative', 'data:text/plain,x', 'guide.md?download=1'):
            with self.subTest(destination=destination):
                with self.assertRaises(ValueError):
                    self.rewrite('[target](' + destination + ')')

    def test_fenced_code_preserves_unresolved_and_external_examples_exactly(self):
        for opening, middle, closing in (('```markdown', '', '```'), ('~~~~', '```\n', '~~~~'),
                                         ('````', '```\n', '`````')):
            with self.subTest(opening=opening):
                code = opening + '\n' + middle + '[not a dependency](/private/no-read)\n[a]: bad\n' + closing + '\n'
                original = code + '[example](../examples/creation/prepared-disks.json)\n'
                expected = code + '[example](../../virmill/examples/creation/prepared-disks.json)\n'
                self.assertEqual(self.rewrite(original), expected)

    def test_inline_code_and_code_inside_labels_keep_original_bytes(self):
        code = '`[literal](missing.md)` and ``a ` [literal](/private/no-read)`` and `multi\nline [x](missing.md)`\n'
        original = code + '[the `code` label](../examples/creation/prepared-disks.json)'
        self.assertEqual(self.rewrite(original), code + '[the `code` label](../../virmill/examples/creation/prepared-disks.json)')

    def test_observed_leading_inline_code_is_not_indented_code(self):
        original = '`SubmitCommand` is defined in [the protocol](../virmill-v1-spec/docs/12-plugin-protocol.md).'
        self.assertEqual(self.rewrite(original),
                         '`SubmitCommand` is defined in [the protocol](virmill-v1-spec/docs/12-plugin-protocol.md).')

    def test_unmatched_backtick_is_prose_and_does_not_hide_a_link(self):
        self.assertEqual(self.rewrite('An unmatched ` [target](adr/0020-cold-recovery-boundary.md)'),
                         'An unmatched ` [target](adr/0020-cold-recovery-boundary.md)')
        with self.assertRaises(ValueError):
            self.rewrite('An unmatched ` [target](missing.md)')

    def test_multiline_and_nested_labels_and_repeated_links(self):
        original = '[CPU [core]\nreview](adr/0020-cold-recovery-boundary.md) [again](adr/0020-cold-recovery-boundary.md)'
        self.assertEqual(self.rewrite(original), original)

    def test_reference_html_and_ambiguous_indented_links_fail(self):
        for original in ('[a]: guide.md\n[a]', '[a][b]', '[a][]', '<a href="guide.md">a</a>',
                         '<img\nsrc="guide.md">', '    [a](guide.md)'):
            with self.subTest(original=original):
                with self.assertRaises(ValueError):
                    self.rewrite(original)

    def test_malformed_links_and_unclosed_code_fail(self):
        for original in ('[a](missing.md', '[a](<guide.md)', '[a](guide.md "title")',
                         '[a](two words.md)', '[a](guide.md\n)', '```\n[a](guide.md)', r'\[a](guide.md)'):
            with self.subTest(original=original):
                with self.assertRaises(ValueError):
                    self.rewrite(original)

    def test_images_are_mapped_or_become_explicit_source_references(self):
        self.assertEqual(self.rewrite('![fixture](../examples/creation/prepared-disks.json)'),
                         '![fixture](../../virmill/examples/creation/prepared-disks.json)')
        self.assertEqual(self.rewrite('![fixture](../tests/fixtures/protection/probe.py)'),
                         'fixture (source checkout: `tests/fixtures/protection/probe.py`)')

    def test_origin_and_mapping_must_be_reviewed_and_canonical(self):
        cases = (
            ('other.md', self.mapping['docs/guide.md'], self.mapping, self.tracked),
            ('docs/guide.md', 'usr/share/doc/other.md', self.mapping, self.tracked),
            ('docs/guide.md', self.mapping['docs/guide.md'], {**self.mapping, 'untracked.md': 'usr/untracked.md'}, self.tracked),
            ('docs/guide.md', self.mapping['docs/guide.md'], {**self.mapping, 'go.mod': self.mapping['docs/guide.md']}, self.tracked),
            ('docs/guide.md', self.mapping['docs/guide.md'], self.mapping, self.tracked | {'../escape'}),
        )
        for source, installed, mapping, tracked in cases:
            with self.subTest(source=source, installed=installed, mapping=mapping):
                with self.assertRaises(ValueError):
                    package_docs.rewrite_markdown(source, installed, b'plain', mapping, tracked)

    def test_input_output_and_inventory_limits_refuse(self):
        with self.assertRaises(ValueError):
            self.rewrite(b'x' * (package_docs.MAX_DOCUMENT_BYTES + 1))
        with self.assertRaises(ValueError):
            self.rewrite(b'\xff')
        with mock.patch.object(package_docs, 'MAX_OUTPUT_BYTES', 8):
            with self.assertRaises(ValueError):
                self.rewrite('[source](../go.mod)')
        with mock.patch.object(package_docs, 'MAX_PATHS', 1):
            with self.assertRaises(ValueError):
                self.rewrite('plain')
        with mock.patch.object(package_docs, 'MAX_INVENTORY_BYTES', 1):
            with self.assertRaises(ValueError):
                self.rewrite('plain')
        with mock.patch.object(package_docs, 'MAX_LINKS', 1):
            with self.assertRaises(ValueError):
                self.rewrite('[a](guide.md) [b](guide.md)')

    def test_no_filesystem_access_and_many_unpaired_brackets(self):
        original = '[' * 10000 + ' [target](adr/0020-cold-recovery-boundary.md)'
        with mock.patch('builtins.open', side_effect=AssertionError('Unexpected filesystem read')):
            self.assertEqual(self.rewrite(original), original)


if __name__ == '__main__':
    unittest.main()
