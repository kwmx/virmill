package tui

import (
	"encoding/xml"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"virmill.local/core/internal/backend/xmlpatch"
)

// tableColumn is a list column shown after NAME and STATE.
type tableColumn struct {
	title string
	width int
	value func(any) string
}

// clock is replaced in tests so relative job times are stable.
var clock = time.Now

func (m Workspace) tableColumns() []tableColumn {
	switch m.Section {
	case 0, 1:
		return []tableColumn{{"CPU", 3, func(v any) string { return vmSpec(v)[0] }}, {"RAM", 9, func(v any) string { return vmSpec(v)[1] }}}
	case 2:
		return []tableColumn{{"TYPE", 8, func(v any) string { return networkView(v).kind() }}, {"ADDRESS", 18, func(v any) string { return networkView(v).address() }}}
	case 3:
		return []tableColumn{{"SIZE", 10, func(v any) string { return optionalSize(object(v)["capacityBytes"]) }},
			{"FREE", 10, func(v any) string { return optionalSize(object(v)["availableBytes"]) }}}
	case 8:
		return []tableColumn{{"STARTED", 11, func(v any) string { return ago(field(v, "createdAt"), clock()) }}}
	}
	return nil
}

// tableLines renders the header and visible rows. Columns that do not fit
// beside a readable name are dropped from the right.
func (m Workspace) tableLines(rows []any, selected, width, count int) []string {
	columns := m.tableColumns()
	stateWidth := 12
	if m.Section == 8 {
		stateWidth = 17
	}
	fixed := func() int {
		n := 4 + stateWidth
		for _, c := range columns {
			n += 2 + c.width
		}
		return n
	}
	for len(columns) > 0 && width-fixed() < 16 {
		columns = columns[:len(columns)-1]
	}
	name := max(8, width-fixed())
	header := "  " + padCell("NAME", name) + "  " + padCell("STATE", stateWidth)
	for _, c := range columns {
		header += "  " + padCell(c.title, c.width)
	}
	lines := []string{m.color(header, "2")}
	start := max(0, selected-count+1)
	for i := start; i < min(len(rows), start+count); i++ {
		marker := "  "
		if i == m.Selected {
			marker = "> "
		}
		line := marker + padCell(m.nameCell(rows[i]), name) + "  " + padCell(m.stateCell(rows[i]), stateWidth)
		for _, c := range columns {
			line += "  " + padCell(c.value(rows[i]), c.width)
		}
		if i == m.Selected {
			line = m.color(line, "1;30;46")
		}
		lines = append(lines, line)
	}
	return lines
}

func (m Workspace) nameCell(v any) string {
	if m.Section == 8 {
		return m.jobName(v)
	}
	return rowName(v)
}

func (m Workspace) stateCell(v any) string {
	if m.Section == 3 {
		return poolState(v)
	}
	return rowState(v)
}

// libvirt reports a started pool as "running"; networks say "active".
func poolState(v any) string {
	if state := rowState(v); state != "running" {
		return state
	}
	return "active"
}

var vmSpecs sync.Map

// vmSpec reads next-boot CPU and RAM from the VM's persistent XML.
func vmSpec(v any) [2]string {
	key := resourceID(v) + "|" + field(v, "fingerprint")
	if cached, ok := vmSpecs.Load(key); ok && field(v, "fingerprint") != "" {
		return cached.([2]string)
	}
	spec := [2]string{"-", "-"}
	if values, err := xmlpatch.ReadResourceValues(field(v, "persistentXML")); err == nil {
		if values.VCPUs != nil {
			spec[0] = fmt.Sprint(*values.VCPUs)
		}
		if values.MemoryBytes != nil {
			spec[1] = memoryText(*values.MemoryBytes)
		}
	}
	if field(v, "fingerprint") != "" {
		vmSpecs.Store(key, spec)
	}
	return spec
}

func memoryText(b uint64) string {
	const mib, gib = 1 << 20, 1 << 30
	switch {
	case b >= gib && b%gib == 0:
		return fmt.Sprintf("%d GiB", b/gib)
	case b >= gib:
		return fmt.Sprintf("%.1f GiB", float64(b)/gib)
	}
	return fmt.Sprintf("%d MiB", b/mib)
}

func optionalSize(v any) string {
	if _, ok := sizeNumber(v); !ok {
		return "-"
	}
	return sizeText(v)
}

