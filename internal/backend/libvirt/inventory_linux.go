//go:build linux && cgo

package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	native "libvirt.org/go/libvirt"
	"sort"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

func inventoryDigest(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func poolFingerprint(p domain.StoragePool) string {
	return inventoryDigest(struct {
		Key                           domain.ResourceKey
		XML                           string
		Active, Persistent, Autostart bool
	}{p.Key, p.XML, p.Active, p.Persistent, p.Autostart})
}

func observePool(p *native.StoragePool, uri string) (domain.StoragePool, error) {
	var out domain.StoragePool
	id, err := p.GetUUIDString()
	if err != nil {
		return out, err
	}
	out.Key = domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "storage-pool", UUID: id}
	if out.Name, err = p.GetName(); err != nil {
		return out, err
	}
	if out.Active, err = p.IsActive(); err != nil {
		return out, err
	}
	if out.Persistent, err = p.IsPersistent(); err != nil {
		return out, err
	}
	if out.Persistent {
		if out.Autostart, err = p.GetAutostart(); err != nil {
			return out, err
		}
	}
	if out.XML, err = p.GetXMLDesc(0); err != nil {
		return out, err
	}
	if len(out.XML) > wire.MaxFrame {
		return out, domain.Fail("INVALID_INPUT", "storage pool XML exceeds response bounds")
	}
	var def struct {
		XMLName xml.Name `xml:"pool"`
		Type    string   `xml:"type,attr"`
	}
	if err = xml.Unmarshal([]byte(out.XML), &def); err != nil {
		return out, err
	}
	out.Type = def.Type
	out.Ownership = "external"
	out.State = "inactive"
	out.Fingerprint = poolFingerprint(out)
	if out.Active {
		info, e := p.GetInfo()
		if e != nil {
			return out, e
		}
		out.State = map[native.StoragePoolState]string{native.STORAGE_POOL_INACTIVE: "inactive", native.STORAGE_POOL_BUILDING: "building", native.STORAGE_POOL_RUNNING: "running", native.STORAGE_POOL_DEGRADED: "degraded", native.STORAGE_POOL_INACCESSIBLE: "inaccessible"}[info.State]
		if out.State == "" {
			out.State = "unknown"
		}
		out.CapacityBytes = &info.Capacity
		out.AllocatedBytes = &info.Allocation
		out.AvailableBytes = &info.Available
	}
	used := 0
	if err = inventoryBudget(out, &used); err != nil {
		return out, err
	}
	return out, nil
}

func inventoryBudget(value any, used *int) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	*used += len(b) + 1
	if *used > wire.MaxFrame-(128<<10) {
		return domain.Fail("INVALID_INPUT", "inventory exceeds bounded response; inspect individual resource UUIDs")
	}
	return nil
}
func listPools(ctx context.Context, c *native.Connect, uri string) ([]domain.StoragePool, error) {
	all, err := c.ListAllStoragePools(0)
	if err != nil {
		return nil, err
	}
	defer func() {
		for i := range all {
			all[i].Free()
		}
	}()
	out := make([]domain.StoragePool, 0, len(all))
	used := 0
	for i := range all {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		p, e := observePool(&all[i], uri)
		if e != nil {
			return nil, e
		}
		if e = inventoryBudget(p, &used); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key.UUID < out[j].Key.UUID })
	return out, nil
}
func (p *Provider) ListStoragePools(ctx context.Context, uri string) ([]domain.StoragePool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := connect(uri, false)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return listPools(ctx, c, uri)
}
func (p *Provider) GetStoragePool(ctx context.Context, uri, id string) (domain.StoragePool, error) {
	var empty domain.StoragePool
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	c, err := connect(uri, false)
	if err != nil {
		return empty, err
	}
	defer c.Close()
	pool, err := c.LookupStoragePoolByUUIDString(id)
	if err != nil {
		return empty, err
	}
	defer pool.Free()
	return observePool(pool, uri)
}

type networkObservation interface {
	GetUUIDString() (string, error)
	GetName() (string, error)
	IsActive() (bool, error)
	IsPersistent() (bool, error)
	GetAutostart() (bool, error)
	GetXMLDesc(native.NetworkXMLFlags) (string, error)
}

func observeNetwork(n networkObservation, uri string) (domain.VirtualNetwork, error) {
	var out domain.VirtualNetwork
	id, err := n.GetUUIDString()
	if err != nil {
		return out, err
	}
	out.Key = domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "network", UUID: id}
	if out.Name, err = n.GetName(); err != nil {
		return out, err
	}
	if out.Active, err = n.IsActive(); err != nil {
		return out, err
	}
	if out.Persistent, err = n.IsPersistent(); err != nil {
		return out, err
	}
	if out.Persistent {
		if out.Autostart, err = n.GetAutostart(); err != nil {
			return out, err
		}
		if out.PersistentXML, err = n.GetXMLDesc(native.NETWORK_XML_INACTIVE); err != nil {
			return out, err
		}
	}
	if out.Active {
		if out.LiveXML, err = n.GetXMLDesc(0); err != nil {
			return out, err
		}
	}
	if len(out.LiveXML)+len(out.PersistentXML) > wire.MaxFrame {
		return out, domain.Fail("INVALID_INPUT", "network XML exceeds response bounds")
	}
	out.Ownership = "external"
	out.IsolationVerification = "not-run"
	out.Fingerprint = inventoryDigest(out)
	used := 0
	if err = inventoryBudget(out, &used); err != nil {
		return out, err
	}
	return out, nil
}
func listNetworks(ctx context.Context, c *native.Connect, uri string) ([]domain.VirtualNetwork, error) {
	all, err := c.ListAllNetworks(0)
	if err != nil {
		return nil, err
	}
	defer func() {
		for i := range all {
			all[i].Free()
		}
	}()
	out := make([]domain.VirtualNetwork, 0, len(all))
	used := 0
	for i := range all {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		n, e := observeNetwork(&all[i], uri)
		if e != nil {
			return nil, e
		}
		if e = inventoryBudget(n, &used); e != nil {
			return nil, e
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key.UUID < out[j].Key.UUID })
	return out, nil
}
func (p *Provider) ListNetworks(ctx context.Context, uri string) ([]domain.VirtualNetwork, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := connect(uri, false)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	return listNetworks(ctx, c, uri)
}
func (p *Provider) GetNetwork(ctx context.Context, uri, id string) (domain.VirtualNetwork, error) {
	var empty domain.VirtualNetwork
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	c, err := connect(uri, false)
	if err != nil {
		return empty, err
	}
	defer c.Close()
	n, err := c.LookupNetworkByUUIDString(id)
	if err != nil {
		return empty, err
	}
	defer n.Free()
	return observeNetwork(n, uri)
}
