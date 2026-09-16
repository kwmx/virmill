//go:build linux

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

// ConsoleSession owns temporary viewer preferences. Close must be called after
// the process exits, or if it is never started. It never stops the guest.
type ConsoleSession struct {
	Command *exec.Cmd
	// Graphical sessions open their own window and run beside the TUI; a
	// serial console takes over the terminal.
	Graphical bool
	configDir string
}

// DisplayAvailable reports whether this process can open a desktop window.
func DisplayAvailable() bool { return consoleHasDisplay(os.Environ()) }

// displaySettle is how long a detached viewer must stay open before it counts
// as opened; virt-viewer exits within it when it cannot attach.
const displaySettle = 2 * time.Second

// StartDetached opens a graphical session in its own window without taking
// over the terminal. It returns once the viewer has stayed open for the settle
// time, or with the viewer's own error when it closed before that. The channel
// reports when the window closes; the session's settings are removed then.
func (s *ConsoleSession) StartDetached() (<-chan error, error) {
	if s == nil || s.Command == nil || !s.Graphical {
		return nil, domain.Fail("INVALID_INPUT", "Only a graphical display opens in its own window.")
	}
	cmd := s.Command
	out := &tailBuffer{}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, out
	// Its own process group: Ctrl-C in the TUI must not close the window.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = s.Close()
		return nil, domain.Fail("OPERATION_FAILED", "Could not open the display window: "+err.Error())
	}
	done := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		if err != nil {
			if detail := strings.TrimSpace(out.String()); detail != "" {
				err = fmt.Errorf("%w: %s", err, detail)
			}
		}
		if closeErr := s.Close(); err == nil {
			err = closeErr
		}
		done <- err
	}()
	select {
	case err := <-done:
		reason := "the viewer exited"
		if err != nil {
			reason = err.Error()
		}
		return nil, domain.Fail("OPERATION_FAILED", "The display window closed right away ("+reason+"). Check that the VM is still running, then try again.")
	case <-time.After(displaySettle):
		return done, nil
	}
}

// tailBuffer keeps the last lines a detached viewer printed, for its error.
type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > 2048 {
		b.data = b.data[len(b.data)-2048:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

func (s *ConsoleSession) Close() error {
	if s == nil || s.configDir == "" {
		return nil
	}
	err := os.RemoveAll(s.configDir)
	if err == nil {
		s.configDir = ""
	}
	return err
}

type consoleEnvironment struct {
	uid        int
	environ    []string
	executable func(string) error
	version    func(context.Context, string, []string) (string, error)
	tempRoot   string
}

// PrepareConsole rechecks the selected VM and returns a fixed-argument console
// process. The caller owns terminal handoff and must call the session's Close.
// The context covers preparation only: a UI request timeout must not terminate
// an interactive session after the console has opened.
func PrepareConsole(ctx context.Context, client Client, connection, id, choiceID, expectedFingerprint string) (*ConsoleSession, error) {
	return prepareConsole(ctx, client, connection, id, choiceID, expectedFingerprint, consoleEnvironment{
		uid: os.Geteuid(), environ: os.Environ(), executable: trustedConsoleExecutable, version: consoleVersion,
	})
}

var consoleUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var consoleFingerprint = regexp.MustCompile(`^[0-9a-f]{64}$`)
var consoleViewerVersion = regexp.MustCompile(`(?m)^virt-viewer version 11\.0(?:-[0-9][A-Za-z0-9.+~]*)?(?:\s|$)`)
var consoleDevice = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)

func prepareConsole(ctx context.Context, client Client, connection, id, choiceID, fingerprint string, env consoleEnvironment) (*ConsoleSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, domain.Fail("OPERATION_FAILED", "The Virmill service is unavailable. Reopen Virmill and try again.")
	}
	if env.uid == 0 {
		return nil, domain.Fail("PERMISSION_DENIED", "Open Virmill as your ordinary user to use a guest console.")
	}
	if connection != "qemu:///system" && connection != "qemu:///session" {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Console access requires a local QEMU connection.")
	}
	if !consoleUUID.MatchString(id) || id == "00000000-0000-0000-0000-000000000000" || !consoleFingerprint.MatchString(fingerprint) || choiceID == "" {
		return nil, domain.Fail("INVALID_INPUT", "Select a console from the VM's current console information.")
	}
	response, err := client.Call(ctx, "vm.console.show", app.Request{Connection: connection, ID: id})
	if err != nil {
		return nil, err
	}
	if response.Error != nil {
		return nil, response.Error
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var info domain.ConsoleInfo
	raw, err := json.Marshal(response.Data)
	if err != nil || json.Unmarshal(raw, &info) != nil {
		return nil, domain.Fail("OPERATION_FAILED", "Could not read console information. Refresh the VM and try again.")
	}
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: connection, Kind: "vm", UUID: id}
	if info.Resource != key || info.ConfigFingerprint != fingerprint {
		return nil, domain.Fail("SOURCE_CHANGED", "The VM changed. Refresh its console options before connecting.")
	}
	if info.State != "running" {
		return nil, domain.Fail("INVALID_STATE", "Start the VM, then open its console.")
	}
	var selected domain.ConsoleChoice
	found := false
	for _, choice := range info.Choices {
		if choice.ID == choiceID {
			if found {
				return nil, domain.Fail("OPERATION_FAILED", "Console information is ambiguous. Refresh the VM.")
			}
			selected, found = choice, true
		}
	}
	if !found {
		return nil, domain.Fail("SOURCE_CHANGED", "This console is no longer configured. Refresh the VM.")
	}
	if !selected.Available {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "This console is unavailable. Refresh the console options for the current reason.")
	}
	session := &ConsoleSession{}
	var executable string
	var args []string
	switch selected.Kind {
	case "serial":
		if selected.Protocol != "serial" || !consoleDevice.MatchString(selected.Device) {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "This VM does not have a supported named serial console.")
		}
		executable = "/usr/bin/virsh"
		args = []string{"--connect", connection, "console", id, "--devname", selected.Device, "--safe"}
	case "graphical":
		if selected.Protocol != "spice" {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "This viewer adapter currently supports SPICE. VNC access needs verified clipboard controls; use a configured serial console meanwhile.")
		}
		if selected.GraphicsIndex != 0 {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Multiple graphical consoles need an explicit supported viewer selector.")
		}
		if !consoleHasDisplay(env.environ) {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "A graphical console needs a desktop session. Open Virmill on this host's desktop, or choose its serial console over SSH.")
		}
		executable = "/usr/bin/virt-viewer"
		args = []string{"--connect", connection, "--attach", "--uuid", "--auto-resize", "never", "--spice-disable-audio", "--spice-disable-usbredir", id}
	default:
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "This console type has no supported local viewer.")
	}
	if err := env.executable(executable); err != nil {
		return nil, err
	}
	processEnv := consoleProcessEnvironment(env.environ)
	if selected.Kind == "graphical" {
		version, err := env.version(ctx, executable, processEnv)
		if err != nil {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Could not verify virt-viewer. Install the supported virt-viewer 11.0 package and try again.")
		}
		if !consoleViewerVersion.MatchString(version) {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "Graphical access requires verified virt-viewer 11.0 clipboard controls. This installed version is not supported yet.")
		}
		dir, err := os.MkdirTemp(env.tempRoot, "virmill-viewer-")
		if err != nil {
			return nil, fmt.Errorf("create private viewer settings: %w", err)
		}
		session.configDir, session.Graphical = dir, true
		if err = os.Mkdir(filepath.Join(dir, "virt-viewer"), 0700); err == nil {
			err = os.WriteFile(filepath.Join(dir, "virt-viewer", "settings"), []byte("[virt-viewer]\nshare-clipboard=false\n"), 0600)
		}
		if err != nil {
			_ = session.Close()
			return nil, fmt.Errorf("write private viewer settings: %w", err)
		}
		processEnv = append(processEnv, "XDG_CONFIG_HOME="+dir)
	}
	if err := ctx.Err(); err != nil {
		_ = session.Close()
		return nil, err
	}
	session.Command = exec.Command(executable, args...)
	session.Command.Env = processEnv
	return session, nil
}

