//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	native "libvirt.org/go/libvirt"
	"os"
	"strings"
	"testing"
	"virmill.local/core/internal/domain"
)

func creationFixture() (domain.CreationTarget, []domain.CreatedVolume) {
	s := domain.CreationSpec{UUID: "b2976600-52bf-4b8e-988e-51ed53008629", Name: "creation fixture & escaped", PoolID: "ddc65530-2834-400a-9f8c-4270a67f1f01", Architecture: "x86_64", Machine: "pc-q35-10.2", VCPUs: 2, MemoryMiB: 512, CPU: domain.CreationCPU{Mode: "host-passthrough"}, Firmware: domain.CreationFirmware{Mode: "bios"}, Clock: "utc", Graphics: "vnc-unix", Disks: []domain.CreationDisk{{SourceID: "boot", Bus: "sata", BootOrder: 1}, {SourceID: "data", Bus: "scsi", BootOrder: 3}, {SourceID: "third", Bus: "virtio", BootOrder: 2}}, NICs: []domain.CreationNIC{{ID: "front", SourceIndex: 0, NetworkID: "e04ebfbf-b906-45e2-a4aa-81ac4e6c760c", Model: "virtio", Link: "up", MAC: "02:00:00:00:00:01"}, {ID: "back", SourceIndex: 1, NetworkID: "e04ebfbf-b906-45e2-a4aa-81ac4e6c760c", Model: "e1000e", Link: "down", MAC: "02:00:00:00:00:02"}}}
	target := domain.CreationTarget{Spec: s, PoolName: "fixture", Emulator: "/usr/bin/qemu-system-x86_64", Networks: []domain.VirtualNetwork{{Key: domain.ResourceKey{UUID: s.NICs[0].NetworkID}, Name: "fixture-net"}}}
	volumes := []domain.CreatedVolume{}
	for i, d := range s.Disks {
		volumes = append(volumes, domain.CreatedVolume{Intent: domain.VolumeIntent{PoolID: s.PoolID, SourceID: d.SourceID, Name: "virmill-" + s.UUID + "-disk-00" + string(rune('0'+i)) + ".qcow2", FileBytes: 65536, VirtualBytes: 1 << 20, SHA256: strings.Repeat("a", 64)}, BackendKey: "fixture-key-" + d.SourceID, Path: "/fixture/" + d.SourceID})
	}
	return target, volumes
}

func TestCreationXMLNativeSimulatedRoundTrip(t *testing.T) {
	c, err := native.NewConnect("test:///default")
	if err != nil {
		t.Skipf("native simulated driver unavailable: %v", err)
	}
	defer c.Close()
	for _, firmware := range []string{"bios", "uefi"} {
		t.Run(firmware, func(t *testing.T) {
			target, volumes := creationFixture()
			if firmware == "uefi" {
				target.Spec.UUID = "b2976600-52bf-4b8e-988e-51ed53008630"
				target.Spec.Name += " UEFI"
				target.Spec.Firmware = domain.CreationFirmware{Mode: "uefi", Code: "/fixture/CODE.fd", Template: "/fixture/VARS.fd", Format: "raw", SecureBoot: true, TPM: true}
			}
			if err = validateCreationSpec(target.Spec); err != nil {
				t.Fatal(err)
			}
			wanted, e := creationXML(target, volumes, strings.Repeat("a", 64))
			if e != nil {
				t.Fatal(e)
			}
			d, e := c.DomainDefineXMLFlags(wanted, native.DOMAIN_DEFINE_VALIDATE)
			if e != nil {
				t.Fatalf("simulated native XML definition rejected: %v\n%s", e, wanted)
			}
			defer d.Free()
			got, e := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
			if e != nil {
				t.Fatal(e)
			}
			if e = matchesCreation(wanted, got); e != nil {
				t.Fatalf("native XML normalization mismatch: %v\n%s", e, got)
			}
			active, e := d.IsActive()
			if e != nil || active {
				t.Fatalf("definition unexpectedly active: %v %v", active, e)
			}
			t.Log("native libvirt test driver XML parser/validator only; no QEMU VM, disk upload, firmware, TPM, network, or guest behavior exercised")
		})
	}
}

