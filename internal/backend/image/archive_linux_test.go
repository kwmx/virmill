//go:build linux && amd64

package image

import (
	"archive/tar"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// windowName is how qemu-img 10 names a raw node over an archive range.
func windowName(w ArchiveWindow) string {
	return fmt.Sprintf(`json:{"offset": %d, "driver": "raw", "size": %d, "file": {"driver": "file", "filename": "%s"}}`, w.Offset, w.Size, archivePath)
}

// memberGraph mirrors qemu-img info --backing-chain for a member read in place.
func memberGraph(format string, w ArchiveWindow, virtual int64) Info {
	file := Info{Format: "file", Filename: archivePath}
	window := Info{Format: "raw", Filename: windowName(w), VirtualSize: w.Size, Children: []Child{{Name: "file", Info: file}}}
	if format == "raw" {
		window.VirtualSize = virtual
		return window
	}
	top := Info{Format: format, Filename: "json:{}", VirtualSize: virtual, Children: []Child{{Name: "file", Info: window}}}
	if format == "vmdk" {
		extent, _ := json.Marshal(windowName(w))
		top.Specific = json.RawMessage(`{"type": "vmdk", "data": {"create-type": "streamOptimized", "extents": [{"filename": ` + string(extent) + `, "format": "", "virtual-size": 67108864}]}}`)
	}
	return top
}

func TestCheckArchiveMemberAcceptsOnlyTheReviewedRange(t *testing.T) {
	w := ArchiveWindow{Offset: 1536, Size: 3235840}
	for _, format := range []string{"vmdk", "qcow2", "raw"} {
		if err := CheckArchiveMember([]Info{memberGraph(format, w, 64<<20)}, format, 128<<20, w); err != nil {
			t.Fatalf("%s member refused: %v", format, err)
		}
	}
	shifted := ArchiveWindow{Offset: w.Offset + 512, Size: w.Size}
	for name, tc := range map[string]struct {
		chain  []Info
		format string
		window ArchiveWindow
		max    int64
	}{
		"another range": {[]Info{memberGraph("vmdk", shifted, 64<<20)}, "vmdk", w, 128 << 20},
		"another file": {func() []Info {
			g := memberGraph("qcow2", w, 64<<20)
			g.Children[0].Info.Children[0].Info.Filename = "/source/other"
			return []Info{g}
		}(), "qcow2", w, 128 << 20},
		// monolithicFlat: the descriptor sits in the archive, its data in another file.
		"flat extent": {func() []Info {
			g := memberGraph("vmdk", w, 64<<20)
			g.Children = append([]Child{{Name: "extents.0", Info: Info{Format: "file", Filename: "flat-flat.vmdk"}}}, g.Children...)
			g.Specific = json.RawMessage(`{"type": "vmdk", "data": {"extents": [{"filename": "flat-flat.vmdk", "format": "FLAT"}]}}`)
			return []Info{g}
		}(), "vmdk", w, 128 << 20},
		"extent elsewhere": {func() []Info {
			g := memberGraph("vmdk", w, 64<<20)
			other, _ := json.Marshal(windowName(shifted))
			g.Specific = json.RawMessage(`{"type": "vmdk", "data": {"extents": [{"filename": ` + string(other) + `}]}}`)
			return []Info{g}
		}(), "vmdk", w, 128 << 20},
		"backing chain":   {[]Info{memberGraph("qcow2", w, 64<<20), memberGraph("qcow2", w, 64<<20)}, "qcow2", w, 128 << 20},
		"backing file":    {func() []Info { g := memberGraph("qcow2", w, 64<<20); g.Backing = "base.qcow2"; return []Info{g} }(), "qcow2", w, 128 << 20},
		"format differs":  {[]Info{memberGraph("vmdk", w, 64<<20)}, "qcow2", w, 128 << 20},
		"too large":       {[]Info{memberGraph("qcow2", w, 64<<20)}, "qcow2", w, 32 << 20},
		"unaligned range": {[]Info{memberGraph("raw", ArchiveWindow{Offset: 100, Size: 512}, 512)}, "raw", ArchiveWindow{Offset: 100, Size: 512}, 1 << 20},
		"unknown option": {func() []Info {
			g := memberGraph("raw", w, 64<<20)
			g.Filename = strings.Replace(g.Filename, `"driver": "raw"`, `"driver": "raw", "cache": {"direct": true}`, 1)
			return []Info{g}
		}(), "raw", w, 128 << 20},
	} {
		if err := CheckArchiveMember(tc.chain, tc.format, tc.max, tc.window); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestArchiveSourceExposesOnlyTheHeldArchiveRange(t *testing.T) {
	w := ArchiveWindow{Offset: 4096, Size: 1 << 20}
	source, err := archiveSource("vmdk", w)
	if err != nil {
		t.Fatal(err)
	}
	var node struct {
		Driver string          `json:"driver"`
		File   json.RawMessage `json:"file"`
	}
	if err = json.Unmarshal([]byte(strings.TrimPrefix(source, "json:")), &node); err != nil || node.Driver != "vmdk" || !isWindow("json:"+string(node.File), w) {
		t.Fatalf("source %s", source)
	}
	if raw, _ := archiveSource("raw", w); !isWindow(raw, w) {
		t.Fatalf("raw source %s", raw)
	}
}

func TestOutputBudgetIsMeasuredButNeverAboveTheWorstCase(t *testing.T) {
	// The owner's appliance: 34 GiB of data in a 120 GiB disk.
	if got, want := OutputBudget(34<<30, 120<<30), int64(34<<30)+(34<<30)/100+(16<<20); got != want {
		t.Fatalf("budget %d, want %d", got, want)
	}
	if got, want := OutputBudget(200<<30, 120<<30), int64(120<<30)+(120<<30)/4+(16<<20); got != want {
		t.Fatalf("budget %d, want the worst case %d", got, want)
	}
}

// countingWriter tells where the next tar entry's data starts.
type countingWriter struct {
	f *os.File
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.f.Write(p)
	c.n += int64(n)
	return n, err
}

func TestRealConfinedArchiveMembers(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit disk-tool fixture run required; this is file conversion, not VM boot")
	}
	ctx, tool := context.Background(), Tool{}
	files := privateTemp(t)
	raw := filepath.Join(files, "disk.raw")
	f, err := os.OpenFile(raw, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(8 << 20)
	f.WriteAt([]byte("Virmill in-place member"), 3<<20)
	f.Close()
	fixtureQEMU(t, "convert", "-f", "raw", "-O", "vmdk", "-o", "subformat=streamOptimized", raw, filepath.Join(files, "stream.vmdk"))
	fixtureQEMU(t, "convert", "-f", "raw", "-O", "qcow2", raw, filepath.Join(files, "disk.qcow2"))
	fixtureQEMU(t, "convert", "-f", "raw", "-O", "vmdk", "-o", "subformat=monolithicFlat", raw, filepath.Join(files, "flat.vmdk"))

	archive := filepath.Join(privateTemp(t), "appliance.ova")
	out, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	counter := &countingWriter{f: out}
	tw := tar.NewWriter(counter)
	windows := map[string]ArchiveWindow{}
	for _, name := range []string{"stream.vmdk", "disk.qcow2", "disk.raw", "flat.vmdk", "flat-flat.vmdk"} {
		b, err := os.ReadFile(filepath.Join(files, name))
		if err != nil {
			t.Fatal(err)
		}
		if err = tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		windows[name] = ArchiveWindow{Offset: counter.n, Size: int64(len(b))}
		tw.Write(b)
		tw.Flush()
	}
	tw.Close()
	out.Close()
	held, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	for name, format := range map[string]string{"stream.vmdk": "vmdk", "disk.qcow2": "qcow2", "disk.raw": "raw"} {
		t.Run(name, func(t *testing.T) {
			w := windows[name]
			chain, err := tool.InspectArchive(ctx, held, privateTemp(t), format, w, 16<<20)
			if err != nil || chain[0].VirtualSize != 8<<20 {
				t.Fatalf("inspect: %v %+v", err, chain)
			}
			size, err := tool.MeasureArchive(ctx, held, privateTemp(t), format, w)
			if err != nil {
				t.Fatal(err)
			}
			budget := OutputBudget(size, 8<<20)
			work := privateTemp(t)
			if err = tool.ConvertArchive(ctx, held, work, format, w, 8<<20, budget); err != nil {
				t.Fatal(err)
			}
			st, err := os.Stat(filepath.Join(work, "disk.qcow2"))
			if err != nil || st.Size() > budget {
				t.Fatalf("output %v %v over budget %d", st, err, budget)
			}
			// An output limit below the image fails the conversion instead of growing.
			if err = tool.ConvertArchive(ctx, held, privateTemp(t), format, w, 8<<20, 64<<10); err == nil {
				t.Fatal("conversion exceeded its output limit")
			}
		})
	}
	if _, err := tool.InspectArchive(ctx, held, privateTemp(t), "vmdk", windows["flat.vmdk"], 16<<20); err == nil {
		t.Fatal("a flat VMDK whose data is another member was read in place")
	}
	shifted := windows["stream.vmdk"]
	shifted.Offset += 512
	if _, err := tool.InspectArchive(ctx, held, privateTemp(t), "vmdk", shifted, 16<<20); err == nil {
		t.Fatal("a shifted range was accepted")
	}
}
