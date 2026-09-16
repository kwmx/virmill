package tui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/domain"
)

func detailTextForTest(value any, width int) string {
	return strings.Join(HumanDetails(value, width), "\n")
}

func TestHumanDetailsStableReadableCollections(t *testing.T) {
	value := map[string]any{
		"state": "recovery-required", "nvramInitializationVerified": false,
		"resourceUUID":     "12345678-1234-4234-8234-123456789abc",
		"devices":          []any{map[string]any{"target": "vda", "readOnly": false}, map[string]any{"target": "sdb", "readOnly": true}},
		"acknowledgements": []string{"preserve-source", "exclusive-network-writer"},
		"ipv4CIDR":         "192.168.10.0/24", "unknownGroup": (*uint)(nil), "zeroBytes": uint64(0),
	}
	got := detailTextForTest(value, 100)
	for _, want := range []string{"Acknowledgements:\n  - preserve-source\n  - exclusive-network-writer", "NVRAM initialization verified: false", "Resource UUID: 12345678-1234-4234-8234-123456789abc", "IPv4 CIDR: 192.168.10.0/24", "Unknown group: Unknown (null)", "Zero bytes: 0", "Read only: false"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.ContainsAny(got, "{}[]") || strings.Index(got, "vda") > strings.Index(got, "sdb") {
		t.Fatal(got)
	}
	if strings.Index(got, "Acknowledgements:") > strings.Index(got, "Devices:") || strings.Index(got, "Devices:") > strings.Index(got, "IPv4 CIDR:") {
		t.Fatal("field keys are not sorted", got)
	}
	for i := 0; i < 30; i++ {
		if detailTextForTest(value, 100) != got {
			t.Fatal("map iteration changed output")
		}
	}
}

func TestHumanDetailsWrapsCompleteValuesAndSanitizes(t *testing.T) {
	value := "12345678-1234-4234-8234-123456789abc" + strings.Repeat("abcdef0123456789", 4) + "東京α"
	for _, width := range []int{2, 7, 24, 80} {
		lines := HumanDetails(value, width)
		if strings.Join(lines, "") != value {
			t.Fatalf("width %d dropped characters: %q", width, lines)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width %d: %q", width, line)
			}
		}
	}
	for _, width := range []int{0, -1, 1} {
		if strings.Join(HumanDetails("東京", width), "") != "東京" {
			t.Fatal("tiny width lost a grapheme")
		}
	}
	text := detailTextForTest(map[string]any{"guest\x1b[31mName\u202e": "أهلا 東京\tα\x00\x07\x1b[31mred\x1b[0m\u202e\u2066\x1b]0;hostile title\x07"}, 80)
	for _, r := range text {
		if (unicode.IsControl(r) && r != '\n') || unicode.Is(unicode.Cf, r) {
			t.Fatalf("unsafe rune %U", r)
		}
	}
	for _, want := range []string{"أهلا", "東京", "αred"} {
		if !strings.Contains(text, want) {
			t.Fatal(text)
		}
	}
	if strings.Contains(text, "hostile title") {
		t.Fatal(text)
	}
}

func TestHumanDetailsNativeXMLNoticeDoesNotHideOtherFacts(t *testing.T) {
	vm := domain.VM{Name: "Original α VM", State: "stopped", PersistentXML: "<domain>" + strings.Repeat("large native configuration", 1000) + "</domain>", LiveXML: "<domain/>", Fingerprint: strings.Repeat("c", 64)}
	got := detailTextForTest(vm, 120)
	if strings.Contains(got, "<domain") || strings.Count(got, nativeXMLNotice) != 2 || !strings.Contains(got, "Original α VM") || !strings.Contains(got, vm.Fingerprint) {
		t.Fatal(got)
	}
	other := detailTextForTest(map[string]any{"sourceXMLSHA256": strings.Repeat("d", 64), "notes": "XML unknown to this provider", "xmlPolicy": "preserve-unknown"}, 120)
	if strings.Contains(other, nativeXMLNotice) || !strings.Contains(other, strings.Repeat("d", 64)) || !strings.Contains(other, "preserve-unknown") {
		t.Fatal(other)
	}
	if got = detailTextForTest(map[string]any{"liveXML": ""}, 80); got != "Live XML: Empty" {
		t.Fatal(got)
	}
}