func TestCreationXMLRejectsMappingChanges(t *testing.T) {
	target, volumes := creationFixture()
	wanted, err := creationXML(target, volumes, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string][2]string{
		"memory":                      {">524288<", ">525312<"},
		"NIC link":                    {`state="down"`, `state="up"`},
		"boot order":                  {`order="3"`, `order="1"`},
		"disk bus":                    {`bus="scsi"`, `bus="sata"`},
		"identity binding":            {strings.Repeat("a", 64), strings.Repeat("b", 64)},
		"extra disk":                  {"</devices>", `<disk type="file"><source file="/unreviewed"/></disk></devices>`},
		"extra host device":           {"</devices>", `<hostdev mode="subsystem" type="usb"/></devices>`},
		"CPU feature":                 {"</cpu>", `<feature policy="require" name="unreviewed"/></cpu>`},
		"external backing":            {`<target dev="sda"`, `<backingStore type="file"><source file="/unreviewed"/></backingStore><target dev="sda"`},
		"unreviewed source attribute": {`pool="fixture"`, `file="/unreviewed" pool="fixture"`},
		"extra metadata":              {"</metadata>", `<other xmlns="urn:other"/></metadata>`},
	} {
		t.Run(name, func(t *testing.T) {
			if err := matchesCreation(wanted, strings.Replace(wanted, change[0], change[1], 1)); err == nil {
				t.Fatal("changed intent accepted")
			}
		})
	}
	volumes[1].Intent.SourceID = "wrong"
	if _, err = creationXML(target, volumes, "binding"); err == nil {
		t.Fatal("misassociated disk accepted")
	}
	if diskSuffix(25) != "z" || diskSuffix(26) != "aa" || diskSuffix(63) != "bl" {
		t.Fatal("disk names are not unique beyond 26")
	}
}

func TestCreationCapabilitiesRequirePositiveSupport(t *testing.T) {
	target, _ := creationFixture()
	const caps = `<domainCapabilities><path>/usr/bin/qemu-system-x86_64</path><domain>kvm</domain><machine>pc-q35-10.2</machine><arch>x86_64</arch><vcpu max="255"/><cpu><mode name="host-passthrough" supported="yes"/><mode name="custom" supported="yes"><model usable="yes">fixture</model><model usable="unknown">uncertain</model></mode></cpu><devices><disk supported="yes"><enum name="bus"><value>sata</value><value>scsi</value><value>virtio</value></enum></disk><interface supported="yes"><enum name="backendType"><value>default</value></enum></interface><graphics supported="yes"><enum name="type"><value>vnc</value></enum></graphics></devices></domainCapabilities>`
	var c domainCaps
	if err := xml.Unmarshal([]byte(caps), &c); err != nil {
		t.Fatal(err)
	}
	if err := checkCaps(c, target.Spec); err != nil {
		t.Fatal(err)
	}
	target.Spec.CPU = domain.CreationCPU{Mode: "custom", Model: "uncertain"}
	if err := checkCaps(c, target.Spec); err == nil {
		t.Fatal("unconfirmed CPU accepted")
	}
	target.Spec.CPU.Model = "fixture"
	if err := checkCaps(c, target.Spec); err != nil {
		t.Fatal(err)
	}
	target.Spec.Firmware = domain.CreationFirmware{Mode: "uefi", Code: "/fixture/CODE.fd", Template: "/fixture/VARS.fd", Format: "raw", SecureBoot: true, TPM: true}
	if err := checkCaps(c, target.Spec); err == nil {
		t.Fatal("unadvertised firmware/TPM accepted")
	}
}

func TestFirmwareDescriptorRequiresExactMappingAndKeyPolicy(t *testing.T) {
	target, _ := creationFixture()
	target.Spec.Firmware = domain.CreationFirmware{Mode: "uefi", Code: "/fixture/CODE.fd", Template: "/fixture/VARS.fd", Format: "raw", SecureBoot: true, TPM: true}
	const encoded = `{"mapping":{"device":"flash","mode":"split","executable":{"filename":"/fixture/CODE.fd","format":"raw"},"nvram-template":{"filename":"/fixture/VARS.fd","format":"raw"}},"targets":[{"architecture":"x86_64","machines":["pc-q35-*"]}],"features":["secure-boot","enrolled-keys","requires-smm"]}`
	var d firmwareDescriptor
	if err := json.Unmarshal([]byte(encoded), &d); err != nil {
		t.Fatal(err)
	}
	if !descriptorMatches(d, target.Spec) {
		t.Fatal("exact supported mapping refused")
	}
	d.Features = []string{"secure-boot", "requires-smm"}
	if descriptorMatches(d, target.Spec) {
		t.Fatal("Secure Boot accepted without enrolled keys")
	}
	d.Features = []string{"secure-boot", "enrolled-keys", "requires-smm"}
	target.Spec.Firmware.SecureBoot = false
	if descriptorMatches(d, target.Spec) {
		t.Fatal("enrolled keys silently substituted for nonsecure firmware")
	}
	d.Features = nil
	if !descriptorMatches(d, target.Spec) {
		t.Fatal("nonsecure mapping refused")
	}
	target.Spec.Firmware.Template = "/fixture/different.fd"
	if descriptorMatches(d, target.Spec) {
		t.Fatal("template replacement accepted")
	}
}

