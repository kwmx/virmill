package helper

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

// These tests exercise common authorization only. All keys and metadata are
// generated in memory; no key files, native VM observations or capture occur.
func auxiliaryAuthFixture(t *testing.T, mode string) (Request, Policy, ed25519.PrivateKey, time.Time) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 8, 1, 0, 0, 0, time.UTC)
	vm := "f81d4fae-7dec-11d0-a765-00a0c91e6bf6" // Native VM IDs need not be v4.
	root := "/approved generated fixture"
	state := AuxiliaryFileState{Generation: "linux-statx-v1:0:7:100:1700000000:000000000", Size: 1024, Modified: "1700000001:000000000", Changed: "1700000002:000000000", Links: 1, Mode: 0100600, UID: 42, GID: 43, ACL: "", SELinux: "fixture-label"}
	directory := state
	directory.Generation, directory.Size, directory.Mode = "linux-statx-v1:0:7:99:1700000000:000000000", 4096, 0040700
	rootState := directory
	rootState.UID, rootState.GID = 0, 0
	second := state
	second.Generation, second.Size = "linux-statx-v1:0:7:101:1700000000:000000000", 2048
	lock := state
	lock.Generation, lock.Size = "linux-statx-v1:0:7:102:1700000000:000000000", 0
	in := &AuxiliaryInventory{
		Version: 1, Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: vm}, Fingerprint: strings.Repeat("b", 64),
		Layout:      domain.ColdStateLayout{VMID: vm, Firmware: domain.ColdFirmware{Loader: "/fixture/CODE.fd", LoaderFormat: "raw", NVRAM: &domain.ColdNVRAM{Path: root + "/vars/state", Format: "raw"}}, TPM: &domain.ColdTPM{Model: "tpm-crb", Version: "2.0", SourceType: "dir", SourcePath: root + "/tpm", PersistentState: "yes"}, SecretReferences: []string{}},
		Root:        AuxiliaryRoot{ID: "aux", Path: root, State: rootState},
		Directories: []AuxiliaryDirectory{{RelativePath: "vars", State: directory}, {RelativePath: "tpm", State: directory}},
		Members:     []AuxiliaryMember{{ID: "nvram", Kind: "nvram", RelativePath: "vars/state", State: state}, {ID: "tpm-000", Kind: "tpm", RelativePath: "tpm/state", State: second}}, TotalBytes: 3072,
		TPMLock: &AuxiliaryMember{ID: "tpm-lock", Kind: "tpm-lock", RelativePath: "tpm/control", State: lock},
	}
	r := Request{APIVersion: domain.APIVersion, ActorUID: 1000, Operation: "state.auxiliary", ResourceID: vm, RootID: "aux", PlanDigest: strings.Repeat("a", 64), JobID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", ExpiresAt: now.Add(time.Minute), KeyID: "generated-key", Mode: mode, Auxiliary: &AuxiliaryRequest{Version: 1, Fingerprint: in.Fingerprint, Expected: in}}
	if mode == "inspect" {
		r.Auxiliary.Expected = nil
	}
	p := Policy{APIVersion: domain.APIVersion, Keys: map[string]string{r.KeyID: hex.EncodeToString(pub)}, Roots: map[string]string{r.RootID: root}, Actors: []uint32{r.ActorUID}, Auxiliary: []AuxiliaryPermission{{ActorUID: r.ActorUID, KeyID: r.KeyID, ResourceID: vm, RootID: r.RootID, StateUID: 42, StateGID: 43, MaxBytes: 4096, MaxMembers: 4, AllowCapture: true}}}
	return r, p, key, now
}

func signAuxiliaryAuth(t *testing.T, r *Request, key ed25519.PrivateKey) {
	t.Helper()
	b, err := SignedBytes(*r)
	if err != nil {
		t.Fatal(err)
	}
	r.Signature = hex.EncodeToString(ed25519.Sign(key, b))
}

