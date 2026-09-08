//go:build linux && cgo

package libvirt

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
)

// These seams hold generated definitions only. They never enumerate the host,
// connect to libvirt or issue a native network mutation.
type creationNetworkFixture struct {
	value                      domain.VirtualNetwork
	reads, starts, frees       int
	onRead                     func(int)
	onStart                    func()
	readErr, startErr, freeErr error
}

func (n *creationNetworkFixture) GetUUIDString() (string, error) {
	n.reads++
	if n.onRead != nil {
		n.onRead(n.reads)
	}
	return n.value.Key.UUID, n.readErr
}
func (n *creationNetworkFixture) GetName() (string, error)    { return n.value.Name, nil }
func (n *creationNetworkFixture) IsActive() (bool, error)     { return n.value.Active, nil }
func (n *creationNetworkFixture) IsPersistent() (bool, error) { return n.value.Persistent, nil }
func (n *creationNetworkFixture) GetAutostart() (bool, error) { return n.value.Autostart, nil }
func (n *creationNetworkFixture) GetXMLDesc(flags native.NetworkXMLFlags) (string, error) {
	if flags == native.NETWORK_XML_INACTIVE {
		return n.value.PersistentXML, nil
	}
	return n.value.LiveXML, nil
}
func (n *creationNetworkFixture) Create() error {
	n.starts++
	n.value.Active = true
	n.value.LiveXML = n.value.PersistentXML
	if n.onStart != nil {
		n.onStart()
	}
	return n.startErr
}
func (n *creationNetworkFixture) Free() error { n.frees++; return n.freeErr }

type creationConnectionFixture struct {
	def                                                   domain.NetworkDefinition
	network                                               *creationNetworkFixture
	others                                                []domain.VirtualNetwork
	names                                                 []string
	forwarding                                            bool
	forwardReads                                          int
	forwardErr                                            error
	onForward                                             func()
	scans, interfaceScans, defines, lookups, closes       int
	writes                                                []bool
	onScan                                                func(int)
	onInterfaces, onDefine, onClose                       func()
	scanErr, interfaceErr, defineErr, lookupErr, closeErr error
	definedXML                                            string
}

func (c *creationConnectionFixture) networks(context.Context) ([]domain.VirtualNetwork, error) {
	c.scans++
	if c.onScan != nil {
		c.onScan(c.scans)
	}
	out := append([]domain.VirtualNetwork{}, c.others...)
	if c.network != nil {
		out = append(out, c.network.value)
	}
	return out, c.scanErr
}
func (c *creationConnectionFixture) interfaces(context.Context) ([]string, error) {
	c.interfaceScans++
	if c.onInterfaces != nil {
		c.onInterfaces()
	}
	return c.names, c.interfaceErr
}
func (c *creationConnectionFixture) ipv4Forwarding(ctx context.Context) (bool, error) {
	c.forwardReads++
	if c.onForward != nil {
		c.onForward()
	}
	return c.forwarding, errors.Join(c.forwardErr, ctx.Err())
}
func (c *creationConnectionFixture) lookup(id string) (networkCreationHandle, error) {
	c.lookups++
	if c.lookupErr != nil {
		return nil, c.lookupErr
	}
	if c.network == nil || c.network.value.Key.UUID != id {
		return nil, errors.New("generated missing network")
	}
	return c.network, nil
}
func (c *creationConnectionFixture) define(raw string) (networkCreationHandle, error) {
	c.defines++
	c.definedXML = raw
	c.network = &creationNetworkFixture{value: domain.VirtualNetwork{
		Key:  domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "network", UUID: c.def.UUID},
		Name: c.def.Name, Persistent: true, PersistentXML: raw,
	}}
	if c.onDefine != nil {
		c.onDefine()
	}
	if c.defineErr != nil {
		return nil, c.defineErr
	}
	return c.network, nil
}
func (c *creationConnectionFixture) close() error {
	c.closes++
	if c.onClose != nil {
		c.onClose()
	}
	return c.closeErr
}
func (c *creationConnectionFixture) open(uri string, write bool) (networkCreationConnection, error) {
	c.writes = append(c.writes, write)
	return c, nil
}

func newCreationNetworkFixture(t *testing.T, defined bool) *creationConnectionFixture {
	t.Helper()
	c := &creationConnectionFixture{def: domain.NetworkDefinition{
		UUID: "12345678-1234-4234-8234-123456789abc", Name: "virmill-12345678-1234-4234-8234-123456789abc", Bridge: "vm123456781234",
		Type: "nat", IPv4CIDR: "192.168.230.0/24", DHCPEnabled: true, AdvertiseDefaultRoute: true, IPv6Mode: "disabled", HostAccess: "allow", Egress: "any",
	}, names: []string{"lo", "eth0"}, forwarding: true}
	if defined {
		raw, err := networkxml.Render(c.def)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.define(raw); err != nil {
			t.Fatal(err)
		}
		c.defines = 0
	}
	return c
}

