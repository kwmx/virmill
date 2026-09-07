package protection

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/domain"
)

func captureFixture() CaptureManifest {
	vm := "11111111-2222-1333-8444-555555555555" // Native identity need not be UUIDv4.
	m := CaptureManifest{
		APIVersion: domain.APIVersion, Kind: "ColdRecoveryPoint", Version: 1,
		ID: "aaaaaaaa-bbbb-5ccc-8ddd-eeeeeeeeeeee", OperationID: "aaaaaaaa-bbbb-4ccc-8ddd-ffffffffffff",
		SourceVM:  domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: vm},
		StartedAt: time.Date(2026, 9, 7, 1, 0, 0, 0, time.UTC), FinishedAt: time.Date(2026, 9, 7, 1, 0, 1, 0, time.UTC),
		SourceFingerprint: strings.Repeat("a", 64), StateBefore: "stopped", StateAfter: "stopped",
		NativeVersions: map[string]string{"libvirt": "synthetic-12.0", "qemu": "synthetic-10.2"},
		Source:         domain.ColdSourceLayout{State: domain.ColdStateLayout{VMID: vm}, Architecture: "x86_64", Machine: "pc-q35-10.2", Disks: []domain.ColdDiskSource{{Target: "vda", Device: "disk", Bus: "virtio", Source: domain.ColdStorageSource{Type: "file", Format: "qcow2", File: "/fixture/source.qcow2"}}}},
		Members:        []CaptureMember{{ID: "disk", Kind: "disk", Path: "disks/vda.raw", Size: 1024, SHA256: strings.Repeat("b", 64)}, {ID: "xml", Kind: "persistent-xml", Path: "xml/persistent.xml", Size: 128, SHA256: strings.Repeat("c", 64)}},
		Disks:          []CapturedDisk{{Target: "vda", MemberID: "disk", Format: "raw", Independent: true}}, PersistentXMLMember: "xml", EffectiveXMLMember: "xml", IndependentlyRecoverable: true,
	}
	m.Source.State.SecretReferences = []string{}
	m.Source.External = []domain.ColdDependency{}
	m.Source.Disks[0].Backing = []domain.ColdStorageSource{}
	m.TPMMembers, m.Secrets = []CapturedTPMFile{}, []CapturedSecret{}
	return m
}

func fullCaptureFixture() CaptureManifest {
	m := captureFixture()
	m.Source.State.Firmware = domain.ColdFirmware{Loader: "/fixture/CODE.fd", LoaderType: "pflash", LoaderFormat: "raw", LoaderSecure: "no", LoaderReadOnly: "yes", NVRAM: &domain.ColdNVRAM{Path: "/fixture/VARS.fd", Format: "raw", Template: "/fixture/template.fd", TemplateFormat: "raw"}}
	secret := "99999999-aaaa-1bbb-8ccc-dddddddddddd"
	m.Source.State.TPM = &domain.ColdTPM{Model: "tpm-crb", Version: "2.0", SourceType: "dir", SourcePath: "/fixture/tpm", PersistentState: "yes", Profile: "custom:restricted", EncryptionSecret: secret}
	m.Source.State.SecretReferences = []string{secret}
	m.NativeVersions["swtpm"] = "synthetic-0.10"
	for _, kind := range []string{"firmware-code", "nvram", "tpm", "secret", "auxiliary-inventory", "effective-xml"} {
		m.Members = append(m.Members, CaptureMember{ID: kind, Kind: kind, Path: "state/" + kind, Size: 16, SHA256: strings.Repeat("d", 64)})
	}
	m.FirmwareCodeMember, m.NVRAMMember, m.AuxiliaryInventoryMember, m.EffectiveXMLMember = "firmware-code", "nvram", "auxiliary-inventory", "effective-xml"
	m.TPMMembers = []CapturedTPMFile{{Name: "state/permanent", MemberID: "tpm"}}
	m.Secrets = []CapturedSecret{{UUID: secret, MemberID: "secret"}}
	return m
}

