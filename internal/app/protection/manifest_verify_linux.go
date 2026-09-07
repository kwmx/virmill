//go:build linux

package protection

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
)

const manifestReadChunk = 256 << 10

type manifestRoot struct {
	file     *os.File
	path     string
	identity fileidentity.Identity
}

type manifestHeldFile struct {
	file     *os.File
	path     string
	identity fileidentity.Identity
}

func openManifestRoot(root string) (*manifestRoot, error) {
	if root == "" || filepath.Clean(root) != root {
		return nil, domain.Fail("INVALID_INPUT", "explicit canonical recovery artifact directory required")
	}
	path, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	f, identity, err := fileidentity.Open(path, true, false)
	if err != nil {
		return nil, err
	}
	return &manifestRoot{file: f, path: path, identity: identity}, nil
}

func (root *manifestRoot) openMember(path string, readable bool) (manifestHeldFile, error) {
	fd, err := unix.Openat2(int(root.file.Fd()), path, &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV,
	})
	if err != nil {
		return manifestHeldFile{}, err
	}
	f := os.NewFile(uintptr(fd), path)
	id, err := fileidentity.InspectFile(f, false)
	if err != nil {
		f.Close()
		return manifestHeldFile{}, err
	}
	if readable {
		// Check type through O_PATH first: even opening a device for reading may
		// have side effects. Reopen only this held ordinary inode, never the
		// caller's path, then check its complete identity again before reading.
		defer f.Close()
		fd, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", f.Fd()), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
		if err != nil {
			return manifestHeldFile{}, err
		}
		bytesFile := os.NewFile(uintptr(fd), path)
		observed, err := fileidentity.InspectFile(bytesFile, false)
		if err != nil {
			bytesFile.Close()
			return manifestHeldFile{}, err
		}
		if observed != id {
			bytesFile.Close()
			return manifestHeldFile{}, domain.Fail("STALE_PLAN", "recovery member changed before readable open")
		}
		return manifestHeldFile{file: bytesFile, path: path, identity: id}, nil
	}
	return manifestHeldFile{file: f, path: path, identity: id}, nil
}

func (root *manifestRoot) recheck(ctx context.Context, held []manifestHeldFile) error {
	for _, member := range held {
		if err := ctx.Err(); err != nil {
			return err
		}
		id, err := fileidentity.InspectFile(member.file, false)
		if err != nil {
			return err
		}
		if id != member.identity {
			return domain.Fail("STALE_PLAN", "recovery artifact changed during verification")
		}
		// The held inode must still occupy the declared path beneath the held
		// root. This reopen uses O_PATH, so a replacement special file cannot block.
		current, err := root.openMember(member.path, false)
		if err != nil {
			return err
		}
		closeErr := current.file.Close()
		if current.identity != member.identity {
			return domain.Fail("STALE_PLAN", "recovery artifact path was replaced during verification")
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	heldID, err := fileidentity.InspectFile(root.file, true)
	if err != nil {
		return err
	}
	currentRoot, currentID, err := fileidentity.Open(root.path, true, false)
	if err != nil {
		return err
	}
	closeErr := currentRoot.Close()
	if heldID != root.identity || currentID != root.identity {
		return domain.Fail("STALE_PLAN", "recovery artifact directory changed during verification")
	}
	if closeErr != nil {
		return closeErr
	}
	return ctx.Err()
}

// copyManifestBytes checks cancellation between bounded reads and never consumes
// more than the declared size plus one byte. It does not spawn a leaked reader
// goroutine when local storage stalls inside a kernel read.
func copyManifestBytes(ctx context.Context, dst io.Writer, src io.Reader, size int64) (int64, error) {
	buf := make([]byte, manifestReadChunk)
	var copied int64
	for copied <= size {
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		want := int64(len(buf))
		if remaining := size + 1 - copied; want > remaining {
			want = remaining
		}
		n, readErr := src.Read(buf[:int(want)])
		if err := ctx.Err(); err != nil {
			return copied, err
		}
		if n < 0 || n > int(want) {
			return copied, io.ErrShortBuffer
		}
		if n > 0 {
			written, err := dst.Write(buf[:n])
			copied += int64(written)
			if err != nil {
				return copied, err
			}
			if written != n {
				return copied, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return copied, nil
		}
		if readErr != nil {
			return copied, readErr
		}
		if n == 0 {
			return copied, io.ErrNoProgress
		}
	}
	return copied, nil
}

func verifyManifestMembers(ctx context.Context, directory string, members []Member) error {
	root, err := openManifestRoot(directory)
	if err != nil {
		return err
	}
	defer root.file.Close()
	held := make([]manifestHeldFile, 0, len(members))
	defer func() {
		for _, member := range held {
			member.file.Close()
		}
	}()
	// Hold the entire declared set before reading any bytes. A later member
	// cannot replace a previously read artifact without a final identity failure.
	for _, member := range members {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := root.openMember(member.Path, true)
		if err != nil {
			return err
		}
		held = append(held, f)
		if f.identity.Size != uint64(member.Size) {
			return domain.Fail("INCOMPLETE_BACKUP", "declared recovery member size differs from file")
		}
	}
	for i, member := range members {
		hash := sha256.New()
		n, err := copyManifestBytes(ctx, hash, held[i].file, member.Size)
		if err != nil {
			return err
		}
		if n != member.Size || hex.EncodeToString(hash.Sum(nil)) != member.SHA256 {
			return domain.Fail("INCOMPLETE_BACKUP", "declared recovery member digest or size mismatch")
		}
	}
	return root.recheck(ctx, held)
}

func readManifestDocument(ctx context.Context, filename string) ([]byte, error) {
	if filename == "" || filepath.Clean(filename) != filename {
		return nil, domain.Fail("INVALID_INPUT", "explicit canonical recovery manifest path required")
	}
	path, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	root, err := openManifestRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer root.file.Close()
	f, err := root.openMember(filepath.Base(path), true)
	if err != nil {
		return nil, err
	}
	defer f.file.Close()
	if f.identity.Size == 0 || f.identity.Size > manifestDocumentLimit {
		return nil, domain.Fail("INVALID_INPUT", "recovery manifest file exceeds the nonempty 8 MiB bound")
	}
	var data bytes.Buffer
	n, err := copyManifestBytes(ctx, &data, f.file, int64(f.identity.Size))
	if err != nil {
		return nil, err
	}
	if n != int64(f.identity.Size) {
		return nil, domain.Fail("STALE_PLAN", "recovery manifest size changed while reading")
	}
	if err := root.recheck(ctx, []manifestHeldFile{f}); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}
