//go:build linux && amd64

package importing

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
)

const streamFormat = "http://www.vmware.com/interfaces/specifications/vmdk.html#streamOptimized"

// streamDescriptor is the phase fixture's descriptor with both disks declared
// streamOptimized, so both may be read in place (ADR 0060).
func streamDescriptor() string {
	return strings.NewReplacer(`ovf:fileRef="f1"`, `ovf:fileRef="f1" ovf:format="`+streamFormat+`"`, `ovf:fileRef="f2"`, `ovf:fileRef="f2" ovf:format="`+streamFormat+`"`).Replace(fixtureOVF)
}

// streamArchive packs that descriptor with synthetic disks, or with real
// streamOptimized VMDKs from qemu-img.
func streamArchive(t *testing.T, real bool) string {
	t.Helper()
	members := [][2]string{{"appliance.ovf", streamDescriptor()}}
	for _, name := range []string{"boot", "data"} {
		content := "synthetic phase fixture " + name
		if real {
			raw := filepath.Join(t.TempDir(), name+".raw")
			f, err := os.OpenFile(raw, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			f.Truncate(8 << 20)
			f.WriteAt([]byte("Virmill in-place "+name), 2<<20)
			f.Close()
			vmdk := filepath.Join(t.TempDir(), name+".vmdk")
			if b, err := exec.Command("/usr/bin/qemu-img", "convert", "-f", "raw", "-O", "vmdk", "-o", "subformat=streamOptimized", raw, vmdk).CombinedOutput(); err != nil {
				t.Fatal(err, string(b))
			}
			b, err := os.ReadFile(vmdk)
			if err != nil {
				t.Fatal(err)
			}
			content = string(b)
		}
		members = append(members, [2]string{name + ".vmdk", content})
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, m := range members {
		tw.WriteHeader(&tar.Header{Name: m[0], Mode: 0600, Size: int64(len(m[1])), Typeflag: tar.TypeReg})
		tw.Write([]byte(m[1]))
	}
	tw.Close()
	archive := filepath.Join(t.TempDir(), "appliance.ova")
	if err := os.WriteFile(archive, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return archive
}

// archiveTool reads disks in place and records the ranges it was given.
type archiveTool struct {
	phaseTool
	refuse  string // the disk whose in-place inspection is refused
	mu      sync.Mutex
	windows map[string]image.ArchiveWindow
}

func (a *archiveTool) InspectArchive(ctx context.Context, archive *os.File, work, format string, w image.ArchiveWindow, bound int64) ([]image.Info, error) {
	b := make([]byte, w.Size)
	if _, err := archive.ReadAt(b, w.Offset); err != nil && err != io.EOF {
		return nil, err
	}
	if a.refuse != "" && strings.HasSuffix(string(b), a.refuse) {
		return nil, errors.New("fixture refuses in-place reading of " + a.refuse)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.windows == nil {
		a.windows = map[string]image.ArchiveWindow{}
	}
	a.windows[string(b)] = w
	return []image.Info{{Filename: "json:{}", Format: format, VirtualSize: 8 << 20}}, nil
}
func (a *archiveTool) MeasureArchive(context.Context, *os.File, string, string, image.ArchiveWindow) (int64, error) {
	return 2 << 20, nil
}
func (a *archiveTool) ConvertArchive(ctx context.Context, archive *os.File, work, format string, w image.ArchiveWindow, size, bound int64) error {
	if bound != image.OutputBudget(2<<20, 8<<20) {
		return errors.New("in-place conversion did not get its measured output limit")
	}
	return os.WriteFile(filepath.Join(work, "disk.qcow2"), []byte("synthetic in-place output"), 0600)
}

func stageReview(t *testing.T, s *Service, p domain.Plan) map[string]any {
	t.Helper()
	_, input, err := s.Store.Plan(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.Review(context.Background(), p, input)
	if err != nil {
		t.Fatal(err)
	}
	return review
}

func unpackedMembers(t *testing.T, destination string, p domain.Plan) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(filepath.Dir(destination), stageName(p), "source"))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestOVADisksAreConvertedInPlaceAndOnlySmallMembersUnpacked(t *testing.T) {
	tool := &archiveTool{}
	s := serviceFixture(t, tool)
	source := streamArchive(t, false)
	destination := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, stageRequest(source, destination))
	if err != nil {
		t.Fatal(err)
	}
	review := stageReview(t, s, p)
	if !reflect.DeepEqual(review["disksReadInPlace"], []string{"boot", "data"}) {
		t.Fatalf("disks read in place: %v", review["disksReadInPlace"])
	}
	// Only the descriptor is unpacked; each disk needs only its measured output.
	if want := int64(64<<20) + 2*image.OutputBudget(2<<20, 8<<20) + int64(len(streamDescriptor())); review["requiredFreeBytes"] != want {
		t.Fatalf("required %v, want %d", review["requiredFreeBytes"], want)
	}
	archive, _ := os.ReadFile(source)
	for content, w := range tool.windows {
		if string(archive[w.Offset:w.Offset+w.Size]) != content {
			t.Fatalf("range %+v is not its member", w)
		}
	}
	j := finish(t, s, accepted(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatalf("preparation failed: %+v", j.Error)
	}
	if _, err = Verify(context.Background(), destination); err != nil {
		t.Fatal(err)
	}
	if names := unpackedMembers(t, destination, p); !reflect.DeepEqual(names, []string{"appliance.ovf"}) {
		t.Fatalf("unpacked %v; only the descriptor should be", names)
	}
}

func TestADiskRefusedInPlaceIsUnpackedAsBefore(t *testing.T) {
	s := serviceFixture(t, &archiveTool{refuse: "data"})
	source := streamArchive(t, false)
	destination := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, stageRequest(source, destination))
	if err != nil {
		t.Fatal(err)
	}
	if got := stageReview(t, s, p)["disksReadInPlace"]; !reflect.DeepEqual(got, []string{"boot"}) {
		t.Fatalf("disks read in place: %v", got)
	}
	j := finish(t, s, accepted(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatalf("preparation failed: %+v", j.Error)
	}
	if names := unpackedMembers(t, destination, p); !reflect.DeepEqual(names, []string{"appliance.ovf", "data.vmdk"}) {
		t.Fatalf("unpacked %v; the refused disk and the descriptor should be", names)
	}
}

func TestRealStreamOptimizedOVAConvertsInPlace(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit isolated disk-tool fixture execution required; not a boot test")
	}
	s := serviceFixture(t, image.Tool{})
	source := streamArchive(t, true)
	original, _ := os.ReadFile(source)
	destination := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, stageRequest(source, destination))
	if err != nil {
		t.Fatal(err)
	}
	if got := stageReview(t, s, p)["disksReadInPlace"]; !reflect.DeepEqual(got, []string{"boot", "data"}) {
		t.Fatalf("disks read in place: %v", got)
	}
	j := finish(t, s, accepted(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatalf("in-place preparation failed: %+v", j.Error)
	}
	artifact, err := Verify(context.Background(), destination)
	if err != nil || len(artifact.Disks) != 2 || artifact.Disks[0].VirtualBytes != 8<<20 {
		t.Fatalf("artifact %+v %v", artifact.Disks, err)
	}
	if names := unpackedMembers(t, destination, p); !reflect.DeepEqual(names, []string{"appliance.ovf"}) {
		t.Fatalf("unpacked %v; the disks should have been read in place", names)
	}
	if after, _ := os.ReadFile(source); !bytes.Equal(after, original) {
		t.Fatal("original archive changed")
	}
}