func networkCreationError(t *testing.T, out domain.VirtualNetwork, err error, code string) {
	t.Helper()
	if !reflect.DeepEqual(out, domain.VirtualNetwork{}) {
		t.Fatalf("error returned a successful observation: %+v", out)
	}
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func foreignCreationNetwork(bridge string) domain.VirtualNetwork {
	const uuid = "98765432-4321-4321-8321-123456789abc"
	return domain.VirtualNetwork{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "network", UUID: uuid}, Name: "foreign", Persistent: true,
		PersistentXML: `<network><name>foreign</name><uuid>` + uuid + `</uuid><bridge name="` + bridge + `"/><dns enable="no"/></network>`}
}

func TestNetworkCreationInputAndConnectionGuardsDoNotOpen(t *testing.T) {
	for _, uri := range []string{"", "test:///default", "qemu+ssh://host/system"} {
		c := newCreationNetworkFixture(t, false)
		out, err := networkCreationRun(context.Background(), uri, c.def, "define", c.open)
		networkCreationError(t, out, err, "UNSUPPORTED_CAPABILITY")
		if len(c.writes) != 0 || c.defines != 0 {
			t.Fatal(c)
		}
	}
	for _, field := range []string{"uuid", "bridge", "cidr", "ipv6", "profile"} {
		t.Run(field, func(t *testing.T) {
			c := newCreationNetworkFixture(t, false)
			switch field {
			case "uuid":
				c.def.UUID = "absent"
			case "bridge":
				c.def.Bridge = "eth0"
			case "cidr":
				c.def.IPv4CIDR = "8.8.8.0/24"
			case "ipv6":
				c.def.IPv6Mode = "nat"
			case "profile":
				c.def.HostAccess = "deny"
			}
			out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, "define", c.open)
			if err == nil || !reflect.DeepEqual(out, domain.VirtualNetwork{}) || len(c.writes) != 0 || c.defines != 0 {
				t.Fatalf("invalid request reached backend: %+v %v", out, err)
			}
		})
	}
}

func TestNetworkCreationRefusesAllObservedIdentityAndBridgeCollisions(t *testing.T) {
	for _, kind := range []string{"uuid", "name", "inactive-bridge", "active-bridge", "host-interface", "second-scan"} {
		t.Run(kind, func(t *testing.T) {
			c := newCreationNetworkFixture(t, false)
			other := foreignCreationNetwork("oldbridge")
			switch kind {
			case "uuid":
				other.Key.UUID = c.def.UUID
			case "name":
				other.Name = c.def.Name
			case "inactive-bridge":
				other = foreignCreationNetwork(c.def.Bridge)
			case "active-bridge":
				other.Active = true
				other.LiveXML = foreignCreationNetwork(c.def.Bridge).PersistentXML
			case "host-interface":
				c.names = append(c.names, c.def.Bridge)
			case "second-scan":
				c.onScan = func(n int) {
					if n == 2 {
						c.others = []domain.VirtualNetwork{foreignCreationNetwork(c.def.Bridge)}
					}
				}
			}
			if kind != "second-scan" {
				c.others = []domain.VirtualNetwork{other}
			}
			out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, "define", c.open)
			networkCreationError(t, out, err, "RESOURCE_BUSY")
			if c.defines != 0 || c.closes != 1 {
				t.Fatal(c)
			}
			if kind == "second-scan" && c.scans != 2 {
				t.Fatal("definition did not repeat collision inventory")
			}
		})
	}
}

