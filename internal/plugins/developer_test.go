package plugins

import (
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"virmill.local/core/internal/app"
)

func TestDeveloperScaffoldBuildAndSignedPackPlans(t *testing.T) {
	m, _, key := managerFixture(t)
	sdk, err := filepath.Abs("../../sdk/go")
	if err != nil {
		t.Fatal(err)
	}
	d := &Developer{Engine: m.Engine, SDKDirectory: sdk}
	m.Engine.Handlers["plugin.new"], m.Engine.Handlers["plugin.pack"] = d, d
	destination := filepath.Join(t.TempDir(), "new-plugin")
	p, err := d.Plan(context.Background(), 1000, "new", app.Request{Path: destination, Input: map[string]any{"id": "example.virmill.generated", "language": "go", "type": "action"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("preview created source")
	}
	applyPlan(t, m, p)
	toolchain, err := filepath.Abs("../../scripts/go")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"test", "-buildvcs=false", "./..."}, {"build", "-buildvcs=false", "-o", "vm-summary", "."}} {
		cmd := exec.Command(toolchain, args...)
		cmd.Dir = destination
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generated source does not build/test: %s %v", output, err)
		}
	}
	manifest, err := WorkspaceManifest(destination)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(filepath.Join(destination, "vm-summary"))
	if err != nil {
		t.Fatal(err)
	}
	entry := manifest.Entrypoints["linux/amd64"]
	entry.SHA256 = hashBytes(binary)
	manifest.Entrypoints["linux/amd64"] = entry
	if err = os.WriteFile(filepath.Join(destination, "manifest.json"), EncodeManifest(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "private-key.hex")
	os.WriteFile(keyPath, []byte(hex.EncodeToString(key)+"\n"), 0600)
	output := filepath.Join(t.TempDir(), "plugin.tar")
	p, err = d.Plan(context.Background(), 1000, "pack", app.Request{Path: destination, Input: map[string]any{"output": output, "keyID": "fixture-key", "signingKeyPath": keyPath}})
	if err != nil {
		t.Fatal(err)
	}
	applyPlan(t, m, p)
	if _, err = d.Plan(context.Background(), 1000, "new", app.Request{Path: destination, Input: map[string]any{"id": "example.virmill.generated"}}); err == nil {
		t.Fatal("existing scaffold overwrite planned")
	}
	_, journal, _ := m.Store.Plan(p.ID)
	if containsSecret(journal, hex.EncodeToString(key)) {
		t.Fatal("private key appeared in journal")
	}
	inside := filepath.Join(destination, "private-key.hex")
	os.WriteFile(inside, []byte(hex.EncodeToString(key)), 0600)
	if _, err = d.Plan(context.Background(), 1000, "pack", app.Request{Path: destination, Input: map[string]any{"output": output + ".new", "keyID": "fixture-key", "signingKeyPath": inside}}); err == nil {
		t.Fatal("key inside payload directory accepted")
	}
}

func containsSecret(b []byte, s string) bool {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return true
		}
	}
	return false
}
