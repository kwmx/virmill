//go:build linux && amd64

// Package seed creates NoCloud ISO files using a fixed confined system generator.
// Guest provisioning is never inferred from successful seed construction.
package seed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"virmill.local/core/internal/domain"
	platform "virmill.local/core/internal/platform/linux"
)

const MaxISOBytes int64 = 16 << 20
const MaxContentBytes = 1 << 20

var memberNames = []string{"meta-data", "network-config", "user-data"}

type Identity struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}
type Artifact struct {
	Path         string            `json:"path"`
	FileBytes    int64             `json:"fileBytes"`
	SHA256       string            `json:"sha256"`
	Members      map[string]string `json:"members"`
	Verification string            `json:"verification"`
}
type Tool struct{}

func (Tool) Identity(ctx context.Context) (Identity, error) {
	id := Identity{Path: "/usr/bin/xorriso"}
	f, err := os.OpenFile(id.Path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return id, domain.Fail("UNSUPPORTED_CAPABILITY", "system xorriso is unavailable")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return id, err
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.Mode().IsRegular() || owner.Uid != 0 || st.Mode().Perm()&0022 != 0 {
		return id, domain.Fail("PERMISSION_DENIED", "seed generator must be a root-owned system executable not writable by other users")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return id, err
	}
	id.SHA256 = hex.EncodeToString(h.Sum(nil))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, id.Path, "-no_rc", "-version")
	cmd.Env = []string{"PATH=/usr/bin", "LC_ALL=C"}
	out := &boundedOutput{limit: 64 << 10}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err = cmd.Run(); err != nil {
		return id, err
	}
	id.Version = strings.SplitN(strings.TrimSpace(out.String()), "\n", 2)[0]
	if !strings.HasPrefix(id.Version, "xorriso ") {
		return id, domain.Fail("UNSUPPORTED_CAPABILITY", "unexpected system seed generator identity")
	}
	return id, nil
}
func validateMembers(files map[string][]byte) error {
	if len(files) != len(memberNames) {
		return domain.Fail("INVALID_INPUT", "NoCloud requires exactly user-data, meta-data and explicit network-config")
	}
	total := 0
	for _, name := range memberNames {
		b, ok := files[name]
		total += len(b)
		if !ok || len(b) == 0 || !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 || total > MaxContentBytes {
			return domain.Fail("INVALID_INPUT", "bounded UTF-8 NoCloud member content required")
		}
	}
	if !bytes.HasPrefix(files["user-data"], []byte("#cloud-config\n")) {
		return domain.Fail("INVALID_INPUT", "only structured cloud-config user data is supported")
	}
	return nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Build requires a fresh private workspace. Fixed filenames and argv prevent
// caller data from becoming generator commands. Timestamps are fixed so preview
// and execution can bind the exact same ISO bytes to a plan before host effects.
func (Tool) Build(ctx context.Context, workspace string, files map[string][]byte) (Artifact, error) {
	var out Artifact
	if err := validateMembers(files); err != nil {
		return out, err
	}
	if err := platform.PrivateDir(workspace); err != nil {
		return out, err
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return out, err
	}
	defer root.Close()
	for _, name := range []string{"source", "seed.iso", "readback"} {
		if _, err = root.Lstat(name); !os.IsNotExist(err) {
			return out, domain.Fail("STALE_PLAN", "seed workspace has existing generator output or input")
		}
	}
	if err = root.Mkdir("source", 0700); err != nil {
		return out, err
	}
	members := map[string]string{}
	for _, name := range memberNames {
		f, e := root.OpenFile("source/"+name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return out, e
		}
		_, e = f.Write(files[name])
		if e == nil {
			e = f.Sync()
		}
		if e == nil {
			e = f.Chmod(0400)
		}
		if closeErr := f.Close(); e == nil {
			e = closeErr
		}
		if e != nil {
			return out, e
		}
		members[name] = digest(files[name])
	}
	if err = run(ctx, workspace, "-no_rc", "-as", "mkisofs", "-quiet", "-r", "-J", "-V", "CIDATA", "--modification-date=2000010100000000", "--set_all_file_dates", "2000010100000000", "-o", "/work/seed.iso", "/work/source"); err != nil {
		return out, err
	}
	f, err := root.OpenFile("seed.iso", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return out, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return out, err
	}
	if !st.Mode().IsRegular() || st.Size() < 17*2048 || st.Size() > MaxISOBytes || st.Size()%2048 != 0 {
		f.Close()
		return out, domain.Fail("RECOVERY_REQUIRED", "seed generator produced an invalid bounded ISO")
	}
	var descriptor [2048]byte
	_, err = f.ReadAt(descriptor[:], 16*2048)
	if err != nil {
		f.Close()
		return out, err
	}
	if descriptor[0] != 1 || string(descriptor[1:6]) != "CD001" || descriptor[6] != 1 || strings.TrimSpace(string(descriptor[40:72])) != "CIDATA" {
		f.Close()
		return out, domain.Fail("RECOVERY_REQUIRED", "generated NoCloud volume label or primary descriptor differs")
	}
	h := sha256.New()
	_, err = io.Copy(h, io.LimitReader(f, MaxISOBytes+1))
	if err == nil {
		err = f.Chmod(0400)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return out, err
	}
	// The reader is also confined. A generated ISO can never extract host files or
	// special devices. No worker output containing seed content enters a job log.
	readbackErr := run(ctx, workspace, "-no_rc", "-abort_on", "FAILURE", "-return_with", "SORRY", "32", "-indev", "/work/seed.iso", "-osirrox", "on", "-extract", "/", "/work/readback", "-rollback_end")
	// Rock Ridge restores the ISO root as read-only. Restore write permission on
	// this private generated directory so cleanup can remove its files, including
	// when the reader exits after a partial extraction.
	dir, openErr := root.OpenFile("readback", os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if openErr == nil {
		chmodErr := dir.Chmod(0700)
		dir.Close()
		if chmodErr != nil {
			return out, chmodErr
		}
	} else if readbackErr == nil {
		return out, openErr
	}
	if readbackErr != nil {
		return out, readbackErr
	}
	entries, err := os.ReadDir(filepath.Join(workspace, "readback"))
	if err != nil {
		return out, err
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if !equalNames(names, memberNames) {
		return out, domain.Fail("RECOVERY_REQUIRED", "generated seed has unexpected members")
	}
	for _, name := range memberNames {
		f, e := root.OpenFile("readback/"+name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if e != nil {
			return out, e
		}
		st, e := f.Stat()
		if e != nil {
			f.Close()
			return out, e
		}
		if !st.Mode().IsRegular() || st.Size() != int64(len(files[name])) {
			f.Close()
			return out, domain.Fail("RECOVERY_REQUIRED", "seed readback member has unexpected type or size")
		}
		b, e := io.ReadAll(io.LimitReader(f, MaxContentBytes+1))
		f.Close()
		if e != nil {
			return out, e
		}
		if !bytes.Equal(b, files[name]) {
			return out, domain.Fail("RECOVERY_REQUIRED", "seed readback content differs")
		}
	}
	return Artifact{Path: "seed.iso", FileBytes: st.Size(), SHA256: hex.EncodeToString(h.Sum(nil)), Members: members, Verification: "iso9660-CIDATA+exact-members+content-readback"}, nil
}
func equalNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type boundedOutput struct {
	bytes.Buffer
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("seed tool output exceeds bound")
	}
	return b.Buffer.Write(p)
}

var slots = make(chan struct{}, 2)

func run(ctx context.Context, workspace string, args ...string) error {
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-slots }()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd, cleanup, err := platform.ConfinedSeedCommand(ctx, workspace, args)
	if err != nil {
		return err
	}
	defer cleanup()
	cmd.Stdout = &boundedOutput{limit: 64 << 10}
	cmd.Stderr = &boundedOutput{limit: 64 << 10}
	cmd.WaitDelay = 2 * time.Second
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("confined NoCloud ISO generation/readback failed (content output withheld): %w", err)
	}
	return nil
}
