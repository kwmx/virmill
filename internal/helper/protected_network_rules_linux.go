//go:build linux && amd64

package helper

import (
	"context"
	"encoding/hex"
	"net/netip"
	"reflect"
	"strconv"
	"strings"

	"virmill.local/core/internal/domain"
)

// Version one remains exactly the original eight ordered argument vectors.
// Version two uses the first half for runtime and the identical second half for
// permanent configuration. No request may supply rule syntax or priorities.
func networkPolicyRules(d domain.NetworkDefinition) [][]string {
	legacy := networkRules(d.Bridge)
	if d.PolicyVersion() == 1 {
		return legacy[:]
	}
	runtime := append([][]string{}, legacy[:4]...)
	if d.Type == "guest-only" {
		for _, pair := range [][2]string{{"INPUT", "--logical-in"}, {"OUTPUT", "--logical-out"}} {
			for _, protocol := range []string{"IPv4", "ARP"} {
				runtime = append(runtime, []string{"--direct", "--add-rule", "eb", "filter", pair[0], "-32768", pair[1], d.Bridge, "-p", protocol, "-j", "DROP"})
			}
		}
	} else {
		// INPUT is after routing and before filter INPUT's broad ACCEPT rules. Using
		// bridge INPUT here would incorrectly deny NAT's routed outbound traffic.
		if d.DHCPEnabled {
			bridgeIP := netip.MustParsePrefix(d.IPv4CIDR).Addr().Next().String() + "/32"
			for i, destination := range []string{"255.255.255.255/32", bridgeIP} {
				runtime = append(runtime, []string{"--direct", "--add-rule", "ipv4", "mangle", "INPUT", strconv.Itoa(-32767 + i), "-d", destination, "-i", d.Bridge, "-p", "udp", "-m", "udp", "--sport", "68", "--dport", "67", "-j", "ACCEPT"})
			}
			for i, protocol := range []string{"udp", "tcp"} {
				runtime = append(runtime, []string{"--direct", "--add-rule", "ipv4", "mangle", "INPUT", strconv.Itoa(-32765 + i), "-d", bridgeIP, "-i", d.Bridge, "-p", protocol, "-m", protocol, "--dport", "53", "-j", "ACCEPT"})
			}
		}
		runtime = append(runtime, []string{"--direct", "--add-rule", "ipv4", "mangle", "INPUT", "-32763", "-i", d.Bridge, "-j", "DROP"})
	}
	rules := append([][]string{}, runtime...)
	for _, args := range runtime {
		rules = append(rules, append([]string{"--permanent"}, args...))
	}
	return rules
}

func protectedBridgeName(value string) bool {
	if len(value) != 14 || !strings.HasPrefix(value, "vm") {
		return false
	}
	raw, err := hex.DecodeString(value[2:])
	return err == nil && hex.EncodeToString(raw) == value[2:]
}
func protectedBridgeDrop(f []string) bool {
	return len(f) == 10 && f[0] == "eb" && f[1] == "filter" && f[3] == "-32768" && protectedBridgeName(f[5]) && f[6] == "-p" && (f[7] == "IPv4" || f[7] == "ARP") && f[8] == "-j" && f[9] == "DROP" && ((f[2] == "INPUT" && f[4] == "--logical-in") || (f[2] == "OUTPUT" && f[4] == "--logical-out"))
}

// Existing, unrelated IPv4 rules are still observations. Only exact new policy
// shapes may coexist on our selected bridge; changed profiles require new authority.
func selectedNetworkPolicy(r Request, rules [][]string, inventory networkRuleInventory) error {
	expected := map[string]bool{}
	for _, args := range rules {
		expected[networkRuleLine(args)] = true
	}
	for _, view := range inventory {
		for line := range view {
			fields := strings.Fields(line)
			relevant := protectedBridgeDrop(fields) && fields[5] == r.Network.Definition.Bridge
			if len(fields) >= 4 && fields[0] == "ipv4" && fields[1] == "mangle" && fields[2] == "INPUT" {
				for _, field := range fields[4:] {
					if field == r.Network.Definition.Bridge {
						relevant = true
					}
				}
			}
			if relevant && !expected[line] {
				return domain.Fail("UNSUPPORTED_CAPABILITY", "selected bridge has a different or ambiguous protected policy")
			}
		}
	}
	return nil
}

