//go:build linux && amd64

package image

import (
	"context"
	"time"

	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/wire"
)

// DescribeFiles auto-detects metadata from exactly the caller-selected held files.
// It never follows a backing chain. Extents may be opened only within the
// selected read-only namespace; no surrounding source directory is exposed.
func (t Tool) DescribeFiles(ctx context.Context, sources []platform.DiskSourceFile, workspace, filename string) (Info, error) {
	var info Info
	if err := ctx.Err(); err != nil {
		return info, err
	}
	if importer.SafePath(filename) != nil {
		return info, domain.Fail("INVALID_INPUT", "choose a contained ordinary disk file")
	}
	guarded, closeAll, err := guardedSources(sources)
	if err != nil {
		return info, err
	}
	defer closeAll()
	before := make([]fileidentity.Identity, len(guarded))
	members := map[string]bool{}
	for i, source := range guarded {
		if importer.SafePath(source.Path) != nil || members[source.Path] {
			return info, domain.Fail("INVALID_INPUT", "unique contained selected files required")
		}
		members[source.Path] = true
		before[i], err = fileidentity.InspectFile(source.File, false)
		if err != nil {
			return info, err
		}
	}
	if !members[filename] {
		return info, domain.Fail("INVALID_INPUT", "disk must be one of the selected files")
	}
	if _, err = t.Identity(ctx); err != nil {
		return info, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// No -f: detection belongs to QEMU, not the filename suffix. No -U or
	// --backing-chain: writer exclusion and unapproved backing isolation remain.
	output, err := runFiles(ctx, guarded, workspace, 1<<20, "info", "--output=json", "/source/"+filename)
	if err != nil {
		if ctx.Err() != nil {
			return info, ctx.Err()
		}
		// A disk parser may echo attacker-controlled descriptor contents. Keep raw
		// diagnostics out of the service response and normal logs.
		return info, domain.Fail("UNSUPPORTED_CAPABILITY", "Cannot read this disk's metadata in isolation. Select its folder and include required extent files; encrypted or unsupported images need conversion first.")
	}
	if err = wire.Decode(output, &info); err != nil {
		return info, domain.Fail("INVALID_INPUT", "disk metadata response is malformed")
	}
	for i, source := range guarded {
		after, e := fileidentity.InspectFile(source.File, false)
		if e != nil {
			return info, e
		}
		if before[i] != after {
			return info, domain.Fail("SOURCE_CHANGED", "selected disk files changed during metadata inspection")
		}
	}
	if err = checkDescription(info, filename, members); err != nil {
		return info, err
	}
	return info, nil
}

func checkDescription(info Info, filename string, members map[string]bool) error {
	// Backing paths are unverified declarations. Removing only that edge allows
	// the existing graph validator to check every actually opened extent/child.
	node := info
	node.Backing, node.FullBacking, node.BackingFormat = "", "", ""
	return CheckChain([]Info{node}, info.Format, filename, 1<<50, members)
}
