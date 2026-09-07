package store

import (
	"encoding/json"
	"virmill.local/core/internal/domain"
)

// PendingJobs is separate from the bounded recent-jobs UI query. Recovery must
// include old unfinished operations even after many newer jobs.
func (s *Store) PendingJobs() ([]domain.Job, error) {
	return s.jobsForRecovery(false)
}

// StorageReferenceJobs includes every unresolved or partial operation, even
// those older than the UI's recent-job limit. Partial recovery ancestors may
// still describe retained disks; callers exclude only their own exact ancestry.
func (s *Store) StorageReferenceJobs() ([]domain.Job, error) {
	return s.jobsForRecovery(true)
}

func (s *Store) jobsForRecovery(includePartial bool) ([]domain.Job, error) {
	query := "SELECT body FROM jobs WHERE json_extract(body,'$.state') NOT IN ('succeeded','failed','canceled')"
	if !includePartial {
		query += " AND json_extract(body,'$.state')!='partial'"
	}
	rows, err := s.DB.Query(query + " ORDER BY rowid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Job{}
	for rows.Next() {
		var body []byte
		var job domain.Job
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &job); err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (s *Store) ResourceJobs(resource string) ([]string, error) {
	rows, err := s.DB.Query("SELECT job_id FROM locks WHERE resource=? ORDER BY job_id", resource)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