func TestPlanDetailsPreservesEveryApprovalAndPolicyFact(t *testing.T) {
	resource := "libvirt|qemu:///system|vm|12345678-1234-4234-8234-123456789abc"
	p := domain.Plan{APIVersion: "virmill/v1", ID: "22345678-1234-4234-8234-123456789abc", Digest: strings.Repeat("a1", 32), InputDigest: strings.Repeat("b2", 32), Operation: "vm.restore", ConnectionID: "qemu:///system", ActorUID: 1000, CreatedAt: time.Date(2026, 9, 8, 1, 2, 3, 0, time.UTC), ExpiresAt: time.Date(2026, 9, 8, 2, 2, 3, 0, time.UTC), ResourceIDs: []string{resource}, Acknowledgements: []string{"preserve-original", "disconnect-network"}, Risks: []string{"Source capture is incomplete."}, Before: map[string]string{resource: strings.Repeat("c3", 32)}, RequiredGrants: []domain.Grant{{Operation: "vm.restore", ResourceID: resource}}, Estimates: domain.Estimates{AdditionalBytes: 18446744073709551615, RequiresDowntime: true, Notes: "Space is estimated."}, Steps: []domain.Step{{ID: "first", Action: "stage independent disk", Preconditions: []string{"stopped source"}, Idempotency: "observe existing intent", Compensation: "retain staged disk", Reconciliation: "inspect original receipt", CompletionPredicate: "exact output digest"}, {ID: "second", Action: "define inactive guest"}}, Review: map[string]any{"nvramInitializationVerified": false, "disconnectNICs": true, "hostAccess": "deny", "defaultRouteAdvertised": false, "unresolved": nil}}
	got := strings.Join(PlanDetails(p, 240), "\n")
	for _, want := range []string{p.ID, p.Digest, p.InputDigest, resource, p.Before[resource], "vm.restore", "Actor UID: 1000", "Created at: 2026-09-08T01:02:03Z", "Expires at: 2026-09-08T02:02:03Z", "API version: virmill/v1", "preserve-original", "disconnect-network", "Source capture is incomplete.", "18446744073709551615", "Requires downtime: true", "Space is estimated.", "stopped source", "observe existing intent", "retain staged disk", "inspect original receipt", "exact output digest", "NVRAM initialization verified: false", "Host access: deny", "Default route advertised: false", "Unresolved: Unknown (null)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Index(got, "stage independent disk") > strings.Index(got, "define inactive guest") || strings.Contains(got, "omitted") || strings.ContainsAny(got, "{}") {
		t.Fatal(got)
	}
	var compact strings.Builder
	for _, line := range PlanDetails(p, 24) {
		if ansi.StringWidth(line) > 24 {
			t.Fatalf("line exceeded width: %q", line)
		}
		compact.WriteString(strings.TrimLeft(line, " "))
	}
	for _, value := range []string{p.ID, p.Digest, p.InputDigest, resource, p.Before[resource]} {
		if !strings.Contains(compact.String(), value) {
			t.Fatalf("narrow plan lost critical identity %q", value)
		}
	}
}

func TestHumanDetailsExactIdentifierMapKeysAndNumbers(t *testing.T) {
	key := strings.Repeat("a12b", 16)
	got := detailTextForTest(map[string]any{key: "original fingerprint", "12345678-1234-4234-8234-123456789abc": "stopped"}, 100)
	if !strings.Contains(got, key+": original fingerprint") {
		t.Fatal(got)
	}
	jsonValue := json.RawMessage(`{"bytes":18446744073709551615,"permission":false}`)
	if got = detailTextForTest(jsonValue, 100); !strings.Contains(got, "18446744073709551615") || !strings.Contains(got, "Permission: false") {
		t.Fatal(got)
	}
	if got = detailTextForTest(json.RawMessage(`{"permission":false,"permission":true}`), 100); !strings.Contains(got, "Unrenderable JSON") || strings.Contains(got, "true") {
		t.Fatal(got)
	}
}

