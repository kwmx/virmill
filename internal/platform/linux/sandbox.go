//go:build linux && amd64

package linux

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"virmill.local/core/internal/domain"
)

// ConfinedCommand mounts only runtime code, the chosen executable and a private
// workspace. Sockets, home, credentials and host /dev are never exposed.
func ConfinedCommand(ctx context.Context, executable, workspace string, args []string) (*exec.Cmd, func(), error) {
	return confinedCommand(ctx, executable, workspace, args, "", "")
}

// ConfinedPackageCommand exposes only a previously verified private package tree.
// The caller retains ownership of the tree for the entire process lifetime.
func ConfinedPackageCommand(ctx context.Context, directory, entrypoint, workspace string) (*exec.Cmd, func(), error) {
	if !filepath.IsAbs(directory) || !filepath.IsLocal(entrypoint) {
		return nil, nil, errors.New("absolute package directory and local entrypoint required")
	}
	if e := PrivateDir(directory); e != nil {
		return nil, nil, e
	}
	return confinedCommand(ctx, filepath.Join(directory, entrypoint), workspace, nil, directory, filepath.ToSlash(entrypoint))
}

func confinedCommand(ctx context.Context, executable, workspace string, args []string, packageDirectory, entrypoint string) (*exec.Cmd, func(), error) {
	if os.Getuid() == 0 {
		return nil, nil, errors.New("untrusted workers must never run as root")
	}
	bwrap, e := exec.LookPath("bwrap")
	if e != nil {
		return nil, nil, domain.Fail("UNSUPPORTED_CAPABILITY", "bubblewrap missing; confinement fails closed")
	}
	limit, e := exec.LookPath("prlimit")
	if e != nil {
		return nil, nil, domain.Fail("UNSUPPORTED_CAPABILITY", "prlimit missing; resource limits unavailable")
	}
	for _, p := range []string{executable, workspace} {
		if !filepath.IsAbs(p) {
			return nil, nil, errors.New("sandbox paths must be absolute")
		}
	}
	if e = PrivateDir(workspace); e != nil {
		return nil, nil, e
	}
	st, e := os.Lstat(executable)
	if e != nil || !st.Mode().IsRegular() {
		return nil, nil, errors.New("worker executable must be a regular file")
	}
	// libseccomp-compatible classic BPF. Reject other syscall architectures and x32;
	// deny sockets, mount operations, ptrace, keyring, BPF, perf and namespace changes.
	filters := []unix.SockFilter{{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4}, {Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: 1, K: unix.AUDIT_ARCH_X86_64}, {Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS}, {Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0}, {Code: unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K, Jf: 1, K: 0x40000000}, {Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_KILL_PROCESS}}
	for _, call := range []uint32{unix.SYS_SOCKET, unix.SYS_SOCKETPAIR, unix.SYS_CONNECT, unix.SYS_PTRACE, unix.SYS_MOUNT, unix.SYS_UMOUNT2, unix.SYS_PIVOT_ROOT, unix.SYS_SETNS, unix.SYS_UNSHARE, unix.SYS_BPF, unix.SYS_PERF_EVENT_OPEN, unix.SYS_KEYCTL, unix.SYS_ADD_KEY, unix.SYS_REQUEST_KEY, unix.SYS_USERFAULTFD} {
		filters = append(filters, unix.SockFilter{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: call}, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)})
	}
	filters = append(filters, unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW})
	var policy bytes.Buffer
	if e = binary.Write(&policy, binary.LittleEndian, filters); e != nil {
		return nil, nil, e
	}
	f, e := os.CreateTemp(workspace, ".seccomp-*")
	if e != nil {
		return nil, nil, e
	}
	cleanup := func() { f.Close(); os.Remove(f.Name()) }
	if _, e = f.Write(policy.Bytes()); e != nil {
		cleanup()
		return nil, nil, e
	}
	if _, e = f.Seek(0, 0); e != nil {
		cleanup()
		return nil, nil, e
	}
	argv := []string{"--unshare-all", "--die-with-parent", "--new-session", "--cap-drop", "ALL", "--clearenv", "--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin", "--symlink", "usr/lib64", "/lib64", "--symlink", "usr/lib", "/lib", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--dir", "/plugin"}
	command := "/plugin/executable"
	if packageDirectory != "" {
		argv = append(argv, "--ro-bind", packageDirectory, "/plugin")
		command = "/plugin/" + entrypoint
	} else {
		argv = append(argv, "--ro-bind", executable, "/plugin/executable")
	}
	argv = append(argv, "--bind", workspace, "/work", "--chdir", "/work", "--setenv", "PATH", "/usr/bin", "--setenv", "LANG", "C.UTF-8", "--setenv", "GOMEMLIMIT", "256MiB", "--seccomp", "3", "--", limit, "--as=2147483648", "--nproc=256", "--cpu=60", "--fsize=67108864", "--nofile=64", "--", command)
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, bwrap, argv...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	cmd.ExtraFiles = []*os.File{f}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return cmd, cleanup, nil
}
func ProbeSandbox(ctx context.Context, workspace string) error {
	cmd, close, e := ConfinedCommand(ctx, "/usr/bin/true", workspace, nil)
	if e != nil {
		return e
	}
	defer close()
	b, e := cmd.CombinedOutput()
	if e != nil {
		return domain.Fail("UNSUPPORTED_CAPABILITY", fmt.Sprintf("sandbox unavailable; normal plugin execution refused: %s", bytes.TrimSpace(b)))
	}
	return nil
}
