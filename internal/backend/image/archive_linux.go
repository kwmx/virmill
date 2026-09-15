//go:build linux && amd64

package image

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"virmill.local/core/internal/domain"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/wire"
)

// ArchiveWindow is the byte range of one member inside a held archive. Tar
// member data always starts on a 512-byte block after at least one header.
type ArchiveWindow struct {
	Offset int64 `json:"offset"`
	Size   int64 `json:"size"`
}

// archivePath is where a confined worker sees the held archive (ADR 0060).
const archivePath = "/source/archive"

func (w ArchiveWindow) valid() bool {
	return w.Offset >= 512 && w.Offset%512 == 0 && w.Size > 0 && w.Size <= 1<<40
}

// archiveSource names one member for qemu-img: its format driver over a raw
// node that exposes only the reviewed range of the held archive.
func archiveSource(format string, w ArchiveWindow) (string, error) {
	window := map[string]any{"driver": "raw", "offset": w.Offset, "size": w.Size, "file": map[string]any{"driver": "file", "filename": archivePath}}
	node := window
	if format != "raw" {
		node = map[string]any{"driver": format, "file": window}
	}
	b, err := json.Marshal(node)
	return "json:" + string(b), err
}

// isWindow reports whether qemu-img named exactly the reviewed range of the
// held archive. Unknown keys fail closed.
func isWindow(filename string, w ArchiveWindow) bool {
	raw, ok := strings.CutPrefix(filename, "json:")
	if !ok {
		return false
	}
	var node struct {
		Driver string `json:"driver"`
		Offset int64  `json:"offset"`
		Size   int64  `json:"size"`
		File   struct {
			Driver   string `json:"driver"`
			Filename string `json:"filename"`
		} `json:"file"`
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&node) != nil || dec.More() {
		return false
	}
	return node.Driver == "raw" && node.Offset == w.Offset && node.Size == w.Size && node.File.Driver == "file" && node.File.Filename == archivePath
}

// CheckArchiveMember accepts a disk read in place only when it is one image
// with no backing file and every node reads the reviewed range of the held
// archive. A VMDK extent in another file, as in monolithicFlat, is refused.
func CheckArchiveMember(chain []Info, format string, maxVirtual int64, w ArchiveWindow) error {
	if !Format(format) || !w.valid() || len(chain) != 1 {
		return domain.Fail("PERMISSION_DENIED", "an in-place disk must be one image without a backing file")
	}
	top := chain[0]
	if top.Format != format || top.Encrypted || top.Dirty || top.VirtualSize <= 0 || top.VirtualSize > maxVirtual || top.Backing != "" || top.FullBacking != "" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "encrypted, dirty, oversized or backed image, or a format other than the declared one")
	}
	outside := domain.Fail("PERMISSION_DENIED", "the image reads data outside its reviewed archive range")
	nodes := 0
	var check func(v Info, root bool) error
	check = func(v Info, root bool) error {
		if nodes++; nodes > 16 {
			return outside
		}
		switch {
		case v.Format == "file":
			if v.Filename != archivePath || len(v.Children) != 0 {
				return outside
			}
			return nil
		case v.Format == "raw" && isWindow(v.Filename, w):
		case root && v.Format == format && format != "raw":
			if len(v.Children) != 1 || v.Children[0].Info.Format != "raw" {
				return outside
			}
		default:
			return outside
		}
		if len(v.Children) != 1 || v.Children[0].Name != "file" {
			return outside
		}
		return check(v.Children[0].Info, false)
	}
	if err := check(top, true); err != nil {
		return err
	}
	return checkSpecific(top.Specific, func(name string) bool { return isWindow(name, w) })
}

