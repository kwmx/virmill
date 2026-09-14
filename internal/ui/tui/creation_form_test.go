package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

const creationFormOperation = "11111111-1111-4111-8111-111111111111"
const creationFormPool = "22222222-2222-4222-8222-222222222222"
const creationFormNetwork = "33333333-3333-4333-8333-333333333333"

func creationFormFixture() CreationForm {
	options := domain.CreationOptions{Architecture: "x86_64", Machines: []string{"pc-q35-test", "pc-i440fx-test"}, Machine: "pc-q35-test", MaxVCPUs: 32, HostMemoryMiB: 32768, CPUModes: []string{"host-model", "custom"}, CPUModels: []string{"test-model"}, Firmware: []domain.CreationFirmwareOption{{Label: "BIOS", Firmware: domain.CreationFirmware{Mode: "bios"}}, {Label: "UEFI", Firmware: domain.CreationFirmware{Mode: "uefi", Code: "/fixture/code.fd", Template: "/fixture/vars.fd", Format: "raw"}}}, DiskBuses: []string{"sata", "scsi", "virtio"}, Graphics: []string{"none", "vnc-unix"}}
	source := CreationSource{System: importer.System{Name: "appliance", Items: []importer.Item{{ResourceType: "3", Quantity: "4"}, {ResourceType: "4", MemoryMiB: 4096}, {ResourceType: "10"}, {ResourceType: "10"}}}, Disks: []CreationSourceDisk{{SourceID: "boot"}, {SourceID: "data"}}, Media: []CreationSourceMedia{{SourceID: "installer"}}}
	return NewCreationForm(creationFormOperation, source, options, []domain.StoragePool{{Key: domain.ResourceKey{UUID: creationFormPool}, Name: "VM storage", Type: "dir", Active: true}, {Key: domain.ResourceKey{UUID: "inactive"}, Name: "Inactive storage"}}, []domain.VirtualNetwork{{Key: domain.ResourceKey{UUID: creationFormNetwork}, Name: "Isolated lab", Active: true}, {Key: domain.ResourceKey{UUID: "inactive"}, Name: "Inactive network"}})
}
func creationFocus(t *testing.T, f CreationForm, id string) CreationForm {
	t.Helper()
	for i, c := range f.controls() {
		if c.id == id {
			f.Focus = i
			return f
		}
	}
	t.Fatalf("missing creation control %s onpage%d", id, f.Page)
	return f
}
func creationPress(f CreationForm, key tea.KeyType) (CreationForm, ImportIntent) {
	return f.Update(tea.KeyMsg{Type: key})
}
func creationComplete(f CreationForm) CreationForm {
	f.Spec.PoolID = creationFormPool
	f.Spec.Firmware = f.Options.Firmware[0].Firmware
	for i := range f.Spec.Disks {
		f.Spec.Disks[i].Bus = "sata"
	}
	for i := range f.Spec.Media {
		f.Spec.Media[i].Bus = "sata"
	}
	for i := range f.Spec.NICs {
		f.Spec.NICs[i].NetworkID = creationFormNetwork
		f.Spec.NICs[i].Model = "virtio"
	}
	return f
}

