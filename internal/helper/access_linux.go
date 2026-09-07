//go:build linux && amd64

package helper

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"virmill.local/core/internal/backend/fileaccess"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

type AccessResult struct {
	Version    int              `json:"version"`
	JobID      string           `json:"jobID"`
	Binding    string           `json:"binding"`
	Complete   bool             `json:"complete"`
	State      fileaccess.State `json:"state"`
	DesiredACL string           `json:"desiredACL"`
}
type accessIntent struct {
	Version    int     `json:"version"`
	Binding    string  `json:"binding"`
	Request    Request `json:"request"`
	DesiredACL string  `json:"desiredACL"`
}

// AccessExecutor has no shell, parser, caller-selected syscall or path override.
// The native observer is supplied by the system helper entry point.
type AccessExecutor struct {
	Backend          domain.ManagedFileAccessBackend
	journalDirectory string // test-only private directory override; never read from configuration or requests
}

var managedName = regexp.MustCompile(`^virmill-([a-f0-9-]{36})-(disk-[0-9]{3}\.qcow2|media-[0-9]{3}\.iso)$`)

func accessBinding(r Request) (string, error) {
	// Expiry and signature authenticate this invocation; the immutable operation
	// binding survives a later, freshly signed observation after a crash.
	return operations.Digest(struct {
		Actor                                     uint32
		Operation, Resource, Root, Plan, Job, Key string
		Access                                    *AccessRequest
	}{r.ActorUID, r.Operation, r.ResourceID, r.RootID, r.PlanDigest, r.JobID, r.KeyID, r.Access})
}
func accessBefore(r Request) (fileaccess.State, error) {
	var before fileaccess.State
	if r.Access == nil {
		return before, errors.New("typed access request required")
	}
	err := wire.Decode(r.Access.Before, &before)
	return before, err
}
func accessPath(r Request, p Policy) (string, error) {
	if r.Access == nil {
		return "", errors.New("typed access request required")
	}
	a := r.Access
	root := p.Roots[r.RootID]
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" || a.RelativePath == "." || !filepath.IsLocal(a.RelativePath) || filepath.Clean(a.RelativePath) != a.RelativePath {
		return "", errors.New("canonical approved root and relative volume path required")
	}
	m := managedName.FindStringSubmatch(a.Mapping.VolumeName)
	if len(m) != 3 || m[1] != r.ResourceID || filepath.Base(a.RelativePath) != a.Mapping.VolumeName || filepath.Join(root, a.RelativePath) != a.Mapping.Path {
		return "", errors.New("selected path is not the exact Virmill VM volume")
	}
	if a.Mapping.VolumeKey == "" || a.Mapping.DiskTarget == "" || len(a.Mapping.DiskTarget) > 32 || len(a.Mapping.Path) > 4096 {
		return "", errors.New("bounded native volume identity required")
	}
	for _, fp := range []string{a.Mapping.VMFingerprint, a.Mapping.PoolFingerprint} {
		b, e := hex.DecodeString(fp)
		if e != nil || len(b) != 32 || hex.EncodeToString(b) != fp {
			return "", errors.New("exact native fingerprints required")
		}
	}
	return root, nil
}
func openAccessFile(r Request, p Policy) (*os.File, error) {
	root, err := accessPath(r, p)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, root, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return nil, err
	}
	if st.Uid != 0 || st.Mode&0022 != 0 {
		return nil, errors.New("approved root must be root-owned and administrator-writable only")
	}
	target, err := unix.Openat2(fd, r.Access.RelativePath, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(target), "selected managed volume"), nil
}
func (e AccessExecutor) native(ctx context.Context, r Request) error {
	if e.Backend == nil {
		return errors.New("independent native mapping observer unavailable")
	}
	m, err := e.Backend.InspectManagedFileVolume(ctx, "qemu:///system", r.ResourceID, r.Access.Mapping.DiskTarget)
	if err != nil {
		return err
	}
	if m != r.Access.Mapping {
		return domain.Fail("STALE_PLAN", "native stopped VM or managed volume mapping changed")
	}
	return nil
}
func readRootJSON(path string, out any) error {
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), "root-owned helper record")
	defer f.Close()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Uid != 0 || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0022 != 0 || st.Nlink != 1 || st.Size > 1<<20 {
		return errors.New("helper record must be a bounded single-link root-owned regular file without group/other write")
	}
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(b) > 1<<20 {
		return errors.New("helper record too large")
	}
	return wire.Decode(b, out)
}
func loadPolicy() (Policy, error) {
	var p Policy
	if err := ownedRoot(filepath.Dir(PolicyPath), true); err != nil {
		return p, err
	}
	err := readRootJSON(PolicyPath, &p)
	return p, err
}

