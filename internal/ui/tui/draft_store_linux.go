//go:build linux

package tui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/wire"
)

const draftDocumentLimit = 1 << 20

var (
	ErrDraftConflict = errors.New("this draft changed in another Virmill window; reload it before saving")
	ErrDraftBusy     = errors.New("another Virmill window is saving drafts; try again")
)

// DraftStore holds frontend choices only. Its callers must supply an explicitly
// allowlisted DTO excluding secrets, credentials, observed capabilities and plan
// authority. Restoring choices never authorizes or resumes a backend operation.
// Maps and interface fields are rejected so future form fields cannot silently
// widen that allowlist. Root-owned TUI sessions are never supported.
type DraftStore struct {
	mu   sync.Mutex
	dir  int
	lock int
	uid  uint32
}

type draftEnvelope struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Generation string          `json:"generation"`
	Document   json.RawMessage `json:"document"`
}

// NewDraftStore opens $XDG_STATE_HOME/virmill/tui-drafts, defaulting to
// ~/.local/state/virmill/tui-drafts. It never follows a symbolic link.
func NewDraftStore() (*DraftStore, error) {
	if os.Geteuid() == 0 || os.Getuid() != os.Geteuid() {
		return nil, errors.New("drafts require an ordinary user session")
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(base) || filepath.Clean(base) != base || strings.ContainsRune(base, 0) {
		return nil, errors.New("XDG_STATE_HOME must be an absolute path without . or .. components")
	}
	uid := uint32(os.Geteuid())
	dir, err := openDraftDirectory(base, uid)
	if err != nil {
		return nil, err
	}
	s := &DraftStore{dir: dir, lock: -1, uid: uid}
	lock, err := unix.Openat(dir, ".lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0600)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("cannot open draft lock: %w", err)
	}
	s.lock = lock
	if _, err := s.privateFile(lock); err != nil {
		s.Close()
		return nil, err
	}
	if err := unix.Fsync(dir); err != nil {
		s.Close()
		return nil, fmt.Errorf("cannot make draft folder durable: %w", err)
	}
	return s, nil
}

func openDraftDirectory(base string, uid uint32) (int, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}
	parts := strings.Split(strings.TrimPrefix(base, "/"), "/")
	if base == "/" {
		parts = nil
	}
	parts = append(parts, "virmill", "tui-drafts")
	for i, part := range parts {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(e, unix.ENOENT) {
			if e = unix.Mkdirat(fd, part, 0700); e == nil || errors.Is(e, unix.EEXIST) {
				if e = unix.Fsync(fd); e == nil {
					next, e = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				}
			}
		}
		unix.Close(fd)
		if e != nil {
			return -1, fmt.Errorf("cannot open private draft folder without symbolic links: %w", e)
		}
		fd = next
		var st unix.Stat_t
		if e = unix.Fstat(fd, &st); e != nil {
			unix.Close(fd)
			return -1, e
		}
		private := i >= len(parts)-2
		// Shared sticky ancestors such as /tmp are allowed; the Virmill folders
		// themselves must be private and owned by this user, never repaired.
		unsafeAncestor := st.Mode&0022 != 0 && st.Mode&unix.S_ISVTX == 0
		if private && (st.Uid != uid || st.Mode&07777 != 0700) || !private && unsafeAncestor {
			unix.Close(fd)
			return -1, errors.New("draft folder ownership or permissions are unsafe; use a private user state folder")
		}
	}
	return fd, nil
}

func (s *DraftStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.lock >= 0 {
		err = unix.Close(s.lock)
		s.lock = -1
	}
	if s.dir >= 0 {
		err = errors.Join(err, unix.Close(s.dir))
		s.dir = -1
	}
	return err
}

func draftName(name string) (string, error) {
	if name != "import" && name != "creation" {
		return "", errors.New("unknown draft kind")
	}
	return name + ".json", nil
}