func TestCaptureManifestValidDeclarations(t *testing.T) {
	for name, edit := range map[string]func(*CaptureManifest){
		"basic":                   func(*CaptureManifest) {},
		"session and equal times": func(m *CaptureManifest) { m.SourceVM.ConnectionID = "qemu:///session"; m.FinishedAt = m.StartedAt },
		"zero UTC offset":         func(m *CaptureManifest) { m.StartedAt = m.StartedAt.In(time.FixedZone("zero", 0)) },
		"empty removable": func(m *CaptureManifest) {
			m.Source.Disks = append(m.Source.Disks, domain.ColdDiskSource{Target: "sda", Device: "cdrom", ReadOnly: true, Empty: true, Bus: "sata", Source: domain.ColdStorageSource{Type: "file", Format: "raw"}, Backing: []domain.ColdStorageSource{}})
		},
		"no disks": func(m *CaptureManifest) {
			m.Source.Disks = []domain.ColdDiskSource{}
			m.Disks = []CapturedDisk{}
			m.Members = m.Members[1:]
		},
		"volume source": func(m *CaptureManifest) {
			m.Source.Disks[0].Source = domain.ColdStorageSource{Type: "volume", Format: "raw", Pool: "pool", Volume: "disk.raw"}
		},
		"declared flattened backing": func(m *CaptureManifest) {
			m.Source.Disks[0].Backing = []domain.ColdStorageSource{{Type: "file", File: "/fixture/base.raw", Format: "raw"}}
		},
		"media": func(m *CaptureManifest) {
			m.Source.Disks[0].Device = "cdrom"
			m.Source.Disks[0].ReadOnly = true
			m.Members[0].Kind = "media"
		},
		"full auxiliary": func(m *CaptureManifest) { *m = fullCaptureFixture() },
		"unresolved native auxiliary paths": func(m *CaptureManifest) {
			*m = fullCaptureFixture()
			m.Source.State.Firmware.NVRAM.Path = ""
			m.Source.State.TPM.SourceType, m.Source.State.TPM.SourcePath = "", ""
		},
		"zero TPM bytes": func(m *CaptureManifest) { *m = fullCaptureFixture(); m.Members[4].Size = 0 },
		"external secret": func(m *CaptureManifest) {
			*m = fullCaptureFixture()
			m.Secrets[0].MemberID = ""
			m.Secrets[0].External = true
			m.IndependentlyRecoverable = false
			m.Members = append(m.Members[:5], m.Members[6:]...)
		},
		"external network source": func(m *CaptureManifest) {
			m.Source.Disks[0].Source = domain.ColdStorageSource{Type: "network", Format: "qcow2"}
			m.Source.External = []domain.ColdDependency{{Kind: "storage-network", Target: "vda"}}
			m.IndependentlyRecoverable = false
		},
		"external backing source": func(m *CaptureManifest) {
			m.Source.Disks[0].Backing = []domain.ColdStorageSource{{Type: "block", Format: "raw"}}
			m.Source.External = []domain.ColdDependency{{Kind: "storage-block", Target: "vda/backing[1]"}}
			m.IndependentlyRecoverable = false
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := captureFixture()
			edit(&m)
			if err := m.Validate(); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			got, err := DecodeCaptureManifest(encoded)
			if err != nil || got.Validate() != nil {
				t.Fatalf("round trip: %v", err)
			}
		})
	}
}

