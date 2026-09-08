package app

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

// All native effects are deterministic generated fixtures; these tests do not
// certify firewall enforcement, packet isolation or any real host/guest state.
func TestProtectedBoundaryAddresslessResultDoesNotClaimSubnetReservation(t *testing.T) {
	for _, cidr := range []bool{false, true} {
		t.Run(map[bool]string{false: "addressless", true: "logical-subnet"}[cidr], func(t *testing.T) {
			s, _, _ := networkService(t)
			p := protectedNetworkPlan(t, s, "guest-only", false, cidr)
			job := awaitConfig(t, s, applyConfig(t, s, p).ID)
			if job.State != "succeeded" {
				t.Fatal(job)
			}
			before := networkIntegritySnapshot(t, s)
			response := s.Call(context.Background(), p.ActorUID, "network.creation.result", Request{Connection: p.ConnectionID, ID: job.ID})
			result, ok := response.Data.(map[string]any)
			if response.Error != nil || !ok || result["subnetReserved"] != cidr || result["packetVerification"] != "not-run" || result["guestRoutingVerified"] != false {
				t.Fatalf("addressless identity record overstated allocation/packet proof: %+v", response)
			}
			records, err := s.networkRecords()
			if err != nil || len(records) != 1 || records[0].JobID != job.ID || records[0].Version != 2 {
				t.Fatal("result lost durable identity binding", records, err)
			}
			if !reflect.DeepEqual(before, networkIntegritySnapshot(t, s)) {
				t.Fatal("read-only creation result changed durable state")
			}
		})
	}
}

func TestProtectedBoundaryCanceledResultContainsNoSuccessPayload(t *testing.T) {
	s, _, _ := networkService(t)
	p := protectedNetworkPlan(t, s, "guest-only", false, false)
	job := awaitConfig(t, s, applyConfig(t, s, p).ID)
	if job.State != "succeeded" {
		t.Fatal(job)
	}
	before := networkIntegritySnapshot(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := s.Call(ctx, p.ActorUID, "network.creation.result", Request{Connection: p.ConnectionID, ID: job.ID})
	if response.Error == nil || response.Data != nil {
		t.Fatalf("canceled result returned success-shaped job/definition data: %+v", response)
	}
	if !reflect.DeepEqual(before, networkIntegritySnapshot(t, s)) {
		t.Fatal("canceled result mutated durable state")
	}
}

func TestProtectedBoundaryAddresslessPlanCannotHideCorruptReservation(t *testing.T) {
	s, native, _ := networkService(t)
	native.fault = "missing-definition"
	p := networkPlan(t, s)
	job := awaitConfig(t, s, applyConfig(t, s, p).ID)
	if job.State != "recovery-required" || job.Step != 0 {
		t.Fatal(job)
	}
	_, input, err := s.Engine.Store.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	recipe, err := parseNetworkRecipe(p, input)
	if err != nil {
		t.Fatal(err)
	}
	beforeRecord, err := s.Engine.Store.MetadataBytes(networkRecordKind, recipe.Definition.UUID)
	if err != nil {
		t.Fatal(err)
	}
	var record networkRecord
	if err = json.Unmarshal(beforeRecord, &record); err != nil {
		t.Fatal(err)
	}
	record.JobID = domain.ID()
	if err = s.Engine.Store.ComparePut(networkRecordKind, record.Definition.UUID, beforeRecord, record); err != nil {
		t.Fatal(err)
	}
	before := networkIntegritySnapshot(t, s)
	result := s.Call(context.Background(), p.ActorUID, "network.create", Request{Connection: p.ConnectionID, Action: "create", Path: protectedNetworkDocument(t, "guest-only", false, false)})
	if result.Error == nil || result.Error.Code != "SOURCE_CHANGED" {
		t.Fatalf("addressless shortcut bypassed corrupt allocation authority: %+v", result)
	}
	if !reflect.DeepEqual(before, networkIntegritySnapshot(t, s)) {
		t.Fatal("refused preview changed records, plans or jobs")
	}
	native.mu.Lock()
	defer native.mu.Unlock()
	if native.definitions != 1 || native.activations != 0 || native.defined != nil {
		t.Fatal("refused addressless preview changed native fixture")
	}
}

func TestProtectedBoundaryMixedVersionRecordsRemainDistinctAfterReopen(t *testing.T) {
	s, native, path := networkService(t)
	legacy := networkPlan(t, s)
	legacyJob := awaitConfig(t, s, applyConfig(t, s, legacy).ID)
	if legacyJob.State != "succeeded" {
		t.Fatal(legacyJob)
	}
	// The fake backend supports one network. Removing its observation here does
	// not remove the original durable reservation or perform a native deletion.
	native.mu.Lock()
	native.defined, native.active = nil, false
	native.mu.Unlock()
	protected := protectedNetworkPlan(t, s, "guest-only", false, false)
	protectedJob := awaitConfig(t, s, applyConfig(t, s, protected).ID)
	if protectedJob.State != "succeeded" {
		t.Fatal(protectedJob)
	}
	before := networkIntegritySnapshot(t, s)
	s.Engine.Close()
	if err := s.Engine.Store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Engine = operations.New(db)
	s.Engine.Handlers["network.create"] = &networkCreationHandler{s: s}
	s.Engine.Handlers["network.creation.resume"] = &networkResumeHandler{networkCreationHandler{s: s}}
	records, err := s.networkRecords()
	if err != nil || len(records) != 2 {
		t.Fatal(records, err)
	}
	versions := map[string]int{}
	for _, record := range records {
		versions[record.JobID] = record.Version
	}
	if versions[legacyJob.ID] != 1 || versions[protectedJob.ID] != 2 {
		t.Fatal("reopening reinterpreted a legacy or protected record", versions)
	}
	report := cidrCheckResult(t, s, cidrCheckRequest("10.197.238.0/24"))
	if len(report.Candidates[0].Conflicts) != 1 || report.Candidates[0].Conflicts[0].Source != "application-reservation" {
		t.Fatal("addressless record invented a subnet or legacy reservation disappeared", report)
	}
	if !reflect.DeepEqual(before, networkIntegritySnapshot(t, s)) {
		t.Fatal("record loading performed a silent migration or mutation")
	}
}
