package tui

import (
	"context"
	"encoding/json"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/wire"
)

func TestNetworkCreationAndRecoveryUseSharedPreview(t *testing.T) {
	for _, tt := range []struct{ command, method, action, value string }{{"network create", "network.create", "create", "network.yaml"}, {"network creation resume", "network.creation.resume", "resume", "job-id"}, {"network creation result", "network.creation.result", "", "job-id"}} {
		r := &recorder{}
		m := New(r, "qemu:///system")
		found := false
		for section := range sections {
			m.Section = section
			for i, a := range m.actions() {
				if a.Command == tt.command {
					m.Selected = i
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Fatal("missing", tt.command)
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Model)
		if cmd != nil || !m.Editing {
			t.Fatal("expected input")
		}
		m.Input = tt.value
		_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("no request")
		}
		cmd()
		if r.method != tt.method || r.request.Action != tt.action || r.request.Apply != nil {
			t.Fatal(r)
		}
	}
}

// Only native observation and helper authorization are substituted. Declaration
// loading, policy review, plan persistence and apply validation use the service.
type protectedNetworkBackend struct {
	domain.ComputeProvider
	domain.ResourceInventory
	checks, mutations int
}

func (b *protectedNetworkBackend) ListNetworks(ctx context.Context, _ string) ([]domain.VirtualNetwork, error) {
	return []domain.VirtualNetwork{}, ctx.Err()
}
func (b *protectedNetworkBackend) CheckNetworkCreation(ctx context.Context, _ string, _ domain.NetworkDefinition) error {
	b.checks++
	return ctx.Err()
}
func (b *protectedNetworkBackend) DefineNetwork(context.Context, string, domain.NetworkDefinition) error {
	b.mutations++
	return errors.New("unexpected generated definition")
}
func (b *protectedNetworkBackend) ActivateNetwork(context.Context, string, domain.NetworkDefinition) error {
	b.mutations++
	return errors.New("unexpected generated activation")
}
func (b *protectedNetworkBackend) InspectCreatedNetwork(context.Context, string, domain.NetworkDefinition) (domain.VirtualNetwork, error) {
	return domain.VirtualNetwork{}, errors.New("no fixture network was created")
}

type protectedNetworkFirewall struct{ checks, mutations int }

func (f *protectedNetworkFirewall) Check(ctx context.Context, _ domain.Plan, d domain.NetworkDefinition) error {
	f.checks++
	if err := ctx.Err(); err != nil {
		return err
	}
	family := "networks"
	if d.PolicyVersion() == 2 {
		family = "protectedNetworks"
	}
	return domain.Fail("PERMISSION_DENIED", family+" exact administrator grant missing")
}
func (f *protectedNetworkFirewall) Apply(context.Context, domain.Plan, domain.NetworkDefinition, string) error {
	f.mutations++
	return errors.New("unexpected generated firewall mutation")
}
func (*protectedNetworkFirewall) Observe(context.Context, domain.Plan, domain.NetworkDefinition, string) error {
	return errors.New("no fixture rules exist")
}

type protectedNetworkClient struct {
	service  *app.Service
	backend  *protectedNetworkBackend
	firewall *protectedNetworkFirewall
	methods  []string
	requests []app.Request
	canceled bool
}

func (c *protectedNetworkClient) Call(ctx context.Context, method string, r app.Request) (app.Response, error) {
	c.methods = append(c.methods, method)
	c.requests = append(c.requests, r)
	if c.canceled {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		cancel()
	}
	return c.service.Call(ctx, 1000, method, r), nil
}
func newProtectedNetworkClient(t *testing.T) *protectedNetworkClient {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "state", "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	b := &protectedNetworkBackend{}
	f := &protectedNetworkFirewall{}
	s := app.New(b, operations.New(db))
	s.NetworkFirewall = f
	s.HostPrefixes = func(ctx context.Context) (domain.HostNetworkPrefixes, error) {
		return domain.HostNetworkPrefixes{Prefixes: []domain.HostNetworkPrefix{}}, ctx.Err()
	}
	t.Cleanup(func() { s.Engine.Close(); db.Close() })
	return &protectedNetworkClient{service: s, backend: b, firewall: f}
}
func protectedNetworkExample(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs("../../../examples/networks/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func protectedNetworkPlan(t *testing.T, r app.Response) domain.Plan {
	t.Helper()
	b, err := json.Marshal(r.Data)
	if err != nil {
		t.Fatal(err)
	}
	var p domain.Plan
	if err = wire.Decode(b, &p); err != nil || r.Error != nil || p.ID == "" || p.Digest == "" {
		t.Fatal("missing shared protected plan", r, err)
	}
	return p
}
func assertProtectedNetworkReview(t *testing.T, p domain.Plan, kind, host, cidr string, dhcp bool, version int) {
	t.Helper()
	b, err := json.Marshal(p.Review["definition"])
	if err != nil {
		t.Fatal(err)
	}
	var d domain.NetworkDefinition
	if err = wire.Decode(b, &d); err != nil {
		t.Fatal(err)
	}
	if d.Type != kind || d.HostAccess != host || d.IPv4CIDR != cidr || d.DHCPEnabled != dhcp || d.AdvertiseDefaultRoute != (kind == "nat" && dhcp) || d.PolicyVersion() != version {
		t.Fatal("UI changed reviewed policy", d)
	}
	x, ok := p.Review["networkXML"].(string)
	if !ok || p.Review["packetVerification"] != "not-run" || p.Review["guestRoutingVerified"] != false || p.Review["autostart"] != false || p.Review["physicalUplinkChanges"] != false {
		t.Fatal("missing review or promoted proof", p.Review)
	}
	if len(p.Steps) != 3 || p.Steps[0].Action != "network.define" || p.Steps[2].Action != "network.activate" {
		t.Fatal("reviewed steps changed", p.Steps)
	}
	filter := "network.ipv6-filter"
	if version == 2 {
		filter = "network.policy-filter"
	}
	if p.Steps[1].Action != filter {
		t.Fatal("wrong helper operation", p.Steps)
	}
	if version == 2 {
		dns := "no"
		if dhcp {
			dns = "yes"
		}
		if !strings.Contains(x, `<dns enable="`+dns+`"/>`) || !strings.Contains(x, `version="2"`) {
			t.Fatal("explicit service or marker lost", x)
		}
		if !strings.Contains(strings.Join(p.Risks, " "), "protectedNetworks") {
			t.Fatal("separate administrator grant omitted")
		}
	} else if strings.Contains(x, "<dns") || !strings.Contains(x, `version="1"`) {
		t.Fatal("legacy XML semantics changed", x)
	}
	if kind == "guest-only" && (strings.Contains(x, "<ip") || strings.Contains(x, "<forward") || !strings.Contains(strings.Join(p.Risks, " "), "static addressing")) {
		t.Fatal("logical subnet became host/guest configuration", x)
	}
}
func assertProtectedNetworkNoMutation(t *testing.T, c *protectedNetworkClient) {
	t.Helper()
	jobs, err := c.service.Engine.Store.Jobs()
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := c.service.Engine.Store.MetadataRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 || len(metadata) != 0 || c.backend.mutations != 0 || c.firewall.mutations != 0 {
		t.Fatal("preview/denial changed network state", jobs, metadata)
	}
}

func protectedNetworkTUIAction(t *testing.T, m Model, command, input string) Model {
	t.Helper()
	found := false
	for section := range sections {
		m.Section = section
		for index, action := range m.actions() {
			if action.Command == command {
				m.Selected = index
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("missing network UI action", command)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil || !m.Editing {
		t.Fatal("network action did not open path form")
	}
	m.Input = input
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil || !m.Busy {
		t.Fatal("path form did not dispatch")
	}
	next, _ = m.Update(cmd())
	return next.(Model)
}
func protectedNetworkTUIPlan(t *testing.T, m Model) domain.Plan {
	t.Helper()
	var response app.Response
	if err := wire.Decode([]byte(m.Output), &response); err != nil {
		t.Fatal("TUI lost structured plan", err, m.Output)
	}
	p := protectedNetworkPlan(t, response)
	if m.Plan == nil || m.Plan.ID != p.ID || m.Plan.Digest != p.Digest {
		t.Fatal("TUI lost exact approval plan")
	}
	return p
}
func TestProtectedNetworkTUIReviewAndApprovalPreservePolicy(t *testing.T) {
	for _, tc := range []struct {
		name, kind, host, cidr string
		dhcp                   bool
		version                int
	}{
		{"protected-nat.yaml", "nat", "services-only", "10.197.240.0/24", true, 2},
		{"protected-lab.yaml", "lab", "services-only", "10.197.241.0/24", true, 2},
		{"protected-static-lab.yaml", "lab", "services-only", "10.197.242.0/24", false, 2},
		{"guest-only.yaml", "guest-only", "deny", "10.88.22.0/24", false, 2},
		{"protected-guest-only-no-cidr.yaml", "guest-only", "deny", "", false, 2},
		{"allowed-host-nat.yaml", "nat", "allow", "10.197.239.0/24", true, 1},
		{"allowed-host-lab.yaml", "lab", "allow", "10.197.238.0/24", true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newProtectedNetworkClient(t)
			path := protectedNetworkExample(t, tc.name)
			m := protectedNetworkTUIAction(t, New(c, "qemu:///system"), "network create", path)
			p := protectedNetworkTUIPlan(t, m)
			assertProtectedNetworkReview(t, p, tc.kind, tc.host, tc.cidr, tc.dhcp, tc.version)
			if c.requests[0].Path != path || c.requests[0].Action != "create" || c.requests[0].Apply != nil || c.firewall.checks != 0 {
				t.Fatal("protected preview changed request or demanded grant", c.requests)
			}
			pages := ""
			for i := 0; m.Offset <= len(wrap(m.Output, m.Width)); i++ {
				if i == 200 {
					t.Fatal("review exceeded bounded fixture paging")
				}
				pages += m.View()
				old := m.Offset
				next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
				m = next.(Model)
				if cmd != nil {
					t.Fatal("paging submitted a service request")
				}
				if m.Offset == old {
					break
				}
			}
			for _, visible := range []string{"hostAccess", tc.host, "networkXML", "packetVerification", "not-run", "guestRoutingVerified"} {
				if !strings.Contains(strings.ReplaceAll(pages, "\n", ""), visible) {
					t.Fatal("protected review omitted from80x24paged result", visible)
				}
			}
			if tc.version == 2 && !strings.Contains(strings.ReplaceAll(pages, "\n", ""), "protectedNetworks") {
				t.Fatal("administrator boundary not visible")
			}
			m.Plan = nil
			m = protectedNetworkTUIAction(t, m, "plan show", p.ID)
			shown := protectedNetworkTUIPlan(t, m)
			stored, body, err := c.service.Engine.Store.Plan(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if shown.Digest != p.Digest || stored.Digest != p.Digest || !reflect.DeepEqual(shown.Review, p.Review) {
				t.Fatal("readback changed approved protection")
			}
			var recipe struct {
				Version int `json:"version"`
			}
			if json.Unmarshal(body, &recipe) != nil || recipe.Version != tc.version {
				t.Fatal("incorrect persisted recipe")
			}
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			m = next.(Model)
			if cmd != nil || !m.Confirm {
				t.Fatal("review did not require confirmation")
			}
			m.Input = "wrong-digest"
			next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = next.(Model)
			if cmd != nil || len(c.requests) != 2 {
				t.Fatal("wrong digest submitted")
			}
			next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			m = next.(Model)
			if cmd != nil || m.Confirm || len(c.requests) != 2 {
				t.Fatal("canceling approval submitted mutation")
			}
			next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
			m = next.(Model)
			if cmd != nil || !m.Confirm {
				t.Fatal("cannot reopen review")
			}
			m.Input = shown.Digest
			next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = next.(Model)
			if cmd == nil {
				t.Fatal("exact digest did not submit")
			}
			next, _ = m.Update(cmd())
			m = next.(Model)
			apply := c.requests[len(c.requests)-1].Apply
			acks := append([]string{}, p.Acknowledgements...)
			sort.Strings(acks)
			if apply == nil || apply.PlanID != p.ID || apply.PlanDigest != p.Digest || !reflect.DeepEqual(apply.Acknowledgements, acks) || c.firewall.checks != 1 {
				t.Fatal("protected approval changed", apply)
			}
			family := "networks"
			if tc.version == 2 {
				family = "protectedNetworks"
			}
			if m.Plan != nil || m.Confirm || !strings.Contains(m.Output, "PERMISSION_DENIED") || !strings.Contains(m.Output, family+" exact administrator grant missing") {
				t.Fatal("grant denial hidden or retained success", m.Output)
			}
			if !reflect.DeepEqual(c.methods, []string{"network.create", "plan.show", "operation.apply"}) {
				t.Fatal(c.methods)
			}
			assertProtectedNetworkNoMutation(t, c)
		})
	}
}
func TestProtectedNetworkTUIFailedPreviewClearsPriorApproval(t *testing.T) {
	for _, fault := range []string{"canceled", "session", "missing-path"} {
		t.Run(fault, func(t *testing.T) {
			c := newProtectedNetworkClient(t)
			path := protectedNetworkExample(t, "protected-nat.yaml")
			m := protectedNetworkTUIAction(t, New(c, "qemu:///system"), "network create", path)
			p := protectedNetworkTUIPlan(t, m)
			switch fault {
			case "canceled":
				c.canceled = true
			case "session":
				m.Connection = "qemu:///session"
			case "missing-path":
				path = filepath.Join(t.TempDir(), "missing.yaml")
			}
			m = protectedNetworkTUIAction(t, m, "network create", path)
			if m.Plan != nil || m.Confirm || strings.Contains(m.Output, p.Digest) || strings.Contains(m.Output, "network.policy-filter") || strings.Contains(m.Output, "networkXML") {
				t.Fatal("failed preview preserved approved protection", m.Output)
			}
			var response app.Response
			if err := wire.Decode([]byte(m.Output), &response); err != nil || response.Error == nil {
				t.Fatal("failure not visible", err, m.Output)
			}
			for _, r := range c.requests {
				if r.Apply != nil {
					t.Fatal("failed preview submitted authority")
				}
			}
			assertProtectedNetworkNoMutation(t, c)
		})
	}
}
