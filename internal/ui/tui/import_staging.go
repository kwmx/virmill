package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// importStagingRoot is the private folder for prepared images when the user
// does not choose one: $XDG_DATA_HOME/virmill/imports, normally
// ~/.local/share/virmill/imports.
func importStagingRoot() string {
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(filepath.Clean(dir), "virmill", "imports")
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(filepath.Clean(home), ".local", "share", "virmill", "imports")
}

// defaultImportFolder names a new folder after the VM or source and the time,
// so each preparation gets its own folder and nothing is overwritten.
func defaultImportFolder(d ImportDraft) string {
	base := d.VMName
	if base == "" {
		base = strings.TrimSuffix(filepath.Base(d.Source), filepath.Ext(d.Source))
	}
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, base)
	name = strings.Trim(name, ".-")
	if len(name) > 48 {
		name = strings.Trim(name[:48], ".-")
	}
	if name == "" {
		name = "import"
	}
	return name + "-" + time.Now().Format("20060102-150405")
}

func importDestination(d ImportDraft) string {
	return filepath.Join(d.DestinationParent, d.DestinationName)
}

// ensureImportStaging creates only the default private folder, mode 0700, and
// only when an import is about to use it. A folder the user chose must exist.
func (m *Workspace) ensureImportStaging() error {
	if m.Import == nil || m.Import.StagingRoot == "" || m.Import.Draft.DestinationParent != m.Import.StagingRoot {
		return nil
	}
	if err := os.MkdirAll(m.Import.StagingRoot, 0o700); err != nil {
		return fmt.Errorf("Could not create the import folder %s: %v. Choose another folder with Back: Destination.", m.Import.StagingRoot, err)
	}
	return nil
}
