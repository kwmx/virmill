//go:build linux

package helper

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileaccess"
	"virmill.local/core/internal/domain"
)

const (
	auxiliaryDirectoryLimit = 128
	auxiliaryDepthLimit     = 16
	auxiliaryMetadataLimit  = 96 << 10
	auxiliaryACLBytes       = 4 + 8*128
	auxiliaryLabelBytes     = 4096
)

// AuxiliaryExecutor observes metadata from an independently supplied native
// backend. The authenticated helper entry point must call Authorize first;
// Inspect additionally checks the typed auxiliary authority at its own boundary.
// It does not read state bytes, acquire producer locks or authorize capture.
type AuxiliaryExecutor struct {
	Backend      domain.ColdStateInspector
	rootOwnerUID uint32 // private ordinary-user fixture override; zero in production
}

func auxiliaryFailure(code, message string) error { return domain.Fail(code, message) }

func auxiliaryCleanPath(p string, absolute bool) bool {
	if p == "" || len(p) > 4096 || !utf8.ValidString(p) || strings.Contains(p, "\\") || filepath.Clean(p) != p || filepath.IsAbs(p) != absolute || p == "/" || p == "." {
		return false
	}
	if !absolute && !filepath.IsLocal(p) {
		return false
	}
	for _, r := range p {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return false
		}
	}
	return true
}

func auxiliaryRelative(root, source string) (string, error) {
	if !auxiliaryCleanPath(root, true) || !auxiliaryCleanPath(source, true) {
		return "", auxiliaryFailure("INVALID_INPUT", "canonical bounded native auxiliary paths required")
	}
	rel, err := filepath.Rel(root, source)
	if err != nil || !auxiliaryCleanPath(rel, false) || len(strings.Split(rel, "/")) > auxiliaryDepthLimit {
		return "", auxiliaryFailure("UNSUPPORTED_CAPABILITY", "native auxiliary source must be strictly beneath the approved root within the depth bound")
	}
	for _, component := range strings.Split(rel, "/") {
		if component == ".lock" {
			return "", auxiliaryFailure("UNSUPPORTED_CAPABILITY", "native auxiliary payload source cannot select a producer control path")
		}
	}
	return rel, nil
}

func (e AuxiliaryExecutor) native(ctx context.Context, r Request) (domain.ColdStateLayout, error) {
	if err := ctx.Err(); err != nil {
		return domain.ColdStateLayout{}, err
	}
	if e.Backend == nil {
		return domain.ColdStateLayout{}, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "independent native auxiliary observer unavailable")
	}
	native, err := e.Backend.InspectColdState(ctx, "qemu:///system", r.ResourceID)
	if err != nil {
		return domain.ColdStateLayout{}, err
	}
	resource := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: r.ResourceID}
	if native.Resource != resource || native.Fingerprint != r.Auxiliary.Fingerprint || native.Layout.VMID != r.ResourceID || native.Source == nil || !reflect.DeepEqual(native.Source.State, native.Layout) {
		return domain.ColdStateLayout{}, auxiliaryFailure("STALE_PLAN", "native resource, persistent layout or fingerprint differs from the authorized VM")
	}
	if !native.Persistent || native.State != "stopped" || native.HasManagedSave || native.Autostart {
		return domain.ColdStateLayout{}, auxiliaryFailure("INVALID_STATE", "auxiliary inventory requires a persistent stopped VM without managed save or autostart")
	}
	// Freeze nested pointers and bound the metadata supplied by the native
	// adapter. No caller-supplied layout participates in source selection.
	raw, err := json.Marshal(native.Layout)
	if err != nil || len(raw) > 32<<10 {
		return domain.ColdStateLayout{}, auxiliaryFailure("INVALID_INPUT", "native auxiliary layout exceeds metadata bounds")
	}
	var layout domain.ColdStateLayout
	if err = json.Unmarshal(raw, &layout); err != nil {
		return domain.ColdStateLayout{}, err
	}
	if !reflect.DeepEqual(native.Layout, layout) {
		return domain.ColdStateLayout{}, auxiliaryFailure("INVALID_INPUT", "native auxiliary layout cannot be represented without changing its identity")
	}
	if err = ctx.Err(); err != nil {
		return domain.ColdStateLayout{}, err
	}
	return layout, nil
}

