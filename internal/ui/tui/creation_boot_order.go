package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

type creationBootDevice struct {
	index, order int
	source       string
}

// Disks always participate. Invalid negative media priorities stay enabled
// during an explicit repair rather than silently becoming attach-only media.
func creationBootDevices(spec domain.CreationSpec) []creationBootDevice {
	devices := make([]creationBootDevice, 0, len(spec.Disks)+len(spec.Media))
	for i, disk := range spec.Disks {
		devices = append(devices, creationBootDevice{i, disk.BootOrder, disk.SourceID})
	}
	for i, medium := range spec.Media {
		if medium.BootOrder != 0 {
			devices = append(devices, creationBootDevice{len(spec.Disks) + i, medium.BootOrder, medium.SourceID})
		}
	}
	slices.SortStableFunc(devices, func(a, b creationBootDevice) int {
		if a.order <= 0 && b.order <= 0 {
			return 0
		}
		if a.order <= 0 && b.order > 0 {
			return 1
		}
		if a.order > 0 && b.order <= 0 {
			return -1
		}
		if a.order < b.order {
			return -1
		}
		if a.order > b.order {
			return 1
		}
		return 0
	})
	return devices
}

func creationBootOrderChoices(spec domain.CreationSpec, deviceIndex int) []string {
	if deviceIndex < 0 || deviceIndex >= len(spec.Disks)+len(spec.Media) {
		return nil
	}
	count := len(creationBootDevices(spec))
	first := 1
	if deviceIndex >= len(spec.Disks) {
		first = 0
		if spec.Media[deviceIndex-len(spec.Disks)].BootOrder == 0 {
			count++ // Re-enabling inserts this medium at one explicit position.
		}
	}
	choices := make([]string, 0, count-first+1)
	for order := first; order <= count; order++ {
		choices = append(choices, strconv.Itoa(order))
	}
	return choices
}

// creationMoveBootOrder is called only after an explicit priority choice.
// Loaded/restored declarations are never normalized as a side effect of render,
// navigation, host observation, or request validation.
func creationMoveBootOrder(spec domain.CreationSpec, deviceIndex, requestedOrder int) (domain.CreationSpec, error) {
	if !slices.Contains(creationBootOrderChoices(spec, deviceIndex), strconv.Itoa(requestedOrder)) {
		return spec, fmt.Errorf("Boot priority: choose an offered position; only installer/media devices support Attach only.")
	}
	ordered := creationBootDevices(spec)
	ordered = slices.DeleteFunc(ordered, func(device creationBootDevice) bool { return device.index == deviceIndex })
	if requestedOrder > 0 {
		ordered = slices.Insert(ordered, requestedOrder-1, creationBootDevice{index: deviceIndex})
	}
	spec.Disks = slices.Clone(spec.Disks)
	spec.Media = slices.Clone(spec.Media)
	if requestedOrder == 0 {
		spec.Media[deviceIndex-len(spec.Disks)].BootOrder = 0
	}
	for i, device := range ordered {
		if device.index < len(spec.Disks) {
			spec.Disks[device.index].BootOrder = i + 1
		} else {
			spec.Media[device.index-len(spec.Disks)].BootOrder = i + 1
		}
	}
	return spec, nil
}

func creationBootOrderSummary(spec domain.CreationSpec) string {
	ordered := creationBootDevices(spec)
	if len(ordered) == 0 {
		return "Boot: no devices selected."
	}
	for i, device := range ordered {
		if device.order != i+1 {
			return "Boot order needs review. Change a priority to rebuild the sequence."
		}
	}
	parts := make([]string, 0, min(3, len(ordered)))
	for i, device := range ordered[:min(3, len(ordered))] {
		name := ansi.Truncate(validation.SafeText(device.source), 18, "…")
		parts = append(parts, fmt.Sprintf("%d. %s", i+1, name))
	}
	text := "Boot: " + strings.Join(parts, " → ")
	if len(ordered) > 3 {
		text += fmt.Sprintf(" · +%d more", len(ordered)-3)
	}
	return text
}
