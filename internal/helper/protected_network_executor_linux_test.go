//go:build linux && amd64

package helper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

// The seam models firewalld's managed inventory separately from the kernel body
// so tests can inject drift/bypass without making real firewall calls.
type protectedExecutorFixture struct {
	*networkExecutorFixture
	trampoline     bool
	kernelCalls    int
	kernelOverride func(string, string) string
}

func newProtectedExecutorFixture(t *testing.T, kind string, dhcp, trampoline bool) *protectedExecutorFixture {
	f := &protectedExecutorFixture{networkExecutorFixture: newNetworkExecutorFixture(t), trampoline: trampoline}
	f.r.Operation = "network.policy-filter"
	f.r.Network.Version = 2
	f.r.Network.Definition.HostAccess = "services-only"
	f.r.Network.Definition.Type = kind
	f.r.Network.Definition.DHCPEnabled = dhcp
	if kind == "nat" {
		f.r.Network.Definition.Egress = "any"
		f.r.Network.Definition.AdvertiseDefaultRoute = dhcp
	}
	if kind == "guest-only" {
		f.r.Network.Definition.HostAccess = "deny"
		f.r.Network.Definition.IPv4CIDR = ""
	}
	f.p.ProtectedNetworks = append([]NetworkPermission{}, f.p.Networks...)
	f.p.Networks = nil
	f.e.run = f.command
	return f
}
func (f *protectedExecutorFixture) command(ctx context.Context, args []string) (networkCommandResult, error) {
	if len(args) == 7 && reflect.DeepEqual(args[:6], []string{"--direct", "--passthrough", "ipv4", "-t", "mangle", "-S"}) {
		f.calls = append(f.calls, append([]string{}, args...))
		f.kernelCalls++
		if f.override != nil {
			if out, err, yes := f.override(args); yes {
				return out, err
			}
		}
		chain := args[6]
		var body string
		if chain == "INPUT" {
			body = "-P INPUT ACCEPT\n"
		} else if chain == "INPUT_direct" && f.trampoline {
			body = "-N INPUT_direct\n"
		} else {
			f.t.Fatal("unnecessary or unreviewed kernel chain", chain)
		}
		if chain == "INPUT" && f.trampoline {
			body += "-A INPUT -j INPUT_direct\n"
		} else {
			type rule struct {
				priority int
				body     string
			}
			var rules []rule
			for line := range f.views[0] {
				fields := strings.Fields(line)
				if len(fields) > 4 && reflect.DeepEqual(fields[:3], []string{"ipv4", "mangle", "INPUT"}) {
					priority, _ := strconv.Atoi(fields[3])
					rules = append(rules, rule{priority, strings.Join(fields[4:], " ")})
				}
			}
			sort.Slice(rules, func(i, j int) bool {
				if rules[i].priority != rules[j].priority {
					return rules[i].priority < rules[j].priority
				}
				return rules[i].body < rules[j].body
			})
			for _, r := range rules {
				body += "-A " + chain + " " + r.body + "\n"
			}
		}
		if f.kernelOverride != nil {
			body = f.kernelOverride(chain, body)
		}
		return networkCommandResult{stdout: body + "\nsuccess\n"}, nil
	}
	return f.networkExecutorFixture.command(ctx, args)
}

