# 0046 — Connect new graphical VMs to the supported viewer

Status: implemented in source; product creation/viewer qualification pending.

The creation form suggested private VNC, while the console adapter deliberately
refused VNC launch because its pinned viewer does not enforce the required
clipboard preference on that path. Adding buttons did not close this workflow:
a graphical ISO installer could be created without a usable Virmill console.
The mandatory desktop integration contract in specification document 08 and the
owner's request for an understandable creation-to-guest-tools flow take precedence
over that incidental implementation default. Locked scope is unchanged.

Add `spice-unix` to the existing explicit creation graphics choices. Observed
libvirt capabilities must advertise SPICE, and preflight repeats the capability
check. New forms prefer it when advertised. Existing VNC/none declarations,
resumed drafts and durable recipes retain their exact choices. No existing guest
is converted. The creation input schema accepts this additive enum value only
with the explicit device policy that disables host audio and describes chipset,
USB, input and serial behavior. Existing input variants stay valid.

The XML uses a local Unix socket, disables clipboard copy/paste and file transfer,
and retains the existing VGA model. It adds no desktop agent, audio device,
monitor channel, USB redirection, shared folder or network listener. SPICE
normalization is matched against the requested configuration; unknown exposure
or security settings cannot be treated as harmless defaults. See the primary
[libvirt graphics contract](https://www.libvirt.org/formatdomain.html#graphical-framebuffers).

The existing shared console launcher still rechecks identity/configuration,
requires an ordinary user and a desktop session, and invokes the verified system
virt-viewer with disabled integration features. A plain SSH terminal cannot show
an arbitrary graphical installer. The form states the desktop requirement before
creation; configured serial access remains a separate option. VNC launch and
unsupported viewer versions remain explicit gaps rather than silent fallback.

Native stopped-definition normalization passed with the exact private graphics
shape. This is feasibility evidence, not product creation, guest boot or viewer
verification. The next fixture must use the actual import/create/start workflows
and observe a real viewer connection. All 71 acceptance scenarios remain required.
