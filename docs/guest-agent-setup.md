# Set up guest integration

Open `virmill`, choose **VMs → Details → Guest tools**. Virmill checks the
selected VM's guest-agent connection before offering installation. This works
for an existing VM regardless of which supported source format created it.

If the connection is missing, shut down the VM and return to **Guest tools**.
Choose **Preview enable connection**, review the change and its acknowledgements,
then apply. Start the VM separately and reopen **Guest tools** to install the
software. A VM with saved runtime state must have that state resolved first;
Virmill does not discard it automatically.

The connection is a high-trust interface through which the host can exchange
management messages with the guest. Enabling it changes the stopped VM's saved
configuration for its next boot. It does not install software, start the VM,
prove that an agent responds, or enable clipboard sharing. Virmill preserves
existing configuration and refuses conflicting or unsupported channel layouts.
For a new VM, the same opt-in is **Guest agent channel** under Advanced hardware.

## Install inside the guest

Keep **Detect Linux (recommended)** for automatic distribution detection. The
built-in installer supports Debian, Ubuntu and Fedora systemd guests; other
systems, including Kali, are not implicitly treated as one of those profiles.
Choose the guest IP address and ordinary SSH username, then browse for a private
key and an independently verified known-hosts file with **Ctrl+O**. The running
guest needs reachable SSH, passwordless sudo and access to its configured package
repositories. These credentials belong to the guest, not the Virmill host.

**Preview installation** reviews installing `qemu-guest-agent` and starting its
service. **Advanced** contains the SSH port and optional `spice-vdagent` desktop
package. Desktop package installation alone does not prove clipboard or display
integration. Windows has manual instructions for trusted VirtIO driver media;
Virmill does not automatically install tools in Windows or an unknown appliance.
See [guest tools](guest-tools.md) for installation prerequisites and failure
behavior.

## CLI equivalents

These commands inspect configuration and prepare a change; `--plan` does not
apply it:

```sh
virmill vm guest-agent show VM_UUID
virmill vm guest-agent enable VM_UUID --plan
```

Review the returned plan using `virmill plan show PLAN_UUID`. Apply its exact
digest, a stable idempotency key and every acknowledgement listed in the plan
with `virmill plan apply`. The channel change explicitly requires
`guest-agent-host-access` as well as the applicable VM mutation acknowledgements.
If the VM changed after review, create a fresh plan instead of reusing the old
approval.

After starting the VM, `virmill guest tools catalog` lists supported profiles.
`virmill guest tools install VM_UUID --input 'JSON' --plan` prepares installation.
Replace `JSON` with the object in [the Linux example](../examples/guest-tools/linux-auto.json),
using your own address and file paths. The TUI lets you fill these options directly.

Check a running VM separately with `virmill vm readiness show VM_UUID`. Its
bounded agent ping observes whether the agent responds; it does not establish
application readiness or identify an SSH server. If an operation is interrupted,
inspect **Jobs** before retrying. An enabled connection, successful package
installation and a responding agent are separate results.
