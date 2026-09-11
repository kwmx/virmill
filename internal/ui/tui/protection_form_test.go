package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

const protectionCapture = "159fcd09-f5e4-4e1a-a195-f18d8e146862"
const protectionPool = "279a3bae-a453-4eb2-a86b-8cde21325651"

func protectionSet(f *ProtectionForm, name, value string) {
	for i := range f.Fields {
		if f.Fields[i].Name == name {
			f.Fields[i].Value = value
			f.Fields[i].Cursor = len([]rune(value))
		}
	}
}
func protectionTestPool() domain.StoragePool {
	return domain.StoragePool{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "storage-pool", UUID: protectionPool}, Name: "VM disks", Type: "dir", State: "running", Active: true}
}

func TestProtectionBackupPlanUsesReferencesOnly(t *testing.T) {
	f, err := NewProtectionForm("backup-create", protectionCapture)
	if err != nil {
		t.Fatal(err)
	}
	protectionSet(&f, "repository", "/media/backups/virmill")
	protectionSet(&f, "passwordFile", "/home/operator/private/restic-password")
	method, r, err := f.Request("qemu:///system")
	if err != nil || method != "backup.create" || r.ID != protectionCapture || r.Action != "create" || r.Path != "" || r.Apply != nil || len(r.Input) != 2 {
		t.Fatalf("bad preview: %s %+v %v", method, r, err)
	}
	if r.Input["passwordFile"] != "/home/operator/private/restic-password" {
		t.Fatal(r.Input)
	}
	for _, value := range []string{"secret text", "/media/backups/virmill/password", "/media/backups/virmill", "/tmp/../password", "/tmp/pass\nword"} {
		protectionSet(&f, "passwordFile", value)
		if _, _, err := f.Request("qemu:///system"); err == nil {
			t.Fatalf("accepted invalid credential reference %q", value)
		}
	}
}

func TestProtectionRestoreUsesObservedPoolAndExactSchema(t *testing.T) {
	f, err := NewProtectionForm("snapshot-restore", protectionCapture)
	if err != nil {
		t.Fatal(err)
	}
	pool := protectionTestPool()
	f.SetPools([]domain.StoragePool{pool})
	protectionSet(&f, "name", "Recovered workstation")
	method, r, err := f.Request("qemu:///system")
	if err != nil || method != "snapshot.restore" || r.Action != "restore" || r.Apply != nil || len(r.Input) != 2 || r.Input["poolID"] != protectionPool {
		t.Fatalf("bad preview %s %+v %v", method, r, err)
	}
	raw, _ := json.Marshal(r.Input)
	if err := validation.Schema("cold-restore-input", raw); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.Request("qemu:///session"); err == nil {
		t.Fatal("accepted pool from different connection")
	}
	for _, name := range []string{"", "   ", "../other", "unsafe\x1b[31m", strings.Repeat("a", 129)} {
		protectionSet(&f, "name", name)
		if _, _, err := f.Request("qemu:///system"); err == nil {
			t.Fatalf("accepted name %q", name)
		}
	}
}

func TestProtectionUnavailablePoolCannotBeTypedAround(t *testing.T) {
	f, _ := NewProtectionForm("snapshot-restore", protectionCapture)
	pool := protectionTestPool()
	pool.Active = false
	f.SetPools([]domain.StoragePool{pool})
	protectionSet(&f, "name", "Restored VM")
	f.Focus = 1
	f, preview, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(protectionPool)})
	if preview || f.Fields[1].Value != "" || f.Error == "" {
		t.Fatalf("unavailable pool input %+v", f)
	}
	protectionSet(&f, "poolID", protectionPool)
	if _, _, err := f.Request("qemu:///system"); err == nil {
		t.Fatal("invented pool accepted")
	}
	pool.Active = true
	pool.Type = "logical"
	f.SetPools([]domain.StoragePool{pool})
	if len(f.Pools) != 0 {
		t.Fatal("unsupported pool offered")
	}
}

func TestProtectionKeyboardReviewAndBackPreserveInputs(t *testing.T) {
	f, _ := NewProtectionForm("backup-create", protectionCapture)
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/media/backups/repo")})
	f, preview, back := f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if preview || back || f.Focus != 1 {
		t.Fatal("field enter did not advance")
	}
	f, _, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/home/operator/password")})
	f, preview, _ = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if preview || f.Focus != 2 {
		t.Fatal("missing explicit preview button")
	}
	before := f.Fields[1].Value
	f, preview, back = f.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !preview || back {
		t.Fatalf("preview not emitted: %s", f.Error)
	}
	f, preview, back = f.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if preview || !back || f.Fields[1].Value != before {
		t.Fatal("back lost inputs")
	}
}

func TestProtectionRenderingBoundsAndSafety(t *testing.T) {
	f, _ := NewProtectionForm("snapshot-restore", protectionCapture)
	pool := protectionTestPool()
	pool.Name = "pool\x1b[2J\nspoofed"
	f.SetPools([]domain.StoragePool{pool})
	f.Error = strings.Repeat("Choose a pool. ", 12)
	for _, size := range [][2]int{{80, 18}, {120, 30}, {40, 10}, {24, 5}, {1, 1}} {
		text := f.View(size[0], size[1])
		lines := strings.Split(text, "\n")
		if len(lines) > size[1] {
			t.Fatalf("height %v: %q", size, text)
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] || strings.Contains(line, "\x1b") {
				t.Fatalf("unsafe or wide line %v: %q", size, line)
			}
		}
	}
	view := f.View(80, 24)
	for _, want := range []string{"stopped VM", "networking disconnected", "Firmware/TPM restore is unavailable", "Preview restore"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing limitation/control %q: %s", want, view)
		}
	}
}

func TestProtectionSelectionRequired(t *testing.T) {
	for _, id := range []string{"", "display name", "00000000-0000-0000-0000-000000000000"} {
		if _, err := NewProtectionForm("backup-create", id); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
	if _, err := NewProtectionForm("delete", protectionCapture); err == nil {
		t.Fatal("accepted unsupported action")
	}
}
