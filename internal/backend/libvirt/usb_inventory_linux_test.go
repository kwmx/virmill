//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
)

type usbFakeNode struct {
	name, raw       string
	nameErr, xmlErr error
	reads           int
	afterXML        func(int)
	flags           []native.NodeDeviceXMLFlags
}

func (n *usbFakeNode) GetName() (string, error) { return n.name, n.nameErr }
func (n *usbFakeNode) GetXMLDesc(flags native.NodeDeviceXMLFlags) (string, error) {
	n.reads++
	n.flags = append(n.flags, flags)
	if n.afterXML != nil {
		n.afterXML(n.reads)
	}
	return n.raw, n.xmlErr
}

type usbFakeSession struct {
	nodes                 []usbObservation
	listErr, closeErr     error
	closes                int
	afterList, afterClose func()
}

func (s *usbFakeSession) devices() ([]usbObservation, error) {
	if s.afterList != nil {
		s.afterList()
	}
	return s.nodes, s.listErr
}
func (s *usbFakeSession) close() error {
	s.closes++
	if s.afterClose != nil {
		s.afterClose()
	}
	return s.closeErr
}

func usbXML(name, nativePath, address string) string {
	p := ""
	if nativePath != "" {
		p = "<path>" + nativePath + "</path>"
	}
	return "<device><name>" + name + "</name>" + p + "<parent>usb_usb1</parent><capability type='usb_device'>" + address + "<product id='0x1000'>Fixture product</product><vendor id='0x1234'>Fixture vendor</vendor></capability></device>"
}
func usbPlainFacts(_ context.Context, d domain.USBDevice, p string) (usbFacts, error) {
	return usbFacts{device: d, nativePath: p}, nil
}
func usbCollect(ctx context.Context, enrich usbEnrich, nodes ...*usbFakeNode) ([]domain.USBDevice, error) {
	return collectUSB(ctx, len(nodes), func(i int) usbObservation { return nodes[i] }, enrich)
}
func usbCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *domain.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("err=%v want=%s", err, code)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("native/attribute text leaked: %v", err)
	}
}

func usbFixture(t *testing.T, root, leaf, device, serial string) (*usbFakeNode, string) {
	t.Helper()
	relative := "pci0000:00/0000:00:14.0/usb1/" + leaf
	dir := filepath.Join(root, relative)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	attributes := map[string]string{"idVendor": "1234\n", "idProduct": "1000\n", "busnum": "1\n", "devnum": device + "\n", "devpath": strings.TrimPrefix(leaf, "1-") + "\n"}
	if serial != "" {
		attributes["serial"] = serial + "\n"
	}
	for name, value := range attributes {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	name := "usb_device_1234_1000_" + strings.ReplaceAll(leaf, "-", "_")
	return &usbFakeNode{name: name, raw: usbXML(name, usbSysfsPrefix+relative, "<bus>1</bus><device>"+device+"</device>")}, dir
}

func TestUSBInventoryUnknownFieldsAndVendorProductAreNotIdentity(t *testing.T) {
	one := &usbFakeNode{name: "usb_one", raw: usbXML("usb_one", "", "")}
	two := &usbFakeNode{name: "usb_two", raw: usbXML("usb_two", "", "<bus>1</bus><device>2</device>")}
	out, err := usbCollect(context.Background(), usbPlainFacts, two, one)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Name != "usb_one" || out[0].Bus != nil || out[0].Device != nil {
		t.Fatalf("unknown address invented or unstable order: %+v", out)
	}
	for _, d := range out {
		if d.StableID != "" || d.Serial != "" || d.PhysicalPort != "" || !d.Ambiguous || d.Reason == "" || d.VendorID != "0x1234" || d.ProductID != "0x1000" {
			t.Fatalf("unobserved identity: %+v", d)
		}
	}
	encoded, _ := json.Marshal(out)
	if !strings.Contains(string(encoded), `"bus":null`) || !strings.Contains(string(encoded), `"device":null`) {
		t.Fatal("nullable addresses omitted")
	}
	if !reflect.DeepEqual(one.flags, []native.NodeDeviceXMLFlags{0, 0}) || !reflect.DeepEqual(two.flags, one.flags) {
		t.Fatal("native flags or observation count changed")
	}
	empty, err := collectUSB(context.Background(), 0, func(int) usbObservation { t.Fatal("read empty inventory"); return nil }, usbPlainFacts)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty native inventory=%v err=%v", empty, err)
	}
}

