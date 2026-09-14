//go:build linux && cgo

package libvirt

import (
	"context"
	"errors"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/poolxml"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const storagePoolCreationInventoryLimit = 4096

var _ domain.StoragePoolCreationProvider = (*Provider)(nil)

type storagePoolCreationHandle interface {
	GetUUIDString() (string, error)
	GetName() (string, error)
	IsActive() (bool, error)
	IsPersistent() (bool, error)
	GetAutostart() (bool, error)
	GetXMLDesc(native.StorageXMLFlags) (string, error)
	Build(native.StoragePoolBuildFlags) error
	Create(native.StoragePoolCreateFlags) error
	SetAutostart(bool) error
	Free() error
}

type storagePoolIdentity struct{ UUID, Name, Path string }

type storagePoolCreationConnection interface {
	pools(context.Context) ([]storagePoolIdentity, error)
	lookup(string) (storagePoolCreationHandle, error)
	define(string) (storagePoolCreationHandle, error)
	close() error
}

type storagePoolCreationOpen func(string, bool) (storagePoolCreationConnection, error)

func (p *Provider) CheckStoragePoolCreation(ctx context.Context, uri string, def domain.StoragePoolDefinition) error {
	_, err := storagePoolCreationRun(ctx, uri, def, "check", openStoragePoolCreation)
	return err
}

func (p *Provider) DefineStoragePool(ctx context.Context, uri string, def domain.StoragePoolDefinition) error {
	_, err := storagePoolCreationRun(ctx, uri, def, "define", openStoragePoolCreation)
	return err
}

func (p *Provider) StartStoragePool(ctx context.Context, uri string, def domain.StoragePoolDefinition) error {
	_, err := storagePoolCreationRun(ctx, uri, def, "start", openStoragePoolCreation)
	return err
}

func (p *Provider) SetStoragePoolAutostart(ctx context.Context, uri string, def domain.StoragePoolDefinition) error {
	_, err := storagePoolCreationRun(ctx, uri, def, "autostart", openStoragePoolCreation)
	return err
}

func (p *Provider) InspectCreatedStoragePool(ctx context.Context, uri string, def domain.StoragePoolDefinition) (domain.StoragePool, error) {
	return storagePoolCreationRun(ctx, uri, def, "inspect", openStoragePoolCreation)
}

func storagePoolCreationUncertain(reason string) error {
	return domain.Fail("RECOVERY_REQUIRED", "storage pool creation "+reason+"; inspect the pool by its UUID before any retry")
}

// Libvirt has no create-only compare-and-swap for pool definitions. These
// checks refuse every observed collision but cannot exclude another writer
// between the last observation and the native call. Define, start and
// autostart are separate effects; none is retried, and nothing here undefines,
// stops, deletes or rewrites a pool or folder.
func storagePoolCreationRun(ctx context.Context, uri string, def domain.StoragePoolDefinition, action string, open storagePoolCreationOpen) (out domain.StoragePool, err error) {
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = Connection(uri); err != nil {
		return out, err
	}
	if err = poolxml.Validate(def); err != nil {
		return out, domain.Fail("INVALID_INPUT", err.Error())
	}
	switch action {
	case "check", "define", "start", "autostart", "inspect":
	default:
		return out, domain.Fail("INVALID_INPUT", "unknown storage pool operation")
	}
	c, err := open(uri, action == "define" || action == "start" || action == "autostart")
	if err != nil {
		return out, err
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
				err = storagePoolCreationUncertain("cleanup failed after native submission")
			} else {
				err = errors.Join(err, cleanup)
			}
		}
		if ctx.Err() != nil {
			if submitted {
				err = storagePoolCreationUncertain("was canceled after native submission")
			} else {
				err = ctx.Err()
			}
		}
		if err != nil {
			out = domain.StoragePool{}
		}
	}()
	if action == "check" || action == "define" {
		if err = checkStoragePoolCollisions(ctx, c, def, false); err != nil || action == "check" {
			return out, err
		}
		raw, renderErr := poolxml.Render(def)
		if renderErr != nil {
			return out, renderErr
		}
		if err = ctx.Err(); err != nil {
			return out, err
		}
		submitted = true
		handle, err = c.define(raw)
		if err != nil {
			return out, storagePoolCreationUncertain("definition was refused or its acknowledgement was lost: " + validation.SafeText(err.Error()))
		}
		out, err = inspectStoragePoolDefinition(handle, uri, def)
		if err != nil || out.Active || !out.Persistent {
			return domain.StoragePool{}, storagePoolCreationUncertain("new definition is not the exact inactive persistent pool")
		}
		return out, nil
	}
	handle, err = c.lookup(def.UUID)
	if err != nil {
		return out, err
	}
	first, err := inspectStoragePoolDefinition(handle, uri, def)
	if err != nil {
		return out, err
	}
	if !first.Persistent {
		return out, domain.Fail("STALE_PLAN", "storage pool is no longer persistent")
	}
	switch action {
	case "inspect":
		return first, nil
	case "start":
		if first.Active {
			return out, domain.Fail("STALE_PLAN", "storage pool is already active; starting is never replayed")
		}
		if err = checkStoragePoolCollisions(ctx, c, def, true); err != nil {
			return out, err
		}
		if err = ctx.Err(); err != nil {
			return out, err
		}
		submitted = true
		// Build creates only a missing folder, with libvirt's default mode. An
		// existing folder keeps its owner, mode, security label and files.
		if err = handle.Build(0); err != nil {
			return out, storagePoolCreationUncertain("could not prepare the folder: " + validation.SafeText(err.Error()))
		}
		if err = handle.Create(0); err != nil {
			return out, storagePoolCreationUncertain("could not start the pool: " + validation.SafeText(err.Error()))
		}
		out, err = inspectStoragePoolDefinition(handle, uri, def)
		if err != nil || !out.Active {
			return domain.StoragePool{}, storagePoolCreationUncertain("start is not the exact active pool")
		}
		return out, nil
	default:
		if !first.Active {
			return out, domain.Fail("STALE_PLAN", "storage pool must be active before autostart is enabled")
		}
		if first.Autostart {
			return first, nil
		}
		if err = ctx.Err(); err != nil {
			return out, err
		}
		submitted = true
		if err = handle.SetAutostart(true); err != nil {
			return out, storagePoolCreationUncertain("could not enable autostart: " + validation.SafeText(err.Error()))
		}
		out, err = inspectStoragePoolDefinition(handle, uri, def)
		if err != nil || !out.Autostart {
			return domain.StoragePool{}, storagePoolCreationUncertain("autostart is not enabled on the exact pool")
		}
		return out, nil
	}
}

