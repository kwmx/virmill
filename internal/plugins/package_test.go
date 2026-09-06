package plugins

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func packageFixture(t *testing.T) ([]byte, ed25519.PublicKey) {
	t.Helper()
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	binary := []byte("#!/bin/sh\nexit 0\n")
	os.WriteFile(filepath.Join(dir, "example"), binary, 0700)
	m := map[string]any{"manifestVersion": "1", "id": "example.virmill.fixture", "name": "Fixture", "version": "0.1.0", "protocol": map[string]any{"minVersion": "1.0", "maxVersion": "1.0", "transport": "stdio-jsonrpc"}, "entrypoints": map[string]any{"linux/amd64": map[string]any{"path": "example", "sha256": hashBytes(binary)}}, "extensionTypes": []string{"action"}, "permissions": []Permission{{"vm.read", "selection"}}, "network": "none"}
	b, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0600)
	var out bytes.Buffer
	if _, e = Pack(dir, priv, "test-key", &out); e != nil {
		t.Fatal(e)
	}
	return out.Bytes(), pub
}
func TestSignedPackageAndScopeIntersection(t *testing.T) {
	b, pub := packageFixture(t)
	v, e := Verify(bytes.NewReader(b), map[string]ed25519.PublicKey{"test-key": pub})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = v.Extract(t.TempDir()); e != nil {
		t.Fatal(e)
	}
	if _, e = Verify(bytes.NewReader(b), nil); e == nil {
		t.Fatal("unknown key trusted")
	}
	tampered := append([]byte{}, b...)
	i := bytes.Index(tampered, []byte("exit 0"))
	tampered[i+5] = '9'
	if _, e = Verify(bytes.NewReader(tampered), map[string]ed25519.PublicKey{"test-key": pub}); e == nil {
		t.Fatal("tampered binary accepted")
	}
	read := Permission{"vm.read", "selection"}
	write := Permission{"vm.write", "all"}
	effective := Effective([]Permission{read, write}, []Permission{read}, []Permission{read, write})
	if len(effective) != 1 || effective[0] != read {
		t.Fatal("permission amplification")
	}
}
