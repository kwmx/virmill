package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/buildinfo"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/update"
)

const newerTag = "v1.0.0-beta.99"

type jobsClient struct{ jobs []domain.Job }

func (j jobsClient) Call(ctx context.Context, method string, r app.Request) (app.Response, error) {
	if method != "operation.list" {
		return app.Response{}, errors.New("unexpected " + method)
	}
	return app.Response{APIVersion: domain.APIVersion, Data: j.jobs, Warnings: []string{}}, nil
}

// useFakeUpdater serves one newer release with an RPM and its SHA256SUMS, and
// records the commands the updater would run.
func useFakeUpdater(t *testing.T, packaged bool) (*[][]string, update.Paths) {
	t.Helper()
	body := []byte("core rpm")
	h := sha256.Sum256(body)
	files := map[string][]byte{"virmill-1.0.0-0.beta.99.x86_64.rpm": body,
		"SHA256SUMS": []byte(hex.EncodeToString(h[:]) + "  virmill-1.0.0-0.beta.99.x86_64.rpm\n")}
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/kwmx/virmill/releases" {
			assets := []map[string]any{}
			for name, b := range files {
				assets = append(assets, map[string]any{"name": name, "size": len(b), "browser_download_url": srv.URL + "/files/" + name})
			}
			json.NewEncoder(w).Encode([]map[string]any{{"tag_name": newerTag, "prerelease": true,
				"html_url": "https://github.com/kwmx/virmill/releases/tag/" + newerTag, "assets": assets}})
			return
		}
		if b, ok := files[strings.TrimPrefix(r.URL.Path, "/files/")]; ok {
			w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	paths := update.Paths{Config: filepath.Join(dir, "config"), Cache: filepath.Join(dir, "cache")}
	ran := &[][]string{}
	oldClient, oldPaths, oldQuery, oldExec, oldBinary := updateClient, updatePaths, updateQuery, updateExec, updateBinary
	t.Cleanup(func() {
		updateClient, updatePaths, updateQuery, updateExec, updateBinary = oldClient, oldPaths, oldQuery, oldExec, oldBinary
	})
	updateClient = func() update.Client {
		return update.Client{API: srv.URL, Repo: update.Repository, HTTP: srv.Client(), Allow: func(*url.URL) error { return nil }}
	}
	updatePaths = func() (update.Paths, error) { return paths, nil }
	updateBinary = func() (string, error) { return "/usr/bin/virmill", nil }
	updateQuery = func(_ context.Context, name string, args ...string) (string, error) {
		switch {
		case packaged && name == "rpm" && args[0] == "-qf":
			return "virmill", nil
		case name == "rpm" && args[0] == "-qp":
			return "virmill 1.0.0 0.beta.99 x86_64", nil
		}
		return "", errors.New("exit status 1")
	}
	updateExec = func(argv []string) error {
		*ran = append(*ran, argv)
		return nil
	}
	return ran, paths
}

func runUpdate(t *testing.T, client jobsClient, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := New(client, &out, &out)
	c.SetIn(strings.NewReader(stdin))
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), err
}

func TestUpdateCheckReportsNewerRelease(t *testing.T) {
	_, paths := useFakeUpdater(t, true)
	out, err := runUpdate(t, jobsClient{}, "", "update", "check")
	if err != nil || !strings.Contains(out, "Virmill 1.0.0-beta.99 is available (you have "+buildinfo.Version+").") ||
		!strings.Contains(out, "releases/tag/"+newerTag) || !strings.Contains(out, "Install it with: virmill update") {
		t.Fatalf("check: %v\n%s", err, out)
	}
	if cached, ok := paths.Cached(); !ok || !cached.Available() {
		t.Fatal("check result not saved", cached)
	}
	out, err = runUpdate(t, jobsClient{}, "", "update", "check", "--output", "json")
	var resp struct {
		Data update.Result `json:"data"`
	}
	if err != nil || json.Unmarshal([]byte(out), &resp) != nil || resp.Data.Latest.Version != "1.0.0-beta.99" {
		t.Fatalf("json check: %v\n%s", err, out)
	}
}

