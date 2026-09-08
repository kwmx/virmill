//go:build linux && amd64

package helper

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
)

type networkNativeFixture struct {
	calls  int
	active bool
	failAt int
	onCall func(int)
	alter  func(*domain.VirtualNetwork)
}

func (*networkNativeFixture) CheckNetworkCreation(context.Context, string, domain.NetworkDefinition) error {
	return errors.New("unexpected create preflight in filter executor")
}
func (*networkNativeFixture) DefineNetwork(context.Context, string, domain.NetworkDefinition) error {
	panic("filter executor must not define a network")
}
func (*networkNativeFixture) ActivateNetwork(context.Context, string, domain.NetworkDefinition) error {
	panic("filter executor must not activate a network")
}
func (f *networkNativeFixture) InspectCreatedNetwork(ctx context.Context, uri string, d domain.NetworkDefinition) (domain.VirtualNetwork, error) {
	f.calls++
	if uri != "qemu:///system" {
		return domain.VirtualNetwork{}, errors.New("unexpected native connection")
	}
	if f.onCall != nil {
		f.onCall(f.calls)
	}
	if f.calls == f.failAt {
		return domain.VirtualNetwork{}, domain.Fail("SOURCE_CHANGED", "synthetic native drift")
	}
	x, err := networkxml.Render(d)
	if err != nil {
		return domain.VirtualNetwork{}, err
	}
	n := domain.VirtualNetwork{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "network", UUID: d.UUID}, Name: d.Name, Persistent: true, Active: f.active, PersistentXML: x}
	if f.active {
		n.LiveXML = x
	}
	if f.alter != nil {
		f.alter(&n)
	}
	return n, nil
}

type networkExecutorFixture struct {
	t            *testing.T
	r            Request
	p            Policy
	e            NetworkExecutor
	native       *networkNativeFixture
	views        networkRuleInventory
	passthroughs [2]string
	chains       [2]string
	adds         int
	lostAckAt    int
	calls        [][]string
	override     func([]string) (networkCommandResult, error, bool)
}

func newNetworkExecutorFixture(t *testing.T) *networkExecutorFixture {
	t.Helper()
	id := "12345678-1234-4234-8234-123456789abc"
	d := domain.NetworkDefinition{UUID: id, Name: "virmill-" + id, Bridge: "vm123456781234", Type: "lab", IPv4CIDR: "192.168.240.0/24", DHCPEnabled: true, IPv6Mode: "disabled", HostAccess: "allow", Egress: "none"}
	f := &networkExecutorFixture{t: t, native: &networkNativeFixture{}, views: networkRuleInventory{map[string]bool{}, map[string]bool{}}}
	f.r = Request{APIVersion: domain.APIVersion, ActorUID: 1000, Operation: "network.ipv6-filter", ResourceID: id, PlanDigest: strings.Repeat("a", 64), JobID: domain.ID(), KeyID: "fixture-key", ExpiresAt: time.Now().Add(time.Minute), Mode: "apply", Network: &NetworkRequest{Version: 1, Definition: d}}
	f.p = Policy{APIVersion: domain.APIVersion, Networks: []NetworkPermission{{ActorUID: f.r.ActorUID, KeyID: f.r.KeyID, ResourceID: id}}}
	f.e = NetworkExecutor{Backend: f.native, JournalPath: t.TempDir(), journalOwnerUID: uint32(os.Getuid()), run: f.command}
	if err := os.Chmod(f.e.JournalPath, 0700); err != nil {
		t.Fatal(err)
	}
	// Existing rules of other families remain byte-for-byte intact. They are
	// inventory text only and are never passed back as executable arguments.
	f.views[0]["ipv4 filter INPUT 0 -s 192.0.2.1 -j ACCEPT"] = true
	f.views[1]["ipv6 filter OUTPUT 0 -d 2001:db8::1 -j REJECT"] = true
	return f
}

