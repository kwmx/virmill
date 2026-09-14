// Package store owns durable schema migrations and transactional job persistence.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"net/url"
	"os"
	"path/filepath"
	"time"
	"virmill.local/core/internal/domain"
)

type Store struct {
	DB   *sql.DB
	lock *os.File
}

const schema = `CREATE TABLE plans(id TEXT PRIMARY KEY,digest TEXT NOT NULL,body BLOB NOT NULL,input BLOB NOT NULL);
CREATE TABLE jobs(id TEXT PRIMARY KEY,plan_id TEXT NOT NULL REFERENCES plans(id),body BLOB NOT NULL);
CREATE TABLE dedup(key TEXT PRIMARY KEY,request_digest TEXT NOT NULL,job_id TEXT NOT NULL REFERENCES jobs(id),created_at TEXT NOT NULL);
CREATE TABLE locks(resource TEXT PRIMARY KEY,job_id TEXT NOT NULL REFERENCES jobs(id));
CREATE TABLE events(job_id TEXT NOT NULL REFERENCES jobs(id),seq INTEGER NOT NULL,body BLOB NOT NULL,PRIMARY KEY(job_id,seq));
CREATE TABLE metadata(kind TEXT NOT NULL,id TEXT NOT NULL,body BLOB NOT NULL,PRIMARY KEY(kind,id));
PRAGMA user_version=3;`

