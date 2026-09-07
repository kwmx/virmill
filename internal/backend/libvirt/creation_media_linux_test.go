//go:build linux && cgo

package libvirt

import (
	"encoding/xml"
	"fmt"
	native "libvirt.org/go/libvirt"
	"strings"
	"testing"
	"virmill.local/core/internal/domain"
)

func mediaCreationFixture(bus string, boot int) (domain.CreationTarget, []domain.CreatedVolume) {
	target, volumes := creationFixture()
	if boot > 0 {
		for i := range target.Spec.Disks {
			target.Spec.Disks[i].BootOrder++
		}
	}
	target.Spec.Media = []domain.CreationMedia{{SourceID: "installer", Bus: bus, BootOrder: boot}}
	volumes = append(volumes, domain.CreatedVolume{Intent: domain.VolumeIntent{PoolID: target.Spec.PoolID, SourceID: "installer", ContentType: "cdrom-iso", Name: "virmill-" + target.Spec.UUID + "-media-000.iso", FileBytes: 81920, VirtualBytes: 81920, SHA256: strings.Repeat("b", 64)}, BackendKey: "fixture-media", Path: "/fixture/media.iso"})
	return target, volumes
}
func TestReadonlyMediaNativeSimulatedDefinition(t *testing.T) {
	c, err := native.NewConnect("test:///default")
	if err != nil {
		t.Skipf("native simulated driver unavailable: %v", err)
	}
	defer c.Close()
	for _, bus := range []string{"sata", "scsi"} {
		for _, boot := range []int{0, 1} {
			t.Run(fmt.Sprintf("%s-boot%d", bus, boot), func(t *testing.T) {
				target, volumes := mediaCreationFixture(bus, boot)
				target.Spec.Name += fmt.Sprintf(" %s %d", bus, boot)
				target.Spec.UUID = domain.ID()
				if err := validateCreationSpec(target.Spec); err != nil {
					t.Fatal(err)
				}
				for _, v := range volumes {
					if err := validateVolume(v.Intent); err != nil {
						t.Fatal(err)
					}
				}
				wanted, err := creationXML(target, volumes, strings.Repeat("c", 64))
				if err != nil {
					t.Fatal(err)
				}
				d, err := c.DomainDefineXMLFlags(wanted, native.DOMAIN_DEFINE_VALIDATE)
				if err != nil {
					t.Fatalf("native simulated media XML rejected: %v\n%s", err, wanted)
				}
				defer d.Free()
				got, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
				if err != nil {
					t.Fatal(err)
				}
				if err = matchesCreation(wanted, got); err != nil {
					t.Fatalf("media normalization mismatch: %v\n%s", err, got)
				}
				if active, err := d.IsActive(); err != nil || active {
					t.Fatal(active, err)
				}
				for _, changed := range []string{strings.Replace(wanted, "<readonly/>", "", 1), strings.Replace(wanted, `device="cdrom"`, `device="disk"`, 1), strings.Replace(wanted, `type="raw"`, `type="qcow2"`, 1)} {
					if err = matchesCreation(wanted, changed); err == nil {
						t.Fatal("media contract change accepted")
					}
				}
				if _, err = creationXML(target, volumes[:len(volumes)-1], "fixture"); err == nil {
					t.Fatal("missing media accepted")
				}
				t.Log("native libvirt test driver XML validation only; read-only SATA/SCSI media and boot-order mapping observed; no storage stream, firmware, guest or ISO boot behavior exercised")
			})
		}
	}
}
func TestMediaBootAndStorageContractsFailClosed(t *testing.T) {
	for _, change := range []func(*domain.CreationSpec){func(s *domain.CreationSpec) { s.Media[0].BootOrder = 2 }, func(s *domain.CreationSpec) { s.Media[0].BootOrder = 5 }, func(s *domain.CreationSpec) { s.Media[0].Bus = "virtio" }, func(s *domain.CreationSpec) { s.Media[0].SourceID = s.Disks[0].SourceID }, func(s *domain.CreationSpec) {
		for i := 0; i < 4; i++ {
			s.Media = append(s.Media, domain.CreationMedia{SourceID: fmt.Sprint(i), Bus: "sata"})
		}
	}} {
		target, _ := mediaCreationFixture("sata", 1)
		change(&target.Spec)
		if err := validateCreationSpec(target.Spec); err == nil {
			t.Fatal("invalid media mapping accepted", target.Spec)
		}
	}
	_, volumes := mediaCreationFixture("sata", 1)
	in := volumes[len(volumes)-1].Intent
	metadata := `<volume><capacity unit="bytes">81920</capacity><target><format type="raw"/></target><backingStore/></volume>`
	if err := verifyVolumeMetadata(metadata, in); err != nil {
		t.Fatal(err)
	}
	if err := verifyVolumeMetadata(strings.Replace(metadata, `type="raw"`, `type="iso"`, 1), in); err != nil {
		t.Fatal("documented native ISO format refused", err)
	}
	if err := verifyVolumeMetadata(strings.Replace(metadata, `type="raw"`, `type="qcow2"`, 1), in); err == nil {
		t.Fatal("media treated as qcow2")
	}
	for _, change := range []func(*domain.VolumeIntent){func(v *domain.VolumeIntent) { v.ContentType = "" }, func(v *domain.VolumeIntent) { v.ContentType = "unknown" }, func(v *domain.VolumeIntent) { v.VirtualBytes++ }, func(v *domain.VolumeIntent) { v.FileBytes++; v.VirtualBytes++ }, func(v *domain.VolumeIntent) { v.Name = strings.Replace(v.Name, ".iso", ".qcow2", 1) }} {
		v := in
		change(&v)
		if err := validateVolume(v); err == nil {
			t.Fatal("invalid media allocation accepted", v)
		}
	}
}
func TestMediaRequiresAdvertisedCDROMCapability(t *testing.T) {
	target, _ := mediaCreationFixture("sata", 1)
	const encoded = `<domainCapabilities><path>/usr/bin/qemu-system-x86_64</path><domain>kvm</domain><machine>pc-q35-10.2</machine><arch>x86_64</arch><vcpu max="255"/><cpu><mode name="host-passthrough" supported="yes"/></cpu><devices><disk supported="yes"><enum name="diskDevice"><value>disk</value><value>cdrom</value></enum><enum name="bus"><value>sata</value><value>scsi</value><value>virtio</value></enum></disk><interface supported="yes"><enum name="backendType"><value>default</value></enum></interface><graphics supported="yes"><enum name="type"><value>vnc</value></enum></graphics></devices></domainCapabilities>`
	var caps domainCaps
	if err := xml.Unmarshal([]byte(encoded), &caps); err != nil {
		t.Fatal(err)
	}
	if err := checkCaps(caps, target.Spec); err != nil {
		t.Fatal(err)
	}
	caps = domainCaps{}
	if err := xml.Unmarshal([]byte(strings.Replace(encoded, "<value>cdrom</value>", "", 1)), &caps); err != nil {
		t.Fatal(err)
	}
	if err := checkCaps(caps, target.Spec); err == nil {
		t.Fatal("unadvertised CD-ROM accepted")
	}
}