type protectedKernelRule struct {
	priority int
	body     string
}

func protectedMangleRule(fields []string) (protectedKernelRule, error) {
	bad := func() (protectedKernelRule, error) {
		return protectedKernelRule{}, domain.Fail("UNSUPPORTED_CAPABILITY", "mangle INPUT contains unknown or bypassing direct rules")
	}
	if len(fields) < 4 || fields[0] != "ipv4" || fields[1] != "mangle" || fields[2] != "INPUT" {
		return bad()
	}
	priority, err := strconv.Atoi(fields[3])
	if err != nil || strconv.Itoa(priority) != fields[3] {
		return bad()
	}
	a := fields[4:]
	if len(a) == 4 {
		if priority != -32763 || a[0] != "-i" || !protectedBridgeName(a[1]) || a[2] != "-j" || a[3] != "DROP" {
			return bad()
		}
	} else {
		if len(a) != 12 && len(a) != 14 {
			return bad()
		}
		if a[0] != "-d" || a[2] != "-i" || !protectedBridgeName(a[3]) || a[4] != "-p" || a[6] != "-m" || a[7] != a[5] || a[len(a)-2] != "-j" || a[len(a)-1] != "ACCEPT" {
			return bad()
		}
		destination, err := netip.ParsePrefix(a[1])
		if err != nil || !destination.Addr().Is4() || destination.Bits() != 32 || destination.String() != a[1] {
			return bad()
		}
		if len(a) == 14 {
			if a[5] != "udp" || a[8] != "--sport" || a[9] != "68" || a[10] != "--dport" || a[11] != "67" {
				return bad()
			}
			if !(priority == -32767 && a[1] == "255.255.255.255/32" || priority == -32766 && destination.Addr().IsPrivate()) {
				return bad()
			}
		} else {
			if !destination.Addr().IsPrivate() || a[8] != "--dport" || a[9] != "53" || !(priority == -32765 && a[5] == "udp" || priority == -32764 && a[5] == "tcp") {
				return bad()
			}
		}
	}
	return protectedKernelRule{priority, strings.Join(a, " ")}, nil
}

func protectedKernelExpected(inventory networkRuleInventory) (map[string]protectedKernelRule, error) {
	result := map[string]protectedKernelRule{}
	// Permanent input policy must be just as bounded before it can become runtime
	// after reload. Only runtime bodies participate in the current kernel equality.
	for index, view := range inventory {
		for line := range view {
			f := strings.Fields(line)
			// A permanent passthrough is dormant until reload; current kernel
			// equality cannot qualify it. Do not interpret or execute its text.
			if len(f) >= 2 && f[0] == "@passthroughs" && f[1] == "ipv4" {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "protected policy cannot coexist with IPv4 tracked passthroughs")
			}
			if len(f) == 4 && f[0] == "@chains" && f[1] == "ipv4" && f[2] == "mangle" && (f[3] == "INPUT" || f[3] == "INPUT_direct") {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "custom mangle input chain conflicts with protected reachability")
			}
			if len(f) < 3 || f[0] != "ipv4" || f[1] != "mangle" || f[2] != "INPUT" {
				continue
			}
			if line != strings.Join(f, " ") {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "noncanonical protected rule inventory")
			}
			rule, err := protectedMangleRule(f)
			if err != nil {
				return nil, err
			}
			if index == 0 {
				if _, exists := result[rule.body]; exists {
					return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "duplicate protected kernel rule body")
				}
				result[rule.body] = rule
			}
		}
	}
	return result, nil
}

