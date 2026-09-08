package app

import (
	"bytes"
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

// The callback runs after the generated provider's definition effect returns,
// before the engine reaches the next step. It never calls a native backend.
type networkBoundaryFixture struct {
	*networkCreationFixture
	afterDefinition func(context.Context) error
}

func (n *networkBoundaryFixture) DefineNetwork(ctx context.Context, uri string, d domain.NetworkDefinition) error {
	if err := n.networkCreationFixture.DefineNetwork(ctx, uri, d); err != nil {
		return err
	}
	return n.afterDefinition(ctx)
}

func assertNetworkBoundaryLocks(t *testing.T, s *Service, p domain.Plan, jobID string) {
	t.Helper()
	rows, err := s.Engine.Store.DB.Query("SELECT resource,job_id FROM locks ORDER BY resource")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := make(map[string]string, len(p.ResourceIDs))
	for _, resource := range p.ResourceIDs {
		want[resource] = jobID
	}
	got := map[string]string{}
	for rows.Next() {
		var resource, owner string
		if err := rows.Scan(&resource, &owner); err != nil {
			t.Fatal(err)
		}
		got[resource] = owner
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("partial definition lost its exact lock set: got %v want %v", got, want)
	}
}

func TestNetworkCreationLaterVPNConflictRetainsDefinitionForReviewedResume(t *testing.T) {
	testNetworkCreationRetainedBoundary(t, false)
}

func TestNetworkCreationBetweenStepCancellationRetainsDefinitionForReviewedResume(t *testing.T) {
	testNetworkCreationRetainedBoundary(t, true)
}

func testNetworkCreationRetainedBoundary(t *testing.T, cancellation bool) {
	t.Helper()
	s, generated, _ := networkService(t)
	var conflict atomic.Bool
	s.HostPrefixes = func(ctx context.Context) (domain.HostNetworkPrefixes, error) {
		out := domain.HostNetworkPrefixes{Prefixes: []domain.HostNetworkPrefix{{CIDR: "0.0.0.0/0", Source: "route"}}}
		if conflict.Load() {
			out.Prefixes = append(out.Prefixes, domain.HostNetworkPrefix{CIDR: "10.197.0.0/16", Source: "route", Table: 901})
		}
		return out, ctx.Err()
	}
	s.Provider = &networkBoundaryFixture{networkCreationFixture: generated, afterDefinition: func(ctx context.Context) error {
		if cancellation {
			_, err := s.Engine.Cancel(operations.OperationID(ctx))
			return err
		}
		conflict.Store(true)
		return nil
	}}
	p := networkPlan(t, s)
	j := awaitConfig(t, s, applyConfig(t, s, p).ID)
	if j.State != "recovery-required" || j.Step != 1 || j.Error == nil {
		t.Fatalf("retained definition was treated as a completed safe boundary: %+v", j)
	}
	if cancellation {
		if !j.CancelRequested || j.Error.Code != "RECOVERY_REQUIRED" {
			t.Fatalf("cancellation history or uncertain disposition missing: %+v", j)
		}
	} else if j.Error.Code != "CIDR_CONFLICT" {
		t.Fatalf("failure did not come from the new VPN prefix: %+v", j)
	}
	generated.mu.Lock()
	definitions, activations, active := generated.definitions, generated.activations, generated.active
	var definition domain.NetworkDefinition
	if generated.defined != nil {
		definition = *generated.defined
	}
	generated.mu.Unlock()
	if definitions != 1 || activations != 0 || active || definition.UUID == "" {
		t.Fatalf("boundary injection did not retain just one inactive definition: define=%d activate=%d active=%v", definitions, activations, active)
	}
	filter := s.NetworkFirewall.(*networkFirewallFixture)
	filter.mu.Lock()
	filterApplications := filter.applications
	filter.mu.Unlock()
	if filterApplications != 0 {
		t.Fatal("failed or canceled boundary still applied a firewall effect", filterApplications)
	}
	assertNetworkBoundaryLocks(t, s, p, j.ID)
	reservation, err := s.Engine.Store.MetadataBytes(networkRecordKind, definition.UUID)
	if err != nil || len(reservation) == 0 {
		t.Fatal("definition lost its original subnet reservation", err)
	}
	if !cancellation {
		if _, err = s.planNetworkResume(context.Background(), p.ActorUID, Request{Connection: p.ConnectionID, Action: "resume", ID: j.ID}); err == nil {
			t.Fatal("resume accepted while the newly observed VPN overlap remained")
		}
		assertNetworkBoundaryLocks(t, s, p, j.ID)
		conflict.Store(false)
	}
	resumed, err := s.planNetworkResume(context.Background(), p.ActorUID, Request{Connection: p.ConnectionID, Action: "resume", ID: j.ID})
	if err != nil {
		t.Fatal("retained definition has no reviewed continuation", err)
	}
	if resumed.ID == p.ID || !reflect.DeepEqual(resumed.ResourceIDs, p.ResourceIDs) {
		t.Fatal("recovery did not create a fresh plan for the exact original resources", resumed)
	}
	for _, step := range resumed.Steps {
		if step.ID == "define" {
			t.Fatal("reviewed continuation could redefine the retained network")
		}
	}
	child := awaitConfig(t, s, applyConfig(t, s, resumed).ID)
	if child.State != "succeeded" || child.RecoveryOf != j.ID {
		t.Fatal("reviewed continuation did not complete its generated predicates", child)
	}
	parent, err := s.Engine.Store.Job(j.ID)
	if err != nil || parent.State != "partial" || parent.RecoveryOperationID != child.ID || parent.Error == nil {
		t.Fatal("original partial history was rewritten or recovery link lost", parent, err)
	}
	finalReservation, err := s.Engine.Store.MetadataBytes(networkRecordKind, definition.UUID)
	if err != nil || !bytes.Equal(reservation, finalReservation) {
		t.Fatal("reviewed continuation changed the original reservation", err)
	}
	generated.mu.Lock()
	definitions, activations, active = generated.definitions, generated.activations, generated.active
	finalDefinition := *generated.defined
	generated.mu.Unlock()
	if definitions != 1 || activations != 1 || !active || finalDefinition != definition {
		t.Fatalf("reviewed continuation replayed or substituted the network: define=%d activate=%d active=%v", definitions, activations, active)
	}
	filter.mu.Lock()
	filterApplications = filter.applications
	filter.mu.Unlock()
	if filterApplications != 1 {
		t.Fatal("reviewed continuation did not apply exactly one generated firewall effect", filterApplications)
	}
	var lockCount int
	if err = s.Engine.Store.DB.QueryRow("SELECT count(*) FROM locks").Scan(&lockCount); err != nil || lockCount != 0 {
		t.Fatal("successful generated continuation retained stale locks", lockCount, err)
	}
}
