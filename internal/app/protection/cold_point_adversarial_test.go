package protection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"virmill.local/core/internal/domain"
)

func coldPointRefuses(t *testing.T, m CaptureManifest, code string) {
	t.Helper()
	var failure *domain.Error
	if err := m.Validate(); !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("declaration did not return %s: %v", code, err)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCaptureManifest(raw)
	if !errors.As(err, &failure) || failure.Code != code || !reflect.DeepEqual(decoded, CaptureManifest{}) {
		t.Fatalf("wire boundary changed refusal or returned partial declaration: %v", err)
	}
}

func TestColdPointExplicitEmptyMappingsCannotHideRequiredMembers(t *testing.T) {
	// Existing omission cases use nil slices and stop at the required-array
	// guard. These explicit arrays must reach semantic completeness checks.
	for _, role := range []string{"disk", "tpm", "secret", "tpm-only-secret"} {
		t.Run(role, func(t *testing.T) {
			m := fullCaptureFixture()
			switch role {
			case "disk":
				m.Disks = []CapturedDisk{}
			case "tpm":
				m.TPMMembers = []CapturedTPMFile{}
			case "secret":
				m.Secrets = []CapturedSecret{}
			case "tpm-only-secret":
				m.Source.State.SecretReferences = []string{}
				m.Secrets = []CapturedSecret{}
			}
			coldPointRefuses(t, m, "INCOMPLETE_BACKUP")
		})
	}
}

func TestColdPointEncryptionReferenceUnionRequiresEveryDisposition(t *testing.T) {
	m := fullCaptureFixture()
	// A secret referenced only by TPM encryption still has a required role;
	// listing it again in the general source array is not required.
	m.Source.State.SecretReferences = []string{}
	if err := m.Validate(); err != nil {
		t.Fatal("TPM-only secret's included disposition was lost", err)
	}
	// A different encryption UUID adds a requirement; it cannot be satisfied
	// merely by including the general source secret.
	m = fullCaptureFixture()
	secondUUID := "77777777-aaaa-1bbb-8ccc-dddddddddddd"
	m.Source.State.TPM.EncryptionSecret = secondUUID
	coldPointRefuses(t, m, "INCOMPLETE_BACKUP")
	member := m.Members[5]
	if member.Kind != "secret" {
		t.Fatal("full fixture secret role changed")
	}
	member.ID, member.Path = "second-secret", "state/second-secret"
	m.Members = append(m.Members, member)
	m.Secrets = append(m.Secrets, CapturedSecret{UUID: secondUUID, MemberID: member.ID})
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeCaptureManifest(raw); err != nil {
		t.Fatal("complete union of distinct secret dispositions refused", err)
	}
}

func TestColdPointDistinctIdentitiesCannotShareOrSwapArtifactRoles(t *testing.T) {
	for _, scenario := range []string{"distinct-tpm-names-one-member", "distinct-secret-uuids-one-member", "nvram-inventory-role-swap"} {
		t.Run(scenario, func(t *testing.T) {
			m := fullCaptureFixture()
			switch scenario {
			case "distinct-tpm-names-one-member":
				m.TPMMembers = append(m.TPMMembers, CapturedTPMFile{Name: "state/volatile", MemberID: m.TPMMembers[0].MemberID})
			case "distinct-secret-uuids-one-member":
				secondUUID := "77777777-aaaa-1bbb-8ccc-dddddddddddd"
				m.Source.State.SecretReferences = append(m.Source.State.SecretReferences, secondUUID)
				m.Secrets = append(m.Secrets, CapturedSecret{UUID: secondUUID, MemberID: m.Secrets[0].MemberID})
			case "nvram-inventory-role-swap":
				// All member IDs/counts remain present. An inventory document
				// cannot serve as confidential NVRAM by swapping references.
				m.NVRAMMember, m.AuxiliaryInventoryMember = m.AuxiliaryInventoryMember, m.NVRAMMember
			}
			coldPointRefuses(t, m, "INVALID_INPUT")
		})
	}
}

