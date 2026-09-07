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

func TestSelectedSourceNamesAndHandlesAreBounded(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "source")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ro, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	for _, selected := range [][]DiskSourceFile{
		{{Path: "disk", File: f}},
		{{Path: "disk"}},
		{{Path: "../disk", File: ro}},
		{{Path: "/disk", File: ro}},
		{{Path: "disk/", File: ro}},
		{{Path: "a/../disk", File: ro}},
		{{Path: "disk", File: ro}, {Path: "disk", File: ro}},
		{{Path: "disk", File: ro}, {Path: "disk/child", File: ro}},
	} {
		if _, err := validateDiskSources(selected); err == nil {
			t.Fatal("invalid selected file boundary accepted", selected)
		}
	}
	parents, err := validateDiskSources([]DiskSourceFile{{Path: "a/b/disk", File: ro}, {Path: "a/other", File: ro}})
	if err != nil || len(parents) != 2 || parents[0] != "a" || parents[1] != "a/b" {
		t.Fatal("ordinary parent mount ordering", parents, err)
	}
}

func TestRealSelectedFileSandboxExposesOnlyReadOnlyWhitelist(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit generated-file namespace probe required")
	}
	dir, workspace := t.TempDir(), t.TempDir()
	if err := os.Chmod(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	sources := []DiskSourceFile{}
	for _, name := range []string{"base", "child", "sibling"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name+"-data"), 0600); err != nil {
			t.Fatal(err)
		}
		if name == "sibling" {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		selected := name
		if name == "child" {
			selected = "disks/child"
		}
		sources = append(sources, DiskSourceFile{Path: selected, File: f})
	}
	probe := filepath.Join(t.TempDir(), "probe")
	// Trusted fixture only: exported adapters always use fixed /usr/bin/qemu-img.
	script := "#!/usr/bin/sh\nset -eu\ntest \"$(cat /source/base)\" = base-data\ntest \"$(cat /source/disks/child)\" = child-data\ntest ! -e /source/sibling\ntest ! -e /source/disks/sibling\ntest ! -e '" + dir + "'\ntest ! -e /proc/self/fd/4\ntest ! -e /proc/self/fd/5\ntest ! -e /run/libvirt/libvirt-sock\nif echo changed > /source/base; then exit 91; fi\nif echo changed > /source/disks/child; then exit 92; fi\n"
	if err := os.WriteFile(probe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd, cleanup, err := confinedCommand(ctx, probe, workspace, nil, "", "", "", nil, sources, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	for _, name := range []string{"base", "child", "sibling"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || string(b) != name+"-data" {
			t.Fatal("fixture source changed", name, err)
		}
	}
	t.Log("actual namespace: exact selected files read-only; original directory, sibling, libvirt socket and inherited source FDs absent")
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
	cmd, cleanup, err := confinedCommand(ctx, probe, workspace, nil, "", "", "", f, nil, 64<<20)
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
