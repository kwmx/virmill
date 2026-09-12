package tui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/domain"
)

func creationBootFixture(media int) CreationForm {
	base := creationFormFixture()
	source := base.Source
	source.Kind = "iso"
	source.Disks = []CreationSourceDisk{{SourceID: "boot"}}
	source.Media = []CreationSourceMedia{{SourceID: "installer"}}
	if media == 2 {
		source.Media = append(source.Media, CreationSourceMedia{SourceID: "tools"})
	}
	f := creationComplete(NewCreationForm(creationFormOperation, source, base.Options, base.Pools, base.Networks))
	f.CPUText, f.MemoryText, f.Page = "6", "3072", 1
	f.Spec.Disks[0].Bus = "scsi"
	f.Spec.NICs[0].Link = "up"
	return f
}

func creationBootKey(t *testing.T, f CreationForm, device int, key tea.KeyType) CreationForm {
	t.Helper()
	f.Disk = device
	f = creationFocus(t, f, "boot")
	next, intent := f.Update(tea.KeyMsg{Type: key})
	if intent.Kind != "" {
		t.Fatal("boot priority edit left the form or requested a service", intent)
	}
	return next
}

func creationBootRetained(t *testing.T, before, after CreationForm) {
	t.Helper()
	withoutOrder := func(spec domain.CreationSpec) domain.CreationSpec {
		raw, _ := json.Marshal(spec)
		var out domain.CreationSpec
		_ = json.Unmarshal(raw, &out)
		for i := range out.Disks {
			out.Disks[i].BootOrder = 0
		}
		for i := range out.Media {
			out.Media[i].BootOrder = 0
		}
		return out
	}
	if !reflect.DeepEqual(withoutOrder(before.Spec), withoutOrder(after.Spec)) || !reflect.DeepEqual(before.Source, after.Source) || !reflect.DeepEqual(before.Options, after.Options) || !reflect.DeepEqual(before.Pools, after.Pools) || !reflect.DeepEqual(before.Networks, after.Networks) || before.OperationID != after.OperationID || before.CPUText != after.CPUText || before.MemoryText != after.MemoryText {
		t.Fatal("boot edit changed source identity, buses, NICs, firmware or unrelated settings")
	}
}

func creationBootValid(t *testing.T, f CreationForm, orders ...int) {
	t.Helper()
	actual := []int{}
	for _, d := range f.Spec.Disks {
		actual = append(actual, d.BootOrder)
	}
	for _, m := range f.Spec.Media {
		actual = append(actual, m.BootOrder)
	}
	if !reflect.DeepEqual(actual, orders) {
		t.Fatal("unexpected disk/media boot order", actual, orders)
	}
	if _, err := f.Request("qemu:///system"); err != nil {
		t.Fatal("edited boot sequence cannot preview", err)
	}
}

func TestCreationBootOrderInstallerAttachOnlyCompactsAndRenders(t *testing.T) {
	f := creationBootFixture(1)
	before := f
	creationBootValid(t, f, 2, 1)
	f = creationBootKey(t, f, 1, tea.KeyLeft)
	creationBootValid(t, f, 1, 0)
	creationBootRetained(t, before, f)
	view := f.View(80, 24)
	for _, want := range []string{"Boot: 1. boot", "Attach only", "Boot priority"} {
		if !strings.Contains(view, want) {
			t.Fatalf("80x24 boot display omits %q: %s", want, view)
		}
	}
	if strings.Contains(view, "2. installer") {
		t.Fatal("attached-only installer still shown in boot sequence", view)
	}
	if len(strings.Split(view, "\n")) > 24 {
		t.Fatal("boot display exceeds terminal rows")
	}
	for _, line := range strings.Split(view, "\n") {
		if ansi.StringWidth(line) > 80 {
			t.Fatal("boot display exceeds terminal width", line)
		}
	}
}

func TestCreationBootOrderMovingDiskFirstShiftsInstaller(t *testing.T) {
	f := creationBootFixture(1)
	before := f
	f = creationBootKey(t, f, 0, tea.KeyLeft)
	creationBootValid(t, f, 1, 2)
	creationBootRetained(t, before, f)
	view := f.View(80, 24)
	if !strings.Contains(view, "Boot: 1. boot") || !strings.Contains(view, "2. installer") {
		t.Fatal("ordered summary does not explain resulting disk-first sequence", view)
	}
	// Moving the disk back is a reorder, never a duplicate priority.
	f = creationBootKey(t, f, 0, tea.KeyRight)
	creationBootValid(t, f, 2, 1)
	creationBootRetained(t, before, f)
}