// Inspect returns a repeated metadata observation, never a capture proof. Every
// failure, including final cancellation, returns the zero inventory.
func (e AuxiliaryExecutor) Inspect(ctx context.Context, r Request, p Policy) (AuxiliaryInventory, error) {
	var empty AuxiliaryInventory
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if r.Operation != "state.auxiliary" || r.APIVersion != domain.APIVersion || p.APIVersion != domain.APIVersion {
		return empty, auxiliaryFailure("INVALID_INPUT", "typed auxiliary helper operation required")
	}
	if err := authorizeAuxiliary(r, p); err != nil {
		return empty, err
	}
	permission, err := auxiliaryPolicy(r, p)
	if err != nil {
		return empty, err
	}
	layout, err := e.native(ctx, r)
	if err != nil {
		return empty, err
	}
	first, err := e.scanAuxiliary(ctx, r, p, permission, layout)
	if err != nil {
		return empty, err
	}
	defer first.close()
	again, err := e.native(ctx, r)
	if err != nil {
		return empty, err
	}
	if !reflect.DeepEqual(layout, again) {
		return empty, auxiliaryFailure("SOURCE_CHANGED", "native auxiliary layout changed during observation")
	}
	second, err := e.scanAuxiliary(ctx, r, p, permission, again)
	if err != nil {
		return empty, err
	}
	defer second.close()
	if !reflect.DeepEqual(first.inventory, second.inventory) {
		return empty, auxiliaryFailure("SOURCE_CHANGED", "auxiliary membership or metadata changed during observation")
	}
	again, err = e.native(ctx, r)
	if err != nil {
		return empty, err
	}
	if !reflect.DeepEqual(layout, again) {
		return empty, auxiliaryFailure("SOURCE_CHANGED", "native auxiliary layout changed before return")
	}
	if err = first.recheck(ctx); err != nil {
		return empty, err
	}
	if r.Auxiliary.Expected != nil && !reflect.DeepEqual(*r.Auxiliary.Expected, first.inventory) {
		return empty, auxiliaryFailure("STALE_PLAN", "fresh auxiliary inventory differs from the authorized expected inventory")
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return first.inventory, nil
}

type auxiliaryHeld struct {
	file  *os.File
	state AuxiliaryFileState
}

type auxiliaryScan struct {
	inventory  AuxiliaryInventory
	root       *os.File
	held       map[string]auxiliaryHeld
	aliases    map[string]string
	names      map[string]string
	permission AuxiliaryPermission
	rootOwner  uint32
}

func (s *auxiliaryScan) close() {
	for _, held := range s.held {
		held.file.Close()
	}
	if s.root != nil {
		s.root.Close()
	}
}

func auxiliaryOpen(root int, name string, directory bool, readable bool) (*os.File, error) {
	flags := uint64(unix.O_PATH | unix.O_CLOEXEC)
	if directory {
		flags |= unix.O_DIRECTORY
	}
	if readable {
		// Only directory entries can be read by this adapter. O_NOATIME avoids
		// changing access timestamps merely by enumerating the approved tree.
		if !directory {
			return nil, auxiliaryFailure("INVALID_INPUT", "auxiliary content reads are forbidden during inventory")
		}
		flags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOATIME
	}
	resolve := uint64(unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS)
	if root != unix.AT_FDCWD {
		resolve |= unix.RESOLVE_BENEATH | unix.RESOLVE_NO_XDEV
	}
	fd, err := unix.Openat2(root, name, &unix.OpenHow{Flags: flags, Resolve: resolve})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), "held auxiliary metadata"), nil
}

