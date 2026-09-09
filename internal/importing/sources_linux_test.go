//go:build linux && amd64

package importing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

func sourcesFixture(t *testing.T) (*app.Service, *store.Store) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(directory, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	engine := operations.New(db)
	t.Cleanup(func() {
		engine.Close()
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	s := &app.Service{Engine: engine, Extensions: map[string]func(context.Context, uint32, app.Request) (any, error){}}
	registerSources(s)
	return s, db
}

func sourceJournalEntry(t *testing.T, db *store.Store, uid uint32, operation, state, kind, name, systemID string, artifactPresent bool) (domain.Job, string) {
	t.Helper()
	destination := filepath.Join("/unmaterialized-fixture", domain.ID())
	input, err := operations.Canonical(map[string]any{"destination": destination})
	if err != nil {
		t.Fatal(err)
	}
	inputDigest, err := operations.Digest(json.RawMessage(input))
	if err != nil {
		t.Fatal(err)
	}
	p := domain.Plan{APIVersion: domain.APIVersion, ID: domain.ID(), ActorUID: uid, ConnectionID: "local", Operation: operation, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour), InputDigest: inputDigest, ResourceIDs: []string{}, Before: map[string]string{}, Steps: []domain.Step{}, Acknowledgements: []string{}, Risks: []string{}}
	p.Digest, err = operations.PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SavePlan(p, input); err != nil {
		t.Fatal(err)
	}
	job, err := db.Accept(p, domain.ID(), p.Digest)
	if err != nil {
		t.Fatal(err)
	}
	job.State = state
	if err := db.Update(job, "Synthetic journal fixture; no image conversion or guest execution."); err != nil {
		t.Fatal(err)
	}
	if artifactPresent {
		artifact := Artifact{APIVersion: domain.APIVersion, Kind: kind, PlanID: p.ID, OperationID: job.ID, InputDigest: p.InputDigest, System: importer.System{Name: name, ID: systemID}, Disks: []PreparedDisk{}, VMDefined: false, GuestBootVerified: false}
		if err := db.Put("import-artifact", p.ID, artifact); err != nil {
			t.Fatal(err)
		}
	}
	return job, destination
}

func sourceJournalHash(t *testing.T, db *store.Store) string {
	t.Helper()
	// Hash exact persisted rows, including plan bodies/inputs and job bodies.
	// Every query is fixed test code; no application input becomes SQL.
	queries := []string{
		"SELECT id,digest,body,input FROM plans ORDER BY id",
		"SELECT id,plan_id,body FROM jobs ORDER BY id",
		"SELECT kind,id,body FROM metadata ORDER BY kind,id",
		"SELECT job_id,seq,body FROM events ORDER BY job_id,seq",
		"SELECT key,request_digest,job_id,created_at FROM dedup ORDER BY key",
		"SELECT resource,job_id FROM locks ORDER BY resource",
	}
	h := sha256.New()
	encoder := json.NewEncoder(h)
	for _, query := range queries {
		rows, err := db.DB.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		for rows.Next() {
			cells := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range cells {
				pointers[i] = &cells[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if err := encoder.Encode(cells); err != nil {
				rows.Close()
				t.Fatal(err)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestImportSourcesListsOnlyCompletedOwnedPreparationObservations(t *testing.T) {
	s, db := sourcesFixture(t)
	ova, ovaDirectory := sourceJournalEntry(t, db, 1000, "import.prepare", "succeeded", "PreparedImport", "Named appliance", "system-a", true)
	disks, disksDirectory := sourceJournalEntry(t, db, 1000, "import.prepare-disks", "succeeded", "PreparedDiskSet", "", "disk-system", true)
	iso, isoDirectory := sourceJournalEntry(t, db, 1000, "import.prepare-install", "succeeded", "PreparedInstallation", "", "", true)
	other, otherDirectory := sourceJournalEntry(t, db, 1001, "import.prepare", "succeeded", "PreparedImport", "Private appliance", "private", true)
	// No metadata is needed for ineligible jobs, and none should be fetched.
	for _, state := range []string{"queued", "running", "verifying", "failed", "canceled", "recovery-required"} {
		sourceJournalEntry(t, db, 1000, "import.prepare", state, "", "", "", false)
	}
	sourceJournalEntry(t, db, 1000, "vm.create", "succeeded", "", "", "", false)
	before := sourceJournalHash(t, db)
	for _, test := range []struct {
		uid      uint32
		expected map[string]map[string]any
	}{
		{1000, map[string]map[string]any{
			ova.ID:   {"operationID": ova.ID, "name": "Named appliance", "kind": "PreparedImport", "destination": ovaDirectory},
			disks.ID: {"operationID": disks.ID, "name": "disk-system", "kind": "PreparedDiskSet", "destination": disksDirectory},
			iso.ID:   {"operationID": iso.ID, "name": filepath.Base(isoDirectory), "kind": "PreparedInstallation", "destination": isoDirectory},
		}},
		{1001, map[string]map[string]any{other.ID: {"operationID": other.ID, "name": "Private appliance", "kind": "PreparedImport", "destination": otherDirectory}}},
		{1002, map[string]map[string]any{}},
	} {
		response := s.Call(context.Background(), test.uid, "import.sources", app.Request{})
		if response.Error != nil {
			t.Fatal(response.Error)
		}
		rows, ok := response.Data.([]map[string]any)
		if !ok || rows == nil {
			t.Fatalf("expected a nonnil observation array: %#v", response.Data)
		}
		actual := map[string]map[string]any{}
		for _, row := range rows {
			id, ok := row["operationID"].(string)
			if !ok || actual[id] != nil {
				t.Fatalf("invalid duplicate source: %#v", row)
			}
			actual[id] = row
		}
		if !reflect.DeepEqual(actual, test.expected) {
			t.Fatalf("owner %d: got %#v want %#v", test.uid, actual, test.expected)
		}
	}
	if after := sourceJournalHash(t, db); before != after {
		t.Fatalf("listing changed persisted journal: %s -> %s", before, after)
	}
	t.Log("SQLite journal observations only; synthetic receipts do not certify converted images or guest behavior.")
}

func TestImportSourcesRejectsUnexpectedInputsWithoutJournalChanges(t *testing.T) {
	s, db := sourcesFixture(t)
	sourceJournalEntry(t, db, 1000, "import.prepare-install", "succeeded", "PreparedInstallation", "fixture", "", true)
	before := sourceJournalHash(t, db)
	for _, request := range []app.Request{{ID: "id"}, {Path: "/tmp/source"}, {Action: "prepare"}, {After: 1}, {After: -1}, {Input: map[string]any{"machine": "q35"}}, {Apply: &operations.ApplyRequest{}}} {
		response := s.Call(context.Background(), 1000, "import.sources", request)
		if response.Error == nil || response.Error.Code != "INVALID_INPUT" || response.Data != nil {
			t.Fatalf("unexpected input accepted: %+v", response)
		}
	}
	if after := sourceJournalHash(t, db); before != after {
		t.Fatal("invalid request changed journal")
	}
}

func TestImportSourcesCancellationEmptyAndPopulatedJournal(t *testing.T) {
	for _, populated := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "populated"}[populated], func(t *testing.T) {
			s, db := sourcesFixture(t)
			if populated {
				sourceJournalEntry(t, db, 1000, "import.prepare", "succeeded", "PreparedImport", "fixture", "", true)
			}
			before := sourceJournalHash(t, db)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			response := s.Call(ctx, 1000, "import.sources", app.Request{})
			if response.Error == nil || response.Data != nil {
				t.Fatalf("canceled source listing returned success: %+v", response)
			}
			if after := sourceJournalHash(t, db); before != after {
				t.Fatal("canceled request changed journal")
			}
		})
	}
}

func TestImportSourcesMissingReceiptFailsClearly(t *testing.T) {
	s, db := sourcesFixture(t)
	sourceJournalEntry(t, db, 1000, "import.prepare", "succeeded", "", "", "", false)
	before := sourceJournalHash(t, db)
	response := s.Call(context.Background(), 1000, "import.sources", app.Request{})
	if response.Error == nil || response.Error.Message == "" || response.Data != nil {
		t.Fatalf("missing receipt fabricated an available source: %+v", response)
	}
	if after := sourceJournalHash(t, db); before != after {
		t.Fatal("missing receipt lookup changed journal")
	}
}
