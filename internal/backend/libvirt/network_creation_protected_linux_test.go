//go:build linux && cgo

package libvirt

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
)

func protectedCreationFixture(t *testing.T, kind string, dhcp, logicalCIDR, defined bool) *creationConnectionFixture {
	t.Helper()
	c := newCreationNetworkFixture(t, false)
	c.def.Type, c.def.HostAccess, c.def.DHCPEnabled = kind, "services-only", dhcp
	c.def.AdvertiseDefaultRoute = kind == "nat" && dhcp
	if kind != "nat" {
		c.def.Egress = "none"
	}
	if kind == "guest-only" {
		c.def.HostAccess = "deny"
		if !logicalCIDR {
			c.def.IPv4CIDR = ""
		}
	}
	if defined {
		x, err := networkxml.Render(c.def)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.define(x); err != nil {
			t.Fatal(err)
		}
		c.defines = 0
	}
	return c
}

func TestNetworkCreationProtectedProfilesKeepDeclaredServiceBoundary(t *testing.T) {
	for _, tc := range []struct {
		kind              string
		dhcp, logicalCIDR bool
	}{
		{"nat", true, true}, {"nat", false, true}, {"lab", true, true}, {"lab", false, true}, {"guest-only", false, false}, {"guest-only", false, true},
	} {
		t.Run(fmt.Sprintf("%s/dhcp=%t/cidr=%t", tc.kind, tc.dhcp, tc.logicalCIDR), func(t *testing.T) {
			c := protectedCreationFixture(t, tc.kind, tc.dhcp, tc.logicalCIDR, false)
			if tc.kind != "nat" {
				c.forwardErr = errors.New("this profile must not read or modify global forwarding")
			}
			for _, action := range []string{"check", "define", "activate", "inspect"} {
				out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, action, c.open)
				if err != nil {
					t.Fatal(action, err)
				}
				if action == "define" && (out.Active || !out.Persistent || out.Autostart || c.network.starts != 0) {
					t.Fatal("definition activated early", out)
				}
				if action == "inspect" && (!out.Active || out.Ownership != "external" || out.IsolationVerification != "not-run") {
					t.Fatal("XML observation promoted packet or ownership proof", out)
				}
			}
			if c.defines != 1 || c.network.starts != 1 || !reflect.DeepEqual(c.writes, []bool{false, true, true, false}) {
				t.Fatal(c)
			}
			wantForward := 0
			if tc.kind == "nat" {
				wantForward = 4
			}
			if c.forwardReads != wantForward {
				t.Fatal("incorrect forwarding prerequisite reads", c.forwardReads)
			}
			if tc.kind == "guest-only" && (strings.Contains(c.definedXML, "<ip") || strings.Contains(c.definedXML, "<forward") || strings.Contains(c.definedXML, "<dhcp")) {
				t.Fatal("logical reservation installed host configuration", c.definedXML)
			}
			dns := "no"
			if tc.dhcp {
				dns = "yes"
			}
			if !strings.Contains(c.definedXML, `<dns enable="`+dns+`"/>`) {
				t.Fatal("paired DNS declaration missing")
			}
		})
	}
}

func TestNetworkCreationGuestOnlyStillRejectsAllIdentityAndBridgeCollisions(t *testing.T) {
	for _, logicalCIDR := range []bool{false, true} {
		for _, collision := range []string{"native-bridge", "host-interface", "uuid", "name", "second-observation"} {
			t.Run(fmt.Sprintf("cidr=%t/%s", logicalCIDR, collision), func(t *testing.T) {
				c := protectedCreationFixture(t, "guest-only", false, logicalCIDR, false)
				other := foreignCreationNetwork("foreignbr")
				switch collision {
				case "native-bridge":
					other = foreignCreationNetwork(c.def.Bridge)
				case "host-interface":
					c.names = append(c.names, c.def.Bridge)
				case "uuid":
					other.Key.UUID = c.def.UUID
				case "name":
					other.Name = c.def.Name
				case "second-observation":
					c.onScan = func(n int) {
						if n == 2 {
							c.names = append(c.names, c.def.Bridge)
						}
					}
				}
				c.others = []domain.VirtualNetwork{other}
				out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, "define", c.open)
				networkCreationError(t, out, err, "RESOURCE_BUSY")
				if c.defines != 0 || c.forwardReads != 0 {
					t.Fatal("guest-only collision performed native effects", c)
				}
			})
		}
	}
}

