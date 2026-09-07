//go:build linux && amd64

package helper

import (
	"context"
	"errors"
	"testing"
	"virmill.local/core/internal/domain"
)

type auxiliaryDispatchBackend struct {
	nativeCalls, accessCalls int
	nativeError              error
}

func (b *auxiliaryDispatchBackend) InspectManagedFileVolume(context.Context, string, string, string) (domain.ManagedFileVolume, error) {
	b.accessCalls++
	return domain.ManagedFileVolume{}, errors.New("ACL backend must not handle auxiliary requests")
}
func (b *auxiliaryDispatchBackend) InspectColdState(context.Context, string, string) (domain.ColdStateInspection, error) {
	b.nativeCalls++
	return domain.ColdStateInspection{}, b.nativeError
}

func TestAuxiliaryDispatchCannotCaptureOrFallThroughToACL(t *testing.T) {
	for _, mode := range []string{"capture", "observe", "apply", ""} {
		t.Run(mode, func(t *testing.T) {
			r, p, _, _ := auxiliaryAuthFixture(t, "capture")
			r.Mode = mode
			backend := &auxiliaryDispatchBackend{}
			out, err := inspectAuxiliaryRequest(context.Background(), backend, r, p)
			var typed *domain.Error
			if out != nil || !errors.As(err, &typed) || typed.Code != "UNSUPPORTED_CAPABILITY" || backend.nativeCalls != 0 || backend.accessCalls != 0 {
				t.Fatal("unimplemented mode performed work", out, err, backend)
			}
		})
	}
}
func TestAuxiliaryDispatchNativeFailureAndCancellationReturnNoInventory(t *testing.T) {
	r, p, _, _ := auxiliaryAuthFixture(t, "inspect")
	sentinel := errors.New("fixture native connection unavailable")
	backend := &auxiliaryDispatchBackend{nativeError: sentinel}
	if out, err := inspectAuxiliaryRequest(context.Background(), backend, r, p); out != nil || !errors.Is(err, sentinel) || backend.nativeCalls != 1 || backend.accessCalls != 0 {
		t.Fatal("native error not preserved", out, err, backend)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := inspectAuxiliaryRequest(ctx, backend, r, p); out != nil || !errors.Is(err, context.Canceled) || backend.nativeCalls != 1 {
		t.Fatal("canceled dispatch reached native observer", out, err)
	}
	if out, err := inspectAuxiliaryRequest(context.Background(), nil, r, p); out != nil || err == nil {
		t.Fatal("missing native adapter returned inventory")
	}
}

func TestAuxiliaryCannotUseLegacyClient(t *testing.T) {
	r, _, _, _ := auxiliaryAuthFixture(t, "inspect")
	if _, err := (Client{KeyPath: "must-not-be-opened"}).Call(context.Background(), r); err == nil {
		t.Fatal("auxiliary operation used legacy client")
	}
	r.Operation = "storage.grant-read"
	if _, err := (Client{KeyPath: "must-not-be-opened"}).InspectAuxiliary(context.Background(), r); err == nil {
		t.Fatal("ACL operation used auxiliary client")
	}
}
