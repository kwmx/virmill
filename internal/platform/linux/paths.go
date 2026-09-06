//go:build linux

package linux

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
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