func (f *networkExecutorFixture) command(_ context.Context, args []string) (networkCommandResult, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.override != nil {
		if out, err, yes := f.override(args); yes {
			return out, err
		}
	}
	if reflect.DeepEqual(args, []string{"--state"}) {
		return networkCommandResult{stdout: "running\n"}, nil
	}
	view := 0
	rest := args
	if len(rest) > 0 && rest[0] == "--permanent" {
		view = 1
		rest = rest[1:]
	}
	if reflect.DeepEqual(rest, []string{"--direct", "--get-all-rules"}) {
		lines := []string{}
		for line := range f.views[view] {
			lines = append(lines, line)
		}
		sort.Strings(lines)
		raw := ""
		if len(lines) > 0 {
			raw = strings.Join(lines, "\n") + "\n"
		}
		return networkCommandResult{stdout: raw}, nil
	}
	if reflect.DeepEqual(rest, []string{"--direct", "--get-all-passthroughs"}) {
		return networkCommandResult{stdout: f.passthroughs[view]}, nil
	}
	if reflect.DeepEqual(rest, []string{"--direct", "--get-all-chains"}) {
		return networkCommandResult{stdout: f.chains[view]}, nil
	}
	if len(rest) < 2 {
		f.t.Fatal("unexpected argv", args)
	}
	action := rest[1]
	copy := append([]string(nil), args...)
	for i, a := range copy {
		if a == "--query-rule" {
			copy[i] = "--add-rule"
		}
	}
	index := -1
	for i, known := range networkRules(f.r.Network.Definition.Bridge) {
		if reflect.DeepEqual(copy, known) {
			index = i
		}
	}
	if index < 0 {
		f.t.Fatal("unreviewed executable arguments", args)
	}
	line := networkRuleLine(copy)
	switch action {
	case "--query-rule":
		if f.views[view][line] {
			return networkCommandResult{stdout: "yes\n"}, nil
		}
		return networkCommandResult{stdout: "no\n", exit: 1}, nil
	case "--add-rule":
		// The runner sees an on-disk intent before every mutation, including a
		// later approved resume job. Real fsync errors are checked in production.
		name := networkRecordName(f.r, fmt.Sprintf("rule%d", index))
		if _, err := os.Stat(filepath.Join(f.e.JournalPath, name)); err != nil {
			f.t.Fatal("add before durable per-rule intent", err)
		}
		if f.views[view][line] {
			f.t.Fatal("executor replayed already-present rule", args)
		}
		f.views[view][line] = true
		f.adds++
		if f.adds == f.lostAckAt {
			return networkCommandResult{}, errors.New("synthetic lost add acknowledgement")
		}
		return networkCommandResult{stdout: "success\n"}, nil
	default:
		f.t.Fatal("mutation outside exact add-rule allowlist", args)
	}
	return networkCommandResult{}, errors.New("unreachable")
}

func (f *networkExecutorFixture) execute() (NetworkResponse, error) {
	return f.e.Execute(context.Background(), f.r, f.p)
}
func (f *networkExecutorFixture) snapshot() map[string][32]byte {
	f.t.Helper()
	result := map[string][32]byte{}
	entries, err := os.ReadDir(f.e.JournalPath)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join(f.e.JournalPath, entry.Name()))
		if err != nil {
			f.t.Fatal(err)
		}
		result[entry.Name()] = sha256.Sum256(b)
	}
	return result
}
func networkCode(t *testing.T, out NetworkResponse, err error, want string) {
	t.Helper()
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != want {
		t.Fatalf("got %v; want %s", err, want)
	}
	if out != (NetworkResponse{}) {
		t.Fatal("failure returned successful data", out)
	}
}
func networkSuccess(t *testing.T, out NetworkResponse, r Request) {
	t.Helper()
	want := NetworkResponse{Version: 1, ResourceID: r.ResourceID, Bridge: r.Network.Definition.Bridge, PlanDigest: r.PlanDigest, JobID: r.JobID, RuntimePresent: true, PermanentPresent: true}
	if out != want {
		t.Fatalf("got %+v; want %+v with packet proof false", out, want)
	}
}

func TestNetworkExecutorCheckHasNoMutationOrNativeExistenceRequirement(t *testing.T) {
	f := newNetworkExecutorFixture(t)
	f.r.Mode = "check"
	out, err := f.execute()
	if err != nil {
		t.Fatal(err)
	}
	if out.RuntimePresent || out.PermanentPresent || out.PacketVerified || out.JobID != f.r.JobID || f.native.calls != 0 || f.adds != 0 || len(f.snapshot()) != 0 || len(f.calls) != 15 {
		t.Fatal("check changed state or overstated its proof", out)
	}
}

