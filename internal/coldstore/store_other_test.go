//go:build !linux || !amd64

package coldstore

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/domain"
)

func TestUnsupportedPlatformNeverReturnsReceipt(t *testing.T) {
	for _, call := range []func(context.Context) (Receipt, error){
		func(ctx context.Context) (Receipt, error) {
			return Publish(ctx, "/unused", protection.CaptureManifest{}, nil)
		},
		func(ctx context.Context) (Receipt, error) { return Inspect(ctx, "/unused", "unused") },
	} {
		got, err := call(context.Background())
		var failure *domain.Error
		if !reflect.DeepEqual(got, Receipt{}) || !errors.As(err, &failure) || failure.Code != "UNSUPPORTED_CAPABILITY" {
			t.Fatal(got, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		got, err = call(ctx)
		if !reflect.DeepEqual(got, Receipt{}) || !errors.Is(err, context.Canceled) {
			t.Fatal(got, err)
		}
	}
}
