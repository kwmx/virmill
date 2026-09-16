//go:build linux && amd64

package importing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

var sourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

type EmptyDiskTool interface {
	Identity(context.Context) (image.Identity, error)
	CreateEmpty(context.Context, string, int64, int64) error
}
type InstallationService struct {
	*Service
	EmptyTool EmptyDiskTool
}
type blankDisk struct {
	ID           string `json:"id"`
	VirtualBytes int64  `json:"virtualBytes"`
}
type installationRequest struct {
	OfflineSources bool        `json:"offlineSources"`
	Destination    string      `json:"destination"`
	SHA256         string      `json:"sha256,omitempty"`
	MediaID        string      `json:"mediaID"`
	Disks          []blankDisk `json:"disks"`
}
type installationInput struct {
	SourceLockProtocol string         `json:"sourceLockProtocol"`
	SourceDirectory    string         `json:"sourceDirectory"`
	SourceIdentity     identity       `json:"sourceIdentity"`
	Files              []DiskSetFile  `json:"files"`
	Destination        string         `json:"destination"`
	ParentIdentity     identity       `json:"parentIdentity"`
	MediaID            string         `json:"mediaID"`
	Disks              []blankDisk    `json:"disks"`
	Recognition        string         `json:"recognition"`
	Tool               image.Identity `json:"tool"`
	RequiredFreeBytes  int64          `json:"requiredFreeBytes"`
}

func installStage(p domain.Plan) string { return ".virmill-install-" + p.ID }
func installStagePath(p domain.Plan, in installationInput) string {
	return filepath.Join(filepath.Dir(in.Destination), installStage(p))
}

