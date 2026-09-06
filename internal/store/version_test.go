package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecordBundledSQLiteVersion(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	s, e := Open(filepath.Join(dir, "version.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var v string
	if e = s.DB.QueryRow("SELECT sqlite_version()").Scan(&v); e != nil {
		t.Fatal(e)
	}
	t.Log("bundled SQLite runtime:", v)
}