func TestNetworkCreationProtectedStateDriftNeverActivatesOrReturnsSuccess(t *testing.T) {
	for _, kind := range []string{"nat", "lab", "guest-only"} {
		for _, drift := range []string{"DNS", "host-IP", "forward", "marker-v1", "reservation", "persistent-layer", "live-layer", "before-start"} {
			t.Run(kind+"/"+drift, func(t *testing.T) {
				c := protectedCreationFixture(t, kind, false, kind != "guest-only", true)
				change := func() {
					c.network.value.PersistentXML = strings.Replace(c.network.value.PersistentXML, `<dns enable="no"/>`, `<dns enable="yes"/>`, 1)
				}
				action := "activate"
				switch drift {
				case "DNS", "persistent-layer":
					change()
				case "host-IP":
					c.network.value.PersistentXML = strings.Replace(c.network.value.PersistentXML, "</network>", `<ip address="10.0.0.1" prefix="8"/></network>`, 1)
				case "forward":
					c.network.value.PersistentXML = strings.Replace(c.network.value.PersistentXML, "</network>", `<forward mode="open"/></network>`, 1)
				case "marker-v1":
					c.network.value.PersistentXML = strings.Replace(c.network.value.PersistentXML, `version="2"`, `version="1"`, 1)
				case "reservation":
					c.def.IPv4CIDR = "10.0.0.0/8"
				case "live-layer":
					action = "inspect"
					c.network.value.Active = true
					c.network.value.LiveXML = c.network.value.PersistentXML
					c.network.value.LiveXML = strings.Replace(c.network.value.LiveXML, `<dns enable="no"/>`, `<dns enable="yes"/>`, 1)
				case "before-start":
					c.network.onRead = func(n int) {
						if n == 2 {
							change()
						}
					}
				}
				out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, action, c.open)
				if err == nil || !reflect.DeepEqual(out, domain.VirtualNetwork{}) || c.defines != 0 || c.network.starts != 0 {
					t.Fatal("drift authorized activation/success", out, err)
				}
			})
		}
	}
}

func TestNetworkCreationProtectedFaultsRetainOriginalDefinition(t *testing.T) {
	for _, fault := range []string{"lost-define-ack", "canceled-define", "wrong-postcondition"} {
		t.Run(fault, func(t *testing.T) {
			c := protectedCreationFixture(t, "guest-only", false, false, false)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "lost-define-ack":
				c.defineErr = errors.New("generated lost acknowledgement")
			case "canceled-define":
				c.onDefine = cancel
			case "wrong-postcondition":
				c.onDefine = func() {
					c.network.value.PersistentXML = strings.Replace(c.network.value.PersistentXML, "</network>", `<ip address="10.0.0.1" prefix="8"/></network>`, 1)
				}
			}
			out, err := networkCreationRun(ctx, "qemu:///system", c.def, "define", c.open)
			networkCreationError(t, out, err, "RECOVERY_REQUIRED")
			if c.defines != 1 || c.network.starts != 0 || c.network.value.Active {
				t.Fatal("uncertain stopped definition replayed", c)
			}
			observed, err := networkCreationRun(context.Background(), "qemu:///system", c.def, "inspect", c.open)
			if fault == "wrong-postcondition" {
				networkCreationError(t, observed, err, "SOURCE_CHANGED")
			} else if err != nil || !observed.Persistent || observed.Active {
				t.Fatal(observed, err)
			}
			if c.defines != 1 || c.network.starts != 0 {
				t.Fatal("inspection replayed definition", c)
			}
		})
	}
}

