//go:build linux && amd64

package plugins

import (
	"context"
	"path/filepath"
	"virmill.local/core/internal/app"
	platform "virmill.local/core/internal/platform/linux"
)

func Register(s *app.Service, cache, data, sdk string) error {
	root := filepath.Join(data, "plugins")
	if err := platform.PrivateDir(root); err != nil {
		return err
	}
	m := &Manager{Store: s.Engine.Store, Engine: s.Engine, Root: root}
	d := &Developer{Engine: s.Engine, SDKDirectory: sdk}
	a := &Actions{Manager: m, Provider: s.Provider, Cache: cache}
	s.Engine.Handlers["plugin.call"] = a
	s.Extensions["plugin.call"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return a.Plan(ctx, uid, r) }
	s.Extensions["plugin.result"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return a.Result(r.ID) }
	for _, action := range []string{"install", "update", "enable", "disable", "remove", "rollback", "grant", "revoke"} {
		s.Engine.Handlers["plugin."+action] = m
	}
	for _, action := range []string{"new", "pack"} {
		s.Engine.Handlers["plugin."+action] = d
	}
	s.Extensions["plugin.plan"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) {
		return m.Plan(ctx, uid, r.Action, r)
	}
	s.Extensions["plugin.develop"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) {
		return d.Plan(ctx, uid, r.Action, r)
	}
	s.Extensions["plugin.list"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return m.List() }
	s.Extensions["plugin.show"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return m.Show(r.ID) }
	s.Extensions["plugin.permissions"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) {
		installation, err := m.Show(r.ID)
		if err != nil {
			return nil, err
		}
		active := installation.Versions[installation.Active]
		return map[string]any{"pluginID": installation.ID, "packageDigest": installation.Active, "enabled": installation.Enabled, "declared": active.Manifest.Permissions, "installedGrants": active.Grants, "invocationGrants": "separate, expiring and resource-scoped"}, nil
	}
	s.Extensions["plugin.validate"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) {
		m, e := WorkspaceManifest(r.Path)
		return map[string]any{"manifest": m, "distributionVerified": false}, e
	}
	s.Extensions["plugin.test"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) {
		return TestWorkspace(ctx, r.Path, cache)
	}
	return nil
}