func TestCreationFormDetectedAndFallbackResources(t *testing.T) {
	f := creationFormFixture()
	if f.CPUText != "4" || f.MemoryText != "4096" || !strings.Contains(f.CPUOrigin, "Detected") || !strings.Contains(f.MemoryOrigin, "Detected") {
		t.Fatal("source hardware lost")
	}
	if f.Spec.Name != "appliance" || f.Spec.Firmware.Mode != "bios" || !strings.Contains(f.FirmwareOrigin, "Suggested") || f.Spec.PoolID != creationFormPool {
		t.Fatal("name missing, or labeled firmware suggestion / only usable pool not preselected")
	}
	empty := NewCreationForm(creationFormOperation, CreationSource{}, f.Options, nil, nil)
	if empty.CPUText != "2" || empty.MemoryText != "2048" || !strings.Contains(empty.CPUOrigin, "Suggested") || empty.Spec.Name != "new-vm" {
		t.Fatal("missing labeled fallback")
	}
	for _, items := range [][]importer.Item{{{ResourceType: "3", Quantity: "4"}, {ResourceType: "3", Quantity: "4"}, {ResourceType: "4", MemoryMiB: 1024}, {ResourceType: "4", MemoryMiB: 2048}}, {{ResourceType: "3", Quantity: "broken"}, {ResourceType: "4", Quantity: "4096", MemoryMiB: 0}}} {
		source := CreationSource{System: importer.System{Items: items}}
		q := NewCreationForm(creationFormOperation, source, f.Options, nil, nil)
		if q.CPUText != "" || q.MemoryText != "" {
			t.Fatal("ambiguous/invalid detection must require user entry")
		}
	}
}

func TestCreationFormNativeMappingsAndRequestSchema(t *testing.T) {
	f := creationFormFixture()
	f = creationFocus(t, f, "pool")
	f, _ = creationPress(f, tea.KeyRight)
	if f.Spec.PoolID != creationFormPool || !strings.Contains(f.View(80, 24), "VM storage") || strings.Contains(f.View(80, 24), "inactive") {
		t.Fatal("human pool choice mapping failed")
	}
	f.Page = 3
	f = creationFocus(t, f, "firmware")
	f, _ = creationPress(f, tea.KeyRight)
	if f.Spec.Firmware.Mode != "uefi" {
		t.Fatal("explicit firmware choice failed")
	}
	f, _ = creationPress(f, tea.KeyLeft)
	if f.Spec.Firmware.Mode != "bios" {
		t.Fatal("explicit firmware choice failed")
	}
	f.Page = 1
	for i := range f.Spec.Disks {
		f.Disk = i
		f = creationFocus(t, f, "bus")
		f, _ = creationPress(f, tea.KeyRight)
	}
	f.Disk = len(f.Spec.Disks)
	f = creationFocus(t, f, "bus")
	f, _ = creationPress(f, tea.KeyRight)
	f.Page = 2
	for i := range f.Spec.NICs {
		f.NIC = i
		f = creationFocus(t, f, "network")
		f, _ = creationPress(f, tea.KeyRight)
		f = creationFocus(t, f, "nicModel")
		f, _ = creationPress(f, tea.KeyRight)
		if f.Spec.NICs[i].SourceIndex != i || f.Spec.NICs[i].NetworkID != creationFormNetwork || f.Spec.NICs[i].Link != "down" {
			t.Fatal("original NIC mapping lost")
		}
	}
	f.Spec.UUID = "should-not-be-forwarded"
	r, err := f.Request("qemu:///system")
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != creationFormOperation || r.Action != "create" || r.Input["identityMode"] != "clone" {
		t.Fatal("creation action binding wrong")
	}
	hardware := r.Input["hardware"].(map[string]any)
	if _, present := hardware["uuid"]; present {
		t.Fatal("clone must omit UUID")
	}
	if hardware["vcpus"] != float64(4) || hardware["memoryMiB"] != float64(4096) {
		t.Fatal("CPU/RAM not emitted")
	}
	b, _ := json.Marshal(r.Input)
	if err := validation.Schema("vm-creation-input", b); err != nil {
		t.Fatal("shared schema rejects native form", err)
	}
}

