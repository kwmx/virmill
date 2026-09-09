//go:build linux && amd64

package importing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
)

func describeAnySource(ctx context.Context, uid uint32, r app.Request, disks *DiskSetService) (any, error) {
	if uid != uint32(os.Getuid()) {
		return nil, domain.Fail("PERMISSION_DENIED", "source metadata must be read by its local session user")
	}
	if r.Path == "" || !filepath.IsAbs(r.Path) || filepath.Clean(r.Path) != r.Path || r.ID != "" || r.Action != "" || r.After != 0 || r.Apply != nil || len(r.Input) != 0 || (r.Connection != "" && r.Connection != "local" && r.Connection != "qemu:///system" && r.Connection != "qemu:///session") {
		return nil, domain.Fail("INVALID_INPUT", "Choose one local source file or folder without extra settings.")
	}
	if strings.EqualFold(filepath.Ext(r.Path), ".ova") {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if len(r.Input) != 0 || r.Action != "" {
			return nil, domain.Fail("INVALID_INPUT", "Choose a source without extra inspection settings.")
		}
		report, err := importer.Describe(ctx, r.Path, importer.DefaultLimits())
		if err != nil {
			return nil, err
		}
		return importer.SourceDescription{Source: report.Source, Kind: "ova", Name: filepath.Base(report.Source), Disks: []importer.SourceDisk{}, Files: []string{}, Warnings: report.Warnings, Appliance: &report}, nil
	}
	return disks.DescribeSource(ctx, uid, r)
}