func TestProtectedNetworkFixedRulesAndVersionOneCompatibility(t *testing.T) {
	for _, test := range []struct {
		kind  string
		dhcp  bool
		count int
	}{{"nat", true, 18}, {"nat", false, 10}, {"lab", true, 18}, {"lab", false, 10}, {"guest-only", false, 16}} {
		t.Run(fmt.Sprintf("%s/%t", test.kind, test.dhcp), func(t *testing.T) {
			f := newProtectedExecutorFixture(t, test.kind, test.dhcp, false)
			rules := networkPolicyRules(f.r.Network.Definition)
			if len(rules) != test.count {
				t.Fatal("unbounded/wrong rules", len(rules))
			}
			for i := 0; i < len(rules)/2; i++ {
				if !reflect.DeepEqual(rules[i+len(rules)/2], append([]string{"--permanent"}, rules[i]...)) {
					t.Fatal("runtime/permanent policy differs")
				}
			}
			if test.kind != "guest-only" {
				wantDrop := "--direct --add-rule ipv4 mangle INPUT -32763 -i vm123456781234 -j DROP"
				if strings.Join(rules[len(rules)/2-1], " ") != wantDrop {
					t.Fatal("host drop does not precede filter INPUT")
				}
				if test.dhcp {
					expected := []string{
						"--direct --add-rule ipv4 mangle INPUT -32767 -d 255.255.255.255/32 -i vm123456781234 -p udp -m udp --sport 68 --dport 67 -j ACCEPT",
						"--direct --add-rule ipv4 mangle INPUT -32766 -d 192.168.240.1/32 -i vm123456781234 -p udp -m udp --sport 68 --dport 67 -j ACCEPT",
						"--direct --add-rule ipv4 mangle INPUT -32765 -d 192.168.240.1/32 -i vm123456781234 -p udp -m udp --dport 53 -j ACCEPT",
						"--direct --add-rule ipv4 mangle INPUT -32764 -d 192.168.240.1/32 -i vm123456781234 -p tcp -m tcp --dport 53 -j ACCEPT",
					}
					for i, line := range expected {
						if strings.Join(rules[i+4], " ") != line {
							t.Fatal("service exception widened", rules[i+4])
						}
					}
				}
			} else {
				expected := []string{"INPUT -32768 --logical-in vm123456781234 -p IPv4 -j DROP", "INPUT -32768 --logical-in vm123456781234 -p ARP -j DROP", "OUTPUT -32768 --logical-out vm123456781234 -p IPv4 -j DROP", "OUTPUT -32768 --logical-out vm123456781234 -p ARP -j DROP"}
				for i, tail := range expected {
					if strings.Join(rules[i+4], " ") != "--direct --add-rule eb filter "+tail {
						t.Fatal("guest-only changes peer forwarding", rules[i+4])
					}
				}
			}
		})
	}
	legacy := newNetworkExecutorFixture(t)
	got := networkPolicyRules(legacy.r.Network.Definition)
	want := networkRules(legacy.r.Network.Definition.Bridge)
	if !reflect.DeepEqual(got, want[:]) {
		t.Fatal("v1 rules changed")
	}
}

func TestProtectedNetworkApplyObserveAndKernelLayouts(t *testing.T) {
	for _, trampoline := range []bool{false, true} {
		for _, kind := range []string{"nat", "lab", "guest-only"} {
			t.Run(fmt.Sprintf("%s/trampoline=%t", kind, trampoline), func(t *testing.T) {
				f := newProtectedExecutorFixture(t, kind, kind != "guest-only", trampoline)
				out, err := f.execute()
				if err != nil {
					t.Fatal(err)
				}
				networkSuccess(t, out, f.r)
				count := len(networkPolicyRules(f.r.Network.Definition))
				if f.adds != count || f.kernelCalls < 2*(count+2) || len(f.snapshot()) != count+3 {
					t.Fatal("missing bounded rule intents/repeated reachability", f.adds, f.kernelCalls, len(f.snapshot()))
				}
				before := f.snapshot()
				calls := len(f.calls)
				out, err = f.execute()
				networkCode(t, out, err, "RECOVERY_REQUIRED")
				if len(f.calls) != calls || !reflect.DeepEqual(before, f.snapshot()) {
					t.Fatal("same job replayed")
				}
				f.r.Mode = "observe"
				f.native.active = true
				out, err = f.execute()
				if err != nil {
					t.Fatal(err)
				}
				networkSuccess(t, out, f.r)
				if f.adds != count || !reflect.DeepEqual(before, f.snapshot()) {
					t.Fatal("observe changed firewall/journal")
				}
				for _, args := range f.calls {
					if strings.Contains(strings.Join(args, " "), "--passthrough") && !reflect.DeepEqual(args[:6], []string{"--direct", "--passthrough", "ipv4", "-t", "mangle", "-S"}) {
						t.Fatal("unbounded/mutating passthrough", args)
					}
				}
			})
		}
	}
}

