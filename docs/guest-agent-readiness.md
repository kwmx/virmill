# Guest-agent readiness observation

`GuestReadinessProvider.InspectGuestReadiness(ctx, uri, uuid)` observes the
selected running VM through the official native libvirt binding. It accepts
only explicit `qemu:///system` or `qemu:///session` and a canonical nonzero UUID.
It neither enables a channel nor installs an agent. The only guest command is
the fixed `{"execute":"guest-ping"}`, with flags zero and no caller parameters.

The shared result has these readiness states. Its `state` field is not the
hypervisor lifecycle state; every returned observation requires the selected VM
to remain running through the final check.

| State | Agent connected | Agent responsive | Evidence |
| --- | --- | --- | --- |
| `absent` | false | false | No exact guest-agent channel in live XML |
| `unknown` | false | false | Exact channel exists, but live target state is absent |
| `disconnected` | false | false | Live target explicitly reports disconnected |
| `unresponsive` | true | false | Connected channel, but a successful ping was not observed |
| `responsive` | true | true | Connected channel and successful fixed ping |

For `unresponsive`, fixed evidence tokens distinguish native timeout,
unsynchronized channel, unusable agent and command rejection. Rejection can
include an error response: the state means the ping readiness gate did not pass,
not that no bytes arrived. Permission, transport, unsupported API, malformed
reply, changed VM and cleanup failures return an error and a zero result. There
is no automatic retry. Missing and disconnected channels never receive a ping.

The adapter uses live XML rather than a persistent channel declaration. It
requires one unambiguous native virtio target named `org.qemu.guest_agent.0` on a
Unix channel. Missing output-only channel state remains unknown. Relevant
foreign namespaces, duplicate identity/channel/target fields, malformed values,
directives and XML outside the existing 1 MiB / 16,384-node / 32-depth bounds
are refused. Unrelated metadata and devices do not become readiness evidence.
Libvirt documents live target `state` as whether a guest process is using the
channel; this alone does not establish agent responsiveness.
[Libvirt channel format](https://libvirt.org/formatdomain.html#channel)

Before and after the command, the adapter checks the exact resource key,
running state, live XML UUID, active runtime ID and channel state. Native
`GetID` brackets each VM observation, and live XML must carry that same ID.
The final name and runtime ID must match the initial observation. This detects
an observed same-UUID restart or channel drift; it is not an atomic exclusion
of external lifecycle changes. The final timestamp is UTC.

`QemuAgentCommand` in the pinned `libvirt.org/go/libvirt v1.12007.0`
(`vendor/libvirt.org/go/libvirt/qemu.go`) accepts command text,
integer timeout and flags, and returns the native reply verbatim. Its underlying
API requires domain/write authorization even for this nonmutating ping, so the
adapter opens a write-authorized connection. No other mutation method is called.
Positive timeout values mean seconds; blocking, default and nowait sentinels
are never selected. The adapter passes at most five seconds, shortened to whole
seconds remaining in a caller deadline. If fewer than one remain it returns
`WAIT_TIMEOUT` without a command.
[Libvirt guest-agent API](https://libvirt.org/html/libvirt-libvirt-qemu.html#virDomainQemuAgentCommand)

QEMU specifies a non-error `guest-ping` return as success. This adapter requires
exact `{"return":{}}` structure (whitespace allowed), or recognizes a structured
error with `class` and `desc`. Reply processing is capped at 4 KiB and rejects
duplicate keys, trailing JSON, case aliases, malformed Unicode and additional
fields. Neither reply contents nor live XML are returned or included in errors.
[QEMU guest-ping](https://www.qemu.org/docs/master/interop/qemu-ga-ref.html#command-guest-ping)

The official binding materializes native XML/reply strings before these parser
limits apply. These are processing bounds, not a native allocation guarantee.
Its synchronous connection, inspection, agent and cleanup C calls cannot be
interrupted by a Go context. The positive native timeout bounds the requested
agent-response wait; it is not a guarantee that the whole native transaction
finishes within five seconds. Context is checked around these boundaries, all
owned handles are closed, and any cancellation observed before return discards
the result. No goroutine frees a handle while C might still use it.

The focused `TestGuestReadiness*` tests use generated XML/JSON and a narrow
native-session seam. They cover absent/unknown/disconnected channels, positive
ping, native negative evidence, restart and identity drift, malformed bounded
inputs, exact fixed command/flags/timeout, cancellation, failure sanitization
and cleanup without replay. They make no native guest-agent availability claim.

This contributes implementation and synthetic regression evidence toward
[GUEST-03](../virmill-v1-spec/docs/13-testing-and-acceptance.md). It does not prove
application health, provisioning completion, guest/network-service identity,
SSH access, an IP address, or all mandatory real-guest acceptance evidence.