func TestUSBInventoryObservedSerialAndPortFallback(t *testing.T) {
	root := t.TempDir()
	serial, _ := usbFixture(t, root, "1-2", "4", " SN/α% ")
	port, _ := usbFixture(t, root, "1-3", "5", "")
	out, err := usbCollect(context.Background(), (usbSysfs{root: root, fixture: true}).observe, serial, port)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]domain.USBDevice{}
	for _, d := range out {
		byName[d.Name] = d
	}
	s := byName[serial.name]
	if s.Serial != " SN/α% " || !strings.Contains(s.StableID, ":serial:") || !strings.Contains(s.StableID, "%2F") || s.Ambiguous || s.PhysicalPort == "" || *s.Bus != 1 || *s.Device != 4 {
		t.Fatalf("serial evidence changed: %+v", s)
	}
	p := byName[port.name]
	if p.Serial != "" || !strings.Contains(p.StableID, ":port:") || p.Ambiguous || !strings.HasSuffix(p.PhysicalPort, "1-3#port=3") || !strings.Contains(p.Reason, "explicit physical-port") {
		t.Fatalf("port fallback missing limitations: %+v", p)
	}
	// Missing native numbers may be filled only by actually read sysfs values.
	serial.raw = strings.Replace(serial.raw, "<bus>1</bus><device>4</device>", "", 1)
	out, err = usbCollect(context.Background(), (usbSysfs{root: root, fixture: true}).observe, serial)
	if err != nil || out[0].Bus == nil || *out[0].Bus != 1 || out[0].Device == nil || *out[0].Device != 4 {
		t.Fatalf("explicit sysfs addresses=%+v err=%v", out, err)
	}
}

func TestUSBInventoryDuplicateSerialDoesNotSilentlyChoosePort(t *testing.T) {
	root := t.TempDir()
	one, _ := usbFixture(t, root, "1-2", "4", "same")
	two, _ := usbFixture(t, root, "1-3", "5", "same")
	out, err := usbCollect(context.Background(), (usbSysfs{root: root, fixture: true}).observe, one, two)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].StableID != out[1].StableID || out[0].PhysicalPort == out[1].PhysicalPort {
		t.Fatal("fixture does not reproduce duplicate serial on distinct ports")
	}
	for _, d := range out {
		if !d.Ambiguous || !strings.Contains(d.Reason, "Duplicate") {
			t.Fatalf("duplicate was auto-selected: %+v", d)
		}
	}
	t.Run("escaped serial keys cannot alias", func(t *testing.T) {
		one := &usbFakeNode{name: "a", raw: usbXML("a", "", "")}
		two := &usbFakeNode{name: "b", raw: usbXML("b", "", "")}
		out, err := usbCollect(context.Background(), func(ctx context.Context, d domain.USBDevice, p string) (usbFacts, error) {
			d.Serial = map[string]string{"a": "a/b", "b": "a%2Fb"}[d.Name]
			return usbFacts{device: d}, nil
		}, one, two)
		if err != nil || out[0].StableID == out[1].StableID || out[0].Ambiguous || out[1].Ambiguous {
			t.Fatalf("escaping lost identity: %+v %v", out, err)
		}
	})
}

