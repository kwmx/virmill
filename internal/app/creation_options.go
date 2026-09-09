package app

import (
	"context"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

func (s *Service) creationOptions(ctx context.Context, r Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.ID != "" || r.Path != "" || r.Action != "" || r.After != 0 || r.Apply != nil || len(r.Input) > 1 {
		return nil, domain.Fail("INVALID_INPUT", "Hardware choices accept only an optional machine name.")
	}
	machine := ""
	for key, value := range r.Input {
		var ok bool
		machine, ok = value.(string)
		if key != "machine" || !ok || len(machine) > 128 || validation.SafeText(machine) != machine {
			return nil, domain.Fail("INVALID_INPUT", "Choose an advertised machine name.")
		}
	}
	if r.Connection != "qemu:///system" && r.Connection != "qemu:///session" {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Hardware choices require a local libvirt connection.")
	}
	backend, ok := s.Provider.(domain.CreationOptionsBackend)
	if !ok {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "This backend cannot report VM hardware choices.")
	}
	return backend.CreationOptions(ctx, r.Connection, machine)
}
