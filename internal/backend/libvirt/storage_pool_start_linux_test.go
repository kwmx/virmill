//go:build linux && cgo

package libvirt

import (
	"context"
	"strings"
	"testing"
)

func TestStoragePoolStartStartsOnlyTheReviewedStoppedPool(t *testing.T) {
	ctx := context.Background()
	uri := "qemu:///system"
	c := &poolConnectionFixture{}
	def := poolFixtureDefinition()
	if _, err := storagePoolCreationRun(ctx, uri, def, "define", c.open); err != nil {
		t.Fatal(err)
	}
	observed, err := observeExistingStoragePool(c.pool, uri)
	if err != nil || observed.Active || observed.Type != "dir" || observed.Fingerprint == "" {
		t.Fatal("stopped pool observation", observed, err)
	}
	poolCreationCode(t, existingStoragePoolRun(ctx, uri, def.UUID, "reviewed-elsewhere", "start", c.open), "STALE_PLAN")
	poolCreationCode(t, existingStoragePoolRun(ctx, uri, def.UUID, "", "autostart", c.open), "STALE_PLAN")
	if c.pool.creates != 0 || c.pool.autostarts != 0 {
		t.Fatal("refused request reached native effects")
	}
	if err = existingStoragePoolRun(ctx, uri, def.UUID, observed.Fingerprint, "start", c.open); err != nil || !c.pool.active || c.pool.creates != 1 || c.pool.builds != 0 {
		t.Fatal("start failed, or built a folder", err)
	}
	poolCreationCode(t, existingStoragePoolRun(ctx, uri, def.UUID, observed.Fingerprint, "start", c.open), "STALE_PLAN")
	for range 2 {
		if err = existingStoragePoolRun(ctx, uri, def.UUID, "", "autostart", c.open); err != nil || !c.pool.autostart {
			t.Fatal("autostart", err)
		}
	}
	if c.pool.creates != 1 || c.pool.autostarts != 1 {
		t.Fatal("start or autostart replayed")
	}
	poolCreationCode(t, existingStoragePoolRun(ctx, uri, "not-a-uuid", "", "start", c.open), "INVALID_INPUT")
	c.pool.active = false
	c.pool.xml = strings.Replace(c.pool.xml, "type='dir'", "type='logical'", 1)
	logical, err := observeExistingStoragePool(c.pool, uri)
	if err != nil {
		t.Fatal(err)
	}
	poolCreationCode(t, existingStoragePoolRun(ctx, uri, def.UUID, logical.Fingerprint, "start", c.open), "UNSUPPORTED_CAPABILITY")
	if c.closes != len(c.writes) {
		t.Fatal("native connections leaked")
	}
}
