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

// SessionBootReady reports whether a guest started on the per-user connection
// could reach QEMU at all, and what to advise when it could not. A libvirt
// socket already present for this user means some other program started the
// daemon, so it carries no restriction from here. With no such socket, the
// first program to use the connection forks the daemon itself, and that is
// only safe while this process may still transition SELinux domains on exec.
func SessionBootReady() (bool, string) {
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime != "" {
		for _, name := range []string{"libvirt/virtqemud-sock", "libvirt/libvirt-sock"} {
			if _, e := os.Stat(filepath.Join(runtime, name)); e == nil {
				return true, ""
			}
		}
	}
	if NoNewPrivileges() {
		return false, "systemctl --user enable --now virtqemud.socket"
	}
	return true, ""
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