func TestUSBInventoryRejectsMalformedOrAmbiguousNativeXML(t *testing.T) {
	base := usbXML("usb_fixture", "/sys/devices/controller/usb1/1-2", "<bus>1</bus><device>4</device>")
	replace := func(a, b string) string { return strings.Replace(base, a, b, 1) }
	for name, raw := range map[string]string{
		"duplicate name":        replace("</name>", "</name><name>other</name>"),
		"name mismatch":         replace("usb_fixture", "other"),
		"duplicate capability":  replace("</capability></device>", "</capability><capability type='usb_device'/></device>"),
		"interface capability":  replace("usb_device", "usb"),
		"foreign namespace":     replace("<device>", "<device xmlns='urn:foreign'>"),
		"foreign numeric":       replace("<bus>1</bus>", "<x:bus xmlns:x='urn:foreign'>1</x:bus>"),
		"duplicate numeric":     replace("<bus>1</bus>", "<bus>1</bus><bus>2</bus>"),
		"empty numeric":         replace("<device>4</device>", "<device/>"),
		"negative number":       replace("<bus>1</bus>", "<bus>-1</bus>"),
		"overflow bus":          replace("<bus>1</bus>", "<bus>65536</bus>"),
		"overflow address":      replace("<device>4</device>", "<device>128</device>"),
		"zero address":          replace("<device>4</device>", "<device>0</device>"),
		"number alias":          replace("<bus>1</bus>", "<bus>01</bus>"),
		"hex address":           replace("<bus>1</bus>", "<bus>0x1</bus>"),
		"missing vendor":        replace("<vendor id='0x1234'>Fixture vendor</vendor>", ""),
		"invalid vendor":        replace("0x1234", "0x12gg"),
		"duplicate attr":        replace("id='0x1234'", "id='0x1234' id='0x5678'"),
		"nested label":          replace("Fixture vendor", "<name>Fixture vendor</name>"),
		"control label":         replace("Fixture product", "SECRET&#10;line"),
		"bidi label":            replace("Fixture product", "SECRET\u202e"),
		"large label":           replace("Fixture product", strings.Repeat("p", 1025)),
		"unmodeled serial":      replace("</capability>", "<serial>unmodeled</serial></capability>"),
		"path traversal":        replace("/sys/devices/controller/usb1/1-2", "/sys/devices/../kernel"),
		"other sysfs root":      replace("/sys/devices/controller/usb1/1-2", "/sys/bus/usb/devices/1-2"),
		"arbitrary path":        replace("/sys/devices/controller/usb1/1-2", "/etc/SECRET"),
		"duplicate path":        replace("</path>", "</path><path>/sys/devices/other</path>"),
		"DTD":                   "<!DOCTYPE device>" + base,
		"duplicate declaration": "<?xml version='1.0'?><?xml version='1.0'?>" + base,
		"entity":                replace("Fixture product", "&outside;"),
		"trailing document":     base + base,
		"malformed":             base[:len(base)-1],
		"large document":        base + strings.Repeat(" ", usbMaxXML),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := parseUSBDevice("usb_fixture", raw)
			usbCode(t, err, "OPERATION_FAILED")
		})
	}
}

func TestUSBInventorySysfsRefusalsAndNoSpecialFileIO(t *testing.T) {
	for _, kind := range []string{"leaf symlink", "parent symlink", "root symlink", "fifo", "directory attribute", "oversized serial", "control serial", "invalid UTF8", "wrong vendor", "wrong product", "wrong bus", "wrong device", "wrong port", "deep port", "missing required", "non sysfs"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			n, dir := usbFixture(t, root, "1-2", "4", "serial")
			s := usbSysfs{root: root, fixture: true}
			write := func(name, value string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(value), 0600); err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "leaf symlink":
				if err := os.Remove(filepath.Join(dir, "serial")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/dev/null", filepath.Join(dir, "serial")); err != nil {
					t.Fatal(err)
				}
			case "parent symlink":
				moved := dir + "-old"
				if err := os.Rename(dir, moved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(moved, dir); err != nil {
					t.Fatal(err)
				}
			case "root symlink":
				link := filepath.Join(t.TempDir(), "root")
				if err := os.Symlink(root, link); err != nil {
					t.Fatal(err)
				}
				s.root = link
			case "fifo":
				if err := os.Remove(filepath.Join(dir, "serial")); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(filepath.Join(dir, "serial"), 0600); err != nil {
					t.Fatal(err)
				}
			case "directory attribute":
				if err := os.Remove(filepath.Join(dir, "serial")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(dir, "serial"), 0700); err != nil {
					t.Fatal(err)
				}
			case "oversized serial":
				write("serial", strings.Repeat("s", 1025)+"\n")
			case "control serial":
				write("serial", "SECRET\nSECOND\n")
			case "invalid UTF8":
				write("serial", "\xff\n")
			case "wrong vendor":
				write("idVendor", "4321\n")
			case "wrong product":
				write("idProduct", "0001\n")
			case "wrong bus":
				write("busnum", "2\n")
			case "wrong device":
				write("devnum", "5\n")
			case "wrong port":
				write("devpath", "3\n")
			case "deep port":
				write("devpath", "1.2.3.4.5.6.7.8\n")
			case "missing required":
				if err := os.Remove(filepath.Join(dir, "idVendor")); err != nil {
					t.Fatal(err)
				}
			case "non sysfs":
				s.fixture = false
			}
			out, err := usbCollect(context.Background(), s.observe, n)
			if err == nil || out != nil {
				t.Fatalf("unsupported source yielded inventory: %+v %v", out, err)
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("source text leaked: %v", err)
			}
		})
	}
}

