//go:build linux && amd64

package coldstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

// Per-call durability dependencies let generated-file tests interrupt exact
// boundaries. The public entrypoints always use real fsync and NOREPLACE.
type durability interface {
	Sync(*os.File) error
	Rename(int, string, int, string) error
}
type localDurability struct{}

func (localDurability) Sync(f *os.File) error { return f.Sync() }
func (localDurability) Rename(from int, old string, to int, name string) error {
	return unix.Renameat2(from, old, to, name, unix.RENAME_NOREPLACE)
}

type node struct {
	file      *os.File
	identity  fileidentity.Identity
	name      string
	directory bool
	mode      uint32
}

func openNode(base int, name string, directory bool, mode uint32, absolute bool) (*node, error) {
	resolve := uint64(unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS)
	if !absolute {
		resolve |= unix.RESOLVE_BENEATH | unix.RESOLVE_NO_XDEV
	}
	fd, err := unix.Openat2(base, name, &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC, Resolve: resolve})
	if err != nil {
		return nil, err
	}
	pin := os.NewFile(uintptr(fd), name)
	id, err := inspectOwned(pin, directory, mode)
	if err != nil {
		return nil, errors.Join(err, pin.Close())
	}
	flags := unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC | unix.O_NOCTTY
	if directory {
		flags |= unix.O_DIRECTORY
	}
	readFD, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", fd), flags, 0)
	if err != nil {
		return nil, errors.Join(err, pin.Close())
	}
	f := os.NewFile(uintptr(readFD), name)
	current, err := inspectOwned(f, directory, mode)
	err = errors.Join(err, pin.Close())
	if err != nil || current != id {
		if err == nil {
			err = domain.Fail("SOURCE_CHANGED", "capture inode changed before readable open")
		}
		return nil, errors.Join(err, f.Close())
	}
	return &node{file: f, identity: id, name: name, directory: directory, mode: mode}, nil
}

func inspectOwned(f *os.File, directory bool, mode uint32) (fileidentity.Identity, error) {
	id, err := fileidentity.InspectFile(f, directory)
	if err != nil {
		return id, err
	}
	var st unix.Stat_t
	if err = unix.Fstat(int(f.Fd()), &st); err != nil {
		return id, err
	}
	if st.Uid != uint32(os.Geteuid()) || st.Mode&07777 != mode {
		return id, domain.Fail("PERMISSION_DENIED", "capture object requires exact user ownership and private mode")
	}
	return id, nil
}

func openCatalog(ctx context.Context, root string) (*node, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if os.Geteuid() == 0 || os.Getuid() != os.Geteuid() {
		return nil, domain.Fail("PERMISSION_DENIED", "capture storage requires an ordinary unprivileged user")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" || strings.TrimSpace(root) != root || len(root) > 4096 || !utf8.ValidString(root) {
		return nil, invalid("canonical absolute private capture catalog required")
	}
	for _, r := range root {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return nil, invalid("control character in capture catalog path")
		}
	}
	return openNode(unix.AT_FDCWD, root, true, 0700, true)
}

// Catalog metadata changes while adding a set; its generation, ownership,
// permissions and absolute path binding must still be the original ones.
func recheckCatalog(ctx context.Context, catalog *node) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id, err := inspectOwned(catalog.file, true, 0700)
	if err != nil {
		return err
	}
	current, err := openNode(unix.AT_FDCWD, catalog.name, true, 0700, true)
	if err != nil {
		return err
	}
	err = current.file.Close()
	if id.Generation != catalog.identity.Generation || current.identity.Generation != catalog.identity.Generation {
		return errors.Join(domain.Fail("SOURCE_CHANGED", "capture catalog path or generation changed"), err)
	}
	return errors.Join(ctx.Err(), err)
}

func absent(fd int, name string) error {
	var st unix.Stat_t
	err := unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	return domain.Fail("RESOURCE_BUSY", "capture ID already has staging or published state; inspect, never replay")
}

