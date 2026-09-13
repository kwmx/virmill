//go:build linux

package linux

import (
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"virmill.local/core/internal/domain"
)

// dependency is a host tool Virmill runs and the distribution packages providing it.
type dependency struct {
	command, purpose string
	optional         bool
	fedora, debian   []string
}

var dependencies = []dependency{
	{"qemu-img", "Converts and checks disk images for import and VM creation", false, []string{"qemu-img"}, []string{"qemu-utils"}},
	{"bwrap", "Sandboxes the tools that read untrusted image files", false, []string{"bubblewrap"}, []string{"bubblewrap"}},
	{"prlimit", "Limits memory and time for those sandboxed tools", false, []string{"util-linux"}, []string{"util-linux"}},
	{"xorriso", "Builds installer and cloud-init media for ISO and cloud-image VMs", true, []string{"xorriso"}, []string{"xorriso"}},
	{"virt-viewer", "Opens a VM's graphical console", true, []string{"virt-viewer"}, []string{"virt-viewer"}},
	{"swtpm", "Provides virtual TPM chips for UEFI/TPM guests", true, []string{"swtpm", "swtpm-tools"}, []string{"swtpm", "swtpm-tools"}},
	{"restic", "Stores encrypted backups", true, []string{"restic"}, []string{"restic"}},
}

// doctorHost is everything the checks read, so tests can describe a host.
type doctorHost struct {
	lookPath  func(string) (string, error)
	exists    func(string) bool
	usable    func(string) bool
	osRelease func() string
	// libvirtGroup reports whether the group exists and this process is in it.
	libvirtGroup func() (exists, member bool)
	platform     string
}

// Doctor runs read-only host checks. It never installs or changes anything.
func Doctor() []domain.Capability {
	return doctor(doctorHost{
		lookPath: exec.LookPath,
		exists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
		usable: func(path string) bool {
			const readWrite = 0x4 | 0x2 // R_OK | W_OK
			return syscall.Access(path, readWrite) == nil
		},
		osRelease: func() string {
			for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
				if data, err := os.ReadFile(path); err == nil {
					return string(data)
				}
			}
			return ""
		},
		libvirtGroup: func() (bool, bool) {
			group, err := user.LookupGroup("libvirt")
			if err != nil {
				return false, false
			}
			gid, err := strconv.Atoi(group.Gid)
			if err != nil {
				return true, false
			}
			groups, err := os.Getgroups()
			return true, err == nil && (slices.Contains(groups, gid) || os.Getgid() == gid)
		},
		platform: runtime.GOOS + "/" + runtime.GOARCH,
	})
}

func doctor(h doctorHost) []domain.Capability {
	family := distroFamily(h.osRelease())
	out := []domain.Capability{kvmCheck(h), libvirtCheck(h, family)}
	if exists, member := h.libvirtGroup(); exists {
		c := check("libvirt-access", "Lets you manage system VMs (qemu:///system) without root")
		if member {
			ready(&c, "LIBVIRT_GROUP_MEMBER", "you are in the libvirt group")
		} else {
			c.Status, c.ReasonCode = "supported-with-prerequisites", "LIBVIRT_GROUP_MISSING"
			c.Reason = "you are not in the libvirt group, so system VMs may ask for a password or be refused"
			c.Alternatives = []string{"sudo usermod -aG libvirt $USER, then sign out and back in"}
		}
		out = append(out, c)
	}
	for _, d := range dependencies {
		c := check(d.command, d.purpose)
		c.Optional = d.optional
		if path, err := h.lookPath(d.command); err == nil {
			ready(&c, "DEPENDENCY_PRESENT_UNVERIFIED", "found at "+path)
		} else {
			c.Status, c.ReasonCode, c.Reason = "unsupported-on-this-configuration", "DEPENDENCY_MISSING", "not installed"
			installFor(&c, family, d.fedora, d.debian, d.command)
		}
		out = append(out, c)
	}
	host := check("host", "Support status of this operating system")
	host.Status, host.ReasonCode = "supported-with-prerequisites", "SUPPORT_UNCERTIFIED"
	host.Reason = "this host (" + h.platform + ") is not in a certified support matrix yet"
	host.EvidenceClass = "build-environment"
	return append(out, host)
}