func TestHumanDetailsPathologicalDataHasExplicitBoundedNotice(t *testing.T) {
	cycle := map[string]any{}
	cycle["self"] = cycle
	for _, input := range []any{cycle, strings.Repeat("x", detailBytes+1), make([]string, detailNodes+1)} {
		lines := HumanDetails(input, 80)
		if !strings.Contains(strings.Join(lines, "\n"), "presentation safety limit reached") || len(lines) > detailRows+3 {
			t.Fatalf("missing bounded notice: %d lines", len(lines))
		}
	}
	if got := HumanDetails(nil, 80); !reflect.DeepEqual(got, []string{"Unknown (null)"}) {
		t.Fatal(got)
	}
	if got := detailTextForTest([]string{}, 80); got != "None (empty collection)" {
		t.Fatal(got)
	}
}

func TestPlanDetailsWarningsKeepWordsAcross80ColumnPages(t *testing.T) {
	// These are the actual guest recipe planner's warnings, including the
	// native/not words split mid-word in the previous 80-column review.
	warnings := []string{
		"The reviewer binds the supplied address and known-hosts key to this VM; native IP identity is unavailable.",
		"Reviewed recipe scripts and arguments persist in private journal input. Do not embed credentials.",
		"A remote script may change guest state before an interrupted SSH response; reconciliation never reruns it.",
		"This built-in uses passwordless sudo inside the guest to install packages from its configured repositories and start qemu-guest-agent. Dependencies may change; there is no automatic rollback or reboot.",
		"The guest needs the org.qemu.guest_agent.0 virtio channel. Desktop package installation does not prove clipboard or display integration works.",
	}
	resource := "libvirt|qemu:///system|vm|12345678-1234-4234-8234-123456789abc"
	p := domain.Plan{APIVersion: domain.APIVersion, ID: "22345678-1234-4234-8234-123456789abc", Digest: strings.Repeat("a1", 32), InputDigest: strings.Repeat("b2", 32), Operation: "guest.recipe.run", ConnectionID: "qemu:///system", ResourceIDs: []string{resource}, Risks: warnings, Before: map[string]string{resource: strings.Repeat("c3", 32)}}
	lines := PlanDetails(p, 80)
	readable := strings.Join(strings.Fields(strings.Join(lines, "\n")), " ")
	for _, warning := range warnings {
		if !strings.Contains(readable, warning) {
			t.Fatalf("review split words or omitted a warning: %q\n%s", warning, strings.Join(lines, "\n"))
		}
	}
	seen := map[string]bool{}
	m := NewWorkspace(nil, p.ConnectionID)
	m.NoColor, m.Plan, m.PlanDetails = true, &p, true
	m.Width, m.Height = 80, 24
	// Read every rendered viewport, including the final clamped page. This
	// checks actual review routing as well as the standalone detail formatter.
	for offset := 0; offset < len(lines); offset += 17 {
		m.Offset = offset
		view := strings.Split(m.View(), "\n")
		if len(view) > 24 {
			t.Fatalf("review exceeds 24 rows: %d", len(view))
		}
		for _, row := range view {
			if ansi.StringWidth(row) > 80 {
				t.Fatalf("review exceeds 80 columns: %q", row)
			}
			seen[strings.TrimRight(row, " ")] = true
		}
	}
	var exact strings.Builder
	for _, line := range lines {
		if !seen[line] {
			t.Fatalf("review line is inaccessible across pages: %q", line)
		}
		exact.WriteString(strings.TrimLeft(line, " "))
	}
	for _, value := range []string{p.ID, p.Digest, p.InputDigest, resource, p.Before[resource]} {
		if !strings.Contains(exact.String(), value) {
			t.Fatalf("review lost exact identity %q", value)
		}
	}
}

func TestHumanDetailsWrappingPreservesIndentAndStructuredRows(t *testing.T) {
	for _, text := range []string{"Name            State       CPU", "<disk type=\"file\" device=\"disk\">", "\"message\": \"retain original spacing\"", "first\tsecond\tthird"} {
		f := newDetails(24)
		f.line(text, 2)
		expanded := strings.ReplaceAll(text, "\t", "    ")
		want := strings.Split(ansi.Hardwrap(expanded, 20, true), "\n")
		for i := range want {
			want[i] = "    " + want[i]
		}
		if !reflect.DeepEqual(f.lines, want) {
			t.Fatalf("structured row or indentation changed: %#v; want %#v", f.lines, want)
		}
	}
}
