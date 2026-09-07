package store

import (
	"encoding/json"
	"time"
	"virmill.local/core/internal/domain"
)

// AcceptRecovery transfers every lock and links the parent and child in one WAL
// transaction. The parent becomes partial, never successful. Any rollback leaves
// its original state and all locks unchanged.
func (s *Store) AcceptRecovery(p domain.Plan, key, digest, parentID string) (domain.Job, error) {
	child := domain.Job{ID: domain.ID(), PlanID: p.ID, State: "queued", CreatedAt: time.Now().UTC(), RecoveryOf: parentID}
	tx, err := s.DB.Begin()
	if err != nil {
		return child, err
	}
	defer tx.Rollback()
	var parentBody, planBody []byte
	if err = tx.QueryRow("SELECT j.body,p.body FROM jobs j JOIN plans p ON p.id=j.plan_id WHERE j.id=?", parentID).Scan(&parentBody, &planBody); err != nil {
		return child, err
	}
	var parent domain.Job
	var prior domain.Plan
	if err = json.Unmarshal(parentBody, &parent); err != nil {
		return child, err
	}
	if err = json.Unmarshal(planBody, &prior); err != nil {
		return child, err
	}
	if (parent.State != "recovery-required" && parent.State != "interrupted") || parent.RecoveryOperationID != "" || prior.ActorUID != p.ActorUID || prior.ConnectionID != p.ConnectionID {
		return child, domain.Fail("STALE_PLAN", "recovery parent is not the actor's unresolved job on this connection")
	}
	resources := map[string]bool{}
	for _, r := range p.ResourceIDs {
		resources[r] = true
	}
	for _, r := range prior.ResourceIDs {
		if !resources[r] {
			return child, domain.Fail("INVALID_INPUT", "recovery must retain every original resource lock")
		}
		var owner string
		if err = tx.QueryRow("SELECT job_id FROM locks WHERE resource=?", r).Scan(&owner); err != nil {
			return child, err
		}
		if owner != parentID {
			return child, domain.Fail("RESOURCE_BUSY", "recovery parent no longer holds all its locks")
		}
	}
	// Defend against a corrupt/incomplete persisted plan dropping an owned lock.
	rows, err := tx.Query("SELECT resource FROM locks WHERE job_id=?", parentID)
	if err != nil {
		return child, err
	}
	owned := map[string]bool{}
	for rows.Next() {
		var r string
		if err = rows.Scan(&r); err != nil {
			rows.Close()
			return child, err
		}
		if !resources[r] {
			rows.Close()
			return child, domain.Fail("INVALID_INPUT", "recovery omitted a held resource")
		}
		owned[r] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return child, err
	}
	body, err := json.Marshal(child)
	if err != nil {
		return child, err
	}
	if _, err = tx.Exec("INSERT INTO jobs(id,plan_id,body) VALUES(?,?,?)", child.ID, p.ID, body); err != nil {
		return child, err
	}
	for r := range resources {
		if owned[r] {
			_, err = tx.Exec("UPDATE locks SET job_id=? WHERE resource=? AND job_id=?", child.ID, r, parentID)
		} else {
			_, err = tx.Exec("INSERT INTO locks(resource,job_id) VALUES(?,?)", r, child.ID)
		}
		if err != nil {
			return child, domain.Fail("RESOURCE_BUSY", "recovery cannot acquire complete resource set")
		}
	}
	parent.State, parent.RecoveryOperationID = "partial", child.ID
	body, err = json.Marshal(parent)
	if err != nil {
		return child, err
	}
	if _, err = tx.Exec("UPDATE jobs SET body=? WHERE id=?", body, parent.ID); err != nil {
		return child, err
	}
	var seq int64
	if err = tx.QueryRow("SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE job_id=?", parent.ID).Scan(&seq); err != nil {
		return child, err
	}
	event := domain.Event{APIVersion: domain.APIVersion, OperationID: parent.ID, Seq: seq, At: time.Now().UTC(), Phase: "partial", Severity: "warning", Message: "Reviewed recovery accepted; resource locks transferred atomically to operation " + child.ID + "; original operation remains partial"}
	body, err = json.Marshal(event)
	if err != nil {
		return child, err
	}
	if _, err = tx.Exec("INSERT INTO events(job_id,seq,body) VALUES(?,?,?)", parent.ID, seq, body); err != nil {
		return child, err
	}
	if _, err = tx.Exec("INSERT INTO dedup(key,request_digest,job_id,created_at) VALUES(?,?,?,?)", key, digest, child.ID, child.CreatedAt.Format(time.RFC3339)); err != nil {
		return child, err
	}
	return child, tx.Commit()
}