func draftDTOType(v any, destination bool) error {
	t := reflect.TypeOf(v)
	if t == nil {
		return errors.New("draft choices are missing")
	}
	if t.Kind() == reflect.Pointer && reflect.ValueOf(v).IsNil() {
		return errors.New("draft choices are missing")
	}
	if destination && (t.Kind() != reflect.Pointer || reflect.ValueOf(v).IsNil()) {
		return errors.New("draft destination must be a nonnil typed pointer")
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return errors.New("draft choices must use an explicit struct allowlist")
	}
	seen := map[reflect.Type]bool{}
	var check func(reflect.Type) error
	check = func(t reflect.Type) error {
		if seen[t] {
			return nil
		}
		seen[t] = true
		marshaler := reflect.TypeOf((*json.Marshaler)(nil)).Elem()
		unmarshaler := reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
		if t.Implements(marshaler) || t.Implements(unmarshaler) || reflect.PointerTo(t).Implements(marshaler) || reflect.PointerTo(t).Implements(unmarshaler) {
			return errors.New("draft allowlist cannot use custom JSON encoders or decoders")
		}
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			return check(t.Elem())
		case reflect.Struct:
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				if f.PkgPath != "" || f.Tag.Get("json") == "-" {
					continue
				}
				if err := check(f.Type); err != nil {
					return err
				}
			}
		case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		default:
			return errors.New("draft allowlist cannot contain maps, interfaces or executable values")
		}
		return nil
	}
	return check(t)
}

func (s *DraftStore) privateFile(fd int) (unix.Stat_t, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return st, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Uid != s.uid || st.Nlink != 1 {
		return st, errors.New("draft files must be private, single-link regular files owned by this user")
	}
	return st, nil
}

func (s *DraftStore) acquire() error {
	if s.dir < 0 || s.lock < 0 {
		return os.ErrClosed
	}
	st, err := s.privateFile(s.lock)
	if err != nil {
		return err
	}
	var named unix.Stat_t
	if err := unix.Fstatat(s.dir, ".lock", &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if st.Dev != named.Dev || st.Ino != named.Ino {
		return ErrDraftConflict
	}
	if err := unix.Flock(s.lock, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return ErrDraftBusy
		}
		return err
	}
	return nil
}

