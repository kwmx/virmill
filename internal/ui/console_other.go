//go:build !linux

package ui

import (
	"context"
	"os/exec"

	"virmill.local/core/internal/domain"
)

type ConsoleSession struct{ Command *exec.Cmd }

func (s *ConsoleSession) Close() error { return nil }
func PrepareConsole(context.Context, Client, string, string, string, string) (*ConsoleSession, error) {
	return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Guest console access requires the local Linux host.")
}
