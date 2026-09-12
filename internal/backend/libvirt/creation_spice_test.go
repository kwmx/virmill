//go:build linux && cgo

package libvirt

import (
	"slices"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

const creationSpiceGraphics = `<graphics type="spice"><listen type="socket"/><clipboard copypaste="no"/><filetransfer enable="no"/></graphics>`

func spiceCreationFixture(t *testing.T) (domain.CreationTarget, []domain.CreatedVolume) {
	t.Helper()
	target, volumes := creationFixture()
	policy, err := domain.DefaultCreationDevices(target.Spec.Machine)
	if err != nil {
		t.Fatal(err)
	}
	target.Spec.DevicePolicy = policy
	target.Spec.Graphics = "spice-unix"
	return target, volumes
}

func TestCreationSpiceRequiresExplicitPrivateProfileAndDevicePolicy(t *testing.T) {
	target, volumes := spiceCreationFixture(t)
	if err := validateCreationSpec(target.Spec); err != nil {
		t.Fatal(err)
	}
	wanted, err := creationXML(target, volumes, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(wanted, creationSpiceGraphics) || !strings.Contains(wanted, `<video><model type="vga"/></video>`) || !strings.Contains(wanted, `<audio id="1" type="none"/>`) {
		t.Fatal("private display or disabled features absent", wanted)
	}
	for _, forbidden := range []string{`<channel`, `<redirdev`, `<sound`, `spicevmc`, `spiceport`, `listen="`, `autoport="yes"`, `copypaste="yes"`, `enable="yes"`} {
		if strings.Contains(wanted, forbidden) {
			t.Fatal("unrequested SPICE integration added", forbidden)
		}
	}
	if err := matchesCreationPolicy(wanted, wanted, target.Spec.DevicePolicy); err != nil {
		t.Fatal(err)
	}
	target.Spec.DevicePolicy = nil
	if err := validateCreationSpec(target.Spec); err == nil {
		t.Fatal("SPICE accepted without explicit device policy")
	}
	if _, err := creationXML(target, volumes, "binding"); err == nil {
		t.Fatal("SPICE rendered with implicit devices")
	}
	for _, graphics := range []string{"spice", "spice-tcp", "spice-anywhere"} {
		target.Spec.Graphics = graphics
		if err := validateCreationSpec(target.Spec); err == nil {
			t.Fatal("ambiguous graphics profile accepted", graphics)
		}
	}
}

func TestCreationSpiceChangesOnlyExplicitGraphicsSelection(t *testing.T) {
	target, volumes := spiceCreationFixture(t)
	spice, err := creationXML(target, volumes, "binding")
	if err != nil {
		t.Fatal(err)
	}
	target.Spec.Graphics = "vnc-unix"
	vnc, err := creationXML(target, volumes, "binding")
	if err != nil {
		t.Fatal(err)
	}
	oldGraphics := `<graphics type="vnc"><listen type="socket"/></graphics>`
	if strings.Replace(spice, creationSpiceGraphics, oldGraphics, 1) != vnc {
		t.Fatal("SPICE selection changed unrelated device intent")
	}
	target.Spec.Graphics = "none"
	none, err := creationXML(target, volumes, "binding")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Replace(vnc, oldGraphics+`<video><model type="vga"/></video>`, "", 1) != none {
		t.Fatal("none/VNC rendering semantics changed")
	}
	for _, graphics := range []string{"none", "vnc-unix"} {
		target.Spec.Graphics = graphics
		target.Spec.DevicePolicy = nil
		if err := validateCreationSpec(target.Spec); err != nil {
			t.Fatal("legacy declaration acquired new requirement", graphics, err)
		}
		xml, err := creationXML(target, volumes, "binding")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(xml, "spice") || strings.Contains(xml, "clipboard") || strings.Contains(xml, "filetransfer") {
			t.Fatal("legacy output acquired SPICE semantics")
		}
	}
}

func TestCreationSpiceCapabilitiesMustBePositivelyObservedAndRechecked(t *testing.T) {
	for _, test := range []struct {
		name, supported string
		values          []string
		want            bool
	}{
		{"not advertised", "yes", []string{"vnc"}, false},
		{"disabled", "no", []string{"vnc", "spice"}, false},
		{"unknown support", "", []string{"spice"}, false},
		{"explicit support", "yes", []string{"vnc", "spice"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			caps, _ := optionsFixture(t)
			caps.Devices.Graphics = capDevice{Supported: test.supported, Enums: []capEnum{{Name: "type", Values: test.values}}}
			options, err := creationOptionsFromCaps(caps, []string{caps.Machine}, 8192, nil)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(options.Graphics, "spice-unix") != test.want {
				t.Fatal("incorrect advertised SPICE choices", options.Graphics)
			}
			spec := domain.CreationSpec{Architecture: caps.Arch, Machine: caps.Machine, VCPUs: 2, CPU: domain.CreationCPU{Mode: "host-model"}, Firmware: domain.CreationFirmware{Mode: "bios"}, Graphics: "spice-unix"}
			if err := checkCaps(caps, spec); (err == nil) != test.want {
				t.Fatal("preflight capability mismatch", err)
			}
			if test.want {
				caps.Devices.Graphics.Enums = nil
				if err := checkCaps(caps, spec); err == nil {
					t.Fatal("previous advertisement substituted for fresh preflight")
				}
			}
		})
	}
}

func TestCreationSpiceReadbackRefusesExposureAndImplicitFeatures(t *testing.T) {
	target, volumes := spiceCreationFixture(t)
	wanted, err := creationXML(target, volumes, "binding")
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string][2]string{
		"TCP wildcard":                      {`<listen type="socket"/>`, `<listen type="address" address="0.0.0.0"/>`},
		"TCP loopback":                      {`<listen type="socket"/>`, `<listen type="address" address="127.0.0.1"/>`},
		"explicit unreviewed socket":        {`<listen type="socket"/>`, `<listen type="socket" socket="/tmp/unreviewed.sock"/>`},
		"additional listener":               {`<listen type="socket"/>`, `<listen type="socket"/><listen type="address" address="0.0.0.0"/>`},
		"TCP port":                          {`<graphics type="spice">`, `<graphics type="spice" port="5900">`},
		"TLS port":                          {`<graphics type="spice">`, `<graphics type="spice" tlsPort="5901">`},
		"auto TCP":                          {`<graphics type="spice">`, `<graphics type="spice" autoport="yes">`},
		"clipboard":                         {`copypaste="no"`, `copypaste="yes"`},
		"file transfer":                     {`<filetransfer enable="no"/>`, `<filetransfer enable="yes"/>`},
		"omitted clipboard restriction":     {`<clipboard copypaste="no"/>`, ""},
		"omitted file transfer restriction": {`<filetransfer enable="no"/>`, ""},
		"audio":                             {`<audio id="1" type="none"/>`, `<audio id="1" type="spice"/>`},
		"implicit sound device":             {`</devices>`, `<sound model="ich9"/></devices>`},
		"redirection":                       {`</devices>`, `<redirdev bus="usb" type="spicevmc"/></devices>`},
		"vdagent channel":                   {`</devices>`, `<channel type="spicevmc"><target type="virtio" name="com.redhat.spice.0"/></channel></devices>`},
		"control channel":                   {`</devices>`, `<channel type="spiceport"><target type="virtio" name="org.spice-space.webdav.0"/></channel></devices>`},
		"extra graphics":                    {`</devices>`, creationSpiceGraphics + `</devices>`},
		"wrong video model":                 {`<model type="vga"/>`, `<model type="qxl"/>`},
		"duplicate graphics attribute":      {`<graphics type="spice">`, `<graphics type="spice" type="spice">`},
		"duplicate clipboard attribute":     {`copypaste="no"`, `copypaste="no" copypaste="yes"`},
		"namespace graphics attribute":      {`<graphics type="spice">`, `<graphics xmlns:x="urn:unknown" type="spice" x:port="5900">`},
		"namespace clipboard attribute":     {`copypaste="no"`, `xmlns:x="urn:unknown" copypaste="no" x:copypaste="yes"`},
		"namespace child":                   {`<clipboard copypaste="no"/>`, `<clipboard xmlns="urn:unknown" copypaste="no"/>`},
		"duplicate clipboard child":         {`<clipboard copypaste="no"/>`, `<clipboard copypaste="no"/><clipboard copypaste="yes"/>`},
	} {
		t.Run(name, func(t *testing.T) {
			observed := strings.Replace(wanted, change[0], change[1], 1)
			if observed == wanted {
				t.Fatal("fixture mutation not applied")
			}
			if err := matchesCreationPolicy(wanted, observed, target.Spec.DevicePolicy); err == nil {
				t.Fatal("unreviewed SPICE semantics certified")
			}
		})
	}
}

func TestCreationSpiceSharedShapeRequiresExactRoot(t *testing.T) {
	if privateSpiceSocketGraphics(nil) {
		t.Fatal("nil graphics accepted")
	}
	root, err := xmlTree(creationSpiceGraphics)
	if err != nil || !privateSpiceSocketGraphics(root) {
		t.Fatal("generated shape rejected", err)
	}
	for _, raw := range []string{
		strings.ReplaceAll(creationSpiceGraphics, "graphics", "other"),
		strings.Replace(creationSpiceGraphics, `type="spice"`, `xmlns="urn:other" type="spice"`, 1),
		strings.Replace(creationSpiceGraphics, `<listen type="socket"/>`, `<listen type="socket" socket="/tmp/runtime.sock"/>`, 1),
		strings.Replace(creationSpiceGraphics, `type="spice"`, `type="spice" passwd="credential"`, 1),
	} {
		root, err := xmlTree(raw)
		if err != nil {
			t.Fatal(err)
		}
		if privateSpiceSocketGraphics(root) {
			t.Fatal("unsafe or unrelated shared shape accepted", raw)
		}
	}
}
