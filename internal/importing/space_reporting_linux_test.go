//go:build linux && amd64

package importing

import (
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

func TestImportSpaceErrorExplainsRequiredAvailableAndShortfall(t *testing.T) {
	err := checkAvailableBytes(150<<30, 100<<30)
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != "INSUFFICIENT_SPACE" {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, text := range []string{"Needs 150.00 GiB", "100.00 GiB available", "50.00 GiB short", "Choose another destination", "free at least 50.00 GiB"} {
		if !strings.Contains(failure.Message, text) {
			t.Errorf("missing helpful space detail %q: %s", text, failure.Message)
		}
	}
	details := failure.Details.(map[string]any)
	if details["requiredBytes"] != int64(150<<30) || details["availableBytes"] != uint64(100<<30) || details["shortfallBytes"] != uint64(50<<30) {
		t.Fatalf("lost exact byte counts: %#v", details)
	}
}

func TestImportSpaceReportingConservativeBounds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		required int64
		free     uint64
	}{
		{"equal", 1 << 30, 1 << 30}, {"empty", 0, 0}, {"large-free", math.MaxInt64, math.MaxUint64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkAvailableBytes(tc.required, tc.free); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := checkAvailableBytes(-1, math.MaxUint64); err == nil {
		t.Fatal("negative required space was accepted")
	}
	err := checkAvailableBytes(1<<30+1, 1<<30)
	if err == nil || !strings.Contains(err.Error(), "free at least 0.01 GiB") {
		t.Fatalf("small shortfall rounded to zero: %v", err)
	}
	for _, tc := range []struct {
		value uint64
		up    bool
		want  string
	}{
		{0, true, "0.00"}, {1, true, "0.01"}, {1, false, "0.00"},
		{1<<30 - 1, true, "1.00"}, {1<<30 - 1, false, "0.99"},
		{math.MaxUint64, true, "17179869184.00"}, {math.MaxUint64, false, "17179869183.99"},
	} {
		if got := spaceGiB(tc.value, tc.up); got != tc.want {
			t.Errorf("spaceGiB(%d,%v)=%q; want %q", tc.value, tc.up, got, tc.want)
		}
	}
}

func TestImportAvailableChecksActualTemporaryFilesystemAndErrors(t *testing.T) {
	dir, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := available(dir, 0); err != nil {
		dir.Close()
		t.Fatal(err)
	}
	dir.Close()
	if err := available(dir, 0); err == nil {
		t.Fatal("closed destination descriptor accepted")
	}
}