func TestProtectedNetworkLostAckAndTwoDigitJournalResume(t *testing.T) {
	for _, lost := range []int{1, 5, 9, 10, 17, 18} {
		t.Run(fmt.Sprint(lost), func(t *testing.T) {
			f := newProtectedExecutorFixture(t, "nat", true, true)
			f.lostAckAt = lost
			out, err := f.execute()
			networkCode(t, out, err, "RECOVERY_REQUIRED")
			if f.adds != lost {
				t.Fatal("continued after lost ack")
			}
			old := f.snapshot()
			f.r.Mode = "observe"
			out, err = f.execute()
			if lost == 18 {
				if err != nil {
					t.Fatal(err)
				}
				networkSuccess(t, out, f.r)
			} else {
				networkCode(t, out, err, "RECOVERY_REQUIRED")
			}
			if !reflect.DeepEqual(old, f.snapshot()) {
				t.Fatal("observation promoted receipt")
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
			if f.adds != 18 {
				t.Fatal("replayed present rules", f.adds)
			}
			for name, hash := range old {
				if f.snapshot()[name] != hash {
					t.Fatal("rewrote recovery evidence", name)
				}
			}
			raw, err := os.ReadFile(filepath.Join(f.e.JournalPath, networkRecordName(f.r, "rule10")))
			if err != nil {
				t.Fatal(err)
			}
			var record networkRecord
			if err = json.Unmarshal(raw, &record); err != nil || record.Version != 2 || record.Rule != 10 {
				t.Fatal("two-digit version2 record", string(raw), err)
			}
		})
	}
}

func TestProtectedNetworkKernelBypassAndAmbiguityRefused(t *testing.T) {
	for name, alter := range map[string]func(string, string) string{
		"early accept": func(chain, raw string) string {
			if chain == "INPUT" {
				return strings.Replace(raw, "-P INPUT ACCEPT\n", "-P INPUT ACCEPT\n-A INPUT -j ACCEPT\n", 1)
			}
			return raw
		},
		"early return": func(chain, raw string) string {
			if chain == "INPUT" {
				return strings.Replace(raw, "-P INPUT ACCEPT\n", "-P INPUT ACCEPT\n-A INPUT -j RETURN\n", 1)
			}
			return raw
		},
		"unknown jump": func(chain, raw string) string { return strings.Replace(raw, "-j INPUT_direct", "-j FOREIGN", 1) },
		"duplicate trampoline": func(chain, raw string) string {
			if chain == "INPUT" {
				return raw + "-A INPUT -j INPUT_direct\n"
			}
			return raw
		},
		"child accept": func(chain, raw string) string {
			if chain == "INPUT_direct" {
				return raw + "-A INPUT_direct -j ACCEPT\n"
			}
			return raw
		},
		"unknown policy": func(chain, raw string) string { return strings.Replace(raw, "-P INPUT ACCEPT", "-P INPUT DROP", 1) },
		"no newline":     func(chain, raw string) string { return strings.TrimSuffix(raw, "\n") },
		"blank line":     func(chain, raw string) string { return raw + "\n" },
		"control":        func(chain, raw string) string { return raw + "\x00\n" },
		"oversized":      func(chain, raw string) string { return strings.Repeat("x", networkOutputLimit+1) },
	} {
		t.Run(name, func(t *testing.T) {
			f := newProtectedExecutorFixture(t, "lab", true, true)
			f.kernelOverride = alter
			out, err := f.execute()
			if err == nil || out != (NetworkResponse{}) || f.adds != 0 || len(f.snapshot()) != 0 {
				t.Fatal("kernel bypass reached mutation", out, err)
			}
		})
	}
}

func TestProtectedNetworkKernelAndInventoryMustAgreeInOrder(t *testing.T) {
	for _, mode := range []string{"missing", "extra", "reordered", "wrong destination", "wrong port", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			f := newProtectedExecutorFixture(t, "lab", true, false)
			out, err := f.execute()
			if err != nil {
				t.Fatal(err)
			}
			networkSuccess(t, out, f.r)
			before := f.snapshot()
			f.r.Mode = "observe"
			f.kernelOverride = func(chain, raw string) string {
				lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
				switch mode {
				case "missing":
					lines = lines[:len(lines)-1]
				case "extra":
					lines = append(lines, "-A INPUT -j RETURN")
				case "reordered":
					lines[1], lines[len(lines)-1] = lines[len(lines)-1], lines[1]
				case "wrong destination":
					return strings.ReplaceAll(raw, "192.168.240.1/32", "192.168.240.2/32")
				case "wrong port":
					return strings.ReplaceAll(raw, "--sport 68", "--sport 69")
				case "duplicate":
					lines = append(lines, lines[1])
				}
				return strings.Join(lines, "\n") + "\n"
			}
			out, err = f.execute()
			if err == nil || out != (NetworkResponse{}) || f.adds != 18 || !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("drift produced successful proof/mutation", out, err)
			}
		})
	}
}