func TestNetworkCreationIncompleteInventoriesRefuseWithoutDefinition(t *testing.T) {
	for _, kind := range []string{"network-read", "interface-read", "duplicate-uuid", "duplicate-name", "missing-xml", "identity-mismatch", "foreign-bridge", "inventory-limit", "interface-limit"} {
		t.Run(kind, func(t *testing.T) {
			c := newCreationNetworkFixture(t, false)
			other := foreignCreationNetwork("oldbridge")
			c.others = []domain.VirtualNetwork{other}
			switch kind {
			case "network-read":
				c.scanErr = errors.New("generated enumeration failure")
			case "interface-read":
				c.interfaceErr = domain.Fail("UNSUPPORTED_CAPABILITY", "generated configured-interface failure")
			case "duplicate-uuid":
				c.others = append(c.others, other)
			case "duplicate-name":
				other.Key.UUID = "a8765432-4321-4321-8321-123456789abc"
				c.others = append(c.others, other)
			case "missing-xml":
				c.others[0].PersistentXML = ""
			case "identity-mismatch":
				c.others[0].PersistentXML = strings.ReplaceAll(other.PersistentXML, "foreign", "different")
			case "foreign-bridge":
				c.others[0].PersistentXML = strings.Replace(other.PersistentXML, `<bridge name=`, `<bridge xmlns="urn:foreign" name=`, 1)
			case "inventory-limit":
				c.others = make([]domain.VirtualNetwork, networkCreationInventoryLimit+1)
			case "interface-limit":
				c.names = make([]string, 2*networkCreationInventoryLimit+1)
			}
			out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, "define", c.open)
			if err == nil || !reflect.DeepEqual(out, domain.VirtualNetwork{}) || c.defines != 0 || c.closes != 1 {
				t.Fatalf("incomplete inventory admitted mutation: %+v %v %+v", out, err, c)
			}
		})
	}
}

func TestNetworkCreationDefinesInactiveThenActivatesExactReviewedNetwork(t *testing.T) {
	for _, profile := range []string{"nat", "lab"} {
		t.Run(profile, func(t *testing.T) {
			c := newCreationNetworkFixture(t, false)
			if profile == "lab" {
				c.def.Type = "lab"
				c.def.Egress = "none"
				c.def.AdvertiseDefaultRoute = false
			}
			_, err := networkCreationRun(context.Background(), "qemu:///system", c.def, "check", c.open)
			if err != nil || c.defines != 0 || !reflect.DeepEqual(c.writes, []bool{false}) {
				t.Fatal(err, c)
			}
			_, err = networkCreationRun(context.Background(), "qemu:///system", c.def, "define", c.open)
			if err != nil || c.defines != 1 || c.network.starts != 0 || c.network.value.Active || c.network.value.Autostart || !c.network.value.Persistent {
				t.Fatal("definition did not remain powered off", err, c)
			}
			if err = networkxml.Match(c.definedXML, c.def); err != nil {
				t.Fatal(err)
			}
			_, err = networkCreationRun(context.Background(), "qemu:///system", c.def, "activate", c.open)
			if err != nil || c.defines != 1 || c.network.starts != 1 || !c.network.value.Active {
				t.Fatal(err, c)
			}
			observed, err := networkCreationRun(context.Background(), "qemu:///system", c.def, "inspect", c.open)
			if err != nil || !observed.Active || observed.IsolationVerification != "not-run" || observed.Ownership != "external" || observed.Fingerprint == "" {
				t.Fatalf("incorrect native observation: %+v %v", observed, err)
			}
			if !reflect.DeepEqual(c.writes, []bool{false, true, true, false}) {
				t.Fatal("connection access modes", c.writes)
			}
			out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, "activate", c.open)
			networkCreationError(t, out, err, "STALE_PLAN")
			if c.network.starts != 1 {
				t.Fatal("activation replayed")
			}
		})
	}
}

func TestNetworkCreationActivateNeverAdoptsDifferentOrChangedXML(t *testing.T) {
	for _, change := range []string{"persistent-xml", "live-xml", "uuid", "name", "transient", "autostart", "changed-before-start", "host-bridge", "other-reservation"} {
		t.Run(change, func(t *testing.T) {
			c := newCreationNetworkFixture(t, true)
			n := c.network
			switch change {
			case "persistent-xml":
				n.value.PersistentXML = strings.Replace(n.value.PersistentXML, "192.168.230.", "192.168.231.", -1)
			case "live-xml":
				n.value.Active = true
				n.value.LiveXML = foreignCreationNetwork("otherbridge").PersistentXML
			case "uuid":
				n.value.Key.UUID = "98765432-4321-4321-8321-123456789abc"
			case "name":
				n.value.Name = "changed"
			case "transient":
				n.value.Persistent = false
			case "autostart":
				n.value.Autostart = true
			case "changed-before-start":
				n.onRead = func(read int) {
					if read == 2 {
						n.value.Autostart = true
					}
				}
			case "host-bridge":
				c.names = append(c.names, c.def.Bridge)
			case "other-reservation":
				c.others = []domain.VirtualNetwork{foreignCreationNetwork(c.def.Bridge)}
			}
			out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, "activate", c.open)
			if err == nil || !reflect.DeepEqual(out, domain.VirtualNetwork{}) || n.starts != 0 || c.defines != 0 || c.closes != 1 {
				t.Fatalf("mismatched network activated: %+v %v %+v", out, err, c)
			}
		})
	}
}

