//go:build linux

package linux

import (
	"os"
	"os/exec"
	"runtime"
	"virmill.local/core/internal/domain"
)

func Doctor() []domain.Capability {
	out := []domain.Capability{}
	if _, e := os.Stat("/dev/kvm"); e != nil {
		out = append(out, domain.Capability{ID: "kvm", Status: "unsupported-on-this-configuration", ReasonCode: "KVM_NOT_VISIBLE", Reason: "/dev/kvm is absent or inaccessible; hardware acceleration cannot be verified", Alternatives: []string{"Use an authorized Linux x86-64 KVM host"}, EvidenceClass: "read-only-probe"})
	} else {
		out = append(out, domain.Capability{ID: "kvm", Status: "supported-with-prerequisites", ReasonCode: "KVM_UNVERIFIED", Reason: "device exists; access and guest execution require separate qualification", Alternatives: []string{}, EvidenceClass: "read-only-probe"})
	}
	for _, name := range []string{"qemu-img", "bwrap", "restic", "virt-v2v", "remote-viewer", "swtpm", "xorriso", "prlimit"} {
		p, e := exec.LookPath(name)
		status, code, reason := "supported-with-prerequisites", "DEPENDENCY_PRESENT_UNVERIFIED", p
		if e != nil {
			status = "unsupported-on-this-configuration"
			code = "DEPENDENCY_MISSING"
			reason = "executable missing from PATH"
		}
		out = append(out, domain.Capability{ID: name, Status: status, ReasonCode: code, Reason: reason, Alternatives: []string{"Review distro dependency setup instructions"}, EvidenceClass: "read-only-probe"})
	}
	out = append(out, domain.Capability{ID: "host", Status: "supported-with-prerequisites", ReasonCode: "SUPPORT_UNCERTIFIED", Reason: runtime.GOOS + "/" + runtime.GOARCH + "; no certified release matrix yet", Alternatives: []string{}, EvidenceClass: "build-environment"})
	return out
}
