package networksettings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"virmill.local/core/internal/app/network"
)

func TestMissingSettingsUseIndependentDefaultsWithoutWrites(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{root, filepath.Join(root, "absent-directory")} {
		got, err := Load(context.Background(), dir)
		if err != nil || !reflect.DeepEqual(got, network.DefaultAllocationConfig()) {
			t.Fatalf("missing settings: %#v, %v", got, err)
		}
		if got.Version != 1 || len(got.Ranges) != 3 {
			t.Fatalf("unexpected defaults: %#v", got)
		}
		for i, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
			if got.Ranges[i].CIDR != cidr || got.Ranges[i].PrefixLength != 24 {
				t.Fatalf("unexpected default range: %#v", got.Ranges[i])
			}
		}
		got.Ranges[0].CIDR = "changed by caller"
		again, err := Load(context.Background(), dir)
		if err != nil || again.Ranges[0].CIDR != "10.0.0.0/8" {
			t.Fatal("default settings share caller-mutable state", again, err)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("loader created config files or directories", entries, err)
	}
}

func TestSettingsCancellationDoesNotBecomeDefaults(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := Load(ctx, t.TempDir())
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, network.AllocationConfig{}) {
		t.Fatal("canceled read returned settings", got, err)
	}
	got, err = Load(context.Background(), "")
	if err == nil || !reflect.DeepEqual(got, network.AllocationConfig{}) {
		t.Fatal("empty directory silently chose working-directory defaults", got, err)
	}
}
