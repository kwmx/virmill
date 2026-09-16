package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/auxiliary"
	backend "virmill.local/core/internal/backend/libvirt"
	"virmill.local/core/internal/coldcapture"
	"virmill.local/core/internal/creating"
	"virmill.local/core/internal/guestsetup"
	"virmill.local/core/internal/importing"
	"virmill.local/core/internal/localbackup"
	"virmill.local/core/internal/networkfirewall"
	"virmill.local/core/internal/networksettings"
	"virmill.local/core/internal/operations"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/plugins"
	"virmill.local/core/internal/storageaccess"
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
	// Decided here, before anything connects to libvirt: once this process has
	// connected it may have forked the very daemon that cannot start guests.
	service.SessionBoot = platform.SessionBootProbe()
	service.BridgeZones = platform.BridgeFirewallZones
	service.HostPrefixes = platform.ObserveHostNetworkPrefixes
	service.NetworkAllocation = func(ctx context.Context) (network.AllocationConfig, error) {
		return networksettings.Load(ctx, p.Config)
	}
	importing.Register(service)
	creating.Register(service, p.Cache)
	storageaccess.Register(service, p.Config)
	auxiliary.Register(service, p.Config)
	coldcapture.Register(service, p.Data, p.Cache, p.Config)
	localbackup.Register(service, p.Data, p.Cache)
	guestsetup.Register(service)
	networkfirewall.Register(service, p.Config)
	if e = platform.PrivateDir(p.Cache); e != nil {
		return e
	}
	sdk := os.Getenv("VIRMILL_SDK_DIRECTORY")
	if sdk == "" {
		sdk = "/usr/share/virmill/sdk/go"
	}
	if e = plugins.Register(service, p.Cache, p.Data, sdk); e != nil {
		return e
	}
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
