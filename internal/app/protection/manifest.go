// Package protection models completeness independently of the application database.
package protection

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"virmill.local/core/internal/app/importer"
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
	if m.APIVersion != "virmill/v1" || m.ID == "" || m.VMID == "" {
		return errors.New("invalid recovery manifest identity")
	}
	if m.Mode != "cold" && m.Mode != "live-disk" {
		return errors.New("unknown capture mode")
	}
	if m.Mode != "cold" && (m.HasTPM || m.HasNVRAM) && m.IndependentlyRecoverable {
		return errors.New("live auxiliary state cannot be claimed complete without certified capture")
	}
	ids := map[string]Member{}
	paths := map[string]bool{}
	kinds := map[string]int{}
	for _, v := range m.Members {
		if _, ok := ids[v.ID]; ok || v.ID == "" {
			return errors.New("duplicate/empty member ID")
		}
		if e := importer.SafePath(v.Path); e != nil {
			return e
		}
		if paths[strings.ToLower(v.Path)] {
			return errors.New("duplicate manifest path")
		}
		paths[strings.ToLower(v.Path)] = true
		b, e := hex.DecodeString(v.SHA256)
		if e != nil || len(b) != 32 || v.Size < 0 {
			return errors.New("invalid member digest/size")
		}
		ids[v.ID] = v
		kinds[v.Kind]++
	}
	if len(m.Required) == 0 {
		return errors.New("required inventory is absent")
	}
	seen := map[string]bool{}
	for _, id := range m.Required {
		if seen[id] {
			return errors.New("duplicate required member")
		}
		seen[id] = true
		if _, ok := ids[id]; !ok {
			return fmt.Errorf("INCOMPLETE_BACKUP: missing %s", id)
		}
	}
	if len(ids) != len(seen) {
		return errors.New("every manifest artifact must be declared in required inventory")
	}
	if kinds["disk"] == 0 || kinds["persistent-xml"] != 1 {
		return errors.New("INCOMPLETE_BACKUP: disks and persistent XML required")
	}
	if m.HasNVRAM && kinds["nvram"] != 1 {
		return errors.New("INCOMPLETE_BACKUP: required NVRAM missing")
	}
	if m.HasTPM && kinds["tpm"] == 0 {
		return errors.New("INCOMPLETE_BACKUP: required TPM state missing")
	}
	if len(m.ExternalSecrets) > 0 && m.IndependentlyRecoverable {
		return errors.New("external secret references cannot establish independent recovery")
	}
	return nil
}
func (m Manifest) Verify(root string) error {
	if e := m.Validate(); e != nil {
		return e
	}
	r, e := os.OpenRoot(root)
	if e != nil {
		return e
	}
	defer r.Close()
	for _, v := range m.Members {
		st, e := r.Lstat(filepath.FromSlash(v.Path))
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			return errors.New("backup member is not regular file")
		}
		f, e := r.Open(filepath.FromSlash(v.Path))
		if e != nil {
			return e
		}
		actual, e := f.Stat()
		if e != nil || !os.SameFile(st, actual) {
			f.Close()
			return errors.New("member changed during open")
		}
		h := sha256.New()
		n, e := io.Copy(h, io.LimitReader(f, v.Size+1))
		f.Close()
		if e != nil {
			return e
		}
		if n != v.Size || hex.EncodeToString(h.Sum(nil)) != v.SHA256 {
			return fmt.Errorf("INCOMPLETE_BACKUP: integrity mismatch %s", v.ID)
		}
	}
	return nil
}
