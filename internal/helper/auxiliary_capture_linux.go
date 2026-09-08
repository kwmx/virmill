//go:build linux && amd64

package helper

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"reflect"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

// AuxiliaryCapture owns its sealed snapshot, original metadata pins, readable
// files and cooperative producer guards until Close. The authenticated endpoint
// must retain this object through final recheck and transfer. Locks do not stop
// native restart, setup or noncooperative writers; native and generation checks
// remain mandatory. This is auxiliary copying, not complete VM recovery proof.
type AuxiliaryCapture struct {
	Response AuxiliaryResponse
	Snapshot *os.File

	mu       sync.Mutex
	closed   bool
	failure  error
	executor AuxiliaryExecutor
	request  Request
	policy   Policy
	scan     *auxiliaryScan
	readers  map[string]*os.File
	guards   []auxiliaryHeld
	snapshot *os.File
}

func auxiliaryInventoryCopy(in *AuxiliaryInventory) (*AuxiliaryInventory, error) {
	data, err := json.Marshal(in)
	if err != nil || len(data) > auxiliaryMetadataLimit {
		return nil, auxiliaryFailure("INVALID_INPUT", "auxiliary inventory exceeds bounded capture metadata")
	}
	var out AuxiliaryInventory
	if err = json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if in == nil || !reflect.DeepEqual(*in, out) {
		return nil, auxiliaryFailure("INVALID_INPUT", "auxiliary inventory cannot be represented without changing its identity")
	}
	return &out, nil
}

// Capture accepts only the previously reviewed exact native inventory. It reads
// no caller-selected path and creates only an anonymous, bounded, sealed memfd.
// On any error it returns nil and closes every file and guard it acquired.
func (e AuxiliaryExecutor) Capture(ctx context.Context, r Request, p Policy) (_ *AuxiliaryCapture, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if r.Operation != "state.auxiliary" || r.Mode != "capture" || r.APIVersion != domain.APIVersion || p.APIVersion != domain.APIVersion || r.Network != nil || !auxiliaryID(r.JobID) || !auxiliaryDigest(r.PlanDigest) {
		return nil, auxiliaryFailure("INVALID_INPUT", "bound typed auxiliary capture operation required")
	}
	if err = authorizeAuxiliary(r, p); err != nil {
		return nil, err
	}
	permission, err := auxiliaryPolicy(r, p)
	if err != nil {
		return nil, err
	}
	// Do not retain mutable caller maps, slices or native-layout pointers.
	expected, err := auxiliaryInventoryCopy(r.Auxiliary.Expected)
	if err != nil {
		return nil, err
	}
	aux := *r.Auxiliary
	aux.Expected = expected
	r.Auxiliary = &aux
	p = Policy{APIVersion: p.APIVersion, Roots: map[string]string{r.RootID: p.Roots[r.RootID]}, Auxiliary: []AuxiliaryPermission{permission}}
	c := &AuxiliaryCapture{executor: e, request: r, policy: p, readers: map[string]*os.File{}}
	defer func() {
		if err != nil {
			_ = c.Close()
		}
	}()
	layout, err := e.native(ctx, r)
	if err != nil {
		return nil, err
	}
	if tpm := layout.TPM; tpm != nil && (tpm.SourceType != "dir" || tpm.SourcePath == "") {
		return nil, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "auxiliary capture requires explicit directory TPM storage; implicit and file backend writer exclusion is unqualified")
	}
	c.scan, err = e.scanAuxiliary(ctx, r, p, permission, layout)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(*expected, c.scan.inventory) {
		return nil, auxiliaryFailure("STALE_PLAN", "fresh auxiliary inventory differs from the reviewed capture inventory")
	}
	if layout.TPM != nil {
		lock := c.scan.inventory.TPMLock
		if lock == nil || lock.State.Size != 0 {
			return nil, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "directory TPM capture requires the existing exact empty producer .lock")
		}
		held := c.scan.held[lock.RelativePath]
		guard, openErr := auxiliaryCaptureRead(ctx, held)
		if openErr != nil {
			return nil, openErr
		}
		c.guards = append(c.guards, auxiliaryHeld{file: guard, state: held.state})
		ofd := unix.Flock_t{Type: unix.F_RDLCK, Whence: io.SeekStart, Start: 0, Len: 0}
		if err = unix.FcntlFlock(guard.Fd(), unix.F_OFD_SETLK, &ofd); err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EACCES) {
				return nil, auxiliaryFailure("RESOURCE_BUSY", "TPM persistent-state producer lock is busy")
			}
			return nil, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "TPM persistent-state OFD read guard unavailable")
		}
	}
	for _, member := range c.scan.inventory.Members {
		held := c.scan.held[member.RelativePath]
		reader, openErr := auxiliaryCaptureRead(ctx, held)
		if openErr != nil {
			return nil, openErr
		}
		c.readers[member.ID] = reader
		if member.Kind == "nvram" {
			guard, guardErr := image.AcquireReadGuard(reader)
			if guardErr != nil {
				return nil, guardErr
			}
			c.guards = append(c.guards, auxiliaryHeld{file: guard, state: held.state})
		}
	}
	// Exclusion is established before bytes are read, then the complete native
	// mapping and directory set are observed again with all original pins held.
	if err = c.Recheck(ctx); err != nil {
		return nil, err
	}
	archive, size, memberHashes, err := c.archiveReader()
	if err != nil {
		return nil, err
	}
	archiveHash := sha256.New()
	c.snapshot, err = CaptureSealed(ctx, io.TeeReader(archive, archiveHash), size)
	if err != nil {
		return nil, err
	}
	c.Snapshot = c.snapshot
	if err = c.Recheck(ctx); err != nil {
		return nil, err
	}
	binding, err := AuxiliaryBinding(r)
	if err != nil {
		return nil, err
	}
	inventoryDigest, err := operations.Digest(c.scan.inventory)
	if err != nil {
		return nil, err
	}
	inventory, err := auxiliaryInventoryCopy(&c.scan.inventory)
	if err != nil {
		return nil, err
	}
	sums := make(map[string]string, len(memberHashes))
	for id, h := range memberHashes {
		sums[id] = hex.EncodeToString(h.Sum(nil))
	}
	c.Response = AuxiliaryResponse{Version: 1, JobID: r.JobID, Binding: binding, Stage: "captured", Inventory: inventory, Artifact: &AuxiliaryArtifact{Format: "ustar-v1", Size: uint64(size), SHA256: hex.EncodeToString(archiveHash.Sum(nil)), InventoryDigest: inventoryDigest, MemberSHA256: sums}}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return c, nil
}

