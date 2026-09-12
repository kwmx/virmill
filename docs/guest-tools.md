# Guest tools

Guest tools help Virmill communicate with a running VM. Choose **Detect Linux**
to check the guest distribution automatically, or select Debian, Ubuntu or
Fedora explicitly. The built-in installer checks the guest's `/etc/os-release`;
other distributions and non-systemd guests require manual installation.

The default installs `qemu-guest-agent` and starts its service. The optional
desktop choice also installs `spice-vdagent`. Desktop package installation does
not prove that clipboard sharing, resizing or a graphical session works: those
features also require compatible SPICE display and channel configuration.

Supply the running VM's IP address, SSH port, ordinary guest username, private-key
file and verified known-hosts file. Addresses are explicit literal IPs; Virmill
does not guess an address or bypass host-key checking. The selected guest account
must support passwordless `sudo` for installation. Do not enter passwords or
private-key contents into settings. The key and known-hosts files must be private,
readable local files accepted by the SSH transport.

The VM needs the `org.qemu.guest_agent.0` virtio channel before installation.
For a new VM, turn on **Guest agent channel** in Advanced hardware during VM
creation. For an existing VM, open **Guest tools**: Virmill checks the connection
and offers a reviewed enable action when it is missing. Shut down first, enable
the connection, then start the VM before installing the software. See the
[guest integration guide](guest-agent-setup.md). Configuration and installation
are separate actions; neither alone proves that the agent responds.


Review the plan and approve **guest-admin-package-install**. This authorizes
package installation and service startup as an administrator **inside the
guest**, through fixed `sudo -n` commands. The coordinator and SSH login remain
ordinary users. No host package manager runs. Guest packages and dependencies
come from the guest's configured distribution repositories; their selected
versions depend on that guest's repository state. This action does not perform
a general distribution upgrade, add repositories, download host scripts, or
request a reboot. Package dependencies and maintainer scripts can change guest
state, and there is no automatic rollback.

The service checks package installation and agent service readiness first. If
they already match, the apply stage is skipped and verification still runs.
Otherwise it installs missing packages and starts the agent. Each stage has a
durable intent and receipt. An interrupted connection can leave an unknown
remote effect; check Jobs and the guest before requesting another installation.
Reconciliation never reruns a script. Each stage is bounded to five minutes,
with a shorter SSH readiness check. Package locks, repository/network failures,
missing sudo and service failures produce guidance; installed packages remain
available for inspection.

For Windows, automatic installation is unavailable. When creating a new VM,
include a trusted, locally supplied VirtIO driver ISO as optical media in its
prepared source. For an existing VM, use driver media that is already attached,
or attach it through your established libvirt management workflow: the current
Virmill existing-VM editor can eject optical media but cannot attach a new ISO.
Open the mounted disc and run the matching signed guest tools installer inside
Windows as an administrator. Virmill does not download or silently run a Windows
installer.

## Shared service contract

`guest.tools.catalog` is read-only and accepts no target or input. It returns
`linux-auto`, `debian`, `ubuntu`, `fedora` and the manual `windows` entry. Linux
entries include the built-in recipe version and SHA-256 digests for the basic
and desktop variants.

`guest.tools.install` takes the VM UUID as `id`, an explicit local libvirt
`connection`, and an input object with all of `profile`, `desktop`, `address`,
`port`, `user`, `identityFile` and `knownHostsFile`. A recipe path or arbitrary
script is not accepted. The shared command registry sends `action: install`;
direct service callers may omit `action`. Examples are in `examples/guest-tools/`; substitute
your own target and paths. The result is a plan, submitted through the normal
authorize/apply workflow. Jobs retain operation `guest.recipe.run`, and results
use `guest.recipe.result`.

Recipes are frozen directly into the plan, without temporary exported files.
Version `1.0.0` and the complete canonical recipe determine the recorded hash.
`privilege: sudo` is accepted only when every field matches an exact compiled
built-in recipe. An arbitrary recipe cannot gain administrative authorization
by copying its name, version or digest. Existing `non-root` recipes keep their
existing policy. Raw guest output is not retained; durable receipts record
exit policy, byte counts and output hashes. The service verifies package and
guest service state; it does not claim an independent host-to-agent handshake.

## Command references and verification

The fixed scripts use the distribution package managers described by
[Debian's apt-get reference](https://manpages.debian.org/bookworm/apt/apt-get.8),
[dpkg-query](https://manpages.debian.org/unstable/dpkg/dpkg-query.1.en.html) and
[DNF's command reference](https://dnf.readthedocs.io/en/stable/command_ref.html).
The [Fedora QEMU agent package description](https://packages.fedoraproject.org/pkgs/qemu/qemu-guest-agent/index.html)
documents the guest channel. [SPICE's guest tools documentation](https://www.spice-space.org/download.html)
describes the optional desktop agent and Windows guest tools.

Local tests cover exact built-in authorization, canonical hashes, shell syntax,
planning, durable stage sequencing, already-configured skips, failure guidance
and no replay. Fixture transports do not validate actual package installation,
service startup, guest channels or desktop behavior. Those support claims need
separate real-guest evidence for GUEST-01, GUEST-02 and GUEST-03; the CLI/TUI flow
also contributes to UX-01 and UX-02.
