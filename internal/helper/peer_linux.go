//go:build linux && amd64

package helper

import (
	"errors"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"slices"
	"unsafe"
)

// PeerGroups uses the kernel's connection-time credential snapshot. It never
// resolves a PID through /proc or guesses supplementary groups through NSS.
func PeerGroups(conn *net.UnixConn) (*unix.Ucred, []uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	var cred *unix.Ucred
	var inner error
	groups := make([]uint32, 4096)
	size := uint32(len(groups) * 4)
	err = raw.Control(func(fd uintptr) {
		cred, inner = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if inner != nil {
			return
		}
		_, _, errno := unix.Syscall6(unix.SYS_GETSOCKOPT, fd, unix.SOL_SOCKET, unix.SO_PEERGROUPS, uintptr(unsafe.Pointer(&groups[0])), uintptr(unsafe.Pointer(&size)), 0)
		if errno != 0 {
			inner = errno
		}
	})
	if err != nil {
		return nil, nil, err
	}
	if inner != nil {
		return nil, nil, inner
	}
	if cred == nil || size%4 != 0 || size > uint32(len(groups)*4) {
		return nil, nil, errors.New("invalid bounded kernel peer group inventory")
	}
	groups = append(groups[:size/4], cred.Gid)
	slices.Sort(groups)
	groups = slices.Compact(groups)
	if len(groups) > 4096 {
		return nil, nil, errors.New("peer group inventory exceeds bound")
	}
	return cred, groups, nil
}
func CurrentGroups() ([]uint32, error) {
	native, err := os.Getgroups()
	if err != nil {
		return nil, err
	}
	groups := []uint32{uint32(os.Getegid())}
	for _, g := range native {
		if g < 0 {
			return nil, errors.New("invalid actor group")
		}
		groups = append(groups, uint32(g))
	}
	slices.Sort(groups)
	groups = slices.Compact(groups)
	if len(groups) > 4096 {
		return nil, errors.New("actor group inventory exceeds bound")
	}
	return groups, nil
}
