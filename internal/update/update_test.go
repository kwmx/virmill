package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestVersionOrderAndParsing(t *testing.T) {
	order := []string{"0.9.9", "1.0.0-beta.3", "1.0.0-beta.10", "v1.0.0", "1.0.1-beta.1", "1.1.0"}
	for i := 0; i+1 < len(order); i++ {
		a, errA := ParseVersion(order[i])
		b, errB := ParseVersion(order[i+1])
		if errA != nil || errB != nil || !a.Less(b) || b.Less(a) {
			t.Fatalf("%s should sort before %s (%v %v)", order[i], order[i+1], errA, errB)
		}
	}
	for _, bad := range []string{"1.0", "1.0.0-rc.1", "01.0.0", "1.0.0-beta.0", "1.0.0-beta", "latest"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Fatalf("%q parsed", bad)
		}
	}
	if v, _ := ParseVersion("v1.0.0-beta.4"); v.String() != "1.0.0-beta.4" {
		t.Fatal(v)
	}
}

func TestDefaultClientAllowsOnlyGitHubOverHTTPS(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://api.github.com/repos/kwmx/virmill/releases": true, "https://github.com/kwmx/virmill/releases/download/x": true,
		"https://objects.githubusercontent.com/a": true, "https://release-assets.githubusercontent.com/a": true,
		"http://github.com/a": false, "https://github.com.example.org/a": false, "https://example.org/a": false,
	} {
		u, _ := url.Parse(raw)
		if (Default().Allow(u) == nil) != ok {
			t.Fatalf("%s allowed=%v", raw, !ok)
		}
	}
}

type release struct {
	Tag        string      `json:"tag_name"`
	Draft      bool        `json:"draft"`
	Prerelease bool        `json:"prerelease"`
	Page       string      `json:"html_url"`
	Assets     []testAsset `json:"assets"`
}

type testAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest,omitempty"`
}

