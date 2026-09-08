//go:build linux

package guestssh

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in only: this executes fixed read-only commands on an owner-
// authorized SSH target. It establishes transport evidence, not guest provisioning.
func TestNativeSSHAuthorizedReadOnly(t *testing.T) {
	path := os.Getenv("VIRMILL_TEST_GUEST_SSH_TARGET")
	if path == "" {
		t.Skip("requires explicit authorized SSH target file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var target Target
	if err = json.Unmarshal(raw, &target); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	tool := Tool{}
	identity, err := tool.Identity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mismatch, mismatchErr := invoke(ctx, []string{"-V"}, nil, time.Second, strings.Repeat("0", 64))
	expectCode(t, mismatchErr, "SOURCE_CHANGED")
	if mismatch.ExitCode != 0 || len(mismatch.Stdout) != 0 || len(mismatch.Stderr) != 0 {
		t.Fatal("changed executable produced output")
	}
	creds, err := tool.InspectTarget(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	target.IdentitySHA256, target.KnownHostsSHA256 = creds.IdentitySHA256, creds.KnownHostsSHA256
	script := Script{SSHSHA256: identity.SHA256, Content: []byte("id -u\nprintf '%s\\n' \"$1\" \"$2\"\n"), Arguments: []string{"literal $(false)", "apostrophe'argument"}, Timeout: 20 * time.Second}
	result, err := tool.Run(ctx, target, script)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(result.Stdout), "\n"), "\n")
	if result.ExitCode != 0 || len(result.Stderr) != 0 || len(lines) != 3 {
		t.Fatal("unexpected read-only SSH result")
	}
	uid, err := strconv.ParseUint(lines[0], 10, 32)
	if err != nil || uid == 0 || lines[1] != script.Arguments[0] || lines[2] != script.Arguments[1] {
		t.Fatal("non-root/literal argument check failed")
	}
	status, err := tool.Run(ctx, target, Script{SSHSHA256: identity.SHA256, Content: []byte("exit 3\n"), Timeout: 20 * time.Second})
	if err != nil || status.ExitCode != 3 {
		t.Fatal("remote check exit 3 was not preserved", err)
	}
	// An empty, explicitly selected trust store must refuse the same host; never TOFU.
	hosts := filepath.Join(t.TempDir(), "known-hosts")
	if err = os.WriteFile(hosts, []byte("# no trusted hosts\n"), 0600); err != nil {
		t.Fatal(err)
	}
	target.KnownHostsFile = hosts
	target.IdentitySHA256 = ""
	target.KnownHostsSHA256 = ""
	bad, err := tool.Run(ctx, target, script)
	expectCode(t, err, "GUEST_TRANSPORT_FAILED")
	if err == nil || bad.ExitCode != 0 || len(bad.Stdout) != 0 || len(bad.Stderr) != 0 {
		t.Fatal("untrusted host accepted or diagnostics exposed")
	}
	t.Logf("actual fixed SSH executable %s SHA256=%s version=%s: ordinary UID=%d, literal arguments preserved, untrusted host refused; no nested guest recipe claim", identity.Path, identity.SHA256, identity.Version, uid)
}