func TestNetworkExecutorApplyObserveAndReplay(t *testing.T) {
	f := newNetworkExecutorFixture(t)
	out, err := f.execute()
	if err != nil {
		t.Fatal(err)
	}
	networkSuccess(t, out, f.r)
	if f.adds != 8 || f.native.calls != 10 || len(f.snapshot()) != 11 {
		t.Fatal("missing repeated native checks or exact durable records", f.adds, f.native.calls, len(f.snapshot()))
	}
	before := f.snapshot()
	commands := len(f.calls)
	out, err = f.execute()
	networkCode(t, out, err, "RECOVERY_REQUIRED")
	if len(f.calls) != commands || !reflect.DeepEqual(before, f.snapshot()) {
		t.Fatal("duplicate apply replayed or changed evidence")
	}
	f.r.Mode = "observe"
	f.r.ExpiresAt = time.Now().Add(2 * time.Minute)
	f.native.active = true
	out, err = f.execute()
	if err != nil {
		t.Fatal(err)
	}
	networkSuccess(t, out, f.r)
	if f.adds != 8 || !reflect.DeepEqual(before, f.snapshot()) {
		t.Fatal("read-only observation changed durable/native state")
	}
	if !f.views[0]["ipv4 filter INPUT 0 -s 192.0.2.1 -j ACCEPT"] || !f.views[1]["ipv6 filter OUTPUT 0 -d 2001:db8::1 -j REJECT"] {
		t.Fatal("foreign direct rules changed")
	}
}

func TestNetworkExecutorLostAcknowledgementAndFreshApprovedResume(t *testing.T) {
	for lost := 1; lost <= 8; lost++ {
		t.Run(fmt.Sprintf("rule%d", lost), func(t *testing.T) {
			f := newNetworkExecutorFixture(t)
			f.lostAckAt = lost
			out, err := f.execute()
			networkCode(t, out, err, "RECOVERY_REQUIRED")
			if f.adds != lost {
				t.Fatal("mutation continued after lost acknowledgement")
			}
			old := f.snapshot()
			if len(old) != 2+lost {
				t.Fatal("wrong partial durable intent count", len(old))
			}
			out, err = f.execute()
			networkCode(t, out, err, "RECOVERY_REQUIRED")
			if f.adds != lost {
				t.Fatal("same job replayed after uncertainty")
			}
			f.r.Mode = "observe"
			out, err = f.execute()
			if lost == 8 {
				if err != nil {
					t.Fatal(err)
				}
				networkSuccess(t, out, f.r)
			} else {
				networkCode(t, out, err, "RECOVERY_REQUIRED")
			}
			if !reflect.DeepEqual(old, f.snapshot()) {
				t.Fatal("observation changed old records")
			}
			f.r.Mode = "apply"
			f.r.JobID = domain.ID()
			f.r.PlanDigest = strings.Repeat("b", 64)
			f.lostAckAt = 0
			out, err = f.execute()
			if err != nil {
				t.Fatal(err)
			}
			networkSuccess(t, out, f.r)
			if f.adds != 8 {
				t.Fatal("resume replayed existing rules or omitted missing rules", f.adds)
			}
			current := f.snapshot()
			for name, hash := range old {
				if current[name] != hash {
					t.Fatal("resume rewrote old job evidence", name)
				}
			}
			f.r.Mode = "observe"
			out, err = f.execute()
			if err != nil {
				t.Fatal(err)
			}
			networkSuccess(t, out, f.r)
		})
	}
}

func TestNetworkExecutorJournalFaultPreventsUnrecordedRule(t *testing.T) {
	for _, stage := range []struct {
		suffix string
		adds   int
	}{{"owner.json", 0}, {"intent.json", 0}, {"rule3.json", 3}, {"complete.json", 8}} {
		t.Run(stage.suffix, func(t *testing.T) {
			f := newNetworkExecutorFixture(t)
			f.e.beforePublish = func(name string) error {
				if strings.HasSuffix(name, stage.suffix) {
					return errors.New("synthetic durable publication failure")
				}
				return nil
			}
			out, err := f.execute()
			networkCode(t, out, err, "RECOVERY_REQUIRED")
			if f.adds != stage.adds {
				t.Fatal("mutation crossed failed publication", f.adds)
			}
			if stage.adds == 8 {
				f.r.Mode = "observe"
				out, err = f.execute()
				if err != nil {
					t.Fatal(err)
				}
				networkSuccess(t, out, f.r)
			}
		})
	}
}