func Open(filename string) (*Store, error) {
	dir := filepath.Dir(filename)
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	st, e := os.Lstat(dir)
	if e != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 {
		return nil, errors.New("journal directory must be private mode 0700")
	}
	lock, e := lockDatabase(filename)
	if e != nil {
		return nil, e
	}
	accepted := false
	defer func() {
		if !accepted {
			lock.Close()
		}
	}()
	if st, e = os.Lstat(filename); e == nil {
		if !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
			return nil, errors.New("journal must be private regular file")
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	f, e := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	u := url.URL{Scheme: "file", Path: filename}
	db, e := sql.Open("sqlite3", u.String()+"?_journal_mode=WAL&_synchronous=FULL&_foreign_keys=on&_busy_timeout=5000")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db, lock: lock}
	var v int
	if e = db.QueryRow("PRAGMA user_version").Scan(&v); e != nil {
		db.Close()
		return nil, e
	}
	if v > 3 {
		db.Close()
		return nil, errors.New("database schema newer than application; use compatible binary")
	}
	if v == 0 {
		tx, e := db.Begin()
		if e != nil {
			db.Close()
			return nil, e
		}
		if _, e = tx.Exec(schema); e != nil {
			tx.Rollback()
			db.Close()
			return nil, e
		}
		if e = tx.Commit(); e != nil {
			db.Close()
			return nil, e
		}
	}
	if v == 1 || v == 2 {
		// Version 3 also makes creation disposition and retention pins mandatory.
		// Earlier binaries must not resume a closed recipe or ignore its pins.
		// This includes version 2's inherited recovery-lock semantics.
		backup := filename + ".pre-v3-" + domain.ID() + ".db"
		if e = s.Backup(backup); e != nil {
			db.Close()
			return nil, fmt.Errorf("pre-migration backup: %w", e)
		}
		if _, e = db.Exec("PRAGMA user_version=3"); e != nil {
			db.Close()
			return nil, e
		}
	}
	accepted = true
	return s, nil
}
func (s *Store) Close() error {
	e := s.DB.Close()
	if s.lock != nil {
		if closeErr := s.lock.Close(); e == nil {
			e = closeErr
		}
	}
	return e
}
func (s *Store) SavePlan(p domain.Plan, input []byte) error {
	body, e := json.Marshal(p)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO plans(id,digest,body,input) VALUES(?,?,?,?)", p.ID, p.Digest, body, input)
	return e
}
func (s *Store) Plan(id string) (domain.Plan, []byte, error) {
	var p domain.Plan
	var body, input []byte
	e := s.DB.QueryRow("SELECT body,input FROM plans WHERE id=?", id).Scan(&body, &input)
	if e != nil {
		return p, nil, e
	}
	e = json.Unmarshal(body, &p)
	return p, input, e
}
func (s *Store) Dedup(key, digest string) (domain.Job, bool, error) {
	var id, old string
	e := s.DB.QueryRow("SELECT job_id,request_digest FROM dedup WHERE key=?", key).Scan(&id, &old)
	if e == sql.ErrNoRows {
		return domain.Job{}, false, nil
	}
	if e != nil {
		return domain.Job{}, false, e
	}
	if old != digest {
		return domain.Job{}, false, domain.Fail("IDEMPOTENCY_CONFLICT", "key was already used with different inputs")
	}
	j, e := s.Job(id)
	return j, true, e
}
func (s *Store) Accept(p domain.Plan, key, digest string) (domain.Job, error) {
	j := domain.Job{ID: domain.ID(), PlanID: p.ID, State: "queued", CreatedAt: time.Now().UTC()}
	body, e := json.Marshal(j)
	if e != nil {
		return j, e
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return j, e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("INSERT INTO jobs(id,plan_id,body) VALUES(?,?,?)", j.ID, p.ID, body); e != nil {
		return j, e
	}
	for _, r := range p.ResourceIDs {
		if _, e = tx.Exec("INSERT INTO locks(resource,job_id) VALUES(?,?)", r, j.ID); e != nil {
			return j, domain.Fail("RESOURCE_BUSY", "a job already holds resource "+r)
		}
	}
	if _, e = tx.Exec("INSERT INTO dedup(key,request_digest,job_id,created_at) VALUES(?,?,?,?)", key, digest, j.ID, j.CreatedAt.Format(time.RFC3339)); e != nil {
		return j, e
	}
	return j, tx.Commit()
}
func (s *Store) Job(id string) (domain.Job, error) {
	var b []byte
	var j domain.Job
	e := s.DB.QueryRow("SELECT body FROM jobs WHERE id=?", id).Scan(&b)
	if e != nil {
		return j, e
	}
	e = json.Unmarshal(b, &j)
	return j, e
}

// jobSummaryQuery reads plan fields with json_extract so large plan reviews are
// not decoded for job lists.
const jobSummaryQuery = `SELECT j.body, json_extract(p.body,'$.operation'), json_extract(p.body,'$.resourceIDs'),
	json_extract(p.body,'$.review.vmName') FROM jobs j LEFT JOIN plans p ON p.id=j.plan_id`

func scanJobSummary(scan func(...any) error) (domain.JobSummary, error) {
	var out domain.JobSummary
	var body []byte
	var operation, resources, name sql.NullString
	if e := scan(&body, &operation, &resources, &name); e != nil {
		return out, e
	}
	if e := json.Unmarshal(body, &out.Job); e != nil {
		return out, e
	}
	out.Operation, out.TargetName = operation.String, name.String
	if resources.Valid && json.Unmarshal([]byte(resources.String), &out.ResourceIDs) != nil {
		out.ResourceIDs = nil
	}
	return out, nil
}

// JobSummary is Job plus what its plan changes.
func (s *Store) JobSummary(id string) (domain.JobSummary, error) {
	return scanJobSummary(s.DB.QueryRow(jobSummaryQuery+" WHERE j.id=?", id).Scan)
}

// JobSummaries lists recent jobs, newest first, with what each plan changes.
func (s *Store) JobSummaries() ([]domain.JobSummary, error) {
	rows, e := s.DB.Query(jobSummaryQuery + " ORDER BY j.rowid DESC LIMIT 1000")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.JobSummary{}
	for rows.Next() {
		j, e := scanJobSummary(rows.Scan)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) Jobs() ([]domain.Job, error) {
	rows, e := s.DB.Query("SELECT body FROM jobs ORDER BY rowid DESC LIMIT 1000")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.Job{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		var j domain.Job
		if e = json.Unmarshal(b, &j); e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Update and its event are committed together; intent is durable before the caller executes an effect.
func (s *Store) Update(j domain.Job, message string) error {
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var previous []byte
	if e = tx.QueryRow("SELECT body FROM jobs WHERE id=?", j.ID).Scan(&previous); e != nil {
		return e
	}
	var old domain.Job
	if e = json.Unmarshal(previous, &old); e != nil {
		return e
	}
	j.CancelRequested = j.CancelRequested || old.CancelRequested
	b, e := json.Marshal(j)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE jobs SET body=? WHERE id=?", b, j.ID); e != nil {
		return e
	}
	var seq int64
	if e = tx.QueryRow("SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE job_id=?", j.ID).Scan(&seq); e != nil {
		return e
	}
	ev := domain.Event{APIVersion: domain.APIVersion, OperationID: j.ID, Seq: seq, At: time.Now().UTC(), Phase: j.State, Severity: "info", Message: message}
	if j.Error != nil {
		ev.Severity = "error"
	}
	b, e = json.Marshal(ev)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO events(job_id,seq,body) VALUES(?,?,?)", j.ID, seq, b); e != nil {
		return e
	}
	if domain.Terminal(j.State) && j.State != "recovery-required" {
		if _, e = tx.Exec("DELETE FROM locks WHERE job_id=?", j.ID); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) Events(id string, after int64) ([]domain.Event, error) {
	if after < 0 {
		return nil, errors.New("negative cursor")
	}
	rows, e := s.DB.Query("SELECT body FROM events WHERE job_id=? AND seq>? ORDER BY seq LIMIT 1000", id, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.Event{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		var ev domain.Event
		if e = json.Unmarshal(b, &ev); e != nil {
			return nil, e
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
func (s *Store) Put(kind, id string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.DB.Exec("INSERT INTO metadata(kind,id,body) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body", kind, id, b)
	return e
}
func (s *Store) Get(kind, id string, v any) error {
	var b []byte
	e := s.DB.QueryRow("SELECT body FROM metadata WHERE kind=? AND id=?", kind, id).Scan(&b)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func (s *Store) Backup(destination string) error {
	if _, e := os.Lstat(destination); !os.IsNotExist(e) {
		return errors.New("backup destination must not exist")
	}
	_, e := s.DB.Exec("VACUUM INTO ?", destination)
	if e != nil {
		return fmt.Errorf("consistent database backup: %w", e)
	}
	if e = os.Chmod(destination, 0600); e != nil {
		return e
	}
	f, e := os.Open(destination)
	if e != nil {
		return e
	}
	e = f.Sync()
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	dir, e := os.Open(filepath.Dir(destination))
	if e != nil {
		return e
	}
	e = dir.Sync()
	closeErr = dir.Close()
	if e != nil {
		return e
	}
	return closeErr
}