func TestCreationFormOriginalNICsCannotDisappear(t *testing.T) {
	f := creationFormFixture()
	f.Page = 2
	for _, c := range f.controls() {
		if c.id == "removeNIC" {
			t.Fatal("original NIC removal was offered")
		}
	}
	f = creationFocus(t, f, "addNIC")
	f, _ = creationPress(f, tea.KeyEnter)
	if len(f.Spec.NICs) != 3 || f.Spec.NICs[2].SourceIndex != -1 || f.Spec.NICs[2].NetworkID != "" || f.Spec.NICs[2].Link != "down" {
		t.Fatal("new NIC defaults are unsafe")
	}
	f = creationFocus(t, f, "removeNIC")
	f, _ = creationPress(f, tea.KeyEnter)
	if len(f.Spec.NICs) != 2 {
		t.Fatal("new NIC could not be removed")
	}
	f = creationComplete(f)
	f.Spec.NICs = f.Spec.NICs[:1]
	if _, err := f.Request("qemu:///system"); err == nil || !strings.Contains(err.Error(), "original adapters") {
		t.Fatal("missing original NIC accepted", err)
	}
}

func TestCreationFormRejectsIncompleteAndUnsupportedChoices(t *testing.T) {
	tests := []struct {
		name, want string
		change     func(*CreationForm)
	}{
		{"CPU", "CPU", func(f *CreationForm) { f.CPUText = "bad" }},
		{"memory", "Memory", func(f *CreationForm) { f.MemoryText = "65536" }},
		{"pool", "Storage pool", func(f *CreationForm) { f.Spec.PoolID = "inactive" }},
		{"firmware", "firmware", func(f *CreationForm) { f.Spec.Firmware = domain.CreationFirmware{Mode: "uefi", Code: "/not-observed"} }},
		{"bus", "controller bus", func(f *CreationForm) { f.Spec.Disks[0].Bus = "not-supported" }},
		{"missing disk", "every prepared", func(f *CreationForm) { f.Spec.Disks = f.Spec.Disks[:1] }},
		{"boot", "boot priority", func(f *CreationForm) { f.Spec.Disks[0].BootOrder = f.Spec.Disks[1].BootOrder }},
		{"network", "active network", func(f *CreationForm) { f.Spec.NICs[0].NetworkID = "" }},
		{"mac", "new MAC", func(f *CreationForm) { f.Spec.NICs[0].MAC = "02:00:00:00:00:01" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := creationComplete(creationFormFixture())
			test.change(&f)
			if _, err := f.Request("qemu:///system"); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatal("missing actionable validation", err)
			}
		})
	}
}

func TestCreationFormMachineReloadPreservesFirmwareAndCPU(t *testing.T) {
	f := creationComplete(creationFormFixture())
	f.Page = 3
	f.Spec.CPU = domain.CreationCPU{Mode: "custom", Model: "test-model"}
	f.Spec.Firmware = f.Options.Firmware[1].Firmware
	f = creationFocus(t, f, "machine")
	f, intent := creationPress(f, tea.KeyRight)
	if intent.Kind != "reload" || f.Spec.Machine != "pc-i440fx-test" {
		t.Fatal("machine did not request refreshed options")
	}
	options := f.Options
	options.Machine = f.Spec.Machine
	options.Firmware = options.Firmware[:1]
	wanted := f.Spec.Firmware
	f.SetOptions(options)
	if f.Spec.Firmware != wanted || f.Spec.CPU.Model != "test-model" || f.Error == "" {
		t.Fatal("unsupported choices were silently replaced")
	}
}

func TestCreationFormEditingIntentsAndTerminalBounds(t *testing.T) {
	f := creationFormFixture()
	f = creationFocus(t, f, "cpu")
	f, _ = creationPress(f, tea.KeyCtrlU)
	// Guided input does not interpret command text or pasted terminal controls.
	f, _ = f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("8")})
	f, _ = creationPress(f, tea.KeyEnter)
	if f.Page != 0 {
		t.Fatal("text Enter submitted the form")
	}
	for page := 0; page < 4; page++ {
		f.Page = page
		for i := range f.controls() {
			f.Focus = i
			for _, size := range [][2]int{{80, 24}, {60, 18}, {80, 14}, {20, 4}, {0, 0}} {
				view := f.View(size[0], size[1])
				if size[0] == 0 {
					if view != "" {
						t.Fatal("zero-size rendering")
					}
					continue
				}
				if len(strings.Split(view, "\n")) > size[1] {
					t.Fatal("view too tall")
				}
				for _, line := range strings.Split(view, "\n") {
					if ansi.StringWidth(line) > size[0] || strings.ContainsRune(line, '\x1b') {
						t.Fatal("unsafe view", line)
					}
				}
			}
		}
	}
	f = creationComplete(f)
	f.CPUText = "4"
	f.Page = 2
	for _, id := range []string{"preview", "export"} {
		f = creationFocus(t, f, id)
		_, intent := creationPress(f, tea.KeyEnter)
		if intent.Kind != id {
			t.Fatal("wrong intent", intent)
		}
	}
	f.Page = 0
	_, intent := creationPress(f, tea.KeyEsc)
	if intent.Kind != "cancel" {
		t.Fatal("cannot cancel")
	}
}

