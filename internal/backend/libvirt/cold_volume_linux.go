//go:build linux && cgo

package libvirt

import (
	"context"
	native "libvirt.org/go/libvirt"
	"path/filepath"
	"virmill.local/core/internal/domain"
)

func (p *Provider) ResolveColdVolume(ctx context.Context, uri string, source domain.ColdStorageSource) (domain.ColdResolvedVolume, error) {
	var out domain.ColdResolvedVolume
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if source.Type != "volume" || source.Pool == "" || source.Volume == "" || source.File != "" {
		return out, domain.Fail("INVALID_INPUT", "exact native pool/volume source required")
	}
	c, err := connect(uri, false)
	if err != nil {
		return out, err
	}
	defer c.Close()
	pool, err := c.LookupStoragePoolByName(source.Pool)
	if err != nil {
		return out, err
	}
	defer pool.Free()
	observed, err := observePool(pool, uri)
	if err != nil {
		return out, err
	}
	if !observed.Active || observed.State != "running" || observed.Type != "dir" {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "cold volume source requires an active directory pool")
	}
	vol, err := pool.LookupStorageVolByName(source.Volume)
	if err != nil {
		return out, err
	}
	defer vol.Free()
	info, err := vol.GetInfo()
	if err != nil {
		return out, err
	}
	if info.Type != native.STORAGE_VOL_FILE {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "cold capture requires a file volume")
	}
	out.Source = source
	out.PoolID = observed.Key.UUID
	out.PoolFingerprint, err = poolCreationFingerprint(observed)
	if err != nil {
		return out, err
	}
	out.Key, err = vol.GetKey()
	if err != nil {
		return out, err
	}
	out.Path, err = vol.GetPath()
	if err != nil {
		return out, err
	}
	if out.Key == "" || !filepath.IsAbs(out.Path) || filepath.Clean(out.Path) != out.Path {
		return domain.ColdResolvedVolume{}, domain.Fail("UNSUPPORTED_CAPABILITY", "native volume omitted a canonical file identity")
	}
	return out, ctx.Err()
}