// journalAccess publishes each record exactly once and fsyncs both the record
// and directory. An incomplete/duplicate intent requires observation, not retry.
func (e AccessExecutor) journalAccess(name string, value any) error {
	fd, err := unix.Openat2(unix.AT_FDCWD, e.journalPath(), &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(fd), "helper journal")
	defer dir.Close()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Uid != 0 || st.Mode&0077 != 0 {
		return errors.New("helper journal must be a private root-owned directory")
	}
	if filepath.Base(name) != name {
		return errors.New("invalid helper record name")
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	target, err := unix.Openat(fd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(target), "new helper record")
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = dir.Sync()
	}
	return err
}
func (e AccessExecutor) journalPath() string {
	if e.journalDirectory != "" {
		return e.journalDirectory
	}
	return JournalPath
}
func (e AccessExecutor) intentName(id string) string {
	return filepath.Join(e.journalPath(), id+".access-intent.json")
}
func (e AccessExecutor) resultName(id string) string {
	return filepath.Join(e.journalPath(), id+".access-complete.json")
}

func (e AccessExecutor) desired(r Request, before fileaccess.State) ([]byte, error) {
	if r.Operation == "storage.grant-read" {
		return before.DesiredRead(r.ActorUID, r.Access.ActorGroups)
	}
	if r.Operation != "storage.revoke-read" {
		return nil, errors.New("access action not allowlisted")
	}
	var original accessIntent
	if err := readRootJSON(e.intentName(r.Access.OriginalGrantJobID), &original); err != nil {
		return nil, err
	}
	o := original.Request
	binding, err := accessBinding(o)
	if err != nil {
		return nil, err
	}
	if original.Version != 1 || original.Binding != binding || o.Operation != "storage.grant-read" || o.Mode != "apply" || o.Access == nil || o.JobID != r.Access.OriginalGrantJobID || o.ActorUID != r.ActorUID || o.RootID != r.RootID || o.ResourceID != r.ResourceID || o.Access.Mapping != r.Access.Mapping || o.Access.RelativePath != r.Access.RelativePath {
		return nil, errors.New("original root-owned grant identity differs")
	}
	var result AccessResult
	if err = readRootJSON(e.resultName(o.JobID), &result); err != nil {
		return nil, err
	}
	if result.Version != 1 || !result.Complete || result.Binding != binding || result.JobID != o.JobID {
		return nil, domain.Fail("STALE_PLAN", "successful grant proof or exact granted metadata changed; refuse inferred restoration")
	}
	old, err := accessBefore(o)
	if err != nil {
		return nil, err
	}
	desired, err := old.DesiredRead(o.ActorUID, o.Access.ActorGroups)
	if err != nil {
		return nil, err
	}
	// A nested, subsequently revoked grant may advance ctime while restoring
	// exactly this ACL. Bind the fresh ctime in this revoke plan; the older proof
	// still requires unchanged generation, bytes-related metadata, ownership and
	// every access entry. This lets reviewed grants be unwound in reverse order.
	if hex.EncodeToString(desired) != original.DesiredACL || result.DesiredACL != original.DesiredACL || !fileaccess.MatchesAccess(result.State, old, desired) || !fileaccess.MatchesAccess(before, old, desired) {
		return nil, errors.New("original grant metadata is inconsistent")
	}
	return old.RestoreACL()
}

