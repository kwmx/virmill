package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationBackupAndNewerSchemaRefusal(t *testing.T) {
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(dir, "journal.db")
	s, e := Open(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Put("test", "key", map[string]int{"value": 1}); e != nil {
		t.Fatal(e)
	}
	backup := filepath.Join(dir, "backup.db")
	if e = s.Backup(backup); e != nil {
		t.Fatal(e)
	}
	s.Close()
	b, e := Open(backup)
	if e != nil {
		t.Fatal(e)
	}
	var result map[string]int
	if e = b.Get("test", "key", &result); e != nil || result["value"] != 1 {
		t.Fatal(e, result)
	}
	b.DB.Exec("PRAGMA user_version=999")
	b.Close()
	if _, e = Open(backup); e == nil {
		t.Fatal("old binary opened newer schema")
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatal("nonprivate DB")
	}
}
