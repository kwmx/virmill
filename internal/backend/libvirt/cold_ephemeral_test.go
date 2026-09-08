//go:build linux && cgo

package libvirt

import "testing"

func TestColdCaptureEphemeralDevicesHaveNoPersistentSource(t *testing.T) {
	for _, raw := range []string{
		`<emulator>/usr/bin/qemu-system-x86_64</emulator>`,
		`<serial type='pty'><target type='isa-serial' port='0'><model name='isa-serial'/></target></serial>`,
		`<console type='pty'><target type='serial' port='0'/></console>`,
		`<graphics type='vnc'><listen type='socket'/></graphics>`,
	} {
		n, err := xmlTree(raw)
		if err != nil || !coldSourceEphemeralDevice(n) {
			t.Fatalf("known configuration refused: %s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`<emulator>/tmp/qemu-system-x86_64</emulator>`,
		`<emulator custom='yes'>/usr/bin/qemu-system-x86_64</emulator>`,
		`<serial type='pty'><source path='/dev/pts/3'/><target type='isa-serial' port='0'><model name='isa-serial'/></target></serial>`,
		`<console type='file'><source path='/tmp/log'/></console>`,
		`<graphics type='vnc' passwd='secret'><listen type='socket'/></graphics>`,
		`<graphics type='vnc'><listen type='socket' socket='/tmp/shared'/></graphics>`,
		`<graphics type='vnc'><listen type='address' address='0.0.0.0'/></graphics>`,
		`<graphics type='vnc'><listen type='socket'/><other/></graphics>`,
	} {
		n, err := xmlTree(raw)
		if err != nil {
			t.Fatal(err)
		}
		if coldSourceEphemeralDevice(n) {
			t.Fatalf("persistent/custom dependency classified ephemeral: %s", raw)
		}
	}
}
