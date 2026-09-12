# Automatic VM startup

Open **VMs**, select a VM, and choose **More → Change VM automatic startup**.
Virmill reads its current setting before showing **Current** and **Requested**.
Use Space or Left/Right to change the toggle, then Tab to **Preview** and Enter.
Review the On/Off change and approve it only when it matches your intent.

This changes future automatic startup. It does not start or stop the VM now.
On the system connection, libvirt normally starts enabled guests when its host
service starts. On the session connection, the user's libvirt service must start;
this is not a promise to boot the guest when the host boots. The Virmill coordinator's
own startup setting is separate; see [service startup](coordinator-startup.md).

Esc from review returns to your choices. Esc from the form leaves the setting
unchanged. Once submitted, the durable job continues if you leave the TUI; check
Jobs for completion or recovery guidance before another attempt. Transient VMs
have no saved definition and cannot use this setting. A changed VM invalidates
the reviewed plan: refresh its settings and create a new preview.

CLI equivalent:

```sh
virmill vm autostart VM_UUID --input '{"enabled":true}' --plan
```

Use `false` to disable. Only a JSON boolean is accepted; quoted strings, numbers,
missing values and extra fields are rejected before a plan is created. Same-value
CLI plans retain their existing idempotent behavior. Apply uses the ordinary
plan/approval workflow described in the [CLI reference](cli-reference.md).

The review shows the exact resource, before/after setting, acknowledgements and
completion checks. Native readback can verify the policy changed; observing an
actual automatic boot after restart is a separate qualification test.