func TestUpdateDownloadOnlyVerifiesAndPrintsCommand(t *testing.T) {
	ran, paths := useFakeUpdater(t, true)
	out, err := runUpdate(t, jobsClient{}, "", "update", "--yes", "--download-only")
	file := filepath.Join(paths.Cache, "updates", "1.0.0-beta.99", "virmill-1.0.0-0.beta.99.x86_64.rpm")
	if err != nil || !strings.Contains(out, "Verified:") || !strings.Contains(out, "Install it with:\n  sudo dnf install "+file+"\n") || len(*ran) != 0 {
		t.Fatalf("download only: %v %v\n%s", err, *ran, out)
	}
}

func TestUpdateInstallsAndRestartsCoordinator(t *testing.T) {
	ran, paths := useFakeUpdater(t, true)
	finished := jobsClient{jobs: []domain.Job{{ID: "a", State: "succeeded"}, {ID: "b", State: "recovery-required"}}}
	out, err := runUpdate(t, finished, "", "update", "--yes")
	file := filepath.Join(paths.Cache, "updates", "1.0.0-beta.99", "virmill-1.0.0-0.beta.99.x86_64.rpm")
	want := [][]string{{"sudo", "dnf", "install", file}, {"systemctl", "--user", "try-restart", "virmilld.service"}}
	if err != nil || !reflect.DeepEqual(*ran, want) || !strings.Contains(out, "Virmill 1.0.0-beta.99 is installed.") {
		t.Fatalf("install: %v %v\n%s", err, *ran, out)
	}
}

func TestUpdateRefusesUnsafeOrUnconfirmedInstalls(t *testing.T) {
	ran, _ := useFakeUpdater(t, true)
	_, err := runUpdate(t, jobsClient{jobs: []domain.Job{{ID: "a", State: "running"}}}, "", "update", "--yes")
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != "RESOURCE_BUSY" || len(*ran) != 0 {
		t.Fatalf("running job: %v %v", err, *ran)
	}
	if _, err = runUpdate(t, jobsClient{}, "", "update", "--non-interactive"); !errors.As(err, &failure) || failure.Code != "INVALID_INPUT" {
		t.Fatalf("non-interactive without --yes: %v", err)
	}
	out, err := runUpdate(t, jobsClient{}, "n\n", "update")
	if err != nil || !strings.Contains(out, "[y/N]") || !strings.Contains(out, "Nothing was changed.") || len(*ran) != 0 {
		t.Fatalf("declined: %v %v\n%s", err, *ran, out)
	}
	if _, err = runUpdate(t, jobsClient{}, "", "update", "--output", "json"); !errors.As(err, &failure) || failure.Code != "INVALID_INPUT" {
		t.Fatalf("json install: %v", err)
	}
	useFakeUpdater(t, false)
	if _, err = runUpdate(t, jobsClient{}, "", "update", "--yes"); !errors.As(err, &failure) || failure.Code != "UNSUPPORTED_CAPABILITY" ||
		!strings.Contains(failure.Message, "cannot be updated automatically") {
		t.Fatalf("source build: %v", err)
	}
}

func TestUpdateChecksSetting(t *testing.T) {
	useFakeUpdater(t, true)
	t.Setenv("VIRMILL_UPDATE_CHECK", "")
	for _, tc := range []struct{ args, want string }{
		{"update checks", "Daily update checks are on."}, {"update checks off", "Daily update checks are off."}, {"update checks", "Daily update checks are off."},
		{"update check", "Daily checks are off. Turn them on with: virmill update checks on"}, {"update checks on", "Daily update checks are on."},
	} {
		out, err := runUpdate(t, jobsClient{}, "", strings.Fields(tc.args)...)
		if err != nil || !strings.Contains(out, tc.want) {
			t.Fatalf("%s: %v\n%s", tc.args, err, out)
		}
	}
	if _, err := runUpdate(t, jobsClient{}, "", "update", "checks", "maybe"); err == nil {
		t.Fatal("checks accepted an unknown value")
	}
}
