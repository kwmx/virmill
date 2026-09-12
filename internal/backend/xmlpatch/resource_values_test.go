package xmlpatch

import (
	"fmt"
	"strings"
	"testing"
)

func observedResourceXML(cpu, memory, extra string) string {
	return `<domain type="kvm" xmlns:vendor="urn:unknown"><name>resource-view</name>` + cpu + memory + `<metadata><vendor:policy exact="yes">retain &#x20; all</vendor:policy></metadata><devices><disk type="file"><source file="/never-opened/image.qcow2"/></disk><vendor:device setting="unchanged"/></devices>` + extra + `</domain>`
}

func TestReadResourceValuesKeepsCurrentAndMaximumIndependent(t *testing.T) {
	raw := observedResourceXML(`<vcpu current="2" placement="static" cpuset="0-7,^3">8</vcpu>`, `<memory unit="GiB">4</memory><currentMemory unit="MB">2048</currentMemory>`, `<maxMemory slots="16" unit="GiB">64</maxMemory><cpu><topology sockets="2" dies="1" cores="2" threads="2"/><numa><cell id="0" cpus="0-7" memory="4194304" unit="KiB"/></numa></cpu><vcpus><vcpu id="0" enabled="yes" hotpluggable="no"/></vcpus><cputune><vcpupin vcpu="0" cpuset="0"/></cputune><memoryBacking><source type="memfd"/></memoryBacking>`)
	got, err := ReadResourceValues(raw)
	if err != nil || got.CPUError != "" || got.MemoryError != "" {
		t.Fatalf("%+v %v", got, err)
	}
	if got.VCPUs == nil || *got.VCPUs != 2 || got.MaximumVCPUs == nil || *got.MaximumVCPUs != 8 || got.MemoryBytes == nil || *got.MemoryBytes != 2048000000 || got.MaximumMemoryBytes == nil || *got.MaximumMemoryBytes != 4<<30 {
		t.Fatalf("current/maximum/hotplug ceiling conflated: %+v", got)
	}
	// Successfully displaying these values must not widen the editing adapter.
	if _, err := EditResources(raw, ResourceEdit{VCPUs: ptr(4)}); err == nil {
		t.Fatal("topology unexpectedly became editable")
	}
	if _, err := EditResources(raw, ResourceEdit{MemoryMiB: ptr(4096)}); err == nil {
		t.Fatal("hotplug/current-memory layout unexpectedly became editable")
	}
}

func TestReadResourceValuesExactUnitsAndFallbacks(t *testing.T) {
	units := map[string]uint64{"": 1024, "b": 1, "bytes": 1, "KB": 1000, "k": 1024, "KiB": 1024, "MB": 1000000, "M": 1 << 20, "MiB": 1 << 20, "GB": 1000000000, "G": 1 << 30, "GiB": 1 << 30, "TB": 1000000000000, "T": 1 << 40, "TiB": 1 << 40}
	for unit, multiplier := range units {
		t.Run(unit, func(t *testing.T) {
			attr := ""
			if unit != "" {
				attr = ` unit="` + unit + `"`
			}
			raw := observedResourceXML(`<vcpu placement="auto"> 1024 </vcpu>`, fmt.Sprintf(`<memory%s dumpCore="off">3</memory>`, attr), "")
			got, err := ReadResourceValues(raw)
			if err != nil || got.CPUError != "" || got.MemoryError != "" {
				t.Fatalf("%+v %v", got, err)
			}
			if *got.VCPUs != 1024 || *got.MaximumVCPUs != 1024 || *got.MemoryBytes != 3*multiplier || *got.MaximumMemoryBytes != 3*multiplier {
				t.Fatal("values rounded or editable limits applied", got)
			}
		})
	}
	got, err := ReadResourceValues(observedResourceXML(`<vcpu>18446744073709551615</vcpu>`, `<memory unit="b">18446744073709551615</memory>`, ""))
	if err != nil || got.CPUError != "" || got.MemoryError != "" || *got.VCPUs != ^uint64(0) || *got.MemoryBytes != ^uint64(0) {
		t.Fatal("valid uint64 limit rejected", got, err)
	}
}

