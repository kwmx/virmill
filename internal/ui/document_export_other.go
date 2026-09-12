//go:build !linux

package ui

import "errors"

func ExportJSONDocument(string, map[string]any) error {
	return errors.New("safe settings export requires Linux")
}