// checkSpecific walks format-specific details: corruption and external data
// files are refused, and every named file must be approved.
func checkSpecific(specific json.RawMessage, approved func(string) bool) error {
	if len(specific) == 0 {
		return nil
	}
	var value any
	if err := wire.Decode(specific, &value); err != nil {
		return err
	}
	var walk func(any) error
	walk = func(value any) error {
		switch n := value.(type) {
		case map[string]any:
			for k, child := range n {
				if k == "corrupt" && child != false {
					return domain.Fail("INVALID_INPUT", "source image reports corruption")
				}
				if k == "data-file" || k == "data-file-raw" {
					return domain.Fail("UNSUPPORTED_CAPABILITY", "external qcow2 data files require a dedicated dependency adapter")
				}
				if k == "filename" {
					name, ok := child.(string)
					if !ok || !approved(name) {
						return domain.Fail("PERMISSION_DENIED", "image extent references an unapproved file")
					}
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range n {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(value)
}

// OutputBudget is the approved converter output for a measured image: the
// measured qcow2 size plus 1% and 16 MiB, never above the old worst case of
// the virtual size plus 25% and 16 MiB (ADR 0060).
func OutputBudget(measured, virtual int64) int64 {
	worst := virtual + virtual/4 + (16 << 20)
	if b := measured + measured/100 + (16 << 20); b < worst {
		return b
	}
	return worst
}

func measured(b []byte) (int64, error) {
	// Newer qemu-img versions add fields such as "bitmaps"; only these two matter.
	var m struct {
		Required       int64 `json:"required"`
		FullyAllocated int64 `json:"fully-allocated"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return 0, err
	}
	if m.Required <= 0 || m.FullyAllocated < m.Required {
		return 0, domain.Fail("INVALID_INPUT", "image measurement is missing or inconsistent")
	}
	return m.Required, nil
}

func archiveSources(archive *os.File) ([]platform.DiskSourceFile, func(), error) {
	if archive == nil {
		return nil, nil, domain.Fail("INVALID_INPUT", "held archive required")
	}
	return guardedSources([]platform.DiskSourceFile{{Path: "archive", File: archive}})
}

// InspectArchive reads one member's block graph in place from the held archive.
func (Tool) InspectArchive(ctx context.Context, archive *os.File, workspace, format string, w ArchiveWindow, maxVirtual int64) ([]Info, error) {
	source, err := archiveSource(format, w)
	if err != nil || !Format(format) || !w.valid() {
		return nil, domain.Fail("INVALID_INPUT", "explicit disk format and archive range required")
	}
	sources, closeGuards, err := archiveSources(archive)
	if err != nil {
		return nil, err
	}
	defer closeGuards()
	b, err := runFiles(ctx, sources, workspace, 64<<20, "info", "--output=json", "--backing-chain", source)
	if err != nil {
		return nil, err
	}
	var chain []Info
	if err = wire.Decode(b, &chain); err != nil {
		return nil, err
	}
	return chain, CheckArchiveMember(chain, format, maxVirtual, w)
}

// MeasureArchive is the size of the qcow2 that converting one in-place member produces.
func (Tool) MeasureArchive(ctx context.Context, archive *os.File, workspace, format string, w ArchiveWindow) (int64, error) {
	source, err := archiveSource(format, w)
	if err != nil || !Format(format) || !w.valid() {
		return 0, domain.Fail("INVALID_INPUT", "explicit disk format and archive range required")
	}
	sources, closeGuards, err := archiveSources(archive)
	if err != nil {
		return 0, err
	}
	defer closeGuards()
	b, err := runFiles(ctx, sources, workspace, 64<<20, "measure", "--output=json", "-O", "qcow2", "-o", "compat=1.1", source)
	if err != nil {
		return 0, err
	}
	return measured(b)
}

// ConvertArchive converts one member in place to /work/disk.qcow2 and checks it
// against the member, with the output limited to maxOutput bytes.
func (Tool) ConvertArchive(ctx context.Context, archive *os.File, workspace, format string, w ArchiveWindow, virtualSize, maxOutput int64) error {
	source, err := archiveSource(format, w)
	if err != nil || !Format(format) || !w.valid() {
		return domain.Fail("INVALID_INPUT", "explicit disk format and archive range required")
	}
	sources, closeGuards, err := archiveSources(archive)
	if err != nil {
		return err
	}
	defer closeGuards()
	return convertSource(workspace, "", source, virtualSize, maxOutput, func(args ...string) ([]byte, error) {
		return runFiles(ctx, sources, workspace, maxOutput, args...)
	})
}

// MeasureFiles is the size of the qcow2 that converting a selected disk produces.
func (Tool) MeasureFiles(ctx context.Context, sources []platform.DiskSourceFile, workspace, filename, format string) (int64, error) {
	guarded, closeGuards, err := guardedSources(sources)
	if err != nil {
		return 0, err
	}
	defer closeGuards()
	if !Format(format) {
		return 0, domain.Fail("INVALID_INPUT", "explicit supported format required")
	}
	b, err := runFiles(ctx, guarded, workspace, 64<<20, "measure", "--output=json", "-O", "qcow2", "-o", "compat=1.1", "-f", format, "/source/"+filename)
	if err != nil {
		return 0, err
	}
	return measured(b)
}
