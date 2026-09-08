//go:build !linux

package tui

import "errors"

func exportImportSettings(string, map[string]any) error {
	return errors.New("safe settings export requires Linux")
}
