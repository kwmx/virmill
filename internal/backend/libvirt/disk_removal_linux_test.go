//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func TestRemovalDiskSourcesRefuseMediaSharedAndParents(t *testing.T) {
	for _, targets := range [][]string{{"vda"}, {"vda", "sda"}, {"missing"}, {"vda", "vda"}, {"/disk"}, nil} {
		got, e := removalDiskSources(removalFixtureXML, targets)
		if reflect.DeepEqual(targets, []string{"vda"}) {
			if e != nil || len(got) != 1 || got[0].path != "/var/lib/images/retained.qcow2" {
				t.Fatal(got, e)
			}
		} else if e == nil {
			t.Fatal("unsafe selection accepted", targets)
		}
	}
	for _, addition := range []string{"<readonly/>", "<shareable/>", "<encryption format='luks'/>"} {
		raw := strings.Replace(removalFixtureXML, "<target dev=\"vda\" bus=\"virtio\"/>", "<target dev=\"vda\" bus=\"virtio\"/>"+addition, 1)
		if _, e := removalDiskSources(raw, []string{"vda"}); e == nil {
			t.Fatal("unsafe source accepted", addition)
		}
	}
}
func TestRemovalVolumeFormatRejectsHiddenStorageProfiles(t *testing.T) {
	base := `<volume type="file"><name>disk.qcow2</name><key>/pool/disk.qcow2</key><capacity>8388608</capacity><target><path>/pool/disk.qcow2</path><format type="qcow2"/></target></volume>`
	if format, e := removalVolumeFormat(base, "disk.qcow2", "/pool/disk.qcow2"); e != nil || format != "qcow2" {
		t.Fatal(format, e)
	}
	observed := strings.Replace(base, "</target>", "<clusterSize unit='B'>65536</clusterSize></target>", 1)
	if format, e := removalVolumeFormat(observed, "disk.qcow2", "/pool/disk.qcow2"); e != nil || format != "qcow2" {
		t.Fatal(format, e)
	}
	for _, value := range []string{"<clusterSize unit='B'>0</clusterSize>", "<clusterSize unit='invalid'>65536</clusterSize>", "<clusterSize unit='B'>-1</clusterSize>", "<clusterSize unit='B'><unknown/></clusterSize>"} {
		if _, e := removalVolumeFormat(strings.Replace(base, "</target>", value+"</target>", 1), "disk.qcow2", "/pool/disk.qcow2"); e == nil {
			t.Fatal("invalid cluster size accepted", value)
		}
	}
	for _, raw := range []string{strings.Replace(base, "type=\"file\"", "type=\"block\"", 1), strings.Replace(base, "qcow2\"/>", "vmdk\"/>", 1), strings.Replace(base, "</target>", "<encryption/></target>", 1), strings.Replace(base, "</volume>", "<unrecognized/></volume>", 1), strings.Replace(base, "<name>", "<name attr='x'>", 1)} {
		if _, e := removalVolumeFormat(raw, "disk.qcow2", "/pool/disk.qcow2"); e == nil {
			t.Fatal("unsupported volume accepted", raw)
		}
	}
}

type diskRemovalSessionFixture struct {
	r             domain.DiskRemoval
	state         string
	graphErr      error
	definitionErr error
	diskErr       error
	mutate        bool
	checks        int
}

func (f *diskRemovalSessionFixture) definition(context.Context, domain.DefinitionRemoval, bool) error {
	return f.definitionErr
}
func (f *diskRemovalSessionFixture) disk(domain.RemovalDisk) (string, error) {
	f.checks++
	if f.mutate && f.checks > 1 {
		return "absent", nil
	}
	return f.state, f.diskErr
}
func (f *diskRemovalSessionFixture) graph(context.Context, domain.DiskRemoval) (string, []string, error) {
	return f.r.GraphDigest, f.r.ResourceIDs, f.graphErr
}
func diskRemovalObservation() domain.DiskRemoval {
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///session", Kind: "vm", UUID: removalFixtureID}
	d := domain.RemovalDisk{Target: "vda", Path: "/pool/disk.qcow2", PoolID: "11111111-2222-4333-8444-555555555555", VolumeName: "disk.qcow2", VolumeKey: "/pool/disk.qcow2", Generation: "linux-statx-v1:1:2:3:4:000000001", Fingerprint: strings.Repeat("d", 64), Format: "qcow2", CapacityBytes: 8 << 20}
	r := domain.DiskRemoval{Definition: domain.DefinitionRemoval{Resource: key, Name: "fixture", Fingerprint: strings.Repeat("a", 64), DefinitionSHA256: strings.Repeat("b", 64), RetainedSources: []string{d.Path}}, Disks: []domain.RemovalDisk{d}, GraphDigest: strings.Repeat("c", 64), ResourceIDs: append([]string{key.String()}, diskRemovalResources(key.ConnectionID, d)...)}
	sort.Strings(r.ResourceIDs)
	return r
}
func TestRemovalDiskStateRequiresExactGraphAndGeneration(t *testing.T) {
	for _, fault := range []string{"good", "absent-before", "unknown", "graph", "lockset", "definition", "disk-generation", "changed-during-graph", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			r := diskRemovalObservation()
			f := &diskRemovalSessionFixture{r: r, state: "present"}
			ctx := context.Background()
			switch fault {
			case "absent-before":
				f.state = "absent"
			case "unknown":
				f.state = "unknown"
			case "graph":
				f.graphErr = errors.New("shared backing reference")
			case "lockset":
				f.r.ResourceIDs = slices.Clone(f.r.ResourceIDs)
				f.r.ResourceIDs = append(f.r.ResourceIDs, "other")
			case "definition":
				f.definitionErr = errors.New("replacement VM")
			case "disk-generation":
				f.diskErr = errors.New("replaced file")
			case "changed-during-graph":
				f.mutate = true
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			states, e := checkRemovalDisks(ctx, f, r, false)
			if fault == "good" {
				if e != nil || !reflect.DeepEqual(states, []string{"present"}) {
					t.Fatal(states, e)
				}
			} else if e == nil {
				t.Fatal("unsafe state accepted", fault)
			}
		})
	}
	r := diskRemovalObservation()
	f := &diskRemovalSessionFixture{r: r, state: "absent"}
	if got, e := checkRemovalDisks(context.Background(), f, r, true); e != nil || !reflect.DeepEqual(got, []string{"absent"}) {
		t.Fatal(got, e)
	}
}
func TestRemovalOwnerCannotHideSavedOrSnapshotState(t *testing.T) {
	if e := cleanupGraphOwnerState(false, 0, 0); e != nil {
		t.Fatal(e)
	}
	for _, counts := range [][3]int{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}, {0, -1, 0}} {
		if e := cleanupGraphOwnerState(counts[0] != 0, counts[1], counts[2]); e == nil {
			t.Fatal("uncertain owner state skipped")
		}
	}
}