func TestNetworkCreationLostAcknowledgementsRequireInspectionWithoutReplay(t *testing.T) {
	for _, action := range []string{"define", "activate"} {
		t.Run(action, func(t *testing.T) {
			c := newCreationNetworkFixture(t, action == "activate")
			fault := errors.New("generated lost native acknowledgement")
			if action == "define" {
				c.defineErr = fault
			} else {
				c.network.startErr = fault
			}
			out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, action, c.open)
			networkCreationError(t, out, err, "RECOVERY_REQUIRED")
			observed, err := networkCreationRun(context.Background(), "qemu:///system", c.def, "inspect", c.open)
			if err != nil || observed.Active != (action == "activate") {
				t.Fatalf("read-only recovery observation failed: %+v %v", observed, err)
			}
			out, err = networkCreationRun(context.Background(), "qemu:///system", c.def, action, c.open)
			want := "RESOURCE_BUSY"
			if action == "activate" {
				want = "STALE_PLAN"
			}
			networkCreationError(t, out, err, want)
			if (action == "define" && (c.defines != 1 || c.network.starts != 0)) || (action == "activate" && (c.defines != 0 || c.network.starts != 1)) {
				t.Fatal("uncertain effect was repeated", c)
			}
		})
	}
}

func TestNetworkCreationCancellationAndCleanupDoNotReturnSuccess(t *testing.T) {
	for _, phase := range []string{"before-open", "inventory", "interfaces", "after-define", "after-start", "during-inspect", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			c := newCreationNetworkFixture(t, phase == "after-start" || phase == "during-inspect")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			action := "define"
			switch phase {
			case "before-open":
				cancel()
			case "inventory":
				c.onScan = func(int) { cancel() }
			case "interfaces":
				c.onInterfaces = cancel
			case "after-define":
				c.onDefine = cancel
			case "after-start":
				action = "activate"
				c.network.onStart = cancel
			case "during-inspect":
				action = "inspect"
				c.network.onRead = func(int) { cancel() }
			case "cleanup":
				c.onClose = cancel
			}
			out, err := networkCreationRun(ctx, "qemu:///system", c.def, action, c.open)
			if phase == "after-define" || phase == "after-start" || phase == "cleanup" {
				networkCreationError(t, out, err, "RECOVERY_REQUIRED")
			} else if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(out, domain.VirtualNetwork{}) {
				t.Fatal("cancellation not respected", out, err)
			}
			if phase == "before-open" {
				if len(c.writes) != 0 {
					t.Fatal(c)
				}
			} else if c.closes != 1 {
				t.Fatal(c)
			}
			if (phase == "inventory" || phase == "interfaces") && c.defines != 0 {
				t.Fatal("canceled preflight defined a network")
			}
		})
	}
	for _, action := range []string{"define", "activate", "inspect"} {
		c := newCreationNetworkFixture(t, action != "define")
		c.closeErr = errors.New("generated close failure")
		out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, action, c.open)
		if err == nil || !reflect.DeepEqual(out, domain.VirtualNetwork{}) || c.closes != 1 || c.network.frees != 1 {
			t.Fatalf("cleanup error hidden: %+v %v", out, err)
		}
	}
}

func TestNetworkCreationNATRequiresPreexistingForwardingWithoutHostSetup(t *testing.T) {
	for _, action := range []string{"check", "define", "activate"} {
		for _, fault := range []string{"disabled", "read-error", "canceled"} {
			t.Run(action+"/"+fault, func(t *testing.T) {
				c := newCreationNetworkFixture(t, action == "activate")
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				switch fault {
				case "disabled":
					c.forwarding = false
				case "read-error":
					c.forwardErr = domain.Fail("UNSUPPORTED_CAPABILITY", "generated unreadable forwarding prerequisite")
				case "canceled":
					c.onForward = cancel
				}
				out, err := networkCreationRun(ctx, "qemu:///system", c.def, action, c.open)
				if fault == "canceled" {
					if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(out, domain.VirtualNetwork{}) {
						t.Fatal(out, err)
					}
				} else {
					networkCreationError(t, out, err, "UNSUPPORTED_CAPABILITY")
				}
				if c.forwardReads != 1 || c.defines != 0 || (c.network != nil && c.network.starts != 0) || c.closes != 1 {
					t.Fatal("forwarding refusal performed a native effect or skipped cleanup", c)
				}
			})
		}
	}
	t.Run("rechecked-before-definition", func(t *testing.T) {
		c := newCreationNetworkFixture(t, false)
		c.onForward = func() { c.forwarding = c.forwardReads == 1 }
		out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, "define", c.open)
		networkCreationError(t, out, err, "UNSUPPORTED_CAPABILITY")
		if c.forwardReads != 2 || c.defines != 0 {
			t.Fatal(c)
		}
	})
	t.Run("lab-does-not-require-global-forwarding", func(t *testing.T) {
		c := newCreationNetworkFixture(t, false)
		c.def.Type, c.def.Egress, c.def.AdvertiseDefaultRoute = "lab", "none", false
		c.forwarding = false
		for _, action := range []string{"check", "define", "activate", "inspect"} {
			if _, err := networkCreationRun(context.Background(), "qemu:///system", c.def, action, c.open); err != nil {
				t.Fatal(action, err)
			}
		}
		if c.forwardReads != 0 || c.defines != 1 || c.network.starts != 1 {
			t.Fatal(c)
		}
	})
}

