package xmlpatch

import (
	"strings"
	"testing"
)

const cloneSource = `<domain type='kvm'>
  <name>noble</name>
  <uuid>b7a924e8-8d0c-41ee-9b40-ec419d657254</uuid>
  <metadata>
    <virmill:creation xmlns:virmill="urn:virmill:v1" apiVersion="virmill/v1" binding="abc"/>
    <other:note xmlns:other="urn:example">kept</other:note>
  </metadata>
  <os firmware='efi'>
    <type arch='x86_64' machine='pc-q35-10.2'>hvm</type>
    <loader readonly='yes' type='pflash' format='raw'>/usr/share/edk2/ovmf/OVMF_CODE.fd</loader>
    <nvram template='/usr/share/edk2/ovmf/OVMF_VARS.fd' templateFormat='raw' format='raw'>/var/lib/libvirt/qemu/nvram/noble_VARS.fd</nvram>
  </os>
  <devices>
    <disk type='volume' device='disk'>
      <driver name='qemu' type='qcow2'/>
      <source pool='images' volume='noble-disk-000.qcow2'/>
      <target dev='sda' bus='sata'/>
      <boot order='1'/>
    </disk>
    <disk type='volume' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source pool='images' volume='noble-media-000.iso'/>
      <target dev='sdb' bus='sata'/>
      <readonly/>
    </disk>
    <disk type='volume' device='disk'>
      <driver name='qemu' type='raw'/>
      <source pool='data' volume='noble-disk-001.raw'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <interface type='network'>
      <mac address='52:54:00:11:22:33'/>
      <source network='default'/>
      <model type='virtio'/>
    </interface>
    <graphics type='spice'>
      <listen type='socket'/>
    </graphics>
  </devices>
</domain>`

const cloneUUID = "d1c2b3a4-1111-4222-8333-444455556666"

func clonePatch() ClonePatch {
	return ClonePatch{UUID: cloneUUID, Name: "noble-clone", FreshNVRAM: true, Disks: []CloneDisk{
		{Target: "sda", Pool: "images", Volume: "virmill-" + cloneUUID + "-disk-000.qcow2"},
		{Target: "vda", Pool: "archive", Volume: "virmill-" + cloneUUID + "-disk-001.qcow2"},
	}}
}

// ADR 0066: the clone differs from the original only in identity, the sources
// of its writable disks, MAC addresses, creation metadata and firmware
// variables path; shared media and everything else stay byte for byte.
func TestCloneDefinitionChangesOnlyIdentityStorageAndAssignedState(t *testing.T) {
	out, err := CloneDefinition(cloneSource, clonePatch())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<name>noble-clone</name>", "<uuid>" + cloneUUID + "</uuid>",
		`<source pool="images" volume="virmill-` + cloneUUID + `-disk-000.qcow2"/>`,
		`<source pool="archive" volume="virmill-` + cloneUUID + `-disk-001.qcow2"/>`,
		"<source pool='images' volume='noble-media-000.iso'/>", // shared read-only media
		`<other:note xmlns:other="urn:example">kept</other:note>`,
		`<nvram template="/usr/share/edk2/ovmf/OVMF_VARS.fd" templateFormat="raw" format="raw"/>`,
		"<boot order='1'/>", "<source network='default'/>", "<listen type='socket'/>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("clone lacks %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"52:54:00:11:22:33", "virmill:creation", "noble_VARS.fd", "noble-disk-000.qcow2", "noble-disk-001.raw", "b7a924e8"} {
		if strings.Contains(out, gone) {
			t.Fatalf("clone still has %q:\n%s", gone, out)
		}
	}
	// What libvirt assigns when it stores the clone does not count as a change.
	stored := strings.Replace(out, "<model type='virtio'/>", "<mac address='52:54:00:aa:bb:cc'/>\n      <model type='virtio'/>", 1)
	stored = strings.Replace(stored, `format="raw"/>`, `format="raw">/var/lib/libvirt/qemu/nvram/noble-clone_VARS.fd</nvram>`, 1)
	a, err := CloneComparable(out)
	if err != nil {
		t.Fatal(err)
	}
	b, err := CloneComparable(stored)
	if err != nil {
		t.Fatal(err)
	}
	da, _ := HardwareDigest(a)
	db, _ := HardwareDigest(b)
	if da != db {
		t.Fatalf("libvirt's assigned MAC and NVRAM path are treated as changes:\n%s\n%s", a, b)
	}
	if macs, _ := MACAddresses(stored); len(macs) != 1 || macs[0] != "52:54:00:aa:bb:cc" {
		t.Fatal(macs)
	}
}

