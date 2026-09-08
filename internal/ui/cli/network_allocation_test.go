package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

// These tests use the shared service and durable plan store. Only native
// inventory and the helper are substituted; no host networking is exercised.
func allocationCLIExample(t *testing.T) (string, network.AllocationConfig) {
	t.Helper()
	raw, err := os.ReadFile(protectedNetworkExample(t, "auto-lab.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "automatic lab declaration.yaml")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../../../examples/network-allocation.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg network.AllocationConfig
	if err = wire.Decode(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if err = network.ValidateAllocationConfig(cfg); err != nil {
		t.Fatal("documented allocation configuration is invalid", err)
	}
	return path, cfg
}

type allocationCLIFirewall struct {
	*protectedNetworkFirewall
	allowCheck bool
	checked    []domain.NetworkDefinition
}

func (f *allocationCLIFirewall) Check(ctx context.Context, p domain.Plan, d domain.NetworkDefinition) error {
	f.checked = append(f.checked, d)
	if f.allowCheck {
		f.checks++
		return ctx.Err()
	}
	return f.protectedNetworkFirewall.Check(ctx, p, d)
}

// The shared service may retain a zero Plan/Job in an error envelope. It must
// never expose a usable identifier, approval or job state as a successful result.
func allocationCLIEmptyErrorData[T any](t *testing.T, data any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var got, empty T
	if err = wire.Decode(raw, &got); err != nil || !reflect.DeepEqual(got, empty) {
		t.Fatal("error exposed a nonzero success payload", string(raw), err)
	}
}

func TestNetworkAllocationCLIPlanReadbackAndExactApply(t *testing.T) {
	for _, output := range []string{"json", "ndjson"} {
		for _, change := range []string{"ranges", "planned-conflict"} {
			t.Run(output+"/"+change, func(t *testing.T) {
				c := newProtectedNetworkClient(t)
				path, cfg := allocationCLIExample(t)
				original, _ := json.Marshal(cfg)
				c.service.NetworkAllocation = func(ctx context.Context) (network.AllocationConfig, error) { return cfg, ctx.Err() }
				c.service.HostPrefixes = func(ctx context.Context) (domain.HostNetworkPrefixes, error) {
					return domain.HostNetworkPrefixes{Prefixes: []domain.HostNetworkPrefix{
						{CIDR: "0.0.0.0/0", Source: "route", Table: 254},
						{CIDR: "10.0.0.0/24", Source: "address", InterfaceIndex: 2},
						{CIDR: "10.0.1.0/24", Source: "route", InterfaceIndex: 3, Table: 100},
					}}, ctx.Err()
				}
				f := &allocationCLIFirewall{protectedNetworkFirewall: c.firewall}
				c.service.NetworkFirewall = f
				response, text, err := protectedNetworkCLI(t, c, output, "network", "create", path, "--plan")
				if err != nil {
					t.Fatal(err)
				}
				p := protectedNetworkPlan(t, response)
				assertProtectedNetworkReview(t, p, "lab", "services-only", "10.0.2.0/24", true, 2)
				if c.requests[0].Path != path || c.requests[0].Action != "create" || c.requests[0].Apply != nil || len(f.checked) != 0 {
					t.Fatal("automatic preview changed the path or acquired apply authority", c.requests)
				}
				if output == "ndjson" && strings.Count(text, "\n") != 1 {
					t.Fatal("automatic preview polluted NDJSON")
				}
				var allocation struct {
					Version        int                      `json:"version"`
					RequestedCIDR  string                   `json:"requestedCIDR"`
					Config         network.AllocationConfig `json:"config"`
					ObservedDigest string                   `json:"observedDigest"`
				}
				raw, _ := json.Marshal(p.Review["allocation"])
				if err = wire.Decode(raw, &allocation); err != nil || allocation.Version != 1 || allocation.RequestedCIDR != "auto" || len(allocation.ObservedDigest) != 64 || !reflect.DeepEqual(allocation.Config, cfg) {
					t.Fatal("shared allocation review was lost", string(raw), err)
				}
				assertProtectedNetworkNoMutation(t, c)
				// Replace the settings after review. Apply must keep the selected
				// subnet; a newly declared conflict must refuse before any job.
				replacement := `{"version":1,"ranges":[{"cidr":"172.16.0.0/12","prefixLength":24}],"planned":[]}`
				wantCode := "PERMISSION_DENIED"
				if change == "planned-conflict" {
					replacement = `{"version":1,"ranges":[{"cidr":"10.0.0.0/8","prefixLength":24}],"planned":[{"id":"new-external-lab","cidr":"10.0.2.0/24"}]}`
					wantCode, f.allowCheck = "CIDR_CONFLICT", true
				}
				if err = wire.Decode([]byte(replacement), &cfg); err != nil {
					t.Fatal(err)
				}
				shown, _, err := protectedNetworkCLI(t, c, output, "plan", "show", p.ID)
				if err != nil {
					t.Fatal(err)
				}
				q := protectedNetworkPlan(t, shown)
				stored, recipe, err := c.service.Engine.Store.Plan(p.ID)
				if err != nil || stored.Digest != p.Digest || q.Digest != p.Digest || !reflect.DeepEqual(q.Review, p.Review) {
					t.Fatal("settings change rewrote the reviewed selection", err)
				}
				var persisted struct {
					Allocation struct {
						Config json.RawMessage `json:"config"`
					} `json:"allocation"`
				}
				if err = json.Unmarshal(recipe, &persisted); err != nil || !json.Valid(persisted.Allocation.Config) {
					t.Fatal("allocation absent from durable recipe", err)
				}
				var before, after any
				_ = json.Unmarshal(original, &before)
				_ = json.Unmarshal(persisted.Allocation.Config, &after)
				if !reflect.DeepEqual(before, after) {
					t.Fatal("durable recipe adopted later settings")
				}
				args := []string{"plan", "apply", p.ID, "--digest", p.Digest, "--idempotency-key", "auto-ui-refusal"}
				for _, ack := range p.Acknowledgements {
					args = append(args, "--ack", ack)
				}
				denied, _, err := protectedNetworkCLI(t, c, output, args...)
				apply := c.requests[len(c.requests)-1].Apply
				acks := append([]string{}, p.Acknowledgements...)
				sort.Strings(acks)
				if err == nil || denied.Error == nil || denied.Error.Code != wantCode || apply == nil || apply.PlanID != p.ID || apply.PlanDigest != p.Digest || !reflect.DeepEqual(apply.Acknowledgements, acks) {
					t.Fatal("automatic allocation approval/refusal changed", denied, apply, err)
				}
				allocationCLIEmptyErrorData[domain.Job](t, denied.Data)
				if len(f.checked) != 1 || f.checked[0].IPv4CIDR != "10.0.2.0/24" || !reflect.DeepEqual(c.methods, []string{"network.create", "plan.show", "operation.apply"}) {
					t.Fatal("apply reallocated or dispatched unexpected work", f.checked, c.methods)
				}
				assertProtectedNetworkNoMutation(t, c)
			})
		}
	}
}

func TestNetworkAllocationCLIFailureHasNoSuccessOrReservation(t *testing.T) {
	for _, fault := range []string{"canceled", "exhausted", "invalid-settings"} {
		t.Run(fault, func(t *testing.T) {
			c := newProtectedNetworkClient(t)
			path, cfg := allocationCLIExample(t)
			wantCode := "OPERATION_FAILED"
			switch fault {
			case "canceled":
				c.canceled = true
			case "exhausted":
				wantCode = "CIDR_CONFLICT"
				c.service.HostPrefixes = func(ctx context.Context) (domain.HostNetworkPrefixes, error) {
					return domain.HostNetworkPrefixes{Prefixes: []domain.HostNetworkPrefix{{CIDR: "0.0.0.0/0", Source: "address"}}}, ctx.Err()
				}
			case "invalid-settings":
				cfg.Version = 0
				wantCode = "INVALID_INPUT"
			}
			c.service.NetworkAllocation = func(ctx context.Context) (network.AllocationConfig, error) { return cfg, ctx.Err() }
			r, text, err := protectedNetworkCLI(t, c, "json", "network", "create", path, "--plan")
			if err == nil || r.Error == nil || r.Error.Code != wantCode || strings.Contains(text, "networkXML") || strings.Contains(text, "observedDigest") {
				t.Fatal("failed automatic preview exposed success", r, err)
			}
			allocationCLIEmptyErrorData[domain.Plan](t, r.Data)
			if len(c.requests) != 1 || c.requests[0].Apply != nil || c.firewall.checks != 0 {
				t.Fatal("failure acquired authority")
			}
			assertProtectedNetworkNoMutation(t, c)
		})
	}
}
