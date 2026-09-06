//go:build linux

package store

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
)

func lockDatabase(path string) (*os.File, error) {
	fd, e := unix.Open(path+".lock", unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), "journal singleton")
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("another coordinator has this journal open")
	}
	return f, nil
}
