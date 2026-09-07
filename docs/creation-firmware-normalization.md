# Creation firmware comparison

Creation reconciliation now recognizes the recorded libvirt normalization that
adds `os firmware='efi'` and the two firmware features `enrolled-keys=no` and
`secure-boot=no` to an explicitly pinned, non-secure pflash definition. The
comparison previously rejected these additions even though the planned and
observed loader code, template and formats matched.

The change applies only to the private XML comparison tree. It changes neither
creation rendering nor the reviewed device policy, stored XML, firmware
selection, creation grants or recovery execution. The complete observed XML is
preserved in the fixture with provenance and byte hashes in
`tests/fixtures/creation/uefi-normalization/README.md`.

Normalization requires all of the following:

- Both roots are unnamespaced KVM domains with exactly the native type attribute.
- Each has one OS element. The wanted OS has no attributes and exactly type,
  loader and NVRAM children; the observed OS adds only `firmware='efi'` and one
  firmware child.
- Both OS types are x86_64 HVM with the same explicitly named machine and no
  additional type attributes or structure.
- Both loaders have exactly readonly=yes, secure=no, type=pflash and the same
  explicit raw/qcow2 format. Their code path is identical, absolute and
  canonical. No loader default is inferred.
- Both NVRAM elements have exactly the same explicit template, template format
  and NVRAM format, matching the loader's format. They contain no structured
  source. Their text uses the pre-existing NVRAM comparison rule described below.
- The added firmware element has no attributes or scalar content and exactly
  one feature for enrolled-keys and one for secure-boot. Both have enabled=no,
  no additional attributes, no nested children and no scalar content. Order is
  immaterial.

Every condition must hold before the two additions are removed from the private
comparison tree. Ordinary matching then checks the complete remaining
definition. Unknown attributes or children, duplicates, foreign namespaces,
enabled=yes, changed code/template/formats, missing explicit loader metadata,
new device settings and altered creation bindings remain mismatches. The
general default-attribute and default-child allowlists are unchanged.

This narrow rule does not interpret enabled Secure Boot or key enrollment.
Libvirt describes loader `secure` as firmware capability metadata, not a switch
that proves the running firmware's enforcement state. Its firmware feature
selection metadata must not be substituted for actual NVRAM/key-state evidence.
See the official [guest firmware format](https://libvirt.org/formatdomain.html#operating-system-booting).

The existing NVRAM path limitation remains explicit: when the wanted NVRAM text
is empty, the matcher accepts empty observed text or any observed text whose
trimmed value starts with `/`. It does not establish that the path is fresh,
belongs to this new identity, is a regular file, contains the intended initial
variables, or is safe to initialize. The new rule neither tightens nor broadens
that behavior. Tests compare the prior and new outcomes for absolute, relative,
empty and structured-source variants. Separate native freshness/identity work
is still required; a passing XML replay is not evidence that it has happened.

Reproduce the local captured-XML and adversarial regressions with:

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -tags libvirt_dlopen ./internal/backend/libvirt -run '^TestCreationFirmwareNormalization' -count=1
```

This is creation/reconciliation prerequisite evidence. The exact acceptance
wording makes IMP-01 and IMP-07 relevant to creation verification stages;
CORE-01/CORE-02 and SNAP-01 remain broader inventory, lifecycle and cold-recovery
workflows. These replay tests complete none of those real-backend scenarios.
CORE-04 concerns preservation during adopted-VM edits, and CORE-05 concerns
concurrent edits invalidating stale plans; this comparison-only change claims
neither scenario. Native reconciliation, job status, resource locks and actual
firmware/TPM recovery remain separately verified work.
