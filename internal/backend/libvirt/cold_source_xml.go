//go:build linux && cgo

package libvirt

import (
	"regexp"
	"strconv"
	"strings"

	"virmill.local/core/internal/domain"
)

const coldSourceDiskLimit = 256
const coldSourceBackingLimit = 16
const coldSourceEntryLimit = 1024
const coldSourceDependencyLimit = 1024

var coldDiskTarget = regexp.MustCompile(`^(?:(?:ioemu:)?(?:fd|hd|sd|vd|xvd|ubd)[a-zA-Z0-9_]+|nvme[0-9]+n[0-9]+(?:p[0-9]+)?)$`)

// InspectColdSourceXML inventories declarations, not accessible files or a
// complete image graph. The original XML must remain authoritative. No source
// is opened and unresolved dependencies must block a later complete capture.
func InspectColdSourceXML(raw string) (domain.ColdSourceLayout, error) {
	empty := domain.ColdSourceLayout{}
	state, err := InspectColdStateXML(raw)
	if err != nil {
		return empty, err
	}
	root, err := coldStateTree(raw)
	if err != nil {
		return empty, err
	}
	out := domain.ColdSourceLayout{State: state, Disks: []domain.ColdDiskSource{}, External: []domain.ColdDependency{}}
	add := func(kind, target string) error {
		if len(out.External) >= coldSourceDependencyLimit {
			return coldInvalid("external dependency limit")
		}
		out.External = append(out.External, domain.ColdDependency{Kind: kind, Target: target})
		return nil
	}
	osNode, _ := coldChild(root, "os", true) // Already checked by state extraction.
	osType, err := coldChild(osNode, "type", false)
	if err != nil {
		return empty, err
	}
	if osType != nil {
		if err := coldAttrs(osType, nil, []string{"arch", "machine"}); err != nil {
			return empty, err
		}
		if len(osType.children) != 0 || coldSourceIdentifier(osType.text, 64) != nil {
			return empty, coldInvalid("OS type must be a scalar identifier")
		}
		out.Architecture, out.Machine = attr(osType, "arch"), attr(osType, "machine")
		for _, value := range []string{out.Architecture, out.Machine} {
			if value != "" && coldSourceIdentifier(value, 256) != nil {
				return empty, coldInvalid("invalid architecture or machine identifier")
			}
		}
	}
	devices, _ := coldChild(root, "devices", true)
	seen := map[string]bool{}
	entries := 0
	for i, n := range devices.children {
		location := "devices/device[" + strconv.Itoa(i+1) + "]"
		if n.name.Local == "disk" {
			if n.name.Space != "" {
				return empty, coldInvalid("foreign disk element")
			}
			if len(out.Disks) >= coldSourceDiskLimit {
				return empty, coldInvalid("disk inventory limit")
			}
			disk, err := coldSourceDisk(n, add)
			if err != nil {
				return empty, err
			}
			targetKey := strings.TrimPrefix(disk.Target, "ioemu:")
			if seen[targetKey] {
				return empty, coldInvalid("duplicate disk target")
			}
			seen[targetKey] = true
			entries += 1 + len(disk.Backing)
			if entries > coldSourceEntryLimit {
				return empty, coldInvalid("storage source inventory limit")
			}
			out.Disks = append(out.Disks, disk)
			continue
		}
		kind := "device-runtime"
		if n.name.Space != "" {
			kind = "runtime-extension"
		} else {
			if coldSourceConfigDevice(n) {
				continue // Configuration is retained in the authoritative XML.
			}
			switch n.name.Local {
			case "tpm":
				continue // Accounted for by the strict cold-state projection.
			case "filesystem", "hostdev", "shmem", "interface", "memory", "pstore", "lease", "emulator":
				kind = n.name.Local
			}
			if kind == "filesystem" || kind == "hostdev" || kind == "shmem" {
				for _, name := range []string{"source", "target", "server", "backend"} {
					if _, err := coldChild(n, name, false); err != nil {
						return empty, err
					}
				}
			}
		}
		if err := add(kind, location); err != nil {
			return empty, err
		}
	}
	// Metadata is inert application-owned XML. Other namespaced subtrees may
	// affect runtime behavior, so record their location without copying content.
	for i, n := range root.children {
		if n.name.Local == "metadata" && n.name.Space == "" || n == devices || n == osNode {
			continue
		}
		kind := ""
		if n.name.Space != "" {
			kind = "runtime-extension"
			if n.name.Space == "http://libvirt.org/schemas/domain/qemu/1.0" && n.name.Local == "commandline" {
				kind = "qemu-commandline"
			}
		} else if coldSourceContainsExternal(n, n.name.Local == "features") {
			kind = "runtime-dependency"
		} else if !coldEnum(n.name.Local, "name", "uuid", "hwuuid", "genid", "title", "description", "memory", "currentMemory", "maxMemory", "memoryBacking", "vcpu", "vcpus", "cpu", "cputune", "numatune", "blkiotune", "memtune", "resource", "sysinfo", "features", "clock", "on_poweroff", "on_reboot", "on_crash", "on_lockfailure", "pm", "launchSecurity", "seclabel", "idmap", "keywrap", "perf", "iothreadids", "iothreads", "defaultiothread", "throttlegroups") {
			kind = "runtime-extension"
		}
		if kind != "" {
			if err := add(kind, "domain/element["+strconv.Itoa(i+1)+"]"); err != nil {
				return empty, err
			}
		}
	}
	for i, n := range osNode.children {
		if n.name.Local == "type" || n.name.Local == "loader" || n.name.Local == "nvram" {
			continue
		}
		if n.name.Space != "" || coldSourceContainsExternal(n, false) || !coldEnum(n.name.Local, "boot", "bootmenu", "bios", "smbios", "firmware") {
			if err := add("boot-runtime", "domain/os/element["+strconv.Itoa(i+1)+"]"); err != nil {
				return empty, err
			}
		}
	}
	return out, nil
}

