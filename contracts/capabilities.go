// Package contracts exposes the implementation capability registry to all views.
package contracts

import (
	"embed"
	"encoding/json"
	"virmill.local/core/internal/domain"
)

//go:embed capabilities.json
var data embed.FS

func Capabilities() ([]domain.Capability, error) {
	b, e := data.ReadFile("capabilities.json")
	if e != nil {
		return nil, e
	}
	var c []domain.Capability
	e = json.Unmarshal(b, &c)
	return c, e
}