func auxiliaryStat(f *os.File) (AuxiliaryFileState, error) {
	var st unix.Statx_t
	required := uint32(unix.STATX_TYPE | unix.STATX_MODE | unix.STATX_NLINK | unix.STATX_UID | unix.STATX_GID | unix.STATX_INO | unix.STATX_SIZE | unix.STATX_MTIME | unix.STATX_CTIME | unix.STATX_BTIME | unix.STATX_MNT_ID)
	if err := unix.Statx(int(f.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_STATX_FORCE_SYNC, int(required), &st); err != nil {
		return AuxiliaryFileState{}, err
	}
	if st.Mask&required != required || st.Ino == 0 || st.Mnt_id == 0 || st.Btime.Sec <= 0 || st.Btime.Nsec >= 1e9 || st.Mtime.Nsec >= 1e9 || st.Ctime.Nsec >= 1e9 || st.Nlink == 0 {
		return AuxiliaryFileState{}, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "filesystem omitted a complete auxiliary birth, mount, ownership or change identity")
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR && (st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1) {
		return AuxiliaryFileState{}, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "auxiliary objects must be directories or single-link regular files")
	}
	return AuxiliaryFileState{
		Generation: fmt.Sprintf("linux-aux-statx-v1:%d:%d:%d:%d:%d:%09d:%d:%d", st.Dev_major, st.Dev_minor, st.Mnt_id, st.Ino, st.Btime.Sec, st.Btime.Nsec, st.Attributes_mask, st.Attributes),
		Size:       st.Size, Modified: fmt.Sprintf("%d:%09d", st.Mtime.Sec, st.Mtime.Nsec), Changed: fmt.Sprintf("%d:%09d", st.Ctime.Sec, st.Ctime.Nsec),
		Links: st.Nlink, Mode: st.Mode, UID: st.Uid, GID: st.Gid,
	}, nil
}

func auxiliaryXattr(f *os.File, name string, limit int) (string, error) {
	// This intentional proc link selects only this already held descriptor.
	// Linux fgetxattr rejects O_PATH; following a caller pathname is forbidden.
	raw := make([]byte, limit)
	n, err := unix.Getxattr(fmt.Sprintf("/proc/self/fd/%d", f.Fd()), name, raw)
	if errors.Is(err, unix.ENODATA) {
		return "", nil
	}
	if err != nil || n <= 0 || n > len(raw) {
		return "", auxiliaryFailure("UNSUPPORTED_CAPABILITY", "bounded auxiliary access ACL or SELinux metadata unavailable")
	}
	return hex.EncodeToString(raw[:n]), nil
}

func auxiliaryMetadata(ctx context.Context, f *os.File) (AuxiliaryFileState, error) {
	if err := ctx.Err(); err != nil {
		return AuxiliaryFileState{}, err
	}
	before, err := auxiliaryStat(f)
	if err != nil {
		return AuxiliaryFileState{}, err
	}
	acl, err := auxiliaryXattr(f, "system.posix_acl_access", auxiliaryACLBytes)
	if err != nil {
		return AuxiliaryFileState{}, err
	}
	if _, err = (fileaccess.State{ACL: acl}).ACLBytes(); err != nil {
		return AuxiliaryFileState{}, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "auxiliary access ACL is not a supported canonical Linux ACL")
	}
	label, err := auxiliaryXattr(f, "security.selinux", auxiliaryLabelBytes)
	if err != nil {
		return AuxiliaryFileState{}, err
	}
	after, err := auxiliaryStat(f)
	if err != nil {
		return AuxiliaryFileState{}, err
	}
	if before != after {
		return AuxiliaryFileState{}, auxiliaryFailure("SOURCE_CHANGED", "auxiliary metadata changed during xattr observation")
	}
	if err = ctx.Err(); err != nil {
		return AuxiliaryFileState{}, err
	}
	after.ACL, after.SELinux = acl, label
	return after, nil
}

