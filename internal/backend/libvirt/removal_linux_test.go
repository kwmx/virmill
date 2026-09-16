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
	"virmill.local/core/internal/domain"
)

const removalFixtureID = "c2ba81ee-8116-42cd-bd76-b453e169cc99"
const removalFixtureXML = `<domain type="kvm"><name>retained</name><uuid>` + removalFixtureID + `</uuid><metadata><private:unknown xmlns:private="urn:example:opaque" private:value="unchanged"/></metadata><memory unit="KiB">1048576</memory><vcpu>2</vcpu><os><type arch="x86_64" machine="pc">hvm</type><boot dev="hd"/></os><devices><emulator>/usr/bin/qemu-system-x86_64</emulator><disk type="file" device="disk"><driver name="qemu" type="qcow2"/><source file="/var/lib/images/retained.qcow2"/><target dev="vda" bus="virtio"/><backingStore type="file"><format type="qcow2"/><source file="/var/lib/images/base.qcow2"/><backingStore/></backingStore></disk><disk type="file" device="cdrom"><source file="/home/user/install.iso"/><target dev="sda" bus="sata"/><readonly/></disk><interface type="network"><source network="default"/><model type="virtio"/></interface><graphics type="vnc"><listen type="socket"/></graphics></devices></domain>`

type removalFixtureHandle struct {
	vm                                                                                    domain.VM
	isActive, isTransient                                                                 bool
	snapshots, checkpoints                                                                int
	readErr, activeErr, persistentErr, snapshotErr, checkpointErr, secureErr, undefineErr error
	secure                                                                                string
	observations, undefined                                                               int
	flags                                                                                 native.DomainUndefineFlagsValues
	changeAt                                                                              int
}

func newRemovalFixture() *removalFixtureHandle {
	vm := domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: removalFixtureID}, Name: "retained", State: "stopped", PersistentXML: removalFixtureXML, Ownership: "external", Tags: []string{}}
	vm.Fingerprint = fingerprint(vm)
	return &removalFixtureHandle{vm: vm, secure: removalFixtureXML}
}
func (f *removalFixtureHandle) observation(string) (domain.VM, error) {
	f.observations++
	if f.observations == f.changeAt {
		f.vm.Autostart = true
		f.vm.Fingerprint = fingerprint(f.vm)
	}
	return f.vm, f.readErr
}
func (f *removalFixtureHandle) active() (bool, error)         { return f.isActive, f.activeErr }
func (f *removalFixtureHandle) persistent() (bool, error)     { return !f.isTransient, f.persistentErr }
func (f *removalFixtureHandle) snapshotCount() (int, error)   { return f.snapshots, f.snapshotErr }
func (f *removalFixtureHandle) checkpointCount() (int, error) { return f.checkpoints, f.checkpointErr }
func (f *removalFixtureHandle) secureXML() (string, error)    { return f.secure, f.secureErr }
func (f *removalFixtureHandle) undefine(flags native.DomainUndefineFlagsValues) error {
	f.undefined++
	f.flags = flags
	return f.undefineErr
}