func TestUSBInventoryMissingOptionalAttributesAndRootHubPort(t *testing.T) {
	for _, mode := range []string{"no serial or port", "empty serial", "root hub"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			n, dir := usbFixture(t, root, "1-2", "4", "")
			switch mode {
			case "no serial or port":
				if err := os.Remove(filepath.Join(dir, "devpath")); err != nil {
					t.Fatal(err)
				}
			case "empty serial":
				if err := os.WriteFile(filepath.Join(dir, "serial"), []byte("\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "root hub":
				parent := filepath.Dir(dir)
				for _, name := range []string{"idVendor", "idProduct", "busnum", "devnum"} {
					value, err := os.ReadFile(filepath.Join(dir, name))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(parent, name), value, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(parent, "devpath"), []byte("0\n"), 0600); err != nil {
					t.Fatal(err)
				}
				n.raw = strings.Replace(n.raw, "/usb1/1-2</path>", "/usb1</path>", 1)
				if err := os.WriteFile(filepath.Join(parent, "devnum"), []byte("1\n"), 0600); err != nil {
					t.Fatal(err)
				}
				n.raw = strings.Replace(n.raw, "<device>4</device>", "<device>1</device>", 1)
			}
			out, err := usbCollect(context.Background(), (usbSysfs{root: root, fixture: true}).observe, n)
			if err != nil {
				t.Fatal(err)
			}
			if out[0].Serial != "" {
				t.Fatal("missing serial invented")
			}
			if mode != "empty serial" && (out[0].PhysicalPort != "" || out[0].StableID != "" || !out[0].Ambiguous) {
				t.Fatalf("missing usable topology invented: %+v", out[0])
			}
		})
	}
}

