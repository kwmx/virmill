# Installed-document links in auxiliary-490b88c

The frozen development package does not have complete local document links.
Across 102 installed Markdown files, **1,269 link occurrences point to 397
uninstalled paths**, affecting 31 documents. This is a reproducible REL-03
documentation finding. It does not change acceptance status or establish
installation, command/tutorial, native-helper, or release qualification.

REL-03 requires that user/admin/plugin documentation and examples pass validation
and correspond to actual command help; its minimum evidence is documentation CI.
The requirement is in the supplied archive's
`virmill-v1-spec/contracts/requirements.json`. This review checks local file-link
availability, one part of that requirement.

## Frozen inputs and scope

The audit ran locally on 2026-09-08. It used the preserved source root
`build/auxiliary-490b88c` and its two `build/package-stage` trees, without running a
package build, installer, CLI, daemon, helper, SSH, or host/service operation.

| Frozen input | Revision or SHA-256 |
|---|---|
| Revision | `490b88cba5bf6e0837e2cb5780e56211c22a7d27` |
| `build/source-inputs.json` | `f0585986ff6693a306a54e86d48db9eecbed5cddca83941d461d3e2bb2727576` |
| Core `install-manifest.json` | `21daefa8c91ce144a3b586a593f5fd8af911c354455c0572e1b744d7078b0f97` |
| Helper `install-manifest.json` | `d51b177c87d01232460bcef5f5f733dc2d09f778abcfe610c6065fe549e15e7e` |
| Frozen `scripts/package.py` | `a0dad88927b983778a1e40e66b84be67a152f76488ddc3e14942ea5c0f16977f` |

The source inventory contains 2,031 tracked regular-file records. The checker
verified all 174 core and five helper payload files against their manifest hashes
and modes, and checked that each stage contains exactly those payload files plus
its manifest. Both manifests say `releaseQualified: false`. The core contains 100
Markdown documents under `/usr/share/doc/virmill` and two example README files
under `/usr/share/virmill/examples`; the helper contains no Markdown. Their total
Markdown input is 927,487 bytes.

Every audited Markdown file matches its frozen source bytes and inventory digest.
For the local references found in those pages, 440 distinct source target files
were also checked against their frozen inventory hashes, and two source target
directories were checked as ordinary directories. This verifies those references;
it is not a fresh integrity audit of every file in the source archive.

Installed resolution uses only the union of both manifests and their implied
directories, rooted at a virtual `/`. It never resolves a missing package target
against the developer machine's `/usr`, home directory, or repository checkout.

## Exact link counts

There are 1,496 inline Markdown links: 95 external URLs, 132 existing installed
local targets, and 1,269 missing installed local targets. No fragment-only,
reference-definition, HTML-link, autolink, or indented-link forms were observed
in this frozen corpus. Code is excluded from link interpretation. Repeated links
count as separate occurrences; unique targets below are normalized installed
paths.

| Missing target class | Occurrences | Unique targets | Documents |
|---|---:|---:|---:|
| Intentionally excluded `docs/evidence/logs` | 1,150 | 309 | 9 |
| Omitted `internal` implementation/test source | 56 | 36 | 9 |
| Omitted `tests` fixtures and fixture directories | 16 | 11 | 11 |
| Omitted vendored binding source | 7 | 3 | 1 |
| Omitted root `go.mod` and `contracts` | 2 | 2 | 1 |
| Omitted normative specification pages | 4 | 3 | 4 |
| Examples installed at a different relative location | 10 | 9 | 6 |
| Schemas installed at a different relative location | 2 | 2 | 1 |
| Source-root `../../docs` detours | 2 | 2 | 2 |
| Nonportable `:line` suffixes | 19 | 19 | 1 |
| Absolute developer-checkout destination | 1 | 1 | 1 |
| **Total** | **1,269** | **397** | **31 distinct** |

The document counts overlap between classes. By document area, the missing-link
occurrences are 1,106 in `docs/requirements-to-evidence.md`, 54 in evidence
narratives, 93 in reviews, and 16 in other documentation.

Of the missing installed occurrences, **1,249 have an exact existing target in
the frozen source archive**. Their absence or wrong relative location is caused
by package selection/layout. The remaining 20 were already nonportable source
links: 19 use a filename with `:line`, and one uses a developer's absolute
checkout path. All 19 line-suffixed references have a known source file after
removing that suffix, but a normal Markdown renderer treats the suffix as part
of the filename; this audit does not silently grant editor-specific behavior.