func cloneAuxiliaryAuth(t *testing.T, r Request) Request {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var out Request
	if err = json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAuxiliaryAuthorizationExplicitPolicyAndModes(t *testing.T) {
	for _, mode := range []string{"inspect", "capture", "observe"} {
		t.Run(mode, func(t *testing.T) {
			r, p, key, now := auxiliaryAuthFixture(t, mode)
			signAuxiliaryAuth(t, &r, key)
			if err := Authorize(r.ActorUID, r, p, now); err != nil {
				t.Fatalf("explicit current permission refused: %v", err)
			}
			for _, absent := range [][]AuxiliaryPermission{nil, {}} {
				old := p
				old.Auxiliary = absent
				if err := Authorize(r.ActorUID, r, old, now); err == nil {
					t.Fatal("legacy actors/keys/roots silently authorized auxiliary access")
				}
			}
			p.Auxiliary[0].AllowCapture = false
			err := Authorize(r.ActorUID, r, p, now)
			if (mode == "inspect") != (err == nil) {
				t.Fatalf("inspect-only policy for %s: %v", mode, err)
			}
		})
	}
}

func TestAuxiliaryAuthorizationRejectsFreshlySignedPolicyViolations(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Request, *Policy)
	}{
		{"permission actor", func(_ *Request, p *Policy) { p.Auxiliary[0].ActorUID++ }},
		{"permission key", func(_ *Request, p *Policy) { p.Auxiliary[0].KeyID = "other" }},
		{"permission VM", func(_ *Request, p *Policy) { p.Auxiliary[0].ResourceID = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee" }},
		{"permission root", func(_ *Request, p *Policy) { p.Auxiliary[0].RootID = "other" }},
		{"wildcard VM", func(_ *Request, p *Policy) { p.Auxiliary[0].ResourceID = "*" }},
		{"wildcard root", func(_ *Request, p *Policy) { p.Auxiliary[0].RootID = "*" }},
		{"wildcard key", func(_ *Request, p *Policy) { p.Auxiliary[0].KeyID = "*" }},
		{"zero actor", func(r *Request, p *Policy) { r.ActorUID = 0; p.Actors = []uint32{0}; p.Auxiliary[0].ActorUID = 0 }},
		{"actor removed", func(_ *Request, p *Policy) { p.Actors = nil }},
		{"key removed", func(r *Request, p *Policy) { delete(p.Keys, r.KeyID) }},
		{"invalid public key", func(r *Request, p *Policy) { p.Keys[r.KeyID] = "not-hex" }},
		{"short public key", func(r *Request, p *Policy) { p.Keys[r.KeyID] = "abcd" }},
		{"revoked public key", func(r *Request, p *Policy) { p.Keys[r.KeyID] = strings.Repeat("0", 64) }},
		{"root removed", func(r *Request, p *Policy) { delete(p.Roots, r.RootID) }},
		{"duplicate permission", func(_ *Request, p *Policy) { p.Auxiliary = append(p.Auxiliary, p.Auxiliary[0]) }},
		{"too many permissions", func(_ *Request, p *Policy) { p.Auxiliary = append(p.Auxiliary, make([]AuxiliaryPermission, 1024)...) }},
		{"zero byte budget", func(_ *Request, p *Policy) { p.Auxiliary[0].MaxBytes = 0 }},
		{"global byte budget", func(_ *Request, p *Policy) { p.Auxiliary[0].MaxBytes = MaxAuxiliaryPayloadBytes + 1 }},
		{"zero member budget", func(_ *Request, p *Policy) { p.Auxiliary[0].MaxMembers = 0 }},
		{"global member budget", func(_ *Request, p *Policy) { p.Auxiliary[0].MaxMembers = MaxAuxiliaryMembers + 1 }},
		{"member count budget", func(_ *Request, p *Policy) { p.Auxiliary[0].MaxMembers = 1 }},
		{"byte count budget", func(_ *Request, p *Policy) { p.Auxiliary[0].MaxBytes = 3071 }},
		{"invalid state owner", func(_ *Request, p *Policy) { p.Auxiliary[0].StateUID = ^uint32(0) }},
		{"invalid state group", func(_ *Request, p *Policy) { p.Auxiliary[0].StateGID = ^uint32(0) }},
		{"root-only path", func(r *Request, p *Policy) { p.Roots[r.RootID] = "/"; r.Auxiliary.Expected.Root.Path = "/" }},
		{"relative root", func(r *Request, p *Policy) {
			p.Roots[r.RootID] = "relative"
			r.Auxiliary.Expected.Root.Path = "relative"
		}},
		{"root alias", func(r *Request, p *Policy) {
			p.Roots[r.RootID] = "/approved/../other"
			r.Auxiliary.Expected.Root.Path = p.Roots[r.RootID]
		}},
		{"root NUL", func(r *Request, p *Policy) {
			p.Roots[r.RootID] = "/approved\x00other"
			r.Auxiliary.Expected.Root.Path = p.Roots[r.RootID]
		}},
		{"changed root mapping", func(r *Request, p *Policy) { p.Roots[r.RootID] = "/new-approved-root" }},
		{"unsupported policy", func(_ *Request, p *Policy) { p.APIVersion = "virmill/v2" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, p, key, now := auxiliaryAuthFixture(t, "capture")
			tc.change(&r, &p)
			signAuxiliaryAuth(t, &r, key)
			if err := Authorize(r.ActorUID, r, p, now); err == nil {
				t.Fatal("fresh signature bypassed an explicit policy bound")
			}
		})
	}
}

func TestAuxiliaryAuthorizationBoundsPermitExactLimits(t *testing.T) {
	r, p, key, now := auxiliaryAuthFixture(t, "capture")
	p.Auxiliary[0].MaxBytes = r.Auxiliary.Expected.TotalBytes
	p.Auxiliary[0].MaxMembers = uint32(len(r.Auxiliary.Expected.Members))
	r.Auxiliary.Expected.Directories = make([]AuxiliaryDirectory, 128)
	// Authorize bounds directory count; the native executor validates contents.
	p.Auxiliary = append(p.Auxiliary, make([]AuxiliaryPermission, 1023)...)
	signAuxiliaryAuth(t, &r, key)
	if err := Authorize(r.ActorUID, r, p, now); err != nil {
		t.Fatalf("exact bounds refused: %v", err)
	}
}

func TestAuxiliaryAuthorizationRejectsInvalidStableIdentities(t *testing.T) {
	for _, vm := range []string{"", "*", "00000000-0000-0000-0000-000000000000", "F81D4FAE-7DEC-11D0-A765-00A0C91E6BF6", "../fixture"} {
		t.Run("VM/"+vm, func(t *testing.T) {
			r, p, key, now := auxiliaryAuthFixture(t, "capture")
			r.ResourceID = vm
			r.Auxiliary.Expected.Resource.UUID = vm
			r.Auxiliary.Expected.Layout.VMID = vm
			p.Auxiliary[0].ResourceID = vm
			signAuxiliaryAuth(t, &r, key)
			if err := Authorize(r.ActorUID, r, p, now); err == nil {
				t.Fatal("consistently substituted invalid native VM identity authorized")
			}
		})
	}
	for _, job := range []string{"", "00000000-0000-0000-0000-000000000000", "f81d4fae-7dec-11d0-a765-00a0c91e6bf6", "../fixture"} {
		t.Run("job/"+job, func(t *testing.T) {
			r, p, key, now := auxiliaryAuthFixture(t, "inspect")
			r.JobID = job
			signAuxiliaryAuth(t, &r, key)
			if err := Authorize(r.ActorUID, r, p, now); err == nil {
				t.Fatal("invalid Virmill job identity authorized")
			}
		})
	}
}

func TestAuxiliaryAuthorizationRejectsInconsistentPayloadTotals(t *testing.T) {
	cases := []struct {
		name   string
		change func(*AuxiliaryInventory)
	}{
		{"underdeclared", func(in *AuxiliaryInventory) { in.TotalBytes-- }},
		{"zero declared", func(in *AuxiliaryInventory) { in.TotalBytes = 0 }},
		{"overdeclared", func(in *AuxiliaryInventory) { in.TotalBytes++ }},
		{"single member over policy", func(in *AuxiliaryInventory) { in.Members[0].State.Size = 4097; in.TotalBytes = 0 }},
		{"sum over policy", func(in *AuxiliaryInventory) { in.Members[0].State.Size = 2049; in.TotalBytes = 3072 }},
		{"uint64 wrapping sum", func(in *AuxiliaryInventory) {
			in.Members[0].State.Size = ^uint64(0)
			in.Members[1].State.Size = 1
			in.TotalBytes = 0
		}},
		{"uint64 maximum total", func(in *AuxiliaryInventory) { in.TotalBytes = ^uint64(0) }},
	}
	for _, mode := range []string{"capture", "observe"} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				r, p, key, now := auxiliaryAuthFixture(t, mode)
				tc.change(r.Auxiliary.Expected)
				signAuxiliaryAuth(t, &r, key)
				if err := Authorize(r.ActorUID, r, p, now); err == nil {
					t.Fatal("freshly signed false total bypassed payload bounds")
				}
			})
		}
	}
}

