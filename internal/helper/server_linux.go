//go:build linux && amd64

package helper

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"virmill.local/core/internal/domain"
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
func Serve(listener net.Listener, backend domain.ManagedFileAccessBackend) error {
	if os.Getuid() != 0 {
		return errors.New("privileged helper requires authenticated system activation")
	}
	if _, err := loadPolicy(); err != nil {
		return err
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		func() {
			defer conn.Close()
			deadline := time.Now().Add(10 * time.Second)
			conn.SetDeadline(deadline)
			ctx, cancel := context.WithDeadline(context.Background(), deadline)
			defer cancel()
			c, ok := conn.(*net.UnixConn)
			if !ok {
				return
			}
			cred, groups, err := PeerGroups(c)
			if err != nil {
				return
			}
			frame, err := wire.ReadFrame(bufio.NewReader(conn))
			var r Request
			if err == nil {
				err = wire.Decode(frame, &r)
			}
			// Reload revocations from the administrator's held, validated policy file.
			p, policyErr := loadPolicy()
			if err == nil {
				err = policyErr
			}
			if err == nil {
				if authErr := Authorize(cred.Uid, r, p, time.Now()); authErr != nil {
					err = domain.Fail("PERMISSION_DENIED", authErr.Error())
				}
			}
			var access json.RawMessage
			var auxiliary *AuxiliaryResponse
			var networkResponse *NetworkResponse
			if err == nil {
				if r.Operation == "network.ipv6-filter" {
					provider, ok := backend.(domain.NetworkCreationProvider)
					if !ok {
						err = domain.Fail("UNSUPPORTED_CAPABILITY", "helper native network inspection unavailable")
					} else {
						// A bounded separate deadline covers the fixed runtime/permanent
						// firewalld requests after authentication has completed.
						networkCtx, networkCancel := context.WithTimeout(context.Background(), 30*time.Second)
						_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
						var observed NetworkResponse
						observed, err = (NetworkExecutor{Backend: provider}).Execute(networkCtx, r, p)
						networkCancel()
						if err == nil {
							networkResponse = &observed
						}
					}
				} else if r.Operation == "state.auxiliary" {
					auxiliary, err = inspectAuxiliaryRequest(ctx, backend, r, p)
				} else if r.Operation == "storage.prepare-directory" {
					err = Execute(r, p)
				} else {
					var result AccessResult
					result, err = (AccessExecutor{Backend: backend}).Execute(ctx, r, p, groups)
					if err == nil {
						access, err = json.Marshal(result)
					}
				}
			}
			result := Response{APIVersion: "virmill/v1", Success: err == nil, Access: access, Auxiliary: auxiliary, Network: networkResponse}
			if err != nil {
				result.Access, result.Auxiliary = nil, nil
				result.Error = err.Error()
				result.ErrorCode = "OPERATION_FAILED"
				var typed *domain.Error
				if errors.As(err, &typed) {
					result.ErrorCode = typed.Code
				}
			}
			json.NewEncoder(conn).Encode(result)
		}()
	}
}
