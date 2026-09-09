package guestsetup

import (
	"context"
	"strings"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

const toolsVersion = "1.0.0"

type ToolProfile struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Automatic           bool     `json:"automatic"`
	DesktopSupported    bool     `json:"desktopSupported"`
	Description         string   `json:"description"`
	Requirements        []string `json:"requirements"`
	ManualSteps         []string `json:"manualSteps"`
	Version             string   `json:"version"`
	RecipeSHA256        string   `json:"recipeSHA256,omitempty"`
	DesktopRecipeSHA256 string   `json:"desktopRecipeSHA256,omitempty"`
}

type toolsParameters struct {
	Profile        string `json:"profile"`
	Desktop        bool   `json:"desktop"`
	Address        string `json:"address"`
	Port           uint16 `json:"port"`
	User           string `json:"user"`
	IdentityFile   string `json:"identityFile"`
	KnownHostsFile string `json:"knownHostsFile"`
}

func (s *Service) ToolsCatalog(ctx context.Context, _ uint32, r app.Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.ID != "" || r.Path != "" || r.Action != "" || r.After != 0 || r.Apply != nil || len(r.Input) != 0 || r.Connection != "" && !local(r.Connection) {
		return nil, invalid("guest tools catalog accepts no target or input")
	}
	profiles := make([]ToolProfile, 0, 5)
	for _, named := range []struct{ id, name string }{{"linux-auto", "Detect Linux"}, {"debian", "Debian"}, {"ubuntu", "Ubuntu"}, {"fedora", "Fedora"}} {
		base, _ := builtinToolsRecipe(named.id, false)
		desktop, _ := builtinToolsRecipe(named.id, true)
		baseHash, _ := operations.Digest(base)
		desktopHash, _ := operations.Digest(desktop)
		profiles = append(profiles, ToolProfile{ID: named.id, Name: named.name, Automatic: true, DesktopSupported: true, Description: "Install QEMU guest agent; optionally add SPICE desktop tools. Debian, Ubuntu and Fedora with systemd only.", Requirements: []string{"Running guest with explicit IP, non-root SSH user, private key and verified host key", "Guest passwordless sudo, working distribution repositories and org.qemu.guest_agent.0 virtio channel", "Desktop tools need a compatible SPICE session and channel for integration"}, ManualSteps: []string{}, Version: toolsVersion, RecipeSHA256: baseHash, DesktopRecipeSHA256: desktopHash})
	}
	profiles = append(profiles, ToolProfile{ID: "windows", Name: "Windows", Description: "Automatic installation is unavailable. Use trusted VirtIO driver media inside Windows.", Requirements: []string{"Windows guest administrator access", "A locally supplied trusted VirtIO driver ISO"}, ManualSteps: []string{"Use already attached trusted driver media, include it when creating a new VM, or attach it through your libvirt management workflow for an existing VM.", "Open the mounted disc inside Windows and run the matching signed guest tools installer. Review driver prompts there."}, Version: toolsVersion})
	return profiles, nil
}

func (s *Service) PlanTools(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uid == 0 {
		return nil, domain.Fail("PERMISSION_DENIED", "guest tools require an ordinary coordinator actor")
	}
	if !local(r.Connection) {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "guest tools require an explicit local libvirt connection")
	}
	if !validUUID(r.ID) || r.Path != "" || r.After != 0 || r.Apply != nil || r.Action != "" && r.Action != "install" {
		return nil, invalid("guest tools install requires a VM UUID and explicit SSH input, without a recipe path")
	}
	data, err := operations.Canonical(r.Input)
	if err != nil {
		return nil, invalid("invalid guest tools input")
	}
	var args toolsParameters
	if err = strictDecode(data, &args); err != nil {
		return nil, invalid("provide profile, desktop, guest IP address, SSH port, non-root user, private-key path and known-hosts path")
	}
	recipe, ok := builtinToolsRecipe(args.Profile, args.Desktop)
	if !ok {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "automatic guest tools support linux-auto, debian, ubuntu or fedora; for Windows attach trusted VirtIO driver media and install inside the guest")
	}
	if err = s.ready(); err != nil {
		return nil, err
	}
	return s.planRecipe(ctx, uid, r, recipe, parameters{Address: args.Address, Port: args.Port, User: args.User, IdentityFile: args.IdentityFile, KnownHostsFile: args.KnownHostsFile, Arguments: []string{}})
}

