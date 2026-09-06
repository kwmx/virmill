package store

import (
	"database/sql"
	"encoding/json"
	"virmill.local/core/internal/domain"
)

// Metadata lists versioned application records without exposing the database to plugins.
func (s *Store) Metadata(kind string) ([]json.RawMessage, error) {
	rows, err := s.DB.Query("SELECT body FROM metadata WHERE kind=? ORDER BY id", kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var body []byte
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(body))
	}
	return out, rows.Err()
}

// ComparePut changes a record only if its exact previously observed bytes still match.
// A nil previous value means the record must be absent. This also protects callers
// which perform filesystem staging between preview and the metadata commit.
func (s *Store) ComparePut(kind, id string, previous []byte, next any) error {
	return s.ComparePutWithResourceJobs(kind, id, previous, next, nil)
}

// ComparePutWithResourceJobs also checks active-operation disposition atomically
// with the metadata change. Newly accepted invocations cannot escape the review
// by appearing between validation and the activation transaction.
func (s *Store) ComparePutWithResourceJobs(kind, id string, previous []byte, next any, reviewed map[string][]string) error {
	body, err := json.Marshal(next)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old []byte
	err = tx.QueryRow("SELECT body FROM metadata WHERE kind=? AND id=?", kind, id).Scan(&old)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if (err == sql.ErrNoRows) != (previous == nil) || string(old) != string(previous) {
		return domain.Fail("STALE_PLAN", "application record changed after preview")
	}
	for resource, approved := range reviewed {
		rows, err := tx.Query("SELECT job_id FROM locks WHERE resource=?", resource)
		if err != nil {
			return err
		}
		for rows.Next() {
			var job string
			if err = rows.Scan(&job); err != nil {
				rows.Close()
				return err
			}
			found := false
			for _, id := range approved {
				if id == job {
					found = true
				}
			}
			if !found {
				rows.Close()
				return domain.Fail("STALE_PLAN", "new active operation requires a fresh disposition review")
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO metadata(kind,id,body) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body", kind, id, body); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) MetadataBytes(kind, id string) ([]byte, error) {
	var body []byte
	err := s.DB.QueryRow("SELECT body FROM metadata WHERE kind=? AND id=?", kind, id).Scan(&body)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return body, err
}
