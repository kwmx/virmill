//go:build linux

package helper

import (
	"bufio"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"virmill.local/core/internal/wire"
)

const PolicyPath = "/etc/virmill/helper-policy.json"
const JournalPath = "/var/lib/virmill-host-helper"

func ownedRoot(path string, directory bool) error {
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok || s.Uid != 0 || st.Mode().Perm()&0022 != 0 || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("helper policy/state/root must be root-owned and not writable by group/other")
	}
	if directory && !st.IsDir() {
		return errors.New("expected root-owned directory")
	}
	if !directory && !st.Mode().IsRegular() {
		return errors.New("expected root-owned regular file")
	}
	return nil
}
func Execute(r Request, p Policy) error {
	root := p.Roots[r.RootID]
	if !filepath.IsAbs(root) {
		return errors.New("policy root must be absolute")
	}
	rootFD, err := unix.Openat2(unix.AT_FDCWD, root, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return err
	}
	storage := os.NewFile(uintptr(rootFD), "approved storage root")
	defer storage.Close()
	var rootStat unix.Stat_t
	if err = unix.Fstat(rootFD, &rootStat); err != nil {
		return err
	}
	if rootStat.Uid != 0 || rootStat.Mode&0022 != 0 {
		return errors.New("opened storage root is not root-owned/private to administrators")
	}
	if e := ownedRoot(JournalPath, true); e != nil {
		return e
	}
	journal, e := os.OpenRoot(JournalPath)
	if e != nil {
		return e
	}
	defer journal.Close()
	intent, e := journal.OpenFile(r.JobID+".json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("helper job already exists or journal unavailable; inspect recovery before retry")
	}
	b, e := json.Marshal(r)
	if e == nil {
		_, e = intent.Write(b)
	}
	if e == nil {
		e = intent.Sync()
	}
	intent.Close()
	if e != nil {
		return e
	}
	dir, e := os.Open(JournalPath)
	if e != nil {
		return e
	}
	e = dir.Sync()
	dir.Close()
	if e != nil {
		return e
	}
	if e = unix.Mkdirat(rootFD, r.ResourceID, 0700); e != nil {
		return e
	}
	targetFD, e := unix.Openat2(rootFD, r.ResourceID, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if e != nil {
		return e
	}
	target := os.NewFile(uintptr(targetFD), "new managed directory")
	defer target.Close()
	if e = target.Chown(int(r.ActorUID), -1); e != nil {
		return e
	}
	if e = target.Sync(); e != nil {
		return e
	}
	if e = storage.Sync(); e != nil {
		return e
	}
	done, e := journal.OpenFile(r.JobID+".complete", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	e = done.Sync()
	done.Close()
	return e
}
func Serve(listener net.Listener) error {
	if os.Getuid() != 0 {
		return errors.New("privileged helper requires authenticated system activation")
	}
	if e := ownedRoot(PolicyPath, false); e != nil {
		return e
	}
	b, e := os.ReadFile(PolicyPath)
	if e != nil {
		return e
	}
	var p Policy
	if e = wire.Decode(b, &p); e != nil {
		return e
	}
	for {
		conn, e := listener.Accept()
		if e != nil {
			return e
		}
		func() {
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(10 * time.Second))
			c, ok := conn.(*net.UnixConn)
			if !ok {
				return
			}
			raw, e := c.SyscallConn()
			if e != nil {
				return
			}
			var cred *unix.Ucred
			var inner error
			e = raw.Control(func(fd uintptr) { cred, inner = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
			if e != nil || inner != nil {
				return
			}
			frame, e := wire.ReadFrame(bufio.NewReader(conn))
			var r Request
			if e == nil {
				e = wire.Decode(frame, &r)
			}
			if e == nil {
				e = Authorize(cred.Uid, r, p, time.Now())
			}
			if e == nil {
				e = Execute(r, p)
			}
			result := map[string]any{"apiVersion": "virmill/v1", "success": e == nil}
			if e != nil {
				result["error"] = e.Error()
			}
			json.NewEncoder(conn).Encode(result)
		}()
	}
}