func TestCaptureManifestRejectsContradictions(t *testing.T) {
	cases := map[string]func(*CaptureManifest){
		"API":                 func(m *CaptureManifest) { m.APIVersion = "virmill/v2" },
		"kind":                func(m *CaptureManifest) { m.Kind = "Backup" },
		"version":             func(m *CaptureManifest) { m.Version = 2 },
		"snapshot uppercase":  func(m *CaptureManifest) { m.ID = strings.ToUpper(m.ID) },
		"operation malformed": func(m *CaptureManifest) { m.OperationID = strings.ReplaceAll(m.OperationID, "-", "") },
		"zero identity": func(m *CaptureManifest) {
			m.SourceVM.UUID = "00000000-0000-0000-0000-000000000000"
			m.Source.State.VMID = m.SourceVM.UUID
		},
		"source VM mismatch":      func(m *CaptureManifest) { m.Source.State.VMID = m.ID },
		"provider":                func(m *CaptureManifest) { m.SourceVM.ProviderID = "other" },
		"source kind":             func(m *CaptureManifest) { m.SourceVM.Kind = "disk" },
		"remote URI":              func(m *CaptureManifest) { m.SourceVM.ConnectionID = "qemu+ssh://example/system" },
		"URI query":               func(m *CaptureManifest) { m.SourceVM.ConnectionID += "?socket=other" },
		"zero started":            func(m *CaptureManifest) { m.StartedAt = time.Time{} },
		"zero finished":           func(m *CaptureManifest) { m.FinishedAt = time.Time{} },
		"reverse times":           func(m *CaptureManifest) { m.FinishedAt = m.StartedAt.Add(-time.Second) },
		"nonUTC":                  func(m *CaptureManifest) { m.FinishedAt = m.FinishedAt.In(time.FixedZone("offset", 3600)) },
		"out of JSON time range":  func(m *CaptureManifest) { m.FinishedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) },
		"running before":          func(m *CaptureManifest) { m.StateBefore = "running" },
		"running after":           func(m *CaptureManifest) { m.StateAfter = "running" },
		"managed save":            func(m *CaptureManifest) { m.HasManagedSave = true },
		"fingerprint":             func(m *CaptureManifest) { m.SourceFingerprint = strings.Repeat("A", 64) },
		"libvirt absent":          func(m *CaptureManifest) { delete(m.NativeVersions, "libvirt") },
		"qemu absent":             func(m *CaptureManifest) { delete(m.NativeVersions, "qemu") },
		"TPM version absent":      func(m *CaptureManifest) { delete(m.NativeVersions, "swtpm") },
		"version control":         func(m *CaptureManifest) { m.NativeVersions["qemu"] = "10\x1b[0m" },
		"version key":             func(m *CaptureManifest) { m.NativeVersions["qemu/other"] = "10" },
		"architecture":            func(m *CaptureManifest) { m.Source.Architecture = "" },
		"machine":                 func(m *CaptureManifest) { m.Source.Machine = "q35\n" },
		"no members":              func(m *CaptureManifest) { m.Members = nil },
		"duplicate member":        func(m *CaptureManifest) { m.Members = append(m.Members, m.Members[0]) },
		"member ID control":       func(m *CaptureManifest) { m.Members[0].ID = "disk\u202e" },
		"member ID oversized":     func(m *CaptureManifest) { m.Members[0].ID = strings.Repeat("a", 257) },
		"member unknown kind":     func(m *CaptureManifest) { m.Members[0].Kind = "memory" },
		"member digest uppercase": func(m *CaptureManifest) { m.Members[0].SHA256 = strings.Repeat("A", 64) },
		"member digest bad hex":   func(m *CaptureManifest) { m.Members[0].SHA256 = strings.Repeat("g", 64) },
		"member digest short":     func(m *CaptureManifest) { m.Members[0].SHA256 = "ab" },
		"zero disk":               func(m *CaptureManifest) { m.Members[0].Size = 0 },
		"negative TPM":            func(m *CaptureManifest) { m.Members[4].Size = -1 },
		"oversized member":        func(m *CaptureManifest) { m.Members[0].Size = manifestMemberSizeLimit + 1 },
		"duplicate paths case":    func(m *CaptureManifest) { m.Members[1].Path = strings.ToUpper(m.Members[0].Path) },
		"path prefix":             func(m *CaptureManifest) { m.Members[1].Path = "disks" },
		"path child":              func(m *CaptureManifest) { m.Members[1].Path = m.Members[0].Path + "/child" },
		"missing persistent":      func(m *CaptureManifest) { m.PersistentXMLMember = "" },
		"missing effective":       func(m *CaptureManifest) { m.EffectiveXMLMember = "" },
		"wrong effective role":    func(m *CaptureManifest) { m.EffectiveXMLMember = "disk" },
		"unused member": func(m *CaptureManifest) {
			m.Members = append(m.Members, CaptureMember{ID: "extra", Kind: "disk", Path: "extra", Size: 1, SHA256: strings.Repeat("a", 64)})
		},
		"source target duplicate": func(m *CaptureManifest) { m.Source.Disks = append(m.Source.Disks, m.Source.Disks[0]) },
		"source target alias duplicate": func(m *CaptureManifest) {
			d := m.Source.Disks[0]
			d.Target = "ioemu:" + d.Target
			m.Source.Disks = append(m.Source.Disks, d)
		},
		"source target invalid":   func(m *CaptureManifest) { m.Source.Disks[0].Target = "../../vda" },
		"disk omitted":            func(m *CaptureManifest) { m.Disks = nil },
		"disk mapped twice":       func(m *CaptureManifest) { m.Disks = append(m.Disks, m.Disks[0]) },
		"disk target extra":       func(m *CaptureManifest) { m.Disks[0].Target = "vdb" },
		"disk missing member":     func(m *CaptureManifest) { m.Disks[0].MemberID = "absent" },
		"disk nonindependent":     func(m *CaptureManifest) { m.Disks[0].Independent = false },
		"disk wrong format":       func(m *CaptureManifest) { m.Disks[0].Format = "vmdk" },
		"media wrong member kind": func(m *CaptureManifest) { m.Source.Disks[0].Device = "floppy" },
		"empty disk":              func(m *CaptureManifest) { m.Source.Disks[0].Empty = true },
		"empty removable mapped": func(m *CaptureManifest) {
			m.Source.Disks[0].Empty = true
			m.Source.Disks[0].Device = "floppy"
			m.Source.Disks[0].Source.File = ""
		},
		"empty removable identity": func(m *CaptureManifest) {
			m.Source.Disks[0].Empty = true
			m.Source.Disks[0].Device = "floppy"
			m.Disks = nil
		},
		"writable CDROM":    func(m *CaptureManifest) { m.Source.Disks[0].Device = "cdrom" },
		"ambiguous source":  func(m *CaptureManifest) { m.Source.Disks[0].Source.Pool = "pool" },
		"empty file source": func(m *CaptureManifest) { m.Source.Disks[0].Source.File = "" },
		"empty volume identity": func(m *CaptureManifest) {
			m.Source.Disks[0].Source = domain.ColdStorageSource{Type: "volume", Pool: "pool"}
		},
		"volume traversal": func(m *CaptureManifest) {
			m.Source.Disks[0].Source = domain.ColdStorageSource{Type: "volume", Pool: "pool", Volume: "../volume"}
		},
		"source absolute path":    func(m *CaptureManifest) { m.Source.Disks[0].Source.File = "relative" },
		"source hidden traversal": func(m *CaptureManifest) { m.Source.Disks[0].Source.File = "/fixture/a/../source" },
		"source format control":   func(m *CaptureManifest) { m.Source.Disks[0].Source.Format = "raw\n" },
		"repeated backing identity": func(m *CaptureManifest) {
			b := m.Source.Disks[0].Source
			b.Format = "raw"
			m.Source.Disks[0].Backing = []domain.ColdStorageSource{b}
		},
		"opaque source no dependency": func(m *CaptureManifest) {
			m.Source.Disks[0].Source = domain.ColdStorageSource{Type: "network"}
			m.IndependentlyRecoverable = false
		},
		"opaque source wrong location": func(m *CaptureManifest) {
			m.Source.Disks[0].Source = domain.ColdStorageSource{Type: "network"}
			m.Source.External = []domain.ColdDependency{{Kind: "storage-network", Target: "vdb"}}
			m.IndependentlyRecoverable = false
		},
		"external independent": func(m *CaptureManifest) {
			m.Source.External = []domain.ColdDependency{{Kind: "hostdev", Target: "devices/device[1]"}}
		},
		"duplicate external": func(m *CaptureManifest) {
			m.IndependentlyRecoverable = false
			d := domain.ColdDependency{Kind: "hostdev", Target: "devices/device[1]"}
			m.Source.External = []domain.ColdDependency{d, d}
		},
		"external terminal injection": func(m *CaptureManifest) {
			m.IndependentlyRecoverable = false
			m.Source.External = []domain.ColdDependency{{Kind: "hostdev", Target: "devices\x1b[1m"}}
		},
		"missing code":                     func(m *CaptureManifest) { m.FirmwareCodeMember = "" },
		"unclaimed code":                   func(m *CaptureManifest) { m.Source.State.Firmware = domain.ColdFirmware{} },
		"loader missing identity":          func(m *CaptureManifest) { m.Source.State.Firmware.Loader = "" },
		"loader unsafe path":               func(m *CaptureManifest) { m.Source.State.Firmware.Loader = "/firmware/../CODE" },
		"loader unknown format":            func(m *CaptureManifest) { m.Source.State.Firmware.LoaderFormat = "iso" },
		"missing NVRAM":                    func(m *CaptureManifest) { m.NVRAMMember = "" },
		"stateless NVRAM":                  func(m *CaptureManifest) { m.Source.State.Firmware.LoaderStateless = "yes" },
		"NVRAM unsafe path":                func(m *CaptureManifest) { m.Source.State.Firmware.NVRAM.Path = "/fixture/VARS\n" },
		"template format without template": func(m *CaptureManifest) { m.Source.State.Firmware.NVRAM.Template = "" },
		"missing auxiliary inventory":      func(m *CaptureManifest) { m.AuxiliaryInventoryMember = "" },
		"missing TPM members":              func(m *CaptureManifest) { m.TPMMembers = nil },
		"unclaimed TPM members":            func(m *CaptureManifest) { m.Source.State.TPM = nil },
		"duplicate TPM name":               func(m *CaptureManifest) { m.TPMMembers = append(m.TPMMembers, m.TPMMembers[0]) },
		"unsafe TPM name":                  func(m *CaptureManifest) { m.TPMMembers[0].Name = "../permanent" },
		"TPM wrong member kind":            func(m *CaptureManifest) { m.TPMMembers[0].MemberID = "nvram" },
		"TPM inconsistent source":          func(m *CaptureManifest) { m.Source.State.TPM.SourcePath = "" },
		"TPM invalid version":              func(m *CaptureManifest) { m.Source.State.TPM.Version = "1.2" },
		"TPM invalid profile":              func(m *CaptureManifest) { m.Source.State.TPM.Profile = "custom\x00" },
		"TPM bad encryption ID":            func(m *CaptureManifest) { m.Source.State.TPM.EncryptionSecret = "bad" },
		"source invalid secret":            func(m *CaptureManifest) { m.Source.State.SecretReferences[0] = "bad" },
		"duplicate source secret": func(m *CaptureManifest) {
			m.Source.State.SecretReferences = append(m.Source.State.SecretReferences, m.Source.State.SecretReferences[0])
		},
		"missing secret disposition":   func(m *CaptureManifest) { m.Secrets = nil },
		"duplicate secret disposition": func(m *CaptureManifest) { m.Secrets = append(m.Secrets, m.Secrets[0]) },
		"extra secret disposition":     func(m *CaptureManifest) { m.Secrets = append(m.Secrets, CapturedSecret{UUID: m.ID, External: true}) },
		"secret both dispositions":     func(m *CaptureManifest) { m.Secrets[0].External = true; m.IndependentlyRecoverable = false },
		"secret neither disposition":   func(m *CaptureManifest) { m.Secrets[0].MemberID = "" },
		"external secret independent":  func(m *CaptureManifest) { m.Secrets[0].MemberID = ""; m.Secrets[0].External = true },
		"secret wrong role":            func(m *CaptureManifest) { m.Secrets[0].MemberID = "tpm" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := fullCaptureFixture()
			mutate(&m)
			err := m.Validate()
			var typed *domain.Error
			if !errors.As(err, &typed) || typed.Code != "INVALID_INPUT" && typed.Code != "INCOMPLETE_BACKUP" {
				t.Fatalf("expected typed refusal, got %v", err)
			}
			if data, e := json.Marshal(m); e == nil {
				got, e := DecodeCaptureManifest(data)
				if e == nil || !reflect.DeepEqual(got, CaptureManifest{}) {
					t.Fatalf("invalid input returned declaration: %v", e)
				}
			}
		})
	}
}

func TestCaptureManifestRejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"", ".", "..", "../disk", "/disk", "a/../disk", "a//disk", "a/./disk", "a/", "C:disk", `a\disk`, "a\x00b", "a\n", "a\u202eb", " disk", "disk ", strings.Repeat("x", 1025), strings.Repeat("a/", 32) + "disk", string([]byte{0xff})} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			m := captureFixture()
			m.Members[0].Path = name
			if err := m.Validate(); err == nil {
				t.Fatal("unsafe member path accepted")
			}
		})
	}
}

func TestCaptureManifestBounds(t *testing.T) {
	for _, count := range []int{manifestMemberLimit, manifestMemberLimit + 1} {
		m := captureFixture()
		m.Source.Disks, m.Disks = nil, nil
		m.Members = m.Members[1:]
		for i := 0; i < count-1; i++ {
			id := fmt.Sprintf("vda%d", i)
			m.Source.Disks = append(m.Source.Disks, domain.ColdDiskSource{Target: id, Device: "disk", Bus: "virtio", Source: domain.ColdStorageSource{Type: "file", File: "/fixture/" + id}, Backing: []domain.ColdStorageSource{}})
			m.Members = append(m.Members, CaptureMember{ID: id, Kind: "disk", Path: id, Size: 1, SHA256: strings.Repeat("a", 64)})
			m.Disks = append(m.Disks, CapturedDisk{Target: id, MemberID: id, Format: "raw", Independent: true})
		}
		if err := m.Validate(); (err == nil) != (count == manifestMemberLimit) {
			t.Fatalf("member count %d: %v", count, err)
		}
	}
	m := captureFixture()
	m.Members[0].Size = manifestMemberSizeLimit
	for i := 1; i < 4; i++ {
		d := m.Source.Disks[0]
		d.Target = fmt.Sprintf("vda%d", i)
		d.Source.File += fmt.Sprint(i)
		m.Source.Disks = append(m.Source.Disks, d)
		member := m.Members[0]
		member.ID, member.Path = d.Target, d.Target
		m.Members = append(m.Members, member)
		m.Disks = append(m.Disks, CapturedDisk{Target: d.Target, MemberID: member.ID, Format: "qcow2", Independent: true})
	}
	m.Members[len(m.Members)-1].Size -= m.Members[1].Size
	if err := m.Validate(); err != nil {
		t.Fatalf("exact total bound refused: %v", err)
	}
	m.Members[len(m.Members)-1].Size++
	if err := m.Validate(); err == nil {
		t.Fatal("aggregate size above limit accepted")
	}
	for name, edit := range map[string]func(*CaptureManifest){
		"native versions": func(m *CaptureManifest) {
			for i := 0; i < captureNativeLimit; i++ {
				m.NativeVersions[fmt.Sprint(i)] = "v"
			}
		},
		"backing count": func(m *CaptureManifest) {
			for i := 0; i <= captureBackingLimit; i++ {
				m.Source.Disks[0].Backing = append(m.Source.Disks[0].Backing, domain.ColdStorageSource{Type: "file", File: fmt.Sprintf("/fixture/base%d", i)})
			}
		},
		"external count": func(m *CaptureManifest) {
			m.IndependentlyRecoverable = false
			for i := 0; i <= captureSourceEntryLimit; i++ {
				m.Source.External = append(m.Source.External, domain.ColdDependency{Kind: "hostdev", Target: fmt.Sprintf("devices/device[%d]", i)})
			}
		},
		"secret count":      func(m *CaptureManifest) { m.Source.State.SecretReferences = make([]string, manifestReferenceLimit+1) },
		"TPM mapping count": func(m *CaptureManifest) { m.TPMMembers = make([]CapturedTPMFile, manifestMemberLimit+1) },
	} {
		t.Run(name, func(t *testing.T) {
			m := captureFixture()
			edit(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("excessive declaration accepted")
			}
		})
	}
}

