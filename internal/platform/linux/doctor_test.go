//go:build linux

package linux

import (
	"errors"
	"reflect"
	"testing"

	"virmill.local/core/internal/domain"
)

type fakeHost struct {
	tools        map[string]string
	paths        map[string]bool
	kvmUsable    bool
	osRelease    string
	runtime      string
	group, inGrp bool
}

func (f fakeHost) host() doctorHost {
	return doctorHost{
		lookPath: func(name string) (string, error) {
			if path, ok := f.tools[name]; ok {
				return path, nil
			}
			return "", errors.New("not found")
		},
		exists:       func(path string) bool { return f.paths[path] },
		runtimeDir:   func() string { return f.runtime },
		usable:       func(string) bool { return f.kvmUsable },
		osRelease:    func() string { return f.osRelease },
		libvirtGroup: func() (bool, bool) { return f.group, f.inGrp },
		platform:     "linux/amd64",
	}
}

func byID(t *testing.T, checks []domain.Capability, id string) domain.Capability {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no %q check in %+v", id, checks)
	return domain.Capability{}
}

func allTools() map[string]string {
	tools := map[string]string{}
	for _, d := range dependencies {
		tools[d.command] = "/usr/bin/" + d.command
	}
	return tools
}

func TestDoctorSuggestsDistributionPackagesForMissingTools(t *testing.T) {
	for _, tc := range []struct {
		release, installer string
		qemuImg            []string
	}{
		{"NAME=Fedora\nID=fedora\n", "sudo dnf install", []string{"qemu-img"}},
		{"ID=ubuntu\nID_LIKE=debian\n", "sudo apt install", []string{"qemu-utils"}},
		{"ID=\"rocky\"\nID_LIKE=\"rhel centos fedora\"\n", "sudo dnf install", []string{"qemu-img"}},
	} {
		tools := allTools()
		delete(tools, "qemu-img")
		delete(tools, "restic")
		checks := doctor(fakeHost{tools: tools, osRelease: tc.release}.host())
		qemu := byID(t, checks, "qemu-img")
		if qemu.ReasonCode != "DEPENDENCY_MISSING" || qemu.Installer != tc.installer || !reflect.DeepEqual(qemu.Packages, tc.qemuImg) || qemu.Optional {
			t.Fatalf("%s: qemu-img guidance %+v", tc.release, qemu)
		}
		restic := byID(t, checks, "restic")
		if !restic.Optional || restic.Purpose == "" || !reflect.DeepEqual(restic.Packages, []string{"restic"}) {
			t.Fatalf("restic guidance %+v", restic)
		}
		bwrap := byID(t, checks, "bwrap")
		if bwrap.ReasonCode != "DEPENDENCY_PRESENT_UNVERIFIED" || bwrap.Reason != "found at /usr/bin/bwrap" || len(bwrap.Packages) != 0 {
			t.Fatalf("present tool reported %+v", bwrap)
		}
	}
}

func TestDoctorUnknownDistributionGivesGenericAdvice(t *testing.T) {
	checks := doctor(fakeHost{tools: map[string]string{}, osRelease: "ID=arch\n"}.host())
	swtpm := byID(t, checks, "swtpm")
	if swtpm.Installer != "" || len(swtpm.Packages) != 0 || len(swtpm.Alternatives) != 1 {
		t.Fatalf("unknown distribution guessed a package manager: %+v", swtpm)
	}
}

func TestDoctorChecksToolsVirmillRuns(t *testing.T) {
	checks := doctor(fakeHost{tools: allTools()}.host())
	for _, c := range checks {
		if c.ID == "virt-v2v" || c.ID == "remote-viewer" {
			t.Fatalf("doctor checks %s, which Virmill never runs", c.ID)
		}
	}
	if byID(t, checks, "virt-viewer").ReasonCode != "DEPENDENCY_PRESENT_UNVERIFIED" {
		t.Fatal("console viewer not checked")
	}
}

func TestDoctorExplainsKVMAccess(t *testing.T) {
	for _, tc := range []struct {
		present, usable bool
		code            string
	}{{false, false, "KVM_NOT_VISIBLE"}, {true, false, "KVM_ACCESS_DENIED"}, {true, true, "KVM_UNVERIFIED"}} {
		kvm := byID(t, doctor(fakeHost{paths: map[string]bool{"/dev/kvm": tc.present}, kvmUsable: tc.usable}.host()), "kvm")
		if kvm.ReasonCode != tc.code || (tc.code != "KVM_UNVERIFIED") != (len(kvm.Alternatives) > 0) {
			t.Fatalf("kvm present=%v usable=%v: %+v", tc.present, tc.usable, kvm)
		}
	}
}