func TestProtectedNetworkMidApplyAndFinalKernelDriftStop(t *testing.T) {
	for _, stage := range []string{"between snapshots", "after first add", "last native", "observe final"} {
		t.Run(stage, func(t *testing.T) {
			f := newProtectedExecutorFixture(t, "lab", true, true)
			drift := false
			if stage == "last native" {
				f.native.onCall = func(n int) {
					if n == 20 {
						drift = true
					}
				}
			}
			if stage == "observe final" {
				out, err := f.execute()
				if err != nil {
					t.Fatal(err)
				}
				networkSuccess(t, out, f.r)
				f.r.Mode = "observe"
				start := f.native.calls
				f.native.onCall = func(n int) {
					if n == start+2 {
						drift = true
					}
				}
			}
			f.kernelOverride = func(chain, raw string) string {
				if stage == "between snapshots" && f.kernelCalls >= 3 {
					drift = true
				}
				if stage == "after first add" && f.adds == 1 {
					drift = true
				}
				if drift && chain == "INPUT" {
					return raw + "-A INPUT -j ACCEPT\n"
				}
				return raw
			}
			out, err := f.execute()
			if err == nil || out != (NetworkResponse{}) {
				t.Fatal("drift after reachability check returned success", out, err)
			}
			switch stage {
			case "between snapshots":
				if f.adds != 0 {
					t.Fatal("mutated before consistent initial observation")
				}
			case "after first add":
				if f.adds != 1 {
					t.Fatal("continued after kernel drift")
				}
			default:
				if f.adds != 18 {
					t.Fatal("fixture did not reach final boundary", f.adds)
				}
			}
		})
	}
}

func TestProtectedNetworkJournalFaultAndCancellationKeepPartialEvidence(t *testing.T) {
	for _, stage := range []string{"rule10.json", "complete.json", "kernel cancellation"} {
		t.Run(stage, func(t *testing.T) {
			f := newProtectedExecutorFixture(t, "nat", true, false)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.e.beforePublish = func(name string) error {
				if strings.HasSuffix(name, stage) {
					return errors.New("generated publication failure")
				}
				return nil
			}
			if stage == "kernel cancellation" {
				f.kernelOverride = func(chain, raw string) string {
					if f.adds == 5 {
						cancel()
					}
					return raw
				}
			}
			out, err := f.e.Execute(ctx, f.r, f.p)
			if err == nil || out != (NetworkResponse{}) {
				t.Fatal("failure returned proof")
			}
			want := 10
			if stage == "complete.json" {
				want = 18
			}
			if stage == "kernel cancellation" {
				want = 5
			}
			if f.adds != want {
				t.Fatal("crossed fault boundary", f.adds, want)
			}
			before := f.snapshot()
			f.r.Mode = "observe"
			f.e.beforePublish = nil
			f.kernelOverride = nil
			out, err = f.execute()
			if stage == "complete.json" {
				if err != nil {
					t.Fatal(err)
				}
				networkSuccess(t, out, f.r)
			} else {
				networkCode(t, out, err, "RECOVERY_REQUIRED")
			}
			if !reflect.DeepEqual(before, f.snapshot()) {
				t.Fatal("observation rewrote partial journal")
			}
		})
	}
}

