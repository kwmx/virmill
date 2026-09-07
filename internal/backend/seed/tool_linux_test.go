//go:build linux && amd64

package seed

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func membersFixture() map[string][]byte {
	return map[string][]byte{"user-data": []byte("#cloud-config\n{\"hostname\":\"seed-fixture\",\"ssh_pwauth\":false}\n"), "meta-data": []byte("{\"instance-id\":\"virmill-generated-fixture\"}\n"), "network-config": []byte("{\"version\":2,\"ethernets\":{}}\n")}
}
func TestSeedMemberBoundsAndNames(t *testing.T) {
	for _, change := range []func(map[string][]byte){func(f map[string][]byte) { delete(f, "network-config") }, func(f map[string][]byte) { f["../escape"] = []byte("x") }, func(f map[string][]byte) { f["user-data"] = []byte("#!/bin/sh\nexit 0") }, func(f map[string][]byte) { f["meta-data"] = []byte{0xff} }, func(f map[string][]byte) { f["meta-data"] = []byte("a\x00b") }, func(f map[string][]byte) { f["meta-data"] = []byte(strings.Repeat("a", MaxContentBytes)) }} {
		f := membersFixture()
		change(f)
		if err := validateMembers(f); err == nil {
			t.Fatal("invalid seed accepted")
		}
	}
}
func TestRealConfinedDeterministicNoCloudSeed(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("enable actual confined generated-seed fixture explicitly")
	}
	tool := Tool{}
	id, err := tool.Identity(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("actual seed generator %s SHA-256 %s", id.Version, id.SHA256)
	var first []byte
	var expected Artifact
	for i := 0; i < 2; i++ {
		work := t.TempDir()
		if err := os.Chmod(work, 0700); err != nil {
			t.Fatal(err)
		}
		a, err := tool.Build(context.Background(), work, membersFixture())
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(work, a.Path))
		if err != nil {
			t.Fatal(err)
		}
		if a.FileBytes != int64(len(b)) || a.SHA256 != digest(b) || len(a.Members) != 3 {
			t.Fatal("incorrect seed receipt", a, len(b))
		}
		if i == 0 {
			first = b
			expected = a
		} else if !bytes.Equal(first, b) || a.SHA256 != expected.SHA256 {
			t.Fatal("seed bytes vary across private workspaces")
		}
		if _, err = tool.Build(context.Background(), work, membersFixture()); err == nil {
			t.Fatal("existing seed overwritten")
		}
	}
	t.Logf("actual confined ISO creation, CIDATA label and exact three-file readback pass; deterministic generated seed SHA-256 %s; no cloud-init guest execution", expected.SHA256)
}
