//go:build !linux || !amd64

package coldcapture

import "virmill.local/core/internal/app"

// The qualified native capture adapters are Linux/amd64 only.
func Register(a *app.Service, data, cache, config string) {}