func TestProtectedNetworkAuthorityOwnershipAndCanonicalHistory(t *testing.T) {
	t.Run("v1 grant cannot authorize v2", func(t *testing.T) {
		f := newProtectedExecutorFixture(t, "lab", true, false)
		f.p.Networks = f.p.ProtectedNetworks
		f.p.ProtectedNetworks = nil
		out, err := f.execute()
		networkCode(t, out, err, "PERMISSION_DENIED")
		if len(f.calls) > 0 || f.adds > 0 {
			t.Fatal("v1 grant dispatched protected rules")
		}
	})
	for _, kind := range []string{"unowned exact rule", "version change", "noncanonical index", "record version"} {
		t.Run(kind, func(t *testing.T) {
			f := newProtectedExecutorFixture(t, "lab", true, false)
			if kind == "unowned exact rule" {
				f.views[0][networkRuleLine(networkPolicyRules(f.r.Network.Definition)[4])] = true
				out, err := f.execute()
				networkCode(t, out, err, "RESOURCE_BUSY")
				if f.adds != 0 || len(f.snapshot()) != 0 {
					t.Fatal("adopted foreign rule")
				}
				return
			}
			out, err := f.execute()
			if err != nil {
				t.Fatal(err)
			}
			f.r.Mode = "observe"
			want := "RECOVERY_REQUIRED"
			switch kind {
			case "version change":
				f.r.Network.Version = 1
				f.r.Network.Definition.HostAccess = "allow"
				f.r.Operation = "network.ipv6-filter"
				f.p.Networks = f.p.ProtectedNetworks
				want = "SOURCE_CHANGED"
			case "noncanonical index":
				old := filepath.Join(f.e.JournalPath, networkRecordName(f.r, "rule10"))
				newName := filepath.Join(f.e.JournalPath, networkRecordName(f.r, "rule010"))
				if err = os.Rename(old, newName); err != nil {
					t.Fatal(err)
				}
			case "record version":
				name := filepath.Join(f.e.JournalPath, networkRecordName(f.r, "rule10"))
				raw, _ := os.ReadFile(name)
				raw = []byte(strings.Replace(string(raw), `"version":2`, `"version":1`, 1))
				if err = os.WriteFile(name, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := f.snapshot()
			out, err = f.execute()
			networkCode(t, out, err, want)
			if !reflect.DeepEqual(before, f.snapshot()) || f.adds != 18 {
				t.Fatal("altered incompatible journal")
			}
		})
	}
}

func TestNetworkVersionOneCoexistsWithDisjointProtectedDrops(t *testing.T) {
	f := newNetworkExecutorFixture(t)
	disjoint := "vmabcdefabcdef"
	for _, view := range f.views {
		view["eb filter INPUT -32768 --logical-in "+disjoint+" -p IPv4 -j DROP"] = true
		view["eb filter OUTPUT -32768 --logical-out "+disjoint+" -p ARP -j DROP"] = true
	}
	out, err := f.execute()
	if err != nil {
		t.Fatal(err)
	}
	networkSuccess(t, out, f.r)
	before := f.snapshot()
	f.r.Mode = "observe"
	out, err = f.execute()
	if err != nil {
		t.Fatal(err)
	}
	networkSuccess(t, out, f.r)
	if f.adds != 8 || !reflect.DeepEqual(before, f.snapshot()) {
		t.Fatal("v1 authority or journal changed")
	}
	for _, args := range f.calls {
		if strings.Contains(strings.Join(args, " "), "--passthrough ") {
			t.Fatal("v1 gained protected kernel probes")
		}
	}

	for name := range before {
		raw, _ := os.ReadFile(filepath.Join(f.e.JournalPath, name))
		var record struct {
			Version int `json:"version"`
		}
		if err = json.Unmarshal(raw, &record); err != nil || record.Version != 1 {
			t.Fatal("v1 record version changed", name)
		}
	}
	f2 := newNetworkExecutorFixture(t)
	f2.views[0]["eb filter INPUT -32768 --logical-in "+f2.r.Network.Definition.Bridge+" -p IPv4 -j DROP"] = true
	out, err = f2.execute()
	networkCode(t, out, err, "UNSUPPORTED_CAPABILITY")
	if f2.adds != 0 {
		t.Fatal("v1 silently accepted different same-bridge policy")
	}
}

func TestProtectedKernelPassthroughEnvelopeIsExact(t *testing.T) {
	for _, test := range []struct {
		name, raw string
		valid     bool
	}{
		{"observed firewalld 2.4.4", "-P INPUT ACCEPT\n\nsuccess\n", true},
		{"bare backend body", "-P INPUT ACCEPT\n", true},
		{"traced", "trace: command\n-P INPUT ACCEPT\n\nsuccess\n", false},
		{"changed trailer", "-P INPUT ACCEPT\n\nSuccess\n", false},
		{"repeated trailer", "-P INPUT ACCEPT\n\nsuccess\n\nsuccess\n", false},
		{"extra blank", "-P INPUT ACCEPT\n\n\nsuccess\n", false},
		{"missing body", "\nsuccess\n", false},
		{"missing separator", "-P INPUT ACCEPT\nsuccess\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newProtectedExecutorFixture(t, "lab", false, false)
			f.r.Mode = "check"
			f.override = func(args []string) (networkCommandResult, error, bool) {
				if len(args) == 7 && args[1] == "--passthrough" {
					return networkCommandResult{stdout: test.raw}, nil, true
				}
				return networkCommandResult{}, nil, false
			}
			out, err := f.execute()
			if test.valid {
				if err != nil || out.Version != 2 || out.RuntimePresent || out.PermanentPresent {
					t.Fatal("supported empty kernel observation", out, err)
				}
			} else if err == nil || out != (NetworkResponse{}) {
				t.Fatal("accepted ambiguous passthrough framing", out, err)
			}
			if f.adds != 0 || len(f.snapshot()) != 0 {
				t.Fatal("read-only preflight changed state")
			}
		})
	}
}

func TestProtectedManagedInputInventoryRejectsForeignSemantics(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		for _, line := range []string{
			"ipv4 mangle INPUT -32769 -j ACCEPT",
			"ipv4 mangle INPUT -32763 -i vmabcdefabcdef -j ACCEPT",
			"ipv4 mangle INPUT -32765 -d 192.168.240.1/32 -i vmabcdefabcdef -p udp -m udp --dport 22 -j ACCEPT",
			"ipv4 mangle INPUT -32767 -d 255.255.255.255/32 -i vmabcdefabcdef -p udp -m udp --sport 69 --dport 67 -j ACCEPT",
			"eb filter INPUT -32768 --logical-in vmabcdefabcdef -p IPv4 -j ACCEPT",
			"eb filter INPUT -32768  --logical-in vmabcdefabcdef -p ARP -j DROP",
		} {
			t.Run(fmt.Sprintf("permanent=%t/%s", permanent, line), func(t *testing.T) {
				f := newProtectedExecutorFixture(t, "lab", true, false)
				view := 0
				if permanent {
					view = 1
				}
				f.views[view][line] = true
				out, err := f.execute()
				if err == nil || out != (NetworkResponse{}) || f.adds != 0 || len(f.snapshot()) != 0 {
					t.Fatal("unknown input semantics reached apply", out, err)
				}
			})
		}
	}
}