func closeNodes(nodes []*node) error {
	var err error
	for i := len(nodes) - 1; i >= 0; i-- {
		err = errors.Join(err, nodes[i].file.Close())
	}
	return err
}

// Publish never replaces or removes a previous set, partial set or source.
func Publish(ctx context.Context, root string, manifest protection.CaptureManifest, sources []Source) (Receipt, error) {
	return publish(ctx, root, manifest, sources, localDurability{})
}

func publish(ctx context.Context, root string, manifest protection.CaptureManifest, sources []Source, disk durability) (out Receipt, err error) {
	receipt, manifestRaw, receiptRaw, ordered, err := prepare(ctx, manifest, sources)
	if err != nil {
		return Receipt{}, err
	}
	catalog, err := openCatalog(ctx, root)
	if err != nil {
		return Receipt{}, err
	}
	defer func() {
		err = errors.Join(err, catalog.file.Close())
		if err != nil {
			out = Receipt{}
		}
	}()
	fd := int(catalog.file.Fd())
	partial := "." + receipt.SnapshotID + ".partial"
	for _, name := range []string{receipt.SnapshotID, partial} {
		if err = absent(fd, name); err != nil {
			return Receipt{}, err
		}
	}
	if err = ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if err = unix.Mkdirat(fd, partial, 0700); err != nil {
		return Receipt{}, err
	}
	// Persist the partial name promptly; all failures retain it for review.
	if err = disk.Sync(catalog.file); err != nil {
		return Receipt{}, err
	}
	stage, err := openNode(fd, partial, true, 0700, false)
	if err != nil {
		return Receipt{}, err
	}
	dirs := []*node{stage}
	defer func() {
		err = errors.Join(err, closeNodes(dirs))
		if err != nil {
			out = Receipt{}
		}
	}()
	tree, _ := expectedTree(receipt.Manifest)
	names := make([]string, 0, len(tree))
	for name := range tree {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names) // Parents sort before their own descendants.
	byName := map[string]*node{"": stage}
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return Receipt{}, err
		}
		parent := path.Dir(name)
		if parent == "." {
			parent = ""
		}
		if err = unix.Mkdirat(int(byName[parent].file.Fd()), path.Base(name), 0700); err != nil {
			return Receipt{}, err
		}
		dir, openErr := openNode(int(stage.file.Fd()), name, true, 0700, false)
		if openErr != nil {
			return Receipt{}, openErr
		}
		dirs, byName[name] = append(dirs, dir), dir
	}
	for _, source := range ordered {
		if err = writeMember(ctx, stage, source.Member.Path, source.Reader, source.Member.Size, source.Member.SHA256, disk); err != nil {
			return Receipt{}, err
		}
	}
	for _, item := range []struct {
		name string
		raw  []byte
	}{{"manifest.json", manifestRaw}, {"receipt.json", receiptRaw}} {
		if err = writeMember(ctx, stage, item.name, bytes.NewReader(item.raw), int64(len(item.raw)), digest(item.raw), disk); err != nil {
			return Receipt{}, err
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err = ctx.Err(); err != nil {
			return Receipt{}, err
		}
		if err = dirs[i].file.Chmod(0500); err != nil {
			return Receipt{}, err
		}
		if err = disk.Sync(dirs[i].file); err != nil {
			return Receipt{}, err
		}
	}
	// Verify the sealed complete tree before it acquires the final name.
	verified, err := inspectSet(ctx, catalog, partial, receipt.SnapshotID, disk)
	if err != nil {
		return Receipt{}, err
	}
	if verified.ManifestSHA256 != receipt.ManifestSHA256 || verified.OperationID != receipt.OperationID {
		return Receipt{}, domain.Fail("SOURCE_CHANGED", "staged capture differs from the requested publication")
	}
	if err = checkSetGeneration(catalog, partial, stage.identity.Generation); err != nil {
		return Receipt{}, err
	}
	if err = recheckCatalog(ctx, catalog); err != nil {
		return Receipt{}, err
	}
	if err = disk.Rename(fd, partial, fd, receipt.SnapshotID); err != nil {
		return Receipt{}, err
	}
	if err = disk.Sync(catalog.file); err != nil {
		return Receipt{}, err
	}
	// A failure here is an uncertain publication, recoverable only by Inspect.
	verified, err = inspectSet(ctx, catalog, receipt.SnapshotID, receipt.SnapshotID, disk)
	if err != nil {
		return Receipt{}, err
	}
	if verified.ManifestSHA256 != receipt.ManifestSHA256 || verified.OperationID != receipt.OperationID {
		return Receipt{}, domain.Fail("SOURCE_CHANGED", "published capture differs from the requested publication")
	}
	if err = checkSetGeneration(catalog, receipt.SnapshotID, stage.identity.Generation); err != nil {
		return Receipt{}, err
	}
	return verified, ctx.Err()
}