func TestCaptureManifestRequiresExplicitCollections(t *testing.T) {
	for name, unset := range map[string]func(*CaptureManifest){
		"members":             func(m *CaptureManifest) { m.Members = nil },
		"disk mappings":       func(m *CaptureManifest) { m.Disks = nil },
		"TPM mappings":        func(m *CaptureManifest) { m.TPMMembers = nil },
		"secret dispositions": func(m *CaptureManifest) { m.Secrets = nil },
		"source disks":        func(m *CaptureManifest) { m.Source.Disks = nil },
		"source external":     func(m *CaptureManifest) { m.Source.External = nil },
		"source secrets":      func(m *CaptureManifest) { m.Source.State.SecretReferences = nil },
		"source backing":      func(m *CaptureManifest) { m.Source.Disks[0].Backing = nil },
		"native versions":     func(m *CaptureManifest) { m.NativeVersions = nil },
	} {
		t.Run(name, func(t *testing.T) {
			m := captureFixture()
			unset(&m)
			if err := m.Validate(); err == nil {
				t.Fatal("nil required collection accepted")
			}
		})
	}
}

func TestCaptureManifestRejectsSharedOutputAndConflictingTPMNames(t *testing.T) {
	m := captureFixture()
	d := m.Source.Disks[0]
	d.Target, d.Source.File = "vdb", "/fixture/second.raw"
	m.Source.Disks = append(m.Source.Disks, d)
	m.Disks = append(m.Disks, CapturedDisk{Target: "vdb", MemberID: "disk", Format: "raw", Independent: true})
	if err := m.Validate(); err == nil {
		t.Fatal("two source disks shared one captured output")
	}
	for _, name := range []string{"state/volatile", "STATE/PERMANENT", "state", "state/permanent/child"} {
		t.Run(name, func(t *testing.T) {
			m := fullCaptureFixture()
			m.Members = append(m.Members, CaptureMember{ID: "tpm2", Kind: "tpm", Path: "state/tpm2", SHA256: strings.Repeat("e", 64), Size: 0})
			m.TPMMembers = append(m.TPMMembers, CapturedTPMFile{Name: name, MemberID: "tpm2"})
			if err := m.Validate(); (err == nil) != (name == "state/volatile") {
				t.Fatalf("TPM name conflict result: %v", err)
			}
		})
	}
}

