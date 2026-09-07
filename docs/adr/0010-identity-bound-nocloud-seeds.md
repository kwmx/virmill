# ADR 0010 — Identity-bound NoCloud seeds

Status: accepted routine implementation decision. Mandatory cloud creation,
clone identity, explicit guest policy, secret handling and durable-operation rules
apply together. They take precedence over treating a reusable prebuilt seed as
safe for every clone or equating file construction with cloud provisioning.
All local 1.0 scope remains required; no host/guest mutation is authorized here.

`vm create` accepts an optional typed `provisioning` object for a declared NoCloud
profile and successful independent disk-set source. The root source digest must
match the selected first-boot disk's original proof. The HTTPS source reference is
a user declaration, not a network request or authenticated publisher claim. A
compatible image with cloud-init/NoCloud, Netplan and networkd is explicitly
asserted; actual guest compatibility remains unqualified.

The VM UUID and MACs are generated before rendering. Metadata uses a distinct
`virmill-VM_UUID` instance ID; every guest NIC is matched to its exact new MAC.
Preview creates a deterministic ISO in private scratch space, verifies it and
removes the scratch files. Its tool, input/member hashes, ISO hash/size and private
cache identity are persisted in the creation plan. Execution regenerates the same
ISO before any managed allocation, then treats it as another read-only media
volume in the existing receipt. Definition remains last. This avoids sharing seed
identity across clones and avoids an unreviewed random media hash during apply.

Only structured nonsecret settings are accepted: hostname, selected account,
public SSH keys, explicit optional passwordless sudo, and complete per-NIC IPv4
intent. This initial profile requires disabled IPv6, prevents ambiguous/default
routes on protected adapters and suppresses their DHCP routes/DNS. It requests
fresh guest SSH host keys and disables implicit cloud-init filesystem growth,
package update/upgrade and automatic package reboot. It does not generalize every
existing guest identity or modify/verify forwarding. Unsupported profiles and
secret/private-key/arbitrary-script input are refused; full IPv6, secret-store,
recipe and guest verification workflows remain required.

A regression test exposed that the bundled schema validator includes rejected
values in formatted errors, including a pasted private key. The creation boundary
now returns a value-free schema failure and directs callers to the bundled input
schema. Tests assert forbidden credential values never reach error text, seed
workers or journal metadata. Accepted public configuration is visible in the
private plan and persists in media/guest state; no secret-redaction or encrypted
seed claim is made for arbitrary user data.

The generator is fixed `/usr/bin/xorriso`, pinned by root-owned executable digest
and observed version. It runs in the existing namespace/seccomp confinement with
no original source/device mounts and a 16 MiB per-file bound. Fixed ISO timestamps
make preview and execution byte-identical for identical inputs. The backend checks
ISO9660/CIDATA and extracts the exact three files in confinement for byte readback.
A real fixture exposed read-only Rock Ridge directory permissions preventing
cleanup; the private generated readback directory is now restored to writable
mode through an opened no-follow directory descriptor. Generator output containing
seed content is withheld from job errors. Dependencies now declare xorriso and the
existing bubblewrap/prlimit runtime requirements using observed package names.

Cancellation during generation joins the worker before removing private output
and finishes canceled only before host allocation. Other failures preserve durable
receipts and uncertainty. Once every backend volume is verified, definition
recovery reuses media and needs neither the generator nor the temporary cache.
The cached stage is reported and may survive a coordinator crash; explicit cache
GC and safe post-provision managed-seed removal remain lifecycle work.

SQLite schema 3 is unchanged. `provisioning`/`seed` are optional typed fields omitted
from legacy inputs. Older strict decoders refuse them; existing receipts retain
meaning and migration/rollback rules. Tests cover legacy creation, source/tool/cache
changes, no allocation on seed failure, cancellation, distinct clone identities,
secret refusal, exact seed replay and recovery without regeneration. CLI/TUI pass
the same input through the shared service. Native guest execution, cloud-init
schema/runtime validation and full readiness remain blocked.

The NoCloud file/label contract comes from
[cloud-init's datasource documentation](https://docs.cloud-init.io/en/latest/reference/datasources/nocloud.html).
The explicit renderer boundary follows its
[network v2 documentation](https://docs.cloud-init.io/en/latest/reference/network-config-format-v2.html),
which distinguishes Netplan pass-through from other renderers' supported subsets.
Installed xorriso 1.5.8.pl02 help provided the exact fixed-date and extraction argv;
its package/executable hash is in the dependency contract. No guest cloud-init
version has been invented: it is absent on the build host and guest images remain
unavailable. Generated ISO tests are file evidence, never hardware evidence.