// recognizeMedia reads only bounded volume-recognition identifiers. It is not a
// filesystem parser, El Torito validator, publisher check or bootability claim.
func recognizeMedia(f *os.File, size int64) (string, error) {
	if size < 17*2048 || size > 64<<30 || size%2048 != 0 {
		return "", domain.Fail("INVALID_INPUT", "installation media must be a sector-aligned ordinary file between 34 KiB and 64 GiB")
	}
	iso, began, udf, ended := false, false, false, false
	for sector := int64(16); sector < 80 && (sector+1)*2048 <= size; sector++ {
		var header [7]byte
		if _, err := f.ReadAt(header[:], sector*2048); err != nil {
			return "", err
		}
		signature := string(header[1:6])
		if signature == "CD001" && header[0] == 1 && header[6] == 1 {
			iso = true
		}
		if header[0] == 0 && header[6] == 1 {
			switch signature {
			case "BEA01":
				began = true
			case "NSR02", "NSR03":
				if began && !ended {
					udf = true
				}
			case "TEA01":
				if began && udf {
					ended = true
				}
			}
		}
	}
	if iso && udf && ended {
		return "iso9660+udf-volume-identifiers", nil
	}
	if iso {
		return "iso9660-volume-identifier", nil
	}
	if udf && ended {
		return "udf-volume-identifiers", nil
	}
	return "", domain.Fail("UNSUPPORTED", "no supported ISO9660/UDF volume identifiers in the bounded recognition window; media bootability is not inferred")
}
func installationBudget(in installationInput) (int64, error) {
	if len(in.Files) != 1 || len(in.Disks) < 1 || len(in.Disks) > 64 || !sourceIDPattern.MatchString(in.MediaID) {
		return 0, domain.Fail("INVALID_INPUT", "one media source and 1–64 explicitly named blank disks required")
	}
	seen := map[string]bool{in.MediaID: true}
	total := int64(64<<20) + in.Files[0].Identity.Size
	for _, d := range in.Disks {
		if seen[d.ID] || !sourceIDPattern.MatchString(d.ID) || d.VirtualBytes < 1<<20 || d.VirtualBytes > 512<<30 || d.VirtualBytes%512 != 0 {
			return 0, domain.Fail("INVALID_INPUT", "unique disk/media IDs and bounded sector-aligned blank disk capacities required")
		}
		seen[d.ID] = true
		total += outputBound(d.VirtualBytes)
	}
	return total, nil
}
func (s *InstallationService) Plan(ctx context.Context, uid uint32, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	b, err := json.Marshal(r.Input)
	if err != nil {
		return empty, err
	}
	if err = validation.Schema("installation-preparation-input", b); err != nil {
		return empty, domain.Fail("INVALID_INPUT", err.Error())
	}
	var requested installationRequest
	if err = wire.Decode(b, &requested); err != nil {
		return empty, err
	}
	if !filepath.IsAbs(r.Path) || filepath.Clean(r.Path) != r.Path || !filepath.IsAbs(requested.Destination) || filepath.Clean(requested.Destination) != requested.Destination {
		return empty, domain.Fail("INVALID_INPUT", "canonical absolute media and destination paths required")
	}
	parent, root, parentID, err := parentDirectory(requested.Destination)
	if err != nil {
		return empty, err
	}
	defer parent.Close()
	defer root.Close()
	if err = ensureAbsent(root, filepath.Base(requested.Destination)); err != nil {
		return empty, err
	}
	sources, sourceID, files, err := openDiskFiles(ctx, filepath.Dir(r.Path), []DiskSetFile{{Path: filepath.Base(r.Path), SHA256: requested.SHA256, ProvidedDigest: requested.SHA256 != ""}})
	if err != nil {
		return empty, err
	}
	defer closeDiskFiles(sources)
	recognition, err := recognizeMedia(sources[0].File, files[0].Identity.Size)
	if err != nil {
		return empty, err
	}
	in := installationInput{SourceLockProtocol: image.SourceLockProtocol, SourceDirectory: filepath.Dir(r.Path), SourceIdentity: sourceID, Files: files, Destination: requested.Destination, ParentIdentity: parentID, MediaID: requested.MediaID, Disks: requested.Disks, Recognition: recognition}
	if in.RequiredFreeBytes, err = installationBudget(in); err != nil {
		return empty, err
	}
	if err = available(parent, in.RequiredFreeBytes); err != nil {
		return empty, err
	}
	if in.Tool, err = s.EmptyTool.Identity(ctx); err != nil {
		return empty, err
	}
	current, rootID, proof, err := openDiskFiles(ctx, in.SourceDirectory, in.Files)
	if err != nil {
		return empty, err
	}
	closeDiskFiles(current)
	if rootID != sourceID || !equalDiskValue(proof, files) {
		return empty, domain.Fail("SOURCE_CHANGED", "installation media changed during preview")
	}
	digest, err := sourceSetDigest(files)
	if err != nil {
		return empty, err
	}
	resources := []string{"import-artifact:local:" + in.Destination, "local-file|" + files[0].OriginalPath, fmt.Sprintf("disk-source:local:%d:%d", files[0].Identity.Device, files[0].Identity.Inode)}
	step := domain.Step{ID: "prepare", Action: "import.prepare-install", Preconditions: []string{"media identity, change time and digest unchanged", "source remains offline with cooperating writer guards", "pinned tool and publication parent, absent destination, sufficient space"}, Idempotency: "reconcile-before-retry", Compensation: "Original media remains unchanged; remove only unpublished staging after joined cancellation; retain uncertain output", Reconciliation: "Verify complete published artifact against durable receipt without repeating disk creation", CompletionPredicate: "Every blank disk is checked and zero-mapped; independent read-only media and source report are durably published"}
	return s.Engine.Plan(ctx, uid, "local", "import.prepare-install", resources, map[string]string{"source-set": digest}, in, []domain.Step{step}, []string{"write-import-artifacts", "offline-source-files"}, []string{"Copies the installer image and creates the new VM's empty disks; no VM is created or started by this step alone", "A checksum confirms the copy matches your file, not that the installer is genuine or will boot", "Keep the installer image closed in other programs while it is copied", "Nothing inside the guest is set up for you; the VM's hardware, boot order and installer disc come from its settings"})
}
func (s *InstallationService) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var in installationInput
	if err := wire.Decode(b, &in); err != nil {
		return nil, err
	}
	return map[string]any{"sourceKind": "installation-media", "files": in.Files, "mediaID": in.MediaID, "blankDisks": in.Disks, "recognition": in.Recognition, "destination": in.Destination, "stagingDirectory": installStagePath(p, in), "requiredFreeBytes": in.RequiredFreeBytes, "tool": in.Tool, "vmDefined": false, "guestBootVerified": false, "unattendedConfiguration": "none"}, nil
}
func (s *InstallationService) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	return s.validate(ctx, p, b, true)
}
func (s *InstallationService) validate(ctx context.Context, p domain.Plan, b []byte, space bool) error {
	var in installationInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	budget, err := installationBudget(in)
	if err != nil {
		return err
	}
	if budget != in.RequiredFreeBytes || in.SourceLockProtocol != image.SourceLockProtocol || !filepath.IsAbs(in.Destination) || filepath.Clean(in.Destination) != in.Destination {
		return domain.Fail("INVALID_INPUT", "invalid installation preparation recipe")
	}
	sources, id, proof, err := openDiskFiles(ctx, in.SourceDirectory, in.Files)
	if err != nil {
		return err
	}
	defer closeDiskFiles(sources)
	if id != in.SourceIdentity || !equalDiskValue(proof, in.Files) {
		return domain.Fail("SOURCE_CHANGED", "installation media changed")
	}
	recognition, err := recognizeMedia(sources[0].File, proof[0].Identity.Size)
	if err != nil {
		return err
	}
	if recognition != in.Recognition {
		return domain.Fail("SOURCE_CHANGED", "media recognition changed")
	}
	found := false
	for _, key := range p.ResourceIDs {
		found = found || key == "local-file|"+in.Files[0].OriginalPath
	}
	if !found {
		return domain.Fail("STALE_PLAN", "missing media path concurrency lock")
	}
	tool, err := s.EmptyTool.Identity(ctx)
	if err != nil {
		return err
	}
	if tool != in.Tool {
		return domain.Fail("SOURCE_CHANGED", "image tool changed")
	}
	parent, root, parentID, err := parentDirectory(in.Destination)
	if err != nil {
		return err
	}
	defer parent.Close()
	defer root.Close()
	if parentID != in.ParentIdentity {
		return domain.Fail("STALE_PLAN", "publication parent changed")
	}
	if err = ensureAbsent(root, filepath.Base(in.Destination)); err != nil {
		return err
	}
	if space {
		return available(parent, in.RequiredFreeBytes)
	}
	return nil
}
func (s *InstallationService) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (result error) {
	if err := s.cancelableDiskWork(ctx, func(worker context.Context) error { return s.Validate(worker, p, b) }); err != nil {
		return err
	}
	var in installationInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	var sources []platform.DiskSourceFile
	var id identity
	var proof []DiskSetFile
	err := s.cancelableDiskWork(ctx, func(worker context.Context) error {
		var e error
		sources, id, proof, e = openDiskFiles(worker, in.SourceDirectory, in.Files)
		return e
	})
	if err != nil {
		closeDiskFiles(sources)
		return err
	}
	defer closeDiskFiles(sources)
	if id != in.SourceIdentity || !equalDiskValue(proof, in.Files) {
		return domain.Fail("SOURCE_CHANGED", "media changed before copy")
	}
	parent, root, parentID, err := parentDirectory(in.Destination)
	if err != nil {
		return err
	}
	defer parent.Close()
	defer root.Close()
	if parentID != in.ParentIdentity {
		return domain.Fail("STALE_PLAN", "publication parent changed")
	}
	stage := installStage(p)
	if err = root.Mkdir(stage, 0700); err != nil {
		return err
	}
	work, err := root.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer work.Close()
	published := false
	defer func() {
		if errors.Is(result, operations.ErrCanceledSafely) && !published {
			if e := root.RemoveAll(stage); e != nil {
				result = fmt.Errorf("canceled but private stage removal failed: %w", e)
			}
		}
	}()
	for _, dir := range []string{"artifact/disks", "artifact/media"} {
		if err = work.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	artifactRoot, err := work.OpenRoot("artifact")
	if err != nil {
		return err
	}
	defer artifactRoot.Close()
	sourceDigest, err := sourceSetDigest(in.Files)
	if err != nil {
		return err
	}
	artifact := Artifact{APIVersion: domain.APIVersion, Kind: "PreparedInstallation", PlanID: p.ID, OperationID: operations.OperationID(ctx), InputDigest: p.InputDigest, CreatedAt: time.Now().UTC(), SourceSHA256: sourceDigest, SourceFiles: in.Files, Tool: in.Tool, System: importer.System{ID: "installation", Name: "User-selected installation media and blank disks", Items: []importer.Item{}, DiskIDs: []string{}}, Disks: []PreparedDisk{}}
	if err = operations.Note(ctx, s.Store, "Intent persisted: copy selected installation media into a private read-only artifact"); err != nil {
		return err
	}
	mediaPath := "media/media-000.iso"
	if err = s.cancelableDiskWork(ctx, func(worker context.Context) error {
		f, e := artifactRoot.OpenFile(mediaPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
		if e != nil {
			return e
		}
		n, e := io.Copy(f, contextReader{ctx: worker, r: io.NewSectionReader(sources[0].File, 0, in.Files[0].Identity.Size)})
		if e == nil && n != in.Files[0].Identity.Size {
			e = io.ErrUnexpectedEOF
		}
		if e == nil {
			e = f.Chmod(0400)
		}
		if e == nil {
			e = f.Sync()
		}
		if closeErr := f.Close(); e == nil {
			e = closeErr
		}
		return e
	}); err != nil {
		return err
	}
	// Hash the actual copied bytes before assigning the source digest to the media.
	digest, size, err := s.sealPreparedFile(ctx, artifactRoot, mediaPath, in.Files[0].Identity.Size)
	if err != nil {
		return err
	}
	if digest != in.Files[0].SHA256 || size != in.Files[0].Identity.Size {
		return domain.Fail("SOURCE_CHANGED", "copied installation media differs from approved source")
	}
	artifact.Media = []PreparedMedia{{SourceID: in.MediaID, SourcePath: in.Files[0].Path, Path: mediaPath, Format: "raw", FileBytes: size, SHA256: digest, Recognition: in.Recognition, Verification: "sha256+sector-alignment+volume-recognition"}}
	for i, disk := range in.Disks {
		name := fmt.Sprintf("disk-work-%03d", i)
		if err = work.Mkdir(name, 0700); err != nil {
			return err
		}
		if err = operations.Note(ctx, s.Store, "Intent persisted: create and verify empty independent disk "+disk.ID); err != nil {
			return err
		}
		if err = s.cancelableDiskWork(ctx, func(worker context.Context) error {
			return s.EmptyTool.CreateEmpty(worker, filepath.Join(installStagePath(p, in), name), disk.VirtualBytes, outputBound(disk.VirtualBytes))
		}); err != nil {
			return err
		}
		digest, size, err := s.sealPreparedFile(ctx, work, name+"/disk.qcow2", outputBound(disk.VirtualBytes))
		if err != nil {
			return err
		}
		target := fmt.Sprintf("disks/disk-%03d.qcow2", i)
		if err = work.Rename(name+"/disk.qcow2", "artifact/"+target); err != nil {
			return err
		}
		artifact.System.DiskIDs = append(artifact.System.DiskIDs, disk.ID)
		artifact.Disks = append(artifact.Disks, PreparedDisk{SourceID: disk.ID, Path: target, Format: "qcow2", VirtualBytes: disk.VirtualBytes, FileBytes: size, SHA256: digest, SourceChain: []image.Info{}, Verification: "virtual-size+qemu-check+zero-map"})
	}
	if err = s.cancelableDiskWork(ctx, func(worker context.Context) error { return s.validate(worker, p, b, false) }); err != nil {
		return err
	}
	report, err := operations.Canonical(map[string]any{"sourceKind": "installation-media", "sourceLockProtocol": in.SourceLockProtocol, "files": in.Files, "mediaID": in.MediaID, "blankDisks": in.Disks, "recognition": in.Recognition, "sourceHardware": "unknown", "publisherAuthenticity": "not-established", "bootability": "not-tested", "unattendedConfiguration": "none"})
	if err != nil {
		return err
	}
	if err = writeFile(artifactRoot, "installation-report.json", report); err != nil {
		return err
	}
	artifact.ReportSHA256 = hashBytes(report)
	manifest, err := operations.Canonical(artifact)
	if err != nil {
		return err
	}
	if err = validation.Schema("prepared-installation", manifest); err != nil {
		return err
	}
	if err = writeFile(artifactRoot, "manifest.json", manifest); err != nil {
		return err
	}
	for _, name := range []string{"disks", "media", "."} {
		dir, e := artifactRoot.OpenRoot(name)
		if e != nil {
			return e
		}
		e = syncRoot(dir)
		dir.Close()
		if e != nil {
			return e
		}
	}
	if canceled, e := operations.CancellationRequested(ctx, s.Store); e != nil {
		return e
	} else if canceled {
		return operations.ErrCanceledSafely
	}
	if err = operations.Note(ctx, s.Store, "Intent persisted: publish complete installation artifacts after media revalidation"); err != nil {
		return err
	}
	if err = s.Store.ComparePut("import-artifact", p.ID, nil, artifact); err != nil {
		return err
	}
	if err = unix.Renameat2(int(parent.Fd()), stage+"/artifact", int(parent.Fd()), filepath.Base(in.Destination), unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	published = true
	return parent.Sync()
}
func (s *Service) sealPreparedFile(ctx context.Context, root *os.Root, name string, bound int64) (digest string, size int64, result error) {
	result = s.cancelableDiskWork(ctx, func(worker context.Context) error {
		var err error
		digest, size, err = sealPreparedFile(worker, root, name, bound)
		return err
	})
	return
}

func sealPreparedFile(ctx context.Context, root *os.Root, name string, bound int64) (string, int64, error) {
	f, err := root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	if !st.Mode().IsRegular() {
		return "", 0, domain.Fail("RECOVERY_REQUIRED", "prepared member is not an ordinary file")
	}
	digest, size, err := hashFile(ctx, f, bound)
	if err == nil {
		err = f.Chmod(0400)
	}
	if err == nil {
		err = f.Sync()
	}
	return digest, size, err
}
func (s *InstallationService) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	var in installationInput
	if err := wire.Decode(b, &in); err != nil {
		return false, err
	}
	actual, err := Verify(ctx, in.Destination)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	stored, err := s.Store.MetadataBytes("import-artifact", p.ID)
	if err != nil {
		return false, err
	}
	var expected Artifact
	if err = wire.Decode(stored, &expected); err != nil {
		return false, err
	}
	if actual.Kind != "PreparedInstallation" || actual.PlanID != p.ID || actual.InputDigest != p.InputDigest || !equalDiskValue(actual, expected) {
		return false, domain.Fail("RECOVERY_REQUIRED", "published installation differs from durable receipt")
	}
	job, err := s.Store.Job(actual.OperationID)
	if err != nil || job.PlanID != p.ID {
		return false, domain.Fail("RECOVERY_REQUIRED", "published installation has no matching durable operation")
	}
	return true, nil
}
func (s *InstallationService) Result(ctx context.Context, uid uint32, id string) (any, error) {
	job, err := s.Store.Job(id)
	if err != nil {
		return nil, err
	}
	p, b, err := s.Store.Plan(job.PlanID)
	if err != nil {
		return nil, err
	}
	if p.ActorUID != uid || p.Operation != "import.prepare-install" {
		return nil, domain.Fail("INVALID_INPUT", "not your installation preparation operation")
	}
	var in installationInput
	if err = wire.Decode(b, &in); err != nil {
		return nil, err
	}
	if job.State != "succeeded" {
		return map[string]any{"operation": job, "destination": in.Destination, "stagingDirectory": installStagePath(p, in), "publicationStatus": "unconfirmed"}, domain.Fail("RECOVERY_REQUIRED", "installation preparation is not confirmed complete")
	}
	artifact, directory, err := Approved(ctx, s.Store, uid, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"artifact": artifact, "directory": directory}, nil
}

var _ EmptyDiskTool = image.Tool{}
