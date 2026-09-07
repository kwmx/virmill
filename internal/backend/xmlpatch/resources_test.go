package xmlpatch

import (
	"fmt"
	"strings"
	"testing"
)

func ptr(n uint64) *uint64 { return &n }
func fixedXML(memory, current, vcpu, extra string) string {
	return `<domain type='kvm' xmlns:qemu='http://libvirt.org/schemas/domain/qemu/1.0'><name>resource-fixture</name>` + memory + current + vcpu + `<metadata><vendor:policy xmlns:vendor='urn:fixture' exact='yes'> opaque &amp; retained </vendor:policy></metadata><!-- keep this --><devices><disk type='file' device='disk'><source file='/not-opened/existing.qcow2'/><target dev='vda' bus='virtio'/></disk></devices><qemu:commandline><qemu:arg value='existing-expert-setting'/></qemu:commandline>` + extra + `</domain>`
}
func TestFixedResourcesPreserveEveryUnrelatedByte(t *testing.T) {
	original := fixedXML(`<memory unit='MiB' dumpCore='off'>256</memory>`, `<currentMemory unit='KiB'>262144</currentMemory>`, `<vcpu placement='static'>2</vcpu>`, "")
	result, err := EditResources(original, ResourceEdit{VCPUs: ptr(4), MemoryMiB: ptr(512)})
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.NewReplacer(">256</memory>", ">512</memory>", ">262144</currentMemory>", ">524288</currentMemory>", ">2</vcpu>", ">4</vcpu>").Replace(original)
	if result != expected {
		t.Fatal("unrelated configuration changed", result)
	}
	if _, err = EditResources(result, ResourceEdit{VCPUs: ptr(4), MemoryMiB: ptr(512)}); err != nil {
		t.Fatal(err)
	}
}
func TestMemoryUnitsRequireExactRepresentationAndConsistentCurrent(t *testing.T) {
	for _, test := range []struct {
		unit      string
		old, next uint64
	}{{"", 262144, 524288}, {"b", 268435456, 536870912}, {"bytes", 268435456, 536870912}, {"k", 262144, 524288}, {"KiB", 262144, 524288}, {"M", 256, 512}, {"MiB", 256, 512}} {
		t.Run(test.unit, func(t *testing.T) {
			attr := ""
			if test.unit != "" {
				attr = ` unit='` + test.unit + `'`
			}
			x := fixedXML(fmt.Sprintf("<memory%s>%d</memory>", attr, test.old), "", "<vcpu>2</vcpu>", "")
			out, err := EditResources(x, ResourceEdit{MemoryMiB: ptr(512)})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, fmt.Sprintf("<memory%s>%d</memory>", attr, test.next)) {
				t.Fatal(out)
			}
		})
	}
	// Decimal units and GiB are accepted only when the requested whole MiB size
	// can be represented without changing the existing unit or rounding bytes.
	for _, test := range []struct {
		unit               string
		old, next, desired uint64
	}{{"KB", 1024, 16384, 15625}, {"MB", 256, 16384, 15625}, {"GB", 1, 16384, 15625000}, {"GiB", 1, 2, 2048}} {
		x := fixedXML(fmt.Sprintf("<memory unit='%s'>%d</memory>", test.unit, test.old), "", "<vcpu>2</vcpu>", "")
		out, err := EditResources(x, ResourceEdit{MemoryMiB: ptr(test.desired)})
		if test.desired > 1048576 {
			if err == nil {
				t.Fatal("maximum ignored")
			}
			continue
		}
		if err != nil {
			t.Fatal(test, err)
		}
		wanted := test.desired * (1 << 20)
		if test.unit == "KB" {
			wanted /= 1000
		} else if test.unit == "MB" {
			wanted /= 1000000
		} else {
			wanted /= 1 << 30
		}
		if !strings.Contains(out, fmt.Sprintf(">%d</memory>", wanted)) {
			t.Fatal(out)
		}
	}
	for _, memory := range []string{`<memory unit='MB'>256</memory>`, `<memory unit='GiB'>1</memory>`, `<memory unit='mystery'>256</memory>`, `<memory unit='GiB'>18446744073709551615</memory>`, `<memory>2<x/></memory>`, `<memory>262144</memory><memory>262144</memory>`, `<memory unit='KiB' future='bound'>262144</memory>`} {
		if _, err := EditResources(fixedXML(memory, "", "<vcpu>2</vcpu>", ""), ResourceEdit{MemoryMiB: ptr(513)}); err == nil {
			t.Fatal("unsafe unit/shape accepted", memory)
		}
	}
	if _, err := EditResources(fixedXML(`<memory>524288</memory>`, `<currentMemory>262144</currentMemory>`, `<vcpu>2</vcpu>`, ""), ResourceEdit{MemoryMiB: ptr(1024)}); err == nil {
		t.Fatal("balloon policy collapsed")
	}
}
func TestResourceDependenciesFailClosed(t *testing.T) {
	for _, extra := range []string{`<cpu><topology sockets='1' cores='2' threads='1'/></cpu>`, `<cpu><numa><cell id='0' cpus='0-1'/></numa></cpu>`, `<vcpus><vcpu id='0' enabled='yes'/></vcpus>`, `<cputune><vcpupin vcpu='0' cpuset='1'/></cputune>`, `<numatune><memory mode='strict'/></numatune>`} {
		if _, err := EditResources(fixedXML(`<memory>262144</memory>`, "", `<vcpu>2</vcpu>`, extra), ResourceEdit{VCPUs: ptr(4)}); err == nil {
			t.Fatal("dependent CPU policy accepted", extra)
		}
	}
	for _, vcpu := range []string{`<vcpu current='1'>2</vcpu>`, `<vcpu cpuset='1'>2</vcpu>`, `<vcpu placement='auto'>2</vcpu>`, `<vcpu>2</vcpu><vcpu>2</vcpu>`, `<vcpu><future/></vcpu>`} {
		if _, err := EditResources(fixedXML(`<memory>262144</memory>`, "", vcpu, ""), ResourceEdit{VCPUs: ptr(4)}); err == nil {
			t.Fatal("dependent CPU attribute accepted", vcpu)
		}
	}
	for _, extra := range []string{`<maxMemory slots='2'>524288</maxMemory>`, `<memtune><hard_limit>524288</hard_limit></memtune>`, `<numatune/>`, `<cpu><numa/></cpu>`, `<memoryBacking><source type='memfd'/></memoryBacking>`, `<devices><memory model='dimm'/></devices>`} {
		if _, err := EditResources(fixedXML(`<memory>262144</memory>`, "", `<vcpu>2</vcpu>`, extra), ResourceEdit{MemoryMiB: ptr(512)}); err == nil {
			t.Fatal("dependent RAM policy accepted", extra)
		}
	}
}
func FuzzResourceEdit(f *testing.F) {
	f.Add(fixedXML(`<memory>262144</memory>`, "", `<vcpu>2</vcpu>`, ""))
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = EditResources(s, ResourceEdit{MemoryMiB: ptr(512), VCPUs: ptr(4)})
	})
}

func TestResourceNodeBudget(t *testing.T) {
	x := `<domain><memory>262144</memory><metadata>` + strings.Repeat(`<x/>`, 65536) + `</metadata></domain>`
	if _, err := EditResources(x, ResourceEdit{MemoryMiB: ptr(512)}); err == nil {
		t.Fatal("unbounded node inventory accepted")
	}
}
