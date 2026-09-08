//go:build linux && amd64

package restic

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
)

func localPath(p string) bool {
	if len(p) > 4096 || !utf8.ValidString(p) || p == "/" || !filepath.IsAbs(p) || filepath.Clean(p) != p || strings.TrimSpace(p) != p || strings.ContainsRune(p, '\\') {
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

type pinned struct {
	f         *os.File
	path      string
	id        fileidentity.Identity
	directory bool
	parent    *pinned
}

func (p *pinned) close() {
	if p != nil {
		p.f.Close()
		p.parent.close()
	}
}
func (p *pinned) recheck(full bool) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(p.f.Fd()), &st); err != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 || st.Mode&07000 != 0 {
		return domain.Fail("SOURCE_CHANGED", "restic input private ownership or permissions changed")
	}
	current, err := fileidentity.InspectFile(p.f, p.directory)
	if err != nil {
		return domain.Fail("SOURCE_CHANGED", "held restic input identity is unavailable")
	}
	observed, err := fileidentity.Observe(p.path, p.directory)
	if err != nil || observed.Generation != p.id.Generation || current.Generation != p.id.Generation || (full && (observed != p.id || current != p.id)) {
		return domain.Fail("SOURCE_CHANGED", "restic input generation or path binding changed")
	}
	if p.parent != nil {
		return p.parent.recheck(false)
	}
	return nil
}

func openPrivate(path string, directory, writable bool) (*pinned, error) {
	if os.Geteuid() == 0 {
		return nil, domain.Fail("PERMISSION_DENIED", "restic adapter requires an ordinary user")
	}
	if !localPath(path) {
		return nil, domain.Fail("INVALID_INPUT", "canonical absolute local nonroot path required")
	}
	f, id, err := fileidentity.Open(path, directory, false)
	if err != nil {
		return nil, domain.Fail("PERMISSION_DENIED", "restic path must exist without symlinks or special files")
	}
	p := &pinned{f: f, path: path, id: id, directory: directory}
	var st unix.Stat_t
	err = unix.Fstat(int(f.Fd()), &st)
	mode := uint32(0600)
	if directory {
		mode = 0500
		if writable {
			mode = 0700
		}
	}
	if err != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 || st.Mode&07000 != 0 || st.Mode&mode != mode || (!directory && (st.Mode&0777 != 0600 || id.Size == 0 || id.Size > maxPassword)) {
		p.close()
		return nil, domain.Fail("PERMISSION_DENIED", "restic input ownership, private permissions or password bounds refused")
	}
	if !directory {
		// Type/generation were checked with O_PATH before any readable open.
		r, err := os.OpenFile(fmt.Sprintf("/proc/self/fd/%d", f.Fd()), os.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		if err != nil {
			p.close()
			return nil, domain.Fail("PERMISSION_DENIED", "held password descriptor unavailable")
		}
		f.Close()
		p.f = r
		if err = p.recheck(true); err != nil {
			p.close()
			return nil, err
		}
	}
	return p, nil
}

func makePrivate(path string) (*pinned, error) {
	if !localPath(path) {
		return nil, domain.Fail("INVALID_INPUT", "new canonical local destination required")
	}
	parent, err := openPrivate(filepath.Dir(path), true, true)
	if err != nil {
		return nil, err
	}
	if err = unix.Mkdirat(int(parent.f.Fd()), filepath.Base(path), 0700); err != nil {
		parent.close()
		return nil, domain.Fail("CONFLICT", "restic destination must be new; retain and inspect any previous attempt")
	}
	p, err := openPrivate(path, true, true)
	if err != nil {
		parent.close()
		return nil, err
	}
	p.parent = parent
	if err = p.recheck(false); err != nil {
		p.close()
		return nil, err
	}
	return p, nil
}

// Walk through held descriptors. No directory entry is followed for bytes;
// callers compare source metadata again after backup. This is not confinement
// against another malicious process running as this same ordinary user.
func scan(ctx context.Context, p *pinned) (map[string]fileidentity.Identity, error) {
	out := map[string]fileidentity.Identity{}
	var visit func(*os.File, string, int) error
	visit = func(dir *os.File, base string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 {
			return domain.Fail("INVALID_INPUT", "restic tree depth exceeds bound")
		}
		fd, err := unix.Openat(int(dir.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			return domain.Fail("PERMISSION_DENIED", "restic tree is unreadable")
		}
		read := os.NewFile(uintptr(fd), "restic-directory")
		defer read.Close()
		for {
			entries, err := read.ReadDir(128)
			for _, entry := range entries {
				if len(out) >= 100000 {
					return domain.Fail("INVALID_INPUT", "restic tree entry count exceeds bound")
				}
				rel := entry.Name()
				if base != "" {
					rel = base + "/" + rel
				}
				if !localPath("/" + rel) {
					return domain.Fail("PERMISSION_DENIED", "restic tree contains an unsafe name")
				}
				fd, e := unix.Openat2(int(dir.Fd()), entry.Name(), &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
				if e != nil {
					return domain.Fail("PERMISSION_DENIED", "restic tree contains a replaced or symbolic entry")
				}
				child := os.NewFile(uintptr(fd), "restic-entry")
				var st unix.Stat_t
				e = unix.Fstat(fd, &st)
				isDir := st.Mode&unix.S_IFMT == unix.S_IFDIR
				if e != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0022 != 0 || st.Mode&07000 != 0 {
					child.Close()
					return domain.Fail("PERMISSION_DENIED", "restic tree entry ownership or write permissions refused")
				}
				id, e := fileidentity.InspectFile(child, isDir)
				if e == nil {
					out[rel] = id
					if isDir {
						e = visit(child, rel, depth+1)
					}
				}
				child.Close()
				if e != nil {
					return domain.Fail("PERMISSION_DENIED", "restic tree requires bounded ordinary directories and single-link files")
				}
			}
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return domain.Fail("PERMISSION_DENIED", "restic directory listing failed")
			}
		}
	}
	if err := visit(p.f, "", 0); err != nil {
		return nil, err
	}
	return out, nil
}
func sameTree(a, b map[string]fileidentity.Identity) bool { return reflect.DeepEqual(a, b) }
