//go:build linux && amd64

package creating

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/domain"
)

// spaceBudgetFileBytes reserves each disk's prepared size instead of its virtual
// size plus 25% (ADR 0060). Recipes without a rule keep the old one.
const spaceBudgetFileBytes = "file-bytes-v1"

// handOver is set when creation releases the prepared copy while uploading its
// disks: asked for, and the pool shares the prepared folder's filesystem, which
// can release ranges of a file (ADR 0060).
type handOver struct {
	PoolPath string `json:"poolPath"`
	Device   uint64 `json:"device,string"`
}

// diskReserve is the pool space one disk needs under the recipe's rule.
func diskReserve(in input, virtual, file uint64) (uint64, bool) {
	switch in.SpaceBudget {
	case "":
		return creationDiskBudget(virtual)
	case spaceBudgetFileBytes:
		if in.HandOver != nil {
			return 16 << 20, true
		}
		return creationAddBytes(file, 16<<20)
	}
	return 0, false
}

// writtenReserve is how much of the budget a verified volume has used.
func writtenReserve(in input, v domain.VolumeIntent) uint64 {
	if in.SpaceBudget == "" || v.ContentType != "" {
		return v.FileBytes
	}
	reserve, _ := diskReserve(in, v.VirtualBytes, v.FileBytes)
	return reserve
}

func deviceOf(path string) (uint64, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	n, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("native file identity unavailable")
	}
	return uint64(n.Dev), nil
}

// canRelease probes whether the filesystem holding dir frees a punched range.
func canRelease(dir string) bool {
	f, err := os.CreateTemp(dir, ".virmill-release-probe-")
	if err != nil {
		return false
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(bytes.Repeat([]byte{0xff}, 1<<20)); err != nil || f.Sync() != nil {
		return false
	}
	var before, after unix.Stat_t
	if unix.Fstat(int(f.Fd()), &before) != nil || unix.Fallocate(int(f.Fd()), unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, 0, 1<<20) != nil {
		return false
	}
	return unix.Fstat(int(f.Fd()), &after) == nil && after.Blocks < before.Blocks
}

func poolPath(poolXML string) string {
	var pool struct {
		Target struct {
			Path string `xml:"path"`
		} `xml:"target"`
	}
	if xml.Unmarshal([]byte(poolXML), &pool) != nil || !filepath.IsAbs(pool.Target.Path) {
		return ""
	}
	return filepath.Clean(pool.Target.Path)
}

// planHandOver returns the hand-over for a creation that asked for one, or nil
// when it would not save space: the pool is on another filesystem, or this one
// cannot release ranges of a file. The prepared copy is then kept as before.
func (s *Service) planHandOver(ctx context.Context, uri, poolID, directory, choice string) (*handOver, error) {
	if choice != "hand-over" {
		return nil, nil
	}
	pool, err := s.Inventory.GetStoragePool(ctx, uri, poolID)
	if err != nil {
		return nil, err
	}
	path := poolPath(pool.XML)
	if path == "" {
		return nil, nil
	}
	device, err := deviceOf(path)
	if err != nil {
		return nil, nil
	}
	if prepared, err := deviceOf(directory); err != nil || prepared != device || !canRelease(filepath.Dir(directory)) {
		return nil, nil
	}
	return &handOver{PoolPath: path, Device: device}, nil
}

// checkHandOver keeps the space credit honest: the pool and the prepared copy
// must still share the reviewed filesystem.
func checkHandOver(in input) error {
	if in.HandOver == nil {
		return nil
	}
	pool, poolErr := deviceOf(in.HandOver.PoolPath)
	prepared, preparedErr := deviceOf(in.Directory)
	if poolErr != nil || preparedErr != nil || pool != in.HandOver.Device || prepared != in.HandOver.Device {
		return domain.Fail("STALE_PLAN", "the pool or the prepared copy is no longer on the reviewed filesystem")
	}
	return nil
}

// releasingReader frees the prepared file behind the bytes already handed to
// the upload stream, so the new volume grows as the prepared copy shrinks.
type releasingReader struct {
	f              *os.File
	read, released int64
}

func (r *releasingReader) release(end int64) error {
	if end <= r.released {
		return nil
	}
	if err := unix.Fallocate(int(r.f.Fd()), unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, r.released, end-r.released); err != nil {
		return fmt.Errorf("release the handed-over prepared copy: %w", err)
	}
	r.released = end
	return nil
}

// Read first releases what the previous Read returned: the upload sends each
// chunk before it asks for the next.
func (r *releasingReader) Read(p []byte) (int, error) {
	if err := r.release(r.read); err != nil {
		return 0, err
	}
	n, err := r.f.Read(p)
	r.read += int64(n)
	return n, err
}