func TestColdPointAggregateSourceInventoryBound(t *testing.T) {
	m := captureFixture()
	template := m.Source.Disks[0]
	m.Source.Disks, m.Disks = []domain.ColdDiskSource{}, []CapturedDisk{}
	m.Members = m.Members[1:]
	// Each chain individually fits the backing bound, and the member count is
	// below its bound. This reaches the distinct aggregate source-entry limit.
	for i := 0; i < 64; i++ {
		disk := template
		disk.Target = fmt.Sprintf("vda%d", i)
		disk.Source.File = fmt.Sprintf("/fixture/disk-%d.qcow2", i)
		disk.Backing = []domain.ColdStorageSource{}
		for j := 0; j < 15; j++ {
			disk.Backing = append(disk.Backing, domain.ColdStorageSource{Type: "file", File: fmt.Sprintf("/fixture/base-%d-%d.raw", i, j)})
		}
		m.Source.Disks = append(m.Source.Disks, disk)
		m.Members = append(m.Members, CaptureMember{ID: disk.Target, Kind: "disk", Path: "disks/" + disk.Target, Size: 1, SHA256: m.SourceFingerprint})
		m.Disks = append(m.Disks, CapturedDisk{Target: disk.Target, MemberID: disk.Target, Format: "raw", Independent: true})
	}
	if len(m.Source.Disks)*(1+len(m.Source.Disks[0].Backing)) != captureSourceEntryLimit {
		t.Fatal("fixture no longer reaches the intended 1024-entry boundary")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeCaptureManifest(raw); err != nil {
		t.Fatal("exact aggregate bound refused", err)
	}
	m.Source.Disks[0].Backing = append(m.Source.Disks[0].Backing, domain.ColdStorageSource{Type: "file", File: "/fixture/one-extra.raw"})
	coldPointRefuses(t, m, "INVALID_INPUT")
}

func TestColdPointForgedAuxiliaryClaimsRemainUnverifiedAndIntegrityChecked(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("ordinary member reader is Linux-only; declaration cases remain portable")
	}
	m := fullCaptureFixture()
	root := t.TempDir()
	// These generated bytes are deliberately not a real disk, native XML,
	// firmware or an authenticated helper inventory. Their hashes can match
	// while every capture/recovery claim remains unproven.
	for i := range m.Members {
		member := &m.Members[i]
		payload := []byte("generated untrusted member: " + member.ID)
		if member.Kind == "auxiliary-inventory" {
			payload = []byte(`{"completeCaptureVerified":true,"independentRecoveryVerified":true,"bootTested":true,"sourceVM":"different-guest","state":"running","helperAuthenticated":true}`)
		}
		hash := sha256.Sum256(payload)
		member.Size, member.SHA256 = int64(len(payload)), hex.EncodeToString(hash[:])
		filename := filepath.Join(root, member.Path)
		if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "manifest.json")
	if err = os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	report, err := CheckManifestContext(context.Background(), manifest, root)
	if err != nil || report["membersChecked"] != true || report["manifestKind"] != "ColdRecoveryPoint" {
		t.Fatal("generated declared integrity was not checked", report, err)
	}
	for _, claim := range []string{"completeCaptureVerified", "independentRecoveryVerified", "bootTested"} {
		if report[claim] != false {
			t.Fatal("forged auxiliary document promoted an unauthenticated claim", claim, report)
		}
	}
	var auxiliaryPath string
	for _, member := range m.Members {
		if member.ID == m.AuxiliaryInventoryMember {
			auxiliaryPath = filepath.Join(root, member.Path)
		}
	}
	if auxiliaryPath == "" {
		t.Fatal("fixture lacks auxiliary inventory member")
	}
	payload, err := os.ReadFile(auxiliaryPath)
	if err != nil {
		t.Fatal(err)
	}
	payload[len(payload)-1] ^= 1 // Same size; only its declared digest now differs.
	if err = os.WriteFile(auxiliaryPath, payload, 0600); err != nil {
		t.Fatal(err)
	}
	report, err = CheckManifestContext(context.Background(), manifest, root)
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != "INCOMPLETE_BACKUP" || report != nil {
		t.Fatal("auxiliary member corruption was skipped or retained success", report, err)
	}
}
