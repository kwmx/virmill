# 03 — CLI contract

All command examples are the target interface to implement. They do not imply the application is already installed.

## Global behavior

`virmill` with a terminal opens the TUI; without a terminal it prints help and exits without prompting. `virmill tui` is explicit. Every normal interactive workflow also exists as a CLI command.

Global options: `--connection`, `--output table|json|ndjson`, `--quiet`, `--verbose`, `--no-color`, `--non-interactive`, `--timeout`, and `--config`. A timeout on a client wait detaches; it does not necessarily cancel the server job. `--yes` confirms ordinary prompts, not privilege escalation, destructive disk deletion, weakened isolation, or stale plans. Those need explicit flags/acknowledgements tied to the generated plan.

All mutating leaf commands accept `--plan` to return a preview without applying and `--wait`/`--detach`. Default interactive behavior previews consequential effects and waits. Noninteractive mutation requires explicit acknowledgement and complete inputs; it must never wait invisibly for a password dialog.

## Command tree

```text
virmill tui
virmill version | doctor | config get|set|edit
virmill host inspect | setup plan|apply | capabilities
virmill connection list|inspect|select|discover
virmill vm list|show|adopt|create|start|stop|reboot|pause|resume|save|restore-saved
virmill vm edit|set|rename|autostart|remove|console|ssh|stats|events
virmill vm disk list|add|attach|detach|resize|move
virmill vm nic list|add|edit|detach|link
virmill import inspect|plan|run
virmill export vm|template|lab
virmill network list|show|create|edit|start|stop|remove|members|diagnose
virmill network bridge plan|apply
virmill network dhcp list|reserve|release
virmill network policy show|set
virmill network forward list|add|remove
virmill device list|show
virmill device usb attach|detach|watch|binding list|binding remove
virmill device pci inspect
virmill share list|add|remove
virmill storage pool list|show|create|adopt|remove
virmill storage volume list|show|import|create|resize|flatten|remove
virmill storage graph|check|gc plan|gc apply
virmill snapshot list|show|create|restore|remove|graph
virmill template list|show|create|version|clone|remove|verify
virmill backup repo list|add|init|check
virmill backup create|list|show|verify|restore|test-restore
virmill backup policy list|create|edit|enable|disable|remove
virmill backup retention plan|apply
virmill lab validate|plan|apply|list|show|start|stop|status|destroy
virmill lab snapshot create|restore|list
virmill guest status|setup|recipe list|recipe show|recipe run
virmill operation list|show|watch|cancel|retry|reconcile
virmill plan show|apply|discard
virmill plugin list|show|install|enable|disable|remove|update|rollback
virmill plugin permissions show|grant|revoke
virmill plugin new|validate|test|pack
virmill plugin call <plugin-id> <action-id>
virmill service status|install|start|stop|persistence
virmill diagnostics collect
virmill completion bash|zsh|fish|powershell
```

A connection in v1 is a local system/session libvirt endpoint. Remote connection handling requires a provider plugin; do not quietly accept remote URIs in the built-in local provider.

## Representative flows

```sh
# Inspect an appliance without modifying the source or host.
virmill import inspect ./appliance.ova --output json
virmill import plan ./appliance.ova --name test-appliance --pool local
virmill plan apply PLAN_ID --wait

# Attach one internet connection plus two lab interfaces.
virmill network create internet --type nat --cidr auto --plan
virmill network create research --type lab --host-access services-only --cidr auto
virmill network create targets --type guest-only
virmill vm nic add workstation --network internet --default-route --apply next-boot
virmill vm nic add workstation --network research --no-default-route --apply next-boot
virmill vm nic add workstation --network targets --no-default-route --apply next-boot

# Persistent USB selection is chosen through stable device inventory IDs.
virmill device list --kind usb
virmill device usb attach workstation --device DEVICE_ID --persistent --apply both

# Restore protection and repeatable labs.
virmill snapshot create workstation --name before-update
virmill backup create workstation --repo external --mode auto --wait
virmill lab validate ./lab.yaml
virmill lab plan ./lab.yaml
virmill lab apply ./lab.yaml --wait
```

`--mode auto` selects the safest supported backup capture. If safe capture requires stopping a running VM, planning requests shutdown approval; it never silently powers it off. An imported guest without a provisioning channel may require its routes configured inside the guest. The NIC command must report that distinction.

## Destructive behavior

`vm stop` means graceful shutdown with a timeout and no automatic escalation. `vm stop --hard` requests hard power-off and displays a data-loss warning. `vm remove` removes a definition while retaining disks by default. Deleting selected disks requires an explicit disk list and separate plan acknowledgement; `--yes` alone is insufficient.

`lab destroy` affects only resources owned by that lab. It retains disks and externally supplied networks by default. Backups are not deleted with a VM or lab. Template deletion refuses while linked-clone dependencies remain.

## Stable output

A JSON response is a single object with `apiVersion`, `data`, `warnings` and `error`. A successful async submission returns `operationID`, state and a watch cursor, not a completed-resource claim. In NDJSON mode, each line is one response/event envelope.

Version output includes application revision, schema/plugin API versions, and build information. Native backend versions are separate runtime fields. Time values are RFC 3339 UTC in machine output; local display may use the user's timezone.

Exit codes: `0` success or accepted detached submission; `1` operation failure; `2` invalid input; `3` unsupported capability; `4` missing authorization; `5` stale plan/conflict; `6` partial result or recovery required; `7` wait timeout; `130` client interruption. The job record distinguishes a detached submission from an actually successful job. Scripts that need completion must use `--wait` or inspect the final operation.

## Help and discoverability

Generate help, reference pages and shell completions from the command registry. Each mutator documents side effects, privilege needs, live/persistent behavior, destructive options and examples. Resource completion must be bounded, nonblocking and read-only; completion must never wake stopped VMs or install dependencies.

`doctor` produces categorized checks with concrete repair plans, not auto-fixes. `diagnostics collect` requires review/redaction before exporting logs or configuration. Never include secrets or guest disk contents in diagnostics.
