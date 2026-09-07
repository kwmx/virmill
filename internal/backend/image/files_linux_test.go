//go:build linux && amd64

package image

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	platform "virmill.local/core/internal/platform/linux"
)

func selectedFiles(t *testing.T, dir string, names ...string) []platform.DiskSourceFile {
	t.Helper()
	out := []platform.DiskSourceFile{}
	for _, name := range names {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		out = append(out, platform.DiskSourceFile{Path: name, File: f})
	}
	return out
}
func TestRealSelectedFileSetReadsSplitExtentsAndContainedBacking(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit generated-file namespace test required")
	}
	ctx := context.Background()
	dir := privateTemp(t)
	if err := os.Mkdir(filepath.Join(dir, "disks"), 0700); err != nil {
		t.Fatal(err)
	}
	fixtureQEMU(t, "create", "-f", "raw", filepath.Join(dir, "base.raw"), "8M")
	fixtureQEMU(t, "create", "-f", "qcow2", "-b", "../base.raw", "-F", "raw", filepath.Join(dir, "disks", "child.qcow2"), "8M")
	fixtureQEMU(t, "create", "-f", "vmdk", "-o", "subformat=twoGbMaxExtentFlat", filepath.Join(dir, "disks", "split.vmdk"), "8M")
	names := []string{"base.raw"}
	entries, err := os.ReadDir(filepath.Join(dir, "disks"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		names = append(names, "disks/"+entry.Name())
	}
	sources := selectedFiles(t, dir, names...)
	for _, disk := range []struct{ path, format string }{{"disks/child.qcow2", "qcow2"}, {"disks/split.vmdk", "vmdk"}} {
		work := privateTemp(t)
		chain, err := (Tool{}).InspectFiles(ctx, sources, work, disk.path, disk.format, 16<<20)
		if err != nil {
			t.Fatalf("%s: %v; observed backing information: %+v", disk.path, err, chain)
		}
		if err = (Tool{}).ConvertFiles(ctx, sources, work, disk.path, disk.format, chain[0].VirtualSize, 32<<20); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = (Tool{}).InspectFiles(ctx, sources, privateTemp(t), "disks/child.qcow2", "raw", 16<<20); err == nil {
		t.Fatal("incorrect declared root format accepted")
	}
	only := selectedFiles(t, dir, "disks/child.qcow2")
	if _, err = (Tool{}).InspectFiles(ctx, only, privateTemp(t), "disks/child.qcow2", "qcow2", 16<<20); err == nil {
		t.Fatal("unselected backing file exposed")
	}
	t.Log("real conversion from held read-only selected files; split VMDK and nested qcow2 backing; no original directory exposure or VM boot")
}
func TestRealSelectedFilesRefuseQEMUWriterLocks(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit generated QEMU file-lock fixture required")
	}
	for _, format := range []string{"raw", "qcow2"} {
		t.Run(format, func(t *testing.T) { selectedWriterLock(t, format) })
	}
}
func selectedWriterLock(t *testing.T, format string) {
	t.Helper()
	dir := privateTemp(t)
	name := filepath.Join(dir, "disk."+format)
	fixtureQEMU(t, "create", "-f", format, name, "8M")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	holder := exec.CommandContext(ctx, "/usr/bin/qemu-io", "-f", format, name)
	stdin, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	holder.Stderr = os.Stderr
	if err = holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); holder.Process.Kill(); holder.Wait() }()
	if _, err = stdin.Write([]byte("info\n")); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	ready := false
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "format name: "+format) {
			ready = true
			break
		}
	}
	if !ready {
		t.Fatal("QEMU fixture never confirmed its open image", scanner.Err())
	}
	sources := selectedFiles(t, dir, "disk."+format)
	if format == "raw" {
		// Keep the original regression observable: info itself does not establish
		// offline access for raw, even without -U. This intentionally bypasses the
		// guarded production adapter on this generated file only.
		if _, err = runFiles(ctx, sources, privateTemp(t), 64<<20, "info", "--output=json", "--backing-chain", "/source/disk.raw"); err != nil {
			t.Fatal("raw metadata-sharing regression fixture changed", err)
		}
		t.Log("direct unguarded qemu-img info accepts a writer-held raw file; the production guard must supply exclusion")
	}
	if _, err = (Tool{}).InspectFiles(ctx, sources, privateTemp(t), "disk."+format, format, 16<<20); err == nil {
		t.Fatal("writer-locked image accepted")
	}
	if err = (Tool{}).ConvertFiles(ctx, sources, privateTemp(t), "disk."+format, format, 8<<20, 16<<20); err == nil {
		t.Fatal("writer-locked image copied")
	}
	t.Log("real QEMU writer-lock refusal across an FD bind; this is a generated-file lock fixture, not a live VM test")
}

func TestRealSourceGuardPreventsNewWriterUntilClosed(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit generated-file QEMU lock fixture required")
	}
	for _, format := range []string{"raw", "qcow2"} {
		t.Run(format, func(t *testing.T) {
			name := filepath.Join(privateTemp(t), "disk."+format)
			fixtureQEMU(t, "create", "-f", format, name, "8M")
			f, err := os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			guard, err := AcquireReadGuard(f)
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Close()
			second, err := AcquireReadGuard(f)
			if err != nil {
				t.Fatal(err)
			}
			if err = second.Close(); err != nil {
				t.Fatal(err)
			}
			// Closing an independent reader must not release the first guard.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if b, err := exec.CommandContext(ctx, "/usr/bin/qemu-io", "-f", format, "-c", "info", name).CombinedOutput(); err == nil {
				t.Fatal("new writer opened despite retained guard", string(b))
			}
			if err = guard.Close(); err != nil {
				t.Fatal(err)
			}
			if b, err := exec.CommandContext(ctx, "/usr/bin/qemu-io", "-f", format, "-c", "info", name).CombinedOutput(); err != nil {
				t.Fatal("guard leaked after its independent descriptor closed", err, string(b))
			}
			t.Log("actual QEMU writer refused while source guard held; allowed after close; original caller descriptor stays open")
		})
	}
}