func TestNetworkExecutorCancellationPreservesStateWithoutProof(t *testing.T) {
	for _, at := range []string{"before", "initial-query", "before-rule", "last-native"} {
		t.Run(at, func(t *testing.T) {
			f := newNetworkExecutorFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch at {
			case "before":
				cancel()
			case "initial-query":
				f.override = func(args []string) (networkCommandResult, error, bool) {
					if strings.Contains(strings.Join(args, " "), "--query-rule") {
						cancel()
					}
					return networkCommandResult{}, nil, false
				}
			case "before-rule":
				f.e.beforePublish = func(name string) error {
					if strings.HasSuffix(name, "rule0.json") {
						cancel()
					}
					return nil
				}
			case "last-native":
				f.native.onCall = func(n int) {
					if n == 10 {
						cancel()
					}
				}
			}
			out, err := f.e.Execute(ctx, f.r, f.p)
			if err == nil || out != (NetworkResponse{}) {
				t.Fatal("cancellation returned success", out, err)
			}
			if at != "last-native" && f.adds != 0 {
				t.Fatal("mutation after cancellation", f.adds)
			}
			if at == "last-native" {
				if f.adds != 8 {
					t.Fatal("fixture did not reach final observation")
				}
				before := f.snapshot()
				f.native.onCall = nil
				f.r.Mode = "observe"
				out, err = f.execute()
				if err != nil {
					t.Fatal(err)
				}
				networkSuccess(t, out, f.r)
				if !reflect.DeepEqual(before, f.snapshot()) {
					t.Fatal("recovery observation changed canceled evidence")
				}
			}
		})
	}
}

func TestNetworkExecutorNativeFailureRetainsPartialRules(t *testing.T) {
	f := newNetworkExecutorFixture(t)
	f.native.failAt = 3
	out, err := f.execute()
	networkCode(t, out, err, "SOURCE_CHANGED")
	if f.adds != 1 || len(f.snapshot()) != 3 {
		t.Fatal("native drift did not stop before next mutation")
	}
	f.native.failAt = 0
	f.r.JobID = domain.ID()
	f.r.PlanDigest = strings.Repeat("b", 64)
	out, err = f.execute()
	if err != nil {
		t.Fatal(err)
	}
	networkSuccess(t, out, f.r)
	if f.adds != 8 {
		t.Fatal("fresh resume did not reuse proven rule")
	}
}

func TestNetworkExecutorUnownedRulesAndExternalChangesRefused(t *testing.T) {
	t.Run("unowned exact rule", func(t *testing.T) {
		f := newNetworkExecutorFixture(t)
		f.r.Mode = "check"
		f.views[0][networkRuleLine(networkRules(f.r.Network.Definition.Bridge)[0])] = true
		out, err := f.execute()
		networkCode(t, out, err, "RESOURCE_BUSY")
		if f.adds != 0 || len(f.snapshot()) != 0 {
			t.Fatal("adopted foreign rule")
		}
	})
	t.Run("foreign bypass", func(t *testing.T) {
		f := newNetworkExecutorFixture(t)
		f.views[0]["eb filter FORWARD -32769 -j ACCEPT"] = true
		out, err := f.execute()
		networkCode(t, out, err, "UNSUPPORTED_CAPABILITY")
		if f.adds != 0 || len(f.snapshot()) != 0 {
			t.Fatal("continued with possible bypass")
		}
	})
	t.Run("foreign family changes during apply", func(t *testing.T) {
		f := newNetworkExecutorFixture(t)
		f.override = func(args []string) (networkCommandResult, error, bool) {
			if f.adds == 1 {
				f.views[1]["ipv4 filter OUTPUT 0 -j DROP"] = true
			}
			return networkCommandResult{}, nil, false
		}
		out, err := f.execute()
		networkCode(t, out, err, "SOURCE_CHANGED")
		if f.adds != 1 {
			t.Fatal("ignored external change")
		}
	})
}