func TestCreationFormSafeControllerDefaults(t *testing.T) {
	base := creationFormFixture()
	source := base.Source
	source.System.Items = append(source.System.Items, importer.Item{ResourceType: "20", InstanceID: "sata-controller"}, importer.Item{ResourceType: "17", Parent: "sata-controller", HostResources: []string{"ovf:/disk/boot"}})
	f := NewCreationForm(creationFormOperation, source, base.Options, base.Pools, base.Networks)
	if f.Spec.Disks[0].Bus != "sata" || f.Spec.Disks[1].Bus != "" {
		t.Fatal("only exact source controller mappings may be inferred")
	}
	source.System.Items = append(source.System.Items, importer.Item{ResourceType: "20", InstanceID: "sata-controller"})
	f = NewCreationForm(creationFormOperation, source, base.Options, base.Pools, base.Networks)
	if f.Spec.Disks[0].Bus != "" {
		t.Fatal("ambiguous controller was inferred")
	}
	source.Kind = "installation-media"
	f = NewCreationForm(creationFormOperation, source, base.Options, base.Pools, base.Networks)
	if f.Spec.Disks[0].Bus != "sata" || f.Spec.Media[0].Bus != "sata" {
		t.Fatal("supported ISO SATA defaults missing")
	}
	options := base.Options
	options.DiskBuses = []string{"virtio"}
	f = NewCreationForm(creationFormOperation, source, options, base.Pools, base.Networks)
	if f.Spec.Disks[0].Bus != "" || f.Spec.Media[0].Bus != "" {
		t.Fatal("unsupported SATA guessed")
	}
}

func TestCreationFormPreviewFocusesInvalidOption(t *testing.T) {
	tests := []struct {
		field  string
		page   int
		change func(*CreationForm)
	}{
		{"cpu", 0, func(f *CreationForm) { f.CPUText = "" }},
		{"pool", 0, func(f *CreationForm) { f.Spec.PoolID = "" }},
		{"firmware", 3, func(f *CreationForm) { f.Spec.Firmware = domain.CreationFirmware{} }},
		{"bus", 1, func(f *CreationForm) { f.Spec.Disks[1].Bus = "" }},
		{"network", 2, func(f *CreationForm) { f.Spec.NICs[1].NetworkID = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			f := creationComplete(creationFormFixture())
			tt.change(&f)
			f.Page = 2
			f = creationFocus(t, f, "preview")
			f, intent := creationPress(f, tea.KeyEnter)
			if intent.Kind != "" || f.Page != tt.page || f.controls()[f.Focus].id != tt.field || f.Error == "" {
				t.Fatalf("invalid option not focused: page%d field%s intent%+v error%s", f.Page, f.controls()[f.Focus].id, intent, f.Error)
			}
		})
	}
}