// Only these complete, versioned values authorize guest sudo. Matching a name,
// version or caller-supplied digest alone is never sufficient.
func isBuiltinToolsRecipe(r Recipe) bool {
	for _, profile := range []string{"linux-auto", "debian", "ubuntu", "fedora"} {
		for _, desktop := range []bool{false, true} {
			known, _ := builtinToolsRecipe(profile, desktop)
			if r == known {
				return true
			}
		}
	}
	return false
}

func builtinToolsRecipe(profile string, desktop bool) (Recipe, bool) {
	if profile != "linux-auto" && profile != "debian" && profile != "ubuntu" && profile != "fedora" {
		return Recipe{}, false
	}
	r := Recipe{APIVersion: domain.APIVersion, Kind: "GuestRecipe"}
	r.Metadata.Name = "virmill-guest-tools-" + profile
	if desktop {
		r.Metadata.Name += "-desktop"
	}
	r.Metadata.Version = toolsVersion
	r.Spec.Profile, r.Spec.Privilege, r.Spec.Transport = "linux-posix", "sudo", "ssh"
	r.Spec.Idempotent, r.Spec.TimeoutSeconds, r.Spec.Reboot = true, 300, "never"
	guard := `set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
ID=
test -r /etc/os-release || exit 20
. /etc/os-release
case "${ID:-}" in debian|ubuntu|fedora) ;; *) exit 20 ;; esac
`
	if profile != "linux-auto" {
		guard += `test "$ID" = '` + profile + "' || exit 20\n"
	}
	guard += `test -d /run/systemd/system && test -x /usr/bin/systemctl || exit 21
test -c /dev/virtio-ports/org.qemu.guest_agent.0 || exit 22
`
	packages := "qemu-guest-agent"
	if desktop {
		packages += " spice-vdagent"
	}
	query := "packages_installed() {\n"
	query += "  case \"$ID\" in\n    debian|ubuntu)\n"
	for _, p := range strings.Fields(packages) {
		query += `      test "$(/usr/bin/dpkg-query -W -f='${Status}' ` + p + ` 2>/dev/null)" = 'install ok installed' || return 1` + "\n"
	}
	query += "      ;;\n    fedora) /usr/bin/rpm -q " + packages + " >/dev/null 2>&1 || return 1 ;;\n  esac\n}\n"
	verify := "packages_installed && /usr/bin/systemctl is-active --quiet qemu-guest-agent.service"
	r.Spec.Check = guard + query + "if " + verify + "; then exit 0; fi\nexit 3\n"
	r.Spec.Apply = guard + query + `test -x /usr/bin/sudo && /usr/bin/sudo -n /usr/bin/true || exit 23
if ! packages_installed; then
  case "$ID" in
    debian|ubuntu)
      /usr/bin/sudo -n /usr/bin/env DEBIAN_FRONTEND=noninteractive /usr/bin/apt-get -q update || exit 24
      /usr/bin/sudo -n /usr/bin/env DEBIAN_FRONTEND=noninteractive /usr/bin/apt-get -y --no-upgrade --no-remove install ` + packages + ` || exit 24
      ;;
    fedora) /usr/bin/sudo -n /usr/bin/dnf -y install ` + packages + ` || exit 24 ;;
  esac
fi
/usr/bin/sudo -n /usr/bin/systemctl start qemu-guest-agent.service || exit 25
`
	r.Spec.Verify = guard + query + verify + " || exit 25\n"
	return r, true
}

func toolsStageFailure(exit int) error {
	message := "guest tools stage failed; review the guest's package and service logs before creating another plan"
	switch exit {
	case 20:
		message = "guest distribution does not match; choose Debian, Ubuntu or Fedora, or install tools manually inside this guest"
	case 21:
		message = "guest tools require a running systemd guest; install and manage tools manually on this guest"
	case 22:
		message = "guest agent channel is missing; configure org.qemu.guest_agent.0 for this VM before installing guest tools"
	case 23:
		message = "guest passwordless sudo is unavailable; configure sudo for the selected guest SSH user or install tools manually inside the guest"
	case 24:
		message = "guest package installation failed; check guest network, repositories, package locks and free space; packages may have changed"
	case 25:
		message = "guest tools verification failed; check qemu-guest-agent.service and the guest agent channel; installed packages are retained"
	}
	return domain.Fail("GUEST_RECIPE_FAILED", message)
}
