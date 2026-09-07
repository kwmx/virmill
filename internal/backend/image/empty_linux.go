//go:build linux && amd64

package image

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

// CreateEmpty creates a fresh independent qcow2 disk inside its private workspace.
// A positive complete zero map verifies initial virtual contents without creating
// a second maximum-sized sparse file. No guest filesystem is created or grown.
func (Tool) CreateEmpty(ctx context.Context, workspace string, virtualBytes, maximumFileBytes int64) error {
	if virtualBytes < 1<<20 || virtualBytes > 512<<30 || virtualBytes%512 != 0 || maximumFileBytes < virtualBytes {
		return domain.Fail("INVALID_INPUT", "bounded sector-aligned blank disk size required")
	}
	if _, err := os.Lstat(filepath.Join(workspace, "disk.qcow2")); !os.IsNotExist(err) {
		return domain.Fail("STALE_PLAN", "blank disk destination already exists")
	}
	source := filepath.Join(workspace, "empty-source")
	if err := os.Mkdir(source, 0700); err != nil {
		return err
	}
	runTool := func(args ...string) ([]byte, error) { return run(ctx, source, workspace, maximumFileBytes, args...) }
	if _, err := runTool("create", "-f", "qcow2", "-o", "compat=1.1,preallocation=off", "/work/disk.qcow2", strconv.FormatInt(virtualBytes, 10)); err != nil {
		return err
	}
	b, err := runTool("info", "--output=json", "-f", "qcow2", "/work/disk.qcow2")
	if err != nil {
		return err
	}
	var info Info
	if err = wire.Decode(b, &info); err != nil {
		return err
	}
	if info.Format != "qcow2" || info.VirtualSize != virtualBytes || info.Encrypted || info.Dirty || info.Backing != "" || info.FullBacking != "" {
		return domain.Fail("SOURCE_CHANGED", "new disk has unexpected format, size or dependencies")
	}
	if _, err = runTool("check", "--output=json", "-f", "qcow2", "/work/disk.qcow2"); err != nil {
		return err
	}
	b, err = runTool("map", "--output=json", "-f", "qcow2", "/work/disk.qcow2")
	if err != nil {
		return err
	}
	return checkZeroMap(b, virtualBytes)
}

func checkZeroMap(b []byte, virtualBytes int64) error {
	var extents []struct {
		Start      int64  `json:"start"`
		Length     int64  `json:"length"`
		Depth      int    `json:"depth"`
		Present    bool   `json:"present"`
		Zero       bool   `json:"zero"`
		Data       bool   `json:"data"`
		Compressed bool   `json:"compressed"`
		Offset     *int64 `json:"offset,omitempty"`
		Filename   string `json:"filename,omitempty"`
	}
	if err := wire.Decode(b, &extents); err != nil {
		return err
	}
	if len(extents) < 1 || len(extents) > 4096 {
		return domain.Fail("RECOVERY_REQUIRED", "new disk needs a bounded complete zero map")
	}
	var end int64
	for _, extent := range extents {
		if !extent.Zero || extent.Depth != 0 || extent.Start != end || extent.Length < 1 || extent.Length > virtualBytes-end {
			return domain.Fail("RECOVERY_REQUIRED", "new disk zero-content map is incomplete or nonzero")
		}
		end += extent.Length
	}
	if end != virtualBytes {
		return domain.Fail("RECOVERY_REQUIRED", "new disk map does not cover the complete virtual capacity")
	}
	return nil
}
