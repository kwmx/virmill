//go:build linux && amd64

package helper

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"virmill.local/core/internal/backend/fileaccess"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

func accessFixture() (Request, Policy, ed25519.PrivateKey) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	vm := domain.ID()
	name := "virmill-" + vm + "-disk-000.qcow2"
	p := Policy{APIVersion: domain.APIVersion, Keys: map[string]string{"test": hex.EncodeToString(pub)}, Roots: map[string]string{"pool": "/disposable-test-only"}, Actors: []uint32{1000}}
	r := Request{APIVersion: domain.APIVersion, ActorUID: 1000, Operation: "storage.grant-read", ResourceID: vm, RootID: "pool", PlanDigest: strings.Repeat("a", 64), JobID: domain.ID(), ExpiresAt: time.Now().Add(time.Minute), KeyID: "test", Mode: "check", Access: &AccessRequest{Mapping: domain.ManagedFileVolume{VMID: vm, VMFingerprint: strings.Repeat("b", 64), DiskTarget: "vda", PoolID: domain.ID(), PoolFingerprint: strings.Repeat("c", 64), VolumeName: name, VolumeKey: "key", Path: "/disposable-test-only/" + name}, RelativePath: name, Before: json.RawMessage(`{}`), ActorGroups: []uint32{1000}}}
	return r, p, key
}
func sign(r *Request, key ed25519.PrivateKey) {
	b, _ := SignedBytes(*r)
	r.Signature = hex.EncodeToString(ed25519.Sign(key, b))
}
func TestAccessAuthorityBindsEveryReviewedField(t *testing.T) {
	r, p, key := accessFixture()
	sign(&r, key)
	if err := Authorize(1000, r, p, time.Now()); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*Request){func(r *Request) { r.Mode = "apply" }, func(r *Request) { r.Access.Mapping.DiskTarget = "vdb" }, func(r *Request) { r.Access.ActorGroups = []uint32{10, 1000} }, func(r *Request) { r.Access.Before = json.RawMessage(`{"uid":0}`) }, func(r *Request) { r.Access.RelativePath = "other" }, func(r *Request) { r.Access.Mapping.VolumeKey = "other" }}
	for _, change := range mutations {
		copy := r
		a := *r.Access
		copy.Access = &a
		change(&copy)
		if Authorize(1000, copy, p, time.Now()) == nil {
			t.Fatal("substituted access authority accepted")
		}
	}
	copy := r
	copy.ExpiresAt = time.Now()
	sign(&copy, key)
	if Authorize(1000, copy, p, copy.ExpiresAt) == nil {
		t.Fatal("grant valid at expiry")
	}
	copy = r
	copy.Access = &AccessRequest{}
	copy.Operation = "storage.prepare-directory"
	copy.Mode = ""
	sign(&copy, key)
	if Authorize(1000, copy, p, time.Now()) == nil {
		t.Fatal("access payload smuggled through legacy action")
	}
	copy = r
	copy.Operation = "storage.revoke-read"
	sign(&copy, key)
	if Authorize(1000, copy, p, time.Now()) == nil {
		t.Fatal("revoke without original accepted")
	}
}
func TestAccessPathRejectsAliasesAndUnrelatedFiles(t *testing.T) {
	r, p, _ := accessFixture()
	if _, err := accessPath(r, p); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"../etc/shadow", "/etc/shadow", ".", "pool/../" + r.Access.Mapping.VolumeName, "wrong.qcow2"} {
		copy := r
		a := *r.Access
		copy.Access = &a
		copy.Access.RelativePath = path
		if _, err := accessPath(copy, p); err == nil {
			t.Fatal("alias accepted", path)
		}
	}
	copy := r
	a := *r.Access
	copy.Access = &a
	copy.Access.Mapping.VolumeName = "virmill-" + domain.ID() + "-disk-000.qcow2"
	if _, err := accessPath(copy, p); err == nil {
		t.Fatal("another VM's volume accepted")
	}
}
func TestAccessBindingAllowsOnlyFreshAuthentication(t *testing.T) {
	r, _, _ := accessFixture()
	a, _ := accessBinding(r)
	r.Mode = "observe"
	r.ExpiresAt = r.ExpiresAt.Add(time.Hour)
	r.Signature = "fresh"
	b, _ := accessBinding(r)
	if a != b {
		t.Fatal("fresh recovery authentication changed immutable binding")
	}
	r.JobID = domain.ID()
	b, _ = accessBinding(r)
	if a == b {
		t.Fatal("binding omitted job")
	}
}
func TestPrivateHelperKeyRejectsPublicModesAndLinks(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	path := filepath.Join(t.TempDir(), "helper-key.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	c := Client{KeyPath: path}
	got, id, err := c.key()
	if err != nil || !reflect.DeepEqual(got, key) || len(id) != 64 {
		t.Fatal("private key load", err)
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.key(); err == nil {
		t.Fatal("public key file mode accepted")
	}
	os.Chmod(path, 0600)
	link := path + ".hard"
	if err = os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.key(); err == nil {
		t.Fatal("hard-linked private key accepted")
	}
	os.Remove(link)
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err = (Client{KeyPath: link}).key(); err == nil {
		t.Fatal("symlink private key accepted")
	}
}
func TestKernelPeerGroupsMatchConnectionCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peer.sock")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		if os.Getenv("VIRMILL_TEST_REQUIRE_IPC") == "1" {
			t.Fatal(err)
		}
		t.Skip("BLOCKED: private Unix IPC unavailable in this sandbox:", err)
	}
	defer l.Close()
	done := make(chan error, 1)
	go func() {
		c, e := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
		if e != nil {
			done <- e
			return
		}
		defer c.Close()
		var b [1]byte
		_, e = c.Read(b[:])
		done <- e
	}()
	conn, err := l.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	cred, groups, err := PeerGroups(conn)
	if err != nil {
		t.Fatal(err)
	}
	want, err := CurrentGroups()
	if err != nil {
		t.Fatal(err)
	}
	if cred.Uid != uint32(os.Getuid()) || cred.Gid != uint32(os.Getgid()) || !reflect.DeepEqual(groups, want) {
		t.Fatal("kernel peer credential mismatch", cred, groups, want)
	}
	conn.Write([]byte{1})
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