## Concrete examples

Line numbers here identify frozen source files, not newly installed line anchors.

* `docs/vm-creation.md:43` links to
  `../examples/creation/prepared-disks.json`. The installed guide resolves that
  to `/usr/share/doc/examples/creation/prepared-disks.json`; the package actually
  provides `/usr/share/virmill/examples/creation/prepared-disks.json`.
  The same class affects NoCloud, acceptance, disk preparation, installation
  preparation, VM configuration, and VM creation examples.
* `docs/reviews/auxiliary-integration-review.md:49–50` links to
  `../../schemas/auxiliary-inspect-input.schema.json` and
  `../../schemas/auxiliary-vm-identity.schema.json`. They resolve under
  `/usr/share/doc/schemas`, while selected schema files live under
  `/usr/share/virmill/schemas`.
* `docs/reviews/cold-capture-endpoint-readiness.md:13` links through
  `../../docs/adr/0020-cold-recovery-boundary.md`. That detour works in the source
  tree but resolves to `/usr/share/doc/docs/adr/...` after installation.
  `docs/reviews/creation-nvram-binding-implementation-review.md:4` has the same
  issue for ADR 0021.
* `docs/plugin-author-guide.md:3` links to the essential protocol page
  `../virmill-v1-spec/docs/12-plugin-protocol.md`. That page is present in the
  contracted source archive and entirely absent from both frozen packages.
* `docs/reviews/swtpm-cold-writer-exclusion.md:183` links to
  `../../tests/fixtures/protection/swtpm-lock-probe/probe.py`; the package selects
  no `tests` tree. The same distinction applies to source-code reviews and
  vendored binding references.
* `docs/reviews/reconcile-integrity-compatibility.md:75` links to
  `../evidence/logs/nvram-reconcile-negative-step-001.log`, which is intentionally
  excluded from installed documentation.
* All 19 `:line` references occur in
  `docs/reviews/creation-nvram-binding-review.md`: 17 implementation filenames,
  one normative specification filename, and one ADR filename.
* `docs/creation-nvram-path-validation.md:37` contains the sole absolute link:
  `<dev-home>/project/virmill/docs/reviews/creation-nvram-binding-review.md`.
  The checker does not open that absolute destination. The parent corrected the
  current source link to `reviews/creation-nvram-binding-review.md`; the frozen
  archive and packages retain their original bytes and this finding.

## Cause and minimum correction

The frozen `scripts/package.py:20–24` defines four source-tree mappings and a
suffix allowlist. `tracked_inputs` requires the exact Git checkout root and
indexed regular-file inputs. `collect_package_files:147–150` copies selected
documentation bytes without changing their source-relative destinations.
The mappings preserve the interior of each selected tree but not the relative
layout between `docs`, `schemas`, `sdk/go`, and `examples`.

The policy deliberately omits evidence logs, untracked/ignored files, and trees
such as implementation, tests, vendor, contracts, and the supplied specification.
Its symlink/regular-file checks and tracked-input restriction address source
selection; they do not establish document-link closure. The frozen
`tests/integration/package_inputs_test.py` tests those selection and path
boundaries but has no installed-document relocation/closure fixture.

The agreed correction leaves the contracted source archive and existing
selection exclusions intact:

1. Rewrite only package Markdown bytes against an exact selected
   source-to-installed mapping. Resolve source-relative paths first, then compute
   the installed relative destination. Preserve fragment text. A mapped `:line`
   reference links to the file and retains a visible `(line N)` annotation.
2. Replace references to known but intentionally omitted source files/directories
   with explicit, non-link source-checkout references preserving label, canonical
   source path, and any line suffix. Do not copy logs, source media, private files,
   or whole implementation/vendor trees merely to make an installed link pass.
3. Ship the public plugin protocol page through an explicit parent-owned mapping
   to `/usr/share/doc/virmill/virmill-v1-spec/docs/12-plugin-protocol.md`. A source-only
   notice would leave an essential installed plugin-document dependency missing.
   This page has no inline links of its own in the frozen source.
4. Refuse unknown local targets, source-root escapes, absolute local destinations,
   and unsupported link forms. Do not guess a publication URL or checkout alias.
   Add integration checks against the selected manifest paths as well as small
   source-to-installed mapping fixtures.

The new pure implementation is `scripts/package_docs.py`, with the interface
`rewrite_markdown(source_path, installed_path, data, installed_by_source,
tracked_sources)`. It performs no filesystem or network reads. The parent owns
packager wiring, protocol selection, source-link correction, architecture record,
and subsequent package qualification.

