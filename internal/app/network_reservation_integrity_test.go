package app

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

// These records refer only to a generated definition that never appeared in
// the fixture backend. Native inventory therefore cannot mask a forgotten
// application reservation. All corruption is confined to temporary SQLite.
func TestNetworkReservationValidShapedCorruptionRefusesAllocation(t *testing.T) {
	for _, corruption := range []string{"subnet", "metadata", "record-plan", "record-digest", "original-plan", "original-input", "job-plan", "job-identity", "metadata-key"} {
		t.Run(corruption, func(t *testing.T) {
			s, generated, _ := networkService(t)
			generated.fault = "missing-definition"
			p := networkPlan(t, s)
			other := networkPlan(t, s)
			j := awaitConfig(t, s, applyConfig(t, s, p).ID)
			if j.State != "recovery-required" || j.Step != 0 {
				t.Fatal("fixture did not retain an absent-definition reservation", j)
			}
			_, input, err := s.Engine.Store.Plan(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			recipe, err := parseNetworkRecipe(p, input)
			if err != nil {
				t.Fatal(err)
			}
			key := recipe.Definition.UUID
			raw, err := s.Engine.Store.MetadataBytes(networkRecordKind, key)
			if err != nil {
				t.Fatal(err)
			}
			var record networkRecord
			if err = json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			before := s.Call(context.Background(), p.ActorUID, "network.cidr.check", cidrCheckRequest(recipe.Definition.IPv4CIDR))
			if before.Error != nil {
				t.Fatal(before.Error)
			}
			report, ok := before.Data.(cidrReport)
			if !ok || len(report.Candidates) != 1 || len(report.Candidates[0].Conflicts) != 1 || report.Candidates[0].Conflicts[0].Source != "application-reservation" {
				t.Fatal("fixture did not depend solely on its valid durable reservation", before)
			}

			writeJSON := func(statement string, value any, args ...any) {
				t.Helper()
				body, err := operations.Canonical(value)
				if err != nil {
					t.Fatal(err)
				}
				result, err := s.Engine.Store.DB.Exec(statement, append([]any{body}, args...)...)
				if err != nil {
					t.Fatal(err)
				}
				if rows, err := result.RowsAffected(); err != nil || rows != 1 {
					t.Fatal("fixture did not change exactly one row", rows, err)
				}
			}
			changedMetadata := json.RawMessage(`{"displayName":"Changed reservation","name":"fixture-lab","tags":["test"]}`)
			switch corruption {
			case "subnet":
				record.Definition.IPv4CIDR = "10.197.237.0/24"
			case "metadata":
				record.Metadata = changedMetadata
			case "record-plan":
				record.PlanID, record.PlanDigest = other.ID, other.Digest
			case "record-digest":
				record.PlanDigest = strings.Repeat("0", 64)
			case "original-plan":
				changed := p
				changed.Risks = append(append([]string{}, p.Risks...), "Generated changed review")
				writeJSON("UPDATE plans SET body=? WHERE id=?", changed, p.ID)
			case "original-input":
				// Keep the record and recipe metadata mutually consistent, so the
				// original input digest is the remaining authoritative binding.
				recipe.Metadata, record.Metadata = changedMetadata, changedMetadata
				writeJSON("UPDATE plans SET input=? WHERE id=?", recipe, p.ID)
			case "job-plan":
				changed := j
				changed.PlanID = other.ID
				writeJSON("UPDATE jobs SET body=? WHERE id=?", changed, j.ID)
			case "job-identity":
				changed := j
				changed.ID = domain.ID()
				writeJSON("UPDATE jobs SET body=? WHERE id=?", changed, j.ID)
			case "metadata-key":
				if _, err = s.Engine.Store.DB.Exec("UPDATE metadata SET id=? WHERE kind=? AND id=?", domain.ID(), networkRecordKind, key); err != nil {
					t.Fatal(err)
				}
			}
			if err := networkxml.Validate(record.Definition); err != nil {
				t.Fatal("corruption accidentally invalidated definition shape", err)
			}
			if corruption != "metadata-key" {
				writeJSON("UPDATE metadata SET body=? WHERE kind=? AND id=?", record, networkRecordKind, key)
			}
			snapshot := networkIntegritySnapshot(t, s)
			check := s.Call(context.Background(), p.ActorUID, "network.cidr.check", cidrCheckRequest("10.197.238.0/24"))
			if check.Error == nil || (check.Error.Code != "SOURCE_CHANGED" && check.Error.Code != "INVALID_STATE") {
				t.Errorf("corrupt reservation produced an allocation report: %+v", check)
			}
			declaration := networkDocument(t, "lab", "allow")
			document, err := os.ReadFile(declaration)
			if err != nil {
				t.Fatal(err)
			}
			// A disjoint candidate must still refuse corrupt allocation authority;
			// an ordinary overlap error must not mask the missing integrity check.
			if err = os.WriteFile(declaration, []byte(strings.ReplaceAll(string(document), "10.197.238.0/24", "10.198.238.0/24")), 0600); err != nil {
				t.Fatal(err)
			}
			preview := s.Call(context.Background(), p.ActorUID, "network.create", Request{Connection: p.ConnectionID, Action: "create", Path: declaration})
			if preview.Error == nil || (preview.Error.Code != "SOURCE_CHANGED" && preview.Error.Code != "INVALID_STATE") {
				t.Errorf("corrupt reservation was treated as usable allocation input: %+v", preview)
			}
			if after := networkIntegritySnapshot(t, s); !reflect.DeepEqual(snapshot, after) {
				t.Fatal("failed observation/preview changed plans, journal, reservation or lock bytes")
			}
			assertNetworkBoundaryLocks(t, s, p, j.ID)
			generated.mu.Lock()
			definitions, activations, defined := generated.definitions, generated.activations, generated.defined
			generated.mu.Unlock()
			if definitions != 1 || activations != 0 || defined != nil {
				t.Fatal("corruption checks issued an unintended provider effect")
			}
			filter := s.NetworkFirewall.(*networkFirewallFixture)
			filter.mu.Lock()
			applications := filter.applications
			filter.mu.Unlock()
			if applications != 0 {
				t.Fatal("corruption checks issued an unintended firewall effect")
			}
		})
	}
}

func networkIntegritySnapshot(t *testing.T, s *Service) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, statement := range []string{
		"SELECT id,digest,body,input FROM plans ORDER BY id",
		"SELECT id,plan_id,body FROM jobs ORDER BY id",
		"SELECT kind,id,body FROM metadata ORDER BY kind,id",
		"SELECT job_id,seq,body FROM events ORDER BY job_id,seq",
		"SELECT resource,job_id FROM locks ORDER BY resource",
		"SELECT key,request_digest,job_id,created_at FROM dedup ORDER BY key",
	} {
		rows, err := s.Engine.Store.DB.Query(statement)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		var all [][]any
		for rows.Next() {
			values, destinations := make([]any, len(columns)), make([]any, len(columns))
			for i := range values {
				destinations[i] = &values[i]
			}
			if err := rows.Scan(destinations...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			all = append(all, values)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(all)
		if err != nil {
			t.Fatal(err)
		}
		out[statement] = string(body)
	}
	return out
}