func (s *auxiliaryScan) pin(ctx context.Context, relative string, directory bool) (auxiliaryHeld, error) {
	if !auxiliaryCleanPath(relative, false) || len(strings.Split(relative, "/")) > auxiliaryDepthLimit {
		return auxiliaryHeld{}, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "auxiliary pathname exceeds canonical name or depth bounds")
	}
	if existing, ok := s.held[relative]; ok {
		if directory && existing.state.Mode&unix.S_IFMT == unix.S_IFDIR {
			return existing, nil
		}
		return auxiliaryHeld{}, auxiliaryFailure("INVALID_INPUT", "auxiliary sources have overlapping or conflicting identities")
	}
	fold := strings.ToLower(relative)
	if prior, ok := s.names[fold]; ok && prior != relative {
		return auxiliaryHeld{}, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "auxiliary names have an ambiguous case alias")
	}
	f, err := auxiliaryOpen(int(s.root.Fd()), relative, directory, false)
	if err != nil {
		return auxiliaryHeld{}, err
	}
	state, err := auxiliaryMetadata(ctx, f)
	if err == nil && directory != (state.Mode&unix.S_IFMT == unix.S_IFDIR) {
		err = auxiliaryFailure("UNSUPPORTED_CAPABILITY", "native auxiliary source type differs from the filesystem")
	}
	if err == nil {
		stateOwner := state.UID == s.permission.StateUID && state.GID == s.permission.StateGID
		adminDirectory := directory && state.UID == s.rootOwner && state.Mode&0022 == 0
		if (!stateOwner && !adminDirectory) || state.Mode&0002 != 0 || !directory && state.Mode&07000 != 0 {
			err = auxiliaryFailure("UNSUPPORTED_CAPABILITY", "auxiliary object ownership or permissions differ from administrator policy")
		}
	}
	if err == nil {
		if prior, ok := s.aliases[state.Generation]; ok && prior != relative {
			err = auxiliaryFailure("UNSUPPORTED_CAPABILITY", "auxiliary objects alias one filesystem generation")
		}
	}
	if err == nil && directory && len(s.inventory.Directories) >= auxiliaryDirectoryLimit {
		err = auxiliaryFailure("UNSUPPORTED_CAPABILITY", "auxiliary directory inventory exceeds bound")
	}
	if err != nil {
		f.Close()
		return auxiliaryHeld{}, err
	}
	held := auxiliaryHeld{file: f, state: state}
	s.held[relative], s.aliases[state.Generation], s.names[fold] = held, relative, relative
	if directory {
		s.inventory.Directories = append(s.inventory.Directories, AuxiliaryDirectory{RelativePath: relative, State: state})
	}
	return held, nil
}

func (s *auxiliaryScan) parents(ctx context.Context, relative string) error {
	parts := strings.Split(relative, "/")
	for i := 1; i < len(parts); i++ {
		if _, err := s.pin(ctx, strings.Join(parts[:i], "/"), true); err != nil {
			return err
		}
	}
	return nil
}

func (s *auxiliaryScan) member(ctx context.Context, relative, kind string) error {
	if err := s.parents(ctx, relative); err != nil {
		return err
	}
	held, err := s.pin(ctx, relative, false)
	if err != nil {
		return err
	}
	if kind == "tpm-lock" {
		if s.inventory.TPMLock != nil || held.state.Size != 0 {
			return auxiliaryFailure("UNSUPPORTED_CAPABILITY", "TPM producer lock must be one empty regular control file")
		}
		s.inventory.TPMLock = &AuxiliaryMember{ID: "tpm-lock", Kind: kind, RelativePath: relative, State: held.state}
		return nil
	}
	if len(s.inventory.Members) >= int(s.permission.MaxMembers) || held.state.Size > s.permission.MaxBytes-s.inventory.TotalBytes || kind == "nvram" && held.state.Size == 0 {
		return auxiliaryFailure("UNSUPPORTED_CAPABILITY", "auxiliary payload metadata exceeds bounds or NVRAM is empty")
	}
	s.inventory.Members = append(s.inventory.Members, AuxiliaryMember{Kind: kind, RelativePath: relative, State: held.state})
	s.inventory.TotalBytes += held.state.Size
	return nil
}

