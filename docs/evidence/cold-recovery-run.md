# Cold capture and independent BIOS recovery — 2026-09-08

Implementation commit `8f22ab1`, Go 1.27.1, authorized Fedora 44 disposable VM,
local `qemu:///system`, libvirt 12.0.0 and QEMU 10.2.2. The ledger records exact
source digests, executable hashes, commands and logs. No source media was uploaded
outside the authorized host or repository workspace.

`cold-recovery-integrated-race-001` passed: 4,293 passing Go test records (743
top-level tests), zero failures and 24 skipped opt-in integration probes. Skips
are not verification. `cold-recovery-reproducibility-001` passed two independent
offline builds of three executables and four DEB/RPM packages. These artifacts
still report `0.0.0-dev`; the subsequent beta label change is separate.

`cold-recovery-native-001` failed its fixture hostname assertion before any guest
or media mutation. The observed hostname was `virmill-test.home`; the recipe was
corrected to accept this exact authorized hostname as well as its short form.
The second source digest also includes generated Bash completions from the build.
Executable hashes remained exactly those from the reproducibility run.

`cold-recovery-native-002` passed the actual CLI/daemon/native workflow:

- Captured both stopped BIOS disks and complete persistent XML, with confined
  QEMU verification and independent local publication.
- Restored into new UUID `a2150092-d2b1-4b74-8e4f-c16cb81a7067`, with two new
  independent qcow2 volumes and a disconnected, initially stopped definition.
- Restored source ACLs before booting the new VM; observed the new VM running
  and correctly reported its absent guest agent.
- Stopped only the new synthetic fixture and preserved both original disk SHA-256
  values, exact original ACLs and every pre-existing guest definition.

The screenshot was copied from the generated test results and visually inspected:
it displays **VIRMILL BIOS MULTIDISK PROBE** and **VIRMILL SECOND DISK PASS**.
The recipe deliberately leaves `bootMarkerVerified=false` because its automated
checks do not read screenshot text; this separate visual observation supplies
that evidence. [Captured display](images/cold-recovery-second-disk.png).

The new guest, volumes, capture, durable jobs and private logs remain on the test
VM under `/home/virmill-test/virmill-tests/cold-recovery-8f22ab1-001` and the
existing disposable test pool. This is a synthetic BIOS guest boot, not an OS,
firmware/TPM restore, independent encrypted repository recovery or full 1.0
acceptance. Those support claims remain open.
