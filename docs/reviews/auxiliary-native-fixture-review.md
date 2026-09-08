# Auxiliary native fixture 003 pre-execution review

The bounded review found two fixture corrections, both addressed by the parent before execution. The final reviewed Python SHA256 is `32b047be8a48c1a2b1d191b1515d3d632329a0eacc6322bb661e7ccc91d36bcc`; all 16 pure self-tests passed independently. No remaining execution blocker was found in the reviewed source. This is permission for no additional action: the parent retains all native execution and mutation ownership.

Reviewed on 2026-09-08: `tests/fixtures/protection/disposable-recorded-run/auxiliary-inspection-native-003.py`, the immutable native-001/002 public failure reports, and the actual frozen `build/auxiliary-490b88c` helper/service/domain/CLI source. The runtime remains `490b88cba5bf6e0837e2cb5780e56211c22a7d27`; recipe changes do not imply new product binaries. Only this review document was edited by the reviewer. No SSH, native command, IPC, policy replacement, guest action or privileged program execution occurred during review.

## Retained earlier failures

Native-001 failed during preflight because domcapabilities was attempted on a libvirt read-only connection; a later preservation comparison also treated volume access-time drift as a configuration change. Native-002 incorporated those fixture corrections and created only its new, never-started UUID `50a0583b-3587-495f-90f4-9a42b8ad2fa3`, but stopped at an expected denial because the fixture demanded empty stderr. The actual CLI emits its typed error diagnostic there. The public native-002 report retains that failure, eight stopped definitions, unchanged prior journal/schema and restored original policy; no successful grant phase is inferred from it.

The successor has a new `003` output, domain name, state root, root ID and policy temporary-file namespace. It neither rewrites nor retries native-001/002. Its preflight observes and preserves the current prior resource set, which now includes the retained native-002 guest and generated state.

## Findings corrected before execution

**Check the current stopped definition before redefining it.** Initially, `Run.define` validated the outgoing UUID/name/no-disk/no-network XML, then cleared `native_xml` and submitted virsh define without comparing the current native definition to the previous acknowledged value. A pure in-memory seam changed the simulated previous XML from one to two vCPUs. The fixture silently overwrote that drift; observed call order was `define submitted`, then `stopped XML observed`.

The parent added a precondition for every subsequent define: previous creation must be acknowledged, `native_xml` must exist, and a fresh stopped/XML observation must exactly equal it before intent/submission. Unknown acknowledgement or a changed definition now refuses without overwrite. The new self-test covers both cases. This guards the explicit TPM-source removal and subsequent restoration. No actual remote race or changed guest was observed by the reviewer, and this endpoint check does not claim atomic exclusion of an external writer after the check.

**Require the FIFO-specific refusal.** The original FIFO assertion required only `UNSUPPORTED_CAPABILITY`, but the three existing payload members already exhausted `maxMembers: 3`. A generic unsupported/member-budget failure could therefore satisfy that assertion without proving the intended special-file boundary. In frozen `auxiliary_inventory_linux.go`, the O_PATH type observation rejects the FIFO before member-budget evaluation. The parent now additionally requires `auxiliary objects must be directories or single-link regular files` in the returned message. No policy bound or runtime implementation was changed. The unresolved-source test already checks its own specific native-path message.

The initial reviewed SHA256 before these corrections was `5dad8ba49cbfd9f70f79d19d960fe0049fdf427d243541c19208115a479bd883`. Both findings were sent to the parent immediately; the reviewer made no fixture or production edit.

## Frozen contract assumptions checked

