//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

func pciFixture(t *testing.T, filename string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "fixtures", "devices", "pci", filename+".xml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type pciFixtureObservation struct {
	name, xml           string
	nameError, xmlError error
	flags               []native.NodeDeviceXMLFlags
	afterXML            func()
}

func (n *pciFixtureObservation) GetName() (string, error) { return n.name, n.nameError }
func (n *pciFixtureObservation) GetXMLDesc(flags native.NodeDeviceXMLFlags) (string, error) {
	n.flags = append(n.flags, flags)
	if n.afterXML != nil {
		n.afterXML()
	}
	return n.xml, n.xmlError
}
func collectPCIFixtures(ctx context.Context, observations ...*pciFixtureObservation) (domain.PCIInventory, error) {
	return collectPCI(ctx, len(observations), func(i int) pciObservation { return observations[i] })
}

func TestPCIInventoryExactNativeFieldsAndUnknowns(t *testing.T) {
	zero := &pciFixtureObservation{name: "pci_0000_02_00_0", xml: pciFixture(t, "group-function-0")}
	one := &pciFixtureObservation{name: "pci_0000_02_00_1", xml: pciFixture(t, "group-function-1")}
	unknown := &pciFixtureObservation{name: "pci_0000_03_00_0", xml: pciFixture(t, "unknown-topology")}
	out, err := collectPCIFixtures(context.Background(), unknown, one, zero)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Devices) != 3 || out.Devices[0].Address != "0000:02:00.0" || out.Devices[1].Address != "0000:02:00.1" || out.Devices[2].Address != "0000:03:00.0" {
		t.Fatal("noncanonical or unsorted PCI identity", out)
	}
	d := out.Devices[0]
	if d.Name != zero.name || d.VendorID != "0x8086" || d.ProductID != "0x10c9" || d.Driver != "fixture_net" || d.Vendor != "Fixture vendor" || d.Product != "Fixture multi-function network device" || d.IOMMUGroup == nil || *d.IOMMUGroup != 12 || d.NUMANode == nil || *d.NUMANode != 0 {
		t.Fatal("native fields were omitted or changed", d)
	}
	if !reflect.DeepEqual(d.GroupMembers, []string{"0000:02:00.0", "0000:02:00.1"}) {
		t.Fatal("SR-IOV relation mistaken for IOMMU membership", d.GroupMembers)
	}
	d = out.Devices[2]
	if d.IOMMUGroup != nil || d.NUMANode != nil || len(d.GroupMembers) != 0 || d.Driver != "" || d.Vendor != "" || d.Product != "" {
		t.Fatal("missing observation was fabricated", d)
	}
	if len(out.Warnings) != 4 || !strings.Contains(out.Warnings[0], "do not establish safe passthrough") {
		t.Fatal("discovery limitations missing", out.Warnings)
	}
	for _, f := range []*pciFixtureObservation{zero, one, unknown} {
		if !reflect.DeepEqual(f.flags, []native.NodeDeviceXMLFlags{0}) {
			t.Fatal("unexpected node-device XML flags", f.flags)
		}
	}
	b, err := json.Marshal(out)
	var decoded domain.PCIInventory
	if err != nil || wire.Decode(b, &decoded) != nil || !reflect.DeepEqual(out, decoded) || !strings.Contains(string(b), `"iommuGroup":null`) || !strings.Contains(string(b), `"numaNode":null`) {
		t.Fatal("wire contract loses explicit unknowns", string(b), err)
	}
	for _, numa := range []string{"", "<numa/>", "<numa node='-1'/>"} {
		got, err := parsePCIDevice(unknown.name, strings.Replace(unknown.xml, "</capability>", numa+"</capability>", 1))
		if err != nil || got.NUMANode != nil {
			t.Fatal("unknown NUMA became node zero", got, err)
		}
	}
	got, err := parsePCIDevice(unknown.name, strings.Replace(unknown.xml, "<domain>0</domain><bus>3</bus><slot>0</slot><function>0</function>", "<domain>65535</domain><bus>0xff</bus><slot>31</slot><function>7</function>", 1))
	if err != nil || got.Address != "ffff:ff:1f.7" {
		t.Fatal("maximum valid address failed", got, err)
	}
}

