package store

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"virmill.local/core/internal/domain"
)

func uncertainStoreJob(t *testing.T) (*Store, domain.Plan, domain.Job, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "journal.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	p := domain.Plan{ID: domain.ID(), ActorUID: 1000, ConnectionID: "fixture", ResourceIDs: []string{"fixture-pool", "fixture-vm"}}
	if err = s.SavePlan(p, []byte(`{"fixture":true}`)); err != nil {
		t.Fatal(err)
	}
	j, err := s.Accept(p, "parent-key", "parent-digest")
	if err != nil {
		t.Fatal(err)
	}
	j.State, j.Error = "recovery-required", domain.Fail("RECOVERY_REQUIRED", "fixture uncertain effect")
	if err = s.Update(j, "fixture uncertain effect"); err != nil {
		t.Fatal(err)
	}
	return s, p, j, path
}

func TestRecoveryAcceptanceTransfersAllLocksAndLinksAtomically(t *testing.T) {
	s, parentPlan, parent, _ := uncertainStoreJob(t)
	childPlan := parentPlan
	childPlan.ID = domain.ID()
	if err := s.SavePlan(childPlan, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	child, err := s.AcceptRecovery(childPlan, "child-key", "child-digest", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Job(parent.ID)
	if err != nil || got.State != "partial" || got.RecoveryOperationID != child.ID || child.RecoveryOf != parent.ID || got.Error == nil {
		t.Fatal("recovery links/state lost", got, child, err)
	}
	for _, r := range parentPlan.ResourceIDs {
		owners, err := s.ResourceJobs(r)
		if err != nil || len(owners) != 1 || owners[0] != child.ID {
			t.Fatal("lock gap or wrong owner", owners, err)
		}
	}
	if _, err = s.AcceptRecovery(childPlan, "competitor", "digest", parent.ID); err == nil {
		t.Fatal("second child accepted")
	}
	dedup, found, err := s.Dedup("child-key", "child-digest")
	if err != nil || !found || dedup.ID != child.ID {
		t.Fatal("dedup not committed with transfer", err)
	}
	events, err := s.Events(parent.ID, 0)
	if err != nil || len(events) != 2 || events[1].Phase != "partial" {
		t.Fatal("parent recovery event missing", events, err)
	}
}

func TestRecoveryCannotDropLocksChangeActorOrPartiallyCommit(t *testing.T) {
	for _, failure := range []string{"drop-lock", "actor", "connection", "commit-failure"} {
		t.Run(failure, func(t *testing.T) {
			s, prior, parent, _ := uncertainStoreJob(t)
			p := prior
			p.ID = domain.ID()
			switch failure {
			case "drop-lock":
				p.ResourceIDs = []string{prior.ResourceIDs[0]}
			case "actor":
				p.ActorUID++
			case "connection":
				p.ConnectionID = "another"
			case "commit-failure":
				if _, err := s.DB.Exec(`CREATE TEMP TRIGGER reject_recovery BEFORE UPDATE ON jobs WHEN json_extract(NEW.body,'$.recoveryOperationID') IS NOT NULL BEGIN SELECT RAISE(ABORT,'fixture transaction failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.SavePlan(p, []byte(`{}`)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.AcceptRecovery(p, "child-key", "digest", parent.ID); err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			got, err := s.Job(parent.ID)
			if err != nil || got.State != "recovery-required" || got.RecoveryOperationID != "" {
				t.Fatal("failed transaction altered parent", got, err)
			}
			for _, resource := range prior.ResourceIDs {
				owners, err := s.ResourceJobs(resource)
				if err != nil || len(owners) != 1 || owners[0] != parent.ID {
					t.Fatal("failed transaction lost lock", owners, err)
				}
			}
			if _, found, err := s.Dedup("child-key", "digest"); found || err != nil {
				t.Fatal("failed transaction committed dedup", found, err)
			}
			jobs, err := s.Jobs()
			if err != nil || len(jobs) != 1 {
				t.Fatal("orphan recovery job", jobs, err)
			}
		})
	}
}

func TestVersionOneMigrationPreservesUncertainStateAndPrivateBackup(t *testing.T) {
	s, prior, parent, path := uncertainStoreJob(t)
	if err := s.Put("vm-creation", prior.ID, map[string]any{"schemaVersion": 1, "fixture": true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	migrated, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()
	var version int
	if err = migrated.DB.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal("semantic version barrier missing", version, err)
	}
	got, err := migrated.Job(parent.ID)
	wantBytes, _ := json.Marshal(parent)
	gotBytes, _ := json.Marshal(got)
	if err != nil || string(wantBytes) != string(gotBytes) {
		t.Fatal("old uncertain job changed", got, err)
	}
	for _, resource := range prior.ResourceIDs {
		owners, err := migrated.ResourceJobs(resource)
		if err != nil || len(owners) != 1 || owners[0] != parent.ID {
			t.Fatal("migration dropped retained lock", owners, err)
		}
	}
	backups, err := filepath.Glob(path + ".pre-v2-*.db")
	if err != nil || len(backups) != 1 {
		t.Fatal("consistent pre-migration backup missing", backups, err)
	}
	st, err := os.Stat(backups[0])
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("backup not private", err)
	}
	backup, err := sql.Open("sqlite3", "file:"+backups[0]+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	if err = backup.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatal("backup is not the old version", version, err)
	}
	var check string
	if err = backup.QueryRow("PRAGMA integrity_check").Scan(&check); err != nil || check != "ok" {
		t.Fatal("backup inconsistent", check, err)
	}
	var body []byte
	if err = backup.QueryRow("SELECT body FROM jobs WHERE id=?", parent.ID).Scan(&body); err != nil || string(body) != string(wantBytes) {
		t.Fatal("backup lost original uncertain job", err)
	}
}
