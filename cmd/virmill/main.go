package main

import (
	"fmt"
	"os"
	"time"
	"virmill.local/core/internal/domain"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/transport/local"
	"virmill.local/core/internal/ui/cli"
)

func main() {
	if os.Getuid() == 0 {
		fmt.Fprintln(os.Stderr, "Virmill must run as an ordinary user")
		os.Exit(4)
	}
	p, e := platform.UserPaths()
	socket := p.Socket
	if e != nil {
		socket = "/nonexistent/virmill/control.sock"
	}
	root := cli.New(local.Client{Socket: socket, Timeout: 30 * time.Second}, os.Stdout, os.Stderr)
	if e = root.Execute(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(domain.ExitCode(e))
	}
}