func TestNetworkExecutorResponseFailuresAndAuthority(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*networkExecutorFixture)
		code   string
	}{
		{"missing backend", func(f *networkExecutorFixture) { f.e.Backend = nil }, "UNSUPPORTED_CAPABILITY"},
		{"permission", func(f *networkExecutorFixture) { f.p.Networks = nil }, "PERMISSION_DENIED"},
		{"malformed digest", func(f *networkExecutorFixture) { f.r.PlanDigest = "x" }, "INVALID_INPUT"},
		{"expired", func(f *networkExecutorFixture) { f.r.ExpiresAt = time.Now() }, "INVALID_INPUT"},
		{"active apply", func(f *networkExecutorFixture) { f.native.active = true }, "SOURCE_CHANGED"},
		{"not running", func(f *networkExecutorFixture) {
			f.override = func(a []string) (networkCommandResult, error, bool) {
				return networkCommandResult{stdout: "not running\n", exit: 252}, nil, true
			}
		}, "UNSUPPORTED_CAPABILITY"},
		{"stderr diagnostic", func(f *networkExecutorFixture) {
			f.override = func(a []string) (networkCommandResult, error, bool) {
				return networkCommandResult{stdout: "running\n", stderr: "warning\n"}, nil, true
			}
		}, "UNSUPPORTED_CAPABILITY"},
		{"oversized", func(f *networkExecutorFixture) {
			f.override = func(a []string) (networkCommandResult, error, bool) {
				return networkCommandResult{stdout: strings.Repeat("x", networkOutputLimit+1)}, nil, true
			}
		}, "UNSUPPORTED_CAPABILITY"},
		{"bad exact query", func(f *networkExecutorFixture) {
			f.override = func(a []string) (networkCommandResult, error, bool) {
				if strings.Contains(strings.Join(a, " "), "--query-rule") {
					return networkCommandResult{stdout: "no\n", exit: 0}, nil, true
				}
				return networkCommandResult{}, nil, false
			}
		}, "UNSUPPORTED_CAPABILITY"},
		{"query inventory disagreement", func(f *networkExecutorFixture) {
			f.override = func(a []string) (networkCommandResult, error, bool) {
				if strings.Contains(strings.Join(a, " "), "--query-rule") {
					return networkCommandResult{stdout: "yes\n"}, nil, true
				}
				return networkCommandResult{}, nil, false
			}
		}, "SOURCE_CHANGED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNetworkExecutorFixture(t)
			tc.change(f)
			out, err := f.execute()
			networkCode(t, out, err, tc.code)
			if f.adds != 0 || len(f.snapshot()) != 0 {
				t.Fatal("failure created authority or mutation")
			}
		})
	}
}

func TestNetworkExecutorResourceAndJobBindingRefusals(t *testing.T) {
	for _, field := range []string{"actor", "key", "definition", "plan", "missing-rule-intent", "corrupt-rule"} {
		t.Run(field, func(t *testing.T) {
			f := newNetworkExecutorFixture(t)
			out, err := f.execute()
			if err != nil {
				t.Fatal(err)
			}
			networkSuccess(t, out, f.r)
			f.r.Mode = "observe"
			want := "SOURCE_CHANGED"
			switch field {
			case "actor":
				f.r.ActorUID++
				f.p.Networks[0].ActorUID = f.r.ActorUID
			case "key":
				f.r.KeyID = "other"
				f.p.Networks[0].KeyID = f.r.KeyID
			case "definition":
				copy := *f.r.Network
				copy.Definition.IPv4CIDR = "10.1.0.0/16"
				f.r.Network = &copy
			case "plan":
				f.r.PlanDigest = strings.Repeat("b", 64)
			case "missing-rule-intent":
				if err = os.Remove(filepath.Join(f.e.JournalPath, networkRecordName(f.r, "rule3"))); err != nil {
					t.Fatal(err)
				}
				want = "RECOVERY_REQUIRED"
			case "corrupt-rule":
				if err = os.WriteFile(filepath.Join(f.e.JournalPath, networkRecordName(f.r, "rule3")), []byte(`{"version":1,"version":1}`), 0600); err != nil {
					t.Fatal(err)
				}
				want = "RECOVERY_REQUIRED"
			}
			before := f.snapshot()
			out, err = f.execute()
			networkCode(t, out, err, want)
			if f.adds != 8 || !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("binding refusal changed resources or evidence")
			}
		})
	}
}