func (s *DraftStore) read(name string) (draftEnvelope, unix.Stat_t, error) {
	var envelope draftEnvelope
	fd, err := unix.Openat(s.dir, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return envelope, unix.Stat_t{}, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	st, err := s.privateFile(fd)
	if err != nil {
		return envelope, st, err
	}
	if st.Size < 1 || st.Size > draftDocumentLimit {
		return envelope, st, errors.New("saved draft exceeds the size limit or is empty")
	}
	b, err := io.ReadAll(io.LimitReader(f, draftDocumentLimit+1))
	if err != nil {
		return envelope, st, err
	}
	if len(b) > draftDocumentLimit {
		return envelope, st, errors.New("saved draft exceeds the size limit")
	}
	if err := wire.Decode(b, &envelope); err != nil {
		return envelope, st, fmt.Errorf("cannot read saved draft: %w", err)
	}
	if envelope.APIVersion != "virmill/v1" || envelope.Kind != "TUIDraft" || !validDraftGeneration(envelope.Generation) || len(envelope.Document) == 0 || string(envelope.Document) == "null" {
		return envelope, st, errors.New("saved draft has an unsupported version or invalid metadata")
	}
	after, err := s.privateFile(fd)
	if err != nil {
		return envelope, st, err
	}
	if !sameDraftFile(after, st) {
		return envelope, st, ErrDraftConflict
	}
	return envelope, st, nil
}

func validDraftGeneration(g string) bool {
	if len(g) != 32 || strings.ToLower(g) != g {
		return false
	}
	b, err := hex.DecodeString(g)
	return err == nil && len(b) == 16 && g != strings.Repeat("0", 32)
}

func sameDraftFile(a, b unix.Stat_t) bool {
	// Reading may update atime; it does not change the identity/content binding.
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

// Load performs strict decoding into the caller's allowlisted DTO. Missing data
// returns os.ErrNotExist. Incompatible fields are an error, never discarded.
func (s *DraftStore) Load(name string, dst any) (string, error) {
	if err := draftDTOType(dst, true); err != nil {
		return "", err
	}
	name, err := draftName(name)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.acquire(); err != nil {
		return "", err
	}
	defer unix.Flock(s.lock, unix.LOCK_UN)
	e, _, err := s.read(name)
	if err != nil {
		return "", err
	}
	// Decode into a fresh value so malformed files cannot partially mutate forms.
	v := reflect.ValueOf(dst)
	fresh := reflect.New(v.Elem().Type())
	if err := wire.Decode(e.Document, fresh.Interface()); err != nil {
		return "", fmt.Errorf("saved choices are incompatible: %w", err)
	}
	v.Elem().Set(fresh.Elem())
	return e.Generation, nil
}

func (s *DraftStore) current(name, expected string) (unix.Stat_t, bool, error) {
	e, st, err := s.read(name)
	if errors.Is(err, os.ErrNotExist) {
		if expected == "" {
			return st, false, nil
		}
		return st, false, ErrDraftConflict
	}
	if err != nil {
		return st, false, err
	}
	if expected == "" || e.Generation != expected {
		return st, true, ErrDraftConflict
	}
	return st, true, nil
}

func (s *DraftStore) unchanged(name string, st unix.Stat_t, exists bool) error {
	var now unix.Stat_t
	err := unix.Fstatat(s.dir, name, &now, unix.AT_SYMLINK_NOFOLLOW)
	if !exists && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !exists || !sameDraftFile(now, st) {
		return ErrDraftConflict
	}
	return nil
}

// Save atomically replaces only the generation the caller last loaded. An empty
// expectedGeneration creates a new draft and never overwrites an existing one.
// A durability error after publication returns the new generation with the error.
func (s *DraftStore) Save(name, expectedGeneration string, document any) (string, error) {
	if err := draftDTOType(document, false); err != nil {
		return "", err
	}
	name, err := draftName(name)
	if err != nil {
		return "", err
	}
	if expectedGeneration != "" && !validDraftGeneration(expectedGeneration) {
		return "", errors.New("invalid draft generation")
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	if string(raw) == "null" {
		return "", errors.New("draft choices are missing")
	}
	if len(raw) > draftDocumentLimit {
		return "", errors.New("draft choices exceed the 1 MiB size limit")
	}
	if err := wire.Validate(raw); err != nil {
		return "", err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	generation := hex.EncodeToString(nonce[:])
	data, err := json.Marshal(draftEnvelope{APIVersion: "virmill/v1", Kind: "TUIDraft", Generation: generation, Document: raw})
	if err != nil {
		return "", err
	}
	if len(data) > draftDocumentLimit {
		return "", errors.New("draft choices exceed the 1 MiB size limit")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.acquire(); err != nil {
		return "", err
	}
	defer unix.Flock(s.lock, unix.LOCK_UN)
	st, exists, err := s.current(name, expectedGeneration)
	if err != nil {
		return "", err
	}
	if exists {
		old, _, err := s.read(name)
		if err != nil {
			return "", err
		}
		t := reflect.TypeOf(document)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if err := wire.Decode(old.Document, reflect.New(t).Interface()); err != nil {
			return "", fmt.Errorf("saved choices are incompatible; reload or explicitly discard the old draft: %w", err)
		}
	}
	fd, err := unix.Openat(s.dir, ".", unix.O_WRONLY|unix.O_TMPFILE|unix.O_CLOEXEC, 0600)
	if err != nil {
		return "", fmt.Errorf("this filesystem cannot safely stage drafts: %w", err)
	}
	f := os.NewFile(uintptr(fd), "draft staging")
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	tmp := ".stage-" + generation
	if err := unix.Linkat(unix.AT_FDCWD, fmt.Sprintf("/proc/self/fd/%d", fd), s.dir, tmp, unix.AT_SYMLINK_FOLLOW); err != nil {
		return "", err
	}
	defer unix.Unlinkat(s.dir, tmp, 0)
	if err := s.unchanged(name, st, exists); err != nil {
		return "", err
	}
	if exists {
		err = unix.Renameat(s.dir, tmp, s.dir, name)
	} else {
		err = unix.Renameat2(s.dir, tmp, s.dir, name, unix.RENAME_NOREPLACE)
	}
	if err != nil {
		return "", err
	}
	if err := unix.Fsync(s.dir); err != nil {
		return generation, fmt.Errorf("draft saved, but folder durability could not be confirmed: %w", err)
	}
	return generation, nil
}

// Delete removes only the draft generation the caller last loaded.
func (s *DraftStore) Delete(name, expectedGeneration string) error {
	name, err := draftName(name)
	if err != nil {
		return err
	}
	if !validDraftGeneration(expectedGeneration) {
		return errors.New("load the draft before deleting it")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.acquire(); err != nil {
		return err
	}
	defer unix.Flock(s.lock, unix.LOCK_UN)
	st, exists, err := s.current(name, expectedGeneration)
	if err != nil {
		return err
	}
	if err := s.unchanged(name, st, exists); err != nil {
		return err
	}
	if err := unix.Unlinkat(s.dir, name, 0); err != nil {
		return err
	}
	if err := unix.Fsync(s.dir); err != nil {
		return fmt.Errorf("draft removed, but folder durability could not be confirmed: %w", err)
	}
	return nil
}
