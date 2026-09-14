// Package poolxml renders and checks the one storage pool shape Virmill
// creates: a persistent directory-backed pool with a fixed name, UUID and
// target folder. It never parses or rewrites other pools for mutation.
package poolxml

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"virmill.local/core/internal/domain"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// A pool lists every file in its folder and allocates new ones there, so
// system folders are refused outright.
var reserved = []string{"/bin", "/boot", "/dev", "/etc", "/lib", "/lib32", "/lib64", "/proc", "/run", "/sbin", "/sys", "/usr", "/var/run"}

// ValidateName accepts names libvirt and shells handle without quoting.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("pool name must be 1-64 letters, digits, dots, dashes or underscores and start with a letter or digit")
	}
	return nil
}

// ValidatePath accepts a canonical absolute folder below a top-level directory.
func ValidatePath(path string) error {
	if path == "" || len(path) > 1024 || !utf8.ValidString(path) || strings.ContainsFunc(path, unicode.IsControl) {
		return fmt.Errorf("storage pool folder must be a readable absolute path")
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("storage pool folder must be an absolute path without . or .. parts or a trailing slash")
	}
	if strings.Count(path, "/") < 2 {
		return fmt.Errorf("choose a folder below a top-level directory, for example /srv/vms")
	}
	for _, r := range reserved {
		if path == r || strings.HasPrefix(path, r+"/") {
			return fmt.Errorf("%s is a system folder; choose another folder for VM disks", r)
		}
	}
	return nil
}

func Validate(d domain.StoragePoolDefinition) error {
	if !uuidPattern.MatchString(d.UUID) || d.UUID == "00000000-0000-0000-0000-000000000000" {
		return fmt.Errorf("storage pool UUID must be a canonical lowercase UUID")
	}
	if err := ValidateName(d.Name); err != nil {
		return err
	}
	return ValidatePath(d.Path)
}

// Render omits permissions: libvirt creates a missing folder with its default
// mode and leaves an existing folder's owner, mode, label and files unchanged.
func Render(d domain.StoragePoolDefinition) (string, error) {
	if err := Validate(d); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("<pool type='dir'>\n  <name>")
	_ = xml.EscapeText(&b, []byte(d.Name))
	b.WriteString("</name>\n  <uuid>" + d.UUID + "</uuid>\n  <target>\n    <path>")
	_ = xml.EscapeText(&b, []byte(d.Path))
	b.WriteString("</path>\n  </target>\n</pool>\n")
	return b.String(), nil
}

// Observed is the identity of any pool type, used for collision checks.
type Observed struct {
	Type, Name, UUID, Path string
	Source                 bool
}

func Parse(raw string) (Observed, error) {
	var v struct {
		XMLName xml.Name `xml:"pool"`
		Type    string   `xml:"type,attr"`
		Name    string   `xml:"name"`
		UUID    string   `xml:"uuid"`
		Source  *struct {
			Inner string `xml:",innerxml"`
		} `xml:"source"`
		Target struct {
			Path string `xml:"path"`
		} `xml:"target"`
	}
	if err := xml.Unmarshal([]byte(raw), &v); err != nil {
		return Observed{}, fmt.Errorf("unreadable storage pool XML")
	}
	out := Observed{Type: v.Type, Name: v.Name, UUID: v.UUID, Path: v.Target.Path}
	out.Source = v.Source != nil && strings.TrimSpace(v.Source.Inner) != ""
	return out, nil
}

// Match reports whether native XML is exactly the reviewed directory pool.
func Match(raw string, d domain.StoragePoolDefinition) error {
	o, err := Parse(raw)
	if err != nil {
		return err
	}
	if o.Type != "dir" || o.Name != d.Name || o.UUID != d.UUID || o.Path != d.Path || o.Source {
		return fmt.Errorf("storage pool differs from the reviewed directory pool")
	}
	return nil
}

// Overlaps reports equal folders or one folder inside the other. Such pools
// would list and allocate each other's files.
func Overlaps(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	return a == b || strings.HasPrefix(b, strings.TrimSuffix(a, "/")+"/") || strings.HasPrefix(a, strings.TrimSuffix(b, "/")+"/")
}
