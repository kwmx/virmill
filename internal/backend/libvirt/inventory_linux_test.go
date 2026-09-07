//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/json"
	"errors"
	native "libvirt.org/go/libvirt"
	"strings"
	"testing"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

func TestReadOnlyPoolAndNetworkInventoryWithNativeTestDriver(t *testing.T) {
	c, err := native.NewConnectReadOnly("test:///default")
	if err != nil {
		t.Skipf("native test driver unavailable: %v", err)
	}
	defer c.Close()
	pools, err := listPools(context.Background(), c, "test:///default")
	if err != nil || len(pools) == 0 {
		t.Fatal("test pool observation failed", pools, err)
	}
	for _, p := range pools {
		if p.Key.Kind != "storage-pool" || p.Key.UUID == "" || p.Key.ConnectionID != "test:///default" || p.Fingerprint == "" || p.Ownership != "external" || p.XML == "" {
			t.Fatal("incomplete or implicitly adopted pool", p)
		}
		if p.Active && (p.CapacityBytes == nil || p.AvailableBytes == nil) {
			t.Fatal("active pool capacity missing")
		}
		before := p.Fingerprint
		value := uint64(12345)
		p.AvailableBytes = &value
		if poolFingerprint(p) != before {
			t.Fatal("dynamic usage invalidated configuration fingerprint")
		}
		p.XML += "<!-- external configuration edit -->"
		if poolFingerprint(p) == before {
			t.Fatal("external pool change invisible")
		}
	}
	networks, err := listNetworks(context.Background(), c, "test:///default")
	var nativeErr native.Error
	if errors.As(err, &nativeErr) && nativeErr.Code == native.ERR_INVALID_ARG && strings.Contains(nativeErr.Message, "unsupported flags") {
		t.Log("BLOCKED: installed libvirt test driver does not implement inactive network XML; adapter correctly returns its error instead of fabricating persistent state. Native network readback remains unqualified.")
	} else if err != nil || len(networks) == 0 {
		t.Fatal("test network observation failed", networks, err)
	}
	for _, n := range networks {
		if n.Key.Kind != "network" || n.Key.UUID == "" || n.Fingerprint == "" || n.Ownership != "external" || n.IsolationVerification != "not-run" {
			t.Fatal("implicit network adoption or verification claim", n)
		}
		if n.Persistent && n.PersistentXML == "" {
			t.Fatal("persistent XML missing")
		}
		if n.Active && n.LiveXML == "" {
			t.Fatal("live XML missing")
		}
		b, err := json.Marshal(n)
		var decoded domain.VirtualNetwork
		if err != nil || wire.Decode(b, &decoded) != nil || decoded != n {
			t.Fatal("network wire contract drift")
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = listPools(canceled, c, "test:///default"); err == nil {
		t.Fatal("canceled inventory was completed")
	}
	if Connection("test:///default") == nil {
		t.Fatal("release URI policy admitted simulation")
	}
	t.Log("Official libvirt read-only calls used its simulated test driver; no host pool/network was started, created or modified; no routing or storage hardware claim")
}

type networkFixture struct {
	failure   error
	requested []native.NetworkXMLFlags
}

func (*networkFixture) GetUUIDString() (string, error) { return "network-fixture-uuid", nil }
func (*networkFixture) GetName() (string, error)       { return "fixture", nil }
func (*networkFixture) IsActive() (bool, error)        { return true, nil }
func (*networkFixture) IsPersistent() (bool, error)    { return true, nil }
func (*networkFixture) GetAutostart() (bool, error)    { return false, nil }
func (n *networkFixture) GetXMLDesc(flags native.NetworkXMLFlags) (string, error) {
	n.requested = append(n.requested, flags)
	if flags == native.NETWORK_XML_INACTIVE {
		return `<network><name>persistent-fixture</name></network>`, n.failure
	}
	return `<network><name>live-fixture</name></network>`, nil
}
func TestNetworkStateSeparationAndUnsupportedFlagAreExplicit(t *testing.T) {
	n := &networkFixture{}
	observed, err := observeNetwork(n, "qemu:///session")
	if err != nil || observed.LiveXML == observed.PersistentXML || observed.IsolationVerification != "not-run" || observed.Ownership != "external" {
		t.Fatal("state or verification claim fabricated", observed, err)
	}
	if len(n.requested) != 2 || n.requested[0] != native.NETWORK_XML_INACTIVE || n.requested[1] != 0 {
		t.Fatal("wrong network state flags", n.requested)
	}
	n = &networkFixture{failure: errors.New("synthetic unsupported inactive XML flag")}
	if _, err = observeNetwork(n, "qemu:///session"); !errors.Is(err, n.failure) || len(n.requested) != 1 {
		t.Fatal("unsupported persistent state silently replaced by live state", n, err)
	}
}

func TestInventoryResponseBound(t *testing.T) {
	used := wire.MaxFrame
	if inventoryBudget(domain.StoragePool{}, &used) == nil {
		t.Fatal("oversized aggregate inventory accepted")
	}
}