func TestPCIInventoryRejectsMalformedAmbiguousAndForeignXML(t *testing.T) {
	base := pciFixture(t, "group-function-0")
	replace := func(old, next string) string { return strings.Replace(base, old, next, 1) }
	cases := map[string]string{
		"missing name":                replace("<name>pci_0000_02_00_0</name>", ""),
		"duplicate name":              replace("<name>pci_0000_02_00_0</name>", "<name>pci_0000_02_00_0</name><name>other</name>"),
		"mismatching name":            replace("pci_0000_02_00_0", "other"),
		"wrong capability":            replace("type='pci'", "type='usb_device'"),
		"missing capability":          "<device><name>pci_0000_02_00_0</name></device>",
		"duplicate capability":        replace("</device>", "<capability type='pci'/></device>"),
		"duplicate address field":     replace("<bus>2</bus>", "<bus>2</bus><bus>3</bus>"),
		"missing address field":       replace("<bus>2</bus>", ""),
		"negative domain":             replace("<domain>0</domain>", "<domain>-1</domain>"),
		"overflow domain":             replace("<domain>0</domain>", "<domain>65536</domain>"),
		"overflow bus":                replace("<bus>2</bus>", "<bus>256</bus>"),
		"overflow slot":               replace("<slot>0</slot>", "<slot>32</slot>"),
		"overflow function":           replace("<function>0</function>", "<function>8</function>"),
		"signed number":               replace("<bus>2</bus>", "<bus>+2</bus>"),
		"address attribute":           replace("<bus>2</bus>", "<bus ignored='yes'>2</bus>"),
		"nested address text":         replace("<bus>2</bus>", "<bus><value>2</value></bus>"),
		"vendor missing ID":           replace("id='0x8086'", ""),
		"vendor invalid ID":           replace("id='0x8086'", "id='0x808g'"),
		"vendor overflow ID":          replace("id='0x8086'", "id='0x18086'"),
		"vendor decimal ID":           replace("id='0x8086'", "id='8086'"),
		"duplicate vendor":            replace("<vendor id='0x8086'>Fixture vendor</vendor>", "<vendor id='0x8086'/><vendor id='0x0000'/>"),
		"duplicate vendor attribute":  replace("id='0x8086'", "id='0x8086' id='0x0000'"),
		"driver duplicate":            replace("<name>fixture_net</name>", "<name>fixture_net</name><name>other</name>"),
		"driver empty":                replace("<name>fixture_net</name>", "<name/>"),
		"driver incomplete":           replace("<driver><name>fixture_net</name></driver>", "<driver/>"),
		"duplicate driver":            replace("</driver>", "</driver><driver><name>other</name></driver>"),
		"group number absent":         replace("number='12'", ""),
		"group negative":              replace("number='12'", "number='-1'"),
		"group overflow":              replace("number='12'", "number='4294967296'"),
		"group duplicate":             replace("</iommuGroup>", "</iommuGroup><iommuGroup number='0'/>"),
		"group missing self":          replace("<address domain='0x0000' bus='0x02' slot='0x00' function='0x0'/>", ""),
		"group duplicate self":        replace("function='0x1'", "function='0x0'"),
		"group member missing field":  replace("bus='0x02' slot='0x00' function='0x1'", "bus='0x02' slot='0x00'"),
		"group member overflow":       replace("function='0x1'", "function='0x8'"),
		"group member duplicate attr": replace("function='0x1'", "function='0x1' function='0x2'"),
		"group foreign attribute":     replace("function='0x1'", "alien:function='0x1' xmlns:alien='urn:foreign'"),
		"numa invalid":                replace("<numa node='0'/>", "<numa node='-2'/>"),
		"numa signed":                 replace("<numa node='0'/>", "<numa node='+1'/>"),
		"numa overflow":               replace("<numa node='0'/>", "<numa node='2147483648'/>"),
		"numa empty attr":             replace("<numa node='0'/>", "<numa node=''/>"),
		"numa duplicate":              replace("<numa node='0'/>", "<numa node='0'/><numa node='1'/>"),
		"foreign root":                replace("<device>", "<device xmlns='urn:foreign'>"),
		"foreign pci namespace":       replace("<capability type='pci'>", "<capability xmlns='urn:foreign' type='pci'>"),
		"foreign name":                replace("<name>pci_0000_02_00_0</name>", "<foreign:name xmlns:foreign='urn:foreign'>pci_0000_02_00_0</foreign:name>"),
		"foreign driver":              replace("<driver>", "<driver xmlns='urn:foreign'>"),
		"foreign group":               replace("<iommuGroup number='12'>", "<iommuGroup xmlns='urn:foreign' number='12'>"),
		"control label":               replace("Fixture vendor", "bad&#x1b;vendor"),
		"line break label":            replace("Fixture vendor", "bad&#x0a;vendor"),
		"bidi label":                  replace("Fixture vendor", "bad\u202evendor"),
		"large label":                 replace("Fixture vendor", strings.Repeat("x", 1025)),
		"entity":                      replace("Fixture vendor", "&external;"),
		"doctype":                     "<!DOCTYPE device [<!ENTITY external SYSTEM 'file:///never-opened'>]>" + base,
		"processing instruction":      "<?untrusted data?>" + base,
		"multiple roots":              base + "<device/>",
		"wrong root":                  strings.ReplaceAll(base, "device>", "domain>"),
		"text outside":                base + "extra",
		"malformed":                   "<device>",
		"depth":                       replace("</device>", strings.Repeat("<unknown>", 32)+strings.Repeat("</unknown>", 32)+"</device>"),
		"nodes":                       replace("</device>", strings.Repeat("<x/>", 16384)+"</device>"),
		"oversize":                    base + strings.Repeat(" ", pciMaxXML),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePCIDevice("pci_0000_02_00_0", data); err == nil {
				t.Fatal("ambiguous or malformed inventory accepted")
			}
		})
	}
}

