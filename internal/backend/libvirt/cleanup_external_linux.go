//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileidentity"
	imageTool "virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
)

// cleanupExternal maps each absolute file path referenced outside the reconciled
// pools to the formats it is declared with ("" when undeclared).
type cleanupExternal map[string]map[string]bool

func (e cleanupExternal) add(path, format string) {
	if e[path] == nil {
		e[path] = map[string]bool{}
	}
	e[path][format] = true
}

// cleanupExternalObservation is what the dependency graph digest binds for one
// out-of-pool reference: the object it reached, or its proven absence.
type cleanupExternalObservation struct {
	Formats []string              `json:"formats"`
	Absent  bool                  `json:"absent"`
	Object  fileidentity.Object   `json:"object"`
	Backing []cleanupExternalLink `json:"backing,omitempty"`
}
type cleanupExternalLink struct {
	Path   string              `json:"path"`
	Format string              `json:"format"`
	Absent bool                `json:"absent"`
	Object fileidentity.Object `json:"object"`
}

type cleanupQcow2Inspector func(context.Context, *os.File) (imageTool.Info, error)

const cleanupExternalChainDepth = 16

// checkCleanupExternal decides whether files referenced outside the reconciled
// pools reach a cleanup volume. A selected volume is a single-link regular file
// (fileidentity refuses others), so its only directory entry is its pool path;
// any other name that reaches it goes through a symlink, "..", a repeated
// separator or a bind mount, all of which resolve to the same device and inode.
// Each reference is therefore resolved as QEMU would open it and compared with
// the selected objects. A path that does not exist reaches no object. Any other
// failure to observe a path, or to read the header of an image whose declared
// format can carry a backing file, refuses: it could hide a reference.
func checkCleanupExternal(ctx context.Context, refs cleanupExternal, candidates []domain.CleanupCandidate, selected map[string]bool, volumes map[string]cleanupGraphVolume, inspect cleanupQcow2Inspector) (map[string]cleanupExternalObservation, error) {
	if len(refs) > 4096 {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "cleanup graph exceeds 4096 references outside storage pools")
	}
	objects := map[fileidentity.Object]bool{}
	for _, v := range candidates {
		// Only a present candidate carries a generation; an absent one has no object to alias.
		if v.Allocated == nil || v.Allocated.Generation == "" {
			continue
		}
		object, err := fileidentity.GenerationObject(v.Allocated.Generation)
		if err != nil {
			return nil, err
		}
		objects[object] = true
	}
	paths := make([]string, 0, len(refs))
	for path := range refs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := map[string]cleanupExternalObservation{}
	for _, path := range paths {
		formats := make([]string, 0, len(refs[path]))
		for format := range refs[path] {
			formats = append(formats, format)
		}
		sort.Strings(formats)
		observation := cleanupExternalObservation{Formats: formats}
		for _, format := range formats {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			chain, err := cleanupExternalChain(ctx, path, format, selected, objects, volumes, inspect)
			if err != nil {
				return nil, err
			}
			observation.Absent, observation.Object = chain[0].Absent, chain[0].Object
			if len(chain) > 1 {
				observation.Backing = append(observation.Backing, chain[1:]...)
			}
		}
		out[path] = observation
	}
	return out, nil
}

// cleanupExternalChain follows one declared reference and any backing files
// its image header names, stopping at a reconciled pool volume, whose own chain
// the pool graph already inspects.
func cleanupExternalChain(ctx context.Context, path, format string, selected map[string]bool, objects map[fileidentity.Object]bool, volumes map[string]cleanupGraphVolume, inspect cleanupQcow2Inspector) ([]cleanupExternalLink, error) {
	chain := []cleanupExternalLink{}
	seen := map[fileidentity.Object]bool{}
	for {
		if len(chain) >= cleanupExternalChainDepth {
			return nil, domain.Fail("RECOVERY_REQUIRED", "backing chain outside storage pools is too deep to prove: "+path)
		}
		if strings.ContainsRune(path, 0) {
			return nil, domain.Fail("INVALID_INPUT", "referenced path contains NUL")
		}
		if selected[path] {
			return nil, domain.Fail("RESOURCE_BUSY", "retained image references a cleanup volume: "+path)
		}
		if volumes[path].path != "" {
			return append(chain, cleanupExternalLink{Path: path, Format: format}), nil
		}
		if format == "iso" {
			format = "raw"
		}
		link := cleanupExternalLink{Path: path, Format: format}
		object, kind, err := fileidentity.Resolve(path)
		if cleanupPathAbsent(err) {
			link.Absent = true
			return append(chain, link), nil
		}
		if err != nil {
			return nil, domain.Fail("RECOVERY_REQUIRED", "cannot observe a file outside storage pools, so it may alias a cleanup volume: "+path+": "+err.Error())
		}
		if objects[object] {
			return nil, domain.Fail("RESOURCE_BUSY", "a path outside storage pools reaches a cleanup volume: "+path)
		}
		link.Object = object
		if format == "raw" {
			// QEMU opens an explicitly raw source without reading a backing file.
			return append(chain, link), nil
		}
		if format != "" && format != "qcow2" {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "image format outside storage pools needs its own dependency inspection: "+format+": "+path)
		}
		if kind != unix.S_IFREG {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "non-regular image outside storage pools without an explicit raw format: "+path)
		}
		backing, backingFormat, err := cleanupExternalHeader(ctx, path, format, object, objects, inspect)
		if err != nil {
			return nil, err
		}
		if seen[object] {
			return nil, domain.Fail("RECOVERY_REQUIRED", "backing graph cycle outside storage pools prevents deletion")
		}
		seen[object] = true
		chain = append(chain, link)
		if backing == "" {
			return chain, nil
		}
		path, format = backing, backingFormat
	}
}

// cleanupExternalHeader opens the referenced file, confirms it is still the
// object that was compared, and returns the backing reference its header names.
func cleanupExternalHeader(ctx context.Context, path, format string, object fileidentity.Object, objects map[fileidentity.Object]bool, inspect cleanupQcow2Inspector) (string, string, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", "", domain.Fail("RECOVERY_REQUIRED", "cannot read the header of an image outside storage pools, so its backing file is unknown: "+path+": "+err.Error())
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	opened, kind, err := fileidentity.FileObject(f)
	if err != nil {
		return "", "", err
	}
	if objects[opened] {
		return "", "", domain.Fail("RESOURCE_BUSY", "a path outside storage pools reaches a cleanup volume: "+path)
	}
	if opened != object || kind != unix.S_IFREG {
		return "", "", domain.Fail("SOURCE_CHANGED", "file outside storage pools changed during dependency inspection: "+path)
	}
	if format == "" {
		var magic [4]byte
		if n, _ := f.ReadAt(magic[:], 0); n != 4 || string(magic[:]) != "QFI\xfb" {
			return "", "", nil
		}
	}
	info, err := inspect(ctx, f)
	if err != nil {
		return "", "", err
	}
	backing, err := cleanupBackingPath(path, info.Backing, info.BackingFormat)
	return backing, info.BackingFormat, err
}

func cleanupPathAbsent(err error) bool {
	return errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ENOTDIR)
}
