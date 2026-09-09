package importer

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestOVFResourceMetadataPreservesUnitsAndNormalizesKnownSizes(t *testing.T) {
	xml := `<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1" xmlns:rasd="http://schemas.dmtf.org/wbem/wscim/1/cim-schema/2/CIM_ResourceAllocationSettingData"><References><File ovf:id="file" ovf:href="boot.vmdk"/></References><DiskSection><Disk ovf:diskId="disk" ovf:fileRef="file" ovf:capacity="60" ovf:capacityAllocationUnits="byte * 2^30" ovf:format="vmdk"/></DiskSection><VirtualSystem ovf:id="guest"><VirtualHardwareSection><Item><rasd:ResourceType>3</rasd:ResourceType><rasd:VirtualQuantity>4</rasd:VirtualQuantity><rasd:AllocationUnits>hertz * 10^6</rasd:AllocationUnits></Item><Item><rasd:ResourceType>4</rasd:ResourceType><rasd:VirtualQuantity>8192</rasd:VirtualQuantity><rasd:AllocationUnits>byte * 2^20</rasd:AllocationUnits></Item><Item><rasd:ResourceType>17</rasd:ResourceType><rasd:HostResource>ovf:/disk/disk</rasd:HostResource></Item></VirtualHardwareSection></VirtualSystem></Envelope>`
	var report Report
	if err := parseOVF([]byte(xml), "guest.ovf", &report); err != nil {
		t.Fatal(err)
	}
	disk := report.Disks[0]
	if disk.Capacity != "60" || disk.CapacityAllocationUnits != "byte * 2^30" || disk.CapacityBytes != 60<<30 {
		t.Fatalf("disk sizing metadata lost: %+v", disk)
	}
	cpu, memory := report.Systems[0].Items[0], report.Systems[0].Items[1]
	if cpu.Quantity != "4" || cpu.AllocationUnits != "hertz * 10^6" || cpu.MemoryMiB != 0 {
		t.Fatalf("CPU quantity/units changed: %+v", cpu)
	}
	if memory.Quantity != "8192" || memory.AllocationUnits != "byte * 2^20" || memory.MemoryMiB != 8192 {
		t.Fatalf("memory units ignored: %+v", memory)
	}
	if len(report.Systems[0].DiskIDs) != 1 || report.Systems[0].DiskIDs[0] != "disk" {
		t.Fatal("normalization changed disk mapping")
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Disks[0] != disk || decoded.Systems[0].Items[1].MemoryMiB != 8192 {
		t.Fatal("normalized metadata did not survive JSON roundtrip")
	}
}

func TestAllocationBytesKnownUnitsAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		quantity, units string
		want            int64
	}{
		{"8192", "byte * 2^20", 8192 << 20}, {"60", "byte*2^30", 60 << 30}, {"4096", "bytes", 4096},
		{"1", "BYTE * 2^20", 1 << 20}, {"4", "byte * 10^9", 4_000_000_000}, {"9223372036854775807", "byte", math.MaxInt64},
	} {
		got, ok := allocationBytes(tc.quantity, tc.units)
		if !ok || got != tc.want {
			t.Errorf("allocationBytes(%q,%q)=%d,%v; want %d", tc.quantity, tc.units, got, ok, tc.want)
		}
	}
	for _, tc := range []struct{ quantity, units string }{
		{"8192", ""}, {"8192", "MB"}, {"4", "hertz * 10^6"}, {"1.5", "byte * 2^30"}, {"-1", "byte"}, {"+1", "byte"}, {"0", "byte"},
		{"9223372036854775808", "byte"}, {"2", "byte * 2^62"}, {"1", "byte * 2^63"}, {"1", "byte * 10^19"}, {"1", "byte * 2^999999999"},
		{"1", "byte * 2^-1"}, {"1", "byte * 3^20"}, {"1", "byte * 2^20 extra"},
	} {
		if got, ok := allocationBytes(tc.quantity, tc.units); ok || got != 0 {
			t.Errorf("unsupported/overflow allocation guessed: %+v => %d", tc, got)
		}
	}
}

