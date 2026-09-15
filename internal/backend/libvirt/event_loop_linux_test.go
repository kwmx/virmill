//go:build linux && cgo

package libvirt

import (
	"testing"

	native "libvirt.org/go/libvirt"
)

// With libvirt's default event implementation registered, a closed
// connection's socket is released only by a later loop iteration. The loop
// must run from process start, not from the first reboot: on the test host
// every read-only call left one socket open until then (83 after 36 minutes).
func TestEventLoopRunsFromStartSoClosedConnectionsAreReleased(t *testing.T) {
	if rebootEvents.initialization != nil {
		t.Skipf("no native event implementation: %v", rebootEvents.initialization)
	}
	if !rebootEvents.running.Load() {
		t.Fatal("event loop is registered but not running; closed connections would keep their sockets")
	}
	select {
	case <-rebootEvents.failed:
		t.Fatal("event loop stopped")
	default:
	}
	// Connections still open and close normally with the loop running.
	for range 3 {
		c, err := native.NewConnectReadOnly("test:///default")
		if err != nil {
			t.Fatal(err)
		}
		if refs, err := c.Close(); err != nil || refs != 0 {
			t.Fatalf("close left %d references: %v", refs, err)
		}
	}
}