func coldSourceIdentifier(value string, limit int) error {
	if value == "" || coldScalar(value, limit) != nil {
		return coldInvalid("missing or invalid source identifier")
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == ':') {
			return coldInvalid("invalid source identifier character")
		}
	}
	return nil
}

// Presence of these fields requires an adapter outside disk-chain extraction.
// Recursion also notices extensions nested in native configuration containers.
// domainFeatures is true only for the direct native domain/features container.
// Its empty ACPI feature marker is configuration; OS ACPI tables and every other
// ACPI shape still require an adapter. This context never propagates recursively.
func coldSourceContainsExternal(n *xmlNode, domainFeatures bool) bool {
	if n.name.Space != "" {
		return true
	}
	switch n.name.Local {
	case "source", "path", "file", "kernel", "initrd", "dtb", "acpi", "bootloader", "init", "initdir", "emulator", "backend", "loader", "nvram":
		return true
	}
	for _, a := range n.attrs {
		if a.Name.Space != "" || a.Name.Local == "file" || a.Name.Local == "path" || a.Name.Local == "dir" {
			return true
		}
	}
	for _, c := range n.children {
		if domainFeatures && c.name.Space == "" && c.name.Local == "acpi" && coldSourceEmpty(c) {
			continue
		}
		if coldSourceContainsExternal(c, false) {
			return true
		}
	}
	return false
}

func coldSourceEmpty(n *xmlNode) bool {
	return n == nil || len(n.attrs) == 0 && len(n.children) == 0 && strings.TrimSpace(n.text) == ""
}