| Fixture assumption | Frozen-source result |
|---|---|
| Denials have a single JSON stdout envelope and a typed stderr diagnostic | Correct for this CLI. `cmd/virmill/main.go` prints the returned error; `domain.Error.Error` renders `code + ": " + message`, and `domain.ExitCode` maps INVALID_INPUT/UNSUPPORTED_CAPABILITY/PERMISSION_DENIED to 2/3/4. `response_record` checks those codes, nil data and the exact diagnostic derived from the JSON message. It does not require the helper's nested error message to lose an existing code prefix. |
| Original policy and root-only policy deny | Correct. A missing root fails the client boundary; a present base actor/key/root without a matching auxiliary permission fails explicit helper authorization. The root-only case first confirms the public identity/root mapping, avoiding a denial masked by missing base setup. |
| A metadata-only permission becomes effective without restarting the helper | Correct. The frozen helper reloads and authorizes its held validated policy per request. No helper service restart is needed between root-only, grant and restored-policy phases. |
| Exact payload inventory is three members totaling 4,128 bytes | Correct: 4,096-byte `nvram.fd`, empty `tpm/empty.state`, and 32-byte `tpm/ordinary.state`. `.lock` is the separate empty `tpmLock` control record, excluded from the payload total/member budget. The dummy loader file is not an auxiliary payload member. |
| State mode is the full Unix regular-file mode and generation is a string | Correct. Frozen `AuxiliaryFileState` emits numeric statx mode, UID/GID, link count and a string generation. Generated mode 0600 files yield `S_IFREG | 0600`; qemu UID/GID come from the actual account lookup. Root owner 0 and the native resource/layout/path field names match the common contract. |
| JSON and NDJSON reads may share identical inventory but use different job IDs | Correct. Each service read creates a fresh correlation ID; inventory metadata contains no observation timestamp that requires it to change. The IDs do not represent durable jobs. The helper uses O_PATH for files and O_NOATIME for directory enumeration, consistent with unchanged fixture metadata checks. |
| Removing explicit TPM source yields INVALID_INPUT | Correct for frozen 490b88c if libvirt preserves the unresolved declaration. `auxiliaryRelative` rejects the empty native source path before the later source-type check. The fixture checks that no source reappeared and requires the specific canonical-path message, so another normalization or unrelated failure cannot masquerade as the expected result. |
| Restoring the new explicit XML may restore the original fingerprint | Correctly conditioned on exact normalized XML restoration. The final inventory must match the original native fingerprint/layout. No old guest is redefined. |

The positive assertion checks resource, root ID/path, native layout, payload set/count/size, separate lock metadata, owner/mode/link/generation and false capture/restore/boot flags. It does not independently certify every ACL/SELinux/directory metadata field; those remain present in the retained response and are covered by separate executor tests. No stronger all-fields or producer-lock qualification should be attributed to this native fixture.

## Mutation and preservation boundaries

The reviewed generated guest has no disk or interface and is never started. Its loader, NVRAM and TPM files are original dummy data under only `/var/lib/virmill-host-helper/auxiliary-fixture-003`; validity as firmware/TPM state is explicitly false. The fixed administration program creates them exclusively, observes them without reading their payload, introduces one owned FIFO, and removes only that FIFO after matching the complete expected fixture metadata. Earlier helper state is recursively observed while excluding only the new 003 subtree; the retained 002 state remains part of preservation.

Only the new UUID/name is accepted by the native define wrapper. There is no guest start, stop, undefine or deletion command. Failure cleanup does not redefine the new guest, unlink generated state or retry inspection. It retains resources and independently attempts observation of earlier journal, guests, pools, media and helper files. The restored explicit definition is a planned success-path step, not an automatic recovery overwrite.

Policy variants preserve the original public actor/key/root grants, add only the new root and optionally one exact VM/key/actor permission, and keep `allowCapture: false`. Policy replacement performs a metadata/content compare before preparation and again before atomic rename, preserves mode/UID/GID/xattrs and declared times, syncs the new file and policy directory, and checks the returned inode/content. Only atime is excluded from current-policy equality because normal public-policy reads can update it. Restoration requires original bytes/xattrs/mode/owner/size/link count/mtime; it does not falsely require the original inode or ctime after replacement.

The recipe requires the parent's exclusive administrator policy window. This is still a compare-then-rename sequence, not a kernel compare-and-swap against an unrelated concurrent administrator. It marks publication uncertain before invocation and refuses automatic restoration/replay if acknowledgement is lost. The corresponding pure self-test passes. No signing key is opened, exported or hashed by the recipe; the installed application uses its existing credential through its normal authorized signing path.

Domcapabilities now uses one fixed query on a normal connection; all other observational virsh calls retain the read-only connection except the allowlisted new-domain defines. Pool allocation/available counters and the single validated volume atime text are excluded narrowly; capacity and other configuration/XML bytes remain compared. Media content hashing remains restricted to selected public small image artifacts, with O_NOATIME. TPM/NVRAM payloads are not selected for content reads. Endpoint preservation observations remain weaker than exclusion of every possible external writer.

## Executed local checks and handoff

The reviewer ran only:

```sh
python3 tests/fixtures/protection/disposable-recorded-run/auxiliary-inspection-native-003.py --self-test
```

The initial source passed 15 tests in 0.165 seconds. After the parent corrections the final source passed 16 tests in 0.148 seconds, including the new drift/uncertain-definition guard. The separate pure define seam reproduced the pre-fix ordering without files, processes or native calls. The embedded root program was compiled as Python text only by self-tests. Source comparisons used the actual frozen 490b88c checkout and public failure reports; no native observation was performed by the reviewer.

The final reviewed source is ready for parent-controlled execution within the already authorized disposable scope. This review supports SNAP-01 and SEC-01/SEC-03/UX-03 fixture prerequisites only. It claims no successful native run, confidential-state capture, producer exclusion, independent restore, firmware/TPM initialization, guest boot, physical hardware evidence or release acceptance.
