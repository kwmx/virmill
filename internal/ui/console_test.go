//go:build linux

package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

const consoleTestUUID = "108708af-93db-402c-bb03-322845296fa4"

type consoleTestClient struct {
	info          domain.ConsoleInfo
	responseError *domain.Error
	err           error
	calls         int
}

func (c *consoleTestClient) Call(_ context.Context, method string, request app.Request) (app.Response, error) {
	c.calls++
	if method != "vm.console.show" || request.Connection != "qemu:///system" || request.ID != consoleTestUUID {
		panic("unexpected console request")
	}
	return app.Response{Data: c.info, Error: c.responseError}, c.err
}
func consoleFixture() (*consoleTestClient, consoleEnvironment) {
	return &consoleTestClient{info: domain.ConsoleInfo{
		Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: consoleTestUUID}, State: "running", ConfigFingerprint: strings.Repeat("a", 64),
		Choices: []domain.ConsoleChoice{{ID: "serial:0", Kind: "serial", Protocol: "serial", Device: "serial0", Available: true}},
	}}, consoleEnvironment{uid: 1000, environ: []string{"HOME=/home/test", "TERM=xterm", "DISPLAY=:0", "LD_PRELOAD=/tmp/bad.so", "SPICE_PROXY=http://bad", "XDG_CONFIG_HOME=/tmp/old"}, executable: func(string) error { return nil }, version: func(context.Context, string, []string) (string, error) { return "virt-viewer version 11.0\n", nil }}
}
func TestConsoleSerialRechecksAndBuildsFixedArguments(t *testing.T) {
	c, e := consoleFixture()
	session, err := prepareConsole(context.Background(), c, "qemu:///system", consoleTestUUID, "serial:0", strings.Repeat("a", 64), e)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	want := []string{"/usr/bin/virsh", "--connect", "qemu:///system", "console", consoleTestUUID, "--devname", "serial0", "--safe"}
	if !reflect.DeepEqual(session.Command.Args, want) || c.calls != 1 {
		t.Fatalf("command=%v calls=%d", session.Command.Args, c.calls)
	}
	if session.Command.Process != nil {
		t.Fatal("preparation launched a process")
	}
	joined := strings.Join(session.Command.Env, "\n")
	for _, bad := range []string{"LD_PRELOAD", "SPICE_PROXY", "XDG_CONFIG_HOME"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("unsafe inherited environment: %s", joined)
		}
	}
	if !strings.Contains(joined, "TERM=xterm") {
		t.Fatal("terminal environment lost")
	}
}
func TestConsoleRefusesChangedUnsafeAndUnavailableChoices(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*consoleTestClient, *consoleEnvironment)
	}{
		{"root", func(_ *consoleTestClient, e *consoleEnvironment) { e.uid = 0 }},
		{"different resource", func(c *consoleTestClient, _ *consoleEnvironment) {
			c.info.Resource.UUID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
		}},
		{"changed config", func(c *consoleTestClient, _ *consoleEnvironment) { c.info.ConfigFingerprint = strings.Repeat("b", 64) }},
		{"stopped", func(c *consoleTestClient, _ *consoleEnvironment) { c.info.State = "shutoff" }},
		{"missing", func(c *consoleTestClient, _ *consoleEnvironment) { c.info.Choices = nil }},
		{"unavailable", func(c *consoleTestClient, _ *consoleEnvironment) { c.info.Choices[0].Available = false }},
		{"duplicate", func(c *consoleTestClient, _ *consoleEnvironment) {
			c.info.Choices = append(c.info.Choices, c.info.Choices[0])
		}},
		{"option device", func(c *consoleTestClient, _ *consoleEnvironment) { c.info.Choices[0].Device = "--force" }},
		{"shell device", func(c *consoleTestClient, _ *consoleEnvironment) {
			c.info.Choices[0].Device = "serial0; touch /tmp/bad"
		}},
		{"protocol", func(c *consoleTestClient, _ *consoleEnvironment) { c.info.Choices[0].Protocol = "spice" }},
		{"service error", func(c *consoleTestClient, _ *consoleEnvironment) {
			c.responseError = domain.Fail("OPERATION_FAILED", "unavailable")
		}},
		{"transport error", func(c *consoleTestClient, _ *consoleEnvironment) { c.err = errors.New("disconnected") }},
		{"missing binary", func(_ *consoleTestClient, e *consoleEnvironment) {
			e.executable = func(string) error { return errors.New("missing prerequisite") }
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, e := consoleFixture()
			tt.mutate(c, &e)
			s, err := prepareConsole(context.Background(), c, "qemu:///system", consoleTestUUID, "serial:0", strings.Repeat("a", 64), e)
			if err == nil || s != nil {
				t.Fatal("unsafe console prepared")
			}
		})
	}
}
func TestConsoleRejectsRemoteAndMalformedIdentityBeforeRPC(t *testing.T) {
	for _, connection := range []string{"qemu+ssh://other/system", "qemu:///system?socket=/tmp/fake", "--connect"} {
		c, e := consoleFixture()
		_, err := prepareConsole(context.Background(), c, connection, consoleTestUUID, "serial:0", strings.Repeat("a", 64), e)
		if err == nil || c.calls != 0 {
			t.Fatal("remote connection accepted")
		}
	}
	c, e := consoleFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prepareConsole(ctx, c, "qemu:///system", consoleTestUUID, "serial:0", strings.Repeat("a", 64), e); err == nil || c.calls != 0 {
		t.Fatal("cancellation ignored")
	}
}
func TestConsoleSPICEUsesPrivatePreferencesAndCleansUp(t *testing.T) {
	c, e := consoleFixture()
	e.tempRoot = t.TempDir()
	c.info.Choices = []domain.ConsoleChoice{{ID: "graphics:0", Kind: "graphical", Protocol: "spice", Available: true}}
	s, err := prepareConsole(context.Background(), c, "qemu:///system", consoleTestUUID, "graphics:0", strings.Repeat("a", 64), e)
	if err != nil {
		t.Fatal(err)
	}
	config := s.configDir
	want := []string{"/usr/bin/virt-viewer", "--connect", "qemu:///system", "--attach", "--uuid", "--auto-resize", "never", "--spice-disable-audio", "--spice-disable-usbredir", consoleTestUUID}
	if !reflect.DeepEqual(s.Command.Args, want) {
		t.Fatal(s.Command.Args)
	}
	data, err := os.ReadFile(filepath.Join(config, "virt-viewer", "settings"))
	if err != nil || string(data) != "[virt-viewer]\nshare-clipboard=false\n" {
		t.Fatalf("config %q err %v", data, err)
	}
	for path, mode := range map[string]os.FileMode{config: 0700, filepath.Join(config, "virt-viewer"): 0700, filepath.Join(config, "virt-viewer", "settings"): 0600} {
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != mode {
			t.Fatalf("private mode %s: %v", path, err)
		}
	}
	if !strings.Contains(strings.Join(s.Command.Env, "\n"), "XDG_CONFIG_HOME="+config) {
		t.Fatal("private settings not selected")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(config); !os.IsNotExist(err) {
		t.Fatal("config retained")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
func TestConsoleGraphicalPrerequisitesAndVersionGate(t *testing.T) {
	for _, kind := range []string{"no display", "old viewer", "future viewer", "version failure", "vnc", "multiple graphics"} {
		t.Run(kind, func(t *testing.T) {
			c, e := consoleFixture()
			e.tempRoot = t.TempDir()
			c.info.Choices = []domain.ConsoleChoice{{ID: "graphics:0", Kind: "graphical", Protocol: "spice", Available: true}}
			switch kind {
			case "no display":
				e.environ = nil
			case "old viewer":
				e.version = func(context.Context, string, []string) (string, error) { return "virt-viewer version 10.0\n", nil }
			case "future viewer":
				e.version = func(context.Context, string, []string) (string, error) { return "virt-viewer version 12.0\n", nil }
			case "version failure":
				e.version = func(context.Context, string, []string) (string, error) { return "", errors.New("unavailable") }
			case "vnc":
				c.info.Choices[0].Protocol = "vnc"
			case "multiple graphics":
				c.info.Choices[0].GraphicsIndex = 1
			}
			s, err := prepareConsole(context.Background(), c, "qemu:///system", consoleTestUUID, "graphics:0", strings.Repeat("a", 64), e)
			if err == nil || s != nil {
				t.Fatal("unsupported viewer prepared")
			}
			entries, _ := os.ReadDir(e.tempRoot)
			if len(entries) != 0 {
				t.Fatal("temporary files leaked")
			}
		})
	}
}
func TestConsoleVersionOutputBound(t *testing.T) {
	var b consoleVersionBuffer
	if _, err := b.Write(make([]byte, 4096)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Write([]byte("x")); err == nil {
		t.Fatal("unbounded version")
	}
}

func TestConsoleNilClient(t *testing.T) {
	_, env := consoleFixture()
	if session, err := prepareConsole(context.Background(), nil, "qemu:///system", consoleTestUUID, "serial:0", strings.Repeat("a", 64), env); err == nil || session != nil {
		t.Fatal("nil service accepted")
	}
}

func TestConsoleFedoraViewerVersion(t *testing.T) {
	c, e := consoleFixture()
	e.tempRoot = t.TempDir()
	c.info.Choices = []domain.ConsoleChoice{{ID: "graphics:0", Kind: "graphical", Protocol: "spice", Available: true}}
	e.version = func(context.Context, string, []string) (string, error) {
		return "virt-viewer version 11.0-18.fc44\n", nil
	}
	session, err := prepareConsole(context.Background(), c, "qemu:///system", consoleTestUUID, "graphics:0", strings.Repeat("a", 64), e)
	if err != nil {
		t.Fatal("installed Fedora package rejected", err)
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
}