type networkXML struct {
	Forward *struct {
		Mode string `xml:"mode,attr"`
	} `xml:"forward"`
	Bridge struct {
		Name string `xml:"name,attr"`
	} `xml:"bridge"`
	IPs []struct {
		Family  string `xml:"family,attr"`
		Address string `xml:"address,attr"`
		Netmask string `xml:"netmask,attr"`
		Prefix  string `xml:"prefix,attr"`
		Ranges  []struct {
			Start string `xml:"start,attr"`
			End   string `xml:"end,attr"`
		} `xml:"dhcp>range"`
	} `xml:"ip"`
}

// networkView reads the persistent network XML, or the live XML when there is none.
func networkView(v any) networkXML {
	var n networkXML
	raw := field(v, "persistentXML")
	if raw == "" {
		raw = field(v, "liveXML")
	}
	if len(raw) <= 1<<20 {
		_ = xml.Unmarshal([]byte(raw), &n)
	}
	return n
}

func (n networkXML) kind() string {
	if n.Forward == nil {
		return "isolated"
	}
	switch n.Forward.Mode {
	case "", "nat":
		return "NAT"
	case "route":
		return "routed"
	}
	return n.Forward.Mode
}

func (n networkXML) address() string {
	for _, ip := range n.IPs {
		if ip.Family != "" && ip.Family != "ipv4" {
			continue
		}
		prefix := ip.Prefix
		if mask := net.ParseIP(ip.Netmask).To4(); prefix == "" && mask != nil {
			ones, _ := net.IPv4Mask(mask[0], mask[1], mask[2], mask[3]).Size()
			prefix = fmt.Sprint(ones)
		}
		if prefix == "" {
			return ip.Address
		}
		return ip.Address + "/" + prefix
	}
	return "-"
}

func (n networkXML) dhcpRange() string {
	for _, ip := range n.IPs {
		if len(ip.Ranges) > 0 && (ip.Family == "" || ip.Family == "ipv4") {
			return ip.Ranges[0].Start + " to " + ip.Ranges[0].End
		}
	}
	return ""
}