func TestCreationDeviceMetadataFromInstalledQEMU(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("enable installed QEMU read-only metadata probes explicitly")
	}
	target, _ := creationFixture()
	target.Spec.NICs = append(target.Spec.NICs, domain.CreationNIC{Model: "rtl8139"})
	target.Spec.Media = []domain.CreationMedia{{Bus: "sata"}, {Bus: "scsi"}}
	proof, err := probeCreationDevices(context.Background(), "/usr/bin/qemu-system-x86_64", target.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if !digestPattern.MatchString(proof) {
		t.Fatal("missing emulator/device proof")
	}
	t.Logf("actual installed QEMU -device MODEL,help only, proof %s; no machine initialization or guest execution", proof)
	for _, secure := range []bool{false, true} {
		suffix := ""
		if secure {
			suffix = ".secboot"
		}
		target.Spec.Firmware = domain.CreationFirmware{Mode: "uefi", Code: "/usr/share/edk2/ovmf/OVMF_CODE_4M" + suffix + ".qcow2", Template: "/usr/share/edk2/ovmf/OVMF_VARS_4M" + suffix + ".qcow2", Format: "qcow2", SecureBoot: secure}
		descriptor, err := matchingFirmwareDescriptor(target.Spec)
		if err != nil {
			t.Fatal(err)
		}
		code, err := firmwareFileDigest(target.Spec.Firmware.Code)
		if err != nil {
			t.Fatal(err)
		}
		template, err := firmwareFileDigest(target.Spec.Firmware.Template)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("read-only installed firmware metadata: secureBoot=%t descriptor=%s code=%s template=%s; enrollment, firmware execution and TPM recovery remain untested", secure, descriptor, code, template)
	}
}

func TestUploadedVolumeMetadataRequiresIndependentExactCapacity(t *testing.T) {
	_, volumes := creationFixture()
	in := volumes[0].Intent
	base := `<volume><capacity unit="bytes">1048576</capacity><target><format type="qcow2"/></target><backingStore/></volume>`
	if err := verifyVolumeMetadata(base, in); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{
		strings.Replace(base, "1048576", "1048577", 1),
		strings.Replace(base, `unit="bytes"`, `unit="KiB"`, 1),
		strings.Replace(base, "qcow2", "raw", 1),
		strings.Replace(base, "<backingStore/>", `<backingStore><path>/unapproved</path></backingStore>`, 1),
	} {
		if err := verifyVolumeMetadata(changed, in); err == nil {
			t.Fatal("unverified native volume metadata accepted", changed)
		}
	}
}

func TestVolumeDownloadConnectionGuardsWithSimulatedStorage(t *testing.T) {
	c, err := native.NewConnect("test:///default")
	if err != nil {
		t.Skipf("native simulated driver unavailable: %v", err)
	}
	defer c.Close()
	ro, err := native.NewConnectReadOnly("test:///default")
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	pools, err := c.ListAllStoragePools(0)
	if err != nil || len(pools) == 0 {
		t.Fatal("simulated pool missing", err)
	}
	defer func() {
		for i := range pools {
			pools[i].Free()
		}
	}()
	v, err := pools[0].StorageVolCreateXML(`<volume><name>virmill-simulated-download-check</name><capacity unit="bytes">1048576</capacity><target><format type="raw"/></target></volume>`, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Free()
	stream, err := ro.NewStream(0)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Free()
	err = v.Download(stream, 0, 0, 0)
	var denied native.Error
	if !errors.As(err, &denied) || denied.Code != native.ERR_INVALID_ARG {
		t.Fatalf("native connection identity guard differs: %v", err)
	}
	poolName, err := pools[0].GetName()
	if err != nil {
		t.Fatal(err)
	}
	readPool, err := ro.LookupStoragePoolByName(poolName)
	if err != nil {
		t.Fatal(err)
	}
	defer readPool.Free()
	readVolume, err := readPool.LookupStorageVolByName("virmill-simulated-download-check")
	if err != nil {
		t.Fatal(err)
	}
	defer readVolume.Free()
	err = readVolume.Download(stream, 0, 0, 0)
	if !errors.As(err, &denied) || denied.Code != native.ERR_OPERATION_DENIED {
		t.Fatalf("native read-only volume guard differs: %v", err)
	}
	t.Log("native API rejects mismatched connections and read-only volume access before transfer; simulated volume only, no file or host storage created; successful native storage streaming remains untested")
}