type accessNativeFixture struct {
	mapping domain.ManagedFileVolume
	calls   int
	failAt  int
}

func (f *accessNativeFixture) InspectManagedFileVolume(context.Context, string, string, string) (domain.ManagedFileVolume, error) {
	f.calls++
	if f.calls == f.failAt {
		return domain.ManagedFileVolume{}, domain.Fail("RESOURCE_BUSY", "synthetic VM state changed")
	}
	return f.mapping, nil
}

// This fixture exercises actual root-owned ACLs, held descriptors and durable
// helper records. Native VM inventory is synthetic: it certifies no hypervisor
// or hardware behavior. Run only on an explicitly authorized disposable host.
func TestDisposableRootAccessGrantRevokeAndLostAcknowledgement(t *testing.T) {
	if os.Getuid() != 0 || os.Getenv("VIRMILL_TEST_DISPOSABLE_HELPER_ACCESS") != "1" {
		t.Skip("BLOCKED: requires explicitly authorized disposable root fixture")
	}
	for _, mode := range []string{"normal", "lost-completion", "stale-before", "native-drift", "revoke-drift", "nested-grant"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			r, p, _ := accessFixture()
			p.Roots[r.RootID] = root
			r.Mode = "apply"
			r.Access.Mapping.Path = filepath.Join(root, r.Access.Mapping.VolumeName)
			content := []byte("generated ACL fixture only; no image parser or guest")
			if err := os.WriteFile(r.Access.Mapping.Path, content, 0600); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(r.Access.Mapping.Path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			before, err := fileaccess.Snapshot(f)
			if err != nil {
				t.Fatal(err)
			}
			r.Access.Before, _ = operations.Canonical(before)
			native := &accessNativeFixture{mapping: r.Access.Mapping}
			journal := t.TempDir()
			if err = os.Chmod(journal, 0700); err != nil {
				t.Fatal(err)
			}
			e := AccessExecutor{Backend: native, journalDirectory: journal}
			if mode == "stale-before" {
				os.Chmod(r.Access.Mapping.Path, 0640)
				if _, err = e.Execute(context.Background(), r, p, []uint32{1000}); err == nil {
					t.Fatal("stale ACL accepted")
				}
				if _, err = os.Stat(e.intentName(r.JobID)); !os.IsNotExist(err) {
					t.Fatal("stale preview journaled")
				}
				return
			}
			if mode == "native-drift" {
				native.failAt = 2
				if _, err = e.Execute(context.Background(), r, p, []uint32{1000}); err == nil {
					t.Fatal("native drift accepted")
				}
				after, _ := fileaccess.Snapshot(f)
				if after != before {
					t.Fatal("changed ACL despite native drift")
				}
				return
			}
			result, err := e.Execute(context.Background(), r, p, []uint32{1000})
			if err != nil || !result.Complete {
				t.Fatal("grant", err)
			}
			if _, err = e.Execute(context.Background(), r, p, []uint32{1000}); err == nil {
				t.Fatal("grant replay accepted")
			}
			if mode == "lost-completion" {
				if err = os.Remove(e.resultName(r.JobID)); err != nil {
					t.Fatal(err)
				}
			}
			observe := r
			observe.Mode = "observe"
			observed, err := e.Execute(context.Background(), observe, p, []uint32{1000})
			if err != nil || observed != result {
				t.Fatal("observation failed", err)
			}
			if mode == "nested-grant" {
				nested := r
				access := *r.Access
				nested.Access = &access
				nested.JobID = domain.ID()
				nested.Access.Before, _ = operations.Canonical(result.State)
				second, err := e.Execute(context.Background(), nested, p, []uint32{1000})
				if err != nil {
					t.Fatal("nested grant", err)
				}
				undo := nested
				undoAccess := *nested.Access
				undo.Access = &undoAccess
				undo.JobID = domain.ID()
				undo.Operation = "storage.revoke-read"
				undo.Access.OriginalGrantJobID = nested.JobID
				undo.Access.Before, _ = operations.Canonical(second.State)
				if _, err = e.Execute(context.Background(), undo, p, []uint32{1000}); err != nil {
					t.Fatal("nested revoke", err)
				}
			}
			if mode == "revoke-drift" {
				os.Chmod(r.Access.Mapping.Path, 0660)
			}
			after, err := fileaccess.Snapshot(f)
			if err != nil {
				t.Fatal(err)
			}
			revoke := r
			a := *r.Access
			revoke.Access = &a
			revoke.JobID = domain.ID()
			revoke.Operation = "storage.revoke-read"
			revoke.Access.OriginalGrantJobID = r.JobID
			revoke.Access.Before, _ = operations.Canonical(after)
			restored, err := e.Execute(context.Background(), revoke, p, []uint32{1000})
			if mode == "revoke-drift" {
				if err == nil {
					t.Fatal("drifted grant restored")
				}
				return
			}
			if err != nil || !restored.Complete {
				t.Fatal("revoke", err)
			}
			raw, _ := before.RestoreACL()
			if !fileaccess.MatchesAccess(restored.State, before, raw) {
				t.Fatal("original access not restored")
			}
			got, err := os.ReadFile(r.Access.Mapping.Path)
			if err != nil || string(got) != string(content) {
				t.Fatal("file bytes changed", err)
			}
		})
	}
}
