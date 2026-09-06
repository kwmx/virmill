//go:build linux && amd64

package plugins

import (
	"context"
	"virmill.local/core/internal/app"
)

func Register(s *app.Service, cache string) {
	s.Extensions["plugin.validate"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) {
		m, e := WorkspaceManifest(r.Path)
		return map[string]any{"manifest": m, "distributionVerified": false}, e
	}
	s.Extensions["plugin.test"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) {
		return TestWorkspace(ctx, r.Path, cache)
	}
}