## Correction checks and limits

`python3 -B tests/integration/package_docs_test.py -v` passed **26 test methods,
zero failures, zero skips**. Tests cover observed relocations, explicit protocol
selection, omitted files and directories, both line-reference dispositions,
absolute/unknown/traversal refusals, URL encoding, code preservation, malformed
or unsupported syntax, mapping consistency, bounds, deterministic input
preservation, and absence of file reads.

A separate in-memory pass through all 102 frozen Markdown documents accepted
101 and refused only the known historical absolute link. Replacing that one
destination in memory with the parent's exact relative correction and supplying
the explicit protocol mapping produced 103 documents, **149 resolved installed
local links, 95 unchanged external links, and 1,252 explicit source-checkout
references**. No frozen source, stage, or package bytes were written. This is
mapping evidence, not an assertion that a corrected package has been built or
installed.

The frozen-corpus pass caught and fixed one new-parser false refusal before
ownership release: masking leading inline code on `docs/uefi-tpm-probe.md:190`
initially made the following link look indented. Indentation detection now uses
the original leading whitespace; an exact regression covers this case.

Implementation and test hashes at ownership release:

* `scripts/package_docs.py`:
  `ee550f6b85ffb7abaf5e0c23237af12f921e64cd55b82f28fb5e15e822476b7d`
* `tests/integration/package_docs_test.py`:
  `f417570cbcb1762497b73c3f003277579c6bb1c5a8077bff401fc85738f84531`

The transformer supports a bounded documented Markdown subset. Reference/HTML
links, link titles, and ambiguous indented links are refused rather than silently
left unresolved. It checks file targets, not fragment existence, source line
accuracy, external URL availability, renderer behavior, example execution, or
help parity. No native package tool or runtime test was executed for this review.
REL-03 and the other release requirements remain unaccepted.

## Reproduce the frozen audit without writing files

The following audit is intentionally independent of the new transformer. Its
input digests are pinned above. It uses only Python's standard library, allows at
most 512 MiB aggregate reads, bounds manifests/documents/paths/links, checks
ordinary files without following symlinks, and rechecks read-file metadata. Its
nine synthetic parser checks pass. Exit zero means that this bounded audit
completed; the printed 1,269 missing occurrences are a failed closure finding,
not a passing installed-document result.

Run from the repository root; this executes only the Python fence below:

~~~sh
python3 -B - <<'PY'
from pathlib import Path
text = Path('docs/reviews/installed-document-links-490b88c.md').read_text()
code = text.split('```python\n', 1)[1].split('\n```', 1)[0]
exec(compile(code, '<frozen-document-link-audit>', 'exec'))
PY
~~~

