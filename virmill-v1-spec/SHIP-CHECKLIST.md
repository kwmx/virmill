# Complete v1 release checklist

**This checklist applies to the application the agent builds, not to this specification bundle. None of these product checks has been certified merely by generating this package.**

- [ ] Exact supported host, native dependency and guest/source matrix published.
- [ ] All CORE/IMP/NET/STO/SNAP/BAK/DEV/GUEST/LAB/JOB/SEC/EXT/UX/REL acceptance scenarios have evidence at their stated level.
- [ ] CLI/TUI feature parity and shared operation engine verified; no mandatory mock/stub/placeholder path.
- [ ] OVA/multi-disk imports preserve source and distinguish definition/start/guest readiness.
- [ ] NAT, LAN bridge, host-services lab, guest-only networks, multi-NIC routes and IPv6 policies tested.
- [ ] Host bridge timeout/crash rollback tested on certified networking stacks.
- [ ] USB identity/conflict/hotplug behavior tested with physical devices.
- [ ] Snapshots, template graph, full/linked clones and dependent-base protections tested.
- [ ] Backup restores verified without original VM files/application database; firmware/TPM/dependency handling proven.
- [ ] Guest-freeze and host-network recovery guardians survive coordinator loss.
- [ ] Lab no-op reapply, partial failure recovery, external-resource-preserving destroy and coordinated restore tested.
- [ ] Provisioning recipes, cloud-init and supported guest transports verified, with secrets redacted.
- [ ] Private coordinator, helper authorization, crash/reboot reconciliation and scheduled credential failures tested.
- [ ] Working plugin SDK/scaffolder, package signing, confinement and two-language conformance delivered.
- [ ] Simulated provider demonstrates the future remote extension boundary without a remote dependency in core.
- [ ] Install/update/migration/uninstall tested; user VMs/disks/backups preserved by default.
- [ ] Common packages and example plugin cross-build on declared future-platform CI targets; no false host-support claims.
- [ ] User/operator/plugin documentation and examples match actual implemented behavior.
- [ ] Known limitations clearly separate environment restrictions from implementation defects.
- [ ] Remaining release blockers resolved or an explicit owner-approved scope amendment recorded.
