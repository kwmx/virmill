// Package networksettings reads optional, non-secret IPv4 allocation settings.
// It never writes configuration or interprets a read failure as an empty file.
package networksettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/wire"
)

const filename = "network-allocation.json"
const maxBytes = 64 << 10

// Load returns defaults only when the optional file is absent. An existing
// invalid or unreadable file returns no configuration. Linux reads a held
// regular inode; other platforms explicitly refuse existing files.
func Load(ctx context.Context, configDir string) (network.AllocationConfig, error) {
	var empty network.AllocationConfig
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if configDir == "" {
		return empty, errors.New("network allocation config directory is empty")
	}
	raw, exists, err := readFile(ctx, configDir)
	if err != nil {
		return empty, fmt.Errorf("read network allocation settings: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !exists {
		return network.DefaultAllocationConfig(), nil
	}
	var cfg network.AllocationConfig
	if err := wire.Decode(raw, &cfg); err != nil {
		return empty, fmt.Errorf("decode network allocation settings: %w", err)
	}
	// encoding/json accepts case-insensitive aliases even with unknown-field
	// rejection. Require the documented field spelling at every object level.
	if err := exactFields(raw); err != nil {
		return empty, err
	}
	if err := network.ValidateAllocationConfig(cfg); err != nil {
		return empty, fmt.Errorf("invalid network allocation settings: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return cfg, nil
}

func exactObject(raw []byte, keys ...string) (map[string]json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, errors.New("network allocation settings require an object")
	}
	for name, value := range fields {
		allowed := false
		for _, key := range keys {
			allowed = allowed || key == name
		}
		if !allowed || string(value) == "null" {
			return nil, fmt.Errorf("unknown or null network allocation field %q", name)
		}
	}
	return fields, nil
}

func exactFields(raw []byte) error {
	fields, err := exactObject(raw, "version", "ranges", "planned")
	if err != nil {
		return err
	}
	for _, array := range []struct {
		name string
		keys []string
	}{{"ranges", []string{"cidr", "prefixLength"}}, {"planned", []string{"id", "cidr"}}} {
		if encoded, present := fields[array.name]; present {
			var items []json.RawMessage
			if err := json.Unmarshal(encoded, &items); err != nil {
				return err
			}
			for _, item := range items {
				if _, err := exactObject(item, array.keys...); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