var networkKinds = map[string]string{
	"NAT":      "NAT (VMs reach outside networks through this host)",
	"isolated": "Isolated (VMs reach only each other and this host)",
	"routed":   "Routed (VMs are reachable without address translation)",
	"bridge":   "Bridge (VMs join a host network directly)",
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

func ownerText(v any) string {
	if field(v, "ownership") == "external" {
		return "Created outside Virmill"
	}
	return "Virmill"
}

// readableDetails describes networks and storage pools in plain terms. Other
// resources keep the complete field list; x shows every field for all of them.
func (m Workspace) readableDetails(width int) []string {
	switch m.Section {
	case 2:
		return networkDetails(m.Detail)
	case 3:
		return poolDetails(m.Detail)
	}
	return HumanDetails(m.Detail, width)
}

func networkDetails(v any) []string {
	n := networkView(v)
	kind := n.kind()
	if text, ok := networkKinds[kind]; ok {
		kind = text
	}
	lines := []string{"Name: " + field(v, "name"), "State: " + rowState(v), "Type: " + kind, "Address: " + n.address()}
	if r := n.dhcpRange(); r != "" {
		lines = append(lines, "DHCP range: "+r)
	}
	if n.Bridge.Name != "" {
		lines = append(lines, "Host bridge: "+n.Bridge.Name)
	}
	lines = append(lines, "Starts with host: "+yesNo(object(v)["autostart"] == true))
	if object(v)["persistent"] == false {
		lines = append(lines, "Temporary: removed when it stops")
	}
	return append(lines, "", "Managed by: "+ownerText(v), "UUID: "+resourceID(v))
}

var poolTypes = map[string]string{"dir": "Folder", "fs": "Formatted disk", "netfs": "Network share", "logical": "LVM volume group",
	"disk": "Whole disk", "iscsi": "iSCSI target", "zfs": "ZFS pool", "rbd": "Ceph RBD"}

func poolDetails(v any) []string {
	var p struct {
		Path string `xml:"target>path"`
	}
	if raw := field(v, "xml"); len(raw) <= 1<<20 {
		_ = xml.Unmarshal([]byte(raw), &p)
	}
	kind := field(v, "type")
	if text, ok := poolTypes[kind]; ok {
		kind = text + " (" + kind + ")"
	}
	o := object(v)
	lines := []string{"Name: " + field(v, "name"), "State: " + poolState(v), "Type: " + kind}
	if p.Path != "" {
		lines = append(lines, "Location: "+p.Path)
	}
	return append(lines, "", "Size: "+optionalSize(o["capacityBytes"]), "Used: "+optionalSize(o["allocatedBytes"]),
		"Free: "+optionalSize(o["availableBytes"]), "", "Starts with host: "+yesNo(o["autostart"] == true),
		"Managed by: "+ownerText(v), "UUID: "+resourceID(v))
}

var xmlLabels = map[string]string{"persistentXML": "Persistent XML (used at next start)", "liveXML": "Live XML (running now)", "xml": "XML"}

// technicalLines lists every field and shows libvirt XML as XML, not escaped JSON.
func technicalLines(detail any, width int) []string {
	fields := object(detail)
	if fields == nil {
		return HumanDetails(detail, width)
	}
	rest, xmlKeys := map[string]any{}, []string{}
	for k, v := range fields {
		if s, ok := v.(string); ok && strings.HasSuffix(strings.ToLower(k), "xml") && strings.TrimSpace(s) != "" {
			xmlKeys = append(xmlKeys, k)
			continue
		}
		rest[k] = v
	}
	sort.Strings(xmlKeys)
	lines := HumanDetails(rest, width)
	for _, k := range xmlKeys {
		label := xmlLabels[k]
		if label == "" {
			label = k
		}
		lines = append(lines, "", label)
		lines = append(lines, strings.Split(strings.TrimRight(fields[k].(string), "\n"), "\n")...)
	}
	return lines
}

// jobLabels name tasks in job lists where the menu label describes a choice
// ("ISO installer") or is too long for a list row.
var jobLabels = map[string]string{
	"vm.hard-stop": "Force off VM", "vm.autostart": "Change automatic startup",
	"vm.create": "Create VM", "vm.create.devices-v1": "Create VM", "vm.create.resume": "Resume VM creation",
	"vm.create.cleanup": "Clean up VM creation", "vm.create.accept-devices-v1": "Accept VM devices",
	"vm.remove-definition-v1": "Remove VM, keep disks", "vm.remove-disks-v1": "Remove VM and disks",
	"import.prepare": "Prepare OVA appliance", "import.prepare-disks": "Prepare disk images", "import.prepare-install": "Prepare ISO installer",
	"guest.recipe.run": "Run guest setup", "guest.tools.install": "Install guest tools",
	"storage.grant-read": "Grant disk read access", "storage.revoke-read": "Revoke disk read access",
	"backup.local-create-v1": "Back up capture", "backup.local-restore-v1": "Recover from backup",
	"backup.local-repository-init-v1": "Create backup repository", "backup.local-repository-check-v1": "Check backup repository",
}

// operationLabel names a job's task the way the menus do.
func operationLabel(op string) string {
	if op == "" {
		return ""
	}
	if label, ok := jobLabels[op]; ok {
		return label
	}
	if action, ok := actionText[operationCommand(op)]; ok {
		return action.label
	}
	return strings.ReplaceAll(op, ".", " ")
}

// jobTarget names what a job changes: the plan's VM name, or a loaded
// resource with the same UUID.
func (m Workspace) jobTarget(v any) string {
	if name := field(v, "targetName"); name != "" {
		return name
	}
	for _, id := range array(object(v)["resourceIDs"]) {
		parts := strings.Split(fmt.Sprint(id), "|")
		uuid := parts[len(parts)-1]
		for _, kind := range []string{"vms", "networks", "pools"} {
			for _, r := range array(m.Data[kind]) {
				if resourceID(r) == uuid {
					return rowName(r)
				}
			}
		}
	}
	return ""
}

// jobName is "task · target"; older coordinators send no task, so the ID stays.
func (m Workspace) jobName(v any) string {
	label := operationLabel(field(v, "operation"))
	if label == "" {
		return resourceID(v)
	}
	if target := m.jobTarget(v); target != "" {
		return label + m.separator() + target
	}
	return label
}

func ago(stamp string, now time.Time) string {
	t, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d/(24*time.Hour)))
	case t.Year() == now.Year():
		return t.Local().Format("Jan 2")
	}
	return t.Local().Format("Jan 2 2006")
}