func consoleHasDisplay(env []string) bool {
	for _, item := range env {
		if (strings.HasPrefix(item, "DISPLAY=") || strings.HasPrefix(item, "WAYLAND_DISPLAY=")) && strings.SplitN(item, "=", 2)[1] != "" {
			return true
		}
	}
	return false
}
func consoleProcessEnvironment(env []string) []string {
	// Preserve only desktop/terminal identity and session transport. In particular,
	// dynamic loader, libvirt URI overrides, viewer preferences and SPICE proxy
	// settings cannot redirect this fixed local session.
	allowed := map[string]bool{"HOME": true, "USER": true, "LOGNAME": true, "TERM": true, "DISPLAY": true, "WAYLAND_DISPLAY": true, "XAUTHORITY": true, "XDG_RUNTIME_DIR": true, "DBUS_SESSION_BUS_ADDRESS": true, "LANG": true}
	out := []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	for _, item := range env {
		key, _, ok := strings.Cut(item, "=")
		if ok && allowed[key] {
			out = append(out, item)
		}
	}
	return out
}
func trustedConsoleExecutable(path string) error {
	if path != "/usr/bin/virsh" && path != "/usr/bin/virt-viewer" {
		return domain.Fail("PERMISSION_DENIED", "Unsupported console executable.")
	}
	for _, name := range []string{"/", "/usr", "/usr/bin", path} {
		info, err := os.Lstat(name)
		if err != nil {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "Install the distribution's "+filepath.Base(path)+" package to open this console (expected "+path+").")
		}
		owner, ok := info.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != 0 || info.Mode().Perm()&0022 != 0 || info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid) != 0 {
			return domain.Fail("PERMISSION_DENIED", "Console tools must be ordinary administrator-owned system executables.")
		}
		if name == path {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
				return domain.Fail("PERMISSION_DENIED", "The console tool is not an executable regular file.")
			}
		} else if !info.IsDir() {
			return domain.Fail("PERMISSION_DENIED", "The system executable directory is not trusted.")
		}
	}
	return nil
}
func consoleVersion(ctx context.Context, path string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = env
	var out consoleVersionBuffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return string(out.data), err
}

type consoleVersionBuffer struct{ data []byte }

func (b *consoleVersionBuffer) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 4096 {
		return 0, fmt.Errorf("viewer version output exceeds limit")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}
