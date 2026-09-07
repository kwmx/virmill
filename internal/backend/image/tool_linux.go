//go:build linux && amd64

// Package image adapts the system qemu-img tool only inside unprivileged isolation.
package image

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"syscall"
	"time"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/wire"
)

type Identity struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}
type Info struct {
	Filename      string          `json:"filename"`
	Format        string          `json:"format"`
	VirtualSize   int64           `json:"virtual-size"`
	ActualSize    int64           `json:"actual-size"`
	Encrypted     bool            `json:"encrypted"`
	Backing       string          `json:"backing-filename"`
	FullBacking   string          `json:"full-backing-filename"`
	BackingFormat string          `json:"backing-filename-format"`
	Specific      json.RawMessage `json:"format-specific"`
	Dirty         bool            `json:"dirty-flag"`
	ClusterSize   int64           `json:"cluster-size"`
	Compressed    bool            `json:"compressed"`
	Snapshots     json.RawMessage `json:"snapshots,omitempty"`
	Limits        json.RawMessage `json:"limits,omitempty"`
	Children      []Child         `json:"children,omitempty"`
}
type Child struct {
	Name string `json:"name"`
	Info Info   `json:"info"`
}
type Tool struct{}

func Format(format string) bool {
	switch format {
	case "raw", "qcow2", "vmdk", "vdi", "vpc", "vhdx":
		return true
	}
	return false
}

func (Tool) Identity(ctx context.Context) (Identity, error) {
	var id Identity
	id.Path = "/usr/bin/qemu-img"
	f, err := os.OpenFile(id.Path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return id, domain.Fail("UNSUPPORTED_CAPABILITY", "system qemu-img is unavailable")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return id, err
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.Mode().IsRegular() || owner.Uid != 0 || st.Mode().Perm()&0022 != 0 {
		return id, domain.Fail("PERMISSION_DENIED", "image tool must be a root-owned system executable, not writable by other users")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return id, err
	}
	id.SHA256 = hex.EncodeToString(h.Sum(nil))
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, id.Path, "--version")
	cmd.Env = []string{"PATH=/usr/bin", "LC_ALL=C"}
	b, err := cmd.Output()
	if err != nil {
		return id, err
	}
	id.Version = strings.SplitN(string(b), "\n", 2)[0]
	return id, nil
}

type boundedBuffer struct {
	mu    sync.Mutex
	b     bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) > b.limit-b.b.Len() {
		return 0, errors.New("image tool output exceeds bound")
	}
	return b.b.Write(p)
}
func (b *boundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte{}, b.b.Bytes()...)
}

var workerSlots = make(chan struct{}, 2)

func run(ctx context.Context, source, workspace string, bound int64, args ...string) ([]byte, error) {
	select {
	case workerSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-workerSlots }()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	cmd, cleanup, err := platform.ConfinedDiskCommand(ctx, source, workspace, args, bound)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	stdout := &boundedBuffer{limit: 1 << 20}
	stderr := &boundedBuffer{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("confined qemu-img %s failed: %w: %s", args[0], err, stderr.Bytes())
	}
	return stdout.Bytes(), nil
}

func approvedFile(filename string, members map[string]bool) bool {
	if !strings.HasPrefix(filename, "/source/") {
		return false
	}
	rel := strings.TrimPrefix(filename, "/source/")
	return importer.SafePath(rel) == nil && members[rel]
}

