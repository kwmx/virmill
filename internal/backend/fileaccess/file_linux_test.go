//go:build linux

package fileaccess

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"virmill.local/core/internal/backend/fileidentity"
)

func TestKernelACLGrantAndAtomicModeOnlyRestoration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated-file")
	content := []byte("generated ACL fixture; no VM or host mutation")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	handle, _, err := fileidentity.Open(path, false, false)
	if err != nil {
		t.Fatal(err)
	}
	original, err := Snapshot(handle)
	handle.Close()
	if err != nil {
		t.Fatal(err)
	}
	actor := uint32(65534)
	if actor == original.UID {
		actor = 65533
	}
	// The coding tool's outer namespace maps only UID 1000. Linux correctly
	// rejects an ACL containing an unmapped UID; distinguish that environment
	// from malformed ACL encoding without swallowing arbitrary syscall errors.
	mapping, e := os.ReadFile("/proc/self/uid_map")
	if e != nil {
		t.Fatal(e)
	}
	mapped := false
	for _, line := range strings.Split(strings.TrimSpace(string(mapping)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			t.Fatal("invalid UID map")
		}
		start, e := strconv.ParseUint(fields[0], 10, 64)
		if e != nil {
			t.Fatal(e)
		}
		count, e := strconv.ParseUint(fields[2], 10, 64)
		if e != nil {
			t.Fatal(e)
		}
		if uint64(actor) >= start && uint64(actor)-start < count {
			mapped = true
		}
	}
	if !mapped {
		t.Skip("BLOCKED: kernel ACL fixture requires a second mapped UID; run the ordinary-user temporary-file test outside this restricted user namespace")
	}
	desired, err := original.DesiredRead(actor, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := fileidentity.Open(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	granted, err := SetACL(f, original, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !MatchesAccess(granted, original, desired) || granted.File.Mode&0777 != 0640 {
		t.Fatal("kernel grant differs from reviewed access", granted)
	}
	restore, err := original.RestoreACL()
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := SetACL(f, granted, restore)
	if err != nil {
		t.Fatal(err)
	}
	if !MatchesAccess(revoked, original, restore) || revoked.ACL != original.ACL || revoked.File.Mode != original.File.Mode {
		t.Fatal("ACL/mode not restored exactly", revoked, original)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != sha256.Sum256(content) {
		t.Fatal("content changed")
	}
	if _, err = SetACL(f, original, desired); err == nil {
		t.Fatal("stale ctime accepted")
	}
}

func TestACLObservationRefusesLinksAndStaleWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	os.WriteFile(path, []byte("fixture"), 0600)
	f, _, err := fileidentity.Open(path, false, true)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before, err := Snapshot(f)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := before.DesiredRead(65534, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Link(path, filepath.Join(dir, "hardlink")); err != nil {
		t.Fatal(err)
	}
	if _, err = Snapshot(f); err == nil {
		t.Fatal("hard-linked file observed as ordinary single generation")
	}
	if _, err = SetACL(f, before, desired); err == nil {
		t.Fatal("hard-linked file changed")
	}
	if err = os.Symlink(path, filepath.Join(dir, "symlink")); err != nil {
		t.Fatal(err)
	}
	if _, _, err = fileidentity.Open(filepath.Join(dir, "symlink"), false, true); err == nil {
		t.Fatal("symlink access accepted")
	}
}
