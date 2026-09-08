//go:build linux && amd64

package restic

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
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
)

func openExecutable(ctx context.Context) (*os.File, Identity, fileidentity.Identity, error) {
	var empty Identity
	var no fileidentity.Identity
	if os.Geteuid() == 0 {
		return nil, empty, no, domain.Fail("PERMISSION_DENIED", "restic adapter requires an ordinary user")
	}
	if err := ctx.Err(); err != nil {
		return nil, empty, no, err
	}
	f, id, err := fileidentity.Open(executable, false, false)
	if err != nil {
		return nil, empty, no, domain.Fail("UNSUPPORTED_CAPABILITY", "administrator-installed /usr/bin/restic is unavailable")
	}
	for _, path := range []string{"/", "/usr", "/usr/bin"} {
		var st unix.Stat_t
		if err := unix.Lstat(path, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Mode&0022 != 0 {
			f.Close()
			return nil, empty, no, domain.Fail("PERMISSION_DENIED", "restic executable ancestors must be administrator-owned ordinary directories")
		}
	}
	var st unix.Stat_t
	err = unix.Fstat(int(f.Fd()), &st)
	if err != nil || st.Uid != 0 || st.Mode&0022 != 0 || st.Mode&07000 != 0 || st.Mode&0111 == 0 || id.Size == 0 || id.Size > 256<<20 {
		f.Close()
		return nil, empty, no, domain.Fail("PERMISSION_DENIED", "restic must be a bounded administrator-owned executable")
	}
	r, err := os.OpenFile(fmt.Sprintf("/proc/self/fd/%d", f.Fd()), os.O_RDONLY|unix.O_CLOEXEC, 0)
	f.Close()
	if err != nil {
		return nil, empty, no, domain.Fail("PERMISSION_DENIED", "held restic executable cannot be read")
	}
	var magic [4]byte
	if _, err = r.ReadAt(magic[:], 0); err != nil || !bytes.Equal(magic[:], []byte{0x7f, 'E', 'L', 'F'}) {
		r.Close()
		return nil, empty, no, domain.Fail("PERMISSION_DENIED", "restic must be a native ELF executable")
	}
	h := sha256.New()
	buf := make([]byte, 128<<10)
	for {
		if err = ctx.Err(); err != nil {
			break
		}
		var n int
		n, err = r.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	if err != io.EOF {
		r.Close()
		if ctx.Err() != nil {
			return nil, empty, no, ctx.Err()
		}
		return nil, empty, no, domain.Fail("OPERATION_FAILED", "restic executable identity could not be read")
	}
	fresh, e := fileidentity.InspectFile(r, false)
	if e != nil || fresh != id {
		r.Close()
		return nil, empty, no, domain.Fail("SOURCE_CHANGED", "restic executable changed while hashing")
	}
	return r, Identity{Path: executable, SHA256: hex.EncodeToString(h.Sum(nil))}, id, nil
}

type outputBound struct {
	mu                sync.Mutex
	data              bytes.Buffer
	size, limit       int
	discard, overflow bool
	cancel            context.CancelFunc
}

func (b *outputBound) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) > b.limit-b.size {
		b.overflow = true
		b.cancel()
		return len(p), nil
	}
	b.size += len(p)
	if !b.discard {
		_, _ = b.data.Write(p)
	}
	return len(p), nil
}

// runProcess is also exercised directly with the test binary: no test needs an
// installed restic or a relaxed production executable ownership check.
func runProcess(ctx context.Context, binary *os.File, in invocation) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/proc/self/fd/3", in.args...)
	cmd.ExtraFiles = append([]*os.File{binary}, in.files...)
	cmd.Env = []string{"PATH=/usr/bin", "LC_ALL=C", "TZ=UTC"}
	cmd.Dir = in.dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = 2 * time.Second
	out := &outputBound{limit: maxOutput, cancel: cancel}
	diagnostic := &outputBound{limit: maxDiagnostic, discard: true, cancel: cancel}
	cmd.Stdout, cmd.Stderr = out, diagnostic
	err := cmd.Run()
	if out.overflow || diagnostic.overflow {
		return nil, domain.Fail("OPERATION_FAILED", "restic output exceeded its bound; effects require observation")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			switch exit.ExitCode() {
			case 3:
				return nil, domain.Fail("INCOMPLETE_BACKUP", "restic reported a partial operation; effects require observation")
			case 10:
				return nil, domain.Fail("NOT_FOUND", "restic repository is unavailable")
			case 11:
				return nil, domain.Fail("RESOURCE_BUSY", "restic repository is locked")
			case 12:
				return nil, domain.Fail("PERMISSION_DENIED", "restic repository credential was refused")
			}
		}
		return nil, domain.Fail("OPERATION_FAILED", "restic command failed; diagnostics withheld and effects require observation")
	}
	return out.data.Bytes(), nil
}

func (t Tool) invoke(ctx context.Context, in invocation) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t.command != nil {
		return t.command(ctx, in)
	}
	f, _, id, err := openExecutable(ctx)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := runProcess(ctx, f, in)
	if err != nil {
		return nil, err
	}
	after, e := fileidentity.InspectFile(f, false)
	path, e2 := fileidentity.Observe(executable, false)
	if e != nil || e2 != nil || after != id || path != id {
		return nil, domain.Fail("SOURCE_CHANGED", "restic executable identity changed during operation")
	}
	return b, nil
}

func (Tool) Identity(ctx context.Context) (Identity, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	f, id, stamp, err := openExecutable(ctx)
	if err != nil {
		return Identity{}, err
	}
	defer f.Close()
	b, err := runProcess(ctx, f, invocation{args: []string{"version"}, label: "version"})
	if err != nil {
		return Identity{}, err
	}
	s := strings.TrimSuffix(string(b), "\n")
	if len(s) > 512 || !strings.HasPrefix(s, "restic ") || strings.ContainsAny(s, "\r\n\x00\x1b") {
		return Identity{}, domain.Fail("UNSUPPORTED_CAPABILITY", "restic version output is unrecognized")
	}
	fresh, e := fileidentity.InspectFile(f, false)
	path, e2 := fileidentity.Observe(executable, false)
	if e != nil || e2 != nil || fresh != stamp || path != stamp {
		return Identity{}, domain.Fail("SOURCE_CHANGED", "restic executable changed during identification")
	}
	id.Version = s
	return id, nil
}