func coldSourceDisk(n *xmlNode, add func(string, string) error) (domain.ColdDiskSource, error) {
	out := domain.ColdDiskSource{Backing: []domain.ColdStorageSource{}}
	if err := coldAttrs(n, nil, []string{"type", "device", "model", "snapshot", "rawio", "sgio"}); err != nil {
		return out, err
	}
	if strings.TrimSpace(n.text) != "" {
		return out, coldInvalid("text in disk declaration")
	}
	out.Device = attr(n, "device")
	if out.Device == "" {
		out.Device = "disk" // Native XML default, not a probed device type.
	}
	if !coldEnum(out.Device, "disk", "lun", "cdrom", "floppy") {
		return out, coldUnsupported("unrecognized disk device")
	}
	target, err := coldChild(n, "target", true)
	if err != nil {
		return out, err
	}
	if err := coldAttrs(target, []string{"dev"}, []string{"bus", "tray", "removable", "rotation_rate", "dpofua"}); err != nil {
		return out, err
	}
	out.Target, out.Bus = attr(target, "dev"), attr(target, "bus")
	if len(out.Target) > 128 || !coldDiskTarget.MatchString(out.Target) || len(target.children) != 0 || strings.TrimSpace(target.text) != "" {
		return out, coldInvalid("invalid disk target")
	}
	if !coldEnum(out.Bus, "ide", "fdc", "scsi", "virtio", "xen", "usb", "uml", "sata", "sd", "nvme") {
		return out, coldUnsupported("unrecognized disk bus")
	}
	for _, c := range n.children {
		if _, err := coldChild(n, c.name.Local, false); err != nil {
			return out, err
		}
		switch c.name.Local {
		case "target", "source", "backingStore", "driver":
		case "readonly":
			if !coldSourceEmpty(c) {
				return out, coldInvalid("readonly must be an empty marker")
			}
			out.ReadOnly = true
		case "mirror", "transient", "shareable", "encryption", "auth", "backenddomain", "privateData":
			if err := add("disk-"+c.name.Local, out.Target); err != nil {
				return out, err
			}
		case "alias", "address", "boot", "serial", "wwn", "vendor", "product", "geometry", "blockio", "iotune", "throttlefilters", "acpi":
			if coldSourceContainsExternal(c, false) {
				return out, coldUnsupported("external disk configuration requires a separate adapter")
			}
		default:
			return out, coldUnsupported("unrecognized disk configuration")
		}
	}
	out.ReadOnly = out.ReadOnly || out.Device == "cdrom" // Documented native default.
	format, err := coldSourceFormat(n, "driver")
	if err != nil {
		return out, err
	}
	if out.Source, out.Empty, err = coldSourceStorage(n, format, out.Target, out.Device == "cdrom" || out.Device == "floppy", add); err != nil {
		return out, err
	}
	if out.Device == "lun" && !coldEnum(out.Source.Type, "block", "network", "volume") {
		return out, coldUnsupported("LUN requires block, network or volume storage")
	}
	backing, err := coldChild(n, "backingStore", false)
	if err != nil {
		return out, err
	}
	identities := map[string]bool{}
	indices := map[string]bool{}
	if key := coldSourceKey(out.Source); key != "" {
		identities[key] = true
	}
	for backing != nil {
		if coldSourceEmpty(backing) {
			out.BackingTerminated = true
			break
		}
		if out.Empty || len(out.Backing) >= coldSourceBackingLimit {
			return out, coldInvalid("empty media with backing or backing chain limit")
		}
		if err := coldAttrs(backing, nil, []string{"type", "index"}); err != nil {
			return out, err
		}
		if err := coldSourceIndex(backing); err != nil {
			return out, err
		}
		if index := attr(backing, "index"); index != "" {
			if indices[index] {
				return out, coldInvalid("duplicate backing index")
			}
			indices[index] = true
		}
		for _, c := range backing.children {
			if c.name.Local != "source" && c.name.Local != "format" && c.name.Local != "backingStore" {
				return out, coldUnsupported("unrecognized backing configuration")
			}
		}
		format, err := coldSourceFormat(backing, "format")
		if err != nil {
			return out, err
		}
		location := out.Target + "/backing[" + strconv.Itoa(len(out.Backing)+1) + "]"
		value, _, err := coldSourceStorage(backing, format, location, false, add)
		if err != nil {
			return out, err
		}
		if key := coldSourceKey(value); key != "" {
			if identities[key] {
				return out, coldInvalid("repeated source identity in backing chain")
			}
			identities[key] = true
		}
		out.Backing = append(out.Backing, value)
		if backing, err = coldChild(backing, "backingStore", false); err != nil {
			return out, err
		}
	}
	return out, nil
}

