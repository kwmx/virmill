//go:build linux && cgo

package libvirt

import (
	"context"
	"errors"
	"strings"
	"testing"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/poolxml"
	"virmill.local/core/internal/domain"
)

type poolCreationFixture struct {
	def                                domain.StoragePoolDefinition
	xml                                string
	active, autostart                  bool
	builds, creates, autostarts, frees int
	buildErr                           error
}

func (p *poolCreationFixture) GetUUIDString() (string, error) { return p.def.UUID, nil }
func (p *poolCreationFixture) GetName() (string, error)       { return p.def.Name, nil }
func (p *poolCreationFixture) IsActive() (bool, error)        { return p.active, nil }
func (p *poolCreationFixture) IsPersistent() (bool, error)    { return true, nil }
func (p *poolCreationFixture) GetAutostart() (bool, error)    { return p.autostart, nil }
func (p *poolCreationFixture) GetXMLDesc(native.StorageXMLFlags) (string, error) {
	return p.xml, nil
}
func (p *poolCreationFixture) Build(native.StoragePoolBuildFlags) error {
	p.builds++
	return p.buildErr
}
func (p *poolCreationFixture) Create(native.StoragePoolCreateFlags) error {
	p.creates++
	if p.active {
		return errors.New("replayed start")
	}
	p.active = true
	return nil
}
func (p *poolCreationFixture) SetAutostart(v bool) error {
	p.autostarts++
	p.autostart = v
	return nil
}
func (p *poolCreationFixture) Free() error { p.frees++; return nil }

type poolConnectionFixture struct {
	existing        []storagePoolIdentity
	pool            *poolCreationFixture
	defines, closes int
	writes          []bool
}

func (c *poolConnectionFixture) pools(context.Context) ([]storagePoolIdentity, error) {
	out := append([]storagePoolIdentity{}, c.existing...)
	if c.pool != nil {
		out = append(out, storagePoolIdentity{UUID: c.pool.def.UUID, Name: c.pool.def.Name, Path: c.pool.def.Path})
	}
	return out, nil
}
func (c *poolConnectionFixture) lookup(id string) (storagePoolCreationHandle, error) {
	if c.pool == nil || c.pool.def.UUID != id {
		return nil, errors.New("pool absent")
	}
	return c.pool, nil
}

// define reports XML the way libvirt does: with an empty source and the
// observed folder permissions added.
func (c *poolConnectionFixture) define(raw string) (storagePoolCreationHandle, error) {
	c.defines++
	o, err := poolxml.Parse(raw)
	if err != nil {
		return nil, err
	}
	reported := strings.Replace(raw, "  <target>", "  <source>\n  </source>\n  <target>", 1)
	reported = strings.Replace(reported, "</path>", "</path>\n    <permissions><mode>0711</mode></permissions>", 1)
	c.pool = &poolCreationFixture{def: domain.StoragePoolDefinition{UUID: o.UUID, Name: o.Name, Path: o.Path}, xml: reported}
	return c.pool, nil
}
func (c *poolConnectionFixture) close() error { c.closes++; return nil }
func (c *poolConnectionFixture) open(_ string, write bool) (storagePoolCreationConnection, error) {
	c.writes = append(c.writes, write)
	return c, nil
}

func poolFixtureDefinition() domain.StoragePoolDefinition {
	return domain.StoragePoolDefinition{UUID: "44444444-4444-4444-8444-444444444444", Name: "default", Path: "/var/lib/libvirt/images", Autostart: true}
}
func poolCreationCode(t *testing.T, err error, code string) {
	t.Helper()
	var e *domain.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("got %v, want %s", err, code)
	}
}