func TestCreationFormPoolKindsAndWrappedError(t *testing.T) {
	f := creationFormFixture()
	f.Pools = append(f.Pools, domain.StoragePool{Key: domain.ResourceKey{UUID: "block"}, Name: "Block storage", Type: "logical", Active: true})
	f = creationFocus(t, f, "pool")
	if len(f.controls()[f.Focus].choices) != 1 {
		t.Fatal("unsupported active pool type was offered")
	}
	f = creationComplete(f)
	f.Spec.PoolID = "block"
	if _, err := f.Request("qemu:///system"); err == nil {
		t.Fatal("unsupported active pool type accepted")
	}
	f.Error = strings.Repeat("Details ", 15) + "choose firmware again."
	view := f.View(80, 24)
	if !strings.Contains(view, "choose firmware again.") {
		t.Fatal("actionable error ending was truncated", view)
	}
	f.Pools = nil
	f = creationFocus(t, f, "pool")
	if !strings.Contains(f.controls()[f.Focus].help, "No storage pool yet") {
		t.Fatal("missing explanation for empty pool list")
	}
	if g, _ := creationPress(f, tea.KeyEnter); !strings.Contains(g.Error, "Create storage pool") {
		t.Fatal("empty pool choice does not point to inline creation", g.Error)
	}
	f = creationFocus(t, f, "create-pool")
	if _, intent := creationPress(f, tea.KeyEnter); intent.Kind != "create-pool" {
		t.Fatal("empty pool list lacks inline pool creation", intent)
	}
}

// Defaults are visible suggestions, never hidden: libvirt's default pool or
// the only usable pool, and the firmware a source declares or labeled BIOS.
func TestCreationFormSuggestedDefaults(t *testing.T) {
	base := creationFormFixture()
	pool := func(id, name string, active bool) domain.StoragePool {
		return domain.StoragePool{Key: domain.ResourceKey{UUID: id}, Name: name, Type: "dir", Active: active}
	}
	for _, tt := range []struct {
		pools []domain.StoragePool
		want  string
	}{
		{[]domain.StoragePool{pool("a", "one", true), pool("b", "two", true)}, ""},
		{[]domain.StoragePool{pool("a", "one", true), pool("b", "default", true)}, "b"},
		{[]domain.StoragePool{pool("a", "one", true), pool("b", "default", false)}, "a"},
		{nil, ""},
	} {
		if got := NewCreationForm(creationFormOperation, base.Source, base.Options, tt.pools, nil).Spec.PoolID; got != tt.want {
			t.Fatalf("pool default %q, want %q", got, tt.want)
		}
	}
	source := base.Source
	source.System.Firmware = "efi"
	f := NewCreationForm(creationFormOperation, source, base.Options, nil, nil)
	if f.Spec.Firmware.Mode != "uefi" || !strings.Contains(f.FirmwareOrigin, "Detected") {
		t.Fatal("declared UEFI firmware not followed", f.Spec.Firmware)
	}
	options := base.Options
	options.Firmware = options.Firmware[1:]
	if f = NewCreationForm(creationFormOperation, base.Source, options, nil, nil); f.Spec.Firmware.Mode != "" || f.FirmwareOrigin != "" {
		t.Fatal("firmware suggested without a BIOS option", f.Spec.Firmware)
	}
}

