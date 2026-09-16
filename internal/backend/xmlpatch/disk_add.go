package xmlpatch

import (
	"fmt"
	"regexp"
	"strconv"

	"virmill.local/core/internal/domain"
)

// ADR 0062: add one new disk to an existing VM. The disk is a pool volume with
// a qcow2 driver on a bus the VM already has, inserted as the last device. No
// controller is added, no boot order is changed and no other XML is rewritten.
type DiskAddition struct {
	Pool   string `json:"pool"`
	Volume string `json:"volume"`
	Bus    string `json:"bus"`
	Target string `json:"target"`
	// Unit is the drive address unit for sata and scsi, and -1 for virtio.
	Unit int `json:"unit"`
}

var diskVolumeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:+-]{0,254}$`)

type diskAdditionModel struct {
	devices     *positionedNode
	targets     map[string]bool
	units       map[string]map[int]bool
	controllers map[string]bool
	buses       []string
}

func diskAdditionRefuse(message string) error {
	return domain.Fail("UNSUPPORTED_CAPABILITY", message)
}

func modelDiskAddition(data string) (*diskAdditionModel, error) {
	root, err := positionedXML(data)
	if err != nil {
		return nil, err
	}
	devices, err := onlyChild(root, "devices")
	if err != nil {
		return nil, err
	}
	m := &diskAdditionModel{devices: devices, targets: map[string]bool{}, units: map[string]map[int]bool{}, controllers: map[string]bool{}}
	for _, part := range devices.parts {
		n := part.child
		if n == nil || n.name.Space != "" {
			continue
		}
		if n.name.Local == "controller" {
			if kind, _ := n.attr("type"); kind != "" {
				m.controllers[kind] = true
			}
			continue
		}
		if n.name.Local != "disk" {
			continue
		}
		if len(m.targets) >= 64 {
			return nil, diskAdditionRefuse("this VM already has the maximum number of disks this editor supports")
		}
		target, err := onlyChild(n, "target")
		if err != nil {
			return nil, err
		}
		dev, _ := target.attr("dev")
		bus, _ := target.attr("bus")
		if !targetID.MatchString(dev) {
			return nil, diskAdditionRefuse("a disk lacks a stable supported target name")
		}
		if m.targets[dev] {
			return nil, diskAdditionRefuse("this VM declares the same disk target twice")
		}
		m.targets[dev] = true
		if bus == "" {
			continue
		}
		if m.units[bus] == nil {
			m.units[bus] = map[int]bool{}
			m.buses = append(m.buses, bus)
		}
		for _, address := range n.children("address") {
			if kind, _ := address.attr("type"); kind != "drive" {
				continue
			}
			value, present := address.attr("unit")
			if !present {
				continue
			}
			unit, err := strconv.Atoi(value)
			if err != nil || unit < 0 || unit > 255 {
				return nil, diskAdditionRefuse("a disk has an unsupported drive address")
			}
			m.units[bus][unit] = true
		}
	}
	return m, nil
}

// busLimit is the number of drive units this editor will use on a bus. SATA
// controllers expose six ports; virtio disks carry no drive unit.
func busLimit(bus string) (int, bool) {
	switch bus {
	case "sata":
		return 6, true
	case "scsi":
		return 16, true
	case "virtio":
		return 0, false
	}
	return 0, false
}

func diskTargetPrefix(bus string) string {
	if bus == "virtio" {
		return "vd"
	}
	return "sd"
}

// InspectDiskAddition resolves the disk this VM would get on the given bus. An
// empty bus follows the bus of the VM's existing disks.
func InspectDiskAddition(data, bus string) (DiskAddition, error) {
	var out DiskAddition
	m, err := modelDiskAddition(data)
	if err != nil {
		return out, err
	}
	if bus == "" {
		if len(m.buses) != 1 {
			return out, domain.Fail("INVALID_INPUT", "choose the bus for the new disk: this VM's disks do not all use one")
		}
		bus = m.buses[0]
	}
	limit, addressed := busLimit(bus)
	if !addressed && bus != "virtio" {
		return out, domain.Fail("INVALID_INPUT", "new disks can use the sata, scsi or virtio bus")
	}
	if addressed && !m.controllers[bus] {
		return out, diskAdditionRefuse("this VM has no " + bus + " controller; adding one is not supported yet")
	}
	out = DiskAddition{Bus: bus, Unit: -1}
	prefix := diskTargetPrefix(bus)
	for letter := byte('a'); letter <= 'z'; letter++ {
		name := prefix + string(letter)
		if !m.targets[name] {
			out.Target = name
			break
		}
	}
	if out.Target == "" {
		return out, diskAdditionRefuse("this VM has no free disk target left on that bus")
	}
	if addressed {
		out.Unit = -1
		for unit := 0; unit < limit; unit++ {
			if !m.units[bus][unit] {
				out.Unit = unit
				break
			}
		}
		if out.Unit < 0 {
			return out, diskAdditionRefuse("the " + bus + " controller has no free port for another disk")
		}
	}
	return out, nil
}

// AddDisk inserts exactly the reviewed disk. It refuses anything the review
// would no longer match, so a stale plan cannot change a different VM.
func AddDisk(data string, add DiskAddition) (string, error) {
	if !diskVolumeName.MatchString(add.Pool) || !diskVolumeName.MatchString(add.Volume) || !targetID.MatchString(add.Target) {
		return "", domain.Fail("INVALID_INPUT", "a new disk needs an exact pool, volume and target")
	}
	if len(add.Target) != 3 || add.Target[:2] != diskTargetPrefix(add.Bus) || add.Target[2] < 'a' || add.Target[2] > 'z' {
		return "", domain.Fail("INVALID_INPUT", "the disk target does not match its bus")
	}
	limit, addressed := busLimit(add.Bus)
	if !addressed && add.Bus != "virtio" {
		return "", domain.Fail("INVALID_INPUT", "new disks can use the sata, scsi or virtio bus")
	}
	if addressed && (add.Unit < 0 || add.Unit >= limit) || !addressed && add.Unit != -1 {
		return "", domain.Fail("INVALID_INPUT", "the reviewed drive port is out of range for this bus")
	}
	m, err := modelDiskAddition(data)
	if err != nil {
		return "", err
	}
	if m.targets[add.Target] {
		return "", domain.Fail("STALE_PLAN", "this VM already has a disk at "+add.Target+"; review again")
	}
	if addressed {
		if !m.controllers[add.Bus] {
			return "", diskAdditionRefuse("this VM has no " + add.Bus + " controller; adding one is not supported yet")
		}
		if m.units[add.Bus][add.Unit] {
			return "", domain.Fail("STALE_PLAN", "the reviewed drive port is now in use; review again")
		}
	}
	fragment := fmt.Sprintf(`<disk type="volume" device="disk"><driver name="qemu" type="qcow2" cache="writethrough" error_policy="stop"/><source pool="%s" volume="%s"/><target dev="%s" bus="%s"/>`,
		restoreEscape(add.Pool), restoreEscape(add.Volume), restoreEscape(add.Target), restoreEscape(add.Bus))
	if addressed {
		fragment += fmt.Sprintf(`<address type="drive" controller="0" bus="0" target="0" unit="%d"/>`, add.Unit)
	}
	fragment += `</disk>`
	if m.devices.empty {
		return replaceSpans(data, []spanReplacement{{m.devices.startEnd - 2, m.devices.startEnd, ">" + fragment + "</devices>"}})
	}
	return replaceSpans(data, []spanReplacement{{m.devices.endStart, m.devices.endStart, fragment}})
}
