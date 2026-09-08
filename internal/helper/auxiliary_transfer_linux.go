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
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

const auxiliaryTransferTimeout = 120 * time.Second

type auxiliaryAcknowledgement struct {
	Version        int    `json:"version"`
	JobID          string `json:"jobID"`
	Binding        string `json:"binding"`
	ArtifactSHA256 string `json:"artifactSHA256"`
}

type auxiliaryDeliveryIntent struct {
	Version int     `json:"version"`
	Binding string  `json:"binding"`
	Request Request `json:"request"`
}

// The new transfer protocol uses the exact serialized typed shape. Comparing
// canonical JSON after strict decoding also rejects Go's case-insensitive field
// aliases and null/missing substitutes, without requiring object key order.
func decodeAuxiliaryTransfer(data []byte, out any) error {
	if err := wire.Decode(data, out); err != nil {
		return err
	}
	raw, err := operations.Canonical(json.RawMessage(data))
	if err != nil {
		return err
	}
	typed, err := operations.Canonical(out)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, typed) {
		return domain.Fail("RECOVERY_REQUIRED", "auxiliary transfer JSON differs from its exact typed shape")
	}
	return nil
}

// AuxiliaryTransfer retains the received immutable snapshot and dedicated
// authenticated connection until Close. Commit is explicit and single-use: the
// coordinator must first accept the verified artifact into its own capture.
// Delivered means helper delivery was acknowledged, not complete VM recovery.
type AuxiliaryTransfer struct {
	Response AuxiliaryResponse
	Snapshot *os.File

	mu        sync.Mutex
	conn      *net.UnixConn
	request   Request
	captured  AuxiliaryResponse
	snapshot  *os.File
	ctx       context.Context
	cancel    context.CancelFunc
	stop      func() bool
	closed    bool
	attempted bool
}

func auxiliaryTransferContext(ctx context.Context, expiry time.Time) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(auxiliaryTransferTimeout)
	if !expiry.IsZero() && expiry.Before(deadline) {
		deadline = expiry
	}
	return context.WithDeadline(ctx, deadline)
}

func validateAuxiliaryTransferRequest(r Request, mode string) error {
	if r.APIVersion != domain.APIVersion || r.Operation != "state.auxiliary" || r.Mode != mode || r.Access != nil || r.Network != nil || r.Auxiliary == nil || r.Auxiliary.Version != 1 || !auxiliaryID(r.ResourceID) || !auxiliaryID(r.JobID) || !auxiliaryDigest(r.PlanDigest) || !auxiliaryDigest(r.Auxiliary.Fingerprint) {
		return domain.Fail("INVALID_INPUT", "exact versioned auxiliary capture/observation request required")
	}
	in := r.Auxiliary.Expected
	if in == nil || in.Version != 1 || in.Resource != (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: r.ResourceID}) || in.Fingerprint != r.Auxiliary.Fingerprint || in.Layout.VMID != r.ResourceID || in.Root.ID != r.RootID || !auxiliaryAbsolutePath(in.Root.Path) || in.Directories == nil || len(in.Directories) > auxiliaryDirectoryLimit || len(in.Members) == 0 || len(in.Members) > MaxAuxiliaryMembers || in.TotalBytes > MaxAuxiliaryPayloadBytes {
		return domain.Fail("INVALID_INPUT", "complete reviewed auxiliary inventory required")
	}
	var total uint64
	for i, m := range in.Members {
		if m.ID != fmt.Sprintf("members/%03d", i) || m.Kind != "nvram" && m.Kind != "tpm" || !auxiliaryCleanPath(m.RelativePath, false) || m.State.Size > MaxAuxiliaryPayloadBytes-total {
			return domain.Fail("INVALID_INPUT", "bounded ordered auxiliary payload inventory required")
		}
		total += m.State.Size
	}
	if total != in.TotalBytes {
		return domain.Fail("INVALID_INPUT", "auxiliary payload total differs from reviewed members")
	}
	_, err := auxiliaryInventoryCopy(in)
	return err
}

