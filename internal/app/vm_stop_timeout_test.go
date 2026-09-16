package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

// unansweredStop reports a shutdown request that timed out, leaving the VM in
// the given state.
type unansweredStop struct {
	fixtureProvider
	after string
}

func (p *unansweredStop) Execute(context.Context, string, string, string, map[string]any) error {
	p.calls++
	p.VM.State = p.after
	return domain.Fail("WAIT_TIMEOUT", "guest did not shut down")
}

// refusedBoot reports a boot that failed, leaving the VM in the given state.
// The host refuses this way when QEMU cannot be executed at all.
type refusedBoot struct {
	fixtureProvider
	after string
	saved bool
}

func (p *refusedBoot) Execute(context.Context, string, string, string, map[string]any) error {
	p.calls++
	p.VM.State, p.VM.HasManagedSave = p.after, p.saved
	return domain.Fail("OPERATION_FAILED", "cannot execute binary qemu-system-x86_64: Operation not permitted")
}

// A boot that could not reach QEMU at all is refused while it is still a plan,
// so no job is created and nothing has to be recovered. The hook stands in for
// the host check, which the service itself never performs.
func TestSessionBootIsRefusedBeforeAnyJob(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	db, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine := operations.New(db)
	defer engine.Close()
	const advice = "run a libvirt client once in a terminal"
	for _, test := range []struct {
		connection string
		hook       func() (bool, string)
		refused    bool
	}{
		{"qemu:///session", func() (bool, string) { return false, advice }, true},
		{"qemu:///session", func() (bool, string) { return true, "" }, false},
		{"qemu:///session", nil, false},                                           // a host this build cannot inspect
		{"qemu:///system", func() (bool, string) { return false, advice }, false}, // the system connection forks nothing
	} {
		p := &fixtureProvider{VM: domain.VM{Key: domain.ResourceKey{ProviderID: "fixture", ConnectionID: test.connection, Kind: "vm", UUID: "fixture-vm"}, State: "stopped", Fingerprint: "before"}}
		s := New(p, engine)
		s.SessionBoot = test.hook
		err := s.bootableConnection(test.connection)
		var de *domain.Error
		if test.refused != (err != nil) {
			t.Fatal(test.connection, err)
		}
		if test.refused && (!errors.As(err, &de) || de.Code != "UNSUPPORTED_CAPABILITY" || len(de.SafeNextActions) != 1 || de.SafeNextActions[0] != advice) {
			t.Fatal("the refusal must name the one command that fixes it", err)
		}
		if p.calls != 0 {
			t.Fatal("the provider was asked to boot a VM that cannot boot")
		}
	}
}

// A boot that never started the guest must fail plainly and free the VM. Until
// it did, a refused start held its VM in recovery-required with no disposition,
// so even forcing it off was refused RESOURCE_BUSY.
func TestRefusedBootFailsWithoutRecoveryWhenNothingStarted(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	db, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine := operations.New(db)
	defer engine.Close()
	input, _ := json.Marshal(map[string]any{"vmID": "fixture-vm"})
	for _, test := range []struct {
		action, after  string
		saved, notDone bool
	}{
		{"start", "stopped", false, true},          // nothing started: fail and free the VM
		{"start", "running", false, false},         // it started after all: keep the uncertainty
		{"start", "paused", false, false},          // something else happened: recover
		{"restore-saved", "stopped", true, true},   // the saved state is untouched
		{"restore-saved", "stopped", false, false}, // the image may have been consumed
		{"restore-saved", "running", true, false},
	} {
		p := &refusedBoot{fixtureProvider: fixtureProvider{VM: domain.VM{Key: domain.ResourceKey{ProviderID: "fixture", ConnectionID: "fixture", Kind: "vm", UUID: "fixture-vm"}, State: "stopped", Fingerprint: "before"}}, after: test.after, saved: test.saved}
		err := (&vmHandler{s: New(p, engine), action: test.action}).Execute(context.Background(), domain.Plan{ConnectionID: "fixture"}, input, domain.Step{})
		var de *domain.Error
		if err == nil || errors.Is(err, operations.ErrNotDone) != test.notDone || !errors.As(err, &de) || de.Code != "OPERATION_FAILED" {
			t.Fatal(test, err)
		}
	}
}

func TestUnansweredStopFailsWithoutRecoveryOnlyWhileTheGuestRuns(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	db, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	engine := operations.New(db)
	defer engine.Close()
	input, _ := json.Marshal(map[string]any{"vmID": "fixture-vm"})
	for _, test := range []struct {
		action, after string
		notDone, ok   bool
	}{
		{"stop", "running", true, false}, // still running: fail and free the VM
		{"stop", "stopped", false, true}, // it stopped after all: verify as usual
		{"stop", "paused", false, false}, // something else changed it: recover
		{"hard-stop", "running", false, false},
	} {
		p := &unansweredStop{fixtureProvider: fixtureProvider{VM: domain.VM{Key: domain.ResourceKey{ProviderID: "fixture", ConnectionID: "fixture", Kind: "vm", UUID: "fixture-vm"}, State: "running", Fingerprint: "before"}}, after: test.after}
		err := (&vmHandler{s: New(p, engine), action: test.action}).Execute(context.Background(), domain.Plan{ConnectionID: "fixture"}, input, domain.Step{})
		var de *domain.Error
		if test.ok != (err == nil) || errors.Is(err, operations.ErrNotDone) != test.notDone || err != nil && (!errors.As(err, &de) || de.Code != "WAIT_TIMEOUT") {
			t.Fatal(test, err)
		}
	}
}
