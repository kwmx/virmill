//go:build linux && amd64

package localbackup

import (
	"context"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const backupReceiptLimit = 64 << 10

func receiptActor(ctx context.Context, uid uint32, r app.Request) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if uid == 0 || uid != uint32(os.Geteuid()) {
		return domain.Fail("PERMISSION_DENIED", "backup receipts require the current ordinary local user")
	}
	if r.Connection != "qemu:///system" && r.Connection != "qemu:///session" {
		return invalid("backup receipts require an explicit local connection")
	}
	if r.Action != "" || r.After != 0 || r.Apply != nil || len(r.Input) != 0 {
		return invalid("backup receipt reads accept no actions or mutation inputs")
	}
	return nil
}

func validateBackupReceipt(receipt domain.BackupReceipt) error {
	if receipt.APIVersion != domain.APIVersion || receipt.Kind != "BackupReceipt" || !validID(receipt.OperationID) || !validID(receipt.CaptureID) || !validHash(receipt.SnapshotID) || !validHash(receipt.ManifestSHA256) || !localPath(receipt.Repository) || receipt.VerifiedAt.IsZero() || receipt.Connection != "qemu:///system" && receipt.Connection != "qemu:///session" {
		return invalid("backup receipt requires virmill/v1 identity, a local repository, exact backup hashes and a verification date")
	}
	return nil
}

// Receipt exports recovery coordinates only after Result verifies the durable
// successful create proof. It does not touch the repository or any credentials.
func (s *Service) Receipt(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if err := receiptActor(ctx, uid, r); err != nil {
		return nil, err
	}
	if !validID(r.ID) || r.Path != "" {
		return nil, invalid("backup receipt requires one completed backup operation UUID")
	}
	if s.Engine == nil || s.Engine.Store == nil {
		return nil, invalid("backup operation history is unavailable")
	}
	result, err := s.Result(ctx, uid, r)
	if err != nil {
		return nil, err
	}
	proof, ok := result.(*Proof)
	if !ok || proof == nil || proof.Kind != "create" || !proof.RoundtripVerified {
		return nil, invalid("only a completed, roundtrip-verified backup create operation has a recovery receipt")
	}
	receipt := domain.BackupReceipt{APIVersion: domain.APIVersion, Kind: "BackupReceipt", OperationID: proof.OperationID, Connection: proof.Connection, Repository: proof.Repository, CaptureID: proof.CaptureID, SnapshotID: proof.SnapshotID, ManifestSHA256: proof.ManifestSHA256, VerifiedAt: proof.VerifiedAt}
	if err = validateBackupReceipt(receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

// Receipts lists verified backups among Store.Jobs' most recent 1000 operations.
// It is deliberately recent history, not a repository inventory or a complete
// archival catalog; exported receipts remain usable without this database.
func (s *Service) Receipts(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if err := receiptActor(ctx, uid, r); err != nil {
		return nil, err
	}
	if r.ID != "" || r.Path != "" {
		return nil, invalid("recent backup receipts accept no target")
	}
	if s.Engine == nil || s.Engine.Store == nil {
		return nil, invalid("backup operation history is unavailable")
	}
	jobs, err := s.Engine.Store.Jobs()
	if err != nil {
		return nil, err
	}
	receipts := []domain.BackupReceipt{}
	for _, job := range jobs {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if job.State != "succeeded" {
			continue
		}
		plan, _, err := s.Engine.Store.Plan(job.PlanID)
		if err != nil {
			return nil, err
		}
		if plan.Operation != operation("create") || plan.ActorUID != uid || plan.ConnectionID != r.Connection {
			continue
		}
		value, err := s.Receipt(ctx, uid, app.Request{Connection: r.Connection, ID: job.ID})
		if err != nil {
			return nil, err
		}
		receipt, ok := value.(domain.BackupReceipt)
		if !ok {
			return nil, recovery("verified backup receipt projection is unavailable")
		}
		receipts = append(receipts, receipt)
	}
	return receipts, ctx.Err()
}

// ReadReceipt imports only the public receipt document. File contents are bounded
// and unambiguous; the held inode and pathname must remain identical throughout.
// A receipt conveys selectors, never successful recovery or apply authority.
func (s *Service) ReadReceipt(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if err := receiptActor(ctx, uid, r); err != nil {
		return nil, err
	}
	if r.ID != "" || !localPath(r.Path) {
		return nil, invalid("choose one canonical absolute backup receipt file path")
	}
	pin, before, err := fileidentity.Open(r.Path, false, false)
	if err != nil {
		return nil, err
	}
	defer pin.Close()
	if before.Size == 0 || before.Size > backupReceiptLimit {
		return nil, invalid("backup receipt must be nonempty and no larger than 64 KiB")
	}
	fd, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", pin.Fd()), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), r.Path)
	defer file.Close()
	opened, err := fileidentity.InspectFile(file, false)
	if err != nil || opened != before {
		return nil, domain.Fail("SOURCE_CHANGED", "backup receipt changed before reading")
	}
	lock := unix.Flock_t{Type: unix.F_RDLCK, Whence: 0, Start: 0, Len: 0}
	if err = unix.FcntlFlock(file.Fd(), unix.F_OFD_SETLK, &lock); err != nil {
		return nil, domain.Fail("RESOURCE_BUSY", "backup receipt has a conflicting writer or its read guard is unavailable")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, backupReceiptLimit+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) != before.Size {
		return nil, domain.Fail("SOURCE_CHANGED", "backup receipt size changed while reading")
	}
	after, err := fileidentity.InspectFile(file, false)
	if err != nil || after != before {
		return nil, domain.Fail("SOURCE_CHANGED", "backup receipt changed while reading")
	}
	current, err := fileidentity.Observe(r.Path, false)
	if err != nil || current != before {
		return nil, domain.Fail("SOURCE_CHANGED", "backup receipt path changed while reading")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	var receipt domain.BackupReceipt
	if err = wire.Decode(raw, &receipt); err != nil {
		return nil, invalid("choose a raw BackupReceipt document without duplicate or unknown fields")
	}
	if err = validation.Schema("backup-receipt", raw); err != nil {
		return nil, invalid("backup receipt does not match the supported virmill/v1 schema")
	}
	if err = validateBackupReceipt(receipt); err != nil {
		return nil, err
	}
	return receipt, ctx.Err()
}
