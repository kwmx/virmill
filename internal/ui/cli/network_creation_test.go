package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestNetworkCreationUsesSharedPreview(t *testing.T) {
	for _, tt := range []struct {
		args                     []string
		method, action, id, path string
	}{{[]string{"network", "create", "network.yaml", "--plan"}, "network.create", "create", "", "network.yaml"}, {[]string{"network", "creation", "resume", "job-id", "--plan"}, "network.creation.resume", "resume", "job-id", ""}, {[]string{"network", "creation", "result", "job-id"}, "network.creation.result", "", "job-id", ""}} {
		r := &recorder{}
		var out bytes.Buffer
		c := New(r, &out, &out)
		c.SetArgs(append(tt.args, "--output", "json", "--non-interactive"))
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
		if r.method != tt.method || r.request.Action != tt.action || r.request.ID != tt.id || (tt.path != "" && filepath.Base(r.request.Path) != tt.path) || r.request.Apply != nil {
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
func protectedNetworkCLI(t *testing.T, c *protectedNetworkClient, output string, args ...string) (app.Response, string, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	cmd := New(c, &out, &stderr)
	cmd.SetArgs(append(args, "--output", output, "--non-interactive"))
	err := cmd.Execute()
	var response app.Response
	if decodeErr := wire.Decode(out.Bytes(), &response); decodeErr != nil {
		t.Fatal("unclean machine response", out.String(), stderr.String(), decodeErr)
	}
	return response, out.String(), err
}
func TestProtectedNetworkCLIReviewExamplesAndExactApproval(t *testing.T) {
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
		for _, output := range []string{"json", "ndjson"} {
			t.Run(tc.name+"/"+output, func(t *testing.T) {
				c := newProtectedNetworkClient(t)
				path := protectedNetworkExample(t, tc.name)
				r, text, err := protectedNetworkCLI(t, c, output, "network", "create", path, "--plan")
				if err != nil {
					t.Fatal(err)
				}
				p := protectedNetworkPlan(t, r)
				assertProtectedNetworkReview(t, p, tc.kind, tc.host, tc.cidr, tc.dhcp, tc.version)
				if c.requests[0].Path != path || c.requests[0].Action != "create" || c.requests[0].Apply != nil || c.firewall.checks != 0 {
					t.Fatal("preview needed grant or changed request", c.requests)
				}
				if output == "ndjson" && strings.Count(text, "\n") != 1 {
					t.Fatal("NDJSON is not one clean record")
				}
				shown, _, err := protectedNetworkCLI(t, c, output, "plan", "show", p.ID)
				if err != nil {
					t.Fatal(err)
				}
				q := protectedNetworkPlan(t, shown)
				stored, body, err := c.service.Engine.Store.Plan(p.ID)
				if err != nil {
					t.Fatal(err)
				}
				if q.Digest != p.Digest || stored.Digest != p.Digest || !reflect.DeepEqual(q.Review, p.Review) {
					t.Fatal("plan readback lost reviewed protection")
				}
				var recipe struct {
					Version int `json:"version"`
				}
				if json.Unmarshal(body, &recipe) != nil || recipe.Version != tc.version {
					t.Fatal("wrong persisted policy recipe", string(body))
				}
				args := []string{"plan", "apply", q.ID, "--digest", q.Digest, "--idempotency-key", "protected-ui-denial"}
				for _, ack := range q.Acknowledgements {
					args = append(args, "--ack", ack)
				}
				denied, _, err := protectedNetworkCLI(t, c, output, args...)
				apply := c.requests[len(c.requests)-1].Apply
				acks := append([]string{}, p.Acknowledgements...)
				sort.Strings(acks)
				if err == nil || denied.Error == nil || denied.Error.Code != "PERMISSION_DENIED" || apply == nil || apply.PlanID != q.ID || apply.PlanDigest != q.Digest || !reflect.DeepEqual(apply.Acknowledgements, acks) || c.firewall.checks != 1 {
					t.Fatal("exact approval/grant refusal lost", denied, apply, err)
				}
				if !reflect.DeepEqual(c.methods, []string{"network.create", "plan.show", "operation.apply"}) {
					t.Fatal(c.methods)
				}
				assertProtectedNetworkNoMutation(t, c)
			})
		}
	}
}
func TestProtectedNetworkCLICancellationAndValidationDoNotExposeSuccess(t *testing.T) {
	for _, fault := range []string{"canceled", "session", "unexpected-input"} {
		t.Run(fault, func(t *testing.T) {
			c := newProtectedNetworkClient(t)
			args := []string{"network", "create", protectedNetworkExample(t, "protected-nat.yaml"), "--plan"}
			switch fault {
			case "canceled":
				c.canceled = true
			case "session":
				args = append(args, "--connection", "qemu:///session")
			case "unexpected-input":
				args = append(args, "--input", `{"hostAccess":"allow"}`)
			}
			r, text, err := protectedNetworkCLI(t, c, "json", args...)
			if err == nil || r.Error == nil || strings.Contains(text, "network.policy-filter") || strings.Contains(text, "networkXML") {
				t.Fatal("failed request displayed successful protected review", r, err)
			}
			if len(c.requests) != 1 || c.requests[0].Apply != nil || c.firewall.checks != 0 {
				t.Fatal("failure acquired authority", c.requests)
			}
			assertProtectedNetworkNoMutation(t, c)
		})
	}
}
