package update

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestLiveGitHubRelease reads the real release list and verifies the newest
// release's packages with the host's rpm or dpkg-deb. It downloads about 30
// MB, so it runs only with VIRMILL_TEST_GITHUB=1; it installs nothing.
func TestLiveGitHubRelease(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_GITHUB") != "1" {
		t.Skip("set VIRMILL_TEST_GITHUB=1 to contact GitHub")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c := Default()
	res, err := c.Check(ctx, "1.0.0-beta.2")
	if err != nil || !res.Available() {
		t.Fatalf("GitHub offered nothing newer than beta.2: %+v %v", res, err)
	}
	latest, _ := ParseVersion(res.Latest.Version)
	if beta3, _ := ParseVersion("1.0.0-beta.3"); latest.Less(beta3) {
		t.Fatalf("newest release %s is older than the published beta.3", latest)
	}
	format := "rpm"
	if _, err := exec.LookPath("rpm"); err != nil {
		format = "deb"
	}
	files, err := c.Prepare(ctx, *res.Latest, Installed{Format: format, Helper: true}, t.TempDir(), ExecRunner)
	if err != nil || len(files) != 2 {
		t.Fatalf("release %s did not verify: %v %v", res.Latest.Version, files, err)
	}
	t.Logf("verified %s %s packages from %s", res.Latest.Version, format, res.Latest.Page)
}