func coldSourceFormat(parent *xmlNode, element string) (string, error) {
	n, err := coldChild(parent, element, false)
	if err != nil || n == nil {
		return "", err
	}
	allowed := []string{"type"}
	if element == "driver" {
		allowed = append(allowed, "name", "cache", "error_policy", "rerror_policy", "io", "ioeventfd", "event_idx", "copy_on_read", "discard", "iothread", "detect_zeroes", "queues", "queue_size", "iommu", "ats", "packed", "page_per_vq", "discard_no_unref")
	}
	var required []string
	if element == "format" {
		required = []string{"type"}
	}
	if err := coldAttrs(n, required, allowed); err != nil {
		return "", err
	}
	if strings.TrimSpace(n.text) != "" {
		return "", coldInvalid("text in disk format declaration")
	}
	for _, c := range n.children {
		if c.name.Space != "" || c.name.Local != "metadata_cache" && !(element == "driver" && c.name.Local == "iothreads") || coldSourceContainsExternal(c, false) {
			return "", coldUnsupported("unrecognized disk driver or format structure")
		}
		if _, err := coldChild(n, c.name.Local, false); err != nil {
			return "", err
		}
	}
	format := attr(n, "type")
	if format != "" && coldSourceIdentifier(format, 64) != nil {
		return "", coldInvalid("invalid disk format identifier")
	}
	return format, nil
}

func coldSourceStorage(parent *xmlNode, format, location string, removable bool, add func(string, string) error) (domain.ColdStorageSource, bool, error) {
	out := domain.ColdStorageSource{Type: attr(parent, "type"), Format: format}
	if out.Type == "" {
		out.Type = "file" // Native diskSourceFile is the schema default.
	}
	if coldSourceIdentifier(out.Type, 64) != nil {
		return out, false, coldInvalid("invalid storage type identifier")
	}
	n, err := coldChild(parent, "source", false)
	if err != nil {
		return out, false, err
	}
	if strings.TrimSpace(parent.text) != "" {
		return out, false, coldInvalid("text in storage declaration")
	}
	emptyMedia := coldSourceEmpty(n)
	if !emptyMedia && removable && (out.Type == "file" || out.Type == "block") && attr(n, "file") == "" && attr(n, "dev") == "" && len(n.children) == 0 && strings.TrimSpace(n.text) == "" {
		// Native empty removable media may retain startup/index metadata. An
		// explicit empty identity attribute is still rejected by this allowlist.
		if err := coldAttrs(n, nil, []string{"index", "startupPolicy"}); err != nil {
			return out, false, err
		}
		if err := coldSourceIndex(n); err != nil {
			return out, false, err
		}
		if !coldEnum(attr(n, "startupPolicy"), "mandatory", "requisite", "optional") {
			return out, false, coldInvalid("invalid empty media startup policy")
		}
		emptyMedia = true
	}
	if emptyMedia {
		if !removable {
			return out, false, coldInvalid("non-removable disk or backing source is empty")
		}
		if out.Type != "file" && out.Type != "volume" {
			return out, true, add(coldSourceDependencyKind(out.Type), location)
		}
		return out, true, nil
	}
	if strings.TrimSpace(n.text) != "" {
		return out, false, coldInvalid("text in storage source")
	}
	if out.Type != "file" && out.Type != "volume" {
		// The XML carries the source details. Do not copy network names, hosts,
		// authentication fields or block paths into a supposedly local source.
		if err := coldSourceOpaque(n); err != nil {
			return out, false, err
		}
		if err := coldSourceUnresolvedIdentity(n, out.Type); err != nil {
			return out, false, err
		}
		return out, false, add(coldSourceDependencyKind(out.Type), location)
	}
	allowed, required := []string{"index", "startupPolicy"}, []string{"file"}
	if out.Type == "file" {
		allowed = append(allowed, "fdgroup")
	} else {
		required, allowed = []string{"pool", "volume"}, append(allowed, "mode")
	}
	if err := coldAttrs(n, required, allowed); err != nil {
		return out, false, err
	}
	if err := coldSourceIndex(n); err != nil {
		return out, false, err
	}
	if !coldEnum(attr(n, "startupPolicy"), "mandatory", "requisite", "optional") || !coldEnum(attr(n, "mode"), "host", "direct") {
		return out, false, coldInvalid("invalid source startup policy or volume mode")
	}
	if out.Type == "file" {
		out.File = attr(n, "file")
		if err := coldPath(out.File); err != nil {
			return out, false, err
		}
	} else {
		out.Pool, out.Volume = attr(n, "pool"), attr(n, "volume")
		if coldSourceIdentifier(out.Pool, 256) != nil || coldSourceIdentifier(out.Volume, 256) != nil || out.Pool == "." || out.Pool == ".." || out.Volume == "." || out.Volume == ".." {
			return out, false, coldUnsupported("pool or volume name outside bounded identifier baseline")
		}
	}
	for _, a := range n.attrs {
		if a.Name.Local == "fdgroup" || a.Name.Local == "mode" {
			if err := add("storage-"+a.Name.Local, location); err != nil {
				return out, false, err
			}
		}
	}
	for _, c := range n.children {
		if c.name.Space != "" {
			return out, false, coldInvalid("foreign storage source element")
		}
		if c.name.Local != "seclabel" {
			if _, err := coldChild(n, c.name.Local, false); err != nil {
				return out, false, err
			}
		}
		switch c.name.Local {
		case "seclabel":
		case "encryption", "slices", "dataStore", "privateData":
			if err := add("storage-"+c.name.Local, location); err != nil {
				return out, false, err
			}
		default:
			return out, false, coldUnsupported("unrecognized local storage source structure")
		}
	}
	return out, false, nil
}