func TestCloneDefinitionRefusesWhatItCannotCopySafely(t *testing.T) {
	missing := clonePatch()
	missing.Disks = missing.Disks[:1]
	sameName := clonePatch()
	sameName.Name = "noble"
	media := clonePatch()
	media.Disks = append(media.Disks, CloneDisk{Target: "sdb", Pool: "images", Volume: "x.iso"})
	noNVRAM := clonePatch()
	noNVRAM.FreshNVRAM = false
	cases := map[string]struct {
		raw   string
		patch ClonePatch
		want  string
	}{
		"uncopied writable disk": {cloneSource, missing, "every writable disk needs a copy"},
		"same name":              {cloneSource, sameName, "different from the original"},
		"media copied":           {cloneSource, media, "read-only media"},
		"stale firmware":         {cloneSource, noNVRAM, "firmware variables differ"},
		"tpm":                    {strings.Replace(cloneSource, "<graphics", "<tpm model='tpm-crb'><backend type='emulator' version='2.0'/></tpm><graphics", 1), clonePatch(), "TPM"},
		"no template":            {strings.Replace(cloneSource, "template='/usr/share/edk2/ovmf/OVMF_VARS.fd' templateFormat='raw' ", "", 1), clonePatch(), "no template"},
		"direct adapter":         {strings.Replace(cloneSource, "<interface type='network'>", "<interface type='direct'>", 1), clonePatch(), "network and bridge"},
		"file disk":              {strings.Replace(cloneSource, "<disk type='volume' device='disk'>\n      <driver name='qemu' type='raw'/>\n      <source pool='data' volume='noble-disk-001.raw'/>", "<disk type='file' device='disk'>\n      <driver name='qemu' type='raw'/>\n      <source file='/srv/noble.raw'/>", 1), clonePatch(), "storage pool"},
	}
	for label, c := range cases {
		if _, err := CloneDefinition(c.raw, c.patch); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v", label, err)
		}
	}
}

// clone-system-native-001: a clone whose only metadata was Virmill's creation
// record left an empty metadata element, which libvirt does not store, so the
// read-back refused a correct clone. The element goes with its last child.
func TestCloneDefinitionDropsMetadataLeftEmpty(t *testing.T) {
	raw := strings.Replace(cloneSource, "\n    <other:note xmlns:other=\"urn:example\">kept</other:note>", "", 1)
	out, err := CloneDefinition(raw, clonePatch())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "metadata") {
		t.Fatalf("empty metadata left behind:\n%s", out)
	}
	// What libvirt stores: the same document without the element at all.
	stored := strings.Replace(raw, "  <metadata>\n    <virmill:creation xmlns:virmill=\"urn:virmill:v1\" apiVersion=\"virmill/v1\" binding=\"abc\"/>\n  </metadata>\n", "", 1)
	stored, err = CloneDefinition(stored, clonePatch())
	if err != nil {
		t.Fatal(err)
	}
	a, _ := CloneComparable(out)
	b, _ := CloneComparable(stored)
	da, _ := HardwareDigest(a)
	db, _ := HardwareDigest(b)
	if da != db {
		t.Fatalf("clone differs from what libvirt stores:\n%s\n---\n%s", out, stored)
	}
}
