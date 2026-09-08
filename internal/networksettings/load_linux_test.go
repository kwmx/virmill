//go:build linux

package networksettings

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/app/network"
)

const customConfig = `{"version":1,"ranges":[{"cidr":"172.20.0.0/16","prefixLength":26}],"planned":[{"id":"planned-lab","cidr":"192.168.90.0/24"}]}`

func writeConfig(t *testing.T, raw string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(raw), 0640); err != nil {
		t.Fatal(err)
	}
	return dir, path
}

func TestCustomSettingsAreExactReadOnlyAndBounded(t *testing.T) {
	for _, size := range []int{len(customConfig), maxBytes} {
		raw := customConfig + strings.Repeat(" ", size-len(customConfig))
		dir, path := writeConfig(t, raw)
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Load(context.Background(), dir)
		want := network.AllocationConfig{Version: 1, Ranges: []network.AllocationRange{{CIDR: "172.20.0.0/16", PrefixLength: 26}}, Planned: []network.PlannedAllocation{{ID: "planned-lab", CIDR: "192.168.90.0/24"}}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("custom settings changed or rejected", got, err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.ModTime() != after.ModTime() {
			t.Fatal("configuration identity, permissions or mtime changed", err)
		}
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != raw {
			t.Fatal("configuration bytes changed", err)
		}
	}
}

func TestExistingInvalidSettingsNeverFallBack(t *testing.T) {
	for name, raw := range map[string]string{
		"empty": "", "malformed": "{", "trailing": customConfig + `{}`,
		"unknown version":    strings.Replace(customConfig, `"version":1`, `"version":2`, 1),
		"unknown root":       strings.Replace(customConfig, `"version":1`, `"version":1,"unexpected":true`, 1),
		"unknown range":      strings.Replace(customConfig, `"prefixLength":26`, `"prefixLength":26,"extra":true`, 1),
		"unknown planned":    strings.Replace(customConfig, `"id":"planned-lab"`, `"id":"planned-lab","extra":true`, 1),
		"duplicate root":     strings.Replace(customConfig, `"version":1`, `"version":1,"version":1`, 1),
		"escaped duplicate":  strings.Replace(customConfig, `"version":1`, `"version":1,"\u0076ersion":1`, 1),
		"nested duplicate":   strings.Replace(customConfig, `"prefixLength":26`, `"prefixLength":26,"prefixLength":27`, 1),
		"root case alias":    strings.Replace(customConfig, `"version":1`, `"Version":1`, 1),
		"nested case alias":  strings.Replace(customConfig, `"prefixLength":26`, `"PrefixLength":26`, 1),
		"planned case alias": strings.Replace(customConfig, `"id":`, `"ID":`, 1),
		"missing version":    strings.Replace(customConfig, `"version":1,`, "", 1),
		"null planned":       strings.Replace(customConfig, `[{"id":"planned-lab","cidr":"192.168.90.0/24"}]`, `null`, 1),
		"null range":         `{"version":1,"ranges":[null],"planned":[]}`,
		"public range":       strings.Replace(customConfig, "172.20.0.0/16", "8.8.0.0/16", 1),
		"oversized":          customConfig + strings.Repeat(" ", maxBytes+1-len(customConfig)),
		"invalid UTF8":       customConfig + string([]byte{0xff}),
	} {
		t.Run(name, func(t *testing.T) {
			dir, _ := writeConfig(t, raw)
			got, err := Load(context.Background(), dir)
			if err == nil || !reflect.DeepEqual(got, network.AllocationConfig{}) {
				t.Fatal("invalid existing configuration yielded usable settings", got, err)
			}
		})
	}
}

func TestUnsafeSettingsLeafNeverOpenedForIO(t *testing.T) {
	for _, kind := range []string{"fifo", "directory", "symlink", "dangling symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, filename)
			var err error
			switch kind {
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			case "symlink":
				_, target := writeConfig(t, customConfig)
				err = os.Symlink(target, path)
			case "dangling symlink":
				err = os.Symlink(filepath.Join(dir, "missing"), path)
			}
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				got, err := Load(context.Background(), dir)
				if !reflect.DeepEqual(got, network.AllocationConfig{}) {
					err = nil
				}
				done <- err
			}()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("unsafe leaf returned settings")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("nonregular settings blocked while opening")
			}
		})
	}
}

func TestUnreadableSettingsAreNotAbsence(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses DAC read permission; test requires ordinary user")
	}
	dir, path := writeConfig(t, customConfig)
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	got, err := Load(context.Background(), dir)
	if !errors.Is(err, os.ErrPermission) || !reflect.DeepEqual(got, network.AllocationConfig{}) {
		t.Fatal("unreadable existing file fell back", got, err)
	}
}

type settingsReaderFunc func([]byte) (int, error)

func (f settingsReaderFunc) Read(p []byte) (int, error) { return f(p) }

func TestBoundedReadHonorsLateCancellationAndReadFailure(t *testing.T) {
	t.Run("cancel with final EOF", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		raw, err := readBounded(ctx, settingsReaderFunc(func(p []byte) (int, error) {
			cancel()
			return copy(p, customConfig), io.EOF
		}))
		if !errors.Is(err, context.Canceled) || raw != nil {
			t.Fatal("EOF hid cancellation", string(raw), err)
		}
	})
	t.Run("read error with bytes", func(t *testing.T) {
		failure := errors.New("synthetic filesystem read failure")
		raw, err := readBounded(context.Background(), settingsReaderFunc(func(p []byte) (int, error) {
			return copy(p, customConfig), failure
		}))
		if !errors.Is(err, failure) || raw != nil {
			t.Fatal("partial failed read yielded bytes", raw, err)
		}
	})
	t.Run("growth beyond stat size remains bounded", func(t *testing.T) {
		raw, err := readBounded(context.Background(), bytes.NewReader(bytes.Repeat([]byte("x"), maxBytes+1)))
		if err == nil || raw != nil {
			t.Fatal("unbounded read", len(raw), err)
		}
	})
	t.Run("no progress refuses", func(t *testing.T) {
		raw, err := readBounded(context.Background(), settingsReaderFunc(func([]byte) (int, error) { return 0, nil }))
		if !errors.Is(err, io.ErrNoProgress) || raw != nil {
			t.Fatal("no-progress read did not stop", raw, err)
		}
	})
}
