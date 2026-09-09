//go:build linux && amd64

package importing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/validation"
)

type sourceMetadataTool interface {
	DescribeFiles(context.Context, []platform.DiskSourceFile, string, string) (image.Info, error)
}

// DescribeSource observes selected files without creating a plan, hashing image
// payloads, or changing media. Metadata is never evidence of bootability or a
// complete backing chain; preparation independently verifies the source again.
func (s *DiskSetService) DescribeSource(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uid != uint32(os.Getuid()) {
		return nil, domain.Fail("PERMISSION_DENIED", "source metadata must be read by its local session user")
	}
	if r.Path == "" || !filepath.IsAbs(r.Path) || filepath.Clean(r.Path) != r.Path || r.ID != "" || r.Action != "" || r.After != 0 || r.Apply != nil || len(r.Input) != 0 || (r.Connection != "" && r.Connection != "local" && r.Connection != "qemu:///system" && r.Connection != "qemu:///session") {
		return nil, domain.Fail("INVALID_INPUT", "choose one canonical local source file or folder; metadata takes no other options")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	stat, err := os.Lstat(r.Path)
	if err != nil {
		return nil, err
	}
	directory := stat.IsDir()
	root := r.Path
	if !directory {
		root = filepath.Dir(r.Path)
	}
	rootFile, rootID, err := fileidentity.Open(root, true, true)
	if err != nil {
		return nil, err
	}
	defer rootFile.Close()
	d := importer.SourceDescription{Source: r.Path, Root: root, Name: validation.SafeText(filepath.Base(r.Path)), Kind: "disk", Disks: []importer.SourceDisk{}, Files: []string{}, Warnings: []string{"Metadata only. Preparation verifies source bytes and dependencies; OS, CPU, RAM and bootability are not inferred."}}
	selected, candidates := []string{}, []string{}
	if directory {
		d.Kind = "disks"
		entries, e := rootFile.ReadDir(1025)
		if e != nil && e != io.EOF {
			return nil, e
		}
		if len(entries) > 1024 {
			return nil, domain.Fail("INVALID_INPUT", "source folder has too many entries; choose a folder with at most 1024 entries")
		}
		nested := false
		for _, entry := range entries {
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			if entry.IsDir() {
				nested = true
				continue
			}
			if !entry.Type().IsRegular() {
				continue
			}
			selected = append(selected, entry.Name())
			if sourceDiskCandidate(entry.Name()) {
				candidates = append(candidates, entry.Name())
			}
		}
		if nested {
			d.Warnings = append(d.Warnings, "Subfolders are not inspected. Choose the folder directly containing the disks and all required extent/backing files.")
		}
		if len(selected) > 256 {
			return nil, domain.Fail("INVALID_INPUT", "source folder has more than 256 ordinary files; select a smaller source folder")
		}
	} else {
		selected = []string{filepath.Base(r.Path)}
		candidates = append(candidates, selected...)
	}
	if len(candidates) == 0 {
		return nil, domain.Fail("INVALID_INPUT", "no supported disk candidates found; choose qcow2, raw/img, VMDK, VDI, VHD or VHDX files")
	}
	sort.Strings(selected)
	sort.Strings(candidates)
	held := []platform.DiskSourceFile{}
	before := map[string]fileidentity.Identity{}
	defer func() { closeDiskFiles(held) }()
	for _, name := range selected {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if importer.SafePath(name) != nil {
			return nil, domain.Fail("INVALID_INPUT", "source contains an unsafe file name")
		}
		f, id, e := fileidentity.Open(filepath.Join(root, name), false, true)
		if e != nil {
			return nil, e
		}
		guard, e := image.AcquireReadGuard(f)
		f.Close()
		if e != nil {
			return nil, e
		}
		held = append(held, platform.DiskSourceFile{Path: name, File: guard})
		before[name] = id
		if id.Size == 0 || id.Size > 1<<50 {
			return nil, domain.Fail("INVALID_INPUT", "selected source files must be nonempty and no larger than 1 PiB")
		}
	}
	verify := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, e := fileidentity.Observe(root, true)
		if e != nil {
			return e
		}
		if current != rootID {
			return domain.Fail("SOURCE_CHANGED", "source folder changed while metadata was read")
		}
		for _, source := range held {
			id, e := fileidentity.InspectFile(source.File, false)
			if e != nil {
				return e
			}
			named, e := fileidentity.Observe(filepath.Join(root, source.Path), false)
			if e != nil {
				return e
			}
			if id != before[source.Path] || named != id {
				return domain.Fail("SOURCE_CHANGED", "selected source changed while metadata was read")
			}
		}
		return nil
	}
	for _, source := range held {
		if _, candidate := before[source.Path]; candidate && (sourceDiskCandidate(source.Path) || !directory) {
			if err = rejectSourceContainer(source.File, source.Path); err != nil {
				return nil, err
			}
		}
	}
	if !directory {
		f := held[0].File
		recognition, e := recognizeMedia(f, int64(before[selected[0]].Size))
		if e == nil {
			d.Kind = "iso"
			d.Format = recognition
			d.PhysicalBytes = int64(before[selected[0]].Size)
			d.Files = selected
			if label := sourceISOLabel(f, d.PhysicalBytes); label != "" {
				d.Name = label
			}
			if err = verify(); err != nil {
				return nil, err
			}
			return d, nil
		}
		if strings.EqualFold(filepath.Ext(r.Path), ".iso") {
			return nil, e
		}
	}
	tool, ok := s.FilesTool.(sourceMetadataTool)
	if !ok {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "confined disk metadata detection is unavailable")
	}
	workspace, err := os.MkdirTemp("", "virmill-source-metadata-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workspace)
	infos := map[string]image.Info{}
	dependencies := map[string]bool{}
	used := map[string]bool{}
	for _, name := range candidates {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		info, e := tool.DescribeFiles(ctx, held, workspace, name)
		if e != nil {
			return nil, e
		}
		infos[name] = info
		used[name] = true
		for _, dep := range sourceInfoFiles(info) {
			if dep != name {
				dependencies[dep] = true
				used[dep] = true
			}
		}
		if info.Backing != "" {
			dep := sourceBacking(name, info.Backing)
			if dep != "" && before[dep].Generation != "" {
				dependencies[dep] = true
				used[dep] = true
			}
		}
	}
	for _, name := range candidates {
		if dependencies[name] {
			continue
		}
		info := infos[name]
		disk := importer.SourceDisk{ID: fmt.Sprintf("disk%d", len(d.Disks)+1), Path: name, Format: info.Format, VirtualBytes: info.VirtualSize, PhysicalBytes: int64(before[name].Size)}
		if info.Format == "raw" {
			d.Warnings = append(d.Warnings, "Raw format has no identifying header; review whether "+validation.SafeText(name)+" is the intended disk.")
		}
		if info.Backing != "" {
			disk.BackingPath = sourceBacking(name, info.Backing)
			disk.BackingFormat = info.BackingFormat
			if disk.BackingPath == "" {
				d.Warnings = append(d.Warnings, "A disk declares a backing file outside the selected source folder. It was not exposed or verified.")
			} else if before[disk.BackingPath].Generation == "" {
				d.Warnings = append(d.Warnings, "A required backing file is missing from this selection: "+validation.SafeText(disk.BackingPath)+". Select the folder containing the complete chain.")
			} else {
				d.Warnings = append(d.Warnings, "Backing files are declared but their complete chain is verified only during preparation.")
			}
		}
		d.Disks = append(d.Disks, disk)
	}
	if len(d.Disks) == 0 || len(d.Disks) > 64 {
		return nil, domain.Fail("INVALID_INPUT", "select 1–64 independent root disks; a backing cycle or excessive disk set needs explicit review")
	}
	for _, name := range selected {
		if used[name] {
			d.Files = append(d.Files, name)
			d.PhysicalBytes += int64(before[name].Size)
		}
	}
	if !directory {
		d.Format = d.Disks[0].Format
	}
	if err = verify(); err != nil {
		return nil, err
	}
	return d, nil
}