func TestProtectedNetworkRejectsDormantReloadBypass(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		for _, kind := range []string{"input passthrough", "unproven IPv4 passthrough", "INPUT custom chain", "INPUT_direct custom chain"} {
			t.Run(fmt.Sprintf("permanent=%t/%s", permanent, kind), func(t *testing.T) {
				f := newProtectedExecutorFixture(t, "lab", true, false)
				view := 0
				if permanent {
					view = 1
				}
				switch kind {
				case "input passthrough":
					f.passthroughs[view] = "ipv4 -t mangle -I INPUT 1 -j ACCEPT\n"
				case "unproven IPv4 passthrough":
					f.passthroughs[view] = "ipv4 -t filter -A OUTPUT -j ACCEPT\n"
				case "INPUT custom chain":
					f.chains[view] = "ipv4 mangle INPUT\n"
				case "INPUT_direct custom chain":
					f.chains[view] = "ipv4 mangle INPUT_direct\n"
				}
				out, err := f.execute()
				networkCode(t, out, err, "UNSUPPORTED_CAPABILITY")
				if f.adds != 0 || len(f.snapshot()) != 0 {
					t.Fatal("dormant reload bypass reached mutation")
				}
			})
		}
	}
	t.Run("unrelated IPv6 declaration retained", func(t *testing.T) {
		f := newProtectedExecutorFixture(t, "lab", false, false)
		f.passthroughs[1] = "ipv6 -t filter -A OUTPUT -j DROP\n"
		f.chains[1] = "ipv4 mangle UNUSED\n"
		beforePass, beforeChains := f.passthroughs, f.chains
		out, err := f.execute()
		if err != nil {
			t.Fatal(err)
		}
		networkSuccess(t, out, f.r)
		if f.passthroughs != beforePass || f.chains != beforeChains {
			t.Fatal("altered unrelated declarative configuration")
		}
	})
}

