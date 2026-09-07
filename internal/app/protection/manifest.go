// Package protection validates declared recovery artifacts. A legacy manifest's
// self-declared inventory cannot establish complete capture or independent recovery.
package protection

import (
	"context"
	"encoding/hex"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

const (
	manifestDocumentLimit         = 8 << 20
	manifestMemberLimit           = 256
	manifestReferenceLimit        = 256
	manifestMemberSizeLimit int64 = 16 << 40
	manifestTotalSizeLimit  int64 = 64 << 40
)

type Member struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type Manifest struct {
	APIVersion               string   `json:"apiVersion"`
	ID                       string   `json:"backupID"`
	VMID                     string   `json:"sourceVMID"`
	Mode                     string   `json:"mode"`
	Consistency              string   `json:"consistency"`
	Required                 []string `json:"requiredMembers"`
	Members                  []Member `json:"members"`
	HasNVRAM                 bool     `json:"requiresNVRAM"`
	HasTPM                   bool     `json:"requiresTPM"`
	ExternalSecrets          []string `json:"externalSecrets"`
	IndependentlyRecoverable bool     `json:"independentlyRecoverable"`
}

func (m Manifest) Validate() error {
	invalid := func(message string) error { return domain.Fail("INVALID_INPUT", message) }
	if m.APIVersion != domain.APIVersion || !manifestText(m.ID) || !manifestText(m.VMID) {
		return invalid("invalid recovery manifest API version or identity")
	}
	switch m.Mode {
	case "cold":
		if m.Consistency != "cold-complete" {
			return invalid("cold capture requires the cold-complete consistency declaration")
		}
	case "live-disk":
		if m.Consistency != "crash-consistent" && m.Consistency != "filesystem-quiesced" && m.Consistency != "application-quiesced" {
			return invalid("invalid live-disk consistency declaration")
		}
	default:
		return invalid("unknown capture mode")
	}
	if m.Mode != "cold" && (m.HasTPM || m.HasNVRAM) && m.IndependentlyRecoverable {
		return invalid("live auxiliary state cannot be claimed complete without certified capture")
	}
	if len(m.Members) == 0 || len(m.Members) > manifestMemberLimit || len(m.Required) == 0 || len(m.Required) > manifestMemberLimit || len(m.ExternalSecrets) > manifestReferenceLimit {
		return invalid("manifest member or reference count outside bounds")
	}
	ids := map[string]Member{}
	paths := map[string]bool{}
	kinds := map[string]int{}
	var total int64
	for _, v := range m.Members {
		if !manifestText(v.ID) {
			return invalid("invalid member ID")
		}
		if _, ok := ids[v.ID]; ok {
			return invalid("duplicate or invalid member ID")
		}
		switch v.Kind {
		case "disk", "persistent-xml", "live-xml", "nvram", "tpm", "secret":
		default:
			return invalid("unknown recovery member kind")
		}
		if len(v.Path) > 1024 {
			return invalid("manifest member path exceeds length bound")
		}
		if e := importer.SafePath(v.Path); e != nil || strings.HasSuffix(v.Path, "/") {
			return invalid("unsafe or noncanonical manifest member path")
		}
		if paths[strings.ToLower(v.Path)] {
			return invalid("duplicate manifest path")
		}
		paths[strings.ToLower(v.Path)] = true
		if len(v.SHA256) != 64 {
			return invalid("invalid member digest length")
		}
		b, e := hex.DecodeString(v.SHA256)
		if e != nil || len(b) != 32 || strings.ToLower(v.SHA256) != v.SHA256 || v.Size <= 0 || v.Size > manifestMemberSizeLimit || v.Size > manifestTotalSizeLimit-total {
			return invalid("invalid member digest, size or aggregate size")
		}
		total += v.Size
		ids[v.ID] = v
		kinds[v.Kind]++
	}
	for name := range paths {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if paths[parent] {
				return invalid("manifest paths conflict as file and parent directory")
			}
		}
	}
	seen := map[string]bool{}
	for _, id := range m.Required {
		if !manifestText(id) || seen[id] {
			return invalid("duplicate or invalid required member")
		}
		seen[id] = true
		if _, ok := ids[id]; !ok {
			return domain.Fail("INCOMPLETE_BACKUP", "declared required member is missing")
		}
	}
	if len(ids) != len(seen) {
		return invalid("every manifest artifact must be declared in required inventory")
	}
	if kinds["disk"] == 0 || kinds["persistent-xml"] != 1 || kinds["live-xml"] > 1 {
		return domain.Fail("INCOMPLETE_BACKUP", "disks and exactly one persistent XML member required; at most one live XML member allowed")
	}
	if m.HasNVRAM && kinds["nvram"] != 1 || !m.HasNVRAM && kinds["nvram"] != 0 {
		return domain.Fail("INCOMPLETE_BACKUP", "NVRAM members contradict the requiresNVRAM declaration")
	}
	if m.HasTPM && kinds["tpm"] == 0 || !m.HasTPM && kinds["tpm"] != 0 {
		return domain.Fail("INCOMPLETE_BACKUP", "TPM members contradict the requiresTPM declaration")
	}
	secrets := map[string]bool{}
	for _, ref := range m.ExternalSecrets {
		if !manifestText(ref) || secrets[ref] {
			return invalid("invalid or duplicate external secret reference")
		}
		secrets[ref] = true
	}
	if len(m.ExternalSecrets) > 0 && m.IndependentlyRecoverable {
		return invalid("external secret references cannot establish independent recovery")
	}
	return nil
}

func manifestText(s string) bool {
	if s == "" || len(s) > 256 || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return false
		}
	}
	return true
}

// Verify checks declared member integrity using a noncanceling compatibility
// context. New service callers should use VerifyContext.
func (m Manifest) Verify(root string) error {
	return m.VerifyContext(context.Background(), root)
}

// VerifyContext checks every declared member. It does not prove that the declared
// list covers the VM, that a coordinated capture occurred or that recovery boots.
func (m Manifest) VerifyContext(ctx context.Context, root string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	return verifyManifestMembers(ctx, root, m.Members)
}

// ReadManifestContext reads one bounded, stable ordinary manifest file and
// validates strict JSON plus the unchanged legacy declaration contract.
func ReadManifestContext(ctx context.Context, filename string) (Manifest, error) {
	var m Manifest
	if err := ctx.Err(); err != nil {
		return m, err
	}
	b, err := readManifestDocument(ctx, filename)
	if err != nil {
		return m, err
	}
	if err := wire.Decode(b, &m); err != nil {
		return Manifest{}, domain.Fail("INVALID_INPUT", "invalid recovery manifest JSON; duplicate, unknown or malformed fields are not accepted")
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}