func CheckChain(chain []Info, format, sourcePath string, maxVirtual int64, members map[string]bool) error {
	if len(chain) == 0 || len(chain) > 64 {
		return domain.Fail("INVALID_INPUT", "empty or excessive backing chain")
	}
	seen := map[string]bool{}
	nodes := 0
	var checkChildren func(Info) error
	checkChildren = func(v Info) error {
		nodes++
		if nodes > 4096 || !approvedFile(v.Filename, members) || (v.Format != "file" && !Format(v.Format)) || v.Encrypted || v.Dirty {
			return domain.Fail("PERMISSION_DENIED", "block graph contains an unapproved, encrypted, dirty or excessive dependency")
		}
		for _, child := range v.Children {
			if err := checkChildren(child.Info); err != nil {
				return err
			}
		}
		return nil
	}
	for i, v := range chain {
		if !Format(v.Format) || v.Encrypted || v.VirtualSize <= 0 || v.VirtualSize > maxVirtual {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "encrypted, unsupported or oversized disk image")
		}
		if !approvedFile(v.Filename, members) || seen[v.Filename] {
			return domain.Fail("PERMISSION_DENIED", "image references an unapproved file or a backing cycle")
		}
		seen[v.Filename] = true
		if err := checkChildren(v); err != nil {
			return err
		}
		if i == 0 && (v.Format != format || v.Filename != "/source/"+sourcePath) {
			return domain.Fail("SOURCE_CHANGED", "detected disk differs from explicit mapping")
		}
		if i+1 < len(chain) {
			if v.Backing == "" || path.IsAbs(v.Backing) || strings.ContainsAny(v.Backing, ":\\") || v.BackingFormat == "" {
				return domain.Fail("PERMISSION_DENIED", "backing files need an explicit format and a contained relative reference")
			}
			resolved := path.Clean(path.Join(path.Dir(v.Filename), v.Backing))
			if !approvedFile(resolved, members) || resolved != chain[i+1].Filename || (v.FullBacking != "" && v.FullBacking != resolved) || v.BackingFormat != chain[i+1].Format {
				return domain.Fail("PERMISSION_DENIED", "backing chain differs from approved source members")
			}
		} else if v.Backing != "" || v.FullBacking != "" {
			return domain.Fail("INVALID_INPUT", "incomplete backing chain")
		}
		if len(v.Specific) > 0 {
			var specific any
			if err := wire.Decode(v.Specific, &specific); err != nil {
				return err
			}
			var walk func(any) error
			walk = func(value any) error {
				switch n := value.(type) {
				case map[string]any:
					for k, child := range n {
						if k == "corrupt" && child != false {
							return domain.Fail("INVALID_INPUT", "source image reports corruption")
						}
						if k == "data-file" || k == "data-file-raw" {
							return domain.Fail("UNSUPPORTED_CAPABILITY", "external qcow2 data files require a dedicated dependency adapter")
						}
						if k == "filename" {
							name, ok := child.(string)
							if !ok || !approvedFile(name, members) {
								return domain.Fail("PERMISSION_DENIED", "image extent references an unapproved file")
							}
						}
						if err := walk(child); err != nil {
							return err
						}
					}
				case []any:
					for _, child := range n {
						if err := walk(child); err != nil {
							return err
						}
					}
				}
				return nil
			}
			if err := walk(specific); err != nil {
				return err
			}
		}
	}
	return nil
}

func (Tool) Inspect(ctx context.Context, source, workspace, filename, format string, maxVirtual int64, members map[string]bool) ([]Info, error) {
	if !Format(format) || importer.SafePath(filename) != nil || !members[filename] {
		return nil, domain.Fail("INVALID_INPUT", "explicit disk format and approved package-relative file required")
	}
	b, err := run(ctx, source, workspace, 64<<20, "info", "--output=json", "--backing-chain", "-f", format, "/source/"+filename)
	if err != nil {
		return nil, err
	}
	var chain []Info
	if err = wire.Decode(b, &chain); err != nil {
		return nil, err
	}
	return chain, CheckChain(chain, format, filename, maxVirtual, members)
}

func (Tool) Convert(ctx context.Context, source, workspace, filename, format string, virtualSize, maxOutput int64) error {
	if !Format(format) || importer.SafePath(filename) != nil || virtualSize <= 0 || maxOutput < virtualSize {
		return domain.Fail("INVALID_INPUT", "invalid conversion mapping or bound")
	}
	if _, err := os.Lstat(workspace + "/disk.qcow2"); !os.IsNotExist(err) {
		return domain.Fail("STALE_PLAN", "conversion destination already exists")
	}
	if _, err := run(ctx, source, workspace, maxOutput, "convert", "-f", format, "-O", "qcow2", "-o", "compat=1.1", "-t", "writethrough", "/source/"+filename, "/work/disk.qcow2"); err != nil {
		return err
	}
	b, err := run(ctx, source, workspace, maxOutput, "info", "--output=json", "-f", "qcow2", "/work/disk.qcow2")
	if err != nil {
		return err
	}
	var out Info
	if err = wire.Decode(b, &out); err != nil {
		return err
	}
	if out.Format != "qcow2" || out.VirtualSize != virtualSize || out.Encrypted || out.Backing != "" || out.FullBacking != "" {
		return domain.Fail("SOURCE_CHANGED", "converted disk has unexpected size, encryption or backing dependency")
	}
	if _, err = run(ctx, source, workspace, maxOutput, "check", "--output=json", "-f", "qcow2", "/work/disk.qcow2"); err != nil {
		return err
	}
	_, err = run(ctx, source, workspace, maxOutput, "compare", "-f", format, "-F", "qcow2", "/source/"+filename, "/work/disk.qcow2")
	return err
}
