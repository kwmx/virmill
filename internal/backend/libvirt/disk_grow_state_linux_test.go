//go:build linux && amd64 && cgo

package libvirt

import (
	"errors"
	"testing"

	"virmill.local/core/internal/domain"
)

func TestGrowStateTellsStalePlansFromChangedVolumes(t *testing.T) {
	reviewed := domain.RemovalDisk{Target: "sda", Path: "/pool/app.qcow2", PoolID: "0f1e2d3c-4b5a-4968-8776-655443322110", VolumeName: "app.qcow2", VolumeKey: "/pool/app.qcow2", Generation: "linux-statx-v1:8:1:42:1:000000000", Fingerprint: "a", Format: "qcow2", CapacityBytes: 20 << 30}
	resized := func(capacity uint64) domain.RemovalDisk {
		d := reviewed
		d.CapacityBytes, d.Fingerprint = capacity, "b"
		return d
	}
	other := reviewed
	other.Generation = "linux-statx-v1:8:1:99:1:000000000"
	for _, test := range []struct {
		name    string
		current domain.RemovalDisk
		state   string
		code    string
	}{
		{"unchanged", reviewed, "before", ""},
		{"grown as requested", resized(40 << 30), "grown", ""},
		{"grown by another plan", resized(30 << 30), "", "STALE_PLAN"},
		{"replaced volume", other, "", "SOURCE_CHANGED"},
	} {
		state, err := growState(test.current, reviewed, 40<<30)
		var de *domain.Error
		if state != test.state || (test.code == "") != (err == nil) || err != nil && (!errors.As(err, &de) || de.Code != test.code) {
			t.Fatal(test.name, state, err)
		}
	}
}