func sourceDiskCandidate(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".qcow2", ".qcow", ".raw", ".img", ".vmdk", ".vdi", ".vhd", ".vpc", ".vhdx":
		return true
	}
	return false
}
func sourceISOLabel(f *os.File, size int64) string {
	for sector := int64(16); sector < 80 && (sector+1)*2048 <= size; sector++ {
		var header [72]byte
		if _, err := f.ReadAt(header[:], sector*2048); err != nil {
			return ""
		}
		if header[0] == 1 && string(header[1:6]) == "CD001" && header[6] == 1 {
			return validation.SafeText(strings.TrimSpace(string(header[40:72])))
		}
	}
	return ""
}
func sourceBacking(name, backing string) string {
	if path.IsAbs(backing) || strings.ContainsAny(backing, ":\\") {
		return ""
	}
	resolved := path.Clean(path.Join(path.Dir(name), backing))
	if importer.SafePath(resolved) != nil {
		return ""
	}
	return resolved
}
func sourceInfoFiles(info image.Info) []string {
	names := map[string]bool{}
	record := func(name string) {
		if strings.HasPrefix(name, "/source/") {
			name = sourceBacking("root", strings.TrimPrefix(name, "/source/"))
			if name != "" {
				names[name] = true
			}
		}
	}
	var walkInfo func(image.Info)
	walkInfo = func(i image.Info) {
		record(i.Filename)
		for _, child := range i.Children {
			walkInfo(child.Info)
		}
		var raw any
		if json.Unmarshal(i.Specific, &raw) == nil {
			var walk func(any)
			walk = func(v any) {
				switch x := v.(type) {
				case map[string]any:
					for k, v := range x {
						if k == "filename" {
							if s, ok := v.(string); ok {
								record(s)
							}
						}
						walk(v)
					}
				case []any:
					for _, v := range x {
						walk(v)
					}
				}
			}
			walk(raw)
		}
	}
	walkInfo(info)
	out := []string{}
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// A raw QEMU result is only its fallback for unrecognized bytes. Known archive
// and configuration containers must never turn into an inferred raw disk.
func rejectSourceContainer(f *os.File, name string) error {
	var header [512]byte
	n, err := f.ReadAt(header[:], 0)
	if err != nil && err != io.EOF {
		return err
	}
	b := header[:n]
	archive := false
	for _, magic := range [][]byte{{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}, {'P', 'K', 3, 4}, {'P', 'K', 5, 6}, {'P', 'K', 7, 8}, {0x1f, 0x8b}, {0xfd, '7', 'z', 'X', 'Z', 0}, {'B', 'Z', 'h'}, {0x28, 0xb5, 0x2f, 0xfd}, {'R', 'a', 'r', '!', 0x1a, 7}} {
		archive = archive || bytes.HasPrefix(b, magic)
	}
	archive = archive || (len(b) >= 262 && string(b[257:262]) == "ustar")
	switch strings.ToLower(filepath.Ext(name)) {
	case ".7z", ".zip", ".gz", ".gzip", ".xz", ".bz2", ".zst", ".rar", ".tar", ".tgz", ".txz", ".ova":
		archive = true
	}
	if archive {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "This source is an archive, not a disk. Extract to a new folder, then choose the contained disk or folder.")
	}
	descriptor := false
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ovf", ".vbox", ".vmx", ".xml":
		descriptor = true
	}
	text := bytes.TrimSpace(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf}))
	descriptor = descriptor || bytes.HasPrefix(text, []byte("<?xml")) || bytes.HasPrefix(text, []byte("<Envelope")) || bytes.HasPrefix(text, []byte("<VirtualBox")) || bytes.HasPrefix(text, []byte(".encoding =")) || bytes.HasPrefix(text, []byte("config.version ="))
	if descriptor {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "This source describes a VM rather than containing a disk. Choose its exported OVA or the folder containing its disk files.")
	}
	return nil
}
