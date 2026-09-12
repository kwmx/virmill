//go:build linux

package tui

import "virmill.local/core/internal/ui"

const importSettingsExportLimit = 1 << 20

func exportImportSettings(path string, input map[string]any) error {
	return ui.ExportJSONDocument(path, input)
}