// ValidateAuxiliaryDelivery validates bound metadata only. The dedicated client
// also verifies seals and every byte of the canonical archive before returning
// captured custody and again before Commit. Observe supplies durable historical
// metadata; it cannot recreate a lost snapshot or certify current native state.
func ValidateAuxiliaryDelivery(r Request, response *AuxiliaryResponse) error {
	invalid := func() error {
		return domain.Fail("RECOVERY_REQUIRED", "helper delivery does not prove the exact reviewed auxiliary artifact")
	}
	if r.Mode != "capture" && r.Mode != "observe" {
		return invalid()
	}
	if err := validateAuxiliaryTransferRequest(r, r.Mode); err != nil {
		return err
	}
	binding, err := AuxiliaryBinding(r)
	if err != nil {
		return err
	}
	if response == nil || response.Version != 1 || response.JobID != r.JobID || response.Binding != binding || response.Stage != "captured" && response.Stage != "delivered" || !reflect.DeepEqual(response.Inventory, r.Auxiliary.Expected) || response.Artifact == nil {
		return invalid()
	}
	a := response.Artifact
	digest, err := operations.Digest(response.Inventory)
	if err != nil {
		return err
	}
	if a.Format != "ustar-v1" || a.InventoryDigest != digest || !auxiliaryDigest(a.SHA256) || len(a.MemberSHA256) != len(response.Inventory.Members) {
		return invalid()
	}
	size := uint64(1024)
	for _, member := range response.Inventory.Members {
		if !auxiliaryDigest(a.MemberSHA256[member.ID]) {
			return invalid()
		}
		size += 512 + (member.State.Size+511)/512*512
	}
	if a.Size != size || a.Size > uint64(MaxAuxiliarySnapshotBytes) {
		return invalid()
	}
	return nil
}

func auxiliaryResponseCopy(in AuxiliaryResponse) (AuxiliaryResponse, error) {
	data, err := json.Marshal(in)
	if err != nil || len(data) > maxSnapshotEnvelope {
		return AuxiliaryResponse{}, domain.Fail("INVALID_INPUT", "auxiliary response exceeds metadata bound")
	}
	var out AuxiliaryResponse
	if err = wire.Decode(data, &out); err != nil {
		return AuxiliaryResponse{}, err
	}
	if !reflect.DeepEqual(in, out) {
		return AuxiliaryResponse{}, domain.Fail("INVALID_INPUT", "auxiliary response cannot preserve its metadata identity")
	}
	return out, nil
}

func auxiliaryArchiveHeader(m AuxiliaryMember) ([]byte, error) {
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	if err := tw.WriteHeader(&tar.Header{Name: m.ID, Typeflag: tar.TypeReg, Mode: 0600, Size: int64(m.State.Size), ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR}); err != nil {
		return nil, err
	}
	if b.Len() != 512 {
		return nil, domain.Fail("RECOVERY_REQUIRED", "auxiliary USTAR header must be exactly one block")
	}
	return b.Bytes(), nil
}

func validateAuxiliaryArchive(ctx context.Context, r Request, response *AuxiliaryResponse, f *os.File) error {
	if err := ValidateAuxiliaryDelivery(r, response); err != nil {
		return err
	}
	if err := validateSealed(f); err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil || uint64(st.Size()) != response.Artifact.Size {
		return domain.Fail("RECOVERY_REQUIRED", "sealed auxiliary archive size differs from metadata")
	}
	archiveHash := sha256.New()
	reader := io.TeeReader(io.NewSectionReader(f, 0, st.Size()), archiveHash)
	buf := make([]byte, 64<<10)
	for _, member := range response.Inventory.Members {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := auxiliaryArchiveHeader(member)
		if err != nil {
			return err
		}
		if _, err = io.ReadFull(reader, buf[:512]); err != nil || !bytes.Equal(buf[:512], header) {
			return domain.Fail("RECOVERY_REQUIRED", "sealed auxiliary archive has a noncanonical member header")
		}
		h := sha256.New()
		for remaining := member.State.Size; remaining > 0; {
			if err := ctx.Err(); err != nil {
				return err
			}
			count := min(remaining, uint64(len(buf)))
			if _, err = io.ReadFull(reader, buf[:count]); err != nil {
				return domain.Fail("RECOVERY_REQUIRED", "sealed auxiliary archive member is incomplete")
			}
			_, _ = h.Write(buf[:count])
			remaining -= count
		}
		if hex.EncodeToString(h.Sum(nil)) != response.Artifact.MemberSHA256[member.ID] {
			return domain.Fail("RECOVERY_REQUIRED", "sealed auxiliary member digest differs")
		}
		padding := (512 - member.State.Size%512) % 512
		if _, err = io.ReadFull(reader, buf[:padding]); err != nil || !bytes.Equal(buf[:padding], make([]byte, padding)) {
			return domain.Fail("RECOVERY_REQUIRED", "sealed auxiliary archive padding differs")
		}
	}
	if _, err = io.ReadFull(reader, buf[:1024]); err != nil || !bytes.Equal(buf[:1024], make([]byte, 1024)) || hex.EncodeToString(archiveHash.Sum(nil)) != response.Artifact.SHA256 {
		return domain.Fail("RECOVERY_REQUIRED", "sealed auxiliary archive footer or digest differs")
	}
	if n, err := reader.Read(buf[:1]); n != 0 || err != io.EOF {
		return domain.Fail("RECOVERY_REQUIRED", "sealed auxiliary archive contains trailing data")
	}
	return ctx.Err()
}