func coldSourceKey(value domain.ColdStorageSource) string {
	if value.File != "" {
		return "file:" + value.File
	}
	if value.Pool != "" && value.Volume != "" {
		return "volume:" + value.Pool + "/" + value.Volume
	}
	return ""
}

func coldSourceDependencyKind(kind string) string {
	if coldEnum(kind, "block", "network", "dir", "nvme", "vhostuser", "vhostvdpa", "ctl") {
		return "storage-" + kind
	}
	return "storage-unsupported"
}

func coldSourceIndex(n *xmlNode) error {
	if value := attr(n, "index"); value != "" {
		index, err := strconv.ParseUint(value, 10, 32)
		if err != nil || index == 0 || strconv.FormatUint(index, 10) != value {
			return coldInvalid("invalid storage source index")
		}
	}
	return nil
}

func coldSourceUnresolvedIdentity(n *xmlNode, kind string) error {
	identity := ""
	switch kind {
	case "block", "vhostvdpa", "ctl":
		identity = "dev"
	case "dir":
		identity = "dir"
	case "network":
		identity = "protocol"
	case "vhostuser":
		identity = "path"
	case "nvme":
		identity = "type"
	default:
		return nil // Future native kind remains explicitly unresolved.
	}
	if attr(n, identity) == "" {
		return coldInvalid("missing external storage identity")
	}
	for _, a := range n.attrs {
		if coldEnum(a.Name.Local, "file", "pool", "volume", "dev", "dir", "path", "protocol", "fdgroup") && a.Name.Local != identity {
			return coldInvalid("competing external storage identities")
		}
	}
	if identity == "dev" || identity == "dir" || identity == "path" {
		return coldPath(attr(n, identity))
	}
	return coldSourceIdentifier(attr(n, identity), 64)
}

