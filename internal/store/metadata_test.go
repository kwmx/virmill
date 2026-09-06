package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMetadataCompareAndReopenMigration(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "journal.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ComparePut("fixture", "one", nil, map[string]any{"schemaVersion": 1, "data": "original"}); err != nil {
		t.Fatal(err)
	}
	old, err := s.MetadataBytes("fixture", "one")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.ComparePut("fixture", "one", nil, map[string]any{"data": "overwrite"}); err == nil {
		t.Fatal("existing metadata clobbered")
	}
	if err = s.ComparePut("fixture", "one", old, map[string]any{"schemaVersion": 1, "data": "updated"}); err != nil {
		t.Fatal(err)
	}
	if err = s.ComparePut("fixture", "one", old, map[string]any{"data": "stale overwrite"}); err == nil {
		t.Fatal("stale metadata accepted")
	}
	rows, err := s.Metadata("fixture")
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
}
