//go:build linux

package fileaccess

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
)

const attribute = "system.posix_acl_access"

// State binds access metadata and the exact ordinary-file generation. UID/GID
// are captured separately because fileidentity also serves non-permission flows.
type State struct {
	File fileidentity.Identity `json:"file"`
	UID  uint32                `json:"uid"`
	GID  uint32                `json:"gid"`
	ACL  string                `json:"acl"` // lowercase hex; empty means no extended access ACL
}

func Snapshot(f *os.File) (State, error) {
	var out State
	if f == nil {
		return out, domain.Fail("INVALID_INPUT", "held file required for access metadata")
	}
	first, err := fileidentity.InspectFile(f, false)
	if err != nil {
		return out, err
	}
	var st unix.Stat_t
	if err = unix.Fstat(int(f.Fd()), &st); err != nil {
		return out, err
	}
	out.UID, out.GID = st.Uid, st.Gid
	// This intentional proc link addresses only our held descriptor. O_PATH lets
	// an unprivileged preview inspect access metadata without reading disk bytes.
	raw := make([]byte, 4+8*maximumEntries)
	n, err := unix.Getxattr(fmt.Sprintf("/proc/self/fd/%d", f.Fd()), attribute, raw)
	if errors.Is(err, unix.ENODATA) {
		raw = nil
	} else if err != nil {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "bounded POSIX access ACL observation unavailable")
	} else {
		raw = raw[:n]
		if _, err = decode(raw); err != nil {
			return out, err
		}
	}
	out.ACL = hex.EncodeToString(raw)
	after, err := fileidentity.InspectFile(f, false)
	if err != nil {
		return out, err
	}
	if first != after {
		return out, domain.Fail("SOURCE_CHANGED", "file changed during ACL observation")
	}
	out.File = after
	return out, nil
}

func (s State) ACLBytes() ([]byte, error) {
	if len(s.ACL) > 2*(4+8*maximumEntries) {
		return nil, errors.New("ACL exceeds bound")
	}
	raw, err := hex.DecodeString(s.ACL)
	if err != nil || hex.EncodeToString(raw) != s.ACL {
		return nil, errors.New("noncanonical access ACL")
	}
	if len(raw) > 0 {
		if _, err = decode(raw); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

// DesiredRead is deterministic over the reviewed file metadata and the actual
// coordinator credentials. The helper independently obtains its peer's groups.
func (s State) DesiredRead(actor uint32, groups []uint32) ([]byte, error) {
	raw, err := s.ACLBytes()
	if err != nil {
		return nil, err
	}
	return GrantRead(raw, uint32(s.File.Mode), s.UID, s.GID, actor, groups)
}

// RestoreACL yields a base ACL for an originally mode-only file. Linux applies
// the mode and removes the redundant base ACL in the same setxattr operation;
// there is no unsafe remove-ACL/then-chmod intermediate state.
func (s State) RestoreACL() ([]byte, error) {
	raw, err := s.ACLBytes()
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		return raw, nil
	}
	mode := s.File.Mode
	return encode([]entry{{userObject, (mode >> 6) & 7, undefinedID}, {groupObject, (mode >> 3) & 7, undefinedID}, {otherEntry, mode & 7, undefinedID}}), nil
}

// SetACL changes only the access ACL on a held read-only ordinary file. Callers
// own policy, leases, durable intent and native mapping checks; this primitive
// still refuses an unexpected metadata generation immediately before its syscall.
func SetACL(f *os.File, expected State, raw []byte) (State, error) {
	var empty State
	if f == nil {
		return empty, domain.Fail("INVALID_INPUT", "held file required")
	}
	if _, err := decode(raw); err != nil {
		return empty, err
	}
	flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY || flags&unix.O_PATH != 0 {
		return empty, domain.Fail("INVALID_INPUT", "ACL mutation requires a held read-only file descriptor")
	}
	current, err := Snapshot(f)
	if err != nil {
		return empty, err
	}
	if current != expected {
		return empty, domain.Fail("STALE_PLAN", "managed-file access metadata changed before effect")
	}
	if err = unix.Fsetxattr(int(f.Fd()), attribute, raw, 0); err != nil {
		return empty, err
	}
	if err = f.Sync(); err != nil {
		return empty, err
	}
	after, err := Snapshot(f)
	if err != nil {
		return empty, err
	}
	if after.File.Generation != expected.File.Generation || after.File.Size != expected.File.Size || after.File.Modified != expected.File.Modified || after.UID != expected.UID || after.GID != expected.GID {
		return after, domain.Fail("RECOVERY_REQUIRED", "file content identity or ownership changed during ACL mutation")
	}
	return after, nil
}

// MatchesAccess ignores ctime because our own metadata change updates it. All
// byte-related and access fields must still match the reviewed result.
func MatchesAccess(current, original State, desired []byte) bool {
	entries, err := decode(desired)
	if err != nil {
		return false
	}
	wantMode := original.File.Mode &^ 0777
	hasMask := false
	for _, e := range entries {
		if e.tag == maskEntry {
			hasMask = true
		}
	}
	for _, e := range entries {
		switch e.tag {
		case userObject:
			wantMode |= e.permissions << 6
		case groupObject:
			if !hasMask {
				wantMode |= e.permissions << 3
			}
		case maskEntry:
			wantMode |= e.permissions << 3
		case otherEntry:
			wantMode |= e.permissions
		}
	}
	raw, err := current.ACLBytes()
	if err != nil {
		return false
	}
	if len(entries) == 3 {
		if len(raw) != 0 && !bytes.Equal(raw, desired) {
			return false
		}
	} else if !bytes.Equal(raw, desired) {
		return false
	}
	return current.File.Generation == original.File.Generation && current.File.Size == original.File.Size && current.File.Modified == original.File.Modified && current.File.Mode == wantMode && current.UID == original.UID && current.GID == original.GID
}