func TestDecodeCaptureManifestStrictAndZeroOnError(t *testing.T) {
	encoded, err := json.Marshal(fullCaptureFixture())
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string][]byte{
		"empty": nil, "null": []byte("null"), "array": []byte("[]"), "oversized": bytes.Repeat([]byte(" "), manifestDocumentLimit+1),
		"unknown":              bytes.Replace(encoded, []byte(`"kind":`), []byte(`"unexpected":false,"kind":`), 1),
		"duplicate":            bytes.Replace(encoded, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		"escaped duplicate":    bytes.Replace(encoded, []byte(`"version":1`), []byte(`"version":1,"\u0076ersion":1`), 1),
		"case alias":           bytes.Replace(encoded, []byte(`"version":1`), []byte(`"Version":1`), 1),
		"case duplicate":       bytes.Replace(encoded, []byte(`"version":1`), []byte(`"version":1,"Version":1`), 1),
		"nested unknown":       bytes.Replace(encoded, []byte(`"providerID":`), []byte(`"unknown":null,"providerID":`), 1),
		"null scalar":          bytes.Replace(encoded, []byte(`"hasManagedSave":false`), []byte(`"hasManagedSave":null`), 1),
		"null struct":          bytes.Replace(encoded, []byte(`"stateBefore":"stopped"`), []byte(`"stateBefore":null`), 1),
		"invalid UTF8":         append(append([]byte{}, encoded[:10]...), 0xff),
		"unpaired surrogate":   bytes.Replace(encoded, []byte(`"stopped"`), []byte(`"\ud800"`), 1),
		"trailing":             append(append([]byte{}, encoded...), []byte(" {}")...),
		"truncated":            encoded[:len(encoded)-1],
		"missing false claim":  bytes.Replace(encoded, []byte(`"hasManagedSave":false,`), nil, 1),
		"missing empty field":  bytes.Replace(encoded, []byte(`"loaderStateless":"",`), nil, 1),
		"null source array":    bytes.Replace(encoded, []byte(`"externalDependencies":[]`), []byte(`"externalDependencies":null`), 1),
		"missing source array": bytes.Replace(encoded, []byte(`,"externalDependencies":[]`), nil, 1),
		"null member size":     bytes.Replace(encoded, []byte(`"size":16`), []byte(`"size":null`), 1),
		"oversized integer":    bytes.Replace(encoded, []byte(`"size":1024`), []byte(`"size":9223372036854775808`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeCaptureManifest(input)
			if err == nil || !reflect.DeepEqual(got, CaptureManifest{}) {
				t.Fatalf("ambiguous JSON accepted: %v", err)
			}
		})
	}
}

func TestCaptureManifestSharedSyntheticDeclaration(t *testing.T) {
	data, err := os.ReadFile("../../../tests/fixtures/protection/capture-manifest/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeCaptureManifest(data); err != nil {
		t.Fatal(err)
	}
	// This is shared declaration coverage only, never native provenance evidence.
}

func FuzzDecodeCaptureManifest(f *testing.F) {
	for _, m := range []CaptureManifest{captureFixture(), fullCaptureFixture()} {
		data, err := json.Marshal(m)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	for _, data := range []string{"null", "{}", `{"version":1,"version":2}`, `{"members":[null]}`} {
		f.Add([]byte(data))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := DecodeCaptureManifest(data)
		if err != nil {
			if !reflect.DeepEqual(m, CaptureManifest{}) {
				t.Fatal("partial manifest on error")
			}
			return
		}
		if err := m.Validate(); err != nil {
			t.Fatalf("decoded invalid declaration: %v", err)
		}
		encoded, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		again, err := DecodeCaptureManifest(encoded)
		if err != nil || !reflect.DeepEqual(m, again) {
			t.Fatalf("unstable accepted declaration: %v", err)
		}
	})
}