// fakeGitHub serves a release list and the files under /files/.
type fakeGitHub struct {
	server   *httptest.Server
	releases []release
	files    map[string][]byte
	hits     atomic.Int32
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{files: map[string][]byte{}}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		switch {
		case r.URL.Path == "/repos/kwmx/virmill/releases":
			json.NewEncoder(w).Encode(f.releases)
		case strings.HasPrefix(r.URL.Path, "/files/"):
			b, ok := f.files[strings.TrimPrefix(r.URL.Path, "/files/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write(b)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGitHub) client() Client {
	return Client{API: f.server.URL, Repo: Repository, HTTP: f.server.Client(), Allow: func(*url.URL) error { return nil }}
}

func (f *fakeGitHub) asset(name string, body []byte) testAsset {
	f.files[name] = body
	return testAsset{Name: name, Size: int64(len(body)), URL: f.server.URL + "/files/" + name, Digest: "sha256:" + sum(body)}
}

func TestCheckFindsNewestRelevantRelease(t *testing.T) {
	f := newFakeGitHub(t)
	f.releases = []release{
		{Tag: "v1.0.0-beta.3", Prerelease: true}, {Tag: "v1.0.0-beta.5", Draft: true}, {Tag: "v1.0.0-beta.4", Prerelease: true, Page: "https://github.com/kwmx/virmill/releases/tag/v1.0.0-beta.4",
			Assets: []testAsset{{Name: "SHA256SUMS", Size: 1, URL: "https://github.com/x", Digest: "sha256:" + strings.Repeat("a", 64)}}},
		{Tag: "nightly"}, {Tag: "v0.9.0"},
	}
	res, err := f.client().Check(context.Background(), "1.0.0-beta.3")
	if err != nil || !res.Available() || res.Latest.Version != "1.0.0-beta.4" || res.Latest.Page == "" ||
		len(res.Latest.Assets) != 1 || res.Latest.Assets[0].Digest != "sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("beta user: %+v %v", res, err)
	}
	if res, err = f.client().Check(context.Background(), "1.0.0-beta.4"); err != nil || res.Available() {
		t.Fatalf("up to date: %+v %v", res, err)
	}
	f.releases = append(f.releases, release{Tag: "v1.0.0"}, release{Tag: "v1.0.1-beta.1", Prerelease: true})
	if res, _ = f.client().Check(context.Background(), "0.9.0"); !res.Available() || res.Latest.Version != "1.0.0" {
		t.Fatalf("stable users are not offered betas: %+v", res.Latest)
	}
	if res, _ = f.client().Check(context.Background(), "1.0.0-beta.3"); res.Latest.Version != "1.0.1-beta.1" {
		t.Fatalf("beta users get the newest version: %+v", res.Latest)
	}
	c := f.client()
	c.Repo = "missing/repo"
	if _, err = c.Check(context.Background(), "1.0.0-beta.3"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing repository: %v", err)
	}
}

func TestCheckIfDueCachesAndRespectsTheSetting(t *testing.T) {
	f := newFakeGitHub(t)
	f.releases = []release{{Tag: "v1.0.0-beta.4", Prerelease: true}}
	p := Paths{Config: filepath.Join(t.TempDir(), "config"), Cache: filepath.Join(t.TempDir(), "cache")}
	now := time.Now()
	if r := CheckIfDue(context.Background(), f.client(), p, "1.0.0-beta.3", now); !r.Available() || f.hits.Load() != 1 {
		t.Fatalf("first check %+v hits=%d", r, f.hits.Load())
	}
	if r := CheckIfDue(context.Background(), f.client(), p, "1.0.0-beta.3", now.Add(time.Hour)); !r.Available() || f.hits.Load() != 1 {
		t.Fatalf("fresh result was not reused: hits=%d", f.hits.Load())
	}
	CheckIfDue(context.Background(), f.client(), p, "1.0.0-beta.4", now.Add(time.Hour))
	CheckIfDue(context.Background(), f.client(), p, "1.0.0-beta.4", now.Add(25*time.Hour))
	if f.hits.Load() != 3 {
		t.Fatalf("new version or a day later must check again: hits=%d", f.hits.Load())
	}
	st, _ := os.Stat(filepath.Join(p.Cache, "update-check.json"))
	if st == nil || st.Mode().Perm() != 0600 {
		t.Fatalf("cache file mode %v", st)
	}
	offline := f.client()
	offline.API = "https://127.0.0.1:1"
	failed := CheckIfDue(context.Background(), offline, p, "1.0.0-beta.3", now.Add(50*time.Hour))
	if failed.Error == "" || !Due(failed, true, "1.0.0-beta.3", now.Add(57*time.Hour)) || Due(failed, true, "1.0.0-beta.3", now.Add(51*time.Hour)) {
		t.Fatalf("failed check is saved and retried after six hours: %+v", failed)
	}

	if !p.ChecksEnabled() {
		t.Fatal("checks are on by default")
	}
	if err := p.SetChecks(false); err != nil || p.ChecksEnabled() {
		t.Fatal("checks off not saved", err)
	}
	t.Setenv("VIRMILL_UPDATE_CHECK", "1")
	if !p.ChecksEnabled() {
		t.Fatal("environment overrides the setting")
	}
	t.Setenv("VIRMILL_UPDATE_CHECK", "0")
	p.SetChecks(true)
	if p.ChecksEnabled() {
		t.Fatal("VIRMILL_UPDATE_CHECK=0 turns checks off")
	}
}

func TestDetectInstallAsksThePackageManager(t *testing.T) {
	fake := func(answers map[string]string) Runner {
		return func(_ context.Context, name string, args ...string) (string, error) {
			if out, ok := answers[name+" "+strings.Join(args, " ")]; ok {
				return out, nil
			}
			return "", errors.New("exit status 1")
		}
	}
	got, err := DetectInstall(context.Background(), fake(map[string]string{
		"rpm -qf --queryformat %{NAME} /usr/bin/virmill": "virmill", "rpm -q virmill-host-helper": "virmill-host-helper-1.0.0-0.beta.3.x86_64"}), "/usr/bin/virmill")
	if err != nil || got != (Installed{Format: "rpm", Helper: true}) {
		t.Fatal(got, err)
	}
	got, err = DetectInstall(context.Background(), fake(map[string]string{"dpkg-query -S /usr/bin/virmill": "virmill: /usr/bin/virmill"}), "/usr/bin/virmill")
	if err != nil || got != (Installed{Format: "deb"}) {
		t.Fatal(got, err)
	}
	if _, err = DetectInstall(context.Background(), fake(nil), "/home/u/src/virmill/build/bin/virmill"); err == nil || !strings.Contains(err.Error(), "cannot be updated automatically") {
		t.Fatal("source build accepted", err)
	}
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// betaFour is a release with RPM and DEB packages and a matching SHA256SUMS.
func betaFour(f *fakeGitHub, sums func(map[string][]byte) string) Release {
	files := map[string][]byte{
		"virmill-1.0.0-0.beta.4.x86_64.rpm": []byte("core rpm"), "virmill-host-helper-1.0.0-0.beta.4.x86_64.rpm": []byte("helper rpm"),
		"virmill_1.0.0.beta.4_amd64.deb": []byte("core deb"), "virmill-host-helper_1.0.0.beta.4_amd64.deb": []byte("helper deb"),
	}
	r := Release{Version: "1.0.0-beta.4"}
	for name, b := range files {
		a := f.asset(name, b)
		r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size, Digest: a.Digest})
	}
	a := f.asset("SHA256SUMS", []byte(sums(files)))
	return Release{Version: r.Version, Assets: append(r.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size, Digest: a.Digest})}
}