func protectedKernelLines(raw string) ([]string, error) {
	if raw == "" || len(raw) > networkOutputLimit || !strings.HasSuffix(raw, "\n") {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "bounded complete kernel rule output required")
	}
	lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
	if len(lines) > 258 {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "kernel input inventory exceeds rule bound")
	}
	for _, line := range lines {
		if len(line) > 2048 || line == "" || line != strings.Join(strings.Fields(line), " ") {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "ambiguous kernel rule line")
		}
		for _, c := range line {
			if c < 32 || c > 126 {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "kernel rule line contains noncanonical bytes")
			}
		}
	}
	return lines, nil
}
func matchProtectedKernelBody(lines []string, chain string, expected map[string]protectedKernelRule) error {
	seen := map[string]bool{}
	last := -32768
	for _, line := range lines {
		prefix := "-A " + chain + " "
		if !strings.HasPrefix(line, prefix) {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "unknown kernel input statement")
		}
		body := strings.TrimPrefix(line, prefix)
		rule, ok := expected[body]
		if !ok || seen[body] || rule.priority < last {
			return domain.Fail("SOURCE_CHANGED", "kernel input policy differs from ordered managed inventory")
		}
		seen[body] = true
		last = rule.priority
	}
	if len(seen) != len(expected) {
		return domain.Fail("SOURCE_CHANGED", "managed input policy is absent from the kernel chain")
	}
	return nil
}

func (e NetworkExecutor) protectedKernelChain(ctx context.Context, chain string) (string, []string, error) {
	// Both chain names below are compile-time choices, never request values. This
	// is the sole passthrough operation: it only lists rules through the exact
	// IPv4 backend that firewalld itself uses for direct rules.
	if chain != "INPUT" && chain != "INPUT_direct" {
		return "", nil, domain.Fail("INVALID_INPUT", "fixed kernel input chain required")
	}
	out, err := e.command(ctx, "--direct", "--passthrough", "ipv4", "-t", "mangle", "-S", chain)
	if err != nil {
		return "", nil, err
	}
	if out.exit != 0 {
		return "", nil, domain.Fail("UNSUPPORTED_CAPABILITY", "kernel mangle INPUT inspection unavailable")
	}
	// firewall-cmd 2.4.4 prints the raw backend output, a separating newline,
	// then its exact success line. Strip only that observed transport suffix;
	// traces, warnings, repeated success markers, and other text remain errors.
	body := strings.TrimSuffix(out.stdout, "\nsuccess\n")
	lines, err := protectedKernelLines(body)
	return out.stdout, lines, err
}
func (e NetworkExecutor) protectedKernel(ctx context.Context, r Request, inventory networkRuleInventory) error {
	if r.Network.Version == 1 {
		return nil
	}
	expected, err := protectedKernelExpected(inventory)
	if err != nil {
		return err
	}
	var previous [2]string
	for pass := 0; pass < 2; pass++ {
		var snapshot [2]string
		raw, lines, err := e.protectedKernelChain(ctx, "INPUT")
		if err != nil {
			return err
		}
		snapshot[0] = raw
		if lines[0] != "-P INPUT ACCEPT" {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "kernel INPUT policy must be the supported ACCEPT baseline")
		}
		if len(lines) == 2 && lines[1] == "-A INPUT -j INPUT_direct" {
			raw, lines, err = e.protectedKernelChain(ctx, "INPUT_direct")
			if err != nil {
				return err
			}
			snapshot[1] = raw
			if lines[0] != "-N INPUT_direct" {
				return domain.Fail("UNSUPPORTED_CAPABILITY", "kernel input trampoline has an unknown declaration")
			}
			if err = matchProtectedKernelBody(lines[1:], "INPUT_direct", expected); err != nil {
				return err
			}
		} else if err = matchProtectedKernelBody(lines[1:], "INPUT", expected); err != nil {
			return err
		}
		if pass == 1 && snapshot != previous {
			return domain.Fail("SOURCE_CHANGED", "kernel input reachability changed during observation")
		}
		previous = snapshot
	}
	return ctx.Err()
}
func (e NetworkExecutor) finalProtectedPolicy(ctx context.Context, r Request, baseline networkRuleInventory) error {
	if r.Network.Version == 1 {
		return nil
	}
	current, err := e.inventory(ctx)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, baseline) {
		return domain.Fail("SOURCE_CHANGED", "protected policy inventory changed before return")
	}
	return e.protectedKernel(ctx, r, current)
}
