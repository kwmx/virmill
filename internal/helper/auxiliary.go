package helper

import (
	"encoding/hex"
	"errors"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

// AuxiliaryPermission is explicit additional authority. Legacy storage roots,
// keys and actors alone never authorize confidential-state observation or copy.
// There is no wildcard resource, key, actor or root. Bounds cover the whole set.
type AuxiliaryPermission struct {
	ActorUID     uint32 `json:"actorUID"`
	KeyID        string `json:"keyID"`
	ResourceID   string `json:"resourceID"`
	RootID       string `json:"rootID"`
	StateUID     uint32 `json:"stateUID"`
	StateGID     uint32 `json:"stateGID"`
	MaxBytes     uint64 `json:"maxBytes"`
	MaxMembers   uint32 `json:"maxMembers"`
	AllowCapture bool   `json:"allowCapture"`
}

const MaxAuxiliaryMembers = 128
const MaxAuxiliaryPayloadBytes uint64 = 255 << 20

// AuxiliaryRequest selects a native VM, never a caller-selected file. Expected
// is the previously observed complete inventory, re-derived by the helper.
type AuxiliaryRequest struct {
	Version     int                 `json:"version"`
	Fingerprint string              `json:"fingerprint"`
	Expected    *AuxiliaryInventory `json:"expected,omitempty"`
}

// AuxiliaryFileState is metadata only. The generation syntax is owned by the
// Linux adapter; these platform-neutral fields contain no confidential bytes.
type AuxiliaryFileState struct {
	Generation string `json:"generation"`
	Size       uint64 `json:"size"`
	Modified   string `json:"modified"`
	Changed    string `json:"changed"`
	Links      uint32 `json:"links"`
	Mode       uint16 `json:"mode"`
	UID        uint32 `json:"uid"`
	GID        uint32 `json:"gid"`
	ACL        string `json:"acl"`
	SELinux    string `json:"selinux"`
}
type AuxiliaryRoot struct {
	ID    string             `json:"id"`
	Path  string             `json:"path"`
	State AuxiliaryFileState `json:"state"`
}
type AuxiliaryDirectory struct {
	RelativePath string             `json:"relativePath"`
	State        AuxiliaryFileState `json:"state"`
}
type AuxiliaryMember struct {
	ID           string             `json:"id"`
	Kind         string             `json:"kind"`
	RelativePath string             `json:"relativePath"`
	State        AuxiliaryFileState `json:"state"`
}
type AuxiliaryInventory struct {
	Version     int                    `json:"version"`
	Resource    domain.ResourceKey     `json:"resource"`
	Fingerprint string                 `json:"fingerprint"`
	Layout      domain.ColdStateLayout `json:"layout"`
	Root        AuxiliaryRoot          `json:"root"`
	Directories []AuxiliaryDirectory   `json:"directories"`
	Members     []AuxiliaryMember      `json:"members"`
	TotalBytes  uint64                 `json:"totalBytes"`
	TPMLock     *AuxiliaryMember       `json:"tpmLock,omitempty"`
}

// The archive is a deterministic USTAR set of members/000, members/001, ... .
// Mapping and hashes remain in typed metadata; no source names become archive
// entries. This proof describes copied auxiliary members, not a complete VM
// recovery point, durable coordinator delivery, independent restore or boot.
type AuxiliaryArtifact struct {
	Format          string            `json:"format"`
	Size            uint64            `json:"size"`
	SHA256          string            `json:"sha256"`
	InventoryDigest string            `json:"inventoryDigest"`
	MemberSHA256    map[string]string `json:"memberSHA256"`
}
type AuxiliaryResponse struct {
	Version   int                 `json:"version"`
	JobID     string              `json:"jobID"`
	Binding   string              `json:"binding"`
	Stage     string              `json:"stage"`
	Inventory *AuxiliaryInventory `json:"inventory,omitempty"`
	Artifact  *AuxiliaryArtifact  `json:"artifact,omitempty"`
}

var auxiliaryUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func auxiliaryID(s string) bool {
	return auxiliaryUUID.MatchString(s) && s != "00000000-0000-0000-0000-000000000000"
}
func auxiliaryDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

func auxiliaryAbsolutePath(s string) bool {
	if s == "/" || len(s) > 4096 || !utf8.ValidString(s) || !path.IsAbs(s) || path.Clean(s) != s || strings.Contains(s, "\\") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return false
		}
	}
	return true
}
func auxiliaryPolicy(r Request, p Policy) (AuxiliaryPermission, error) {
	var selected AuxiliaryPermission
	count := 0
	if len(p.Auxiliary) > 1024 {
		return selected, errors.New("auxiliary permission inventory exceeds bound")
	}
	for _, grant := range p.Auxiliary {
		if grant.ActorUID == r.ActorUID && grant.KeyID == r.KeyID && grant.ResourceID == r.ResourceID && grant.RootID == r.RootID {
			selected = grant
			count++
		}
	}
	root := p.Roots[r.RootID]
	if count != 1 || selected.ActorUID == 0 || selected.StateUID == ^uint32(0) || selected.StateGID == ^uint32(0) || selected.MaxBytes == 0 || selected.MaxBytes > MaxAuxiliaryPayloadBytes || selected.MaxMembers == 0 || selected.MaxMembers > MaxAuxiliaryMembers || !auxiliaryID(selected.ResourceID) || !auxiliaryAbsolutePath(root) {
		return AuxiliaryPermission{}, errors.New("one explicit bounded VM/key/actor/root auxiliary permission required")
	}
	if r.Mode != "inspect" && !selected.AllowCapture {
		return AuxiliaryPermission{}, errors.New("administrator has not authorized auxiliary capture")
	}
	return selected, nil
}
func authorizeAuxiliary(r Request, p Policy) error {
	if r.Access != nil || r.Auxiliary == nil || r.Auxiliary.Version != 1 || !auxiliaryID(r.ResourceID) || !auxiliaryDigest(r.Auxiliary.Fingerprint) {
		return errors.New("versioned native auxiliary identity and fingerprint required")
	}
	switch r.Mode {
	case "inspect":
		if r.Auxiliary.Expected != nil {
			return errors.New("inspection cannot carry caller-selected source metadata")
		}
	case "capture", "observe":
		in := r.Auxiliary.Expected
		if in == nil || in.Version != 1 || in.Resource != (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: r.ResourceID}) || in.Fingerprint != r.Auxiliary.Fingerprint || in.Layout.VMID != r.ResourceID || in.Root.ID != r.RootID || in.Root.Path != p.Roots[r.RootID] || in.Members == nil || in.Directories == nil {
			return errors.New("capture requires the exact native inventory and approved root mapping")
		}
	default:
		return errors.New("explicit auxiliary inspect/capture/observe mode required")
	}
	permission, err := auxiliaryPolicy(r, p)
	if err != nil {
		return err
	}
	if in := r.Auxiliary.Expected; in != nil && (len(in.Members) == 0 || len(in.Members) > int(permission.MaxMembers) || len(in.Directories) > 128 || in.TotalBytes > permission.MaxBytes) {
		return errors.New("auxiliary inventory exceeds administrator bounds")
	}
	if in := r.Auxiliary.Expected; in != nil {
		var total uint64
		for _, member := range in.Members {
			if member.State.Size > permission.MaxBytes-total {
				return errors.New("auxiliary member sizes exceed administrator bound")
			}
			total += member.State.Size
		}
		if total != in.TotalBytes {
			return errors.New("auxiliary declared total differs from complete member sizes")
		}
	}
	return nil
}