func goodSums(files map[string][]byte) string {
	var b strings.Builder
	for name, body := range files {
		fmt.Fprintf(&b, "%s  %s\n", sum(body), name)
	}
	return b.String()
}

// packageInfo answers rpm -qp and dpkg-deb --show from the fake file names.
func packageInfo(_ context.Context, name string, args ...string) (string, error) {
	file := filepath.Base(args[len(args)-1])
	helper := strings.HasPrefix(file, "virmill-host-helper")
	pkg := "virmill"
	if helper {
		pkg = "virmill-host-helper"
	}
	switch name {
	case "rpm":
		return pkg + " 1.0.0 0.beta.4 x86_64", nil
	case "dpkg-deb":
		return pkg + " 1.0.0~beta.4 amd64", nil
	}
	return "", errors.New("unexpected command " + name)
}

func TestPrepareDownloadsAndVerifiesPackages(t *testing.T) {
	f := newFakeGitHub(t)
	r := betaFour(f, goodSums)
	cache := t.TempDir()
	files, err := f.client().Prepare(context.Background(), r, Installed{Format: "rpm", Helper: true}, cache, packageInfo)
	want := []string{filepath.Join(cache, "updates/1.0.0-beta.4/virmill-1.0.0-0.beta.4.x86_64.rpm"), filepath.Join(cache, "updates/1.0.0-beta.4/virmill-host-helper-1.0.0-0.beta.4.x86_64.rpm")}
	if err != nil || !reflect.DeepEqual(files, want) {
		t.Fatal(files, err)
	}
	for _, path := range files {
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0600 {
			t.Fatalf("%s: %v %v", path, st, err)
		}
	}
	files, err = f.client().Prepare(context.Background(), r, Installed{Format: "deb"}, cache, packageInfo)
	if err != nil || len(files) != 1 || filepath.Base(files[0]) != "virmill_1.0.0.beta.4_amd64.deb" {
		t.Fatal(files, err)
	}
	if got := InstallCommand("deb", files); !reflect.DeepEqual(got, append([]string{"sudo", "apt", "install"}, files...)) {
		t.Fatal(got)
	}
	if got := InstallCommand("rpm", []string{"/a.rpm"}); !reflect.DeepEqual(got, []string{"sudo", "dnf", "install", "/a.rpm"}) {
		t.Fatal(got)
	}
}