func TestDoctorFindsMissingOrStoppedLibvirt(t *testing.T) {
	fedora := "ID=fedora\n"
	missing := byID(t, doctor(fakeHost{osRelease: fedora}.host()), "libvirt")
	if missing.ReasonCode != "LIBVIRT_MISSING" || !reflect.DeepEqual(missing.Packages, []string{"libvirt-daemon-kvm"}) || len(missing.Alternatives) != 1 {
		t.Fatalf("missing libvirt on Fedora: %+v", missing)
	}
	debian := byID(t, doctor(fakeHost{osRelease: "ID=debian\n"}.host()), "libvirt")
	if !reflect.DeepEqual(debian.Packages, []string{"libvirt-daemon-system"}) || len(debian.Alternatives) != 0 {
		t.Fatalf("missing libvirt on Debian: %+v", debian)
	}
	stopped := byID(t, doctor(fakeHost{paths: map[string]bool{"/usr/sbin/virtqemud": true}}.host()), "libvirt")
	if stopped.ReasonCode != "LIBVIRT_STOPPED" || !reflect.DeepEqual(stopped.Alternatives, []string{"sudo systemctl enable --now virtqemud.socket"}) {
		t.Fatalf("stopped libvirt: %+v", stopped)
	}
	running := byID(t, doctor(fakeHost{paths: map[string]bool{"/run/libvirt/libvirt-sock": true}}.host()), "libvirt")
	if running.ReasonCode != "LIBVIRT_RUNNING" || len(running.Alternatives) != 0 {
		t.Fatalf("running libvirt: %+v", running)
	}
}

// Nothing starts libvirt for a user session on some hosts, and then the first
// program to use the per-user connection forks the daemon itself. Started from
// a hardened service that daemon can never execute QEMU, so the check asks for
// socket activation while the system service alone looks healthy.
func TestDoctorAsksForPerUserLibvirtActivation(t *testing.T) {
	system := map[string]bool{"/run/libvirt/virtqemud-sock": true}
	unactivated := byID(t, doctor(fakeHost{paths: system, runtime: "/run/user/1000"}.host()), "libvirt")
	if unactivated.ReasonCode != "LIBVIRT_SESSION_UNACTIVATED" || len(unactivated.Alternatives) != 2 ||
		unactivated.Alternatives[0] != "virsh -c qemu:///session list --all" {
		t.Fatalf("per-user libvirt not started: %+v", unactivated)
	}
	// Once a per-user socket exists, the host is simply ready.
	for _, name := range []string{"libvirt/virtqemud-sock", "libvirt/libvirt-sock"} {
		paths := map[string]bool{"/run/libvirt/virtqemud-sock": true, "/run/user/1000/" + name: true}
		ready := byID(t, doctor(fakeHost{paths: paths, runtime: "/run/user/1000"}.host()), "libvirt")
		if ready.ReasonCode != "LIBVIRT_RUNNING" || len(ready.Alternatives) != 0 {
			t.Fatalf("per-user libvirt started through %s: %+v", name, ready)
		}
	}
	// A host that reports no runtime directory keeps the old verdict.
	unknown := byID(t, doctor(fakeHost{paths: system}.host()), "libvirt")
	if unknown.ReasonCode != "LIBVIRT_RUNNING" {
		t.Fatalf("no runtime directory: %+v", unknown)
	}
}

func TestDoctorLibvirtGroupCheckOnlyWhenGroupExists(t *testing.T) {
	for _, c := range doctor(fakeHost{}.host()) {
		if c.ID == "libvirt-access" {
			t.Fatal("reported group membership for a host without a libvirt group")
		}
	}
	outside := byID(t, doctor(fakeHost{group: true}.host()), "libvirt-access")
	if outside.ReasonCode != "LIBVIRT_GROUP_MISSING" || len(outside.Alternatives) != 1 {
		t.Fatalf("non-member guidance %+v", outside)
	}
	if byID(t, doctor(fakeHost{group: true, inGrp: true}.host()), "libvirt-access").ReasonCode != "LIBVIRT_GROUP_MEMBER" {
		t.Fatal("member not recognized")
	}
}