func (s *auxiliaryScan) walkTPM(ctx context.Context, relative, tpmRoot string) error {
	held, err := s.pin(ctx, relative, true)
	if err != nil {
		return err
	}
	f, err := auxiliaryOpen(int(s.root.Fd()), relative, true, true)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := auxiliaryMetadata(ctx, f)
	if err != nil {
		return err
	}
	if opened != held.state {
		return auxiliaryFailure("SOURCE_CHANGED", "TPM directory changed before enumeration")
	}
	var names []string
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		chunk, readErr := f.Readdirnames(32)
		if len(names)+len(chunk) > int(s.permission.MaxMembers)+auxiliaryDirectoryLimit+1 {
			return auxiliaryFailure("UNSUPPORTED_CAPABILITY", "TPM directory entry inventory exceeds bound")
		}
		names = append(names, chunk...)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	sort.Strings(names)
	for i, name := range names {
		if err = ctx.Err(); err != nil {
			return err
		}
		if !auxiliaryCleanPath(name, false) || strings.Contains(name, "/") || i > 0 && names[i-1] == name || name == ".lock" && relative != tpmRoot {
			return auxiliaryFailure("UNSUPPORTED_CAPABILITY", "TPM directory contains an ambiguous or unsupported control name")
		}
		child := relative + "/" + name
		if len(child) > 4096 || len(strings.Split(child, "/")) > auxiliaryDepthLimit {
			return auxiliaryFailure("UNSUPPORTED_CAPABILITY", "TPM entry exceeds pathname or depth bound")
		}
		// Observe type using O_PATH before opening a directory for enumeration.
		pin, openErr := auxiliaryOpen(int(s.root.Fd()), child, false, false)
		if openErr != nil {
			return openErr
		}
		state, statErr := auxiliaryStat(pin)
		pin.Close()
		if statErr != nil {
			return statErr
		}
		if name == ".lock" {
			err = s.member(ctx, child, "tpm-lock")
		} else if state.Mode&unix.S_IFMT == unix.S_IFDIR {
			err = s.walkTPM(ctx, child, tpmRoot)
		} else {
			err = s.member(ctx, child, "tpm")
		}
		if err != nil {
			return err
		}
	}
	after, err := auxiliaryMetadata(ctx, f)
	if err != nil {
		return err
	}
	if after != held.state {
		return auxiliaryFailure("SOURCE_CHANGED", "TPM directory membership changed during enumeration")
	}
	return nil
}