// The user's own images start on libvirt's default NAT network; appliances
// keep their mapped adapters, disconnected.
func TestCreationFormConnectsOwnImagesToDefaultNAT(t *testing.T) {
	base := creationFormFixture()
	nat := domain.VirtualNetwork{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "network", UUID: creationFormNetwork}, Name: "default", Active: true, PersistentXML: "<network><name>default</name><forward mode='nat'/></network>"}
	pools := []domain.StoragePool{{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "storage-pool", UUID: creationFormPool}, Name: "default", Type: "dir", Active: true}}
	boot := []CreationSourceDisk{{SourceID: "boot"}}
	want := domain.CreationNIC{ID: "nic1", SourceIndex: -1, NetworkID: creationFormNetwork, Model: "e1000e", Link: "up"}
	for _, source := range []CreationSource{{Kind: "PreparedDiskSet", Disks: boot}, {Kind: "PreparedInstallation", Disks: boot, Media: []CreationSourceMedia{{SourceID: "installer"}}}} {
		f := NewCreationForm(creationFormOperation, source, base.Options, pools, []domain.VirtualNetwork{nat})
		if len(f.Spec.NICs) != 1 || f.Spec.NICs[0] != want {
			t.Fatal("own image not connected to default NAT", source.Kind, f.Spec.NICs)
		}
		if _, err := f.Request("qemu:///system"); err != nil {
			t.Fatal("defaulted setup is not complete", source.Kind, err)
		}
		f.Page = 2
		f = creationFocus(t, f, "network")
		if !strings.Contains(f.controls()[f.Focus].help, "default NAT network") {
			t.Fatal("network suggestion not labeled")
		}
	}
	inactive, isolated, other := nat, nat, nat
	inactive.Active = false
	isolated.PersistentXML = "<network><name>default</name></network>"
	other.Name = "lab"
	for name, tt := range map[string]struct {
		source  CreationSource
		network domain.VirtualNetwork
	}{
		"appliance": {CreationSource{Kind: "PreparedImport", Disks: boot}, nat},
		"inactive":  {CreationSource{Kind: "PreparedDiskSet", Disks: boot}, inactive},
		"isolated":  {CreationSource{Kind: "PreparedDiskSet", Disks: boot}, isolated},
		"not-named": {CreationSource{Kind: "PreparedDiskSet", Disks: boot}, other},
	} {
		if f := NewCreationForm(creationFormOperation, tt.source, base.Options, pools, []domain.VirtualNetwork{tt.network}); len(f.Spec.NICs) != 0 || f.NetworkOrigin != "" {
			t.Fatal(name, "adapter added", f.Spec.NICs)
		}
	}
	if f := creationFormFixture(); len(f.Spec.NICs) != 2 || f.Spec.NICs[0].Link != "down" || f.Spec.NICs[0].NetworkID != "" {
		t.Fatal("appliance adapters changed", f.Spec.NICs)
	}
}

func TestCreationFormBeforePreparationAllowsOnlyUnboundValidDraft(t *testing.T) {
	f := creationComplete(creationFormFixture())
	f.OperationID = ""
	if _, err := f.Request("qemu:///system"); err == nil {
		t.Fatal("normal creation requires a completed operation")
	}
	f.BeforePreparation = true
	request, err := f.Request("qemu:///system")
	if err != nil || request.ID != "" {
		t.Fatal("unbound draft must not invent an operation identity", request.ID, err)
	}
	hardware := request.Input["hardware"].(map[string]any)
	if _, present := hardware["uuid"]; present {
		t.Fatal("unbound draft invented VM identity")
	}
	if hardware["name"] != f.Spec.Name || request.Input["identityMode"] != "clone" {
		t.Fatal("unbound hardware declaration changed")
	}
	f.Page = 2
	view := f.View(80, 24)
	if !strings.Contains(view, "Review import") || !strings.Contains(view, "Choose VM hardware before preparing the images.") || strings.Contains(view, "Preview VM creation") {
		t.Fatal("preparation boundary is unclear", view)
	}
	f = creationFocus(t, f, "preview")
	_, intent := creationPress(f, tea.KeyEnter)
	if intent.Kind != "preview" {
		t.Fatal("valid draft did not return its review intent")
	}
	f.OperationID = "not-a-real-operation"
	if _, err := f.Request("qemu:///system"); err == nil {
		t.Fatal("invalid supplied identity must not be ignored")
	}
	f.OperationID = ""
	f.Spec.Firmware = domain.CreationFirmware{}
	if _, err := f.Request("qemu:///system"); err == nil || !strings.Contains(err.Error(), "firmware") {
		t.Fatal("unbound draft bypassed hardware validation", err)
	}
	f = creationFocus(t, f, "preview")
	f, intent = creationPress(f, tea.KeyEnter)
	if intent.Kind != "" || f.Page != 3 || f.controls()[f.Focus].id != "firmware" {
		t.Fatal("invalid pre-preparation draft was submitted")
	}
}