// AuxiliaryBinding excludes only invocation freshness and inspect/capture/observe
// mode. Observation of an existing journal record must preserve all other grant
// fields, including the exact previously observed native inventory.
func AuxiliaryBinding(r Request) (string, error) {
	return operations.Digest(struct {
		Actor                                     uint32
		Operation, Resource, Root, Plan, Job, Key string
		Auxiliary                                 *AuxiliaryRequest
	}{r.ActorUID, r.Operation, r.ResourceID, r.RootID, r.PlanDigest, r.JobID, r.KeyID, r.Auxiliary})
}

// ValidateAuxiliaryInspection binds a metadata response to the dedicated request.
// The privileged executor separately derives and validates every native path.
func ValidateAuxiliaryInspection(r Request, response *AuxiliaryResponse) error {
	invalid := func() error {
		return domain.Fail("RECOVERY_REQUIRED", "helper inventory does not prove the requested metadata observation")
	}
	if r.Operation != "state.auxiliary" || r.Mode != "inspect" || r.Access != nil || r.Auxiliary == nil || r.Auxiliary.Version != 1 || r.Auxiliary.Expected != nil || response == nil {
		return invalid()
	}
	binding, err := AuxiliaryBinding(r)
	if err != nil {
		return err
	}
	in := response.Inventory
	if response.Version != 1 || response.JobID != r.JobID || response.Binding != binding || response.Stage != "inspected" || response.Artifact != nil || in == nil || in.Version != 1 || in.Resource != (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: r.ResourceID}) || in.Fingerprint != r.Auxiliary.Fingerprint || in.Layout.VMID != r.ResourceID || in.Root.ID != r.RootID || !auxiliaryAbsolutePath(in.Root.Path) || in.Directories == nil || in.Members == nil || len(in.Directories) > 128 || len(in.Members) == 0 || len(in.Members) > MaxAuxiliaryMembers || in.TotalBytes > MaxAuxiliaryPayloadBytes {
		return invalid()
	}
	var total uint64
	for _, member := range in.Members {
		if member.State.Size > MaxAuxiliaryPayloadBytes-total {
			return invalid()
		}
		total += member.State.Size
	}
	if total != in.TotalBytes {
		return invalid()
	}
	return nil
}
