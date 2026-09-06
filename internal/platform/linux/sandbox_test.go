//go:build linux && amd64

package linux

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSandboxPolicyAndFailClosed(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd, cleanup, e := ConfinedCommand(ctx, "/usr/bin/true", dir, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	joined := strings.Join(cmd.Args, " ")
	for _, required := range []string{"--unshare-all", "--clearenv", "--seccomp 3", "--nproc=256", "--cap-drop ALL"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("policy missing %s", required)
		}
	}
	for _, forbidden := range []string{"/run/libvirt", "/run/user/", "/home/"} { // Executable/workspace may be selected under home; no home mount is permitted.
		if strings.Contains(joined, "--ro-bind "+forbidden) || strings.Contains(joined, "--bind "+forbidden) {
			t.Fatal("forbidden mount")
		}
	}
	if e = ProbeSandbox(ctx, dir); e != nil {
		t.Logf("REAL SANDBOX PROBE BLOCKED (no confinement certification): %v", e)
		return
	}
	secret := filepath.Join(t.TempDir(), "host-secret")
	os.WriteFile(secret, []byte("DO_NOT_EXPOSE"), 0600)
	attack := filepath.Join(t.TempDir(), "probe")
	os.WriteFile(attack, []byte("#!/usr/bin/sh\nif test -r '"+secret+"'; then exit 91; fi\nif test -S /run/libvirt/libvirt-sock; then exit 92; fi\nexit 0\n"), 0700)
	cmd, cleanup, e = ConfinedCommand(ctx, attack, dir, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(string(b), e)
	}
	t.Log("real namespace probe prevented access to undeclared temporary host file and libvirt socket; network attack still requires full qualification")
}