func (e AccessExecutor) Execute(ctx context.Context, r Request, p Policy, groups []uint32) (AccessResult, error) {
	var result AccessResult
	if r.Access == nil || !reflect.DeepEqual(groups, r.Access.ActorGroups) {
		return result, errors.New("signed groups differ from kernel socket peer credentials")
	}
	if err := ownedRoot(e.journalPath(), true); err != nil {
		return result, err
	}
	journalStat, err := os.Lstat(e.journalPath())
	if err != nil {
		return result, err
	}
	if journalStat.Mode().Perm() != 0700 {
		return result, errors.New("helper journal directory must be private mode 0700")
	}
	if _, err := accessPath(r, p); err != nil {
		return result, err
	}
	before, err := accessBefore(r)
	if err != nil {
		return result, err
	}
	binding, err := accessBinding(r)
	if err != nil {
		return result, err
	}
	// Authorize is repeated by the server for each invocation against current policy.
	if err = e.native(ctx, r); err != nil {
		return result, err
	}
	f, err := openAccessFile(r, p)
	if err != nil {
		return result, err
	}
	defer f.Close()
	guard, err := image.AcquireReadGuard(f)
	if err != nil {
		return result, err
	}
	defer guard.Close()
	current, err := fileaccess.Snapshot(f)
	if err != nil {
		return result, err
	}
	if r.Mode == "observe" {
		var intent accessIntent
		if err = readRootJSON(e.intentName(r.JobID), &intent); err != nil {
			return result, err
		}
		savedBinding, err := accessBinding(intent.Request)
		if err != nil {
			return result, err
		}
		if intent.Version != 1 || intent.Binding != binding || savedBinding != binding || intent.Request.Mode != "apply" {
			return result, errors.New("durable helper intent differs from accepted operation")
		}
		desired, err := hex.DecodeString(intent.DesiredACL)
		if err != nil {
			return result, err
		}
		if !fileaccess.MatchesAccess(current, before, desired) {
			return result, domain.Fail("RECOVERY_REQUIRED", "intended access state not observed; no mutation replayed")
		}
		if err = e.native(ctx, r); err != nil {
			return result, err
		}
		if err = verifyAccessPath(r, p, current); err != nil {
			return result, err
		}
		result = AccessResult{1, r.JobID, binding, true, current, hex.EncodeToString(desired)}
		var saved AccessResult
		err = readRootJSON(e.resultName(r.JobID), &saved)
		if errors.Is(err, os.ErrNotExist) {
			err = e.journalAccess(filepath.Base(e.resultName(r.JobID)), result)
		} else if err == nil && saved != result {
			err = domain.Fail("RECOVERY_REQUIRED", "completed helper receipt differs from current metadata")
		}
		return result, err
	}
	if current != before {
		return result, domain.Fail("STALE_PLAN", "access metadata changed since review")
	}
	desired, err := e.desired(r, before)
	if err != nil {
		return result, err
	}
	if err = e.native(ctx, r); err != nil {
		return result, err
	}
	if err = verifyAccessPath(r, p, current); err != nil {
		return result, err
	}
	if r.Mode == "check" {
		return AccessResult{Version: 1, JobID: r.JobID, Binding: binding, State: current, DesiredACL: hex.EncodeToString(desired)}, nil
	}
	if r.Mode != "apply" {
		return result, errors.New("explicit helper mode required")
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	intent := accessIntent{1, binding, r, hex.EncodeToString(desired)}
	if err = e.journalAccess(filepath.Base(e.intentName(r.JobID)), intent); err != nil {
		return result, err
	}
	// No pathname lookup occurs in the metadata write; generation is checked again
	// on this held descriptor immediately before the one bounded xattr syscall.
	after, err := fileaccess.SetACL(f, before, desired)
	if err != nil {
		return result, err
	}
	if !fileaccess.MatchesAccess(after, before, desired) {
		return result, domain.Fail("RECOVERY_REQUIRED", "helper access postcondition differs")
	}
	if err = e.native(ctx, r); err != nil {
		return result, err
	}
	if err = verifyAccessPath(r, p, after); err != nil {
		return result, err
	}
	result = AccessResult{1, r.JobID, binding, true, after, hex.EncodeToString(desired)}
	err = e.journalAccess(filepath.Base(e.resultName(r.JobID)), result)
	return result, err
}

func verifyAccessPath(r Request, p Policy, expected fileaccess.State) error {
	f, err := openAccessFile(r, p)
	if err != nil {
		return err
	}
	defer f.Close()
	state, err := fileaccess.Snapshot(f)
	if err != nil {
		return err
	}
	if state != expected {
		return domain.Fail("RECOVERY_REQUIRED", "volume path no longer resolves to the observed metadata generation")
	}
	return nil
}