// test:///default is libvirt's in-process simulated driver. It creates no host
// bridge, address, firewall, daemon or VM; these checks qualify native XML
// parsing/formatting only. Production Connection still refuses this URI.
func TestNetworkCreationProtectedNativeInMemoryXML(t *testing.T) {
	version, err := native.GetVersion()
	if err != nil {
		t.Fatal("native library version unavailable:", err)
	}
	t.Logf("native libvirt %d.%d.%d; in-memory test driver only", version/1000000, (version/1000)%1000, version%1000)
	for i, tc := range []struct {
		kind              string
		dhcp, logicalCIDR bool
	}{
		{"nat", true, true}, {"nat", false, true}, {"lab", true, true}, {"lab", false, true}, {"guest-only", false, false}, {"guest-only", false, true},
	} {
		t.Run(fmt.Sprintf("%s/dhcp=%t/cidr=%t", tc.kind, tc.dhcp, tc.logicalCIDR), func(t *testing.T) {
			c, err := native.NewConnect("test:///default")
			if err != nil {
				t.Fatal("libvirt in-memory driver unavailable:", err)
			}
			defer func() {
				if _, err := c.Close(); err != nil {
					t.Error(err)
				}
			}()
			d := protectedCreationFixture(t, tc.kind, tc.dhcp, tc.logicalCIDR, false).def
			d.UUID = fmt.Sprintf("abcd%04x-1234-4234-8234-123456789abc", i)
			d.Name = "virmill-" + d.UUID
			d.Bridge = "vm" + strings.ReplaceAll(d.UUID, "-", "")[:12]
			raw, err := networkxml.Render(d)
			if err != nil {
				t.Fatal(err)
			}
			n, err := c.NetworkDefineXMLFlags(raw, native.NETWORK_DEFINE_VALIDATE)
			if err != nil {
				t.Fatal("in-memory driver rejected generated XML:", err)
			}
			defer func() {
				active, err := n.IsActive()
				if err != nil {
					t.Error(err)
				}
				if active {
					if err := n.Destroy(); err != nil {
						t.Error(err)
					}
				}
				if err := n.Undefine(); err != nil {
					t.Error(err)
				}
				if err := n.Free(); err != nil {
					t.Error(err)
				}
			}()
			// This driver rejects NETWORK_XML_INACTIVE. Its flags0 XML checks
			// parser/formatter semantics only; injected tests above exercise the
			// adapter's mandatory independent persistent/live observations.
			active, err := n.IsActive()
			if err != nil || active {
				t.Fatal("in-memory definition active prematurely", err)
			}
			persistent, err := n.IsPersistent()
			if err != nil || !persistent {
				t.Fatal("in-memory definition not persistent", err)
			}
			autostart, err := n.GetAutostart()
			if err != nil || autostart {
				t.Fatal("in-memory definition autostart enabled", err)
			}
			before, err := n.GetXMLDesc(0)
			if err != nil {
				t.Fatal(err)
			}
			if err = networkxml.Match(before, d); err != nil {
				t.Fatal("inactive test-driver formatting differs:", err, before)
			}
			if err = n.Create(); err != nil {
				t.Fatal(err)
			}
			active, err = n.IsActive()
			if err != nil || !active {
				t.Fatal("in-memory activation flag missing", err)
			}
			after, err := n.GetXMLDesc(0)
			if err != nil {
				t.Fatal(err)
			}
			if err = networkxml.Match(after, d); err != nil {
				t.Fatal("active test-driver formatting differs:", err, after)
			}
			if tc.kind == "guest-only" && (strings.Contains(before, "<ip") || strings.Contains(after, "<ip")) {
				t.Fatal("native test driver assigned a host IP to logical guest-only reservation")
			}
			t.Log("native in-memory parser/formatter only; flags0 XML, no persistent/live layer or packet qualification")
		})
	}
}
