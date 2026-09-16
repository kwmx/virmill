package tui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

type CreationSourceDisk struct {
	SourceID   string `json:"sourceID"`
	SourcePath string `json:"sourcePath,omitempty"`
}

// CreationSourceFile is an original source file and its recorded digest.
type CreationSourceFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type CreationSourceMedia struct {
	SourceID string `json:"sourceID"`
}
type CreationSource struct {
	Kind   string                `json:"kind"`
	System importer.System       `json:"system"`
	Disks  []CreationSourceDisk  `json:"disks"`
	Media  []CreationSourceMedia `json:"media"`
	// SourceFiles binds a cloud image's declared digest (ADR 0059).
	SourceFiles []CreationSourceFile `json:"sourceFiles,omitempty"`
}

// CreationForm collects a clone declaration; only the workspace may request a
// reviewed service plan. It never defines a VM, changes networks or opens media.
type CreationForm struct {
	OperationID string
	// BeforePreparation permits an unbound hardware draft. The workspace must
	// bind a successful preparation operation before requesting VM creation.
	BeforePreparation       bool
	Source                  CreationSource
	Spec                    domain.CreationSpec
	Options                 domain.CreationOptions
	Pools                   []domain.StoragePool
	Networks                []domain.VirtualNetwork
	CPUText, MemoryText     string
	CPUOrigin, MemoryOrigin string
	FirmwareOrigin          string
	NameOrigin              string
	NetworkOrigin           string
	SuggestedNetworkID      string
	StartAfter              bool // start the VM once creation succeeds (ADR 0057)
	RemovePrepared          bool // remove the prepared copy afterwards (ADR 0058)
	CloudEnabled            bool // create the user with cloud-init (ADR 0059)
	CloudDecided            bool
	Cloud                   cloudSetup
	CloudOrigin             string
	Page, Focus, Disk, NIC  int
	Error                   string
	cursor                  int
	cursorField             string
}

func NewCreationForm(operationID string, source CreationSource, options domain.CreationOptions, pools []domain.StoragePool, networks []domain.VirtualNetwork) CreationForm {
	f := CreationForm{OperationID: operationID, Source: source, Options: options, Pools: slices.Clone(pools), Networks: slices.Clone(networks)}
	f.Spec = domain.CreationSpec{Name: source.System.Name, Architecture: options.Architecture, Machine: options.Machine, Clock: "utc", Disks: []domain.CreationDisk{}, NICs: []domain.CreationNIC{}}
	if f.Spec.Name == "" {
		f.Spec.Name = source.System.ID
	}
	if f.Spec.Name == "" {
		f.Spec.Name = "new-vm"
	}
	f.CPUText, f.CPUOrigin = creationSourceValue(source.System.Items, "3", "2")
	f.MemoryText, f.MemoryOrigin = creationSourceValue(source.System.Items, "4", "2048")
	if slices.Contains(options.CPUModes, "host-model") {
		f.Spec.CPU.Mode = "host-model"
	} else if len(options.CPUModes) > 0 {
		f.Spec.CPU.Mode = options.CPUModes[0]
	}
	if slices.Contains(options.Graphics, "spice-unix") {
		f.Spec.Graphics = "spice-unix"
	} else if slices.Contains(options.Graphics, "vnc-unix") {
		f.Spec.Graphics = "vnc-unix"
	} else if len(options.Graphics) > 0 {
		f.Spec.Graphics = options.Graphics[0]
	}
	for i, d := range source.Disks {
		f.Spec.Disks = append(f.Spec.Disks, domain.CreationDisk{SourceID: d.SourceID, Bus: creationSourceBus(source, d.SourceID, options), BootOrder: i + 1 + len(source.Media)})
	}
	for i, m := range source.Media {
		f.Spec.Media = append(f.Spec.Media, domain.CreationMedia{SourceID: m.SourceID, Bus: creationSourceBus(source, "", options), BootOrder: i + 1})
	}
	for _, item := range source.System.Items {
		if item.ResourceType == "10" {
			i := len(f.Spec.NICs)
			f.Spec.NICs = append(f.Spec.NICs, domain.CreationNIC{ID: fmt.Sprintf("nic%d", i+1), SourceIndex: i, Link: "down"})
		}
	}
	f.addDefaultNIC()
	f.StartAfter, f.RemovePrepared = true, true
	f.Spec.DevicePolicy, _ = domain.DefaultCreationDevices(f.Spec.Machine)
	f.Spec.PoolID = defaultCreationPool(f.Pools)
	f.Spec.Firmware, f.FirmwareOrigin = defaultCreationFirmware(options, source.System.Firmware)
	return f
}