```python
from pathlib import Path, PurePosixPath
from urllib.parse import unquote, urlsplit
from collections import Counter, defaultdict
import hashlib, json, os, posixpath, re, stat

ROOT = Path("build/auxiliary-490b88c")
STAGE = ROOT / "build/package-stage"
MAP = (("docs", "usr/share/doc/virmill"), ("schemas", "usr/share/virmill/schemas"),
       ("sdk/go", "usr/share/virmill/sdk/go"), ("examples", "usr/share/virmill/examples"))
budget = 512 * 1024 * 1024
def read(path, limit):
    global budget
    assert all(not p.is_symlink() for p in [path, *path.parents]), str(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as f:
        before = os.fstat(f.fileno())
        assert stat.S_ISREG(before.st_mode) and before.st_size <= limit, str(path)
        data = f.read(limit + 1)
        after = os.fstat(f.fileno())
    fields = ("st_dev", "st_ino", "st_mode", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns")
    assert tuple(getattr(before,k) for k in fields) == tuple(getattr(after,k) for k in fields)
    assert len(data) == before.st_size <= limit
    budget -= len(data)
    assert budget >= 0
    return data, before.st_mode & 0o7777

def digest(data):
    return hashlib.sha256(data).hexdigest()

def paths_with_dirs(paths):
    result = set(paths)
    for p in paths:
        result.update(str(x) for x in PurePosixPath(p).parents if str(x) != ".")
    return result

def unfenced(text):
    rows, fence = [], None
    for line in text.splitlines(keepends=True):
        match = re.match(r"^ {0,3}(" + chr(96) + r"{3,}|~{3,})(.*)$", line)
        if fence:
            if match and match[1][0] == fence[0] and len(match[1]) >= len(fence) and not match[2].strip():
                fence = None
            rows.append("\n" if line.endswith("\n") else "")
        elif match:
            fence = match[1]
            rows.append("\n" if line.endswith("\n") else "")
        else:
            rows.append(line)
    assert fence is None, "Unclosed fenced code block"
    return "".join(rows)

def destinations(text):
    text = unfenced(text)
    # Corpus is ordinary inline links. Fail rather than silently count unsupported forms.
    assert not re.search(r"(?im)^ {0,3}\[[^\]]+\]:|<(?:a|img)\b[^>]*(?:href|src)\s*=", text)
    text = re.sub(r"(" + chr(96) + r"+)([^\n]*?)\1", lambda m: " " * len(m[0]), text)
    rx = re.compile(r"\[[^\]]*\]\((<[^>\n]*>|[^()\s]+)\)")
    found = list(rx.finditer(text))
    assert len(found) == text.count("]("), "Unsupported inline destination syntax"
    return [(text.count("\n", 0, m.start()) + 1, m[1].removeprefix("<").removesuffix(">")) for m in found]

package_script,_ = read(ROOT / "scripts/package.py", 1024*1024)
assert digest(package_script) == "a0dad88927b983778a1e40e66b84be67a152f76488ddc3e14942ea5c0f16977f"
inventory_raw,_ = read(ROOT / "build/source-inputs.json", 8*1024*1024)
inventory = json.loads(inventory_raw)
assert digest(inventory_raw) == "f0585986ff6693a306a54e86d48db9eecbed5cddca83941d461d3e2bb2727576"
source = {x["path"]: x for x in inventory["files"]}
assert len(source) == len(inventory["files"]) <= 10000
source_nodes = paths_with_dirs(source)
installed, markdown, manifests = {}, {}, {}
for package in ("virmill", "virmill-host-helper"):
    raw,_ = read(STAGE / package / "install-manifest.json", 1024*1024)
    assert digest(raw) == {"virmill":"21daefa8c91ce144a3b586a593f5fd8af911c354455c0572e1b744d7078b0f97",
        "virmill-host-helper":"d51b177c87d01232460bcef5f5f733dc2d09f778abcfe610c6065fe549e15e7e"}[package]
    manifest = json.loads(raw)
    assert manifest["package"] == package and manifest["releaseQualified"] is False
    assert len(manifest["files"]) <= 1000
    for entry in manifest["files"]:
        path = entry["path"]
        assert not path.startswith("/") and str(PurePosixPath(path)) == path and ".." not in PurePosixPath(path).parts
        assert path not in installed
        data,mode = read(STAGE / package / path, 128*1024*1024)
        assert digest(data) == entry["sha256"] and mode == entry["mode"], path
        installed[path] = entry
        if path.endswith(".md"):
            assert len(data) <= 1024*1024
            origin = next(s + path[len(t):] for s,t in MAP if path.startswith(t+"/"))
            assert digest(data) == source[origin]["sha256"], origin
            frozen,_ = read(ROOT / origin, 1024*1024)
            assert data == frozen
            markdown[path] = (origin,data.decode("utf-8"))
    actual = set()
    entry_count = 0
    for base, dirs, files in os.walk(STAGE / package, followlinks=False):
        entry_count += len(dirs) + len(files)
        assert entry_count <= 2000
        assert all(not (Path(base) / n).is_symlink() for n in dirs + files)
        actual.update((Path(base) / n).relative_to(STAGE / package).as_posix() for n in files)
    assert actual == {e["path"] for e in manifest["files"]} | {"install-manifest.json"}
    manifests[package] = {"sha256":digest(raw), "files":len(manifest["files"]),
        "markdown":sum(e["path"].endswith(".md") for e in manifest["files"])}
assert len(markdown) <= 512
installed_nodes = paths_with_dirs(installed)
links = []
for path,(origin,text) in sorted(markdown.items()):
    for line,url in destinations(text):
        parts = urlsplit(url)
        if parts.scheme or parts.netloc:
            kind, target, source_target = "external", "", ""
        elif not parts.path:
            kind, target, source_target = "fragment-only", "", ""
        else:
            part = unquote(parts.path)
            target = posixpath.normpath(posixpath.join(posixpath.dirname("/"+path),part)).lstrip("/")
            source_target = posixpath.normpath(posixpath.join(posixpath.dirname(origin),part))
            if target in installed_nodes:
                kind = "installed"
            elif part.startswith("/"):
                kind = "host-absolute"
            elif re.search(r":[0-9]+$", source_target):
                kind = "nonportable-line-suffix"
            elif source_target.startswith("docs/evidence/logs/"):
                kind = "excluded-evidence-log"
            elif source_target.startswith("examples/"):
                kind = "relocated-example"
            elif source_target.startswith("schemas/"):
                kind = "relocated-schema"
            elif source_target.startswith("docs/"):
                kind = "source-root-doc-detour"
            elif source_target.startswith("internal/"):
                kind = "omitted-implementation"
            elif source_target.startswith("tests/"):
                kind = "omitted-test-fixture"
            elif source_target.startswith("vendor/"):
                kind = "omitted-vendor"
            elif source_target == "go.mod" or source_target.startswith("contracts/"):
                kind = "omitted-contract"
            elif source_target.startswith("virmill-v1-spec/"):
                kind = "omitted-specification"
            else:
                kind = "other-missing"
        links.append({"document":origin,"line":line,"url":url,"installedTarget":target,
            "sourceTarget":source_target,"sourcePresent":source_target in source_nodes,"kind":kind})
assert len(links) <= 10000
assert sum(t.count("](") for o,t in markdown.values()) == len(links)
assert not any(re.search(r"(?m)^(?: {4}|\t).*\]\(|<(?:https?://|mailto:)", unfenced(t)) for o,t in markdown.values())
missing = [x for x in links if x["kind"] not in ("external","fragment-only","installed")]
classes={}
for kind in sorted(set(x["kind"] for x in missing)):
    rows=[x for x in missing if x["kind"]==kind]
    classes[kind]={"occurrences":len(rows),"uniqueTargets":len(set(x["installedTarget"] for x in rows)),
        "documents":len(set(x["document"] for x in rows)),"sourcePresent":sum(x["sourcePresent"] for x in rows)}

source_targets = {x["sourceTarget"] for x in links if x["sourcePresent"]}
source_file_targets = source_targets & source.keys()
source_directory_targets = source_targets - source.keys()
for target in sorted(source_file_targets):
    raw,_ = read(ROOT / target, 4*1024*1024)
    assert len(raw) == source[target]["bytes"] and digest(raw) == source[target]["sha256"], target
for target in source_directory_targets:
    path = ROOT / target
    assert all(not p.is_symlink() for p in [path, *path.parents])
    assert stat.S_ISDIR(path.stat().st_mode)

result = {"revision":inventory["revision"],"sourceInventorySHA256":digest(inventory_raw), "frozenPackageScriptSHA256":digest(package_script),
    "manifests":manifests, "sourceTargetFilesVerified":len(source_file_targets),
    "sourceTargetDirectoriesVerified":len(source_directory_targets),"markdownBytes":sum(len(t.encode()) for o,t in markdown.values()),
    "counts":dict(Counter(x["kind"] for x in links)), "totalLinks":len(links),
    "missingOccurrences":len(missing),"missingUniqueTargets":len(set(x["installedTarget"] for x in missing)),
    "missingDocuments":len(set(x["document"] for x in missing)), "classes":classes,
    "sourceMissingRelative":[x for x in links if x["kind"] not in ("external","fragment-only","host-absolute") and not x["sourcePresent"]],
    "missing":missing}

self_tests = 0
def check_links(text, expected):
    global self_tests
    assert destinations(text) == expected
    self_tests += 1
def refuse_links(text):
    global self_tests
    try:
        destinations(text)
    except AssertionError:
        self_tests += 1
    else:
        raise AssertionError("Expected unsupported syntax refusal")
check_links("[a](../examples/a.json)", [(1,"../examples/a.json")])
check_links("[a\nb](next.md#section)", [(1,"next.md#section")])
check_links("[a](one.md) [b](one.md)", [(1,"one.md"),(1,"one.md")])
check_links(chr(96)*3+"\n[a](one.md)\n"+chr(96)*3+"\n[b](two.md)", [(4,"two.md")])
check_links(chr(96)+"[a](one.md)"+chr(96)+" [b](two.md)", [(1,"two.md")])
refuse_links("[a](two words.md)")
refuse_links("[a]: target.md\n[a]")
refuse_links('<a href="target.md">target</a>')
refuse_links(chr(96)*3+"\nunclosed")
print(json.dumps({**{k:v for k,v in result.items() if k not in ("missing","sourceMissingRelative")},
    "checkerSelfTestsPassed":self_tests},indent=2))
```
