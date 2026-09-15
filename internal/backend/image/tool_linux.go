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
	return runCommand(ctx, args, func(worker context.Context) (*exec.Cmd, func(), error) {
		return platform.ConfinedDiskCommand(worker, source, workspace, args, bound)
	})
}

func runFiles(ctx context.Context, sources []platform.DiskSourceFile, workspace string, bound int64, args ...string) ([]byte, error) {
	return runCommand(ctx, args, func(worker context.Context) (*exec.Cmd, func(), error) {
		return platform.ConfinedDiskFilesCommand(worker, sources, workspace, args, bound)
	})
}

func runCommand(ctx context.Context, args []string, makeCommand func(context.Context) (*exec.Cmd, func(), error)) ([]byte, error) {
	select {
	case workerSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-workerSlots }()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	cmd, cleanup, err := makeCommand(ctx)
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

// QEMU may report contained backing names without cleaning their ../ segments.
// The source namespace contains only regular files and ordinary directories;
// normalize those names without allowing a traversal above /source, even if a
// later segment would re-enter it. Keep the original report unchanged for audit.
func canonicalSourceName(filename string) string {
	if !strings.HasPrefix(filename, "/source/") {
		return ""
	}
	rel := strings.TrimPrefix(filename, "/source/")
	depth := 0
	for _, part := range strings.Split(rel, "/") {
		switch part {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return ""
			}
		default:
			depth++
		}
	}
	rel = path.Clean(rel)
	if importer.SafePath(rel) != nil {
		return ""
	}
	return "/source/" + rel
}
func approvedFile(filename string, members map[string]bool) bool {
	canonical := canonicalSourceName(filename)
	return canonical != "" && members[strings.TrimPrefix(canonical, "/source/")]
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
		canonical := canonicalSourceName(v.Filename)
		if !approvedFile(v.Filename, members) || seen[canonical] {
			return domain.Fail("PERMISSION_DENIED", "image references an unapproved file or a backing cycle")
		}
		seen[canonical] = true
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
			resolved := canonicalSourceName(path.Dir(v.Filename) + "/" + v.Backing)
			if !approvedFile(resolved, members) || resolved != canonicalSourceName(chain[i+1].Filename) || (v.FullBacking != "" && canonicalSourceName(v.FullBacking) != resolved) || v.BackingFormat != chain[i+1].Format {
				return domain.Fail("PERMISSION_DENIED", "backing chain differs from approved source members")
			}
		} else if v.Backing != "" || v.FullBacking != "" {
			return domain.Fail("INVALID_INPUT", "incomplete backing chain")
		}
		if err := checkSpecific(v.Specific, func(name string) bool { return approvedFile(name, members) }); err != nil {
			return err
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
	return convert(workspace, filename, format, virtualSize, maxOutput, func(args ...string) ([]byte, error) { return run(ctx, source, workspace, maxOutput, args...) })
}

// InspectFiles detects the root format and inspects all declared backing nodes
// using only the selected held file set. There is no force-share option.
func (Tool) InspectFiles(ctx context.Context, sources []platform.DiskSourceFile, workspace, filename, format string, maxVirtual int64) ([]Info, error) {
	guarded, closeGuards, err := guardedSources(sources)
	if err != nil {
		return nil, err
	}
	defer closeGuards()
	sources = guarded
	members := map[string]bool{}
	for _, source := range sources {
		members[source.Path] = true
	}
	if !Format(format) || importer.SafePath(filename) != nil || !members[filename] {
		return nil, domain.Fail("INVALID_INPUT", "selected file and explicit supported format required")
	}
	b, err := runFiles(ctx, sources, workspace, 64<<20, "info", "--output=json", "--backing-chain", "/source/"+filename)
	if err != nil {
		return nil, err
	}
	var chain []Info
	if err = wire.Decode(b, &chain); err != nil {
		return nil, err
	}
	return chain, CheckChain(chain, format, filename, maxVirtual, members)
}

func (Tool) ConvertFiles(ctx context.Context, sources []platform.DiskSourceFile, workspace, filename, format string, virtualSize, maxOutput int64) error {
	guarded, closeGuards, err := guardedSources(sources)
	if err != nil {
		return err
	}
	defer closeGuards()
	sources = guarded
	return convert(workspace, filename, format, virtualSize, maxOutput, func(args ...string) ([]byte, error) { return runFiles(ctx, sources, workspace, maxOutput, args...) })
}

func convert(workspace, filename, format string, virtualSize, maxOutput int64, runTool func(...string) ([]byte, error)) error {
	if !Format(format) || importer.SafePath(filename) != nil {
		return domain.Fail("INVALID_INPUT", "invalid conversion mapping or bound")
	}
	return convertSource(workspace, format, "/source/"+filename, virtualSize, maxOutput, runTool)
}

// convertSource converts one confined source to /work/disk.qcow2 and checks the
// result against it. An empty format means the source name carries its driver.
// maxOutput is the measured budget or the worst case, which can be smaller than
// the virtual size (ADR 0060).
func convertSource(workspace, format, source string, virtualSize, maxOutput int64, runTool func(...string) ([]byte, error)) error {
	if virtualSize <= 0 || maxOutput <= 0 || maxOutput > 1<<40 {
		return domain.Fail("INVALID_INPUT", "invalid conversion mapping or bound")
	}
	if _, err := os.Lstat(workspace + "/disk.qcow2"); !os.IsNotExist(err) {
		return domain.Fail("STALE_PLAN", "conversion destination already exists")
	}
	from := []string{source}
	if format != "" {
		from = []string{"-f", format, source}
	}
	args := append(append([]string{"convert"}, from[:len(from)-1]...), "-O", "qcow2", "-o", "compat=1.1", "-t", "writethrough", source, "/work/disk.qcow2")
	if _, err := runTool(args...); err != nil {
		return err
	}
	b, err := runTool("info", "--output=json", "-f", "qcow2", "/work/disk.qcow2")
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
	if _, err = runTool("check", "--output=json", "-f", "qcow2", "/work/disk.qcow2"); err != nil {
		return err
	}
	_, err = runTool(append(append([]string{"compare"}, from[:len(from)-1]...), "-F", "qcow2", source, "/work/disk.qcow2")...)
	return err
}
