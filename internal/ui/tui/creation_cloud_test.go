package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"virmill.local/core/internal/domain"
)

const cloudTestKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGNsb3VkLXRlc3Qta2V5LW9ubHktZm9yLXZpcm1pbGw"
const cloudTestDigest = "3b5d5c3712955042212316173ccf37be800b2d33b9a8a8f0f0f0f0f0f0f0f0f0"

func cloudKeyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "id_ed25519.pub")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func cloudForm(t *testing.T, before bool) CreationForm {
	t.Helper()
	base := creationFormFixture()
	nat := domain.VirtualNetwork{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "network", UUID: creationFormNetwork}, Name: "default", Active: true, PersistentXML: "<network><forward mode='nat'/></network>"}
	pools := []domain.StoragePool{{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "storage-pool", UUID: creationFormPool}, Name: "default", Type: "dir", Active: true}}
	source := CreationSource{Kind: "PreparedDiskSet", Disks: []CreationSourceDisk{{SourceID: "disk1", SourcePath: "jammy-server-cloudimg-amd64.img"}}, SourceFiles: []CreationSourceFile{{Path: "jammy-server-cloudimg-amd64.img", SHA256: cloudTestDigest}}}
	op := creationFormOperation
	if before {
		source.SourceFiles, op = nil, ""
	}
	f := NewCreationForm(op, source, base.Options, pools, []domain.VirtualNetwork{nat})
	f.BeforePreparation = before
	f.Spec.Name = "jammy-server-cloudimg-amd64"
	f.suggestCloud()
	f.Cloud.KeyFile = cloudKeyFile(t, cloudTestKey+" user@laptop\n")
	f.Cloud.User = "owner"
	f.Cloud.Reference = "https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img"
	return f
}

func TestPublicKeysAreReadWithoutCommentsAndPrivateKeysRefused(t *testing.T) {
	keys, err := readPublicKeys(cloudKeyFile(t, cloudTestKey+" me@host\n\n"+cloudTestKey+" duplicate\n"))
	if err != nil || !slices.Equal(keys, []string{cloudTestKey}) {
		t.Fatal("public key parsing", keys, err)
	}
	for _, content := range []string{"-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n", "not a key\n", ""} {
		if _, err := readPublicKeys(cloudKeyFile(t, content)); err == nil || !strings.HasPrefix(err.Error(), "SSH public key:") {
			t.Fatal("unusable key file accepted", content, err)
		}
	}
	if _, err := readPublicKeys("relative/key.pub"); err == nil {
		t.Fatal("relative key path accepted")
	}
	for in, want := range map[string]string{"Jammy Server 22.04": "jammy-server-22-04", "--": "vm", "Fedora_Cloud Base": "fedora-cloud-base"} {
		if got := cloudHostname(in); got != want {
			t.Fatalf("cloudHostname(%q) = %q", in, got)
		}
	}
}

func TestCloudImageSetupBuildsTheReviewedNoCloudDeclaration(t *testing.T) {
	f := cloudForm(t, false)
	if !f.CloudEnabled || !strings.Contains(f.CloudOrigin, "cloud image") {
		t.Fatal("cloud image name did not suggest cloud-init setup")
	}
	r, err := f.Request("qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	p := r.Input["provisioning"].(map[string]any)
	if p["profile"] != "nocloud-netplan-ipv4-v1" || p["sourceDiskID"] != "disk1" || p["sourceSHA256"] != cloudTestDigest || p["userName"] != "owner" || p["hostname"] != "jammy-server-cloudimg-amd64" || p["passwordlessSudo"] != true || p["mediaID"] != cloudSeedMedia {
		t.Fatal("provisioning declaration", p)
	}
	if keys := p["authorizedKeys"].([]any); len(keys) != 1 || keys[0] != cloudTestKey {
		t.Fatal("authorized keys", keys)
	}
	interfaces := p["interfaces"].([]any)
	if len(interfaces) != 1 || interfaces[0].(map[string]any)["mode"] != "dhcp" || interfaces[0].(map[string]any)["defaultRoute"] != true {
		t.Fatal("connected NAT adapter is not the DHCP default route", interfaces)
	}
	media := r.Input["hardware"].(map[string]any)["media"].([]any)
	if len(media) != 1 || media[0].(map[string]any)["sourceID"] != cloudSeedMedia || media[0].(map[string]any)["bootOrder"] != float64(0) {
		t.Fatal("seed medium not mapped read-only and non-booting", media)
	}
	acks := expectedCreationAcks(f)
	for _, want := range []string{"attach-readonly-media", "guest-root-provisioning", "rotate-guest-host-keys", "guest-passwordless-sudo"} {
		if !slices.Contains(acks, want) {
			t.Fatalf("predicted consequences lack %s: %v", want, acks)
		}
	}
	f.Cloud.Reference = "http://insecure.example/image.img"
	if _, err = f.Request("qemu:///system"); err == nil || !strings.HasPrefix(err.Error(), "Downloaded from:") {
		t.Fatal("non-https source accepted", err)
	}
	f.FocusError(err)
	if f.controls()[f.Focus].id != "cloudSource" {
		t.Fatal("source error not focused on its field")
	}
	f.CloudDecided, f.CloudEnabled = true, false
	f.suggestCloud()
	if f.CloudEnabled {
		t.Fatal("user's No was overridden")
	}
	if r, err = f.Request("qemu:///system"); err != nil || r.Input["provisioning"] != nil {
		t.Fatal("cloud-init setup sent when off", err)
	}
}

// Before preparation the digest is unknown; the chain compares settings without
// it, so approving the import and creating after preparation still match.
func TestCloudDigestIsBoundAfterPreparation(t *testing.T) {
	before := cloudForm(t, true)
	r, err := before.Request("qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	if r.Input["provisioning"].(map[string]any)["sourceSHA256"] != strings.Repeat("0", 64) {
		t.Fatal("unknown digest not left to the chain")
	}
	after := cloudForm(t, false)
	after.Cloud = before.Cloud
	sent, err := after.Request("qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	if chainSettings(r.Input) != chainSettings(sent.Input) || canonicalInput(r.Input) == canonicalInput(sent.Input) {
		t.Fatal("digest normalization is wrong")
	}
	after.Cloud.User = "someone-else"
	if changed, _ := after.Request("qemu:///system"); chainSettings(changed.Input) == chainSettings(r.Input) {
		t.Fatal("a changed user still matched the approval")
	}
	noDigest := cloudForm(t, false)
	noDigest.Source.SourceFiles = nil
	if _, err := noDigest.Request("qemu:///system"); err == nil || !strings.HasPrefix(err.Error(), "Cloud image:") {
		t.Fatal("prepared image without a digest accepted", err)
	}
}

func TestCloudChoiceOnlyForDiskImages(t *testing.T) {
	f := cloudForm(t, false)
	f = creationFocus(t, f, "cloudInit")
	f, _ = creationPress(f, tea.KeyRight)
	if f.CloudEnabled || !f.CloudDecided {
		t.Fatal("cloud choice did not toggle off")
	}
	for _, c := range creationFormFixture().controls() {
		if strings.HasPrefix(c.id, "cloud") {
			t.Fatal("cloud setup offered for a non-disk-image source")
		}
	}
}