func checkSetGeneration(catalog *node, name, generation string) error {
	current, err := openNode(int(catalog.file.Fd()), name, true, 0500, false)
	if err != nil {
		return err
	}
	err = current.file.Close()
	if current.identity.Generation != generation {
		return errors.Join(domain.Fail("SOURCE_CHANGED", "capture set directory was replaced"), err)
	}
	return err
}

func writeMember(ctx context.Context, stage *node, name string, reader io.ReaderAt, size int64, sha string, disk durability) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	fd, err := unix.Openat2(int(stage.file.Fd()), name, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0600, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err = inspectOwned(f, false, 0600); err != nil {
		return err
	}
	if err = copyExact(ctx, f, reader, size, sha); err != nil {
		return err
	}
	if err = f.Chmod(0400); err != nil {
		return err
	}
	if err = disk.Sync(f); err != nil {
		return err
	}
	return ctx.Err()
}

// Inspect verifies and refreshes durability of the existing final set. It never
// copies a source, completes staging, renames a set or repairs damaged metadata.
func Inspect(ctx context.Context, root, snapshotID string) (out Receipt, err error) {
	if err = ctx.Err(); err != nil {
		return Receipt{}, err
	}
	if !validID(snapshotID) {
		return Receipt{}, invalid("canonical nonzero snapshot UUID required")
	}
	catalog, err := openCatalog(ctx, root)
	if err != nil {
		return Receipt{}, err
	}
	defer func() {
		err = errors.Join(err, catalog.file.Close())
		if err != nil {
			out = Receipt{}
		}
	}()
	return inspectSet(ctx, catalog, snapshotID, snapshotID, localDurability{})
}

