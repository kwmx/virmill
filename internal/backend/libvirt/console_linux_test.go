//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

const consoleTestID = "c066da96-1b31-45fe-9333-04212eaa5234"

func consoleTestVM(devices string) domain.VM {
	xml := `<domain type="kvm"><name>Guest</name><uuid>` + consoleTestID + `</uuid><metadata><a:data xmlns:a="urn:external">keep</a:data></metadata><devices>` + devices + `</devices></domain>`
	return domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: consoleTestID}, Name: "Guest", State: "running", LiveXML: xml, Fingerprint: strings.Repeat("a", 64)}
}
func TestConsoleDiscoveryPrivateChoices(t *testing.T) {
	v := consoleTestVM(`<console type="pty"><source path="/dev/pts/22"/><target type="serial" port="0"/><alias name="serial0"/></console><graphics type="spice"><listen type="none"/></graphics>`)
	got, err := consoleFromVM(v)
	if err != nil {
		t.Fatal(err)
	}
	if got.Resource != v.Key || got.ConfigFingerprint != v.Fingerprint || len(got.Choices) != 2 || !got.Choices[0].Available || got.Choices[0].Device != "serial0" || !got.Choices[1].Available {
		t.Fatalf("wrong observed choices: %+v", got)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "/dev/pts") || strings.Contains(string(b), "persistentXML") || strings.Contains(string(b), "liveXML") {
		t.Fatal("transport path or source XML disclosed")
	}
}
func TestConsoleDiscoveryPrivateGraphics(t *testing.T) {
	cases := []struct {
		name, xml string
		safe      bool
	}{
		{"unix", `<graphics type="vnc"><listen type="socket" socket="/private/secret.sock"/></graphics>`, true},
		{"unallocated unix", `<graphics type="vnc"><listen type="socket"/></graphics>`, true},
		{"none", `<graphics type="spice"><listen type="none"/></graphics>`, true},
		{"ipv4", `<graphics type="vnc" listen="127.0.0.1" port="5900"><listen type="address" address="127.0.0.1"/></graphics>`, true},
		{"ipv6", `<graphics type="spice"><listen type="address" address="::1"/></graphics>`, true},
		{"legacy socket", `<graphics type="vnc" socket="/private/secret.sock"/>`, true},
		{"websocket", `<graphics type="vnc" websocket="5700"><listen type="socket"/></graphics>`, false},
		{"default unknown", `<graphics type="vnc"/>`, false},
		{"wildcard", `<graphics type="vnc" listen="0.0.0.0"/>`, false},
		{"ipv6 wildcard", `<graphics type="vnc" listen="::"/>`, false},
		{"dns localhost", `<graphics type="vnc" listen="localhost"/>`, false},
		{"remote", `<graphics type="spice"><listen type="address" address="192.0.2.1"/></graphics>`, false},
		{"network", `<graphics type="vnc"><listen type="network" network="default" address="127.0.0.1"/></graphics>`, false},
		{"mixed remote", `<graphics type="vnc"><listen type="socket"/><listen type="address" address="192.0.2.1"/></graphics>`, false},
		{"mixed wildcard", `<graphics type="vnc" listen="0.0.0.0"><listen type="socket"/></graphics>`, false},
		{"relative socket", `<graphics type="vnc" socket="../console"/>`, false},
		{"other backend", `<graphics type="sdl" display=":0"/>`, false},
		{"foreign listen", `<graphics type="vnc"><listen type="socket"/><a:listen xmlns:a="x" type="address" address="192.0.2.1"/></graphics>`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := consoleFromVM(consoleTestVM(tc.xml))
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Choices) != 1 || got.Choices[0].Available != tc.safe {
				t.Fatalf("%+v", got)
			}
			b, _ := json.Marshal(got)
			if strings.Contains(string(b), "secret.sock") || strings.Contains(string(b), "192.0.2.1") {
				t.Fatal("endpoint disclosed")
			}
		})
	}
}
func TestConsoleDiscoverySerialAndAvailability(t *testing.T) {
	for _, tc := range []struct {
		name, device, state string
		want                bool
	}{
		{"running serial", `<console type="pty"><target type="serial"/><alias name="serial0"/></console>`, "running", true},
		{"virtio", `<console type="pty"><target type="virtio"/><alias name="console0"/></console>`, "running", true},
		{"missing alias", `<console type="pty"><target type="serial"/></console>`, "running", false},
		{"unsafe alias", `<console type="pty"><target type="serial"/><alias name="--force"/></console>`, "running", false},
		{"tcp console", `<console type="tcp"><target type="serial"/><alias name="serial0"/></console>`, "running", false},
		{"stopped", `<console type="pty"><target type="serial"/></console>`, "stopped", false},
		{"paused", `<console type="pty"><target type="serial"/><alias name="serial0"/></console>`, "paused", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := consoleTestVM(tc.device)
			v.State = tc.state
			if v.State == "stopped" {
				v.PersistentXML = v.LiveXML
				v.LiveXML = ""
			}
			got, err := consoleFromVM(v)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Choices) != 1 || got.Choices[0].Available != tc.want {
				t.Fatalf("%+v", got)
			}
		})
	}
	v := consoleTestVM(`<graphics type="vnc"><listen type="socket"/></graphics><graphics type="spice"><listen type="none"/></graphics>`)
	got, err := consoleFromVM(v)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got.Choices {
		if c.Available {
			t.Fatal("ambiguous viewer selection enabled")
		}
	}
	got, err = consoleFromVM(consoleTestVM(""))
	if err != nil || len(got.Choices) != 0 || len(got.Warnings) != 1 {
		t.Fatalf("empty console reporting %+v %v", got, err)
	}
}
func TestConsoleDiscoveryRejectsMalformedXML(t *testing.T) {
	good := consoleTestVM(`<graphics type="vnc"><listen type="socket"/></graphics>`)
	for _, raw := range []string{
		"", "<domain", `<domain/><domain/>`, `<!DOCTYPE domain><domain/>`, `<domain><uuid>` + consoleTestID + `</uuid><uuid>` + consoleTestID + `</uuid><devices/></domain>`,
		strings.Replace(good.LiveXML, `type="vnc"`, `type="vnc" type="spice"`, 1),
		strings.Replace(good.LiveXML, `<devices>`, `<devices/><devices>`, 1),
		strings.Replace(good.LiveXML, consoleTestID, "c066da96-1b31-45fe-9333-04212eaa5235", 1),
		strings.Replace(good.LiveXML, `<graphics type="vnc">`, `<graphics type="vnc" xmlns="urn:foreign">`, 1),
		strings.Repeat("x", consoleXMLLimit+1),
		`<domain>` + strings.Repeat("<x>", 65) + strings.Repeat("</x>", 65) + `</domain>`,
		strings.Replace(good.LiveXML, `</devices>`, `<?execute anything?></devices>`, 1),
		strings.Replace(good.LiveXML, `<name>Guest</name>`, `<name>Other</name>`, 1),
		consoleTestVM(`<console type="pty"><target type="serial"/><alias name="serial0"/></console><console type="pty"><target type="serial"/><alias name="serial0"/></console>`).LiveXML,
	} {
		v := good
		v.LiveXML = raw
		if _, err := consoleFromVM(v); err == nil {
			t.Fatalf("accepted invalid XML %.100q", raw)
		}
	}
}
func TestConsoleDiscoveryReadOnlyBoundary(t *testing.T) {
	calls := 0
	get := func(ctx context.Context, uri, id string) (domain.VM, error) { calls++; return consoleTestVM(""), nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		ctx     context.Context
		uri, id string
	}{{ctx, "qemu:///system", consoleTestID}, {context.Background(), "qemu+ssh://other/system", consoleTestID}, {context.Background(), "qemu:///system", "--anything"}} {
		if _, err := inspectConsole(tc.ctx, tc.uri, tc.id, get); err == nil {
			t.Fatal("invalid input accepted")
		}
	}
	if calls != 0 {
		t.Fatal("read invoked before validation")
	}
	if _, err := inspectConsole(context.Background(), "qemu:///system", consoleTestID, get); err != nil || calls != 1 {
		t.Fatalf("normal observation %v calls=%d", err, calls)
	}
	get = func(context.Context, string, string) (domain.VM, error) {
		return domain.VM{}, errors.New("secret path")
	}
	if _, err := inspectConsole(context.Background(), "qemu:///system", consoleTestID, get); err == nil || strings.Contains(err.Error(), "secret path") {
		t.Fatalf("native error not contained: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	get = func(context.Context, string, string) (domain.VM, error) { cancel(); return consoleTestVM(""), nil }
	if _, err := inspectConsole(ctx, "qemu:///system", consoleTestID, get); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-read cancellation: %v", err)
	}
	get = func(context.Context, string, string) (domain.VM, error) {
		v := consoleTestVM("")
		v.Key.UUID = "c066da96-1b31-45fe-9333-04212eaa5235"
		return v, nil
	}
	if _, err := inspectConsole(context.Background(), "qemu:///system", consoleTestID, get); err == nil {
		t.Fatal("wrong resource accepted")
	}
}

func TestConsoleDiscoveryRefusesViewerPowerControl(t *testing.T) {
	for _, channel := range []string{`<channel type="spiceport"><target type="virtio" name="org.qemu.monitor.qmp.0"/></channel>`, `<channel type="spiceport"><target type="virtio" name="org.qemu.monitor.hmp.0"/></channel>`} {
		info, err := consoleFromVM(consoleTestVM(channel + `<graphics type="spice"><listen type="none"/></graphics>`))
		if err != nil || len(info.Choices) != 1 || info.Choices[0].Available {
			t.Fatal("viewer could control VM power", info, err)
		}
	}
	vm := consoleTestVM(`<graphics type="spice"><listen type="none"/></graphics>`)
	vm.LiveXML = strings.Replace(vm.LiveXML, "</domain>", `<q:commandline xmlns:q="http://libvirt.org/schemas/domain/qemu/1.0"><q:arg value="-spice"/></q:commandline></domain>`, 1)
	info, err := consoleFromVM(vm)
	if err != nil || info.Choices[0].Available {
		t.Fatal("opaque QEMU arguments accepted", info, err)
	}
}
