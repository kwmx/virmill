//go:build linux && cgo

// Package libvirt is the only core package importing the official native binding.
package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	native "libvirt.org/go/libvirt"
	"time"
	"virmill.local/core/internal/domain"
)

type Provider struct{}

func Connection(uri string) error {
	if uri != "qemu:///system" && uri != "qemu:///session" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "built-in provider accepts only explicit local qemu:///system or qemu:///session")
	}
	return nil
}
func connect(uri string, write bool) (*native.Connect, error) {
	if e := Connection(uri); e != nil {
		return nil, e
	}
	if write {
		return native.NewConnect(uri)
	}
	return native.NewConnectReadOnly(uri)
}
func fingerprint(v domain.VM) string {
	v.Fingerprint = ""
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func observe(d *native.Domain, uri string) (domain.VM, error) {
	var v domain.VM
	id, e := d.GetUUIDString()
	if e != nil {
		return v, e
	}
	v.Key = domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}
	v.Name, e = d.GetName()
	if e != nil {
		return v, e
	}
	state, _, e := d.GetState()
	if e != nil {
		return v, e
	}
	v.State = map[native.DomainState]string{native.DOMAIN_NOSTATE: "unknown", native.DOMAIN_RUNNING: "running", native.DOMAIN_BLOCKED: "blocked", native.DOMAIN_PAUSED: "paused", native.DOMAIN_SHUTDOWN: "shutting-down", native.DOMAIN_SHUTOFF: "stopped", native.DOMAIN_CRASHED: "crashed", native.DOMAIN_PMSUSPENDED: "suspended"}[state]
	persistent, e := d.IsPersistent()
	if e != nil {
		return v, e
	}
	if persistent {
		v.HasManagedSave, e = d.HasManagedSaveImage(0)
		if e != nil {
			return v, e
		}
		v.PersistentXML, e = d.GetXMLDesc(native.DOMAIN_XML_INACTIVE)
		if e != nil {
			return v, e
		}
		v.Autostart, e = d.GetAutostart()
		if e != nil {
			return v, e
		}
	}
	active, e := d.IsActive()
	if e != nil {
		return v, e
	}
	if active {
		v.LiveXML, e = d.GetXMLDesc(0)
		if e != nil {
			return v, e
		}
	}
	v.Ownership = "external"
	v.Tags = []string{}
	v.Fingerprint = fingerprint(v)
	return v, nil
}
func (p *Provider) List(ctx context.Context, uri string) ([]domain.VM, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	c, e := connect(uri, false)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	domains, e := c.ListAllDomains(0)
	if e != nil {
		return nil, e
	}
	out := []domain.VM{}
	for i := range domains {
		v, err := observe(&domains[i], uri)
		domains[i].Free()
		if err != nil {
			for j := i + 1; j < len(domains); j++ {
				domains[j].Free()
			}
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (p *Provider) Get(ctx context.Context, uri, id string) (domain.VM, error) {
	if e := ctx.Err(); e != nil {
		return domain.VM{}, e
	}
	c, e := connect(uri, false)
	if e != nil {
		return domain.VM{}, e
	}
	defer c.Close()
	d, e := c.LookupDomainByUUIDString(id)
	if e != nil {
		return domain.VM{}, e
	}
	defer d.Free()
	return observe(d, uri)
}
func (p *Provider) Capabilities(ctx context.Context, uri string) ([]domain.Capability, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	c, e := connect(uri, false)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	v, e := c.GetLibVersion()
	if e != nil {
		return nil, e
	}
	out := []domain.Capability{{ID: "inventory", Status: "supported", ReasonCode: "LIBVIRT_READ_ACCESS", Reason: fmt.Sprintf("read-only native libvirt access; library version %d", v), Alternatives: []string{}, EvidenceClass: "runtime-probe"}}
	if uri == "qemu:///session" {
		out = append(out, domain.Capability{ID: "host-network", Status: "unsupported-on-this-configuration", ReasonCode: "SESSION_CONNECTION", Reason: "session connection does not grant system network management", Alternatives: []string{"Select qemu:///system explicitly"}, EvidenceClass: "connection-policy"})
	}
	return out, nil
}
func (p *Provider) Execute(ctx context.Context, uri, id, action string, input map[string]any) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	c, e := connect(uri, true)
	if e != nil {
		return e
	}
	defer c.Close()
	d, e := c.LookupDomainByUUIDString(id)
	if e != nil {
		return e
	}
	defer d.Free()
	switch action {
	case "start":
		return d.Create()
	case "restore-saved":
		present, err := d.HasManagedSaveImage(0)
		if err != nil {
			return err
		}
		if !present {
			return domain.Fail("STALE_PLAN", "managed save image is missing; no fresh boot was attempted")
		}
		return d.Create()
	case "stop":
		if e = d.Shutdown(); e != nil {
			return e
		}
		deadline := time.NewTimer(60 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return domain.Fail("RECOVERY_REQUIRED", "graceful shutdown timed out; no hard power-off attempted")
			case <-tick.C:
				state, _, e := d.GetState()
				if e != nil {
					return e
				}
				if state == native.DOMAIN_SHUTOFF {
					return nil
				}
			}
		}
	case "hard-stop":
		return d.Destroy()
	case "pause":
		return d.Suspend()
	case "resume":
		return d.Resume()
	case "save":
		return d.ManagedSave(0)
	case "autostart":
		v, ok := input["enabled"].(bool)
		if !ok {
			return errors.New("enabled must be boolean")
		}
		return d.SetAutostart(v)
	case "set":
		x, ok := input["xml"].(string)
		if !ok {
			return errors.New("validated XML missing")
		}
		state, _, e := d.GetState()
		if e != nil {
			return e
		}
		if state != native.DOMAIN_SHUTOFF {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "powered-off edit required")
		}
		defined, e := c.DomainDefineXMLFlags(x, native.DOMAIN_DEFINE_VALIDATE)
		if e != nil {
			return e
		}
		return defined.Free()
	default:
		return domain.Fail("NOT_IMPLEMENTED", "no native execution adapter for "+action)
	}
}
