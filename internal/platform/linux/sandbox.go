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
	"sort"
	"strings"
	"syscall"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
)

// ConfinedCommand mounts only runtime code, the chosen executable and a private
// workspace. Sockets, home, credentials and host /dev are never exposed.
func ConfinedCommand(ctx context.Context, executable, workspace string, args []string) (*exec.Cmd, func(), error) {
	return confinedCommand(ctx, executable, workspace, args, "", "", "", nil, nil, 64<<20)
}

// ConfinedSeedCommand limits the fixed ISO generator to a private workspace
// and 16 MiB per output file. It receives no original source or device mounts.
func ConfinedSeedCommand(ctx context.Context, workspace string, args []string) (*exec.Cmd, func(), error) {
	return confinedCommand(ctx, "/usr/bin/xorriso", workspace, args, "", "", "", nil, nil, 16<<20)
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
	return confinedCommand(ctx, filepath.Join(directory, entrypoint), workspace, nil, directory, filepath.ToSlash(entrypoint), "", nil, nil, 64<<20)
}

// ConfinedDiskCommand gives the fixed system image tool read-only access to a
// private imported source tree and write access only to one output workspace.
// The approved per-file output limit does not change plugin worker limits.
func ConfinedDiskCommand(ctx context.Context, source, workspace string, args []string, maximumFileBytes int64) (*exec.Cmd, func(), error) {
	if maximumFileBytes < 1 || maximumFileBytes > 1<<40 {
		return nil, nil, errors.New("invalid disk worker output bound")
	}
	if e := PrivateDir(source); e != nil {
		return nil, nil, e
	}
	return confinedCommand(ctx, "/usr/bin/qemu-img", workspace, args, "", "", source, nil, nil, maximumFileBytes)
}

// ConfinedDiskFileCommand exposes one held read-only regular file at /source/image.
// It never exposes the parent directory or follows dependencies outside that file.
// The caller must retain the descriptor until cmd.Wait returns.
func ConfinedDiskFileCommand(ctx context.Context, source *os.File, workspace string, args []string) (*exec.Cmd, func(), error) {
	if source == nil {
		return nil, nil, errors.New("held source file required")
	}
	return confinedCommand(ctx, "/usr/bin/qemu-img", workspace, args, "", "", "", source, nil, 64<<20)
}

type DiskSourceFile struct {
	Path string
	File *os.File
}

// ConfinedDiskFilesCommand exposes exactly the selected held files, under their
// reviewed relative names. It neither copies nor mounts the original directory.
// Keep all descriptors open until Wait returns. Plugin mounts are unchanged.
func ConfinedDiskFilesCommand(ctx context.Context, sources []DiskSourceFile, workspace string, args []string, maximumFileBytes int64) (*exec.Cmd, func(), error) {
	if len(sources) == 0 || maximumFileBytes < 1 || maximumFileBytes > 1<<40 {
		return nil, nil, errors.New("bounded disk sources and output required")
	}
	return confinedCommand(ctx, "/usr/bin/qemu-img", workspace, args, "", "", "", nil, sources, maximumFileBytes)
}

func validateDiskSources(sources []DiskSourceFile) ([]string, error) {
	if len(sources) > 10000 {
		return nil, errors.New("too many selected image files")
	}
	names, parents := map[string]bool{}, map[string]bool{}
	bytes := 0
	for _, source := range sources {
		bytes += len(source.Path)
		if source.File == nil || importer.SafePath(source.Path) != nil || filepath.Clean(source.Path) != source.Path || names[source.Path] || bytes > 1<<20 {
			return nil, errors.New("unique bounded local source names and open files required")
		}
		names[source.Path] = true
		st, err := source.File.Stat()
		flags, flagErr := unix.FcntlInt(source.File.Fd(), unix.F_GETFL, 0)
		if err != nil || flagErr != nil || !st.Mode().IsRegular() || flags&unix.O_ACCMODE != unix.O_RDONLY || flags&unix.O_PATH != 0 {
			return nil, errors.New("selected source must be a held read-only regular file")
		}
		parent := filepath.Dir(source.Path)
		for parent != "." {
			parents[parent] = true
			parent = filepath.Dir(parent)
		}
	}
	paths := []string{}
	for parent := range parents {
		if names[parent] {
			return nil, errors.New("selected file collides with a source directory")
		}
		paths = append(paths, parent)
	}
	sort.Slice(paths, func(i, j int) bool {
		a, b := strings.Count(paths[i], "/"), strings.Count(paths[j], "/")
		if a != b {
			return a < b
		}
		return paths[i] < paths[j]
	})
	return paths, nil
}

func confinedCommand(ctx context.Context, executable, workspace string, args []string, packageDirectory, entrypoint, sourceDirectory string, sourceFile *os.File, sourceFiles []DiskSourceFile, maximumFileBytes int64) (*exec.Cmd, func(), error) {
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
	if sourceFile != nil {
		st, err := sourceFile.Stat()
		flags, flagErr := unix.FcntlInt(sourceFile.Fd(), unix.F_GETFL, 0)
		if err != nil || flagErr != nil || !st.Mode().IsRegular() || flags&unix.O_ACCMODE != unix.O_RDONLY || flags&unix.O_PATH != 0 || sourceDirectory != "" || packageDirectory != "" {
			return nil, nil, domain.Fail("INVALID_INPUT", "single-file image confinement requires one held read-only regular file")
		}
	}
	var sourceParents []string
	if len(sourceFiles) > 0 {
		if sourceFile != nil || sourceDirectory != "" || packageDirectory != "" {
			return nil, nil, errors.New("one source exposure mode required")
		}
		sourceParents, e = validateDiskSources(sourceFiles)
		if e != nil {
			return nil, nil, e
		}
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
	if sourceDirectory != "" {
		argv = append(argv, "--ro-bind", sourceDirectory, "/source")
	}
	if sourceFile != nil {
		argv = append(argv, "--dir", "/source", "--ro-bind-fd", "4", "/source/image")
	}
	if len(sourceFiles) > 0 {
		argv = append(argv, "--dir", "/source")
		for _, parent := range sourceParents {
			argv = append(argv, "--dir", "/source/"+parent)
		}
		for i, source := range sourceFiles {
			argv = append(argv, "--ro-bind-fd", fmt.Sprint(4+i), "/source/"+source.Path)
		}
	}
	cpuSeconds := 60
	openFiles := 64
	if sourceDirectory != "" || len(sourceFiles) > 0 {
		cpuSeconds = 1800
		openFiles = 1024 // split-image descriptors may hold hundreds of extents
	}
	argv = append(argv, "--bind", workspace, "/work", "--chdir", "/work", "--setenv", "PATH", "/usr/bin", "--setenv", "LANG", "C.UTF-8", "--setenv", "GOMEMLIMIT", "256MiB", "--seccomp", "3", "--", limit, "--as=2147483648", "--nproc=256", fmt.Sprintf("--cpu=%d", cpuSeconds), fmt.Sprintf("--fsize=%d", maximumFileBytes), fmt.Sprintf("--nofile=%d", openFiles), "--", command)
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, bwrap, argv...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	cmd.ExtraFiles = []*os.File{f}
	if sourceFile != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, sourceFile)
	}
	for _, source := range sourceFiles {
		cmd.ExtraFiles = append(cmd.ExtraFiles, source.File)
	}
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
