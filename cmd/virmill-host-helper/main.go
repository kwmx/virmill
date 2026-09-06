//go:build linux

package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"virmill.local/core/internal/helper"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("virmill-host-helper 0.0.0-dev; protocol virmill/v1")
		return
	}
	pid, _ := strconv.Atoi(os.Getenv("LISTEN_PID"))
	if os.Getuid() != 0 || os.Getenv("LISTEN_FDS") != "1" || pid != os.Getpid() {
		fmt.Fprintln(os.Stderr, "helper accepts only authenticated system socket activation")
		os.Exit(4)
	}
	f := os.NewFile(3, "system-activation")
	listener, e := net.FileListener(f)
	f.Close()
	if e == nil {
		e = helper.Serve(listener)
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