// Unsupported native source bodies are never projected. Still refuse namespace
// substitution and competing singleton identities rather than calling them an
// unambiguous dependency. Repeated native hosts/seclabels/cookies are legitimate.
func coldSourceOpaque(n *xmlNode) error {
	for _, a := range n.attrs {
		if a.Name.Space != "" || coldScalar(a.Value, 4096) != nil || a.Value == "" {
			return coldInvalid("foreign or malformed external storage attribute")
		}
	}
	seen := map[string]bool{}
	for _, c := range n.children {
		if c.name.Space != "" {
			return coldInvalid("foreign external storage element")
		}
		if c.name.Local != "host" && c.name.Local != "seclabel" && c.name.Local != "cookie" {
			if seen[c.name.Local] {
				return coldInvalid("duplicate external storage element")
			}
			seen[c.name.Local] = true
		}
		if err := coldSourceOpaque(c); err != nil {
			return err
		}
	}
	return nil
}

// Configuration-only classification is deliberately narrower than the native
// schema. A new attribute, child, backend or namespace remains a dependency.
// This is not native validation or permission to capture device runtime state.
func coldSourceConfigDevice(n *xmlNode) bool {
	// These stopped-device forms have no caller-selected persistent files.
	// Capture records the fixed system emulator's hash and native versions;
	// fresh runtime PTYs and automatically assigned VNC sockets are recreated
	// by libvirt. Any explicit socket/file, backend or extension remains external.
	if coldSourceEphemeralDevice(n) {
		return true
	}
	var attrs []string
	children := ""
	switch n.name.Local {
	case "controller":
		if !coldEnum(attr(n, "type"), "pci", "usb", "scsi", "sata", "ide", "fdc", "ccid", "isa", "virtio-serial") || attr(n, "type") == "" {
			return false
		}
		attrs, children = strings.Fields("type index model ports vectors"), "model target master driver"
	case "input":
		if !coldEnum(attr(n, "type"), "mouse", "tablet", "keyboard") || attr(n, "type") == "" {
			return false
		}
		attrs, children = strings.Fields("type bus model"), "driver"
	case "video":
		children = "driver model"
	case "memballoon":
		attrs, children = strings.Fields("model autodeflate freePageReporting"), "stats driver"
	case "watchdog":
		attrs = strings.Fields("model action")
	case "panic":
		attrs = []string{"model"}
	case "hub":
		if attr(n, "type") != "usb" {
			return false
		}
		attrs = []string{"type"}
	case "sound":
		attrs, children = strings.Fields("model multichannel streams"), "audio codec driver"
	case "audio":
		return attr(n, "type") == "none" && coldSourceConfigLeaf(n, "id type")
	default:
		return false
	}
	if n.name.Space != "" || coldAttrs(n, nil, attrs) != nil || strings.TrimSpace(n.text) != "" {
		return false
	}
	for _, a := range n.attrs {
		if coldSourceIdentifier(a.Value, 256) != nil {
			return false
		}
	}
	seen := map[string]bool{}
	for _, c := range n.children {
		if c.name.Space != "" || seen[c.name.Local] && !(n.name.Local == "sound" && c.name.Local == "codec") {
			return false
		}
		seen[c.name.Local] = true
		switch c.name.Local {
		case "alias":
			if !coldSourceConfigLeaf(c, "name") {
				return false
			}
		case "address":
			if !coldSourceConfigLeaf(c, "type domain bus slot function multifunction controller target unit port cssid ssid devno reg iobase irq base") {
				return false
			}
		case "acpi":
			if !coldSourceConfigLeaf(c, "index") {
				return false
			}
		default:
			if !strings.Contains(" "+children+" ", " "+c.name.Local+" ") {
				return false
			}
			switch c.name.Local {
			case "driver":
				if attr(c, "name") != "" && attr(c, "name") != "qemu" || !coldSourceConfigLeaf(c, "name queues cmd_per_lun max_sectors iothread ioeventfd iommu ats packed page_per_vq vgaconf") {
					return false
				}
			case "model":
				if n.name.Local == "controller" {
					if !coldSourceConfigLeaf(c, "name") {
						return false
					}
				} else if !coldSourceConfigVideoModel(c) {
					return false
				}
			case "target":
				if !coldSourceConfigLeaf(c, "chassisNr chassis port busNr index hotplug memReserve") {
					return false
				}
			case "master":
				if !coldSourceConfigLeaf(c, "startport") {
					return false
				}
			case "stats":
				if !coldSourceConfigLeaf(c, "period") {
					return false
				}
			case "codec":
				if !coldSourceConfigLeaf(c, "type cad") {
					return false
				}
			case "audio":
				if !coldSourceConfigLeaf(c, "id") {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}

func coldSourceEphemeralDevice(n *xmlNode) bool {
	if n.name.Space != "" {
		return false
	}
	switch n.name.Local {
	case "emulator":
		return len(n.attrs) == 0 && len(n.children) == 0 && strings.TrimSpace(n.text) == "/usr/bin/qemu-system-x86_64"
	case "graphics":
		if coldAttrs(n, []string{"type"}, nil) != nil || attr(n, "type") != "vnc" || strings.TrimSpace(n.text) != "" || len(n.children) != 1 {
			return false
		}
		listen := n.children[0]
		return listen.name.Space == "" && listen.name.Local == "listen" && coldSourceConfigLeaf(listen, "type") && len(listen.attrs) == 1 && attr(listen, "type") == "socket"
	case "serial", "console":
		if coldAttrs(n, []string{"type"}, nil) != nil || attr(n, "type") != "pty" || strings.TrimSpace(n.text) != "" || len(n.children) != 1 {
			return false
		}
		target := n.children[0]
		if target.name.Space != "" || target.name.Local != "target" || coldAttrs(target, []string{"type", "port"}, nil) != nil || strings.TrimSpace(target.text) != "" || attr(target, "port") != "0" {
			return false
		}
		if n.name.Local == "console" {
			return attr(target, "type") == "serial" && len(target.children) == 0
		}
		if attr(target, "type") != "isa-serial" || len(target.children) != 1 {
			return false
		}
		model := target.children[0]
		return model.name.Space == "" && model.name.Local == "model" && coldSourceConfigLeaf(model, "name") && len(model.attrs) == 1 && attr(model, "name") == "isa-serial"
	}
	return false
}

func coldSourceConfigLeaf(n *xmlNode, attrs string) bool {
	if n.name.Space != "" || len(n.children) != 0 || strings.TrimSpace(n.text) != "" || coldAttrs(n, nil, strings.Fields(attrs)) != nil {
		return false
	}
	for _, a := range n.attrs {
		if coldSourceIdentifier(a.Value, 256) != nil {
			return false
		}
	}
	return true
}

func coldSourceConfigVideoModel(n *xmlNode) bool {
	if coldAttrs(n, []string{"type"}, strings.Fields("ram vgamem vram64 vram heads primary blob edid")) != nil || strings.TrimSpace(n.text) != "" || !coldEnum(attr(n, "blob"), "off") || !coldEnum(attr(n, "type"), "vga", "cirrus", "vmvga", "xen", "vbox", "virtio", "gop", "none", "bochs", "ramfb", "qxl") {
		return false
	}
	for _, a := range n.attrs {
		if coldSourceIdentifier(a.Value, 256) != nil {
			return false
		}
	}
	seen := map[string]bool{}
	for _, c := range n.children {
		if seen[c.name.Local] {
			return false
		}
		seen[c.name.Local] = true
		switch c.name.Local {
		case "acceleration":
			if !coldSourceConfigLeaf(c, "accel2d accel3d") || !coldEnum(attr(c, "accel2d"), "no") || !coldEnum(attr(c, "accel3d"), "no") {
				return false
			}
		case "resolution":
			if !coldSourceConfigLeaf(c, "x y") {
				return false
			}
		default:
			return false
		}
	}
	return true
}
