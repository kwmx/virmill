//go:build linux && amd64

package linux

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSingleFileSandboxRefusesWriteHandles(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "source")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, err = ConfinedDiskFileCommand(context.Background(), f, t.TempDir(), nil); err == nil {
		t.Fatal("writable handle accepted")
	}
	if _, _, err = ConfinedDiskFileCommand(context.Background(), nil, t.TempDir(), nil); err == nil {
		t.Fatal("nil handle accepted")
	}
}

func TestRealSingleFileSandboxHidesSiblingsAndClosesDescriptor(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit generated-file namespace probe required")
	}
	dir, workspace := t.TempDir(), t.TempDir()
	if err := os.Chmod(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	name, secret := filepath.Join(dir, "selected"), filepath.Join(dir, "sibling")
	if err := os.WriteFile(name, []byte("selected-data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("hidden-data"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	probe := filepath.Join(t.TempDir(), "probe")
	// This executable is a generated fixture for the private constructor only.
	// The exported disk adapter always executes the fixed system qemu-img.
	script := "#!/usr/bin/sh\nset -eu\ntest \"$(cat /source/image)\" = selected-data\ntest ! -e /source/sibling\ntest ! -e '" + secret + "'\ntest ! -e /proc/self/fd/4\ntest ! -e /run/libvirt/libvirt-sock\nif echo changed > /source/image; then exit 91; fi\n"
	if err = os.WriteFile(probe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd, cleanup, err := confinedCommand(ctx, probe, workspace, nil, "", "", "", f, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	b, err := os.ReadFile(name)
	if err != nil || string(b) != "selected-data" {
		t.Fatal("source changed", err)
	}
	t.Log("real namespace fixture: one read-only file, no sibling, no host socket, no inherited source descriptor")
}
