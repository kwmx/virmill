//go:build !linux

package tui

func freeBytes(string) (uint64, bool) { return 0, false }
