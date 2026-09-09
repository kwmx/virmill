//go:build linux && cgo

package libvirt

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
)

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func diskSuffix(n int) string {
	out := ""
	for {
		out = string(rune('a'+n%26)) + out
		n = n/26 - 1
		if n < 0 {
			return out
		}
	}
}

// creationXML only generates new domains. It is never used to edit/adopt an
// existing domain or replace XML containing unknown configuration.
func creationXML(t domain.CreationTarget, volumes []domain.CreatedVolume, binding string) (string, error) {
	s := t.Spec
	if err := s.DevicePolicy.Validate(s.Machine); err != nil {
		return "", err
	}
	if s.GuestAgent && s.DevicePolicy == nil {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "guest-agent channel requires an explicit automatic device-placement policy")
	}
	if len(volumes) != len(s.Disks)+len(s.Media) {
		return "", errors.New("complete volume set required")
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<domain type="kvm"><name>%s</name><uuid>%s</uuid><metadata><virmill:creation xmlns:virmill="urn:virmill:v1" apiVersion="virmill/v1" binding="%s"/></metadata><memory unit="KiB">%d</memory><currentMemory unit="KiB">%d</currentMemory><vcpu placement="static">%d</vcpu>`, xmlText(s.Name), xmlText(s.UUID), xmlText(binding), s.MemoryMiB*1024, s.MemoryMiB*1024, s.VCPUs)
	fmt.Fprintf(&b, `<os><type arch="x86_64" machine="%s">hvm</type>`, xmlText(s.Machine))
	if s.Firmware.Mode == "uefi" {
		secure := "no"
		if s.Firmware.SecureBoot {
			secure = "yes"
		}
		fmt.Fprintf(&b, `<loader readonly="yes" type="pflash" secure="%s" format="%s">%s</loader><nvram template="%s" templateFormat="%s" format="%s"/>`, secure, xmlText(s.Firmware.Format), xmlText(s.Firmware.Code), xmlText(s.Firmware.Template), xmlText(s.Firmware.Format), xmlText(s.Firmware.Format))
	}
	b.WriteString(`</os><features><acpi/><apic/>`)
	if s.Firmware.SecureBoot {
		b.WriteString(`<smm state="on"/>`)
	}
	b.WriteString(`</features>`)
	fmt.Fprintf(&b, `<cpu mode="%s">`, xmlText(s.CPU.Mode))
	if s.CPU.Mode == "custom" {
		fmt.Fprintf(&b, `<model fallback="forbid">%s</model>`, xmlText(s.CPU.Model))
	}
	fmt.Fprintf(&b, `</cpu><clock offset="%s"/><on_poweroff>destroy</on_poweroff><on_reboot>restart</on_reboot><on_crash>preserve</on_crash><devices><emulator>%s</emulator>`, xmlText(s.Clock), xmlText(t.Emulator))
	sata, scsi := 0, 0
	for _, d := range s.Disks {
		if d.Bus == "sata" {
			sata++
		}
		if d.Bus == "scsi" {
			scsi++
		}
	}
	for _, m := range s.Media {
		if m.Bus == "sata" {
			sata++
		}
		if m.Bus == "scsi" {
			scsi++
		}
	}
	if s.DevicePolicy != nil {
		pci := "pci-root"
		if s.DevicePolicy.Chipset == "q35" {
			pci = "pcie-root"
		} else {
			b.WriteString(`<controller type="ide" index="0"/>`)
		}
		fmt.Fprintf(&b, `<controller type="pci" index="0" model="%s"/><controller type="usb" index="0" model="%s"/>`, pci, s.DevicePolicy.USBController)
	}
	if sata > 0 || (s.DevicePolicy != nil && s.DevicePolicy.Chipset == "q35") {
		b.WriteString(`<controller type="sata" index="0"/>`)
	}
	if scsi > 0 {
		b.WriteString(`<controller type="scsi" index="0" model="virtio-scsi"/>`)
	}
	if s.GuestAgent {
		b.WriteString(creationGuestAgentDevices)
	}
	sata, scsi = 0, 0
	for i, d := range s.Disks {
		v := volumes[i]
		if v.Intent.SourceID != d.SourceID || v.Intent.PoolID != s.PoolID || v.Intent.ContentType != "" {
			return "", errors.New("volume/controller mapping differs")
		}
		prefix := "sd"
		if d.Bus == "virtio" {
			prefix = "vd"
		}
		fmt.Fprintf(&b, `<disk type="volume" device="disk"><driver name="qemu" type="qcow2" cache="writethrough" error_policy="stop"/><source pool="%s" volume="%s"/><target dev="%s%s" bus="%s"/><boot order="%d"/>`, xmlText(t.PoolName), xmlText(v.Intent.Name), prefix, diskSuffix(i), xmlText(d.Bus), d.BootOrder)
		if d.Bus == "sata" {
			fmt.Fprintf(&b, `<address type="drive" controller="0" bus="0" target="0" unit="%d"/>`, sata)
			sata++
		}
		if d.Bus == "scsi" {
			fmt.Fprintf(&b, `<address type="drive" controller="0" bus="0" target="0" unit="%d"/>`, scsi)
			scsi++
		}
		b.WriteString(`</disk>`)
	}
	for i, m := range s.Media {
		v := volumes[len(s.Disks)+i]
		if v.Intent.SourceID != m.SourceID || v.Intent.PoolID != s.PoolID || v.Intent.ContentType != "cdrom-iso" || (m.Bus != "sata" && m.Bus != "scsi") {
			return "", errors.New("read-only media mapping differs")
		}
		fmt.Fprintf(&b, `<disk type="volume" device="cdrom"><driver name="qemu" type="raw" cache="writethrough" error_policy="stop"/><source pool="%s" volume="%s"/><target dev="sd%s" bus="%s"/><readonly/>`, xmlText(t.PoolName), xmlText(v.Intent.Name), diskSuffix(len(s.Disks)+i), xmlText(m.Bus))
		if m.BootOrder > 0 {
			fmt.Fprintf(&b, `<boot order="%d"/>`, m.BootOrder)
		}
		unit := scsi
		if m.Bus == "sata" {
			unit = sata
			sata++
		} else {
			scsi++
		}
		fmt.Fprintf(&b, `<address type="drive" controller="0" bus="0" target="0" unit="%d"/></disk>`, unit)
	}
	networks := map[string]string{}
	for _, n := range t.Networks {
		networks[n.Key.UUID] = n.Name
	}
	for _, n := range s.NICs {
		name, ok := networks[n.NetworkID]
		if !ok {
			return "", errors.New("selected network missing")
		}
		fmt.Fprintf(&b, `<interface type="network"><mac address="%s"/><source network="%s"/><model type="%s"/><link state="%s"/></interface>`, xmlText(n.MAC), xmlText(name), xmlText(n.Model), xmlText(n.Link))
	}
	if s.Firmware.TPM {
		b.WriteString(`<tpm model="tpm-crb"><backend type="emulator" version="2.0" persistent_state="yes"/></tpm>`)
	}
	if s.Graphics == "vnc-unix" {
		b.WriteString(`<graphics type="vnc"><listen type="socket"/></graphics><video><model type="vga"/></video>`)
	}
	if s.DevicePolicy != nil {
		b.WriteString(`<input type="mouse" bus="ps2"/><input type="keyboard" bus="ps2"/><audio id="1" type="none"/><serial type="pty"><target type="isa-serial" port="0"><model name="isa-serial"/></target></serial>`)
		if s.DevicePolicy.Chipset == "q35" {
			fmt.Fprintf(&b, `<watchdog model="itco" action="%s"/>`, s.DevicePolicy.WatchdogAction)
		}
		fmt.Fprintf(&b, `<memballoon model="%s"/>`, s.DevicePolicy.MemoryBalloon)
	} else {
		b.WriteString(`<serial type="pty"><target port="0"/></serial>`)
	}
	b.WriteString(`<console type="pty"><target type="serial" port="0"/></console></devices></domain>`)
	return b.String(), xmlpatch.Validate(b.String())
}

type xmlNode struct {
	name     xml.Name
	attrs    []xml.Attr
	text     string
	children []*xmlNode
}

func xmlTree(data string) (*xmlNode, error) {
	if err := xmlpatch.Validate(data); err != nil {
		return nil, err
	}
	d := xml.NewDecoder(strings.NewReader(data))
	var root *xmlNode
	var stack []*xmlNode
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch v := token.(type) {
		case xml.StartElement:
			n := &xmlNode{name: v.Name, attrs: v.Attr}
			if len(stack) == 0 {
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, n)
			}
			stack = append(stack, n)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(v)
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		}
	}
	return root, nil
}

func verifyVolumeMetadata(data string, expected domain.VolumeIntent) error {
	root, err := xmlTree(data)
	if err != nil {
		return err
	}
	if root == nil || root.name != (xml.Name{Local: "volume"}) {
		return errors.New("invalid volume metadata root")
	}
	capacity, target := child(root, "capacity"), child(root, "target")
	if capacity == nil || target == nil || child(target, "format") == nil {
		return errors.New("volume capacity/format missing")
	}
	value, err := strconv.ParseUint(strings.TrimSpace(capacity.text), 10, 64)
	if err != nil {
		return err
	}
	format := "qcow2"
	observedFormat := attr(child(target, "format"), "type")
	if expected.ContentType == "cdrom-iso" {
		format = "raw"
		// libvirt may identify ISO9660 bytes as iso after pool refresh. Both
		// describe the same raw CD-ROM bytes; SHA-256 and exact size still bind them.
		if observedFormat == "iso" {
			observedFormat = "raw"
		}
	} else if expected.ContentType != "" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "unknown volume content type")
	}
	if (attr(capacity, "unit") != "bytes" && attr(capacity, "unit") != "") || value != expected.VirtualBytes || observedFormat != format {
		return domain.Fail("RECOVERY_REQUIRED", "uploaded volume format or capacity differs")
	}
	for _, n := range root.children {
		if n.name.Local == "backingStore" && (len(n.attrs) != 0 || len(n.children) != 0 || strings.TrimSpace(n.text) != "") {
			return domain.Fail("RECOVERY_REQUIRED", "uploaded volume has unexpected backing metadata")
		}
	}
	return nil
}
func attr(n *xmlNode, name string) string {
	for _, a := range n.attrs {
		if a.Name.Local == name && a.Name.Space == "" {
			return a.Value
		}
	}
	return ""
}
func child(n *xmlNode, name string) *xmlNode {
	for _, c := range n.children {
		if c.name.Local == name && c.name.Space == "" {
			return c
		}
	}
	return nil
}
func matchesNode(want, got *xmlNode) bool {
	return matchesNodeAt(want, got, "")
}
func matchesNodeAt(want, got *xmlNode, parent string) bool {
	if want == nil || got == nil || want.name != got.name {
		return false
	}
	path := parent + "/" + want.name.Local
	for _, a := range want.attrs {
		if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
			continue
		}
		found := false
		for _, b := range got.attrs {
			if a.Name == b.Name && a.Value == b.Value {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, a := range got.attrs {
		if a.Name.Space == "xmlns" || a.Name.Local == "xmlns" {
			continue
		}
		found := false
		for _, b := range want.attrs {
			if a.Name == b.Name {
				found = true
				break
			}
		}
		if !found && !creationDefaultAttr(path, a) {
			return false
		}
	}
	if path == "/domain/os/nvram" {
		// Keep this strictness local to NVRAM; attribute lookup above alone does
		// not distinguish duplicate names or ignored namespace declarations.
		seen := map[xml.Name]bool{}
		for _, a := range got.attrs {
			if a.Name.Space != "" || a.Name.Local == "xmlns" || seen[a.Name] {
				return false
			}
			seen[a.Name] = true
		}
		// Empty-to-empty is unresolved structural equality, not an assigned file.
		// Validate raw decoded text so whitespace cannot hide an invalid path,
		// including when a nonempty wanted path happens to match it exactly.
		if got.text != "" && coldPath(got.text) != nil {
			return false
		}
		if want.text != "" && want.text != got.text {
			return false
		}
	} else if strings.TrimSpace(want.text) != strings.TrimSpace(got.text) {
		return false
	}
	used := map[int]bool{}
	for _, w := range want.children {
		found := false
		for i, g := range got.children {
			if !used[i] && matchesNodeAt(w, g, path) {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for i, g := range got.children {
		if !used[i] && !creationDefaultChild(path, g) {
			return false
		}
	}
	return true
}

// Only known backend normalization is accepted. New semantics require an adapter
// update and evidence; unknown extra configuration must not certify completion.
func creationDefaultAttr(path string, a xml.Attr) bool {
	if a.Name.Space != "" {
		return false
	}
	switch path + "/@" + a.Name.Local {
	case "/domain/devices/video/model/@vram":
		return a.Value == "16384"
	case "/domain/devices/video/model/@heads":
		return a.Value == "1"
	case "/domain/devices/video/model/@primary":
		return a.Value == "yes"
	case "/domain/cpu/@check":
		return a.Value == "none" || a.Value == "partial"
	case "/domain/cpu/@migratable":
		return a.Value == "on"
	}
	return false
}
func creationDefaultChild(parent string, n *xmlNode) bool {
	if n.name.Space != "" || strings.TrimSpace(n.text) != "" || len(n.children) != 0 {
		return false
	}
	if parent == "/domain/devices/disk" && n.name.Local == "backingStore" {
		return len(n.attrs) == 0
	}
	if parent == "/domain/devices/interface" && n.name.Local == "target" {
		return len(n.attrs) == 1 && n.attrs[0].Name == (xml.Name{Local: "dev"}) && n.attrs[0].Value != ""
	}
	if parent == "/domain/devices/disk" || parent == "/domain/devices/interface" || parent == "/domain/devices/controller" || parent == "/domain/devices/video" || parent == "/domain/devices/serial" || parent == "/domain/devices/console" || parent == "/domain/devices/tpm" {
		if n.name.Local == "alias" {
			return len(n.attrs) == 1 && n.attrs[0].Name == (xml.Name{Local: "name"}) && n.attrs[0].Value != ""
		}
		if n.name.Local == "address" && attr(n, "type") == "pci" {
			for _, a := range n.attrs {
				if a.Name.Space != "" || (a.Name.Local != "type" && a.Name.Local != "domain" && a.Name.Local != "bus" && a.Name.Local != "slot" && a.Name.Local != "function" && a.Name.Local != "multifunction") {
					return false
				}
			}
			return true
		}
	}
	return false
}
func memoryKiB(n *xmlNode) error {
	for _, key := range []string{"memory", "currentMemory"} {
		c := child(n, key)
		if c == nil {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimSpace(c.text), 10, 64)
		if err != nil {
			return err
		}
		switch attr(c, "unit") {
		case "", "KiB", "k":
		case "MiB", "M":
			if value > 1<<40 {
				return errors.New("memory size overflow")
			}
			value *= 1024
		default:
			return errors.New("unsupported normalized memory unit")
		}
		c.text = strconv.FormatUint(value, 10)
		c.attrs = []xml.Attr{{Name: xml.Name{Local: "unit"}, Value: "KiB"}}
	}
	return nil
}
func matchesCreation(wanted, observed string) error {
	return matchesCreationPolicy(wanted, observed, nil)
}
func matchesCreationPolicy(wanted, observed string, policy *domain.CreationDevicePolicy) error {
	w, err := xmlTree(wanted)
	if err != nil {
		return err
	}
	g, err := xmlTree(observed)
	if err != nil {
		return err
	}
	if err = memoryKiB(w); err != nil {
		return err
	}
	if err = memoryKiB(g); err != nil {
		return err
	}
	if err = normalizeCreationGuestAgent(w, g); err != nil {
		return err
	}
	if policy != nil {
		if err = normalizeCreationPCI(w, g, policy); err != nil {
			return err
		}
	}
	normalizeCreationNonSecureFirmware(w, g)
	if !matchesNode(w, g) {
		return domain.Fail("RECOVERY_REQUIRED", "defined configuration differs from reviewed creation intent")
	}
	wdev, gdev := child(w, "devices"), child(g, "devices")
	if wdev == nil || gdev == nil {
		return errors.New("domain devices missing")
	}
	for _, kind := range []string{"disk", "interface", "hostdev", "filesystem", "tpm", "channel", "graphics", "serial", "console"} {
		a, b := 0, 0
		for _, c := range wdev.children {
			if c.name.Local == kind {
				a++
			}
		}
		for _, c := range gdev.children {
			if c.name.Local == kind {
				b++
			}
		}
		if a != b {
			return domain.Fail("RECOVERY_REQUIRED", "defined domain contains an unexpected "+kind+" device")
		}
	}
	return nil
}

// Captured libvirt normalization adds EFI selection metadata to an already
// pinned non-secure pflash mapping. Remove only that exact redundant cohort
// from the private comparison tree. Rendering, the full observed XML and the
// existing NVRAM path comparison remain unchanged. This proves no key state or
// NVRAM freshness, and must not be generalized to Secure Boot enabled firmware.
func normalizeCreationNonSecureFirmware(w, g *xmlNode) {
	if w == nil || g == nil || w.name != (xml.Name{Local: "domain"}) || g.name != w.name || !creationFirmwareExactAttrs(w, map[string]string{"type": "kvm"}) || !creationFirmwareExactAttrs(g, map[string]string{"type": "kvm"}) {
		return
	}
	unique := func(parent *xmlNode, name string) *xmlNode {
		var found *xmlNode
		for _, c := range parent.children {
			if c.name.Local == name {
				if found != nil || c.name.Space != "" {
					return nil
				}
				found = c
			}
		}
		return found
	}
	wos, gos := unique(w, "os"), unique(g, "os")
	if wos == nil || gos == nil || !creationFirmwareExactAttrs(wos, nil) || !creationFirmwareExactAttrs(gos, map[string]string{"firmware": "efi"}) || strings.TrimSpace(wos.text) != "" || strings.TrimSpace(gos.text) != "" || len(wos.children) != 3 || len(gos.children) != 4 {
		return
	}
	wt, gt := unique(wos, "type"), unique(gos, "type")
	wl, gl := unique(wos, "loader"), unique(gos, "loader")
	wn, gn := unique(wos, "nvram"), unique(gos, "nvram")
	firmware := unique(gos, "firmware")
	if wt == nil || gt == nil || wl == nil || gl == nil || wn == nil || gn == nil || firmware == nil {
		return
	}
	typeAttrs := map[string]string{"arch": "x86_64", "machine": attr(wt, "machine")}
	format := attr(wl, "format")
	loaderAttrs := map[string]string{"readonly": "yes", "secure": "no", "type": "pflash", "format": format}
	nvramAttrs := map[string]string{"template": attr(wn, "template"), "templateFormat": format, "format": format}
	if attr(wt, "machine") == "" || strings.TrimSpace(wt.text) != "hvm" || strings.TrimSpace(gt.text) != "hvm" || !creationFirmwareExactAttrs(wt, typeAttrs) || !creationFirmwareExactAttrs(gt, typeAttrs) || len(wt.children) != 0 || len(gt.children) != 0 {
		return
	}
	if format != "raw" && format != "qcow2" || !creationFirmwareExactAttrs(wl, loaderAttrs) || !creationFirmwareExactAttrs(gl, loaderAttrs) || len(wl.children) != 0 || len(gl.children) != 0 || !creationFirmwarePinnedPath(wl.text) || gl.text != wl.text {
		return
	}
	if !creationFirmwareExactAttrs(wn, nvramAttrs) || !creationFirmwareExactAttrs(gn, nvramAttrs) || len(wn.children) != 0 || len(gn.children) != 0 || !creationFirmwarePinnedPath(attr(wn, "template")) || !matchesNodeAt(wn, gn, "/domain/os") {
		return
	}
	if !creationFirmwareExactAttrs(firmware, nil) || strings.TrimSpace(firmware.text) != "" || len(firmware.children) != 2 {
		return
	}
	features := map[string]bool{}
	for _, feature := range firmware.children {
		name := attr(feature, "name")
		if feature.name != (xml.Name{Local: "feature"}) || name != "enrolled-keys" && name != "secure-boot" || features[name] || !creationFirmwareExactAttrs(feature, map[string]string{"name": name, "enabled": "no"}) || len(feature.children) != 0 || strings.TrimSpace(feature.text) != "" {
			return
		}
		features[name] = true
	}
	gos.attrs = nil
	kept := make([]*xmlNode, 0, 3)
	for _, c := range gos.children {
		if c != firmware {
			kept = append(kept, c)
		}
	}
	gos.children = kept
}

func creationFirmwareExactAttrs(n *xmlNode, expected map[string]string) bool {
	if n == nil || n.name.Space != "" || len(n.attrs) != len(expected) {
		return false
	}
	seen := map[string]bool{}
	for _, a := range n.attrs {
		value, ok := expected[a.Name.Local]
		if a.Name.Space != "" || !ok || seen[a.Name.Local] || a.Value != value {
			return false
		}
		seen[a.Name.Local] = true
	}
	return true
}

func creationFirmwarePinnedPath(value string) bool {
	return value != "/" && path.IsAbs(value) && path.Clean(value) == value && strings.TrimSpace(value) == value
}