func TestPCIInventoryRejectsPartialGroupsDuplicatesAndNativeFailures(t *testing.T) {
	zeroXML, oneXML := pciFixture(t, "group-function-0"), pciFixture(t, "group-function-1")
	sentinel := errors.New("synthetic native read failure")
	cases := map[string][]*pciFixtureObservation{
		"missing group member":              {{name: "pci_0000_02_00_0", xml: zeroXML}},
		"duplicate identity":                {{name: "pci_0000_02_00_0", xml: zeroXML}, {name: "pci_0000_02_00_0", xml: zeroXML}},
		"duplicate address":                 {{name: "pci_0000_02_00_0", xml: zeroXML}, {name: "other", xml: strings.Replace(zeroXML, "pci_0000_02_00_0", "other", 1)}},
		"group number conflict":             {{name: "pci_0000_02_00_0", xml: zeroXML}, {name: "pci_0000_02_00_1", xml: strings.Replace(oneXML, "number='12'", "number='13'", 1)}},
		"group set conflict":                {{name: "pci_0000_02_00_0", xml: zeroXML}, {name: "pci_0000_02_00_1", xml: strings.Replace(oneXML, "<address domain='0x0000' bus='0x02' slot='0x00' function='0x0'/>", "", 1)}},
		"member group unknown":              {{name: "pci_0000_02_00_0", xml: zeroXML}, {name: "pci_0000_02_00_1", xml: strings.Replace(strings.Replace(pciFixture(t, "unknown-topology"), "pci_0000_03_00_0", "pci_0000_02_00_1", 1), "<domain>0</domain><bus>3</bus><slot>0</slot><function>0</function>", "<domain>0</domain><bus>2</bus><slot>0</slot><function>1</function>", 1)}},
		"native name error":                 {{name: "pci_0000_02_00_0", xml: zeroXML, nameError: sentinel}},
		"native XML error after first read": {{name: "pci_0000_02_00_0", xml: zeroXML}, {name: "pci_0000_02_00_1", xml: oneXML, xmlError: sentinel}},
	}
	for name, observations := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := collectPCIFixtures(context.Background(), observations...)
			if err == nil || len(got.Devices) != 0 || len(got.Warnings) != 0 {
				t.Fatal("partial native observation returned as complete", got, err)
			}
			if strings.HasPrefix(name, "native") && !errors.Is(err, sentinel) {
				t.Fatal("native error was hidden", err)
			}
		})
	}
	for _, uri := range []string{"test:///default", "qemu+ssh://host/system", "", "qemu:///system?socket=/other"} {
		if _, err := (&Provider{}).InspectPCI(context.Background(), uri); err == nil {
			t.Fatal("release adapter admitted URI", uri)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Provider{}).InspectPCI(ctx, "qemu:///system"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation tried opening a real host", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	got, err := collectPCIFixtures(ctx, &pciFixtureObservation{name: "pci_0000_02_00_0", xml: zeroXML, afterXML: cancel})
	if !errors.Is(err, context.Canceled) || len(got.Devices) != 0 {
		t.Fatal("mid-read cancellation returned success", got, err)
	}
}

func TestPCIInventoryBoundsAndEmptyDiscovery(t *testing.T) {
	for _, count := range []int{-1, pciMaxDevices + 1} {
		_, err := collectPCI(context.Background(), count, func(int) pciObservation { t.Fatal("read after count exceeded bounds"); return nil })
		if err == nil {
			t.Fatal("count bound missing")
		}
	}
	empty, err := collectPCIFixtures(context.Background())
	if err != nil || empty.Devices == nil || len(empty.Devices) != 0 || len(empty.Warnings) != 1 {
		t.Fatal("successful empty discovery lost explicit limits", empty, err)
	}
	base := pciFixture(t, "unknown-topology")
	large := strings.Replace(base, "</device>", "<!--"+strings.Repeat("x", pciMaxXML-len(base)-8)+"--></device>", 1)
	_, err = collectPCI(context.Background(), 64, func(i int) pciObservation {
		name := fmt.Sprintf("device_%d", i)
		data := strings.Replace(large, "pci_0000_03_00_0", name, 1)
		data = strings.Replace(data, "<bus>3</bus>", fmt.Sprintf("<bus>%d</bus>", i), 1)
		return &pciFixtureObservation{name: name, xml: data}
	})
	if err == nil || !strings.Contains(err.Error(), "aggregate node-device XML") {
		t.Fatal("aggregate native XML bound missing", err)
	}
	// Backslashes expand in JSON, so the response can exceed its frame budget
	// even when all individual XML strings and their aggregate remain bounded.
	escaped := strings.Replace(base, "<product id='0x0001'/>", "<product id='0x0001'>"+strings.Repeat(`\`, 1024)+"</product>", 1)
	escaped = strings.Replace(escaped, "<vendor id='0x1234'/>", "<vendor id='0x1234'>"+strings.Repeat(`\`, 1024)+"</vendor>", 1)
	_, err = collectPCI(context.Background(), pciMaxDevices, func(i int) pciObservation {
		name := fmt.Sprintf("device_%d", i)
		data := strings.Replace(escaped, "pci_0000_03_00_0", name, 1)
		data = strings.Replace(data, "<domain>0</domain>", fmt.Sprintf("<domain>%d</domain>", i), 1)
		return &pciFixtureObservation{name: name, xml: data}
	})
	if err == nil || !strings.Contains(err.Error(), "inventory exceeds bounded response") {
		t.Fatal("aggregate JSON response bound missing", err)
	}
}

func TestReadOnlyPCIInventoryWithNativeTestDriver(t *testing.T) {
	probe, err := native.NewConnectReadOnly("test:///default")
	if err != nil {
		t.Skipf("native test driver unavailable: %v", err)
	}
	probe.Close()
	// The default test driver has no PCI nodes. Its documented custom config
	// supplies only generated devices; all native state stays in memory.
	config := "<node>" + pciFixture(t, "group-function-0") + pciFixture(t, "group-function-1") + pciFixture(t, "unknown-topology") + `<device><name>usb_fixture</name><capability type='usb_device'><bus>1</bus><device>2</device><product id='0x5678'/><vendor id='0x1234'/></capability></device></node>`
	path := filepath.Join(t.TempDir(), "native-pci-test.xml")
	if err = os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := native.NewConnectReadOnly("test://" + path)
	if err != nil {
		t.Fatal("native test driver rejected generated node-device fixtures", err)
	}
	defer c.Close()
	out, err := inspectPCI(context.Background(), c)
	var nativeErr native.Error
	if errors.As(err, &nativeErr) && (nativeErr.Code == native.ERR_NO_SUPPORT || nativeErr.Code == native.ERR_OPERATION_UNSUPPORTED) {
		t.Skipf("BLOCKED: native test driver does not implement PCI node-device discovery: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Devices) != 3 {
		t.Fatalf("native PCI filter returned %d devices; expected 3 PCI devices and no USB device", len(out.Devices))
	}
	for _, d := range out.Devices {
		if d.Address == "" || d.Name == "" || d.VendorID == "" || d.ProductID == "" {
			t.Fatal("incomplete native test-driver PCI observation", d)
		}
	}
	if out.Devices[0].IOMMUGroup == nil || *out.Devices[0].IOMMUGroup != 12 || len(out.Devices[0].GroupMembers) != 2 || out.Devices[2].IOMMUGroup != nil || out.Devices[2].NUMANode != nil {
		t.Fatal("native readback lost group or unknown topology", out)
	}
	if out.Devices[0].Driver == "" {
		t.Log("LIMITATION: native test driver omits configured driver names; driver decoding has fixture evidence only until a real backend observation")
	}
	after, err := inspectPCI(context.Background(), c)
	if err != nil || !reflect.DeepEqual(out, after) {
		t.Fatal("read-only native observation changed in-memory device inventory", after, err)
	}
	t.Logf("Native in-memory test driver returned %d PCI devices through read-only bindings; this is simulated evidence, not physical-host or passthrough evidence", len(out.Devices))
}

func FuzzPCIInventoryXML(f *testing.F) {
	f.Add("fixture", `<device><name>fixture</name><capability type='pci'><domain>0</domain><bus>0</bus><slot>1</slot><function>0</function><vendor id='0x1234'/><product id='0x5678'/></capability></device>`)
	f.Add("fixture", `<device xmlns='urn:foreign'><name>fixture</name></device>`)
	f.Add("fixture", `<!DOCTYPE device><device/>`)
	f.Fuzz(func(t *testing.T, name, data string) {
		got, err := parsePCIDevice(name, data)
		if err != nil {
			return
		}
		if got.Name != name || got.Name == "" || len(got.Address) != 12 || len(got.VendorID) != 6 || len(got.ProductID) != 6 || got.GroupMembers == nil {
			t.Fatal("successful parse has incomplete identity", got)
		}
		b, err := json.Marshal(got)
		if err != nil || wire.Validate(b) != nil {
			t.Fatal("successful parse is not wire-safe", err)
		}
	})
}