func receiveAuxiliaryDelivery(ctx context.Context, conn *net.UnixConn, r Request, stage string, wantFD bool) (_ AuxiliaryResponse, resultFile *os.File, err error) {
	frame, f, err := receiveSnapshotFrame(ctx, conn, true)
	defer func() {
		if f != nil && resultFile == nil {
			_ = f.Close()
		}
	}()
	if err != nil {
		if ctx.Err() != nil {
			return AuxiliaryResponse{}, nil, ctx.Err()
		}
		return AuxiliaryResponse{}, nil, err
	}
	var result Response
	if err = decodeAuxiliaryTransfer(frame, &result); err != nil {
		return AuxiliaryResponse{}, nil, err
	}
	if result.APIVersion != domain.APIVersion || result.Access != nil || result.Network != nil {
		return AuxiliaryResponse{}, nil, domain.Fail("RECOVERY_REQUIRED", "unexpected auxiliary response family")
	}
	if !result.Success {
		if f != nil || result.Auxiliary != nil || result.Error == "" {
			return AuxiliaryResponse{}, nil, domain.Fail("RECOVERY_REQUIRED", "ambiguous auxiliary transfer refusal")
		}
		code := result.ErrorCode
		switch code {
		case "PERMISSION_DENIED", "SOURCE_CHANGED", "STALE_PLAN", "UNSUPPORTED_CAPABILITY", "INVALID_INPUT", "INVALID_STATE", "INCOMPLETE_BACKUP", "RESOURCE_BUSY", "RECOVERY_REQUIRED", "OPERATION_FAILED":
		default:
			code = "OPERATION_FAILED"
		}
		return AuxiliaryResponse{}, nil, domain.Fail(code, result.Error)
	}
	if result.Error != "" || result.ErrorCode != "" || result.Auxiliary == nil || result.Auxiliary.Stage != stage || (f != nil) != wantFD {
		return AuxiliaryResponse{}, nil, domain.Fail("RECOVERY_REQUIRED", "auxiliary transfer stage or descriptor count differs")
	}
	if err = ValidateAuxiliaryDelivery(r, result.Auxiliary); err != nil {
		return AuxiliaryResponse{}, nil, err
	}
	if wantFD {
		if err = validateAuxiliaryArchive(ctx, r, result.Auxiliary, f); err != nil {
			return AuxiliaryResponse{}, nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return AuxiliaryResponse{}, nil, err
	}
	return *result.Auxiliary, f, nil
}

func receiveAuxiliaryTransfer(ctx context.Context, conn *net.UnixConn, r Request, cancel context.CancelFunc) (*AuxiliaryTransfer, error) {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	result, f, err := receiveAuxiliaryDelivery(ctx, conn, r, "captured", true)
	if err != nil {
		stop()
		conn.Close()
		cancel()
		return nil, err
	}
	stored, err := auxiliaryResponseCopy(result)
	if err == nil {
		var expected *AuxiliaryInventory
		expected, err = auxiliaryInventoryCopy(r.Auxiliary.Expected)
		if err == nil {
			aux := *r.Auxiliary
			aux.Expected = expected
			r.Auxiliary = &aux
		}
	}
	if err != nil {
		stop()
		f.Close()
		conn.Close()
		cancel()
		return nil, err
	}
	return &AuxiliaryTransfer{Response: result, Snapshot: f, captured: stored, snapshot: f, conn: conn, ctx: ctx, cancel: cancel, stop: stop, request: r}, nil
}

func (c Client) CaptureAuxiliary(ctx context.Context, r Request) (*AuxiliaryTransfer, error) {
	if err := validateAuxiliaryTransferRequest(r, "capture"); err != nil {
		return nil, err
	}
	ctx, cancel := auxiliaryTransferContext(ctx, r.ExpiresAt)
	conn, r, err := c.connectRequest(ctx, r)
	if err != nil {
		cancel()
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	return receiveAuxiliaryTransfer(ctx, conn, r, cancel)
}

func (c Client) ObserveAuxiliary(ctx context.Context, r Request) (AuxiliaryResponse, error) {
	if err := validateAuxiliaryTransferRequest(r, "observe"); err != nil {
		return AuxiliaryResponse{}, err
	}
	ctx, cancel := auxiliaryTransferContext(ctx, r.ExpiresAt)
	defer cancel()
	conn, r, err := c.connectRequest(ctx, r)
	if err != nil {
		return AuxiliaryResponse{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	result, _, err := receiveAuxiliaryDelivery(ctx, conn, r, "delivered", false)
	return result, err
}

func writeAuxiliaryFrame(ctx context.Context, conn *net.UnixConn, value any) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > maxSnapshotEnvelope {
		return domain.Fail("INVALID_INPUT", "bounded auxiliary metadata frame required")
	}
	data = append(data, '\n')
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return ctx.Err()
}

func (t *AuxiliaryTransfer) Commit(ctx context.Context) (_ AuxiliaryResponse, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.attempted || t.conn == nil {
		return AuxiliaryResponse{}, domain.Fail("RECOVERY_REQUIRED", "auxiliary commit is unavailable or was already attempted; observe durable delivery")
	}
	t.attempted = true
	defer func() {
		if err != nil {
			_ = t.closeLocked()
		}
	}()
	if err = t.ctx.Err(); err != nil {
		return AuxiliaryResponse{}, err
	}
	commitCtx, cancel := context.WithCancel(t.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	stopConnection := context.AfterFunc(commitCtx, func() { _ = t.conn.Close() })
	defer stopConnection()
	if err = ctx.Err(); err != nil {
		return AuxiliaryResponse{}, err
	}
	if !reflect.DeepEqual(t.Response, t.captured) || t.Snapshot != t.snapshot {
		return AuxiliaryResponse{}, domain.Fail("RECOVERY_REQUIRED", "caller changed the captured auxiliary response or descriptor")
	}
	if err = validateAuxiliaryArchive(commitCtx, t.request, &t.captured, t.snapshot); err != nil {
		return AuxiliaryResponse{}, err
	}
	ack := auxiliaryAcknowledgement{Version: 1, JobID: t.captured.JobID, Binding: t.captured.Binding, ArtifactSHA256: t.captured.Artifact.SHA256}
	if err = writeAuxiliaryFrame(commitCtx, t.conn, ack); err != nil {
		return AuxiliaryResponse{}, err
	}
	result, _, err := receiveAuxiliaryDelivery(commitCtx, t.conn, t.request, "delivered", false)
	if err != nil {
		return AuxiliaryResponse{}, err
	}
	want := t.captured
	want.Stage = "delivered"
	if !reflect.DeepEqual(want, result) {
		return AuxiliaryResponse{}, domain.Fail("RECOVERY_REQUIRED", "delivered auxiliary artifact differs from the acknowledged capture")
	}
	if err = commitCtx.Err(); err != nil {
		return AuxiliaryResponse{}, err
	}
	return result, nil
}

func (t *AuxiliaryTransfer) closeLocked() error {
	if t.closed {
		return nil
	}
	t.closed = true
	if t.stop != nil {
		t.stop()
	}
	var err error
	if t.conn != nil {
		err = errors.Join(err, t.conn.Close())
	}
	if t.snapshot != nil {
		err = errors.Join(err, t.snapshot.Close())
	}
	if t.cancel != nil {
		t.cancel()
	}
	return err
}

func (t *AuxiliaryTransfer) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closeLocked()
}

// Test-only seams remain private and cannot be populated from a request,
// administrator policy, executable argument or environmental override.
type auxiliaryTransferRuntime struct {
	journalDirectory string
	journalOwnerUID  uint32
	policy           func() (Policy, error)
	beforePublish    func(string) error
}

func serveAuxiliaryCapture(ctx context.Context, conn *net.UnixConn, e *AuxiliaryExecutor, r Request, p Policy) error {
	return (auxiliaryTransferRuntime{}).serve(ctx, conn, e, r, p)
}

func (rt auxiliaryTransferRuntime) serve(ctx context.Context, conn *net.UnixConn, e *AuxiliaryExecutor, r Request, p Policy) error {
	if conn == nil || r.Mode != "capture" && r.Mode != "observe" {
		return domain.Fail("INVALID_INPUT", "dedicated auxiliary capture/observe connection required")
	}
	ctx, cancel := auxiliaryTransferContext(ctx, r.ExpiresAt)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	cred, _, err := PeerGroups(conn)
	if err != nil {
		return err
	}
	if err = Authorize(cred.Uid, r, p, time.Now()); err != nil {
		return domain.Fail("PERMISSION_DENIED", err.Error())
	}
	if err = validateAuxiliaryTransferRequest(r, r.Mode); err != nil {
		return err
	}
	initialGrant, err := auxiliaryPolicy(r, p)
	if err != nil {
		return err
	}
	initialRoot := p.Roots[r.RootID]
	binding, err := AuxiliaryBinding(r)
	if err != nil {
		return err
	}
	journal, err := rt.openJournal()
	if err != nil {
		return err
	}
	defer journal.Close()
	intentName, deliveredName := r.JobID+".auxiliary-intent.json", r.JobID+".auxiliary-delivered.json"
	if r.Mode == "observe" {
		var intent auxiliaryDeliveryIntent
		var delivered AuxiliaryResponse
		if err = rt.readRecord(ctx, journal, intentName, &intent); err != nil {
			return domain.Fail("RECOVERY_REQUIRED", "auxiliary capture intent unavailable; never recopy during observation")
		}
		originalBinding, bindErr := AuxiliaryBinding(intent.Request)
		if bindErr != nil || intent.Version != 1 || intent.Binding != binding || originalBinding != binding || intent.Request.Mode != "capture" || validateAuxiliaryTransferRequest(intent.Request, "capture") != nil {
			return domain.Fail("RECOVERY_REQUIRED", "durable auxiliary capture intent differs from observation")
		}
		if err = rt.readRecord(ctx, journal, deliveredName, &delivered); err != nil {
			return domain.Fail("RECOVERY_REQUIRED", "auxiliary delivery is incomplete or unavailable; retained intent forbids replay")
		}
		if delivered.Stage != "delivered" || ValidateAuxiliaryDelivery(r, &delivered) != nil {
			return domain.Fail("RECOVERY_REQUIRED", "durable auxiliary delivery metadata differs from the exact binding")
		}
		return writeAuxiliaryFrame(ctx, conn, Response{APIVersion: domain.APIVersion, Success: true, Auxiliary: &delivered})
	}
	if e == nil {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "native auxiliary capture executor unavailable")
	}
	// Both names are exclusive. Even a malformed or incomplete previous record
	// makes this job ineligible for another copy or implicit retry.
	for _, name := range []string{intentName, deliveredName} {
		var st unix.Stat_t
		err = unix.Fstatat(int(journal.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
		if err == nil || !errors.Is(err, unix.ENOENT) {
			return domain.Fail("RECOVERY_REQUIRED", "auxiliary job already has evidence or journal state is uncertain; observe without recopy")
		}
	}
	if err = rt.writeRecord(ctx, journal, intentName, auxiliaryDeliveryIntent{Version: 1, Binding: binding, Request: r}); err != nil {
		return err
	}
	capture, err := e.Capture(ctx, r, p)
	if err != nil {
		return err
	}
	defer capture.Close()
	if err = ValidateAuxiliaryDelivery(r, &capture.Response); err != nil {
		return err
	}
	frame, err := json.Marshal(Response{APIVersion: domain.APIVersion, Success: true, Auxiliary: &capture.Response})
	if err != nil {
		return err
	}
	if err = SendSealedSnapshot(ctx, conn, frame, capture.Snapshot); err != nil {
		return err
	}
	ackData, ackFD, err := receiveSnapshotFrame(ctx, conn, true)
	if ackFD != nil {
		ackFD.Close()
		return domain.Fail("RECOVERY_REQUIRED", "auxiliary delivery acknowledgement carried an unexpected descriptor")
	}
	if err != nil {
		return err
	}
	var ack auxiliaryAcknowledgement
	if err = decodeAuxiliaryTransfer(ackData, &ack); err != nil {
		return err
	}
	wantAck := auxiliaryAcknowledgement{Version: 1, JobID: r.JobID, Binding: binding, ArtifactSHA256: capture.Response.Artifact.SHA256}
	if ack != wantAck {
		return domain.Fail("RECOVERY_REQUIRED", "auxiliary acknowledgement differs from captured artifact or job binding")
	}
	load := rt.policy
	if load == nil {
		load = loadPolicy
	}
	currentPolicy, err := load()
	if err != nil {
		return err
	}
	if err = Authorize(cred.Uid, r, currentPolicy, time.Now()); err != nil {
		return domain.Fail("PERMISSION_DENIED", err.Error())
	}
	currentGrant, grantErr := auxiliaryPolicy(r, currentPolicy)
	if grantErr != nil || initialGrant != currentGrant || initialRoot != currentPolicy.Roots[r.RootID] {
		return domain.Fail("PERMISSION_DENIED", "auxiliary policy changed before acknowledged delivery")
	}
	if err = capture.Recheck(ctx); err != nil {
		return err
	}
	delivered := capture.Response
	delivered.Stage = "delivered"
	if err = rt.writeRecord(ctx, journal, deliveredName, delivered); err != nil {
		return err
	}
	return writeAuxiliaryFrame(ctx, conn, Response{APIVersion: domain.APIVersion, Success: true, Auxiliary: &delivered})
}

func (rt auxiliaryTransferRuntime) openJournal() (*os.File, error) {
	path := rt.journalDirectory
	if path == "" {
		path = JournalPath
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, domain.Fail("INVALID_INPUT", "canonical private helper journal required")
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "held auxiliary delivery journal")
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil || st.Uid != rt.journalOwnerUID || st.Mode&0777 != 0700 {
		f.Close()
		return nil, domain.Fail("PERMISSION_DENIED", "auxiliary journal must be an owner-only administrator directory")
	}
	return f, nil
}

func (rt auxiliaryTransferRuntime) writeRecord(ctx context.Context, dir *os.File, name string, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if rt.beforePublish != nil {
		if err := rt.beforePublish(name); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := rt.checkJournal(dir); err != nil {
		return err
	}
	if filepath.Base(name) != name {
		return domain.Fail("INVALID_INPUT", "invalid auxiliary journal record name")
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) > maxSnapshotEnvelope {
		return domain.Fail("INVALID_INPUT", "auxiliary journal metadata exceeds bound")
	}
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return domain.Fail("RECOVERY_REQUIRED", "auxiliary journal record already exists or cannot be published")
	}
	f := os.NewFile(uintptr(fd), "new auxiliary metadata record")
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err == nil {
		err = dir.Sync()
	}
	if err != nil {
		return err
	}
	if err = rt.checkJournal(dir); err != nil {
		return err
	}
	return ctx.Err()
}

func (rt auxiliaryTransferRuntime) readRecord(ctx context.Context, dir *os.File, name string, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if filepath.Base(name) != name {
		return domain.Fail("INVALID_INPUT", "invalid auxiliary journal record name")
	}
	if err := rt.checkJournal(dir); err != nil {
		return err
	}
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	pin := os.NewFile(uintptr(fd), "pinned auxiliary metadata record")
	defer pin.Close()
	var before unix.Stat_t
	if err = unix.Fstat(fd, &before); err != nil || before.Uid != rt.journalOwnerUID || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0777 != 0600 || before.Nlink != 1 || before.Size <= 0 || before.Size > maxSnapshotEnvelope {
		return domain.Fail("RECOVERY_REQUIRED", "auxiliary record is not bounded private single-link regular metadata")
	}
	readFD, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", fd), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOATIME, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(readFD), "held auxiliary metadata reader")
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSnapshotEnvelope+1))
	if err != nil || int64(len(data)) != before.Size {
		return domain.Fail("RECOVERY_REQUIRED", "auxiliary record read is incomplete or changed")
	}
	var after, named unix.Stat_t
	if err = unix.Fstat(readFD, &after); err != nil {
		return err
	}
	if err = unix.Fstatat(int(dir.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if before != after || before != named {
		return domain.Fail("RECOVERY_REQUIRED", "auxiliary record generation or rooted association changed")
	}
	if err = decodeAuxiliaryTransfer(data, out); err != nil {
		return err
	}
	if err = rt.checkJournal(dir); err != nil {
		return err
	}
	return ctx.Err()
}

func (rt auxiliaryTransferRuntime) checkJournal(dir *os.File) error {
	path := rt.journalDirectory
	if path == "" {
		path = JournalPath
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var held, current unix.Stat_t
	if err = unix.Fstat(int(dir.Fd()), &held); err != nil {
		return err
	}
	if err = unix.Fstat(fd, &current); err != nil {
		return err
	}
	if held.Dev != current.Dev || held.Ino != current.Ino || held.Uid != rt.journalOwnerUID || current.Uid != rt.journalOwnerUID || held.Mode&0777 != 0700 || current.Mode&0777 != 0700 || current.Nlink == 0 {
		return domain.Fail("RECOVERY_REQUIRED", "private auxiliary journal identity or permissions changed")
	}
	return nil
}
