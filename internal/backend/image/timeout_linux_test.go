//go:build linux && amd64

package image

import (
	"testing"
	"time"
)

// owner-ova-native-003: a 34 GiB disk written at about 18 MiB/s outlasted the
// flat 30-minute worker limit. The limit now grows with the allowed output.
func TestWorkerTimeoutGrowsWithTheAllowedOutput(t *testing.T) {
	if got := workerTimeout(64 << 20); got != 30*time.Minute+16*time.Second {
		t.Fatalf("inspection limit %v", got)
	}
	// The owner's appliance: a measured 34 GiB output budget.
	budget := OutputBudget(34<<30, 120<<30)
	if got := workerTimeout(budget); got < 2*time.Hour+30*time.Minute || got > 3*time.Hour+30*time.Minute {
		t.Fatalf("large conversion limit %v", got)
	}
}