func inspectStoragePoolDefinition(handle storagePoolCreationHandle, uri string, def domain.StoragePoolDefinition) (domain.StoragePool, error) {
	var out domain.StoragePool
	id, err := handle.GetUUIDString()
	if err != nil {
		return out, err
	}
	if id != def.UUID {
		return out, domain.Fail("SOURCE_CHANGED", "storage pool UUID differs from the reviewed pool")
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
	if out.Name != def.Name || poolxml.Match(out.XML, def) != nil {
		return domain.StoragePool{}, domain.Fail("SOURCE_CHANGED", "storage pool differs from the reviewed directory pool")
	}
	out.Type = "dir"
	out.Ownership = "external"
	out.State = "inactive"
	if out.Active {
		out.State = "running"
	}
	out.Fingerprint = poolFingerprint(out)
	return out, nil
}

func checkStoragePoolCollisions(ctx context.Context, c storagePoolCreationConnection, def domain.StoragePoolDefinition, allowSelected bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	all, err := c.pools(ctx)
	if err != nil {
		return err
	}
	if len(all) > storagePoolCreationInventoryLimit {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "storage pool collision inventory exceeds bounds")
	}
	seen, names := map[string]bool{}, map[string]bool{}
	for _, p := range all {
		if !uuidPattern.MatchString(p.UUID) || p.Name == "" || seen[p.UUID] || names[p.Name] {
			return domain.Fail("SOURCE_CHANGED", "incomplete or ambiguous existing storage pool identities")
		}
		seen[p.UUID], names[p.Name] = true, true
		if allowSelected && p.UUID == def.UUID {
			continue
		}
		switch {
		case p.UUID == def.UUID:
			return domain.Fail("INVALID_STATE", "a storage pool already uses the reviewed UUID")
		case p.Name == def.Name:
			return domain.Fail("INVALID_STATE", "a storage pool named "+p.Name+" already exists; start or use it, or choose another name")
		case poolxml.Overlaps(p.Path, def.Path):
			return domain.Fail("INVALID_STATE", "storage pool "+p.Name+" already uses "+validation.SafeText(p.Path)+"; use that pool or choose a separate folder")
		}
	}
	if allowSelected && !seen[def.UUID] {
		return domain.Fail("STALE_PLAN", "the reviewed storage pool is no longer defined")
	}
	return nil
}

type nativeStoragePoolCreation struct {
	connection *native.Connect
}

func openStoragePoolCreation(uri string, write bool) (storagePoolCreationConnection, error) {
	c, err := connect(uri, write)
	if err != nil {
		return nil, err
	}
	return &nativeStoragePoolCreation{connection: c}, nil
}
func (s *nativeStoragePoolCreation) close() error { _, err := s.connection.Close(); return err }
func (s *nativeStoragePoolCreation) lookup(id string) (storagePoolCreationHandle, error) {
	p, err := s.connection.LookupStoragePoolByUUIDString(id)
	if err != nil {
		return nil, err
	}
	return p, nil
}
func (s *nativeStoragePoolCreation) define(raw string) (storagePoolCreationHandle, error) {
	p, err := s.connection.StoragePoolDefineXML(raw, native.STORAGE_POOL_DEFINE_VALIDATE)
	if err != nil {
		return nil, err
	}
	return p, nil
}
func (s *nativeStoragePoolCreation) pools(ctx context.Context) (out []storagePoolIdentity, err error) {
	all, err := s.connection.ListAllStoragePools(0)
	if err != nil {
		return nil, err
	}
	defer func() {
		for i := range all {
			if e := all[i].Free(); e != nil && err == nil {
				err = e
			}
		}
	}()
	if len(all) > storagePoolCreationInventoryLimit {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "storage pool collision inventory exceeds bounds")
	}
	for i := range all {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		id, e := all[i].GetUUIDString()
		if e != nil {
			return nil, e
		}
		name, e := all[i].GetName()
		if e != nil {
			return nil, e
		}
		raw, e := all[i].GetXMLDesc(0)
		if e != nil {
			return nil, e
		}
		if len(raw) > wire.MaxFrame {
			return nil, domain.Fail("INVALID_INPUT", "storage pool XML exceeds bounds")
		}
		observed, e := poolxml.Parse(raw)
		if e != nil {
			return nil, domain.Fail("SOURCE_CHANGED", "unreadable existing storage pool definition")
		}
		out = append(out, storagePoolIdentity{UUID: id, Name: name, Path: observed.Path})
	}
	return out, nil
}
