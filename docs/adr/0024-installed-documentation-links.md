# ADR 0024: Relocate installed documentation links

Status: accepted routine packaging correction. Mandatory documentation, plugin
protocol delivery, source and release evidence remain required in full.

The source checkout and installed package have different layouts. Existing
packaging copied Markdown bytes unchanged, even when examples and schemas moved
to another directory or linked source and evidence logs were intentionally absent.
The frozen 490b88c audit found 1,269 broken installed link occurrences across 31
documents. The same source-relative destinations generally resolve in the source
checkout; those are distinct validation targets.

Keep package source selection bound to indexed regular files and retain existing
source suffix and evidence-log exclusions. During package assembly only, rewrite
local Markdown links using the exact source-to-installed mapping. Links to
packaged files become relative installed links. Known source-only destinations
become explicit source-checkout references with their path and optional line
number. Do not manufacture publication URLs or bundle omitted payloads merely
to make a link resolve. Source documents and historical evidence stay unchanged,
apart from correcting one host-specific absolute source link to a portable path.

The language-neutral plugin protocol is essential installed documentation. Add
the exact public `virmill-v1-spec/docs/12-plugin-protocol.md` as an explicit indexed
input under `/usr/share/doc/virmill/virmill-v1-spec/docs/12-plugin-protocol.md`.
Its logical archive-root name is preserved. The installed author guide must link
to that page, rather than referring plugin authors only to the source checkout.

Transformation is deterministic and does not read link targets. Preserve code,
external links and anchors. Unknown, escaping, absolute local or unsupported
link forms fail the build visibly. A source line suffix becomes visible line
information, rather than a nonexistent installed filename. Checks must cover
the source and installed link graphs separately, including exact transformed
bytes in package manifests and reproducibility comparisons.

No executable, schema, authorization policy or publication destination changes.
This corrects installed reference usability; it does not validate every tutorial
or satisfy REL-03 without its remaining command-help and workflow evidence.