func TestNetworkVersionOneFrozenJournalBytesRemainReadable(t *testing.T) {
	// Fixed pre-v2 JSON shape and independently computed canonical owner digest.
	// No current record/owner constructor or digest helper supplies these bytes.
	const owner = `{"version":1,"actorUID":1000,"keyID":"fixture-key","resourceID":"12345678-1234-4234-8234-123456789abc","definition":{"uuid":"12345678-1234-4234-8234-123456789abc","name":"virmill-12345678-1234-4234-8234-123456789abc","bridge":"vm123456781234","type":"lab","ipv4CIDR":"192.168.240.0/24","dhcpEnabled":true,"advertiseDefaultRoute":false,"ipv6Mode":"disabled","hostAccess":"allow","egress":"none"}}`
	const binding = "3e6b2bd58171a1b5d4bba04593d3a2ce834eef703a77b9e84299fa87a9e210bf"
	const job = "87654321-4321-4321-8321-cba987654321"
	f := newNetworkExecutorFixture(t)
	f.r.JobID = job
	prefix := "network-12345678-1234-4234-8234-123456789abc."
	want := map[string]string{prefix + "owner.json": owner}
	record := func(kind string, index int, args []string) string {
		encoded, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf(`{"version":1,"resourceBinding":"%s","jobID":"%s","planDigest":"%s","kind":"%s","rule":%d,"arguments":%s}`, binding, job, strings.Repeat("a", 64), kind, index, encoded)
	}
	want[prefix+"job-"+job+".intent.json"] = record("job", -1, []string{})
	want[prefix+"job-"+job+".complete.json"] = record("complete", -1, []string{})
	legacyTails := []string{
		"INPUT -32768 --logical-in vm123456781234 -p IPv6 -j DROP",
		"OUTPUT -32768 --logical-out vm123456781234 -p IPv6 -j DROP",
		"FORWARD -32768 --logical-in vm123456781234 -p IPv6 -j DROP",
		"FORWARD -32768 --logical-out vm123456781234 -p IPv6 -j DROP",
	}
	for i := 0; i < 8; i++ {
		args := strings.Fields("--direct --add-rule eb filter " + legacyTails[i%4])
		if i >= 4 {
			args = append([]string{"--permanent"}, args...)
		}
		want[fmt.Sprintf("%sjob-%s.rule%d.json", prefix, job, i)] = record("rule", i, args)
	}
	// Seed a completed old journal to prove migration-free read compatibility.
	for name, raw := range want {
		if err := os.WriteFile(filepath.Join(f.e.JournalPath, name), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 8; i++ {
		f.views[i/4]["eb filter "+legacyTails[i%4]] = true
	}
	before := f.snapshot()
	f.r.Mode = "observe"
	out, err := f.execute()
	if err != nil {
		t.Fatal("pre-v2 journal was not readable", err)
	}
	networkSuccess(t, out, f.r)
	if f.adds != 0 || !reflect.DeepEqual(before, f.snapshot()) {
		t.Fatal("v1 observation rewrote or replayed historical state")
	}
	// A fresh v1 apply must still emit exactly the same persisted bytes.
	g := newNetworkExecutorFixture(t)
	g.r.JobID = job
	if _, err := g.execute(); err != nil {
		t.Fatal(err)
	}
	if len(g.snapshot()) != len(want) {
		t.Fatal("v1 journal record count changed")
	}
	for name, expected := range want {
		actual, err := os.ReadFile(filepath.Join(g.e.JournalPath, name))
		if err != nil || string(actual) != expected {
			t.Fatalf("v1 persisted bytes changed for %s: %s; %v", name, actual, err)
		}
	}
}