func TestNetworkExecutorJournalSpecialFilesAndPermissions(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo", "public-file", "public-directory", "wrong-owner", "journal-symlink"} {
		t.Run(kind, func(t *testing.T) {
			f := newNetworkExecutorFixture(t)
			f.r.Mode = "check"
			path := filepath.Join(f.e.JournalPath, networkPrefix(f.r.ResourceID)+"owner.json")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink("absent-generated-fixture", path)
			case "hardlink":
				if err = os.WriteFile(path, []byte(`{}`), 0600); err == nil {
					err = os.Link(path, path+".link")
				}
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "public-file":
				err = os.WriteFile(path, []byte(`{}`), 0644)
			case "public-directory":
				err = os.Chmod(f.e.JournalPath, 0755)
			case "wrong-owner":
				f.e.journalOwnerUID = uint32(os.Getuid()) + 1
			case "journal-symlink":
				link := filepath.Join(t.TempDir(), "link")
				err = os.Symlink(f.e.JournalPath, link)
				f.e.JournalPath = link
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			out, err := f.e.Execute(ctx, f.r, f.p)
			if err == nil || out != (NetworkResponse{}) || f.adds != 0 || len(f.calls) != 0 {
				t.Fatal("unsafe journal accepted or touched firewall", out, err)
			}
		})
	}
}

func TestNetworkDirectRuleParser(t *testing.T) {
	valid := "eb filter FORWARD -32768 --logical-in vm123456781234 -p IPv6 -j DROP\n"
	if out, err := parseNetworkRules(valid + "ipv4 filter INPUT 0 -j ACCEPT\n"); err != nil || len(out) != 2 {
		t.Fatal(out, err)
	}
	cases := map[string]string{
		"ACCEPT": strings.Replace(valid, "DROP", "ACCEPT", 1), "unknown bridge": strings.Replace(valid, "vm123456781234", "br0", 1),
		"wrong priority": strings.Replace(valid, "-32768", "-32769", 1), "wrong table": strings.Replace(valid, "filter", "nat", 1),
		"unknown chain": strings.Replace(valid, "FORWARD", "FORWARD_direct", 1), "wrong protocol": strings.Replace(valid, "IPv6", "IPv4", 1),
		"interface wildcard": strings.Replace(valid, "vm123456781234", "vm12345678123+", 1), "unknown qualifier": strings.Replace(valid, "--logical-in", "-i", 1),
		"duplicate": valid + valid, "no newline": strings.TrimSuffix(valid, "\n"), "blank line": valid + "\n", "control": valid + "\x00\n",
		"unknown family": "bridge filter INPUT 0 -j DROP\n", "oversized": strings.Repeat("x", networkOutputLimit+1),
		"extra predicate": strings.Replace(valid, "-j DROP", "-s 00:00:00:00:00:00 -j DROP", 1), "input wrong direction": strings.Replace(valid, "FORWARD", "OUTPUT", 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseNetworkRules(raw); err == nil {
				t.Fatal("accepted ambiguous/potential bypass rule")
			}
		})
	}
}

func TestNetworkCommandOutputBound(t *testing.T) {
	var out networkOutput
	if n, err := out.Write(make([]byte, networkOutputLimit)); n != networkOutputLimit || err != nil {
		t.Fatal(n, err)
	}
	if _, err := out.Write([]byte("x")); err == nil || !out.exceeded || out.Len() != networkOutputLimit {
		t.Fatal("output overflow did not fail closed")
	}
}

func TestNetworkFixedCommandContract(t *testing.T) {
	bridge := "vm123456781234"
	want := []string{
		"--direct --add-rule eb filter INPUT -32768 --logical-in vm123456781234 -p IPv6 -j DROP",
		"--direct --add-rule eb filter OUTPUT -32768 --logical-out vm123456781234 -p IPv6 -j DROP",
		"--direct --add-rule eb filter FORWARD -32768 --logical-in vm123456781234 -p IPv6 -j DROP",
		"--direct --add-rule eb filter FORWARD -32768 --logical-out vm123456781234 -p IPv6 -j DROP",
	}
	for i, args := range networkRules(bridge) {
		expected := want[i%4]
		if i >= 4 {
			expected = "--permanent " + expected
		}
		if strings.Join(args, " ") != expected {
			t.Fatal("changed fixed command/versioned intent contract", i, args)
		}
	}
}