func TestCreationBootOrderMultipleMediaDisableReenableAndReorder(t *testing.T) {
	f := creationBootFixture(2)
	before := f
	creationBootValid(t, f, 3, 1, 2)
	f = creationBootKey(t, f, 1, tea.KeyLeft)
	creationBootValid(t, f, 2, 0, 1)
	f = creationBootKey(t, f, 2, tea.KeyLeft)
	creationBootValid(t, f, 1, 0, 0)
	f = creationBootKey(t, f, 1, tea.KeyRight)
	creationBootValid(t, f, 2, 1, 0)
	f = creationBootKey(t, f, 2, tea.KeyRight)
	creationBootValid(t, f, 3, 2, 1)
	f = creationBootKey(t, f, 1, tea.KeyRight)
	creationBootValid(t, f, 2, 3, 1)
	creationBootRetained(t, before, f)
	view := f.View(80, 24)
	for _, want := range []string{"1. tools", "2. boot", "3. installer"} {
		if !strings.Contains(view, want) {
			t.Fatal("multi-media summary does not match final order", view)
		}
	}
}

func TestCreationBootOrderInvalidRestoredChoicesRemainExplicitUntilEdited(t *testing.T) {
	for _, fault := range []string{"gap", "duplicate"} {
		t.Run(fault, func(t *testing.T) {
			original := creationBootFixture(1)
			if fault == "gap" {
				original.Spec.Media[0].BootOrder = 0
			} else {
				original.Spec.Disks[0].BootOrder = 1
			}
			saved := saveCreationValues(original)
			restored := creationBootFixture(1)
			saved.restore(&restored)
			if !reflect.DeepEqual(restored.Spec, original.Spec) {
				t.Fatal("loading silently rewrote invalid boot order")
			}
			if _, err := restored.Request("qemu:///system"); err == nil {
				t.Fatal("invalid restored sequence accepted")
			}
			if !strings.Contains(restored.View(80, 24), "Boot order needs review") {
				t.Fatal("invalid restored configuration is hidden", restored.View(80, 24))
			}
			restored, _ = restored.Update(tea.KeyMsg{Type: tea.KeyTab})
			if !reflect.DeepEqual(restored.Spec, original.Spec) {
				t.Fatal("navigation silently normalized invalid boot order")
			}
			if fault == "gap" {
				restored = creationBootKey(t, restored, 0, tea.KeyLeft)
			} else {
				restored = creationBootKey(t, restored, 1, tea.KeyLeft)
			}
			creationBootValid(t, restored, 1, 0)
			creationBootRetained(t, original, restored)
		})
	}
}

func TestCreationBootOrderPreparedInstallationSuggestsSupportedSATAPreservingRestoredBus(t *testing.T) {
	base := creationFormFixture()
	source := CreationSource{Kind: "PreparedInstallation", Disks: []CreationSourceDisk{{SourceID: "install-disk"}}, Media: []CreationSourceMedia{{SourceID: "install-iso"}}}
	f := NewCreationForm(creationFormOperation, source, base.Options, base.Pools, base.Networks)
	if f.Spec.Disks[0].Bus != "sata" || f.Spec.Media[0].Bus != "sata" {
		t.Fatal("prepared installation omitted supported SATA suggestion")
	}
	f.Page = 1
	f = creationFocus(t, f, "bus")
	if !strings.Contains(f.View(80, 24), "Suggested: SATA for installation media") {
		t.Fatal("SATA suggestion is not explained", f.View(80, 24))
	}
	options := base.Options
	options.DiskBuses = []string{"scsi", "virtio"}
	unsupported := NewCreationForm(creationFormOperation, source, options, base.Pools, base.Networks)
	if unsupported.Spec.Disks[0].Bus != "" || unsupported.Spec.Media[0].Bus != "" {
		t.Fatal("unavailable SATA silently substituted another controller")
	}
	for _, kind := range []string{"PreparedAppliance", "PreparedDisks"} {
		source.Kind = kind
		other := NewCreationForm(creationFormOperation, source, base.Options, base.Pools, base.Networks)
		if other.Spec.Disks[0].Bus != "" || other.Spec.Media[0].Bus != "" {
			t.Fatal("installation suggestion leaked to an undetected source", kind)
		}
	}
	f.Spec.Disks[0].Bus, f.Spec.Media[0].Bus = "virtio", "scsi"
	saved := saveCreationValues(f)
	resumed := NewCreationForm(creationFormOperation, f.Source, base.Options, base.Pools, base.Networks)
	saved.restore(&resumed)
	resumed.SetOptions(base.Options)
	if resumed.Spec.Disks[0].Bus != "virtio" || resumed.Spec.Media[0].Bus != "scsi" {
		t.Fatal("reload replaced explicitly restored controller choices")
	}
}
