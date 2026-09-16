package xmlpatch

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// TestStoredDefinitionIsTheReviewedInsert checks the readback property against
// definitions a host really produced: removing the added disk from what libvirt
// stored must give back the definition from before the insert, and nothing
// else may differ. A digest of the whole definition cannot confirm an insert,
// because libvirt files a new disk among the other disks while HardwareDigest
// treats child ordering as significant; that is what refused the first five
// native additions. Point VIRMILL_BEFORE_DEFINITION and
// VIRMILL_STORED_DEFINITION at the pair to run this; it is skipped otherwise,
// so it never runs in an ordinary suite and no host's XML is committed.
func TestStoredDefinitionIsTheReviewedInsert(t *testing.T) {
	read := func(name string) string {
		path := os.Getenv(name)
		if path == "" {
			t.Skip("set VIRMILL_BEFORE_DEFINITION and VIRMILL_STORED_DEFINITION to a definition pair to compare")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	before, stored := read("VIRMILL_BEFORE_DEFINITION"), read("VIRMILL_STORED_DEFINITION")
	had, err := DiskTargets(before)
	if err != nil {
		t.Fatal(err)
	}
	holds, err := DiskTargets(stored)
	if err != nil {
		t.Fatal(err)
	}
	var gained []string
	for _, target := range holds {
		if !slices.Contains(had, target) {
			gained = append(gained, target)
		}
	}
	if len(gained) != 1 || len(holds) != len(had)+1 {
		t.Fatal("the pair does not differ by exactly one disk: had", strings.Join(had, ","), "holds", strings.Join(holds, ","))
	}
	added := gained[0]
	without, err := WithoutDisk(stored, added)
	if err != nil {
		t.Fatal(err)
	}
	wanted, err := HardwareDigest(before)
	if err != nil {
		t.Fatal(err)
	}
	got, err := HardwareDigest(without)
	if err != nil {
		t.Fatal(err)
	}
	if wanted != got {
		reportDefinitionDifference(t, before, without)
		t.Fatal("removing " + added + " does not give back the definition from before the insert")
	}
	// Record what the replaced check would have said about this same pair.
	add, err := InspectDiskAddition(before, "")
	if err != nil {
		t.Fatal(err)
	}
	source, err := onlyChild(diskAt(t, stored, added), "source")
	if err != nil {
		t.Fatal(err)
	}
	add.Pool, _ = source.attr("pool")
	add.Volume, _ = source.attr("volume")
	if add.Target != added {
		t.Fatal("the next free target is " + add.Target + ", not the added " + added)
	}
	after, err := AddDisk(before, add)
	if err != nil {
		t.Fatal(err)
	}
	reviewed, err := HardwareDigest(after)
	if err != nil {
		t.Fatal(err)
	}
	whole, err := HardwareDigest(stored)
	if err != nil {
		t.Fatal(err)
	}
	if reviewed == whole {
		t.Log("on this pair libvirt happened to file the disk where the insert put it, so a whole-definition digest would have matched too")
		return
	}
	t.Log("a whole-definition digest would have refused this correct addition; libvirt filed " + added + " elsewhere among the devices")
	reportDefinitionDifference(t, after, stored)
}

func diskAt(t *testing.T, data, target string) *positionedNode {
	t.Helper()
	_, disks, err := diskElements(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, disk := range disks {
		if disk.target == target {
			return disk.node
		}
	}
	t.Fatal("no disk " + target)
	return nil
}

// reportDefinitionDifference narrows a mismatch to one element, and to one
// device when the difference is inside <devices>.
func reportDefinitionDifference(t *testing.T, left, right string) {
	t.Helper()
	a, err := positionedXML(left)
	if err != nil {
		t.Fatal(err)
	}
	b, err := positionedXML(right)
	if err != nil {
		t.Fatal(err)
	}
	digest := func(n *positionedNode, raw string) string { return Digest(raw[n.start:n.end]) }
	for _, name := range []string{"devices", "os", "features", "cpu", "clock", "metadata", "vcpu", "memory", "currentMemory"} {
		first, firstErr := onlyChild(a, name)
		second, secondErr := onlyChild(b, name)
		if firstErr != nil || secondErr != nil {
			continue
		}
		if digest(first, left) == digest(second, right) {
			continue
		}
		t.Log("differs in", name)
		if name != "devices" {
			continue
		}
		for i, part := range first.parts {
			if part.child == nil || i >= len(second.parts) || second.parts[i].child == nil {
				continue
			}
			if digest(part.child, left) != digest(second.parts[i].child, right) {
				t.Log("  device", i, "left: ", part.child.name.Local)
				t.Log("  device", i, "right:", second.parts[i].child.name.Local)
			}
		}
	}
}