func readDocument(ctx context.Context, f *node) ([]byte, error) {
	if f.identity.Size == 0 || f.identity.Size > documentLimit {
		return nil, incomplete("capture metadata outside nonempty document bound")
	}
	raw := make([]byte, int(f.identity.Size))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	n, err := f.file.ReadAt(raw, 0)
	if err != nil || n != len(raw) {
		return nil, errors.Join(incomplete("capture metadata size changed"), err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}

func inspectSet(ctx context.Context, catalog *node, name, snapshotID string, disk durability) (out Receipt, err error) {
	if err = ctx.Err(); err != nil {
		return Receipt{}, err
	}
	set, err := openNode(int(catalog.file.Fd()), name, true, 0500, false)
	if err != nil {
		return Receipt{}, err
	}
	nodes := []*node{set}
	defer func() {
		err = errors.Join(err, closeNodes(nodes))
		if err != nil {
			out = Receipt{}
		}
	}()
	byName := map[string]*node{"": set}
	for _, name := range []string{"manifest.json", "receipt.json"} {
		f, e := openNode(int(set.file.Fd()), name, false, 0400, false)
		if e != nil {
			return Receipt{}, e
		}
		nodes, byName[name] = append(nodes, f), f
	}
	raw, err := readDocument(ctx, byName["manifest.json"])
	if err != nil {
		return Receipt{}, err
	}
	manifest, err := protection.DecodeCaptureManifest(raw)
	if err != nil {
		return Receipt{}, err
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(raw, canonical) || manifest.ID != snapshotID {
		return Receipt{}, incomplete("capture manifest encoding or snapshot binding differs")
	}
	receiptRaw, err := readDocument(ctx, byName["receipt.json"])
	if err != nil {
		return Receipt{}, err
	}
	var receipt Receipt
	if err = wire.Decode(receiptRaw, &receipt); err != nil {
		return Receipt{}, incomplete("invalid capture receipt JSON")
	}
	want := Receipt{Version: 1, SnapshotID: manifest.ID, OperationID: manifest.OperationID, ManifestSHA256: digest(raw), Manifest: manifest}
	wantRaw, err := json.Marshal(want)
	if err != nil || !bytes.Equal(receiptRaw, wantRaw) {
		return Receipt{}, incomplete("capture receipt differs from the exact manifest and operation")
	}
	tree, err := expectedTree(manifest)
	if err != nil {
		return Receipt{}, err
	}
	dirNames := []string{""}
	for name := range tree {
		if name != "" {
			dirNames = append(dirNames, name)
		}
	}
	sort.Strings(dirNames)
	for _, name := range dirNames {
		if err = ctx.Err(); err != nil {
			return Receipt{}, err
		}
		if name != "" {
			dir, e := openNode(int(set.file.Fd()), name, true, 0500, false)
			if e != nil {
				return Receipt{}, e
			}
			nodes, byName[name] = append(nodes, dir), dir
		}
		entries, e := byName[name].file.ReadDir(len(tree[name]) + 1)
		if e != nil && e != io.EOF {
			return Receipt{}, e
		}
		if len(entries) != len(tree[name]) {
			return Receipt{}, incomplete("capture directory has missing or extra entries")
		}
		for _, entry := range entries {
			if _, ok := tree[name][entry.Name()]; !ok {
				return Receipt{}, incomplete("capture set contains an undeclared entry")
			}
		}
	}
	// Retain every member before reading, then recheck every held path after all
	// verification and syncs. Special files are rejected through O_PATH first.
	for _, member := range manifest.Members {
		if err = ctx.Err(); err != nil {
			return Receipt{}, err
		}
		f, e := openNode(int(set.file.Fd()), member.Path, false, 0400, false)
		if e != nil {
			return Receipt{}, e
		}
		nodes, byName[member.Path] = append(nodes, f), f
		if f.identity.Size != uint64(member.Size) {
			return Receipt{}, incomplete("capture member size differs")
		}
	}
	for _, member := range manifest.Members {
		if err = copyExact(ctx, io.Discard, byName[member.Path].file, member.Size, member.SHA256); err != nil {
			return Receipt{}, err
		}
	}
	for _, f := range nodes {
		if !f.directory {
			if err = ctx.Err(); err != nil {
				return Receipt{}, err
			}
			if err = disk.Sync(f.file); err != nil {
				return Receipt{}, err
			}
		}
	}
	for i := len(dirNames) - 1; i >= 0; i-- {
		if err = ctx.Err(); err != nil {
			return Receipt{}, err
		}
		if err = disk.Sync(byName[dirNames[i]].file); err != nil {
			return Receipt{}, err
		}
	}
	if err = disk.Sync(catalog.file); err != nil {
		return Receipt{}, err
	}
	for _, f := range nodes {
		if err = ctx.Err(); err != nil {
			return Receipt{}, err
		}
		currentID, e := inspectOwned(f.file, f.directory, f.mode)
		if e != nil {
			return Receipt{}, e
		}
		base, relative := int(set.file.Fd()), f.name
		if f == set {
			base, relative = int(catalog.file.Fd()), name
		}
		current, e := openNode(base, relative, f.directory, f.mode, false)
		if e != nil {
			return Receipt{}, e
		}
		if e = current.file.Close(); e != nil {
			return Receipt{}, e
		}
		if currentID != f.identity || current.identity != f.identity {
			return Receipt{}, domain.Fail("SOURCE_CHANGED", "capture member or directory changed during verification")
		}
	}
	if err = recheckCatalog(ctx, catalog); err != nil {
		return Receipt{}, err
	}
	return want, ctx.Err()
}
