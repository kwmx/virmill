//go:build !linux

package tui

import (
	"fmt"
	"os"
)

func pickerMakeFolder(string, string, os.FileInfo) (bool, error) {
	return false, fmt.Errorf("Creating folders safely in the explorer requires Linux.")
}
