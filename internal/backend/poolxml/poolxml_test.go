package poolxml

import (
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

const fixtureUUID = "44444444-4444-4444-8444-444444444444"

func TestValidateAcceptsOnlyCanonicalFoldersAndNames(t *testing.T) {
	good := domain.StoragePoolDefinition{UUID: fixtureUUID, Name: "default", Path: "/var/lib/libvirt/images"}
	if err := Validate(good); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "-lead", "has space", "slash/name", strings.Repeat("n", 65), "ümlaut"} {
		d := good
		d.Name = name
		if Validate(d) == nil {
			t.Fatalf("name %q accepted", name)
		}
	}
	for _, path := range []string{"", "relative/images", "/", "/srv", "/var/lib/libvirt/images/", "/srv/a/../b", "/srv//vms", "/etc/vms", "/usr/share/vms", "/proc/1", "/dev/vms", "/run/vms", "/srv/bad\nname", "/srv/" + strings.Repeat("x", 1100)} {
		d := good
		d.Path = path
		if Validate(d) == nil {
			t.Fatalf("path %q accepted", path)
		}
	}
	for _, id := range []string{"", "44444444-4444-4444-8444-44444444444A", "00000000-0000-0000-0000-000000000000", "not-a-uuid"} {
		d := good
		d.UUID = id
		if Validate(d) == nil {
			t.Fatalf("uuid %q accepted", id)
		}
	}
	for _, path := range []string{"/srv/vms", "/home/user/VMs", "/var/lib/libvirt/images", "/mnt/data/vm disks"} {
		if err := ValidatePath(path); err != nil {
			t.Fatalf("path %q refused: %v", path, err)
		}
	}
}

func TestRenderMatchesNativeReportedPool(t *testing.T) {
	d := domain.StoragePoolDefinition{UUID: fixtureUUID, Name: "default", Path: "/srv/a&b <vms>"}
	raw, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "/srv/a&amp;b &lt;vms&gt;") || strings.Contains(raw, "permissions") || strings.Contains(raw, "<source") {
		t.Fatal("render must escape the folder and omit permissions and source", raw)
	}
	// Libvirt adds an empty source, capacity and the observed folder permissions.
	reported := strings.Replace(raw, "  <target>", "  <capacity unit='bytes'>1</capacity>\n  <source>\n  </source>\n  <target>", 1)
	reported = strings.Replace(reported, "</path>", "</path>\n    <permissions><mode>0711</mode><label>system_u:object_r:virt_image_t:s0</label></permissions>", 1)
	if err := Match(reported, d); err != nil {
		t.Fatal(err)
	}
	for _, drift := range []string{
		strings.Replace(reported, "type='dir'", "type='fs'", 1),
		strings.Replace(reported, "&lt;vms&gt;", "other", 1),
		strings.Replace(reported, "<source>\n  </source>", "<source><device path='/dev/sdb'/></source>", 1),
		strings.Replace(reported, "<name>default</name>", "<name>other</name>", 1),
		"<pool",
	} {
		if Match(drift, d) == nil {
			t.Fatal("drift accepted", drift)
		}
	}
}

func TestOverlapsFindsEqualAndNestedFolders(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"/var/lib/libvirt/images", "/var/lib/libvirt/images", true},
		{"/var/lib/libvirt", "/var/lib/libvirt/images", true},
		{"/var/lib/libvirt/images/sub", "/var/lib/libvirt/images", true},
		{"/var/lib/libvirt/images2", "/var/lib/libvirt/images", false},
		{"/dev/vg0", "/var/lib/libvirt/images", false},
		{"", "/var/lib/libvirt/images", false},
	} {
		if got := Overlaps(tt.a, tt.b); got != tt.want {
			t.Fatalf("Overlaps(%q, %q) = %v", tt.a, tt.b, got)
		}
	}
}
