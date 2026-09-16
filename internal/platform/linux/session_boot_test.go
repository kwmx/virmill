//go:build linux

package linux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The verdict is taken once, before this process connects to libvirt, because
// connecting can fork the unusable daemon itself. A probe that answered afresh
// each time would see that daemon's own socket and report the host healthy,
// which is exactly how the first attempt at this check stayed inert.
func TestSessionBootProbeKeepsItsVerdictWhenASocketAppearsLater(t *testing.T) {
	runtime := t.TempDir()
	if err := os.Mkdir(filepath.Join(runtime, "libvirt"), 0700); err != nil {
		t.Fatal(err)
	}
	probe := sessionBootProbe(runtime, true)
	ok, advice := probe()
	if ok || advice != SessionLibvirtAdvice {
		t.Fatal("a restricted process with no per-user socket must be refused", ok, advice)
	}
	// The advice has to work on a host that ships no per-user socket unit, so
	// it must name a plain libvirt client first.
	if !strings.Contains(advice, "virsh -c qemu:///session") {
		t.Fatal("the advice must name an action that works everywhere", advice)
	}
	socket := filepath.Join(runtime, "libvirt/virtqemud-sock")
	if err := os.WriteFile(socket, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if ok, _ = probe(); ok {
		t.Fatal("the socket the unusable daemon created must not change the verdict")
	}
	// A socket that was already there belongs to someone else's daemon.
	if ok, advice = sessionBootProbe(runtime, true)(); !ok || advice != "" {
		t.Fatal("an existing per-user socket must be allowed", ok, advice)
	}
	if err := os.Remove(socket); err != nil {
		t.Fatal(err)
	}
	if ok, _ = sessionBootProbe(runtime, true)(); ok {
		t.Fatal("removing the socket must be observed by a fresh probe")
	}
	// The restriction is read from this process, not from the runtime
	// directory, so it still decides when that directory is unknown.
	if ok, advice = sessionBootProbe("", true)(); ok || advice == "" {
		t.Fatal("a restricted process with no runtime directory must be refused, with advice", ok, advice)
	}
}

// A process that may still transition SELinux domains forks a daemon that
// works, so it is never refused, whatever its runtime directory holds.
func TestSessionBootProbeAllowsUnrestrictedHosts(t *testing.T) {
	for _, test := range []struct {
		name, runtime string
	}{
		{"no per-user socket", t.TempDir()},
		{"no runtime directory", ""},
	} {
		if ok, advice := sessionBootProbe(test.runtime, false)(); !ok || advice != "" {
			t.Fatal(test.name, ok, advice)
		}
	}
}

// NoNewPrivileges reads this process, which no test may change; it must simply
// answer without failing, and a process that has the bit cannot clear it.
func TestNoNewPrivilegesReadsThisProcess(t *testing.T) {
	if NoNewPrivileges() && !NoNewPrivileges() {
		t.Fatal("the answer must not change between reads")
	}
}
