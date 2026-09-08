//go:build !linux

package networksettings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"virmill.local/core/internal/app/network"
	"virmill.local/core/internal/domain"
)

func TestExistingSettingsExplicitlyUnsupported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(context.Background(), dir)
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != "UNSUPPORTED_CAPABILITY" || !reflect.DeepEqual(got, network.AllocationConfig{}) {
		t.Fatal("unsupported platform accepted settings", got, err)
	}
}