func (e AuxiliaryExecutor) scanAuxiliary(ctx context.Context, r Request, p Policy, permission AuxiliaryPermission, layout domain.ColdStateLayout) (_ *auxiliaryScan, err error) {
	rootPath := p.Roots[r.RootID]
	if !auxiliaryCleanPath(rootPath, true) {
		return nil, auxiliaryFailure("INVALID_INPUT", "canonical approved auxiliary root required")
	}
	s := &auxiliaryScan{permission: permission, rootOwner: e.rootOwnerUID, held: map[string]auxiliaryHeld{}, aliases: map[string]string{}, names: map[string]string{}}
	defer func() {
		if err != nil {
			s.close()
		}
	}()
	s.root, err = auxiliaryOpen(unix.AT_FDCWD, rootPath, true, false)
	if err != nil {
		return nil, err
	}
	rootState, err := auxiliaryMetadata(ctx, s.root)
	if err != nil {
		return nil, err
	}
	if rootState.UID != e.rootOwnerUID || rootState.Mode&0022 != 0 {
		return nil, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "approved auxiliary root must be administrator-owned and writable only by its owner")
	}
	s.inventory = AuxiliaryInventory{Version: 1, Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: r.ResourceID}, Fingerprint: r.Auxiliary.Fingerprint, Layout: layout, Root: AuxiliaryRoot{ID: r.RootID, Path: rootPath, State: rootState}, Directories: []AuxiliaryDirectory{}, Members: []AuxiliaryMember{}}
	s.aliases[rootState.Generation] = "."
	if nvram := layout.Firmware.NVRAM; nvram != nil {
		rel, relErr := auxiliaryRelative(rootPath, nvram.Path)
		if relErr != nil {
			return nil, relErr
		}
		if nvram.Format != "" && nvram.Format != "raw" && nvram.Format != "qcow2" {
			return nil, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "unsupported native NVRAM file format")
		}
		if err = s.member(ctx, rel, "nvram"); err != nil {
			return nil, err
		}
	}
	if tpm := layout.TPM; tpm != nil {
		rel, relErr := auxiliaryRelative(rootPath, tpm.SourcePath)
		if relErr != nil {
			return nil, relErr
		}
		if tpm.Version != "1.2" && tpm.Version != "2.0" || tpm.SourceType != "file" && tpm.SourceType != "dir" {
			return nil, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "explicit supported TPM file or directory source required")
		}
		before := len(s.inventory.Members)
		if err = s.parents(ctx, rel); err != nil {
			return nil, err
		}
		if tpm.SourceType == "file" {
			if filepath.Base(rel) == ".lock" {
				return nil, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "TPM file source cannot select a producer control lock")
			}
			err = s.member(ctx, rel, "tpm")
		} else {
			err = s.walkTPM(ctx, rel, rel)
		}
		if err != nil {
			return nil, err
		}
		if len(s.inventory.Members) == before {
			return nil, auxiliaryFailure("INCOMPLETE_BACKUP", "explicit TPM source has no state members")
		}
	}
	if len(s.inventory.Members) == 0 {
		return nil, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "native VM has no explicit auxiliary state members")
	}
	sort.Slice(s.inventory.Directories, func(i, j int) bool {
		return s.inventory.Directories[i].RelativePath < s.inventory.Directories[j].RelativePath
	})
	sort.Slice(s.inventory.Members, func(i, j int) bool { return s.inventory.Members[i].RelativePath < s.inventory.Members[j].RelativePath })
	for i := range s.inventory.Members {
		s.inventory.Members[i].ID = fmt.Sprintf("members/%03d", i)
	}
	raw, marshalErr := json.Marshal(s.inventory)
	if marshalErr != nil || len(raw) > auxiliaryMetadataLimit {
		return nil, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "auxiliary inventory exceeds metadata envelope bound")
	}
	if err = s.recheck(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *auxiliaryScan) recheck(ctx context.Context) error {
	paths := make([]string, 0, len(s.held))
	for relative := range s.held {
		paths = append(paths, relative)
	}
	// Recheck files before directories, deepest directory first. Membership
	// changes during child checks must still invalidate the containing tree.
	sort.Slice(paths, func(i, j int) bool {
		iDir := s.held[paths[i]].state.Mode&unix.S_IFMT == unix.S_IFDIR
		jDir := s.held[paths[j]].state.Mode&unix.S_IFMT == unix.S_IFDIR
		if iDir != jDir {
			return !iDir
		}
		if iDir && strings.Count(paths[i], "/") != strings.Count(paths[j], "/") {
			return strings.Count(paths[i], "/") > strings.Count(paths[j], "/")
		}
		return paths[i] < paths[j]
	})
	for _, relative := range paths {
		held := s.held[relative]
		current, err := auxiliaryMetadata(ctx, held.file)
		if err != nil {
			return err
		}
		f, err := auxiliaryOpen(int(s.root.Fd()), relative, held.state.Mode&unix.S_IFMT == unix.S_IFDIR, false)
		if err != nil {
			return err
		}
		fromPath, pathErr := auxiliaryMetadata(ctx, f)
		f.Close()
		if pathErr != nil {
			return pathErr
		}
		if current != held.state || fromPath != held.state {
			return auxiliaryFailure("SOURCE_CHANGED", "held auxiliary object or its rooted path changed")
		}
	}
	current, err := auxiliaryMetadata(ctx, s.root)
	if err != nil {
		return err
	}
	f, err := auxiliaryOpen(unix.AT_FDCWD, s.inventory.Root.Path, true, false)
	if err != nil {
		return err
	}
	fromPath, pathErr := auxiliaryMetadata(ctx, f)
	f.Close()
	if pathErr != nil {
		return pathErr
	}
	if current != s.inventory.Root.State || fromPath != s.inventory.Root.State {
		return auxiliaryFailure("SOURCE_CHANGED", "approved auxiliary root changed during observation")
	}
	return ctx.Err()
}