func TestPrepareRefusesFilesThatDoNotMatch(t *testing.T) {
	core := "virmill-1.0.0-0.beta.4.x86_64.rpm"
	setDigest := func(r *Release, name, digest string) {
		for i := range r.Assets {
			if r.Assets[i].Name == name {
				r.Assets[i].Digest = digest
			}
		}
	}
	for name, tc := range map[string]struct {
		sums  func(map[string][]byte) string
		edit  func(*fakeGitHub, *Release)
		info  Runner
		fails string
	}{
		"sums disagree with GitHub": {sums: func(files map[string][]byte) string {
			return strings.Replace(goodSums(files), sum(files[core]), strings.Repeat("0", 64), 1)
		}, fails: "disagree"},
		"tampered download": {edit: func(f *fakeGitHub, r *Release) { f.files[core] = []byte("core rpX") }, fails: "does not match its SHA-256"},
		"no GitHub digest":  {edit: func(f *fakeGitHub, r *Release) { setDigest(r, core, "") }, fails: "lists no SHA-256 digest"},
		"odd GitHub digest": {edit: func(f *fakeGitHub, r *Release) { setDigest(r, core, "md5:abc") }, fails: "lists no SHA-256 digest"},
		"sums file altered": {edit: func(f *fakeGitHub, r *Release) { setDigest(r, "SHA256SUMS", "sha256:"+strings.Repeat("0", 64)) },
			fails: "SHA256SUMS does not match the digest GitHub recorded"},
		"unlisted file":  {sums: func(map[string][]byte) string { return strings.Repeat("a", 64) + "  other.rpm\n" }, fails: "does not list"},
		"malformed sums": {sums: func(map[string][]byte) string { return "not a checksum line\n" }, fails: "is not a checksum"},
		"size differs": {edit: func(f *fakeGitHub, r *Release) {
			for i := range r.Assets {
				r.Assets[i].Size++
			}
		}, fails: "bytes, but the release lists"},
		"no sums file": {edit: func(f *fakeGitHub, r *Release) { r.Assets = r.Assets[:len(r.Assets)-1] }, fails: "no SHA256SUMS"},
		"two core packages": {edit: func(f *fakeGitHub, r *Release) {
			r.Assets = append(r.Assets, Asset{Name: "virmill-1.0.0-0.beta.4.x86_64.rpm.bak.x86_64.rpm", Size: 1})
		}, fails: "expected one"},
		"unsafe name": {edit: func(f *fakeGitHub, r *Release) {
			for i := range r.Assets {
				if strings.HasPrefix(r.Assets[i].Name, "virmill-1.0.0") {
					r.Assets[i].Name = "../virmill-1.0.0-0.beta.4.x86_64.rpm"
				}
			}
		}, fails: "expected one"},
		"wrong package inside": {info: func(context.Context, string, ...string) (string, error) { return "virmill 1.0.0 0.beta.3 x86_64", nil },
			fails: "is not the expected package"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeGitHub(t)
			sums := goodSums
			if tc.sums != nil {
				sums = tc.sums
			}
			r := betaFour(f, sums)
			if tc.edit != nil {
				tc.edit(f, &r)
			}
			info := packageInfo
			if tc.info != nil {
				info = tc.info
			}
			cache := t.TempDir()
			files, err := f.client().Prepare(context.Background(), r, Installed{Format: "rpm"}, cache, info)
			if err == nil || !strings.Contains(err.Error(), tc.fails) {
				t.Fatalf("want %q, got %v %v", tc.fails, files, err)
			}
			left, _ := filepath.Glob(filepath.Join(cache, "updates", "*", "*"))
			if len(left) != 0 {
				t.Fatalf("refused download left files: %v", left)
			}
		})
	}
}

func TestStableReleasesNeedAManualInstall(t *testing.T) {
	v, _ := ParseVersion("1.0.0")
	if err := checkPackage(context.Background(), packageInfo, "rpm", "/x.rpm", "virmill", v); err == nil || !strings.Contains(err.Error(), "manual install") {
		t.Fatal(err)
	}
}
