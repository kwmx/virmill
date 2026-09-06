package store

import (
	"encoding/json"
	"virmill.local/core/internal/domain"
)

// PendingJobs is separate from the bounded recent-jobs UI query. Recovery must
// include old unfinished operations even after many newer jobs.
func (s *Store) PendingJobs() ([]domain.Job, error) {
	rows, err := s.DB.Query("SELECT body FROM jobs WHERE json_extract(body,'$.state') NOT IN ('succeeded','failed','partial','canceled') ORDER BY rowid")
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