func TestUSBInventoryDetectsNativeAndSysfsDrift(t *testing.T) {
	for _, kind := range []string{"native name", "native address", "serial", "disappearance", "directory replacement", "attribute replacement"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			n, dir := usbFixture(t, root, "1-2", "4", "serial")
			n.afterXML = func(read int) {
				if read != 2 {
					return
				}
				switch kind {
				case "native name":
					n.raw = strings.Replace(n.raw, n.name, "other", 1)
				case "native address":
					n.raw = strings.Replace(n.raw, "<device>4</device>", "<device>5</device>", 1)
				case "serial":
					if err := os.WriteFile(filepath.Join(dir, "serial"), []byte("changed\n"), 0600); err != nil {
						t.Fatal(err)
					}
				case "disappearance":
					if err := os.RemoveAll(dir); err != nil {
						t.Fatal(err)
					}
				case "directory replacement":
					if err := os.Rename(dir, dir+"-old"); err != nil {
						t.Fatal(err)
					}
					usbFixture(t, root, "1-2", "4", "serial")
				case "attribute replacement":
					file := filepath.Join(dir, "serial")
					if err := os.Rename(file, file+"-old"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, []byte("serial\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			out, err := usbCollect(context.Background(), (usbSysfs{root: root, fixture: true}).observe, n)
			if err == nil || out != nil {
				t.Fatalf("drift yielded inventory: %+v %v", out, err)
			}
		})
	}
}

func TestUSBInventoryBoundsDuplicatesAndFailureAreNotEmptySuccess(t *testing.T) {
	for _, count := range []int{-1, usbMaxDevices + 1} {
		out, err := collectUSB(context.Background(), count, func(int) usbObservation { t.Fatal("out of bound inventory read"); return nil }, usbPlainFacts)
		if err == nil || out != nil {
			t.Fatal("count accepted")
		}
	}
	for _, kind := range []string{"name", "address", "path"} {
		t.Run("duplicate "+kind, func(t *testing.T) {
			one := &usbFakeNode{name: "one", raw: usbXML("one", "", "<bus>1</bus><device>2</device>")}
			two := &usbFakeNode{name: "two", raw: usbXML("two", "", "<bus>1</bus><device>3</device>")}
			switch kind {
			case "name":
				two.name = "one"
				two.raw = strings.Replace(two.raw, "two", "one", 1)
			case "address":
				two.raw = strings.Replace(two.raw, "<device>3</device>", "<device>2</device>", 1)
			case "path":
				one.raw = usbXML("one", "/sys/devices/same", "")
				two.raw = usbXML("two", "/sys/devices/same", "")
			}
			out, err := usbCollect(context.Background(), usbPlainFacts, one, two)
			if err == nil || out != nil {
				t.Fatal("duplicate native identity accepted")
			}
		})
	}
	t.Run("aggregate XML", func(t *testing.T) {
		var nodes []*usbFakeNode
		for i := 0; i < 80; i++ {
			name := "usb_" + strconv.Itoa(i)
			raw := usbXML(name, "", "") + strings.Repeat(" ", 60000)
			nodes = append(nodes, &usbFakeNode{name: name, raw: raw})
		}
		out, err := usbCollect(context.Background(), usbPlainFacts, nodes...)
		if err == nil || out != nil {
			t.Fatal("aggregate XML limit omitted")
		}
	})
	t.Run("serialized output including escaped match keys", func(t *testing.T) {
		var nodes []*usbFakeNode
		for i := 0; i < 2100; i++ {
			name := "usb_" + strconv.Itoa(i)
			nodes = append(nodes, &usbFakeNode{name: name, raw: usbXML(name, "", "")})
		}
		out, err := usbCollect(context.Background(), func(ctx context.Context, d domain.USBDevice, p string) (usbFacts, error) {
			d.Serial = strings.Repeat(" ", 1024)
			return usbFacts{device: d}, nil
		}, nodes...)
		if err == nil || out != nil {
			t.Fatal("serialized response limit omitted")
		}
	})
}

func TestUSBInventoryCancellationNativeFailuresAndCleanup(t *testing.T) {
	for _, phase := range []string{"open", "list", "name", "XML", "sysfs", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			n := &usbFakeNode{name: "usb_one", raw: usbXML("usb_one", "", "")}
			s := &usbFakeSession{nodes: []usbObservation{n}}
			if phase == "list" {
				s.afterList = cancel
			}
			if phase == "name" {
				n.nameErr = errors.New("SECRET native")
			}
			if phase == "XML" {
				n.afterXML = func(int) { cancel() }
			}
			if phase == "cleanup" {
				s.afterClose = cancel
			}
			out, err := inspectUSB(ctx, "qemu:///system", func(uri string) (usbSession, error) {
				if uri != "qemu:///system" {
					t.Fatal("wrong URI")
				}
				if phase == "open" {
					cancel()
				}
				return s, nil
			}, func(ctx context.Context, d domain.USBDevice, p string) (usbFacts, error) {
				if phase == "sysfs" {
					cancel()
				}
				return usbPlainFacts(ctx, d, p)
			})
			if err == nil || out != nil || s.closes != 1 {
				t.Fatalf("out=%v err=%v closes=%d", out, err, s.closes)
			}
			if phase != "name" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if strings.Contains(err.Error(), "SECRET") {
				t.Fatal("native error leaked")
			}
		})
	}
	for _, code := range []native.ErrorNumber{native.ERR_NO_SUPPORT, native.ERR_ACCESS_DENIED, native.ERR_INTERNAL_ERROR} {
		s := &usbFakeSession{listErr: native.Error{Code: code, Message: "SECRET"}}
		out, err := inspectUSB(context.Background(), "qemu:///system", func(string) (usbSession, error) { return s, nil }, usbPlainFacts)
		if err == nil || out != nil || s.closes != 1 || strings.Contains(err.Error(), "SECRET") {
			t.Fatalf("out=%v err=%v close=%d", out, err, s.closes)
		}
	}
	s := &usbFakeSession{closeErr: errors.New("SECRET close")}
	out, err := inspectUSB(context.Background(), "qemu:///session", func(string) (usbSession, error) { return s, nil }, usbPlainFacts)
	if err == nil || out != nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("cleanup returned empty success")
	}
}

func TestUSBInventoryRejectsUnsupportedURIAndCanceledContextBeforeOpen(t *testing.T) {
	open := func(string) (usbSession, error) { t.Fatal("native connection opened"); return nil, nil }
	for _, uri := range []string{"", "test:///default", "qemu+ssh://host/system"} {
		_, err := inspectUSB(context.Background(), uri, open, usbPlainFacts)
		usbCode(t, err, "UNSUPPORTED_CAPABILITY")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := inspectUSB(ctx, "qemu:///system", open, usbPlainFacts); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