// The proc magic link deliberately reopens an already pinned regular inode.
// It never follows a native/user pathname for payload access. O_NONBLOCK avoids
// blocking even on unexpected special-file substitution; O_NOATIME preserves
// source access metadata. No source or producer lock is created or truncated.
func auxiliaryCaptureRead(ctx context.Context, held auxiliaryHeld) (*os.File, error) {
	if held.file == nil || held.state.Mode&unix.S_IFMT != unix.S_IFREG || held.state.Links != 1 {
		return nil, auxiliaryFailure("INVALID_INPUT", "capture requires a pinned single-link regular auxiliary object")
	}
	before, err := auxiliaryMetadata(ctx, held.file)
	if err != nil {
		return nil, err
	}
	if before != held.state {
		return nil, auxiliaryFailure("SOURCE_CHANGED", "auxiliary source changed before its held-inode read open")
	}
	fd, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", held.file.Fd()), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOATIME|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "held auxiliary capture reader")
	after, err := auxiliaryMetadata(ctx, f)
	if err == nil && after != held.state {
		err = auxiliaryFailure("SOURCE_CHANGED", "auxiliary source changed while reopening its held inode")
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (c *AuxiliaryCapture) archiveReader() (io.Reader, int64, map[string]hash.Hash, error) {
	parts := make([]io.Reader, 0, 3*len(c.scan.inventory.Members)+1)
	sums := make(map[string]hash.Hash, len(c.scan.inventory.Members))
	size := int64(1024) // two USTAR end blocks
	for i, member := range c.scan.inventory.Members {
		if member.ID != fmt.Sprintf("members/%03d", i) || member.Kind != "nvram" && member.Kind != "tpm" || member.State.Size > MaxAuxiliaryPayloadBytes || c.readers[member.ID] == nil {
			return nil, 0, nil, auxiliaryFailure("INVALID_INPUT", "invalid ordered auxiliary archive member")
		}
		payload := int64(member.State.Size)
		padding := (512 - payload%512) % 512
		if 512+payload+padding > MaxAuxiliarySnapshotBytes-size {
			return nil, 0, nil, auxiliaryFailure("UNSUPPORTED_CAPABILITY", "auxiliary USTAR exceeds anonymous snapshot bound")
		}
		size += 512 + payload + padding
		var header bytes.Buffer
		writer := tar.NewWriter(&header)
		if err := writer.WriteHeader(&tar.Header{Name: member.ID, Typeflag: tar.TypeReg, Mode: 0600, Size: payload, ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR}); err != nil {
			return nil, 0, nil, err
		}
		// Only the fixed header is used; SectionReader supplies the declared
		// content without buffering it or letting any member exceed its bound.
		if header.Len() != 512 {
			return nil, 0, nil, auxiliaryFailure("INVALID_INPUT", "USTAR header is not one fixed block")
		}
		h := sha256.New()
		sums[member.ID] = h
		parts = append(parts, bytes.NewReader(header.Bytes()), io.TeeReader(io.NewSectionReader(c.readers[member.ID], 0, payload), h), bytes.NewReader(make([]byte, padding)))
	}
	parts = append(parts, bytes.NewReader(make([]byte, 1024)))
	return io.MultiReader(parts...), size, sums, nil
}

// Recheck refuses a changed native state, membership, path association, held
// generation, access ACL or label. It leaves custody intact even on failure so
// the caller controls final cleanup; it never authorizes reuse after drift.
func (c *AuxiliaryCapture) Recheck(ctx context.Context) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() {
		if err != nil && c.failure == nil {
			c.failure = err
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.closed || c.scan == nil {
		return auxiliaryFailure("INVALID_STATE", "auxiliary capture is closed or unavailable")
	}
	if c.failure != nil {
		return c.failure
	}
	layout, err := c.executor.native(ctx, c.request)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(layout, c.scan.inventory.Layout) {
		return auxiliaryFailure("SOURCE_CHANGED", "native auxiliary layout changed during capture custody")
	}
	again, err := c.executor.scanAuxiliary(ctx, c.request, c.policy, c.scan.permission, layout)
	if err != nil {
		return err
	}
	defer again.close()
	if !reflect.DeepEqual(c.scan.inventory, again.inventory) {
		return auxiliaryFailure("SOURCE_CHANGED", "complete auxiliary inventory changed during capture custody")
	}
	for _, member := range c.scan.inventory.Members {
		state, err := auxiliaryMetadata(ctx, c.readers[member.ID])
		if err != nil {
			return err
		}
		if state != member.State {
			return auxiliaryFailure("SOURCE_CHANGED", "held auxiliary capture reader changed")
		}
	}
	for _, guard := range c.guards {
		state, err := auxiliaryMetadata(ctx, guard.file)
		if err != nil {
			return err
		}
		if state != guard.state {
			return auxiliaryFailure("SOURCE_CHANGED", "held auxiliary producer guard changed")
		}
	}
	layout, err = c.executor.native(ctx, c.request)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(layout, c.scan.inventory.Layout) {
		return auxiliaryFailure("SOURCE_CHANGED", "native auxiliary layout changed before capture recheck completed")
	}
	if err = c.scan.recheck(ctx); err != nil {
		return err
	}
	if c.snapshot != nil {
		if err = validateSealed(c.snapshot); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (c *AuxiliaryCapture) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var result error
	if c.snapshot != nil {
		result = errors.Join(result, c.snapshot.Close())
	}
	for _, reader := range c.readers {
		result = errors.Join(result, reader.Close())
	}
	for _, guard := range c.guards {
		result = errors.Join(result, guard.file.Close())
	}
	if c.scan != nil {
		c.scan.close()
	}
	return result
}
