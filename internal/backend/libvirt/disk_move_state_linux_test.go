//go:build linux && amd64 && cgo

package libvirt

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func moveFixture() domain.DiskMove {
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///session", Kind: "vm", UUID: "7e472a89-a207-4615-af5f-2a10a734943b"}
	disk := domain.RemovalDisk{Target: "sdb", Path: "/pool/source/virmill-7e472a89-a207-4615-af5f-2a10a734943b-disk-001.qcow2",
		PoolID: "0f1e2d3c-4b5a-4968-8776-655443322110", VolumeName: "virmill-7e472a89-a207-4615-af5f-2a10a734943b-disk-001.qcow2",
		VolumeKey:  "/pool/source/virmill-7e472a89-a207-4615-af5f-2a10a734943b-disk-001.qcow2",
		Generation: "linux-statx-v1:8:1:42:1:000000000", Fingerprint: strings.Repeat("d", 64), Format: "qcow2",
		CapacityBytes: 1 << 30, AllocatedBytes: 200 << 20}
	name := "virmill-7e472a89-a207-4615-af5f-2a10a734943b-disk-000.qcow2"
	m := domain.DiskMove{VM: key, VMFingerprint: strings.Repeat("a", 64), DefinitionSHA256: strings.Repeat("b", 64),
		Disk: disk, SourcePoolName: "source", PoolID: "11111111-2222-4333-8444-555555555555", PoolName: "archive",
		Volume:     domain.VolumeIntent{PoolID: "11111111-2222-4333-8444-555555555555", Name: name, VirtualBytes: 1 << 30, FileBytes: 1 << 30, SHA256: strings.Repeat("c", 64)},
		VolumePath: filepath.Join("/pool/archive", name), DestinationAvailableBytes: 8 << 30}
	m.ResourceIDs = append([]string{key.String(), "local-file|" + m.VolumePath}, diskRemovalResources(key.ConnectionID, disk)...)
	sort.Strings(m.ResourceIDs)
	return m
}

// The state is read from the definition and the two volumes, never guessed.
func TestMoveStateReportsOnlyWhatTheHostShows(t *testing.T) {
	for name, test := range map[string]struct {
		namesCopy, sourcePresent, copyPresent bool
		want, code                            string
	}{
		"nothing done yet":                  {false, true, false, "before", ""},
		"copy written":                      {false, true, true, "copied", ""},
		"definition retargeted":             {true, true, true, "retargeted", ""},
		"original deleted":                  {true, false, true, "old-deleted", ""},
		"named copy is missing":             {true, true, false, "", "RECOVERY_REQUIRED"},
		"named copy and original missing":   {true, false, false, "", "RECOVERY_REQUIRED"},
		"original gone before the copy":     {false, false, false, "", "SOURCE_CHANGED"},
		"original gone, copy not yet named": {false, false, true, "", "SOURCE_CHANGED"},
	} {
		state, err := moveState(test.namesCopy, test.sourcePresent, test.copyPresent)
		if test.code == "" {
			if err != nil || state != test.want {
				t.Fatal(name, state, err)
			}
			continue
		}
		var refusal *domain.Error
		if !errors.As(err, &refusal) || refusal.Code != test.code {
			t.Fatal(name, err)
		}
	}
}

// A move must carry a complete, exact identity for both copies, and the two
// pools must differ: a move within one pool would copy a disk onto itself.
func TestValidDiskMoveRequiresACompleteDistinctMove(t *testing.T) {
	if err := validDiskMove(moveFixture()); err != nil {
		t.Fatal(err)
	}
	for name, spoil := range map[string]func(*domain.DiskMove){
		"same pool":            func(m *domain.DiskMove) { m.PoolID = m.Disk.PoolID },
		"intent in other pool": func(m *domain.DiskMove) { m.Volume.PoolID = m.Disk.PoolID },
		"no destination name":  func(m *domain.DiskMove) { m.PoolName = "" },
		"no source name":       func(m *domain.DiskMove) { m.SourcePoolName = "" },
		"raw disk":             func(m *domain.DiskMove) { m.Disk.Format = "raw" },
		"unbound definition":   func(m *domain.DiskMove) { m.DefinitionSHA256 = "" },
		"copy path elsewhere":  func(m *domain.DiskMove) { m.VolumePath = "/pool/archive/other.qcow2" },
		"copy over original":   func(m *domain.DiskMove) { m.VolumePath = m.Disk.Path },
		"relative copy path":   func(m *domain.DiskMove) { m.VolumePath = "archive/copy.qcow2" },
		"media content type":   func(m *domain.DiskMove) { m.Volume.ContentType = "cdrom-iso" },
		"unsorted locks":       func(m *domain.DiskMove) { m.ResourceIDs = []string{"z", "a", "m", "b"} },
		"missing lock":         func(m *domain.DiskMove) { m.ResourceIDs = m.ResourceIDs[:3] },
	} {
		m := moveFixture()
		spoil(&m)
		if err := validDiskMove(m); err == nil {
			t.Fatal(name, "was accepted")
		}
	}
}
