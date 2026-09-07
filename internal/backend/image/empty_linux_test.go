//go:build linux && amd64

package image

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBlankDiskRequiresCompletePositiveZeroMap(t *testing.T) {
	valid := []byte(`[{"start":0,"length":1048576,"depth":0,"present":false,"zero":true,"data":false}]`)
	if err := checkZeroMap(valid, 1048576); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`[]`, `null`, `[{"start":0,"length":1048576,"depth":1,"zero":true}]`, `[{"start":0,"length":1048576,"zero":false}]`, `[{"start":512,"length":1048064,"zero":true}]`, `[{"start":0,"length":1048575,"zero":true}]`, `[{"start":0,"length":1048577,"zero":true}]`, `[{"start":0,"length":-1,"zero":true}]`, `[{"start":0,"length":1048576,"zero":true,"unexpected":true}]`} {
		if err := checkZeroMap([]byte(bad), 1048576); err == nil {
			t.Fatal("unproven zeros accepted", bad)
		}
	}
}
func TestRealConfinedEmptyDiskCreation(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("enable actual generated image fixture execution explicitly")
	}
	ctx := context.Background()
	tool := Tool{}
	for _, capacity := range []int64{1 << 20, 8 << 20, 8<<20 + 512} {
		work := privateTemp(t)
		if err := tool.CreateEmpty(ctx, work, capacity, 32<<20); err != nil {
			t.Fatal(err)
		}
		if err := tool.CreateEmpty(ctx, work, capacity, 32<<20); err == nil {
			t.Fatal("existing disk overwritten")
		}
		raw := filepath.Join(work, "zeros.raw")
		f, err := os.OpenFile(raw, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.Truncate(capacity); err != nil {
			t.Fatal(err)
		}
		f.Close()
		fixtureQEMU(t, "compare", "-f", "raw", "-F", "qcow2", raw, filepath.Join(work, "disk.qcow2"))
		t.Logf("actual confined create/check/map and independent QEMU comparison prove %d empty virtual bytes; no filesystem or guest executed", capacity)
	}
}
