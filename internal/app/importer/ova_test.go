package importer

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

const descriptor = `<?xml version="1.0"?><Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1" xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData"><References><File ovf:id="f1" ovf:href="boot.vmdk"/><File ovf:id="f2" ovf:href="data.vmdk"/></References><DiskSection><Disk ovf:diskId="boot" ovf:fileRef="f1"/><Disk ovf:diskId="data" ovf:fileRef="f2"/></DiskSection><VirtualSystemCollection><VirtualSystem ovf:id="one"><Name>one</Name><VirtualHardwareSection><Item><rasd:ResourceType>17</rasd:ResourceType><rasd:Parent>controller0</rasd:Parent><rasd:AddressOnParent>0</rasd:AddressOnParent><rasd:HostResource>ovf:/disk/boot</rasd:HostResource></Item><Item><rasd:ResourceType>17</rasd:ResourceType><rasd:Parent>controller0</rasd:Parent><rasd:AddressOnParent>1</rasd:AddressOnParent><rasd:HostResource>ovf:/disk/data</rasd:HostResource></Item><Item><rasd:ResourceType>10</rasd:ResourceType><rasd:Connection>internet</rasd:Connection></Item><Item><rasd:ResourceType>10</rasd:ResourceType><rasd:Connection>lab</rasd:Connection></Item></VirtualHardwareSection></VirtualSystem><VirtualSystem ovf:id="two"><Name>two</Name></VirtualSystem></VirtualSystemCollection></Envelope>`

func fixture(t *testing.T, desc string, extra *tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, f := range []struct{ name, data string }{{"appliance.ovf", desc}, {"boot.vmdk", "synthetic-boot"}, {"data.vmdk", "synthetic-data"}} {
		if e := w.WriteHeader(&tar.Header{Name: f.name, Mode: 0600, Size: int64(len(f.data)), Typeflag: tar.TypeReg}); e != nil {
			t.Fatal(e)
		}
		w.Write([]byte(f.data))
	}
	if extra != nil {
		if e := w.WriteHeader(extra); e != nil {
			t.Fatal(e)
		}
	}
	w.Close()
	return b.Bytes()
}
func TestMultiDiskMultiSystemInspection(t *testing.T) {
	b := fixture(t, descriptor, nil)
	r, e := InspectTar(context.Background(), bytes.NewReader(b), DefaultLimits())
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Disks) != 2 || len(r.Systems) != 2 || len(r.Systems[0].DiskIDs) != 2 || r.Systems[0].Items[1].AddressOnParent != "1" {
		t.Fatalf("lost disk/system/controller: %+v", r)
	}
	sum := sha256.Sum256(b)
	if r.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("source digest incomplete")
	}
	if r.Readiness != "inspection-only" {
		t.Fatal("false boot readiness")
	}
}
func TestArchiveAttacks(t *testing.T) {
	for _, h := range []*tar.Header{{Name: "../escape", Typeflag: tar.TypeReg}, {Name: "/absolute", Typeflag: tar.TypeReg}, {Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}, {Name: "hard", Typeflag: tar.TypeLink, Linkname: "boot.vmdk"}, {Name: "special", Typeflag: tar.TypeChar}, {Name: "BOOT.VMDK", Typeflag: tar.TypeReg}} {
		b := fixture(t, descriptor, h)
		if _, e := InspectTar(context.Background(), bytes.NewReader(b), DefaultLimits()); e == nil {
			t.Fatal("accepted attack", h.Name)
		}
	}
	for _, desc := range []string{"<!DOCTYPE x [<!ENTITY e SYSTEM \"file:///etc/passwd\">]>" + descriptor, strings.Replace(descriptor, "boot.vmdk", "../../host", 1), strings.Replace(descriptor, "ovf:/disk/boot", "file:///etc/passwd", 1)} {
		if _, e := InspectTar(context.Background(), bytes.NewReader(fixture(t, desc, nil)), DefaultLimits()); e == nil {
			t.Fatal("accepted malicious descriptor")
		}
	}
	if _, e := InspectTar(context.Background(), bytes.NewReader(fixture(t, descriptor, nil)), Limits{Bytes: 128, Members: 10}); e == nil {
		t.Fatal("budget ignored")
	}
}
func FuzzArchive(f *testing.F) {
	f.Add([]byte("invalid"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			return
		}
		_, _ = InspectTar(context.Background(), bytes.NewReader(b), Limits{Bytes: 1 << 20, Members: 100})
	})
}
