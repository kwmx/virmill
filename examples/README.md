# Example inputs

These files describe the future `virmill` command contract. They are not configuration for an already installed application. No VM image, private key, real backup repository or plugin signing key is provided.

`multi-network-lab.yaml` creates three local networks and three VMs. The workstation has internet and two lab interfaces; the other VMs have only their designated lab interface. The workstation is dual-homed/multi-homed and must trigger the documented isolation warning. Its presence does not prove strict containment of a compromised guest. The CIDRs are examples, not guarantees of availability on your network.

Create/verify the referenced local templates first. Then the intended workflow is `virmill lab validate FILE`, `virmill lab plan FILE`, review, and `virmill lab apply FILE`. The agent must implement those commands and verify that they match the generated help; these examples alone do not implement them.

See `plugins/vm-summary/README.md` for the only runnable program in this package: a synthetic, read-only protocol example that never contacts a VM host.