func TestReadResourceValuesCPUFailureKeepsMemoryReadable(t *testing.T) {
	cases := map[string]string{
		"missing":                     "",
		"duplicate":                   `<vcpu>2</vcpu><vcpu>2</vcpu>`,
		"namespaced element":          `<vendor:vcpu>2</vendor:vcpu>`,
		"namespaced sibling":          `<vcpu>2</vcpu><vendor:vcpu>4</vendor:vcpu>`,
		"namespaced attribute":        `<vcpu vendor:current="1">2</vcpu>`,
		"unknown attribute":           `<vcpu future="yes">2</vcpu>`,
		"structured":                  `<vcpu>2<value>3</value></vcpu>`,
		"empty":                       `<vcpu/>`,
		"zero":                        `<vcpu>0</vcpu>`,
		"negative":                    `<vcpu>-2</vcpu>`,
		"fractional":                  `<vcpu>1.5</vcpu>`,
		"overflow":                    `<vcpu>18446744073709551616</vcpu>`,
		"current zero":                `<vcpu current="0">2</vcpu>`,
		"current empty":               `<vcpu current="">2</vcpu>`,
		"current larger than maximum": `<vcpu current="3">2</vcpu>`,
		"current overflow":            `<vcpu current="18446744073709551616">2</vcpu>`,
		"unknown placement":           `<vcpu placement="future">2</vcpu>`,
	}
	for name, cpu := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ReadResourceValues(observedResourceXML(cpu, `<memory unit="MiB">3072</memory>`, ""))
			if err != nil || got.CPUError == "" || got.VCPUs != nil || got.MaximumVCPUs != nil {
				t.Fatalf("invalid CPU was not isolated: %+v %v", got, err)
			}
			if got.MemoryError != "" || got.MemoryBytes == nil || *got.MemoryBytes != 3072<<20 {
				t.Fatal("CPU failure hid readable memory", got)
			}
		})
	}
}

func TestReadResourceValuesMemoryFailureKeepsCPUReadable(t *testing.T) {
	cases := map[string]string{
		"missing":                    "",
		"only hotplug ceiling":       `<maxMemory unit="GiB">4</maxMemory>`,
		"duplicate maximum":          `<memory>2</memory><memory>2</memory>`,
		"duplicate current":          `<memory>2</memory><currentMemory>1</currentMemory><currentMemory>1</currentMemory>`,
		"namespaced maximum":         `<vendor:memory>2</vendor:memory>`,
		"namespaced maximum sibling": `<memory>2</memory><vendor:memory>3</vendor:memory>`,
		"namespaced current":         `<memory>2</memory><vendor:currentMemory>1</vendor:currentMemory>`,
		"namespaced unit":            `<memory vendor:unit="MiB">2</memory>`,
		"unknown attribute":          `<memory future="yes">2</memory>`,
		"structured":                 `<memory>2<value>3</value></memory>`,
		"empty":                      `<memory/>`,
		"zero":                       `<memory>0</memory>`,
		"negative":                   `<memory>-1</memory>`,
		"fractional":                 `<memory>0.5</memory>`,
		"unknown unit":               `<memory unit="mystery">2</memory>`,
		"unknown dump policy":        `<memory dumpCore="maybe">2</memory>`,
		"integer overflow":           `<memory unit="b">18446744073709551616</memory>`,
		"byte conversion overflow":   `<memory unit="KiB">18446744073709551615</memory>`,
		"current zero":               `<memory>2</memory><currentMemory>0</currentMemory>`,
		"current exceeds maximum":    `<memory unit="MiB">2</memory><currentMemory unit="KiB">2049</currentMemory>`,
		"current unknown unit":       `<memory>2</memory><currentMemory unit="future">1</currentMemory>`,
	}
	for name, memory := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ReadResourceValues(observedResourceXML(`<vcpu current="2">4</vcpu>`, memory, ""))
			if err != nil || got.MemoryError == "" || got.MemoryBytes != nil || got.MaximumMemoryBytes != nil {
				t.Fatalf("invalid memory was not isolated: %+v %v", got, err)
			}
			if got.CPUError != "" || got.VCPUs == nil || *got.VCPUs != 2 || *got.MaximumVCPUs != 4 {
				t.Fatal("memory failure hid readable CPU", got)
			}
		})
	}
}

func TestReadResourceValuesRejectsInvalidWholeDocument(t *testing.T) {
	for name, raw := range map[string]string{
		"wrong root":           `<other><vcpu>2</vcpu><memory>2048</memory></other>`,
		"namespace root":       `<domain xmlns="urn:foreign"><vcpu>2</vcpu></domain>`,
		"multiple roots":       `<domain/><domain/>`,
		"unbalanced":           `<domain><vcpu>2</domain>`,
		"duplicate attributes": `<domain><vcpu current="1" current="2">2</vcpu><memory>2048</memory></domain>`,
		"directive":            `<!DOCTYPE domain><domain><vcpu>2</vcpu></domain>`,
		"byte bound":           strings.Repeat("x", Limit+1),
		"node bound":           `<domain><metadata>` + strings.Repeat(`<x/>`, 65536) + `</metadata></domain>`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ReadResourceValues(raw)
			if err == nil || got.VCPUs != nil || got.MemoryBytes != nil {
				t.Fatalf("invalid document yielded values: %+v %v", got, err)
			}
		})
	}
}