func TestNetworkCreationNativeSuccessRequiresExactPostcondition(t *testing.T) {
	for _, fault := range []string{"define-active", "define-different", "activate-inactive", "activate-different", "activate-read-failed"} {
		t.Run(fault, func(t *testing.T) {
			action := "define"
			if strings.HasPrefix(fault, "activate") {
				action = "activate"
			}
			c := newCreationNetworkFixture(t, action == "activate")
			change := func() {
				switch fault {
				case "define-active":
					c.network.value.Active = true
					c.network.value.LiveXML = c.network.value.PersistentXML
				case "define-different", "activate-different":
					c.network.value.PersistentXML = foreignCreationNetwork("otherbridge").PersistentXML
				case "activate-inactive":
					c.network.value.Active = false
					c.network.value.LiveXML = ""
				case "activate-read-failed":
					c.network.readErr = errors.New("generated failed postcondition observation")
				}
			}
			if action == "define" {
				c.onDefine = change
			} else {
				c.network.onStart = change
			}
			out, err := networkCreationRun(context.Background(), "qemu:///system", c.def, action, c.open)
			networkCreationError(t, out, err, "RECOVERY_REQUIRED")
			if c.closes != 1 || c.network.frees != 1 || c.defines+c.network.starts != 1 {
				t.Fatal(c)
			}
		})
	}
}

func TestNetworkCreationBridgeProjectionRefusesAmbiguity(t *testing.T) {
	v := foreignCreationNetwork("reserved")
	got, err := networkCollisionBridge(v.PersistentXML, v.Key.UUID, v.Name)
	if err != nil || got != "reserved" {
		t.Fatal(got, err)
	}
	for _, raw := range []string{
		strings.Replace(v.PersistentXML, `name="reserved"`, `name="reserved" name="different"`, 1),
		strings.Replace(v.PersistentXML, `<bridge name="reserved"/>`, `<bridge name="reserved"/><bridge name="different"/>`, 1),
		strings.Replace(v.PersistentXML, `<bridge name="reserved"/>`, `<bridge xmlns:x="urn:foreign" x:name="reserved"/>`, 1),
		strings.Replace(v.PersistentXML, `<bridge name="reserved"/>`, `<bridge/>`, 1),
		strings.Replace(v.PersistentXML, `name="reserved"`, `name="invalid bridge"`, 1),
		strings.Replace(v.PersistentXML, `name="reserved"`, `name="0123456789012345"`, 1),
		strings.Replace(v.PersistentXML, `<name>foreign</name>`, `<name><nested/>foreign</name>`, 1),
		strings.Replace(v.PersistentXML, `<name>foreign</name>`, `<name>`+strings.Repeat(" ", 4097)+`foreign</name>`, 1),
		`<!DOCTYPE network [<!ENTITY x "unsafe">]>` + v.PersistentXML,
		`<?xml version="1.0"?><?xml version="1.0"?>` + v.PersistentXML,
		v.PersistentXML + v.PersistentXML,
		strings.Repeat("x", networkCreationXMLLimit+1),
	} {
		if _, err := networkCollisionBridge(raw, v.Key.UUID, v.Name); err == nil {
			t.Fatal("ambiguous foreign network XML accepted")
		}
	}
	noBridge := strings.Replace(v.PersistentXML, `<bridge name="reserved"/>`, "", 1)
	if got, err := networkCollisionBridge(noBridge, v.Key.UUID, v.Name); err != nil || got != "" {
		t.Fatal("network without a bridge was guessed", got, err)
	}
}
