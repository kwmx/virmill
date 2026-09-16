//go:build linux

package linux

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

type Paths struct{ Config, State, Data, Cache, Runtime, Socket string }

func UserPaths() (Paths, error) {
	home, e := os.UserHomeDir()
	if e != nil {
		return Paths{}, e
	}
	get := func(key, def string) string {
		if s := os.Getenv(key); s != "" {
			return s
		}
		return filepath.Join(home, def)
	}
	p := Paths{Config: filepath.Join(get("XDG_CONFIG_HOME", ".config"), "virmill"), State: filepath.Join(get("XDG_STATE_HOME", ".local/state"), "virmill"), Data: filepath.Join(get("XDG_DATA_HOME", ".local/share"), "virmill"), Cache: filepath.Join(get("XDG_CACHE_HOME", ".cache"), "virmill")}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		return p, errors.New("XDG_RUNTIME_DIR is required for the private coordinator")
	}
	for _, path := range []string{p.Config, p.State, p.Data, p.Cache, runtime} {
		if !filepath.IsAbs(path) {
			return p, errors.New("XDG directories must be absolute")
		}
	}
	p.Runtime = filepath.Join(runtime, "virmill")
	p.Socket = filepath.Join(p.Runtime, "control.sock")
	return p, nil
}

// NoNewPrivileges reports whether this process carries the no-new-privileges
// bit, which systemd sets from NoNewPrivileges=yes and which children inherit.
// A process that has it cannot transition SELinux domains on exec, so a libvirt
// daemon forked from here could never execute QEMU. An unreadable status file
// reports false: the caller must not refuse work on a host it cannot inspect.
func NoNewPrivileges() bool {
	data, e := os.ReadFile("/proc/self/status")
	if e != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if value, ok := strings.CutPrefix(line, "NoNewPrivs:"); ok {
			return strings.TrimSpace(value) == "1"
		}
	}
	return false
}

// SessionLibvirtAdvice is what to do when this coordinator started with no
// per-user libvirt socket. Virmill ships that socket and its own service asks for
// it (ADR 0064), so restarting the coordinator starts the socket first and lets
// it decide again. Where the socket unit does not apply, the host check names
// the command for that host.
const SessionLibvirtAdvice = "restart Virmill's coordinator so it starts your own libvirt socket: systemctl --user restart virmilld.service (if that does not help, run virmill doctor)"

// SessionBootProbe decides once whether guests started on the per-user
// connection can reach QEMU, and returns that fixed verdict.
//
// It must be called before this process connects to libvirt, and the verdict
// must not be recomputed afterwards. With no per-user socket, the first program
// to use the connection forks the daemon as its own child; started from a
// service with no-new-privileges the daemon can never transition SELinux
// domains on exec, so it can never execute QEMU. Asking later always answers
// "a socket exists", because the unusable daemon just created it: the very
// situation to refuse looks identical to a healthy one. A socket present
// beforehand means some other program owns the daemon, which carries no
// restriction from here.
func SessionBootProbe() func() (bool, string) {
	return sessionBootProbe(os.Getenv("XDG_RUNTIME_DIR"), NoNewPrivileges())
}

func sessionBootProbe(runtime string, restricted bool) func() (bool, string) {
	ready := !restricted
	if runtime != "" && !ready {
		for _, name := range []string{"libvirt/virtqemud-sock", "libvirt/libvirt-sock"} {
			if _, e := os.Stat(filepath.Join(runtime, name)); e == nil {
				ready = true
				break
			}
		}
	}
	return func() (bool, string) {
		if ready {
			return true, ""
		}
		return false, SessionLibvirtAdvice
	}
}
func PrivateDir(path string) error {
	if e := os.MkdirAll(path, 0700); e != nil {
		return e
	}
	s, e := os.Lstat(path)
	if e != nil {
		return e
	}
	st, ok := s.Sys().(*syscall.Stat_t)
	if !ok || !s.IsDir() || s.Mode().Perm() != 0700 || st.Uid != uint32(os.Getuid()) {
		return errors.New("private directory must be owned by UID " + strconv.Itoa(os.Getuid()) + " with mode 0700 and cannot be a symlink")
	}
	return nil
}
