//go:build linux && cgo

package libvirt

import (
	"context"
	"reflect"
	"strings"
	"testing"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
)

// volumePath resolves pool volumes the way a registered file pool does.
func (f *removalFixtureHandle) volumePath(pool, volume string) (string, error) {
	if pool != "images" {
		return "", domain.Fail("SOURCE_CHANGED", "unknown storage pool")
	}
	return "/var/lib/libvirt/images/" + volume, nil
}

const uefiRemovalID = "8f14e45f-ceea-467a-9e63-8c1a2b3c4d5e"
const uefiRemovalXML = `<domain type="kvm"><name>uefi-created</name><uuid>` + uefiRemovalID + `</uuid><memory unit="KiB">2097152</memory><vcpu>2</vcpu>` +
	`<os firmware="efi"><type arch="x86_64" machine="q35">hvm</type><loader readonly="yes" type="pflash" format="raw">/usr/share/edk2/ovmf/OVMF_CODE.fd</loader>` +
	`<nvram template="/usr/share/edk2/ovmf/OVMF_VARS.fd" format="raw">/var/lib/libvirt/qemu/nvram/uefi-created_VARS.fd</nvram><boot dev="hd"/></os>` +
	`<devices><emulator>/usr/bin/qemu-system-x86_64</emulator>` +
	`<disk type="volume" device="disk"><driver name="qemu" type="qcow2"/><source pool="images" volume="created-disk-000.qcow2"/><target dev="sda" bus="sata"/></disk>` +
	`<tpm model="tpm-crb"><backend type="emulator" version="2.0"/></tpm>` +
	`<interface type="network"><source network="default"/><model type="virtio"/></interface></devices></domain>`

func uefiRemovalFixture(t *testing.T, raw string) *removalFixtureHandle {
	t.Helper()
	f := newRemovalFixture()
	f.vm.Name, f.vm.Key.UUID = "uefi-created", uefiRemovalID
	f.vm.PersistentXML, f.secure = raw, raw
	f.vm.Fingerprint = fingerprint(f.vm)
	return f
}

// ADR 0063: the VMs Virmill creates use pool volumes, and UEFI VMs keep their
// firmware settings file and TPM state when the definition is removed.
func TestRemovalKeepsPoolVolumeDisksFirmwareAndTPMState(t *testing.T) {
	f := uefiRemovalFixture(t, uefiRemovalXML)
	inspected, err := inspectRemovalHandle(context.Background(), f, f.vm.Key)
	if err != nil {
		t.Fatal(err)
	}
	firmware := "/var/lib/libvirt/qemu/nvram/uefi-created_VARS.fd"
	want := []string{"/var/lib/libvirt/images/created-disk-000.qcow2", firmware}
	if !reflect.DeepEqual(inspected.RetainedSources, want) || inspected.Firmware != firmware || !inspected.EmulatedTPM {
		t.Fatalf("inspection: %#v", inspected)
	}
	if err := removeHeldDefinition(context.Background(), f, inspected); err != nil {
		t.Fatal(err)
	}
	if f.undefined != 1 || f.flags != native.DOMAIN_UNDEFINE_KEEP_NVRAM|native.DOMAIN_UNDEFINE_KEEP_TPM {
		t.Fatal("firmware settings or TPM state were not kept", f.flags)
	}
}

func TestRemovalRefusesUnknownFirmwareAndPassthroughTPM(t *testing.T) {
	for name, replacement := range map[string][2]string{
		"passthrough TPM": {`<backend type="emulator" version="2.0"/>`, `<backend type="passthrough"><device path="/dev/tpm0"/></backend>`},
		"second TPM":      {`<tpm model="tpm-crb">`, `<tpm model="tpm-tis"><backend type="emulator" version="2.0"/></tpm><tpm model="tpm-crb">`},
		"relative NVRAM":  {`>/var/lib/libvirt/qemu/nvram/uefi-created_VARS.fd<`, `>nvram/uefi-created_VARS.fd<`},
		"unknown pool":    {`pool="images"`, `pool="elsewhere"`},
	} {
		raw := strings.Replace(uefiRemovalXML, replacement[0], replacement[1], 1)
		if raw == uefiRemovalXML {
			t.Fatal(name, "fixture unchanged")
		}
		if _, err := inspectRemovalHandle(context.Background(), uefiRemovalFixture(t, raw), uefiRemovalFixture(t, raw).vm.Key); err == nil {
			t.Fatal(name, "accepted")
		}
	}
}