func TestAuxiliaryAuthorizationControlMetadataIsNotPayloadBudget(t *testing.T) {
	r, p, key, now := auxiliaryAuthFixture(t, "capture")
	p.Auxiliary[0].MaxBytes = r.Auxiliary.Expected.TotalBytes
	p.Auxiliary[0].MaxMembers = uint32(len(r.Auxiliary.Expected.Members))
	r.Auxiliary.Expected.TPMLock.State.Size = p.Auxiliary[0].MaxBytes + 1
	signAuxiliaryAuth(t, &r, key)
	if err := Authorize(r.ActorUID, r, p, now); err != nil {
		t.Fatalf("metadata-only control inode counted as payload: %v", err)
	}
	// Missing producer-control metadata is still representable. Whether capture
	// may proceed without it is a separate native-executor precondition.
	r.Auxiliary.Expected.TPMLock = nil
	signAuxiliaryAuth(t, &r, key)
	if err := Authorize(r.ActorUID, r, p, now); err != nil {
		t.Fatalf("optional control metadata became mandatory at authorization: %v", err)
	}
}

func TestAuxiliaryAuthorizationRejectsPayloadFamilyAndInventoryConfusion(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Request)
	}{
		{"access mixed with auxiliary", func(r *Request) { r.Access = &AccessRequest{} }},
		{"missing auxiliary", func(r *Request) { r.Auxiliary = nil }},
		{"future auxiliary version", func(r *Request) { r.Auxiliary.Version = 2 }},
		{"zero auxiliary version", func(r *Request) { r.Auxiliary.Version = 0 }},
		{"empty fingerprint", func(r *Request) { r.Auxiliary.Fingerprint = "" }},
		{"noncanonical fingerprint", func(r *Request) { r.Auxiliary.Fingerprint = strings.Repeat("B", 64) }},
		{"short fingerprint", func(r *Request) { r.Auxiliary.Fingerprint = strings.Repeat("b", 62) }},
		{"empty mode", func(r *Request) { r.Mode = "" }},
		{"ACL apply mode", func(r *Request) { r.Mode = "apply" }},
		{"unimplemented mode", func(r *Request) { r.Mode = "restore" }},
		{"inspection with expected inventory", func(r *Request) { r.Mode = "inspect" }},
		{"capture without inventory", func(r *Request) { r.Auxiliary.Expected = nil }},
		{"observe without inventory", func(r *Request) { r.Mode = "observe"; r.Auxiliary.Expected = nil }},
		{"future inventory", func(r *Request) { r.Auxiliary.Expected.Version = 2 }},
		{"wrong native provider", func(r *Request) { r.Auxiliary.Expected.Resource.ProviderID = "other" }},
		{"wrong native connection", func(r *Request) { r.Auxiliary.Expected.Resource.ConnectionID = "qemu:///session" }},
		{"wrong native kind", func(r *Request) { r.Auxiliary.Expected.Resource.Kind = "storage-pool" }},
		{"wrong native VM", func(r *Request) { r.Auxiliary.Expected.Resource.UUID = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee" }},
		{"wrong inventory fingerprint", func(r *Request) { r.Auxiliary.Expected.Fingerprint = strings.Repeat("c", 64) }},
		{"wrong layout VM", func(r *Request) { r.Auxiliary.Expected.Layout.VMID = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee" }},
		{"wrong inventory root ID", func(r *Request) { r.Auxiliary.Expected.Root.ID = "other" }},
		{"wrong inventory root path", func(r *Request) { r.Auxiliary.Expected.Root.Path = "/other" }},
		{"nil members", func(r *Request) { r.Auxiliary.Expected.Members = nil }},
		{"empty members", func(r *Request) { r.Auxiliary.Expected.Members = []AuxiliaryMember{} }},
		{"nil directories", func(r *Request) { r.Auxiliary.Expected.Directories = nil }},
		{"too many directories", func(r *Request) { r.Auxiliary.Expected.Directories = make([]AuxiliaryDirectory, 129) }},
		{"oversized total", func(r *Request) { r.Auxiliary.Expected.TotalBytes = 4097 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, p, key, now := auxiliaryAuthFixture(t, "capture")
			tc.change(&r)
			signAuxiliaryAuth(t, &r, key)
			if err := Authorize(r.ActorUID, r, p, now); err == nil {
				t.Fatal("fresh signature authorized a confused or unsupported payload")
			}
		})
	}
}

func TestAuxiliaryAuthorizationSignatureBindsNestedInventory(t *testing.T) {
	r, p, key, now := auxiliaryAuthFixture(t, "capture")
	signAuxiliaryAuth(t, &r, key)
	original, err := SignedBytes(r)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := hex.DecodeString(r.Signature)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		change func(*Request)
	}{
		{"actor", func(r *Request) { r.ActorUID++ }},
		{"operation", func(r *Request) { r.Operation = "storage.prepare-directory" }},
		{"key", func(r *Request) { r.KeyID = "other" }},
		{"resource", func(r *Request) { r.ResourceID = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee" }},
		{"root", func(r *Request) { r.RootID = "other" }},
		{"plan", func(r *Request) { r.PlanDigest = strings.Repeat("c", 64) }},
		{"job", func(r *Request) { r.JobID = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee" }},
		{"expiry", func(r *Request) { r.ExpiresAt = r.ExpiresAt.Add(time.Minute) }},
		{"mode", func(r *Request) { r.Mode = "observe" }},
		{"root generation", func(r *Request) { r.Auxiliary.Expected.Root.State.Generation += "-changed" }},
		{"directory path", func(r *Request) { r.Auxiliary.Expected.Directories[0].RelativePath = "other" }},
		{"directory metadata", func(r *Request) { r.Auxiliary.Expected.Directories[0].State.Changed += "-changed" }},
		{"member ID", func(r *Request) { r.Auxiliary.Expected.Members[0].ID = "other" }},
		{"member kind", func(r *Request) { r.Auxiliary.Expected.Members[0].Kind = "tpm" }},
		{"member path", func(r *Request) { r.Auxiliary.Expected.Members[0].RelativePath = "other" }},
		{"member generation", func(r *Request) { r.Auxiliary.Expected.Members[0].State.Generation += "-changed" }},
		{"member size", func(r *Request) { r.Auxiliary.Expected.Members[0].State.Size++ }},
		{"member modified", func(r *Request) { r.Auxiliary.Expected.Members[0].State.Modified += "-changed" }},
		{"member changed", func(r *Request) { r.Auxiliary.Expected.Members[0].State.Changed += "-changed" }},
		{"member links", func(r *Request) { r.Auxiliary.Expected.Members[0].State.Links++ }},
		{"member mode", func(r *Request) { r.Auxiliary.Expected.Members[0].State.Mode++ }},
		{"member UID", func(r *Request) { r.Auxiliary.Expected.Members[0].State.UID++ }},
		{"member GID", func(r *Request) { r.Auxiliary.Expected.Members[0].State.GID++ }},
		{"member ACL", func(r *Request) { r.Auxiliary.Expected.Members[0].State.ACL = "fixture-acl" }},
		{"member label", func(r *Request) { r.Auxiliary.Expected.Members[0].State.SELinux = "changed-fixture-label" }},
		{"member order", func(r *Request) {
			in := r.Auxiliary.Expected
			in.Members[0], in.Members[1] = in.Members[1], in.Members[0]
		}},
		{"total bytes", func(r *Request) { r.Auxiliary.Expected.TotalBytes++ }},
		{"firmware layout", func(r *Request) { r.Auxiliary.Expected.Layout.Firmware.NVRAM.Format = "qcow2" }},
		{"TPM layout", func(r *Request) { r.Auxiliary.Expected.Layout.TPM.SourcePath += "-changed" }},
		{"TPM control path", func(r *Request) { r.Auxiliary.Expected.TPMLock.RelativePath = "other" }},
		{"TPM control generation", func(r *Request) { r.Auxiliary.Expected.TPMLock.State.Generation += "-changed" }},
		{"TPM control presence", func(r *Request) { r.Auxiliary.Expected.TPMLock = nil }},
		{"secret references", func(r *Request) {
			r.Auxiliary.Expected.Layout.SecretReferences = []string{"bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee"}
		}},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			changed := cloneAuxiliaryAuth(t, r)
			tc.change(&changed)
			data, err := SignedBytes(changed)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(data, original) || ed25519.Verify(key.Public().(ed25519.PublicKey), data, signature) {
				t.Fatal("reviewed field omitted from cryptographic signature")
			}
			if err := Authorize(r.ActorUID, changed, p, now); err == nil {
				t.Fatal("substituted inventory authorized with old signature")
			}
		})
	}
}

func TestAuxiliaryAuthorizationFreshAuthenticationAndRevocation(t *testing.T) {
	r, p, key, now := auxiliaryAuthFixture(t, "capture")
	signAuxiliaryAuth(t, &r, key)
	for _, peer := range []uint32{0, r.ActorUID + 1} {
		if err := Authorize(peer, r, p, now); err == nil {
			t.Fatal("request actor substituted for kernel peer")
		}
	}
	if err := Authorize(r.ActorUID, r, p, r.ExpiresAt); err == nil {
		t.Fatal("grant remained valid at expiry")
	}
	r.ExpiresAt = now.Add(15*time.Minute + time.Nanosecond)
	signAuxiliaryAuth(t, &r, key)
	if err := Authorize(r.ActorUID, r, p, now); err == nil {
		t.Fatal("overlong freshly signed grant authorized")
	}
	r.ExpiresAt = now.Add(15 * time.Minute)
	signAuxiliaryAuth(t, &r, key)
	if err := Authorize(r.ActorUID, r, p, now); err != nil {
		t.Fatalf("exact maximum expiry refused: %v", err)
	}
	later := now.Add(time.Hour)
	r.Mode, r.ExpiresAt = "observe", later.Add(time.Minute)
	if err := Authorize(r.ActorUID, r, p, later); err == nil {
		t.Fatal("new observation reused old authentication")
	}
	signAuxiliaryAuth(t, &r, key)
	if err := Authorize(r.ActorUID, r, p, later); err != nil {
		t.Fatalf("fresh observation authentication refused: %v", err)
	}
	// Authentication is allowed to refresh; durable receipt/inventory matching
	// remains the executor's independent obligation, not proven by this test.
	p.Auxiliary[0].AllowCapture = false
	if err := Authorize(r.ActorUID, r, p, later); err == nil {
		t.Fatal("fresh signature bypassed revoked capture/observe permission")
	}
}

func TestAuxiliaryAuthorizationLegacySignedBytesRemainExact(t *testing.T) {
	// This pre-auxiliary record shape deliberately has no Auxiliary field. Its
	// canonical bytes are the legacy signing contract, including signature:"".
	type legacyRequest struct {
		APIVersion string         `json:"apiVersion"`
		ActorUID   uint32         `json:"actorUID"`
		Operation  string         `json:"operation"`
		ResourceID string         `json:"resourceID"`
		RootID     string         `json:"rootID"`
		PlanDigest string         `json:"planDigest"`
		JobID      string         `json:"jobID"`
		ExpiresAt  time.Time      `json:"expiresAt"`
		KeyID      string         `json:"keyID"`
		Signature  string         `json:"signature"`
		Mode       string         `json:"mode,omitempty"`
		Access     *AccessRequest `json:"access,omitempty"`
	}
	for _, operation := range []string{"storage.prepare-directory", "storage.grant-read", "storage.revoke-read"} {
		t.Run(operation, func(t *testing.T) {
			r, p, key, now := auxiliaryAuthFixture(t, "inspect")
			r.Operation, r.Auxiliary, r.Mode = operation, nil, ""
			r.ResourceID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
			p.Auxiliary = nil
			if operation != "storage.prepare-directory" {
				r.Mode = "check"
				r.Access = &AccessRequest{Mapping: domain.ManagedFileVolume{VMID: r.ResourceID, PoolID: "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee"}, RelativePath: "fixture.qcow2", Before: json.RawMessage(`{"fixture":true}`), ActorGroups: []uint32{1000}}
				if operation == "storage.revoke-read" {
					r.Access.OriginalGrantJobID = "cccccccc-bbbb-4ccc-8ddd-eeeeeeeeeeee"
				}
			}
			old := legacyRequest{r.APIVersion, r.ActorUID, r.Operation, r.ResourceID, r.RootID, r.PlanDigest, r.JobID, r.ExpiresAt, r.KeyID, "", r.Mode, r.Access}
			want, err := operations.Canonical(old)
			if err != nil {
				t.Fatal(err)
			}
			r.Signature = "must be cleared from signed representation"
			got, err := SignedBytes(r)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) || bytes.Contains(got, []byte(`"auxiliary"`)) || !bytes.Contains(got, []byte(`"signature":""`)) {
				t.Fatalf("legacy signed representation changed:\nwant %s\ngot  %s", want, got)
			}
			r.Signature = hex.EncodeToString(ed25519.Sign(key, want))
			if err := Authorize(r.ActorUID, r, p, now); err != nil {
				t.Fatalf("legacy signature no longer authorizes legacy action: %v", err)
			}
			// Start with that valid legacy request, then add and freshly sign the
			// unrelated payload. A failed signature or invalid legacy UUID must
			// not be mistaken for enforcement of the payload-family boundary.
			r.Auxiliary = &AuxiliaryRequest{Version: 1, Fingerprint: strings.Repeat("b", 64)}
			signAuxiliaryAuth(t, &r, key)
			if err := Authorize(r.ActorUID, r, p, now); err == nil {
				t.Fatal("valid legacy operation accepted a freshly signed auxiliary payload")
			}
		})
	}
}