func TestNetworkExecutorRepeatsIndependentNativePredicate(t *testing.T) {
	for name, alter := range map[string]func(*domain.VirtualNetwork){
		"wrong key":        func(n *domain.VirtualNetwork) { n.Key.UUID = domain.ID() },
		"wrong connection": func(n *domain.VirtualNetwork) { n.Key.ConnectionID = "qemu:///session" },
		"wrong name":       func(n *domain.VirtualNetwork) { n.Name = "foreign" },
		"autostart":        func(n *domain.VirtualNetwork) { n.Autostart = true },
		"transient":        func(n *domain.VirtualNetwork) { n.Persistent = false },
		"empty XML":        func(n *domain.VirtualNetwork) { n.PersistentXML = "" },
		"changed XML": func(n *domain.VirtualNetwork) {
			n.PersistentXML = strings.Replace(n.PersistentXML, `zone="trusted"`, `zone="public"`, 1)
		},
		"unexpected live": func(n *domain.VirtualNetwork) { n.LiveXML = n.PersistentXML },
	} {
		t.Run(name, func(t *testing.T) {
			f := newNetworkExecutorFixture(t)
			f.native.alter = alter
			out, err := f.execute()
			networkCode(t, out, err, "SOURCE_CHANGED")
			if len(f.calls) != 0 || f.adds != 0 || len(f.snapshot()) != 0 {
				t.Fatal("invalid native evidence reached firewall or journal mutation")
			}
		})
	}
}

func TestNetworkJournalCooperativeWriterExclusion(t *testing.T) {
	f := newNetworkExecutorFixture(t)
	held, err := f.e.openJournal()
	if err != nil {
		t.Fatal(err)
	}
	defer held.file.Close()
	out, err := f.execute()
	networkCode(t, out, err, "RESOURCE_BUSY")
	if len(f.calls) != 0 || f.adds != 0 || len(f.snapshot()) != 0 {
		t.Fatal("second writer reached native mutation")
	}
}

func TestNetworkExecutorTrackedPassthroughAndChainBoundary(t *testing.T) {
	for _, kind := range []string{"passthroughs", "chains"} {
		for view := 0; view < 2; view++ {
			t.Run(fmt.Sprintf("%s/%d", kind, view), func(t *testing.T) {
				f := newNetworkExecutorFixture(t)
				if kind == "passthroughs" {
					f.passthroughs[view] = "eb -t filter -I FORWARD 1 -j ACCEPT\n"
				} else {
					f.chains[view] = "eb filter foreign\n"
				}
				out, err := f.execute()
				networkCode(t, out, err, "UNSUPPORTED_CAPABILITY")
				if f.adds != 0 || len(f.snapshot()) != 0 {
					t.Fatal("tracked bridge bypass reached mutation")
				}
			})
		}
	}
	t.Run("other families retained", func(t *testing.T) {
		f := newNetworkExecutorFixture(t)
		f.passthroughs = [2]string{"ipv4 -t filter -A OUTPUT -j ACCEPT\n", "ipv6 -t filter -A OUTPUT -j DROP\n"}
		f.chains = [2]string{"ipv4 filter CUSTOM\n", "ipv6 filter CUSTOM\n"}
		oldPass, oldChains := f.passthroughs, f.chains
		out, err := f.execute()
		if err != nil {
			t.Fatal(err)
		}
		networkSuccess(t, out, f.r)
		if f.passthroughs != oldPass || f.chains != oldChains {
			t.Fatal("foreign tracked configuration changed")
		}
	})
	t.Run("other family change detected", func(t *testing.T) {
		f := newNetworkExecutorFixture(t)
		f.override = func(a []string) (networkCommandResult, error, bool) {
			if f.adds == 1 {
				f.chains[1] = "ipv4 filter ADMIN-CHANGED\n"
			}
			return networkCommandResult{}, nil, false
		}
		out, err := f.execute()
		networkCode(t, out, err, "SOURCE_CHANGED")
		if f.adds != 1 {
			t.Fatal("mutation continued after administrator changed tracked inventory")
		}
	})
	for _, raw := range []string{"eb filter chain\n", "ipv4 filter chain", "ipv4 filter chain\n\n", "ipv4 filter chain\nipv4 filter chain\n", "ipv4 filter chain extra\n", "unknown filter chain\n", "ipv4 filter \x00\n"} {
		if _, err := parseNetworkOther(raw, "chains"); err == nil {
			t.Fatal("malformed tracked chain inventory accepted", raw)
		}
	}
}