func TestDefinitionRemovalRetainsDeclaredSourcesAndOpaqueMetadata(t *testing.T) {
	f := newRemovalFixture()
	before := f.vm
	inspected, err := inspectRemovalHandle(context.Background(), f, f.vm.Key)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/home/user/install.iso", "/var/lib/images/base.qcow2", "/var/lib/images/retained.qcow2"}
	if !reflect.DeepEqual(inspected.RetainedSources, want) || inspected.Resource != f.vm.Key || inspected.Name != f.vm.Name || inspected.Fingerprint != f.vm.Fingerprint || len(inspected.DefinitionSHA256) != 64 {
		t.Fatalf("inspection: %#v", inspected)
	}
	if err := removeHeldDefinition(context.Background(), f, inspected); err != nil {
		t.Fatal(err)
	}
	if f.undefined != 1 || f.flags != 0 || !reflect.DeepEqual(f.vm, before) {
		t.Fatalf("remove invoked unexpected effects: %#v", f)
	}
	// Even inert extension changes remain bound to review, rather than being reconstructed away.
	f = newRemovalFixture()
	f.vm.PersistentXML = strings.Replace(f.vm.PersistentXML, "unchanged", "different", 1)
	f.secure = f.vm.PersistentXML
	f.vm.Fingerprint = fingerprint(f.vm)
	if err := removeHeldDefinition(context.Background(), f, inspected); err == nil || f.undefined != 0 {
		t.Fatal("changed opaque metadata was accepted")
	}
}
func TestDefinitionRemovalRejectsUnsafeObservationsAndUncertainInspection(t *testing.T) {
	boom := errors.New("inspection unavailable")
	tests := map[string]func(*removalFixtureHandle){
		"running": func(f *removalFixtureHandle) { f.vm.State = "running" }, "unknown": func(f *removalFixtureHandle) { f.vm.State = "unknown" }, "paused": func(f *removalFixtureHandle) { f.vm.State = "paused" }, "autostart": func(f *removalFixtureHandle) { f.vm.Autostart = true }, "save": func(f *removalFixtureHandle) { f.vm.HasManagedSave = true }, "live XML": func(f *removalFixtureHandle) { f.vm.LiveXML = "<domain/>" }, "transient": func(f *removalFixtureHandle) { f.isTransient = true }, "active disagreement": func(f *removalFixtureHandle) { f.isActive = true }, "snapshot": func(f *removalFixtureHandle) { f.snapshots = 1 }, "checkpoint": func(f *removalFixtureHandle) { f.checkpoints = 1 }, "negative snapshot": func(f *removalFixtureHandle) { f.snapshots = -1 }, "identity": func(f *removalFixtureHandle) { f.vm.Key.UUID = "12121212-1212-1212-1212-121212121212" }, "read": func(f *removalFixtureHandle) { f.readErr = boom }, "active read": func(f *removalFixtureHandle) { f.activeErr = boom }, "persistent read": func(f *removalFixtureHandle) { f.persistentErr = boom }, "snapshots unsupported": func(f *removalFixtureHandle) { f.snapshotErr = boom }, "checkpoints unsupported": func(f *removalFixtureHandle) { f.checkpointErr = boom }, "secure unavailable": func(f *removalFixtureHandle) { f.secureErr = boom }, "protected XML": func(f *removalFixtureHandle) { f.secure += "<!-- private -->" }, "concurrent change": func(f *removalFixtureHandle) { f.changeAt = 2 },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			f := newRemovalFixture()
			key := f.vm.Key
			change(f)
			if _, err := inspectRemovalHandle(context.Background(), f, key); err == nil {
				t.Fatal("unsafe observation accepted")
			}
			if f.undefined != 0 {
				t.Fatal("inspection mutated domain")
			}
		})
	}
}
func TestDefinitionRemovalRevalidatesAllBindingsAndDoesNotReplayFailure(t *testing.T) {
	for _, field := range []string{"fingerprint", "digest", "sources", "name"} {
		t.Run(field, func(t *testing.T) {
			f := newRemovalFixture()
			p, err := inspectRemovalHandle(context.Background(), f, f.vm.Key)
			if err != nil {
				t.Fatal(err)
			}
			switch field {
			case "fingerprint":
				p.Fingerprint = "bad"
			case "digest":
				p.DefinitionSHA256 = "bad"
			case "sources":
				p.RetainedSources = nil
			case "name":
				p.Name = "another"
			}
			if err := removeHeldDefinition(context.Background(), f, p); err == nil || f.undefined != 0 {
				t.Fatal("changed binding accepted")
			}
		})
	}
	f := newRemovalFixture()
	p, err := inspectRemovalHandle(context.Background(), f, f.vm.Key)
	if err != nil {
		t.Fatal(err)
	}
	lost := errors.New("lost acknowledgement")
	f.undefineErr = lost
	if err := removeHeldDefinition(context.Background(), f, p); !errors.Is(err, lost) || f.undefined != 1 {
		t.Fatal("failure swallowed or replayed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := removeHeldDefinition(ctx, f, p); !errors.Is(err, context.Canceled) || f.undefined != 1 {
		t.Fatal("canceled removal invoked effect")
	}
}
func TestDefinitionRemovalXMLRefusesAuxiliaryAndUnknownStorageProfiles(t *testing.T) {
	cases := map[string]string{
		// ADR 0063 supports UEFI with its loader and NVRAM, pool-volume disks and
		// an emulated TPM; those cases are covered in removal_uefi_linux_test.go.
		"unknown firmware":    strings.Replace(removalFixtureXML, "<os>", `<os firmware="bogus">`, 1),
		"varstore":            strings.Replace(removalFixtureXML, "</os>", `<varstore path="/firmware/vars.json"/></os>`, 1),
		"device nvram":        strings.Replace(removalFixtureXML, "</devices>", `<nvram>/state/vars.fd</nvram></devices>`, 1),
		"volume without pool": strings.Replace(removalFixtureXML, `<source file="/var/lib/images/retained.qcow2"/>`, `<source volume="retained.qcow2"/>`, 1),
		"pstore":              strings.Replace(removalFixtureXML, "</devices>", `<pstore backend="acpi-erst"><path>/state/erst</path></pstore></devices>`, 1),
		"unknown device":      strings.Replace(removalFixtureXML, "</devices>", `<future-state path="/state/unknown"/></devices>`, 1),
		"runtime extension":   strings.Replace(removalFixtureXML, "</domain>", `<q:commandline xmlns:q="http://libvirt.org/schemas/domain/qemu/1.0"/></domain>`, 1),
		"foreign loader":      strings.Replace(removalFixtureXML, "</os>", `<x:loader xmlns:x="urn:future"/></os>`, 1),
		"noncanonical path":   strings.Replace(removalFixtureXML, "/var/lib/images/retained.qcow2", "/var/lib/images/../retained.qcow2", 1),
		"credential storage":  strings.Replace(removalFixtureXML, `<source file="/var/lib/images/retained.qcow2"/>`, `<source file="/var/lib/images/retained.qcow2"/><auth username="secret"/>`, 1),
		"network storage":     strings.Replace(removalFixtureXML, `<disk type="file" device="disk">`, `<disk type="network" device="disk">`, 1),
		"structured identity": strings.Replace(removalFixtureXML, "<name>retained</name>", "<name><value>retained</value></name>", 1),
		"duplicate uuid":      strings.Replace(removalFixtureXML, "</uuid>", "</uuid><uuid>"+removalFixtureID+"</uuid>", 1),
		"duplicate attr":      strings.Replace(removalFixtureXML, `type="kvm"`, `type="kvm" type="qemu"`, 1),
		"malformed":           "<domain>", "directive": "<!DOCTYPE domain>" + removalFixtureXML, "oversize": strings.Repeat(" ", coldStateXMLLimit+1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := removalSources(raw, removalFixtureID, "retained", newRemovalFixture()); err == nil {
				t.Fatal("unsafe XML accepted")
			}
		})
	}
}
func TestDefinitionRemovalAbsenceIsOnlyExactNativeNoDomain(t *testing.T) {
	nativeMissing := native.Error{Code: native.ERR_NO_DOMAIN, Message: "domain missing"}
	for _, err := range []error{nativeMissing, fmt.Errorf("lookup: %w", nativeMissing)} {
		absent, e := removalLookupAbsent(err)
		if !absent || e != nil {
			t.Fatalf("typed absence lost: %v", e)
		}
	}
	for _, err := range []error{errors.New("domain not found"), native.Error{Code: native.ERR_NO_CONNECT}, native.Error{Code: native.ERR_OPERATION_DENIED}} {
		absent, e := removalLookupAbsent(err)
		if absent || !errors.Is(e, err) {
			t.Fatalf("failure became absence: %v", err)
		}
	}
	for _, id := range []string{"guest-name", "00000000-0000-0000-0000-000000000000", strings.ToUpper(removalFixtureID), removalFixtureID + " "} {
		key := newRemovalFixture().vm.Key
		key.UUID = id
		if err := removalKey(key); err == nil {
			t.Fatalf("invalid id accepted %q", id)
		}
	}
	key := newRemovalFixture().vm.Key
	key.ConnectionID = "qemu+ssh://elsewhere/system"
	if err := removalKey(key); err == nil {
		t.Fatal("remote connection accepted")
	}
}
