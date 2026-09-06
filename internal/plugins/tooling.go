package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"virmill.local/core/internal/domain"
)

func TestWorkspace(ctx context.Context, path, cache string) (ConformanceReport, error) {
	root, e := filepath.Abs(path)
	if e != nil {
		return ConformanceReport{}, e
	}
	b, e := os.ReadFile(filepath.Join(root, "manifest.json"))
	if e != nil {
		return ConformanceReport{}, e
	}
	m, e := ValidateManifest(b)
	if e != nil {
		return ConformanceReport{}, e
	}
	entry, ok := m.Entrypoints["linux/amd64"]
	if !ok {
		return ConformanceReport{}, domain.Fail("UNSUPPORTED_CAPABILITY", "no Linux/amd64 development entrypoint")
	}
	workspace, e := os.MkdirTemp(cache, "virmill-conformance-")
	if e != nil {
		return ConformanceReport{}, e
	}
	if e = os.Chmod(workspace, 0700); e != nil {
		return ConformanceReport{}, e
	}
	defer os.RemoveAll(workspace)
	return Conformance(ctx, filepath.Join(root, entry.Path), workspace, m)
}

// Scaffold creates buildable local SDK source; it never invents a registry endpoint.
// The caller supplies the reviewed bundled SDK directory and a new destination.
func Scaffold(destination, sdkDirectory, id string) error {
	probe := []byte(`{"manifestVersion":"1","id":"` + id + `","name":"VM Summary","version":"0.1.0","protocol":{"minVersion":"1.0","maxVersion":"1.0","transport":"stdio-jsonrpc"},"entrypoints":{"linux/amd64":{"path":"vm-summary"}},"extensionTypes":["action"],"permissions":[{"name":"vm.read","scope":"selection"}],"network":"none"}`)
	if _, e := ValidateManifest(probe); e != nil {
		return e
	}
	if strings.ContainsAny(id, "\"\\") {
		return errors.New("invalid plugin ID")
	}
	if e := os.Mkdir(destination, 0700); e != nil {
		return e
	}
	files := map[string][]byte{"manifest.json": probe, "main.go": []byte("package main\nimport (\"context\";\"fmt\";\"os\";\"virmill.local/sdk/example\")\nfunc main(){if e:=example.New(\"" + id + "\").Serve(context.Background(),os.Stdin,os.Stdout);e!=nil{fmt.Fprintln(os.Stderr,e);os.Exit(1)}}\n"), "go.mod": []byte("module virmill.local/plugin\n\ngo 1.27.1\n\nrequire virmill.local/sdk v0.1.0\nreplace virmill.local/sdk => ./sdk\n"), "main_test.go": []byte("package main\nimport (\"testing\";\"virmill.local/sdk/example\")\nfunc TestManifestIdentity(t *testing.T){if example.New(\"" + id + "\").Identity.ID!=\"" + id + "\"{t.Fatal(\"identity drift\")}}\n"), "README.md": []byte("# Virmill action plugin\n\nBuild: `go build -o vm-summary .`; test: `go test ./...`.\nRun `virmill plugin test .` through the coordinator to exercise confined synthetic input.\nThe copied local SDK is versioned development source, not a published registry.\nThis action uses only selected VM inventory supplied over the protocol.\nBefore distribution, set the executable's actual SHA-256 in manifest.json and use the signed package format.\n")}
	for _, name := range []string{"go.mod", "server.go", "protocol/json.go", "example/summary.go"} {
		b, e := os.ReadFile(filepath.Join(sdkDirectory, name))
		if e != nil {
			return e
		}
		files["sdk/"+name] = b
	}
	for name, b := range files {
		p := filepath.Join(destination, name)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return e
		}
		if e := os.WriteFile(p, b, 0600); e != nil {
			return e
		}
	}
	return nil
}
func WorkspaceManifest(path string) (Manifest, error) {
	b, e := os.ReadFile(filepath.Join(path, "manifest.json"))
	if e != nil {
		return Manifest{}, e
	}
	return ValidateManifest(b)
}
func EncodeManifest(m Manifest) []byte {
	b, _ := json.MarshalIndent(m, "", "  ")
	return append(b, '\n')
}
