//go:build linux && cgo

package libvirt

import (
	"strings"
	"testing"
)

func TestColdSourcePrivateSpiceHasNoUncapturedEndpoint(t *testing.T) {
	graphics := `<graphics type="spice"><listen type="socket"/><clipboard copypaste="no"/><filetransfer enable="no"/></graphics>`
	for _, tc := range []struct {
		name, from, to string
		safe           bool
	}{
		{"exact generated", "", "", true},
		{"runtime socket", `type="socket"`, `type="socket" socket="/tmp/runtime.sock"`, false},
		{"credentials", `type="spice"`, `type="spice" passwd="secret"`, false},
		{"remote listener", `type="socket"`, `type="address" address="0.0.0.0"`, false},
		{"clipboard enabled", `copypaste="no"`, `copypaste="yes"`, false},
		{"transfer enabled", `enable="no"`, `enable="yes"`, false},
		{"omitted restriction", `<clipboard copypaste="no"/>`, "", false},
		{"duplicate attribute", `copypaste="no"`, `copypaste="no" copypaste="yes"`, false},
		{"extra child", `</graphics>`, `<channel name="main" mode="insecure"/></graphics>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := graphics
			if tc.from != "" {
				raw = strings.Replace(raw, tc.from, tc.to, 1)
			}
			layout, err := InspectColdSourceXML(coldSourceTestDomain(coldSourceTestDisk + raw))
			if err != nil {
				if !tc.safe {
					return
				} // Malformed XML is refused before dependency projection.
				t.Fatal(err)
			}
			if (len(layout.External) == 0) != tc.safe {
				t.Fatalf("uncaptured dependencies=%v", layout.External)
			}
			if len(layout.Disks) != 1 || layout.Disks[0].Source.File != "/fixture/leaf.qcow2" {
				t.Fatal("source disk lost")
			}
		})
	}
}
