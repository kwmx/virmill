package xmlpatch

import (
	"encoding/xml"
	"fmt"
	"strings"

	"virmill.local/core/internal/domain"
)

// ReadResourceValues reads configuration values without inferring edit support.
// A current vCPU count or balloon target may differ from its configured maximum;
// CPU topology and memory hotplug policy do not make those scalars unreadable.
// Memory remains exact bytes, including values not representable in whole MiB.
func ReadResourceValues(raw string) (domain.ResourceValues, error) {
	var out domain.ResourceValues
	root, err := positionedXML(raw)
	if err != nil {
		return out, err
	}
	if current, maximum, err := observedCPUValues(root); err != nil {
		out.CPUError = err.Error()
	} else {
		out.VCPUs, out.MaximumVCPUs = &current, &maximum
	}
	if current, maximum, err := observedMemoryValues(root); err != nil {
		out.MemoryError = err.Error()
	} else {
		out.MemoryBytes, out.MaximumMemoryBytes = &current, &maximum
	}
	return out, nil
}

// A namespaced lookalike is not an alternative source for a core scalar. Report
// that family as unreadable even when a normal sibling also exists.
func observedResourceNode(root *positionedNode, name string, required bool) (*positionedNode, error) {
	var found *positionedNode
	for _, part := range root.parts {
		n := part.child
		if n == nil || n.name.Local != name {
			continue
		}
		if n.name != (xml.Name{Local: name}) || found != nil {
			return nil, fmt.Errorf("%s has duplicate or namespaced values", name)
		}
		found = n
	}
	if found == nil && required {
		return nil, fmt.Errorf("%s is missing from the domain configuration", name)
	}
	return found, nil
}

func observedResourceScalar(n *positionedNode, attrs ...string) (uint64, error) {
	if !n.onlyAttrs(attrs...) {
		return 0, fmt.Errorf("%s has unsupported or namespaced attributes", n.name.Local)
	}
	var text strings.Builder
	for _, part := range n.parts {
		if part.child != nil || part.kind != "text" {
			return 0, fmt.Errorf("%s must contain a plain positive integer", n.name.Local)
		}
		text.WriteString(part.text)
	}
	value, err := positiveResource(text.String())
	if err != nil {
		return 0, fmt.Errorf("%s must contain a positive integer within the supported numeric range", n.name.Local)
	}
	return value, nil
}

func observedCPUValues(root *positionedNode) (uint64, uint64, error) {
	n, err := observedResourceNode(root, "vcpu", true)
	if err != nil {
		return 0, 0, err
	}
	maximum, err := observedResourceScalar(n, "current", "placement", "cpuset")
	if err != nil {
		return 0, 0, err
	}
	if placement, ok := n.attr("placement"); ok && placement != "static" && placement != "auto" {
		return 0, 0, fmt.Errorf("vcpu placement is unsupported")
	}
	current := maximum
	if value, ok := n.attr("current"); ok {
		current, err = positiveResource(value)
		if err != nil || current > maximum {
			return 0, 0, fmt.Errorf("current vCPU count must be positive and no greater than the configured maximum")
		}
	}
	return current, maximum, nil
}

func observedMemoryBytes(n *positionedNode) (uint64, error) {
	value, err := observedResourceScalar(n, "unit", "dumpCore")
	if err != nil {
		return 0, err
	}
	if dump, ok := n.attr("dumpCore"); ok && dump != "on" && dump != "off" {
		return 0, fmt.Errorf("%s has an unsupported dumpCore value", n.name.Local)
	}
	unit, _ := n.attr("unit")
	// Match the established exact memory units used by the fixed-resource
	// editor without importing its whole-MiB editing restrictions.
	units := map[string]uint64{"": 1024, "b": 1, "bytes": 1, "KB": 1000, "k": 1024, "KiB": 1024, "MB": 1000000, "M": 1 << 20, "MiB": 1 << 20, "GB": 1000000000, "G": 1 << 30, "GiB": 1 << 30, "TB": 1000000000000, "T": 1 << 40, "TiB": 1 << 40}
	multiplier, ok := units[unit]
	if !ok {
		return 0, fmt.Errorf("%s has an unsupported memory unit", n.name.Local)
	}
	if value > ^uint64(0)/multiplier {
		return 0, fmt.Errorf("%s overflows the supported byte count", n.name.Local)
	}
	return value * multiplier, nil
}

func observedMemoryValues(root *positionedNode) (uint64, uint64, error) {
	n, err := observedResourceNode(root, "memory", true)
	if err != nil {
		return 0, 0, err
	}
	maximum, err := observedMemoryBytes(n)
	if err != nil {
		return 0, 0, err
	}
	n, err = observedResourceNode(root, "currentMemory", false)
	if err != nil {
		return 0, 0, err
	}
	current := maximum
	if n != nil {
		current, err = observedMemoryBytes(n)
		if err != nil {
			return 0, 0, err
		}
		if current > maximum {
			return 0, 0, fmt.Errorf("current memory exceeds the configured memory maximum")
		}
	}
	return current, maximum, nil
}
