package update

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const (
	maxPackage   = 512 << 20
	maxChecksums = 64 << 10
)

// Installed describes the packages that put Virmill on this host.
type Installed struct {
	Format string // "rpm" or "deb"
	Helper bool   // virmill-host-helper is installed too
}

// Runner runs a read-only query command and returns its trimmed output.
type Runner func(ctx context.Context, name string, args ...string) (string, error)

func ExecRunner(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// DetectInstall asks rpm or dpkg which package owns the running binary.
func DetectInstall(ctx context.Context, run Runner, binary string) (Installed, error) {
	if out, err := run(ctx, "rpm", "-qf", "--queryformat", "%{NAME}", binary); err == nil && out == "virmill" {
		_, helperErr := run(ctx, "rpm", "-q", "virmill-host-helper")
		return Installed{Format: "rpm", Helper: helperErr == nil}, nil
	}
	if out, err := run(ctx, "dpkg-query", "-S", binary); err == nil && strings.HasPrefix(out, "virmill: ") {
		status, _ := run(ctx, "dpkg-query", "-W", "-f=${Status}", "virmill-host-helper")
		return Installed{Format: "deb", Helper: status == "install ok installed"}, nil
	}
	return Installed{}, fmt.Errorf("%s was not installed from a Virmill RPM or DEB package, so it cannot be updated automatically", binary)
}

var safeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~-]{0,200}$`)

// packageAsset finds the one release file for a package in the given format.
func packageAsset(r Release, name, format string) (Asset, error) {
	var found []Asset
	for _, a := range r.Assets {
		var match bool
		switch format {
		case "rpm":
			match = strings.HasPrefix(a.Name, name+"-") && strings.HasSuffix(a.Name, ".x86_64.rpm") &&
				!(name == "virmill" && strings.HasPrefix(a.Name, "virmill-host-helper"))
		case "deb":
			match = strings.HasPrefix(a.Name, name+"_") && strings.HasSuffix(a.Name, "_amd64.deb")
		}
		if match {
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		return Asset{}, fmt.Errorf("release %s has %d %s packages for %s, expected one", r.Version, len(found), format, name)
	}
	if !safeName.MatchString(found[0].Name) {
		return Asset{}, fmt.Errorf("release file name %q is not allowed", found[0].Name)
	}
	return found[0], nil
}

// Prepare downloads the packages this host needs from release r into
// cache/updates/VERSION. Each file must match the release's SHA256SUMS, the
// SHA-256 digest GitHub recorded for the upload and the size it reports, and
// must name itself as the expected package and version.
func (c Client) Prepare(ctx context.Context, r Release, inst Installed, cache string, run Runner) ([]string, error) {
	v, err := ParseVersion(r.Version)
	if err != nil {
		return nil, err
	}
	if runtime.GOARCH != "amd64" {
		return nil, errors.New("Virmill packages are built for x86-64 only")
	}
	names := []string{"virmill"}
	if inst.Helper {
		names = append(names, "virmill-host-helper")
	}
	assets := make([]Asset, len(names))
	for i, name := range names {
		if assets[i], err = packageAsset(r, name, inst.Format); err != nil {
			return nil, err
		}
	}
	sums, err := c.checksums(ctx, r)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cache, "updates", v.String())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if st, err := os.Lstat(dir); err != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return nil, fmt.Errorf("download folder %s must be a private directory (mode 0700)", dir)
	}
	files := make([]string, len(assets))
	for i, a := range assets {
		want, ok := sums[a.Name]
		if !ok {
			return nil, fmt.Errorf("SHA256SUMS of release %s does not list %s", r.Version, a.Name)
		}
		recorded, err := githubDigest(a)
		if err != nil {
			return nil, err
		}
		if recorded != want {
			return nil, fmt.Errorf("SHA256SUMS and the digest GitHub recorded for %s disagree; it was not downloaded", a.Name)
		}
		files[i] = filepath.Join(dir, a.Name)
		if err := c.download(ctx, a, want, files[i]); err != nil {
			return nil, err
		}
		if err := checkPackage(ctx, run, inst.Format, files[i], names[i], v); err != nil {
			os.Remove(files[i])
			return nil, err
		}
	}
	return files, nil
}

func (c Client) checksums(ctx context.Context, r Release) (map[string]string, error) {
	var asset *Asset
	for i := range r.Assets {
		if r.Assets[i].Name == "SHA256SUMS" {
			if asset != nil {
				return nil, errors.New("release lists SHA256SUMS twice")
			}
			asset = &r.Assets[i]
		}
	}
	if asset == nil {
		return nil, fmt.Errorf("release %s has no SHA256SUMS file", r.Version)
	}
	recorded, err := githubDigest(*asset)
	if err != nil {
		return nil, err
	}
	resp, err := c.get(ctx, asset.URL, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxChecksums+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxChecksums {
		return nil, errors.New("SHA256SUMS is larger than expected")
	}
	if h := sha256.Sum256(body); hex.EncodeToString(h[:]) != recorded {
		return nil, errors.New("SHA256SUMS does not match the digest GitHub recorded for it")
	}
	sums := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if len(fields) != 2 || len(fields[0]) != 64 || strings.Trim(fields[0], "0123456789abcdef") != "" || !safeName.MatchString(name) {
			return nil, fmt.Errorf("SHA256SUMS line is not a checksum: %q", scanner.Text())
		}
		sums[name] = fields[0]
	}
	return sums, scanner.Err()
}

// githubDigest is the SHA-256 GitHub recorded when the file was uploaded.
func githubDigest(a Asset) (string, error) {
	digest, ok := strings.CutPrefix(a.Digest, "sha256:")
	if !ok || len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" {
		return "", fmt.Errorf("GitHub lists no SHA-256 digest for %s, so it cannot be checked", a.Name)
	}
	return digest, nil
}

// download writes a to path only when its size and SHA-256 match.
func (c Client) download(ctx context.Context, a Asset, want, path string) error {
	if a.Size <= 0 || a.Size > maxPackage {
		return fmt.Errorf("%s has an unexpected size of %d bytes", a.Name, a.Size)
	}
	resp, err := c.get(ctx, a.URL, "application/octet-stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	part := path + ".part"
	os.Remove(part)
	f, err := os.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(part)
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, a.Size+1))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	switch {
	case err != nil:
		return fmt.Errorf("downloading %s: %w", a.Name, err)
	case n != a.Size:
		return fmt.Errorf("%s is %d bytes, but the release lists %d", a.Name, n, a.Size)
	case hex.EncodeToString(h.Sum(nil)) != want:
		return fmt.Errorf("%s does not match its SHA-256 in SHA256SUMS; it was not kept", a.Name)
	}
	return os.Rename(part, path)
}

// checkPackage asks rpm or dpkg-deb what the downloaded file says it is.
func checkPackage(ctx context.Context, run Runner, format, path, name string, v Version) error {
	var out, want string
	var err error
	switch format {
	case "rpm":
		version, release, ok := v.rpmVersion()
		if !ok {
			return fmt.Errorf("Virmill %s needs a manual install: its package version is not defined yet", v)
		}
		want = fmt.Sprintf("%s %s %s x86_64", name, version, release)
		out, err = run(ctx, "rpm", "-qp", "--nosignature", "--queryformat", "%{NAME} %{VERSION} %{RELEASE} %{ARCH}", path)
	case "deb":
		version, ok := v.debVersion()
		if !ok {
			return fmt.Errorf("Virmill %s needs a manual install: its package version is not defined yet", v)
		}
		want = fmt.Sprintf("%s %s amd64", name, version)
		out, err = run(ctx, "dpkg-deb", "--show", "--showformat=${Package} ${Version} ${Architecture}", path)
	default:
		return fmt.Errorf("unknown package format %q", format)
	}
	if err != nil || out != want {
		return fmt.Errorf("%s is not the expected package %q (it reports %q)", filepath.Base(path), want, out)
	}
	return nil
}

// InstallCommand is the argv that installs prepared packages with the
// distribution's package manager, which asks for the user's password.
func InstallCommand(format string, files []string) []string {
	tool := "dnf"
	if format == "deb" {
		tool = "apt"
	}
	return append([]string{"sudo", tool, "install"}, files...)
}
