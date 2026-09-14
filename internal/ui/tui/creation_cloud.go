package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"virmill.local/core/internal/domain"
)

// Cloud images have no password: cloud-init creates the user and installs the
// public key through the reviewed NoCloud profile (ADR 0010, ADR 0059).
const cloudSeedMedia = "cloud-init"

var cloudKeyPattern = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa|ecdsa-sha2-nistp(256|384|521)) [A-Za-z0-9+/]+=*$`)
var cloudUserPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

type cloudSetup struct {
	User, KeyFile, Reference string
	Sudo                     bool
}

// defaultCloudSetup suggests the login name and the first public key found.
func defaultCloudSetup() cloudSetup {
	c := cloudSetup{User: "user", Sudo: true}
	if name := strings.ToLower(os.Getenv("USER")); cloudUserPattern.MatchString(name) && name != "root" {
		c.User = name
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, key := range []string{"id_ed25519.pub", "id_ecdsa.pub", "id_rsa.pub"} {
			path := filepath.Join(home, ".ssh", key)
			if st, err := os.Lstat(path); err == nil && st.Mode().IsRegular() {
				c.KeyFile = path
				break
			}
		}
	}
	return c
}

// looksLikeCloudImage matches common cloud image names (cloudimg, genericcloud,
// Cloud-Base). It only preselects the choice; the user confirms it in review.
func looksLikeCloudImage(name string) bool {
	return strings.Contains(strings.ToLower(name), "cloud")
}

// suggestCloud turns cloud-init setup on for a cloud-looking disk image once;
// a user's own choice is never overridden.
func (f *CreationForm) suggestCloud() {
	if f == nil || f.CloudDecided || f.CloudEnabled || f.Source.Kind != "PreparedDiskSet" || !looksLikeCloudImage(f.Spec.Name) {
		return
	}
	f.CloudEnabled, f.Cloud, f.CloudOrigin = true, defaultCloudSetup(), "Suggested: the name looks like a cloud image."
}

// readPublicKeys returns public keys without options or comments. A private
// key is refused before any of its content is used.
func readPublicKeys(path string) ([]string, error) {
	if !guidedPath(path) {
		return nil, fmt.Errorf("SSH public key: enter the full path of your key's .pub file.")
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<10 {
		return nil, fmt.Errorf("SSH public key: %s is not a readable public key file.", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("SSH public key: %s could not be read.", path)
	}
	if strings.Contains(string(b), "PRIVATE KEY") {
		return nil, fmt.Errorf("SSH public key: that file is a private key. Choose the matching .pub file.")
	}
	keys := []string{}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if key := fields[0] + " " + fields[1]; cloudKeyPattern.MatchString(key) && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 || len(keys) > 32 {
		return nil, fmt.Errorf("SSH public key: %s has no usable ssh-ed25519, ssh-rsa or ecdsa public key.", path)
	}
	return keys, nil
}

// cloudHostname derives a valid hostname from the VM name.
func cloudHostname(name string) string {
	h := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, strings.ToLower(name))
	for strings.Contains(h, "--") {
		h = strings.ReplaceAll(h, "--", "-")
	}
	h = strings.Trim(h, "-")
	if len(h) > 63 {
		h = strings.Trim(h[:63], "-")
	}
	if h == "" {
		h = "vm"
	}
	return h
}

// cloudSeedID is the seed medium's ID, distinct from every disk and medium.
func cloudSeedID(s domain.CreationSpec) string {
	id := cloudSeedMedia
	for {
		taken := slices.ContainsFunc(s.Disks, func(d domain.CreationDisk) bool { return d.SourceID == id }) ||
			slices.ContainsFunc(s.Media, func(m domain.CreationMedia) bool { return m.SourceID == id })
		if !taken {
			return id
		}
		id += "-seed"
	}
}

// cloudProvisioning builds the reviewed NoCloud declaration. Before
// preparation the source digest is not known yet; the one-approval chain binds
// it to that preparation and the service checks it (ADR 0059).
func (f CreationForm) cloudProvisioning(s domain.CreationSpec, seed string) (map[string]any, error) {
	boot := ""
	for _, d := range s.Disks {
		if d.BootOrder == 1 {
			boot = d.SourceID
		}
	}
	if boot == "" {
		return nil, fmt.Errorf("Cloud image: the cloud image disk must boot first.")
	}
	digest := ""
	for _, d := range f.Source.Disks {
		if d.SourceID != boot {
			continue
		}
		for _, file := range f.Source.SourceFiles {
			if d.SourcePath != "" && file.Path == d.SourcePath {
				digest = file.SHA256
			}
		}
	}
	if digest == "" {
		if !f.BeforePreparation {
			return nil, fmt.Errorf("Cloud image: these prepared images have no recorded source digest. Prepare the image again.")
		}
		digest = strings.Repeat("0", 64)
	}
	if !cloudUserPattern.MatchString(f.Cloud.User) || f.Cloud.User == "root" {
		return nil, fmt.Errorf("Cloud user name: use lowercase letters, digits, - or _, not root.")
	}
	keys, err := readPublicKeys(f.Cloud.KeyFile)
	if err != nil {
		return nil, err
	}
	ref := f.Cloud.Reference
	if !strings.HasPrefix(ref, "https://") || len(ref) > 2048 || strings.ContainsAny(ref, " \t\r\n?#") || strings.Contains(ref, "@") {
		return nil, fmt.Errorf("Downloaded from: enter the https:// address of this image, without a query or login. Virmill records it; it does not download it.")
	}
	interfaces := []any{}
	normal := false
	for i, n := range s.NICs {
		e := map[string]any{"nicID": n.ID, "role": "normal", "mode": "none", "address": "", "gateway": "", "dns": []any{}, "defaultRoute": false, "routeMetric": float64(100 + i)}
		if n.Link == "up" {
			e["mode"] = "dhcp"
			if normal {
				e["role"] = "protected"
			} else {
				e["defaultRoute"], normal = true, true
			}
		}
		interfaces = append(interfaces, e)
	}
	authorized := []any{}
	for _, k := range keys {
		authorized = append(authorized, k)
	}
	return map[string]any{"profile": "nocloud-netplan-ipv4-v1", "cloudInitCompatible": true, "sourceDiskID": boot, "sourceSHA256": digest,
		"sourceReference": ref, "hostname": cloudHostname(s.Name), "userName": f.Cloud.User, "authorizedKeys": authorized,
		"passwordlessSudo": f.Cloud.Sudo, "ipv6": "disabled", "mediaID": seed, "interfaces": interfaces}, nil
}
