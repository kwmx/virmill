//go:build linux && cgo

package libvirt

import (
	"context"
	"errors"
	"slices"

	"virmill.local/core/internal/backend/poolxml"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

var _ domain.StoragePoolStartProvider = (*Provider)(nil)

func (p *Provider) StartExistingStoragePool(ctx context.Context, uri, id, fingerprint string) error {
	return existingStoragePoolRun(ctx, uri, id, fingerprint, "start", openStoragePoolCreation)
}

func (p *Provider) EnableStoragePoolAutostart(ctx context.Context, uri, id string) error {
	return existingStoragePoolRun(ctx, uri, id, "", "autostart", openStoragePoolCreation)
}

func existingStoragePoolUncertain(reason string) error {
	return domain.Fail("RECOVERY_REQUIRED", "storage pool start "+reason+"; inspect the pool before any retry")
}

// Starting never builds a folder, redefines, stops or deletes a pool. A pool
// whose folder or storage is missing fails with libvirt's reason. Neither
// effect is retried.
func existingStoragePoolRun(ctx context.Context, uri, id, fingerprint, action string, open storagePoolCreationOpen) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = Connection(uri); err != nil {
		return err
	}
	if !uuidPattern.MatchString(id) || (action != "start" && action != "autostart") {
		return domain.Fail("INVALID_INPUT", "invalid storage pool start request")
	}
	c, err := open(uri, true)
	if err != nil {
		return err
	}
	var handle storagePoolCreationHandle
	submitted := false
	defer func() {
		var cleanup error
		if handle != nil {
			cleanup = handle.Free()
		}
		cleanup = errors.Join(cleanup, c.close())
		if cleanup != nil {
			if submitted {
				err = existingStoragePoolUncertain("cleanup failed after native submission")
			} else {
				err = errors.Join(err, cleanup)
			}
		}
		if ctx.Err() != nil {
			if submitted {
				err = existingStoragePoolUncertain("was canceled after native submission")
			} else {
				err = ctx.Err()
			}
		}
	}()
	if handle, err = c.lookup(id); err != nil {
		return err
	}
	first, err := observeExistingStoragePool(handle, uri)
	if err != nil {
		return err
	}
	if first.Key.UUID != id || !first.Persistent {
		return domain.Fail("STALE_PLAN", "storage pool is not the reviewed persistent pool")
	}
	if action == "start" {
		if first.Active {
			return domain.Fail("STALE_PLAN", "storage pool is already active; starting is never replayed")
		}
		if first.Fingerprint != fingerprint {
			return domain.Fail("STALE_PLAN", "storage pool changed since review")
		}
		if !slices.Contains([]string{"dir", "fs", "netfs"}, first.Type) {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "only file-based pools are started")
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		submitted = true
		if err = handle.Create(0); err != nil {
			return existingStoragePoolUncertain("could not start the pool: " + validation.SafeText(err.Error()))
		}
		if after, e := observeExistingStoragePool(handle, uri); e != nil || !after.Active {
			return existingStoragePoolUncertain("is not confirmed active")
		}
		return nil
	}
	if !first.Active {
		return domain.Fail("STALE_PLAN", "storage pool must be active before autostart is enabled")
	}
	if first.Autostart {
		return nil
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	submitted = true
	if err = handle.SetAutostart(true); err != nil {
		return existingStoragePoolUncertain("could not enable autostart: " + validation.SafeText(err.Error()))
	}
	if after, e := observeExistingStoragePool(handle, uri); e != nil || !after.Autostart {
		return existingStoragePoolUncertain("autostart is not confirmed")
	}
	return nil
}

// observeExistingStoragePool computes the same fingerprint as inventory.
func observeExistingStoragePool(handle storagePoolCreationHandle, uri string) (domain.StoragePool, error) {
	var out domain.StoragePool
	id, err := handle.GetUUIDString()
	if err != nil {
		return out, err
	}
	out.Key = domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "storage-pool", UUID: id}
	if out.Name, err = handle.GetName(); err != nil {
		return out, err
	}
	if out.Active, err = handle.IsActive(); err != nil {
		return out, err
	}
	if out.Persistent, err = handle.IsPersistent(); err != nil {
		return out, err
	}
	if out.Persistent {
		if out.Autostart, err = handle.GetAutostart(); err != nil {
			return out, err
		}
	}
	if out.XML, err = handle.GetXMLDesc(0); err != nil {
		return out, err
	}
	if len(out.XML) > wire.MaxFrame {
		return out, domain.Fail("INVALID_INPUT", "storage pool XML exceeds response bounds")
	}
	observed, err := poolxml.Parse(out.XML)
	if err != nil {
		return out, domain.Fail("SOURCE_CHANGED", "unreadable storage pool definition")
	}
	out.Type = observed.Type
	out.Fingerprint = poolFingerprint(out)
	return out, nil
}