func kvmCheck(h doctorHost) domain.Capability {
	c := check("kvm", "Hardware acceleration for VMs")
	switch {
	case !h.exists("/dev/kvm"):
		c.Status, c.ReasonCode = "unsupported-on-this-configuration", "KVM_NOT_VISIBLE"
		c.Reason = "/dev/kvm is missing, so VMs cannot use hardware acceleration"
		c.Alternatives = []string{"Turn on Intel VT-x or AMD-V in the firmware settings",
			"Load the KVM module: sudo modprobe kvm_intel (or kvm_amd)"}
	case !h.usable("/dev/kvm"):
		c.Status, c.ReasonCode = "supported-with-prerequisites", "KVM_ACCESS_DENIED"
		c.Reason = "/dev/kvm exists but you cannot open it; system VMs still work, qemu:///session VMs do not"
		c.Alternatives = []string{"sudo usermod -aG kvm $USER, then sign out and back in"}
	default:
		ready(&c, "KVM_UNVERIFIED", "/dev/kvm is present and you can open it; no VM was started to confirm")
	}
	return c
}

// libvirtCheck looks for the daemon the packages do not pull in: they need only the client library.
func libvirtCheck(h doctorHost, family string) domain.Capability {
	c := check("libvirt", "Runs and manages VMs; Virmill talks to it")
	switch {
	case h.exists("/run/libvirt/virtqemud-sock") || h.exists("/run/libvirt/libvirt-sock"):
		ready(&c, "LIBVIRT_RUNNING", "the libvirt service is running")
	case h.exists("/usr/sbin/virtqemud") || h.exists("/usr/sbin/libvirtd"):
		c.Status, c.ReasonCode = "unsupported-on-this-configuration", "LIBVIRT_STOPPED"
		c.Reason = "libvirt is installed but not running"
		unit := "libvirtd.socket"
		if h.exists("/usr/sbin/virtqemud") {
			unit = "virtqemud.socket"
		}
		c.Alternatives = []string{"sudo systemctl enable --now " + unit}
	default:
		c.Status, c.ReasonCode, c.Reason = "unsupported-on-this-configuration", "LIBVIRT_MISSING", "the libvirt service is not installed"
		installFor(&c, family, []string{"libvirt-daemon-kvm"}, []string{"libvirt-daemon-system"}, "libvirtd")
		if family == "fedora" {
			c.Alternatives = append(c.Alternatives, "Then start it: sudo systemctl enable --now virtqemud.socket")
		}
	}
	return c
}

func check(id, purpose string) domain.Capability {
	return domain.Capability{ID: id, Purpose: purpose, Alternatives: []string{}, EvidenceClass: "read-only-probe"}
}

func ready(c *domain.Capability, code, reason string) {
	c.Status, c.ReasonCode, c.Reason = "supported-with-prerequisites", code, reason
}

func installFor(c *domain.Capability, family string, fedora, debian []string, command string) {
	switch family {
	case "fedora":
		c.Packages, c.Installer = fedora, "sudo dnf install"
	case "debian":
		c.Packages, c.Installer = debian, "sudo apt install"
	default:
		c.Alternatives = append(c.Alternatives, "Install the package that provides "+command+" with your distribution's package manager")
	}
}

// distroFamily reads os-release ID and ID_LIKE: "fedora", "debian" or "".
func distroFamily(osRelease string) string {
	var ids []string
	for _, line := range strings.Split(osRelease, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && (key == "ID" || key == "ID_LIKE") {
			ids = append(ids, strings.Fields(strings.Trim(value, `"'`))...)
		}
	}
	for _, id := range ids {
		switch id {
		case "fedora", "rhel", "centos":
			return "fedora"
		case "debian", "ubuntu":
			return "debian"
		}
	}
	return ""
}