func creationSourceValue(items []importer.Item, kind, fallback string) (string, string) {
	matches := []importer.Item{}
	for _, item := range items {
		if item.ResourceType == kind {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		return fallback, "Suggested: source did not specify this value."
	}
	if len(matches) != 1 {
		return "", "Source has conflicting values; enter the intended value."
	}
	item := matches[0]
	if kind == "4" {
		if item.MemoryMiB < 128 || item.MemoryMiB > 1<<20 {
			return "", "Source memory is not usable; enter memory in MiB."
		}
		return strconv.FormatInt(item.MemoryMiB, 10), "Detected from the source appliance."
	}
	n, err := strconv.ParseUint(item.Quantity, 10, 32)
	if err != nil || n < 1 || n > 512 {
		return "", "Source CPU count is not usable; enter a CPU count."
	}
	return strconv.FormatUint(n, 10), "Detected from the source appliance."
}

// SetOptions preserves user choices after a machine refresh. Unsupported choices
// stay visible as errors instead of silently changing CPU or firmware semantics.
func (f *CreationForm) SetOptions(options domain.CreationOptions) {
	f.Options = options
	if f.Spec.Machine == "" {
		f.Spec.Machine = options.Machine
	}
	if f.Spec.Architecture == "" {
		f.Spec.Architecture = options.Architecture
	}
	if f.Spec.Machine != options.Machine || !slices.Contains(options.Machines, f.Spec.Machine) {
		f.Error = "Choose a supported machine and refresh its options."
		return
	}
	if !slices.Contains(options.CPUModes, f.Spec.CPU.Mode) || (f.Spec.CPU.Mode == "custom" && f.Spec.CPU.Model != "" && !slices.Contains(options.CPUModels, f.Spec.CPU.Model)) {
		f.Error = "This machine does not support the selected CPU. Choose another CPU option."
		return
	}
	if f.Spec.Firmware.Mode != "" && f.firmwareIndex() < 0 {
		f.Error = "This machine does not offer the selected firmware. Choose firmware again."
		return
	}
	f.Error = ""
}

func creationFormChoice(id, label, help, value string, choices []string) importControl {
	if value == "" {
		value = "Choose…"
	}
	return importControl{id: id, label: label, help: help, kind: "choice", value: value, choices: choices}
}
func (f CreationForm) firmwareIndex() int {
	return slices.IndexFunc(f.Options.Firmware, func(o domain.CreationFirmwareOption) bool { return o.Firmware == f.Spec.Firmware })
}
func (f CreationForm) controls() []importControl {
	var c []importControl
	choice := func(id, label, help, value string, choices []string) {
		c = append(c, creationFormChoice(id, label, help, value, choices))
	}
	switch f.Page {
	case 0:
		nameHelp := "Choose a name not already used on this connection."
		if f.NameOrigin != "" {
			nameHelp = f.NameOrigin
		}
		c = append(c, importText("name", "VM name", nameHelp, f.Spec.Name), importText("cpu", "CPU cores", f.CPUOrigin, f.CPUText), importText("memory", "Memory (MiB)", f.MemoryOrigin, f.MemoryText))
		pools := []string{}
		poolName := ""
		for _, p := range f.Pools {
			if creationUsablePool(p) {
				pools = append(pools, p.Key.UUID)
				if p.Key.UUID == f.Spec.PoolID {
					poolName = p.Name
				}
			}
		}
		poolHelp := "Where copies of the VM's disks are stored."
		stopped, hasStopped := f.startablePool()
		if len(pools) == 0 {
			poolHelp = "No storage pool yet. Choose Create storage pool below; your settings stay here."
			if hasStopped {
				poolHelp = "Your storage pools are stopped. Start one below, or create libvirt's standard pool."
			}
		}
		choice("pool", "Storage pool", poolHelp, poolName, pools)
		if len(pools) == 0 {
			if hasStopped {
				c = append(c, importButton("start-pool", "Start pool "+validation.SafeText(stopped.Name), "Starts this existing pool after one short review; your settings stay here."))
			}
			c = append(c, importButton("create-pool", "Create storage pool", "Sets up libvirt's standard folder for VM disks after one short review."))
		}
		firmware := ""
		if i := f.firmwareIndex(); i >= 0 {
			firmware = f.Options.Firmware[i].Label
		}
		labels := []string{}
		for _, option := range f.Options.Firmware {
			labels = append(labels, option.Label)
		}
		firmwareHelp := "Match the original BIOS or UEFI. This is required for the guest to boot."
		if f.FirmwareOrigin != "" {
			firmwareHelp = f.FirmwareOrigin
		}
		choice("firmware", "Firmware", firmwareHelp, firmware, labels)
		if f.Source.Kind == "PreparedDiskSet" {
			cloudHelp := "Choose Yes for cloud images (Ubuntu, Fedora, Debian cloud downloads): they have no password, so cloud-init creates your user with your SSH key."
			if f.CloudOrigin != "" && f.CloudEnabled {
				cloudHelp = f.CloudOrigin + " Cloud-init creates your user with your SSH key."
			}
			choice("cloudInit", "Cloud image", cloudHelp, strconv.FormatBool(f.CloudEnabled), []string{"false", "true"})
			if f.CloudEnabled {
				c = append(c, importText("cloudUser", "Cloud user name", "Your login in the guest; lowercase letters, digits, - or _.", f.Cloud.User),
					importText("cloudKey", "SSH public key file", "Your key's .pub file; a private key is refused and never copied.", f.Cloud.KeyFile),
					importText("cloudSource", "Downloaded from", "The https:// address of this image. Virmill records it; it does not download it.", f.Cloud.Reference))
				choice("cloudSudo", "Administrator access", "Cloud users have no password, so sudo needs this to work.", strconv.FormatBool(f.Cloud.Sudo), []string{"true", "false"})
			}
		}
		c = append(c, importButton("advanced", "Advanced hardware", "Optional: CPU model, display, guest tools channel and other devices."), importButton("next", "Continue to disks", "Review storage controllers and the order in which devices boot."))
	case 1:
		total := len(f.Spec.Disks) + len(f.Spec.Media)
		if total > 0 {
			index := max(0, min(f.Disk, total-1))
			names := []string{}
			for _, d := range f.Spec.Disks {
				names = append(names, "Disk: "+d.SourceID)
			}
			for _, m := range f.Spec.Media {
				names = append(names, "Installer/media: "+m.SourceID)
			}
			choice("disk", "Device", "Left/Right checks every prepared disk and read-only medium.", names[index], names)
			bus := ""
			boot := 0
			choices := slices.Clone(f.Options.DiskBuses)
			if index < len(f.Spec.Disks) {
				bus = f.Spec.Disks[index].Bus
				boot = f.Spec.Disks[index].BootOrder
			} else {
				m := f.Spec.Media[index-len(f.Spec.Disks)]
				bus, boot = m.Bus, m.BootOrder
				choices = slices.DeleteFunc(choices, func(v string) bool { return v == "virtio" })
			}
			busHelp := "Choose a bus supported by this guest; an image alone cannot prove drivers."
			if (f.Source.Kind == "PreparedInstallation" || f.Source.Kind == "installation-media") && slices.Contains(choices, "sata") {
				busHelp = "Suggested: SATA for installation media. Choose another bus only if the guest supports it."
			}
			if f.Source.Kind == "PreparedImport" && slices.Contains(choices, "sata") {
				busHelp = "Suggested: SATA unless the appliance names a SATA controller itself; it boots on nearly every guest. Choose VirtIO only if the guest has its drivers."
			}
			if f.Source.Kind == "PreparedDiskSet" && slices.Contains(choices, "sata") {
				busHelp = "Suggested: SATA, which nearly every guest boots from. Choose VirtIO for speed if the guest has its drivers."
			}
			choice("bus", "Controller bus", busHelp, bus, choices)
			bootHelp := "1 boots first; other devices shift to keep the order."
			if index >= len(f.Spec.Disks) {
				bootHelp += " Attach only skips booting this medium."
			}
			bootValue := strconv.Itoa(boot)
			if boot < 0 || (boot == 0 && index < len(f.Spec.Disks)) {
				bootValue = "Invalid: " + bootValue
			}
			choice("boot", "Boot priority", bootHelp, bootValue, creationBootOrderChoices(f.Spec, index))
		}
		c = append(c, importButton("back", "Back", "Return to CPU, memory and storage."), importButton("next", "Continue to networks", "Check network access. Your own images start on the default NAT network; appliance adapters start disconnected."))
	case 2:
		if len(f.Spec.NICs) > 0 {
			i := max(0, min(f.NIC, len(f.Spec.NICs)-1))
			nic := f.Spec.NICs[i]
			names := []string{}
			for _, n := range f.Spec.NICs {
				origin := "new"
				if n.SourceIndex >= 0 {
					origin = "original"
				}
				names = append(names, n.ID+" ("+origin+")")
			}
			choice("nic", "Adapter", "Left/Right checks each adapter. Original adapters cannot be removed.", names[i], names)
			c = append(c, importText("nicName", "Adapter name", "Use a unique short name for this network adapter.", nic.ID))
			networks := []string{}
			selected := ""
			for _, n := range f.Networks {
				if n.Active && guidedUUID.MatchString(n.Key.UUID) && n.Key.UUID != "00000000-0000-0000-0000-000000000000" {
					networks = append(networks, n.Key.UUID)
					if n.Key.UUID == nic.NetworkID {
						selected = n.Name
						if selected == "" {
							selected = n.Key.UUID
						}
					}
				}
			}
			networkHelp := "Choose a network. Multiple networks may bypass isolation through the guest."
			if len(networks) == 0 {
				networkHelp = "No active networks. Choose Create network or Refresh networks; VM choices stay here."
			}
			if nic.NetworkID != "" && selected == "" {
				selected = "Unavailable: " + nic.NetworkID
				networkHelp = "This network is unavailable. Refresh networks or choose another; your selection is retained."
			} else if f.NetworkOrigin != "" && nic.NetworkID == f.SuggestedNetworkID && selected != "" {
				networkHelp = f.NetworkOrigin
			}
			choice("network", "Network", networkHelp, selected, networks)
			choice("nicModel", "Adapter model", "Guest drivers must support this model; host support is rechecked in the plan.", nic.Model, []string{"virtio", "e1000e", "rtl8139"})
			choice("link", "Cable", "Disconnected blocks this adapter on first boot. Connected allows network access.", nic.Link, []string{"down", "up"})
			if nic.SourceIndex == -1 {
				c = append(c, importButton("removeNIC", "Remove new adapter", "Only this added adapter is removed; original adapters stay mapped."))
			}
		}
		choice("startAfter", "After creation", "Starting is part of the same review; choose Leave it off to start it later yourself.", strconv.FormatBool(f.StartAfter), []string{"true", "false"})
		choice("removePrepared", "Prepared copy", "Frees the disk space the import used; the VM keeps its own disks. Keep it to create more VMs from these images.", strconv.FormatBool(f.RemovePrepared), []string{"true", "false"})
		previewLabel, previewHelp := "Preview VM creation", "Create a powered-off VM after approval. Start is a separate action."
		if f.StartAfter {
			previewHelp = "Create the VM and start it after one approval."
		}
		if f.BeforePreparation {
			previewLabel, previewHelp = "Review import", "One review covers preparing the images, creating the VM and starting it if chosen."
		}
		c = append(c,
			importButton("create-network", "Create network", "Create a network in a separate review, then return with your VM choices kept."),
			importButton("refresh-networks", "Refresh networks", "Read available networks again. Keep all VM choices; selecting a network is separate."),
			importButton("addNIC", "Add network adapter", "Add an adapter with no network chosen and its cable disconnected."), importButton("back", "Back", "Review disk mappings."), importButton("preview", previewLabel, previewHelp), importButton("export", "Export settings", "Save these choices for reuse without creating a VM."))
	case 3:
		choice("machine", "Machine", "Changing machine reloads observed host choices and preserves your selections.", f.Spec.Machine, f.Options.Machines)
		choice("cpuMode", "CPU mode", "Host model is a host-derived default; custom chooses an explicit compatible model.", f.Spec.CPU.Mode, f.Options.CPUModes)
		if f.Spec.CPU.Mode == "custom" {
			choice("cpuModel", "CPU model", "Only models reported usable by this host are offered.", f.Spec.CPU.Model, f.Options.CPUModels)
		}
		labels := []string{}
		for _, o := range f.Options.Firmware {
			labels = append(labels, o.Label)
		}
		value := ""
		if i := f.firmwareIndex(); i >= 0 {
			value = f.Options.Firmware[i].Label
		}
		choice("firmware", "Firmware", "Match the guest's original BIOS/UEFI needs. No firmware is chosen automatically.", value, labels)
		choice("clock", "Hardware clock", "UTC is suggested; some guests expect local time.", f.Spec.Clock, []string{"utc", "localtime"})
		choice("guestAgent", "Guest agent channel", "Enable before installing QEMU guest tools. It allows host/guest communication.", strconv.FormatBool(f.Spec.GuestAgent), []string{"false", "true"})
		choice("graphics", "Display", creationDisplayHelp(f.Spec.Graphics, f.Options.Graphics), f.Spec.Graphics, f.Options.Graphics)
		if p := f.Spec.DevicePolicy; p != nil {
			choice("usb", "USB controller", "Adding a controller does not attach host USB devices.", p.USBController, []string{"none", "qemu-xhci"})
			choice("balloon", "Memory balloon", "Allows the host to adjust guest memory; requires a virtio driver in the guest.", p.MemoryBalloon, []string{"none", "virtio"})
			watchdog := []string{"none"}
			if p.Chipset == "q35" {
				watchdog = append(watchdog, "reset")
			}
			choice("watchdog", "Watchdog", "Restart if the guest watchdog detects a hang. Requires guest configuration.", p.WatchdogAction, watchdog)
		}
		c = append(c, importButton("done", "Done", "Return to the main VM options."))
	}
	return c
}

func (f CreationForm) Update(key tea.KeyMsg) (CreationForm, ImportIntent) {
	f.Spec.Disks = slices.Clone(f.Spec.Disks)
	f.Spec.Media = slices.Clone(f.Spec.Media)
	f.Spec.NICs = slices.Clone(f.Spec.NICs)
	if f.Spec.DevicePolicy != nil {
		p := *f.Spec.DevicePolicy
		f.Spec.DevicePolicy = &p
	}
	none := ImportIntent{}
	if key.Type == tea.KeyEsc {
		if f.Page == 3 {
			f.Page = 0
		} else if f.Page > 0 {
			f.Page--
		} else {
			return f, ImportIntent{Kind: "cancel"}
		}
		f.Focus = 0
		f.Error = ""
		return f, none
	}
	controls := f.controls()
	if len(controls) == 0 {
		return f, none
	}
	f.Focus = max(0, min(f.Focus, len(controls)-1))
	switch key.Type {
	case tea.KeyTab, tea.KeyDown:
		f.Focus = (f.Focus + 1) % len(controls)
		return f, none
	case tea.KeyShiftTab, tea.KeyUp:
		f.Focus = (f.Focus + len(controls) - 1) % len(controls)
		return f, none
	}
	c := controls[f.Focus]
	activate := key.Type == tea.KeyEnter || key.Type == tea.KeySpace
	if c.kind == "choice" && (activate || key.Type == tea.KeyLeft || key.Type == tea.KeyRight) {
		direction := 1
		if key.Type == tea.KeyLeft {
			direction = -1
		}
		if len(c.choices) == 0 {
			f.Error = "No supported choices are available for " + c.label + "."
			if c.id == "pool" {
				f.Error = "No storage pool yet. Choose Create storage pool; your settings stay here."
			}
			return f, none
		}
		next := func(value string) string { return importCycle(value, c.choices, direction) }
		switch c.id {
		case "pool":
			f.Spec.PoolID = next(f.Spec.PoolID)
		case "machine":
			machine := next(f.Spec.Machine)
			if machine != f.Spec.Machine {
				f.Spec.Machine = machine
				if f.Spec.DevicePolicy != nil {
					f.Spec.DevicePolicy.Chipset = domain.CreationChipset(machine)
				}
				f.Error = ""
				return f, ImportIntent{Kind: "reload"}
			}
		case "cpuMode":
			f.Spec.CPU.Mode = next(f.Spec.CPU.Mode)
			if f.Spec.CPU.Mode != "custom" {
				f.Spec.CPU.Model = ""
			}
		case "cpuModel":
			f.Spec.CPU.Model = next(f.Spec.CPU.Model)
		case "firmware":
			i := f.firmwareIndex()
			if i < 0 {
				if direction > 0 {
					i = 0
				} else {
					i = len(f.Options.Firmware) - 1
				}
			} else {
				i = (i + direction + len(f.Options.Firmware)) % len(f.Options.Firmware)
			}
			f.Spec.Firmware = f.Options.Firmware[i].Firmware
		case "clock":
			f.Spec.Clock = next(f.Spec.Clock)
		case "guestAgent":
			f.Spec.GuestAgent = !f.Spec.GuestAgent
		case "startAfter":
			f.StartAfter = !f.StartAfter
		case "removePrepared":
			f.RemovePrepared = !f.RemovePrepared
		case "cloudInit":
			f.CloudEnabled, f.CloudDecided = !f.CloudEnabled, true
			if f.CloudEnabled && f.Cloud.User == "" {
				f.Cloud = defaultCloudSetup()
			}
		case "cloudSudo":
			f.Cloud.Sudo = !f.Cloud.Sudo
		case "graphics":
			f.Spec.Graphics = next(f.Spec.Graphics)
		case "usb":
			f.Spec.DevicePolicy.USBController = next(f.Spec.DevicePolicy.USBController)
		case "balloon":
			f.Spec.DevicePolicy.MemoryBalloon = next(f.Spec.DevicePolicy.MemoryBalloon)
		case "watchdog":
			f.Spec.DevicePolicy.WatchdogAction = next(f.Spec.DevicePolicy.WatchdogAction)
		case "disk":
			f.Disk = (max(0, min(f.Disk, len(c.choices)-1)) + direction + len(c.choices)) % len(c.choices)
		case "bus", "boot":
			i := max(0, min(f.Disk, len(f.Spec.Disks)+len(f.Spec.Media)-1))
			if c.id == "boot" {
				order := 0
				if i < len(f.Spec.Disks) {
					order = f.Spec.Disks[i].BootOrder
				} else {
					order = f.Spec.Media[i-len(f.Spec.Disks)].BootOrder
				}
				requested, _ := strconv.Atoi(next(strconv.Itoa(order)))
				var err error
				f.Spec, err = creationMoveBootOrder(f.Spec, i, requested)
				if err != nil {
					f.Error = err.Error()
					return f, none
				}
				break
			}
			if i < len(f.Spec.Disks) {
				f.Spec.Disks[i].Bus = next(f.Spec.Disks[i].Bus)
			} else {
				m := &f.Spec.Media[i-len(f.Spec.Disks)]
				m.Bus = next(m.Bus)
			}
		case "nic":
			f.NIC = (max(0, min(f.NIC, len(c.choices)-1)) + direction + len(c.choices)) % len(c.choices)
		case "network", "nicModel", "link":
			n := &f.Spec.NICs[max(0, min(f.NIC, len(f.Spec.NICs)-1))]
			switch c.id {
			case "network":
				n.NetworkID = next(n.NetworkID)
			case "nicModel":
				n.Model = next(n.Model)
			case "link":
				n.Link = next(n.Link)
			}
		}
		f.cursorField = ""
		f.Error = ""
		return f, none
	}
	if c.kind == "button" && activate {
		switch c.id {
		case "create-network", "refresh-networks", "create-pool":
			return f, ImportIntent{Kind: c.id}
		case "start-pool":
			p, _ := f.startablePool()
			return f, ImportIntent{Kind: c.id, Target: p.Key.UUID}
		case "advanced":
			f.Page = 3
			f.Focus = 0
		case "done":
			f.Page = 0
			f.Focus = 0
		case "back":
			f.Page = max(0, f.Page-1)
			f.Focus = 0
		case "next":
			// Stop at the first unfinished field on this or an earlier step.
			// Later disk/network choices remain editable on their own pages.
			if _, err := f.Request("qemu:///system"); err != nil {
				invalid := f
				invalid.FocusError(err)
				if invalid.Page <= f.Page || invalid.Page == 3 {
					if f.Page == 0 && strings.Contains(err.Error(), "firmware") {
						invalid.Page = 0
						for i, control := range invalid.controls() {
							if control.id == "firmware" {
								invalid.Focus = i
							}
						}
					}
					return invalid, none
				}
			}
			f.Page = min(2, f.Page+1)
			f.Focus = 0
		case "preview", "export":
			if _, err := f.Request("qemu:///system"); err != nil {
				f.FocusError(err)
				return f, none
			}
			return f, ImportIntent{Kind: c.id}
		case "addNIC":
			if len(f.Spec.NICs) >= 32 {
				f.Error = "A VM can have at most 32 network adapters."
				return f, none
			}
			id := ""
			for i := 1; id == ""; i++ {
				candidate := fmt.Sprintf("nic%d", i)
				if !slices.ContainsFunc(f.Spec.NICs, func(n domain.CreationNIC) bool { return n.ID == candidate }) {
					id = candidate
				}
			}
			f.Spec.NICs = append(f.Spec.NICs, domain.CreationNIC{ID: id, SourceIndex: -1, Link: "down"})
			f.NIC = len(f.Spec.NICs) - 1
			f.Focus = 0
		case "removeNIC":
			i := max(0, min(f.NIC, len(f.Spec.NICs)-1))
			if f.Spec.NICs[i].SourceIndex == -1 {
				f.Spec.NICs = slices.Delete(f.Spec.NICs, i, i+1)
				f.NIC = max(0, min(i, len(f.Spec.NICs)-1))
				f.Focus = 0
			}
		}
		f.Error = ""
		f.cursorField = ""
		return f, none
	}
	if c.kind == "text" && key.Type != tea.KeyEnter {
		if f.cursorField != c.id {
			f.cursor = utf8.RuneCountInString(c.value)
			f.cursorField = c.id
		}
		input := GuidedForm{Fields: []GuidedField{{Name: c.id, Label: c.label, Value: c.value, Cursor: f.cursor, Limit: 255}}}
		if key.Type == tea.KeyCtrlU {
			input.Fields[0].Value = ""
			input.Fields[0].Cursor = 0
		} else {
			input, _, _ = input.Update(key)
		}
		f.cursor = input.Fields[0].Cursor
		if input.Error != "" {
			f.Error = input.Error
			return f, none
		}
		value := input.Fields[0].Value
		switch c.id {
		case "name":
			f.Spec.Name = value
		case "cpu":
			f.CPUText = value
			f.CPUOrigin = "Your selected CPU count."
		case "memory":
			f.MemoryText = value
			f.MemoryOrigin = "Your selected memory size."
		case "nicName":
			f.Spec.NICs[max(0, min(f.NIC, len(f.Spec.NICs)-1))].ID = value
		case "cloudUser":
			f.Cloud.User = value
		case "cloudKey":
			f.Cloud.KeyFile = value
		case "cloudSource":
			f.Cloud.Reference = value
		}
		f.Error = ""
	}
	return f, none
}

func (f CreationForm) Request(connection string) (app.Request, error) {
	fail := func(s string) (app.Request, error) { return app.Request{}, fmt.Errorf("%s", s) }
	if !guidedLocal(connection) || (!guidedUUID.MatchString(f.OperationID) && !(f.BeforePreparation && f.OperationID == "")) {
		return fail("Choose a completed image-preparation job on this connection.")
	}
	s := f.Spec
	s.Disks = slices.Clone(s.Disks)
	s.Media = slices.Clone(s.Media)
	s.NICs = slices.Clone(s.NICs)
	if _, err := validation.DisplayName(s.Name); err != nil {
		return fail("VM name: enter a valid nonempty display name.")
	}
	cpu, ok := guidedNumber(f.CPUText, 512)
	if !ok || f.Options.MaxVCPUs == 0 || cpu > uint64(f.Options.MaxVCPUs) {
		return fail(fmt.Sprintf("CPU cores: enter 1–%d supported cores.", f.Options.MaxVCPUs))
	}
	memory, ok := guidedNumber(f.MemoryText, 1<<20)
	if !ok || memory < 128 {
		return fail("Memory: enter 128–1048576 MiB.")
	}
	if f.Options.HostMemoryMiB == 0 || memory > f.Options.HostMemoryMiB {
		return fail(fmt.Sprintf("Memory: this host reports %d MiB; choose a value within that capacity.", f.Options.HostMemoryMiB))
	}
	s.VCPUs, s.MemoryMiB = uint(cpu), memory
	if !slices.ContainsFunc(f.Pools, func(p domain.StoragePool) bool { return creationUsablePool(p) && p.Key.UUID == s.PoolID }) {
		return fail("Storage pool: select an active pool.")
	}
	if s.Architecture != f.Options.Architecture || s.Machine != f.Options.Machine || !slices.Contains(f.Options.Machines, s.Machine) {
		return fail("Hardware: choose a machine and wait for its options to refresh.")
	}
	if !slices.Contains(f.Options.CPUModes, s.CPU.Mode) || (s.CPU.Mode == "custom" && !slices.Contains(f.Options.CPUModels, s.CPU.Model)) || (s.CPU.Mode != "custom" && s.CPU.Model != "") {
		return fail("Hardware: choose a supported CPU mode and, for custom mode, a CPU model.")
	}
	if f.firmwareIndex() < 0 {
		return fail("Hardware options: choose firmware matching this guest's BIOS or UEFI needs.")
	}
	if !slices.Contains(f.Options.Graphics, s.Graphics) || !slices.Contains([]string{"utc", "localtime"}, s.Clock) {
		return fail("Hardware options: choose a supported display and hardware clock.")
	}
	if s.DevicePolicy != nil {
		if err := s.DevicePolicy.Validate(s.Machine); err != nil {
			return fail("Hardware options: review USB, memory balloon and watchdog choices for this machine.")
		}
	}
	if len(s.Disks) != len(f.Source.Disks) || len(s.Media) != len(f.Source.Media) || len(s.Disks) < 1 || len(s.Disks) > 64 || len(s.Media) > 4 {
		return fail("Disks: include every prepared disk and medium exactly once.")
	}
	seen := map[string]bool{}
	orders := map[int]bool{}
	bootCount := len(s.Disks)
	sata := 0
	for _, m := range s.Media {
		if m.BootOrder > 0 {
			bootCount++
		}
	}
	for _, d := range s.Disks {
		if seen[d.SourceID] || !slices.ContainsFunc(f.Source.Disks, func(v CreationSourceDisk) bool { return v.SourceID == d.SourceID }) {
			return fail("Disks: each prepared source disk must appear exactly once.")
		}
		seen[d.SourceID] = true
		if !slices.Contains(f.Options.DiskBuses, d.Bus) {
			return fail("Disk " + d.SourceID + ": choose a supported controller bus.")
		}
		if d.BootOrder < 1 || d.BootOrder > bootCount || orders[d.BootOrder] {
			return fail("Disk " + d.SourceID + ": assign a unique boot priority starting at 1.")
		}
		orders[d.BootOrder] = true
		if d.Bus == "sata" {
			sata++
		}
	}
	for _, m := range s.Media {
		if seen[m.SourceID] || !slices.ContainsFunc(f.Source.Media, func(v CreationSourceMedia) bool { return v.SourceID == m.SourceID }) {
			return fail("Media: each prepared source medium must appear exactly once.")
		}
		seen[m.SourceID] = true
		if !slices.Contains(f.Options.DiskBuses, m.Bus) || !slices.Contains([]string{"sata", "scsi"}, m.Bus) {
			return fail("Media " + m.SourceID + ": choose SATA or SCSI supported by this machine.")
		}
		if m.BootOrder < 0 || m.BootOrder > bootCount || (m.BootOrder > 0 && orders[m.BootOrder]) {
			return fail("Media " + m.SourceID + ": choose a unique boot priority, or Attach only to skip booting.")
		}
		if m.BootOrder > 0 {
			orders[m.BootOrder] = true
		}
		if m.Bus == "sata" {
			sata++
		}
	}
	if sata > 6 {
		return fail("Disks: SATA supports six devices; move extra devices to SCSI or virtio.")
	}
	original := 0
	for _, item := range f.Source.System.Items {
		if item.ResourceType == "10" {
			original++
		}
	}
	sourceNICs := map[int]bool{}
	ids := map[string]bool{}
	if len(s.NICs) > 32 {
		return fail("Networks: keep at most 32 adapters.")
	}
	for _, n := range s.NICs {
		if !guidedRootID.MatchString(n.ID) || len(n.ID) > 63 || ids[n.ID] {
			return fail("Networks: give each adapter a unique short name.")
		}
		ids[n.ID] = true
		if n.SourceIndex < -1 || n.SourceIndex >= original || (n.SourceIndex >= 0 && sourceNICs[n.SourceIndex]) {
			return fail("Networks: map each original adapter exactly once.")
		}
		if n.SourceIndex >= 0 {
			sourceNICs[n.SourceIndex] = true
		}
		if !guidedUUID.MatchString(n.NetworkID) || n.NetworkID == "00000000-0000-0000-0000-000000000000" || !slices.ContainsFunc(f.Networks, func(v domain.VirtualNetwork) bool { return v.Active && v.Key.UUID == n.NetworkID }) {
			return fail("Adapter " + n.ID + ": choose an active network, even with its cable disconnected.")
		}
		if !slices.Contains([]string{"virtio", "e1000e", "rtl8139"}, n.Model) || !slices.Contains([]string{"down", "up"}, n.Link) {
			return fail("Adapter " + n.ID + ": choose a model and cable state.")
		}
		if n.MAC != "" {
			return fail("Networks: new VMs receive new MAC addresses; clear any source MAC.")
		}
	}
	if len(sourceNICs) != original {
		return fail("Networks: original adapters cannot be omitted; map them with cable down if needed.")
	}
	// The NoCloud seed is one more read-only medium that never boots.
	var provisioning map[string]any
	if f.CloudEnabled {
		if f.Source.Kind != "PreparedDiskSet" {
			return fail("Cloud image: cloud-init setup needs an existing disk image.")
		}
		seed := cloudSeedID(s)
		bus := "sata"
		if sata >= 6 || !slices.Contains(f.Options.DiskBuses, "sata") {
			bus = "scsi"
		}
		if !slices.Contains(f.Options.DiskBuses, bus) {
			return fail("Cloud image: this machine offers no SATA or SCSI bus for the setup disk.")
		}
		p, err := f.cloudProvisioning(s, seed)
		if err != nil {
			return app.Request{}, err
		}
		s.Media = append(s.Media, domain.CreationMedia{SourceID: seed, Bus: bus, BootOrder: 0})
		provisioning = p
	}
	b, err := json.Marshal(s)
	if err != nil {
		return fail("Review the VM options before continuing.")
	}
	hardware := map[string]any{}
	if err := json.Unmarshal(b, &hardware); err != nil {
		return app.Request{}, err
	}
	delete(hardware, "uuid")
	r := app.Request{ID: f.OperationID, Connection: connection, Action: "create", Input: map[string]any{"identityMode": "clone", "hardware": hardware}}
	if provisioning != nil {
		r.Input["provisioning"] = provisioning
	}
	// Removing the prepared copy afterwards lets creation hand it over while
	// copying, which halves the peak space when both share a filesystem (ADR 0060).
	if f.RemovePrepared {
		r.Input["preparedCopy"] = "hand-over"
	}
	b, err = json.Marshal(r.Input)
	if err != nil {
		return app.Request{}, err
	}
	if err = validation.Schema("vm-creation-input", b); err != nil {
		return fail("Review the named hardware, disk and network options; the creation request is incomplete.")
	}
	return r, nil
}

func (f CreationForm) View(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	clean := func(v string) string {
		return ansi.Truncate(strings.NewReplacer("\n", " ", "\t", " ").Replace(validation.SafeText(v)), width, "")
	}
	if width < 40 || height < 10 {
		return strings.Join([]string{clean("Resize to edit VM options."), clean("Your choices are retained.")}[:min(2, height)], "\n")
	}
	titles := []string{"VM options", "Disks and boot", "Network adapters", "Advanced hardware"}
	page := max(0, min(f.Page, 3))
	purpose := "Creates a powered-off copy. Start it separately when ready."
	if f.StartAfter {
		purpose = "Creates a copy of the images and starts it after one review."
	}
	if f.BeforePreparation {
		purpose = "Choose VM hardware before preparing the images."
	}
	lines := []string{clean("Create a VM"), clean(purpose), "", clean(titles[page]), ""}
	if page < 3 {
		lines[3] = clean(fmt.Sprintf("Step %d of 3 · %s", page+1, titles[page]))
	}
	if page == 0 || page == 2 {
		lines[4] = clean("Display: " + creationFriendlyValue("graphics", f.Spec.Graphics))
		lines = append(lines, wrap(validation.SafeText(creationDisplayHelp(f.Spec.Graphics, f.Options.Graphics)), width)...)
	}
	if page == 1 {
		lines = append(lines[:4], wrap(creationBootOrderSummary(f.Spec), width)...)
	}

	controls := f.controls()
	if len(controls) == 0 {
		return strings.Join(lines, "\n")
	}
	focus := max(0, min(f.Focus, len(controls)-1))
	primary := -1
	body := []int{}
	for i, c := range controls {
		if c.id == "next" || c.id == "preview" || c.id == "done" {
			primary = i
		} else {
			body = append(body, i)
		}
	}
	footer := []string{}
	if f.Error != "" {
		messages := wrap(validation.SafeText(f.Error), width)
		footer = append(footer, messages[:min(3, len(messages))]...)
	}
	if primary >= 0 {
		marker := "  "
		if focus == primary {
			marker = "> "
		}
		footer = append(footer, clean(marker+"[ "+controls[primary].label+" ]"))
	}
	help := wrap(validation.SafeText(controls[focus].help), width)
	footer = append(footer, help[:min(2, len(help))]...)
	footer = append(footer, clean("Tab next · Left/Right change · Esc back"))
	// Leave room for the focused control even when an error and help wrap.
	for len(lines)+len(footer)+2 > height && len(lines) > 2 {
		lines = append(lines[:2], lines[3:]...)
	}
	room := max(1, height-len(lines)-len(footer))
	showScroll := len(body) > room && room > 1
	if showScroll {
		room--
	}
	pos := slices.Index(body, focus)
	start := max(0, pos-room+1)
	if pos < 0 {
		start = max(0, len(body)-room)
	}
	for j := start; j < min(len(body), start+room); j++ {
		i := body[j]
		c := controls[i]
		marker := "  "
		if i == focus {
			marker = "> "
		}
		value := validation.SafeText(creationFriendlyValue(c.id, c.value))
		row := ""
		switch c.kind {
		case "button":
			row = "[ " + c.label + " ]"
		case "choice":
			row = c.label + ": < " + value + " >"
		default:
			if i == focus {
				r := []rune(value)
				cursor := len(r)
				if f.cursorField == c.id {
					cursor = max(0, min(f.cursor, len(r)))
				}
				value = importTail(string(r[:cursor]), max(1, width-ansi.StringWidth(c.label)-9)) + "|" + string(r[cursor:])
			}
			row = c.label + ": [" + value + "]"
		}
		lines = append(lines, clean(marker+row))
	}
	if showScroll {
		lines = append(lines, clean(fmt.Sprintf("Options %d–%d of %d · Tab to see more", start+1, min(len(body), start+room), len(body))))
	}
	lines = append(lines, footer...)
	return strings.Join(lines[:min(height, len(lines))], "\n")
}

func creationUsablePool(p domain.StoragePool) bool {
	return p.Active && slices.Contains([]string{"dir", "fs", "netfs"}, p.Type)
}

var natForward = regexp.MustCompile(`<forward\b[^>]*\bmode=['"]nat['"]`)

// addDefaultNIC connects the user's own images (disk sets and installers) to
// libvirt's active default NAT network with a widely supported adapter model.
// Appliance adapters keep their mapping and start disconnected (spec 05).
func (f *CreationForm) addDefaultNIC() {
	nat, ok := defaultNATNetwork(f.Networks)
	if !ok {
		return
	}
	if f.Source.Kind == "PreparedImport" {
		// Appliance adapters keep their mapping and start disconnected (spec 05);
		// only the unset network and model get labelled suggestions.
		for i := range f.Spec.NICs {
			if f.Spec.NICs[i].NetworkID == "" {
				f.Spec.NICs[i].NetworkID = nat
				f.NetworkOrigin = "Suggested: libvirt's default NAT network. The cable stays disconnected; connect it only if you trust this appliance."
			}
			if f.Spec.NICs[i].Model == "" {
				f.Spec.NICs[i].Model = "e1000e"
			}
		}
		f.SuggestedNetworkID = nat
		return
	}
	if len(f.Spec.NICs) != 0 || (f.Source.Kind != "PreparedDiskSet" && f.Source.Kind != "PreparedInstallation" && f.Source.Kind != "installation-media") {
		return
	}
	f.Spec.NICs = append(f.Spec.NICs, domain.CreationNIC{ID: "nic1", SourceIndex: -1, NetworkID: nat, Model: "e1000e", Link: "up"})
	f.NetworkOrigin = "Suggested: libvirt's default NAT network, so the guest can reach the internet. Set Cable to Disconnected to keep it offline."
	f.SuggestedNetworkID = nat
}

// defaultNATNetwork is libvirt's active "default" network when it uses NAT.
func defaultNATNetwork(networks []domain.VirtualNetwork) (string, bool) {
	for _, n := range networks {
		xml := n.LiveXML
		if xml == "" {
			xml = n.PersistentXML
		}
		if n.Name == "default" && n.Active && guidedUUID.MatchString(n.Key.UUID) && natForward.MatchString(xml) {
			return n.Key.UUID, true
		}
	}
	return "", false
}

// uniqueName suggests the next free name when a VM on this connection already
// uses the current one, as when importing the same appliance again. The change
// is labelled and editable; creation still checks names itself.
func (f *CreationForm) uniqueName(vms []domain.VM) {
	used := map[string]bool{}
	for _, v := range vms {
		used[v.Name] = true
	}
	if f == nil || !used[f.Spec.Name] {
		return
	}
	base := f.Spec.Name
	for i := 2; i < 1000; i++ {
		if candidate := fmt.Sprintf("%s %d", base, i); !used[candidate] {
			f.Spec.Name = candidate
			f.NameOrigin = "Suggested: a VM named " + base + " already exists on this connection."
			return
		}
	}
}

// startablePool is a stopped persistent file-based pool, preferring "default".
func (f CreationForm) startablePool() (domain.StoragePool, bool) {
	var found domain.StoragePool
	ok := false
	for _, p := range f.Pools {
		if !p.Active && p.Persistent && slices.Contains([]string{"dir", "fs", "netfs"}, p.Type) && guidedUUID.MatchString(p.Key.UUID) && (!ok || p.Name == "default") {
			found, ok = p, true
		}
	}
	return found, ok
}

// defaultCreationPool preselects libvirt's "default" pool, the only usable
// pool, the only one that starts with the host, or else the one with the most
// free space. The settings page names it and Advanced settings change it, so a
// host with several pools never stops VM setup for a choice (ADR 0065).
func defaultCreationPool(pools []domain.StoragePool) string {
	usable := []domain.StoragePool{}
	for _, p := range pools {
		if creationUsablePool(p) {
			if p.Name == "default" {
				return p.Key.UUID
			}
			usable = append(usable, p)
		}
	}
	if len(usable) == 1 {
		return usable[0].Key.UUID
	}
	autostart := []domain.StoragePool{}
	for _, p := range usable {
		if p.Autostart {
			autostart = append(autostart, p)
		}
	}
	if len(autostart) == 1 {
		return autostart[0].Key.UUID
	}
	best, most := "", uint64(0)
	for _, p := range usable {
		if p.AvailableBytes == nil {
			return ""
		}
		if *p.AvailableBytes > most {
			best, most = p.Key.UUID, *p.AvailableBytes
		}
	}
	return best
}

// defaultCreationFirmware follows firmware the source declares. Otherwise it
// suggests BIOS, which most disk images boot with, and says so in the form.
func defaultCreationFirmware(options domain.CreationOptions, declared string) (domain.CreationFirmware, string) {
	uefi := strings.Contains(strings.ToLower(declared), "efi")
	for _, o := range options.Firmware {
		bios := o.Firmware.Mode == "bios"
		if uefi && !bios && !o.Firmware.SecureBoot && !o.Firmware.TPM || !uefi && bios {
			if declared != "" {
				return o.Firmware, "Detected from the source. Change it only if the guest does not boot."
			}
			return o.Firmware, "Suggested: BIOS, which most disk images use. Choose UEFI if this image was made for UEFI."
		}
	}
	return domain.CreationFirmware{}, ""
}
func creationSourceBus(source CreationSource, diskID string, options domain.CreationOptions) string {
	if !slices.Contains(options.DiskBuses, "sata") {
		return ""
	}
	// Installers and plain disk images carry no controller metadata. SATA boots
	// on nearly every guest without extra drivers; the form labels it Suggested.
	if source.Kind == "PreparedInstallation" || source.Kind == "installation-media" || source.Kind == "PreparedDiskSet" {
		return "sata"
	}
	// Appliances often name controllers QEMU cannot offer (VMware SCSI, IDE on
	// Q35). SATA is then the labelled suggestion rather than a required choice.
	fallback := ""
	if source.Kind == "PreparedImport" {
		fallback = "sata"
	}
	attachments := []importer.Item{}
	for _, item := range source.System.Items {
		if item.ResourceType == "17" && slices.Contains(item.HostResources, "ovf:/disk/"+diskID) {
			attachments = append(attachments, item)
		}
	}
	if len(attachments) != 1 || attachments[0].Parent == "" {
		return fallback
	}
	controllers := []importer.Item{}
	for _, item := range source.System.Items {
		if item.InstanceID == attachments[0].Parent {
			controllers = append(controllers, item)
		}
	}
	if len(controllers) == 1 && controllers[0].ResourceType == "20" {
		return "sata"
	}
	return fallback
}

// FocusError returns users to the option named by local validation, without
// discarding other pages. The workspace can also use it after a service refusal.
func (f *CreationForm) FocusError(err error) {
	if err == nil {
		return
	}
	f.Error = err.Error()
	field := "name"
	f.Page = 0
	switch {
	case strings.HasPrefix(f.Error, "Cloud image:"):
		field = "cloudInit"
	case strings.HasPrefix(f.Error, "Cloud user name:"):
		field = "cloudUser"
	case strings.HasPrefix(f.Error, "SSH public key:"):
		field = "cloudKey"
	case strings.HasPrefix(f.Error, "Downloaded from:"):
		field = "cloudSource"
	case strings.HasPrefix(f.Error, "CPU cores:"):
		field = "cpu"
	case strings.HasPrefix(f.Error, "Memory:"):
		field = "memory"
	case strings.HasPrefix(f.Error, "Storage pool:"):
		field = "pool"
	case strings.Contains(f.Error, "firmware"):
		f.Page = 3
		field = "firmware"
	case strings.Contains(f.Error, "CPU mode"):
		f.Page = 3
		field = "cpuMode"
	case strings.HasPrefix(f.Error, "Hardware"):
		f.Page = 3
		field = "machine"
		if strings.Contains(f.Error, "display") {
			field = "graphics"
		}
		if strings.Contains(f.Error, "watchdog") {
			field = "watchdog"
		}
	case strings.HasPrefix(f.Error, "Disk "), strings.HasPrefix(f.Error, "Disks:"), strings.HasPrefix(f.Error, "Media"):
		f.Page = 1
		field = "disk"
		for i, d := range f.Spec.Disks {
			if strings.HasPrefix(f.Error, "Disk "+d.SourceID+":") {
				f.Disk = i
				field = "bus"
				if strings.Contains(f.Error, "boot priority") {
					field = "boot"
				}
			}
		}
		for i, m := range f.Spec.Media {
			if strings.HasPrefix(f.Error, "Media "+m.SourceID+":") {
				f.Disk = len(f.Spec.Disks) + i
				field = "bus"
				if strings.Contains(f.Error, "boot priority") {
					field = "boot"
				}
			}
		}
	case strings.HasPrefix(f.Error, "Adapter "), strings.HasPrefix(f.Error, "Networks:"):
		f.Page = 2
		field = "nic"
		for i, n := range f.Spec.NICs {
			if strings.HasPrefix(f.Error, "Adapter "+n.ID+":") {
				f.NIC = i
				field = "network"
				if strings.Contains(f.Error, "model") {
					field = "nicModel"
				}
			}
		}
	}
	f.Focus = 0
	for i, c := range f.controls() {
		if c.id == field {
			f.Focus = i
			break
		}
	}
	f.cursorField = ""
}

// creationFriendlyValue keeps protocol values out of simple decisions without
// changing the declaration sent to the shared service.
func creationFriendlyValue(id, value string) string {
	labels := map[string]map[string]string{
		"boot":           {"0": "Attach only"},
		"guestAgent":     {"false": "Disabled", "true": "Enabled"},
		"startAfter":     {"true": "Start the VM", "false": "Leave it off"},
		"removePrepared": {"true": "Remove after creation", "false": "Keep"},
		"cloudInit":      {"false": "No", "true": "Yes, create my user with cloud-init"},
		"cloudSudo":      {"true": "Passwordless sudo", "false": "None"},
		"link":           {"down": "Disconnected", "up": "Connected"},
		"graphics":       {"none": "No display", "vnc-unix": "Local display (VNC)", "spice-unix": "Local display (SPICE)"},
		"cpuMode":        {"host-model": "Host-compatible (host-model)", "host-passthrough": "Host CPU (host-passthrough)", "custom": "Choose a CPU model"},
		"clock":          {"utc": "UTC", "localtime": "Local time"},
		"usb":            {"none": "Disabled", "qemu-xhci": "USB 3 (qemu-xhci)"},
		"balloon":        {"none": "Disabled", "virtio": "Enabled (virtio)"},
		"watchdog":       {"none": "Disabled", "reset": "Restart guest on timeout"},
		"bus":            {"sata": "SATA", "scsi": "SCSI", "virtio": "Virtio (guest driver required)"},
	}
	if text := labels[id][value]; text != "" {
		return text
	}
	return value
}

// Display defaults apply only to a newly constructed form. Reloaded/resumed
// declarations retain their explicit Graphics value and are reviewed unchanged.
func creationDisplayHelp(graphics string, options []string) string {
	switch graphics {
	case "spice-unix":
		return "Requires this host's desktop. A plain SSH terminal cannot show the display."
	case "vnc-unix":
		if slices.Contains(options, "spice-unix") {
			return "VNC launch is unavailable. Choose SPICE in Advanced hardware for Console."
		}
		return "VNC launch is unavailable in Virmill. Check guest access before creating."
	case "none":
		return "No graphical display. Guest serial or SSH access must be configured separately."
	default:
		return "No supported display selected. Check Advanced hardware before creating."
	}
}
