//go:build linux && amd64

package coldfiles_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/backend/coldfiles"
	"virmill.local/core/internal/backend/fileidentity"
)

// This exercises actual QEMU software on newly generated ordinary files only.
// No VM, namespace, mount, device, source-media or host-service operation occurs.
func TestRealQEMUSourceTransferRetainsReadGuard(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("set VIRMILL_TEST_DISK_TOOLS=1 for generated-file QEMU transfer qualification")
	}
	for _, tool := range []string{"/usr/bin/qemu-img", "/usr/bin/qemu-io"} {
		output, err := sourceQEMUCommand(t, tool, "--version")
		if err != nil {
			t.Fatalf("required installed tool unavailable: %s: %v\n%s", tool, err, output)
		}
		t.Logf("exact %s --version output:\n%s", tool, output)
	}
	for _, format := range []string{"raw", "qcow2"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			relative := "source." + format
			path := filepath.Join(root, relative)
			output, err := sourceQEMUCommand(t, "/usr/bin/qemu-img", "create", "-f", format, path, "1048576")
			if err != nil {
				t.Fatalf("create fresh 1 MiB fixture: %v\n%s", err, output)
			}
			// qemu-io's default image open requests write permissions. Its info
			// command observes the generated file without issuing a data write.
			sourceQEMUWriterControl(t, path, format, "before guard")
			expected, err := fileidentity.Observe(path, false)
			if err != nil {
				t.Fatal(err)
			}
			source, err := coldfiles.Open(t.Context(), root, relative, expected)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := source.Close(); err != nil {
					t.Error(err)
				}
			})
			transferred, err := source.DupForTransfer()
			if err != nil {
				t.Fatal(err)
			}
			defer transferred.Close()
			sourceQEMUWriterRefused(t, path, format, "source and transferred descriptor retained")
			if source.Identity() != expected {
				t.Fatal("source returned a different identity")
			}
			if err := source.Recheck(t.Context()); err != nil {
				t.Fatal("refused QEMU open changed held source identity", err)
			}
			if err := source.Close(); err != nil {
				t.Fatal(err)
			}
			sourceQEMUWriterRefused(t, path, format, "only transferred descriptor retained")
			if err := transferred.Close(); err != nil {
				t.Fatal(err)
			}
			sourceQEMUWriterControl(t, path, format, "after last transferred descriptor closed")
			t.Log("PASS native QEMU software/file qualification of Source.Open and DupForTransfer lock lifetime; no VM or hardware claim")
		})
	}
}

func sourceQEMUCommand(t *testing.T, tool string, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, tool, args...)
	command.Env = []string{"LC_ALL=C", "LANG=C", "PATH=/usr/bin:/bin"}
	command.WaitDelay = time.Second
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("QEMU command exceeded its deadline; this is not lock-refusal evidence: %s %q: %v\n%s", tool, args, ctx.Err(), output)
	}
	return output, err
}

func sourceQEMUWriterControl(t *testing.T, path, format, phase string) {
	t.Helper()
	output, err := sourceQEMUCommand(t, "/usr/bin/qemu-io", "-f", format, "-c", "info", path)
	if err != nil || !strings.Contains(string(output), "format name: "+format) {
		t.Fatalf("QEMU writer-open control failed %s: %v\n%s", phase, err, output)
	}
	t.Logf("QEMU writer-open control succeeded %s:\n%s", phase, output)
}

func sourceQEMUWriterRefused(t *testing.T, path, format, phase string) {
	t.Helper()
	output, err := sourceQEMUCommand(t, "/usr/bin/qemu-io", "-f", format, "-c", "info", path)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() <= 0 ||
		!strings.Contains(string(output), `Failed to get "write" lock`) ||
		!strings.Contains(string(output), "Is another process using the image ["+path+"]?") ||
		strings.Contains(string(output), "format name:") {
		t.Fatalf("expected specific QEMU write-lock refusal %s; another failure is not qualifying evidence: %v\n%s", phase, err, output)
	}
	t.Logf("QEMU specifically refused its write lock %s:\n%s", phase, output)
}
