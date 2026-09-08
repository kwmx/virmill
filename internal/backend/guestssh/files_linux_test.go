//go:build linux

package guestssh

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/domain"
)

const fixtureKey = "generated private credential, never an actual SSH key\n"
const fixtureHosts = "[192.0.2.42]:2222 ssh-ed25519 generated-known-host-fixture\n"

func targetFiles(t *testing.T) Target {
	t.Helper()
	target := validTarget()
	dir := t.TempDir()
	target.IdentityFile = filepath.Join(dir, "identity key's file")
	target.KnownHostsFile = filepath.Join(dir, "known_hosts")
	for name, content := range map[string]string{target.IdentityFile: fixtureKey, target.KnownHostsFile: fixtureHosts} {
		if err := os.WriteFile(name, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return target
}
func hashText(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func expectCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *domain.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("err=%v want=%s", err, code)
	}
	if strings.Contains(err.Error(), fixtureKey) || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("sensitive diagnostic leaked")
	}
}
func option(args []string, key string) string {
	for i, a := range args {
		if a == key && i+1 < len(args) {
			return args[i+1]
		}
		if a == "-o" && i+1 < len(args) && strings.HasPrefix(args[i+1], key+"=") {
			return strings.TrimPrefix(args[i+1], key+"=")
		}
	}
	return ""
}

func TestInspectTargetHashesPrivatePinnedFilesAndChecksExpectedDigests(t *testing.T) {
	target := targetFiles(t)
	id, err := (Tool{}).InspectTarget(context.Background(), target)
	if err != nil || id.IdentitySHA256 != hashText(fixtureKey) || id.KnownHostsSHA256 != hashText(fixtureHosts) {
		t.Fatalf("id=%+v err=%v", id, err)
	}
	target.IdentitySHA256, target.KnownHostsSHA256 = id.IdentitySHA256, id.KnownHostsSHA256
	if got, err := (Tool{}).InspectTarget(context.Background(), target); err != nil || got != id {
		t.Fatalf("fresh expected identity refused: %+v %v", got, err)
	}
	target.IdentitySHA256 = strings.Repeat("a", 64)
	got, err := (Tool{}).InspectTarget(context.Background(), target)
	expectCode(t, err, "SOURCE_CHANGED")
	if got != (TargetIdentity{}) {
		t.Fatal("stale hashes exposed success")
	}
	calls := 0
	out, err := run(context.Background(), target, validScript(), func(context.Context, []string, []byte, time.Duration) (Result, error) { calls++; return Result{}, nil })
	expectCode(t, err, "SOURCE_CHANGED")
	if calls != 0 || !reflect.DeepEqual(out, Result{}) {
		t.Fatal("mismatched held hashes reached execution")
	}
}

func TestCredentialsRejectSymlinksAliasesModesAndSpecialFiles(t *testing.T) {
	for _, kind := range []string{"leaf symlink", "parent symlink", "fifo", "directory", "hardlink", "key mode", "host mode", "empty", "oversized", "missing"} {
		t.Run(kind, func(t *testing.T) {
			v := targetFiles(t)
			switch kind {
			case "leaf symlink":
				if err := os.Remove(v.IdentityFile); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(v.KnownHostsFile, v.IdentityFile); err != nil {
					t.Fatal(err)
				}
			case "parent symlink":
				link := filepath.Join(t.TempDir(), "link")
				if err := os.Symlink(filepath.Dir(v.IdentityFile), link); err != nil {
					t.Fatal(err)
				}
				v.IdentityFile = filepath.Join(link, filepath.Base(v.IdentityFile))
			case "fifo":
				if err := os.Remove(v.IdentityFile); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(v.IdentityFile, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(v.IdentityFile); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(v.IdentityFile, 0700); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Remove(v.KnownHostsFile); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(v.IdentityFile, v.KnownHostsFile); err != nil {
					t.Fatal(err)
				}
			case "key mode":
				if err := os.Chmod(v.IdentityFile, 0644); err != nil {
					t.Fatal(err)
				}
			case "host mode":
				if err := os.Chmod(v.KnownHostsFile, 0644); err != nil {
					t.Fatal(err)
				}
			case "empty":
				if err := os.WriteFile(v.IdentityFile, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.Truncate(v.IdentityFile, maxCredential+1); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(v.IdentityFile); err != nil {
					t.Fatal(err)
				}
			}
			id, err := (Tool{}).InspectTarget(context.Background(), v)
			if err == nil || id != (TargetIdentity{}) {
				t.Fatalf("unsafe source accepted: %+v %v", id, err)
			}
		})
	}
}

func TestSealedCredentialBytesAndOriginalDriftAreBoundTogether(t *testing.T) {
	for _, kind := range []string{"rewrite", "replace", "mode"} {
		t.Run(kind, func(t *testing.T) {
			v := targetFiles(t)
			v.IdentitySHA256, v.KnownHostsSHA256 = hashText(fixtureKey), hashText(fixtureHosts)
			out, err := run(context.Background(), v, validScript(), func(ctx context.Context, args []string, content []byte, timeout time.Duration) (Result, error) {
				keyPath := option(args, "-i")
				b, err := os.ReadFile(keyPath)
				if err != nil || string(b) != fixtureKey {
					t.Fatalf("sealed original unreadable: %v", err)
				}
				f, err := os.OpenFile(keyPath, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				if st, err := f.Stat(); err != nil || st.Mode().Perm() != 0600 {
					t.Fatal("credential copy lost 0600")
				}
				if _, err := f.WriteAt([]byte("replacement"), 0); !errors.Is(err, unix.EPERM) {
					t.Fatalf("credential copy writable: %v", err)
				}
				f.Close()
				switch kind {
				case "rewrite":
					if err := os.WriteFile(v.IdentityFile, []byte("changed credential"), 0600); err != nil {
						t.Fatal(err)
					}
				case "replace":
					if err := os.Rename(v.IdentityFile, v.IdentityFile+".old"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(v.IdentityFile, []byte(fixtureKey), 0600); err != nil {
						t.Fatal(err)
					}
				case "mode":
					if err := os.Chmod(v.KnownHostsFile, 0644); err != nil {
						t.Fatal(err)
					}
				}
				b, err = os.ReadFile(keyPath)
				if err != nil || string(b) != fixtureKey {
					t.Fatalf("source writer changed bytes supplied to SSH: %v", err)
				}
				return Result{Stdout: []byte("must discard")}, nil
			})
			expectCode(t, err, "SOURCE_CHANGED")
			if !reflect.DeepEqual(out, Result{}) {
				t.Fatal("drift retained guest success")
			}
		})
	}
}

func TestCredentialFailuresDoNotLeakDescriptors(t *testing.T) {
	v := targetFiles(t)
	count := func() int {
		fds, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(fds)
	}
	// Warm up the runtime's descriptor support before comparing the owned set.
	c, err := prepare(context.Background(), v, true)
	if err != nil {
		t.Fatal(err)
	}
	c.close()
	before := count()
	for i := 0; i < 20; i++ {
		c, err := prepare(context.Background(), v, true)
		if err != nil {
			t.Fatal(err)
		}
		c.close()
		bad := v
		bad.IdentitySHA256, bad.KnownHostsSHA256 = strings.Repeat("a", 64), strings.Repeat("b", 64)
		if c, err := prepare(context.Background(), bad, true); err == nil || c != nil {
			t.Fatal("stale prepare passed")
		}
	}
	if after := count(); after != before {
		t.Fatalf("descriptors before=%d after=%d", before, after)
	}
}