func TestResourceMetadataOmittedForOldOrUnknownDescriptors(t *testing.T) {
	for _, units := range []string{"", "widgets", "byte"} {
		xml := `<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1"><VirtualSystem ovf:id="guest"><VirtualHardwareSection><Item><ResourceType>4</ResourceType><VirtualQuantity>3</VirtualQuantity><AllocationUnits>` + units + `</AllocationUnits></Item></VirtualHardwareSection></VirtualSystem></Envelope>`
		var report Report
		if err := parseOVF([]byte(xml), "guest.ovf", &report); err != nil {
			t.Fatal(err)
		}
		item := report.Systems[0].Items[0]
		if item.MemoryMiB != 0 || item.AllocationUnits != units || item.Quantity != "3" {
			t.Fatalf("unknown/fractional MiB memory was guessed: %+v", item)
		}
	}
	var old Report
	if err := json.Unmarshal([]byte(`{"disks":[{"id":"disk","fileRef":"file","path":"boot.vmdk","capacity":"60","format":"vmdk"}],"systems":[{"id":"guest","hardware":[{"resourceType":"4","quantity":"8192"}]}]}`), &old); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"capacityBytes", "capacityAllocationUnits", "allocationUnits", "memoryMiB"} {
		if strings.Contains(string(data), `"`+field+`"`) {
			t.Fatalf("old report gained fabricated %s metadata: %s", field, data)
		}
	}
}

func TestOVFDefaultDiskBytesAndVirtualBoxLegacyMemoryUnits(t *testing.T) {
	for _, tc := range []struct {
		name, systemType, diskUnits string
		wantMemory, wantCapacity    int64
	}{
		{"virtualbox-legacy", "virtualbox-2.2", "", 8192, 128849018880},
		{"unknown-producer", "other-1", "", 0, 128849018880},
		{"missing-producer", "", "", 0, 128849018880},
		{"unknown-disk-unit", "virtualbox-2.2", ` ovf:capacityAllocationUnits="widgets"`, 8192, 0},
		{"explicit-empty-unit", "virtualbox-2.2", ` ovf:capacityAllocationUnits=""`, 8192, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xml := `<Envelope xmlns="http://schemas.dmtf.org/ovf/envelope/1" xmlns:ovf="http://schemas.dmtf.org/ovf/envelope/1"><References><File ovf:id="file" ovf:href="boot.vmdk"/></References><DiskSection><Disk ovf:diskId="disk" ovf:fileRef="file" ovf:capacity="128849018880"` + tc.diskUnits + `/></DiskSection><VirtualSystem ovf:id="guest"><VirtualHardwareSection><System><VirtualSystemType>` + tc.systemType + `</VirtualSystemType></System><Item><ResourceType>3</ResourceType><VirtualQuantity>2</VirtualQuantity></Item><Item><ResourceType>4</ResourceType><VirtualQuantity>8192</VirtualQuantity><AllocationUnits>MegaBytes</AllocationUnits></Item><Item><ResourceType>17</ResourceType><HostResource>ovf:/disk/disk</HostResource></Item></VirtualHardwareSection></VirtualSystem></Envelope>`
			var report Report
			if err := parseOVF([]byte(xml), "guest.ovf", &report); err != nil {
				t.Fatal(err)
			}
			if report.Disks[0].CapacityBytes != tc.wantCapacity || report.Systems[0].Items[1].MemoryMiB != tc.wantMemory {
				t.Fatalf("incorrect default/legacy normalization: %+v %+v", report.Disks[0], report.Systems[0].Items[1])
			}
			if report.Disks[0].Capacity != "128849018880" || report.Systems[0].Items[1].AllocationUnits != "MegaBytes" {
				t.Fatal("raw descriptor metadata changed")
			}
		})
	}
}
