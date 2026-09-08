//go:build !linux || !amd64

package networkfirewall

import "virmill.local/core/internal/app"

func Register(s *app.Service, config string) {}
