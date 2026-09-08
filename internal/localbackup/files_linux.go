//go:build linux && amd64

package localbackup

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
)

func localPath(p string) bool {
	if len(p) > 4096 || p == "/" || !filepath.IsAbs(p) || filepath.Clean(p) != p || !utf8.ValidString(p) || strings.TrimSpace(p) != p || strings.ContainsRune(p, '\\') {
		return false
	}
	for _, r := range p {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
func within(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }
func disjoint(a, b string) bool  { return !within(a, b) && !within(b, a) }
func private(path string, directory bool, limit uint64) (fileidentity.Identity, error) {
	if !localPath(path) {
		return fileidentity.Identity{}, invalid("canonical absolute nonroot local path required")
	}
	f, id, err := fileidentity.Open(path, directory, false)
	if err != nil {
		return id, domain.Fail("PERMISSION_DENIED", "local backup path is missing, symbolic or not an ordinary object")
	}
	defer f.Close()
	var st unix.Stat_t
	err = unix.Fstat(int(f.Fd()), &st)
	mode := uint32(0600)
	if directory {
		mode = 0700
	}
	if err != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&07777 != mode || !directory && (id.Size == 0 || id.Size > limit) {
		return fileidentity.Identity{}, domain.Fail("PERMISSION_DENIED", "local backup requires current-user private directory or bounded single-link file")
	}
	return id, nil
}
func absent(path string) error {
	if !localPath(path) {
		return invalid("canonical new local destination required")
	}
	_, err := fileidentity.Observe(path, true)
	if os.IsNotExist(err) {
		return nil
	}
	if err == nil {
		return invalid("destination already exists; existing recovery sets are never overwritten")
	}
	return domain.Fail("PERMISSION_DENIED", "destination path is symbolic, inaccessible or occupied")
}
func observeDirectory(path string, allowAbsent bool) (binding, error) {
	b := binding{Path: path}
	id, err := private(path, true, 0)
	if err == nil {
		b.Exists = true
		b.Identity = id
		return b, nil
	}
	if !allowAbsent {
		return b, err
	}
	if err = absent(path); err != nil {
		return b, err
	}
	parent, err := private(filepath.Dir(path), true, 0)
	if err != nil {
		return b, err
	}
	b.Parent = parent
	return b, nil
}
func observeFile(path string, password bool) (binding, error) {
	limit := uint64(1 << 20)
	if password {
		limit = 4096
	}
	id, err := private(path, false, limit)
	if err != nil {
		return binding{}, err
	}
	parent, err := private(filepath.Dir(path), true, 0)
	if err != nil {
		return binding{}, err
	}
	return binding{Path: path, Exists: true, Identity: id, Parent: parent}, nil
}
func checkFile(b binding, password bool) error {
	fresh, err := observeFile(b.Path, password)
	if err != nil {
		return err
	}
	if !b.Exists || fresh.Identity != b.Identity || fresh.Parent.Generation != b.Parent.Generation {
		return domain.Fail("SOURCE_CHANGED", "repository configuration or credential generation changed")
	}
	return nil
}
func checkDirectory(b binding) error {
	fresh, err := observeDirectory(b.Path, !b.Exists)
	if err != nil {
		return err
	}
	if fresh.Exists != b.Exists || b.Exists && fresh.Identity.Generation != b.Identity.Generation || !b.Exists && fresh.Parent.Generation != b.Parent.Generation {
		return domain.Fail("SOURCE_CHANGED", "reviewed directory generation or absence changed")
	}
	return nil
}
func syncDirectory(path string) error {
	f, _, err := fileidentity.Open(path, true, true)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func createDirectory(path string) error {
	parent, err := private(filepath.Dir(path), true, 0)
	if err != nil {
		return err
	}
	_ = parent
	f, _, err := fileidentity.Open(filepath.Dir(path), true, false)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = unix.Mkdirat(int(f.Fd()), filepath.Base(path), 0700); err != nil {
		return recovery("private staging already exists or creation was not acknowledged; retain it")
	}
	if _, err = private(path, true, 0); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
