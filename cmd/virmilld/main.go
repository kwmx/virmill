package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"virmill.local/core/internal/app"
	backend "virmill.local/core/internal/backend/libvirt"
	"virmill.local/core/internal/operations"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/plugins"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/transport/local"
)

func run() error {
	if os.Getuid() == 0 {
		return fmt.Errorf("virmilld must run as an ordinary user")
	}
	p, e := platform.UserPaths()
	if e != nil {
		return e
	}
	if e = platform.PrivateDir(p.State); e != nil {
		return e
	}
	db, e := store.Open(filepath.Join(p.State, "journal.db"))
	if e != nil {
		return e
	}
	defer db.Close()
	engine := operations.New(db)
	defer engine.Close()
	service := app.New(&backend.Provider{}, engine)
	service.Inspector = platform.Doctor
	if e = platform.PrivateDir(p.Cache); e != nil {
		return e
	}
	plugins.Register(service, p.Cache)
	server, e := local.Listen(p.Socket, service)
	if e != nil {
		return e
	}
	defer server.Close()
	if e = engine.Recover(); e != nil {
		return e
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(ch)
	go func() { <-ch; server.Close() }()
	return server.Serve()
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