func TestStoragePoolCreationDefinesStartsAndEnablesAutostart(t *testing.T) {
	ctx := context.Background()
	c := &poolConnectionFixture{existing: []storagePoolIdentity{{UUID: "55555555-5555-4555-8555-555555555555", Name: "iso", Path: "/srv/iso"}}}
	def := poolFixtureDefinition()
	if _, err := storagePoolCreationRun(ctx, "qemu:///system", def, "check", c.open); err != nil || c.defines != 0 || c.writes[0] {
		t.Fatal("check must be read-only", err, c.writes)
	}
	out, err := storagePoolCreationRun(ctx, "qemu:///system", def, "define", c.open)
	if err != nil || c.defines != 1 || out.Active || !out.Persistent || out.Type != "dir" {
		t.Fatal("define failed", err, out)
	}
	_, err = storagePoolCreationRun(ctx, "qemu:///system", def, "define", c.open)
	poolCreationCode(t, err, "INVALID_STATE")
	if c.defines != 1 {
		t.Fatal("definition was replayed")
	}
	if _, err = storagePoolCreationRun(ctx, "qemu:///system", def, "autostart", c.open); err == nil || c.pool.autostarts != 0 {
		t.Fatal("autostart before start must be refused", err)
	}
	out, err = storagePoolCreationRun(ctx, "qemu:///system", def, "start", c.open)
	if err != nil || !out.Active || c.pool.builds != 1 || c.pool.creates != 1 || out.State != "running" {
		t.Fatal("start failed", err, out)
	}
	_, err = storagePoolCreationRun(ctx, "qemu:///system", def, "start", c.open)
	poolCreationCode(t, err, "STALE_PLAN")
	if c.pool.creates != 1 || c.pool.builds != 1 {
		t.Fatal("start was replayed")
	}
	for range 2 {
		if out, err = storagePoolCreationRun(ctx, "qemu:///system", def, "autostart", c.open); err != nil || !out.Autostart {
			t.Fatal("autostart failed", err)
		}
	}
	if c.pool.autostarts != 1 {
		t.Fatal("enabled autostart was set again")
	}
	if out, err = storagePoolCreationRun(ctx, "qemu:///system", def, "inspect", c.open); err != nil || !out.Active || !out.Autostart || out.Key.UUID != def.UUID {
		t.Fatal("inspect failed", err, out)
	}
	if c.closes != len(c.writes) || c.pool.frees == 0 {
		t.Fatal("native handles leaked")
	}
}

func TestStoragePoolCreationRefusesNameUUIDAndFolderCollisions(t *testing.T) {
	ctx := context.Background()
	def := poolFixtureDefinition()
	for _, existing := range []storagePoolIdentity{
		{UUID: "55555555-5555-4555-8555-555555555555", Name: "default", Path: "/srv/other"},
		{UUID: def.UUID, Name: "other", Path: "/srv/other"},
		{UUID: "55555555-5555-4555-8555-555555555555", Name: "images", Path: "/var/lib/libvirt/images"},
		{UUID: "55555555-5555-4555-8555-555555555555", Name: "parent", Path: "/var/lib/libvirt"},
		{UUID: "55555555-5555-4555-8555-555555555555", Name: "child", Path: "/var/lib/libvirt/images/iso"},
	} {
		c := &poolConnectionFixture{existing: []storagePoolIdentity{existing}}
		_, err := storagePoolCreationRun(ctx, "qemu:///system", def, "define", c.open)
		poolCreationCode(t, err, "INVALID_STATE")
		if c.defines != 0 {
			t.Fatal("collision was defined", existing)
		}
	}
	c := &poolConnectionFixture{existing: []storagePoolIdentity{{UUID: "55555555-5555-4555-8555-555555555555", Name: "a"}, {UUID: "66666666-6666-4666-8666-666666666666", Name: "a"}}}
	_, err := storagePoolCreationRun(ctx, "qemu:///system", def, "check", c.open)
	poolCreationCode(t, err, "SOURCE_CHANGED")
	if _, err = storagePoolCreationRun(ctx, "qemu+ssh://host/system", def, "check", c.open); err == nil {
		t.Fatal("remote connection accepted")
	}
	bad := def
	bad.Path = "/etc/vms"
	_, err = storagePoolCreationRun(ctx, "qemu:///system", bad, "check", c.open)
	poolCreationCode(t, err, "INVALID_INPUT")
}

func TestStoragePoolCreationReportsFolderFailureAndDrift(t *testing.T) {
	ctx := context.Background()
	def := poolFixtureDefinition()
	c := &poolConnectionFixture{}
	if _, err := storagePoolCreationRun(ctx, "qemu:///system", def, "define", c.open); err != nil {
		t.Fatal(err)
	}
	c.pool.buildErr = errors.New("cannot create directory: Permission denied")
	_, err := storagePoolCreationRun(ctx, "qemu:///system", def, "start", c.open)
	poolCreationCode(t, err, "RECOVERY_REQUIRED")
	if !strings.Contains(err.Error(), "could not prepare the folder") || !strings.Contains(err.Error(), "Permission denied") || c.pool.creates != 0 {
		t.Fatal("folder failure not explained or start continued", err)
	}
	c.pool.xml = strings.Replace(c.pool.xml, "/var/lib/libvirt/images", "/srv/elsewhere", 1)
	_, err = storagePoolCreationRun(ctx, "qemu:///system", def, "inspect", c.open)
	poolCreationCode(t, err, "SOURCE_CHANGED")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = storagePoolCreationRun(canceled, "qemu:///system", def, "start", c.open); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled request reached native code", err)
	}
}
