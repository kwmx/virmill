//go:build linux && amd64

package importing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
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

type FilesDiskTool interface {
	Identity(context.Context) (image.Identity, error)
	InspectFiles(context.Context, []platform.DiskSourceFile, string, string, string, int64) ([]image.Info, error)
	ConvertFiles(context.Context, []platform.DiskSourceFile, string, string, string, int64, int64) error
	MeasureFiles(context.Context, []platform.DiskSourceFile, string, string, string) (int64, error)
}
type DiskSetService struct {
	*Service
	FilesTool FilesDiskTool
}
type DiskSetFile struct {
	Path           string   `json:"path"`
	OriginalPath   string   `json:"originalPath"`
	Identity       identity `json:"identity"`
	Changed        string   `json:"changed"`
	SHA256         string   `json:"sha256"`
	ProvidedDigest bool     `json:"providedDigest"`
}
type diskSetMapping struct {
	ID                  string `json:"id"`
	Path                string `json:"path"`
	Format              string `json:"format"`
	MaximumVirtualBytes int64  `json:"maximumVirtualBytes"`
}
type diskSetRequest struct {
	OfflineSources bool   `json:"offlineSources"`
	Destination    string `json:"destination"`
	Files          []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256,omitempty"`
	} `json:"files"`
	Disks []diskSetMapping `json:"disks"`
}
type diskSetInput struct {
	SourceLockProtocol string           `json:"sourceLockProtocol"`
	SourceDirectory    string           `json:"sourceDirectory"`
	SourceIdentity     identity         `json:"sourceIdentity"`
	Destination        string           `json:"destination"`
	ParentIdentity     identity         `json:"parentIdentity"`
	Files              []DiskSetFile    `json:"files"`
	Disks              []diskSetMapping `json:"disks"`
	Chains             [][]image.Info   `json:"chains"`
	Tool               image.Identity   `json:"tool"`
	RequiredFreeBytes  int64            `json:"requiredFreeBytes"`
	// OutputBytes is each disk's measured conversion limit (ADR 0060). Plans
	// from older builds omit it and keep the worst case.
	OutputBytes []int64 `json:"outputBytes,omitempty"`
}

// diskOutputBytes is the output limit for converting disk i.
func diskOutputBytes(in diskSetInput, i int) int64 {
	if len(in.OutputBytes) == len(in.Disks) {
		return in.OutputBytes[i]
	}
	return outputBound(in.Disks[i].MaximumVirtualBytes)
}

func diskSetStage(p domain.Plan) string { return ".virmill-disks-" + p.ID }
func diskSetStagePath(p domain.Plan, in diskSetInput) string {
	return filepath.Join(filepath.Dir(in.Destination), diskSetStage(p))
}
func changedTime(st os.FileInfo) (string, error) {
	native, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("native change time unavailable")
	}
	return fmt.Sprintf("%d:%09d", native.Ctim.Sec, native.Ctim.Nsec), nil
}
func openDiskFiles(ctx context.Context, directory string, selected []DiskSetFile) ([]platform.DiskSourceFile, identity, []DiskSetFile, error) {
	var rootID identity
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory || len(selected) < 1 || len(selected) > 10000 {
		return nil, rootID, nil, domain.Fail("INVALID_INPUT", "canonical source directory and bounded explicit file selection required")
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, directory, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, rootID, nil, err
	}
	dir := os.NewFile(uintptr(fd), directory)
	defer dir.Close()
	st, err := dir.Stat()
	if err != nil {
		return nil, rootID, nil, err
	}
	rootID, err = fileIdentity(st)
	if err != nil {
		return nil, rootID, nil, err
	}
	rootID.Size = 0
	rootID.ModifiedNS = 0
	files := []platform.DiskSourceFile{}
	observed := []DiskSetFile{}
	success := false
	defer func() {
		if !success {
			closeDiskFiles(files)
		}
	}()
	seen := map[string]bool{}
	var total int64
	for _, selected := range selected {
		if err = ctx.Err(); err != nil {
			return nil, rootID, nil, err
		}
		if importer.SafePath(selected.Path) != nil || filepath.Clean(selected.Path) != selected.Path || seen[selected.Path] {
			return nil, rootID, nil, domain.Fail("INVALID_INPUT", "unique canonical relative source file paths required")
		}
		seen[selected.Path] = true
		fileFD, e := unix.Openat2(fd, selected.Path, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
		if e != nil {
			return nil, rootID, nil, e
		}
		opened := os.NewFile(uintptr(fileFD), filepath.Join(directory, selected.Path))
		f, guardErr := image.AcquireReadGuard(opened)
		opened.Close()
		if guardErr != nil {
			return nil, rootID, nil, guardErr
		}
		files = append(files, platform.DiskSourceFile{Path: selected.Path, File: f})
		st, e := f.Stat()
		if e != nil {
			return nil, rootID, nil, e
		}
		if !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > outputBound(512<<30) {
			return nil, rootID, nil, domain.Fail("INVALID_INPUT", "source members must be bounded nonempty ordinary files")
		}
		id, e := fileIdentity(st)
		if e != nil {
			return nil, rootID, nil, e
		}
		changed, e := changedTime(st)
		if e != nil {
			return nil, rootID, nil, e
		}
		digest, n, e := hashFile(ctx, f, st.Size())
		if e != nil {
			return nil, rootID, nil, e
		}
		if selected.SHA256 != "" && selected.SHA256 != digest {
			return nil, rootID, nil, domain.Fail("SOURCE_CHANGED", "selected source digest differs: "+selected.Path)
		}
		end, e := f.Stat()
		if e != nil {
			return nil, rootID, nil, e
		}
		after, e := fileIdentity(end)
		if e != nil {
			return nil, rootID, nil, e
		}
		afterChange, e := changedTime(end)
		if e != nil {
			return nil, rootID, nil, e
		}
		if n != id.Size || id != after || changed != afterChange {
			return nil, rootID, nil, domain.Fail("SOURCE_CHANGED", "source changed while hashing: "+selected.Path)
		}
		if _, e = f.Seek(0, io.SeekStart); e != nil {
			return nil, rootID, nil, e
		}
		total += n
		if total > 64<<40 {
			return nil, rootID, nil, domain.Fail("INVALID_INPUT", "selected physical source bytes exceed the 64 TiB inspection bound")
		}
		observed = append(observed, DiskSetFile{Path: selected.Path, OriginalPath: filepath.Join(directory, selected.Path), Identity: id, Changed: changed, SHA256: digest, ProvidedDigest: selected.ProvidedDigest})
	}
	success = true
	return files, rootID, observed, nil
}
func closeDiskFiles(files []platform.DiskSourceFile) {
	for _, f := range files {
		f.File.Close()
	}
}
func equalDiskValue(a, b any) bool {
	x, e := operations.Canonical(a)
	if e != nil {
		return false
	}
	y, e := operations.Canonical(b)
	return e == nil && bytes.Equal(x, y)
}
func (s *DiskSetService) Plan(ctx context.Context, uid uint32, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	b, err := json.Marshal(r.Input)
	if err != nil {
		return empty, err
	}
	if err = validation.Schema("disk-set-preparation-input", b); err != nil {
		return empty, domain.Fail("INVALID_INPUT", err.Error())
	}
	var requested diskSetRequest
	if err = wire.Decode(b, &requested); err != nil {
		return empty, err
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
	selected := []DiskSetFile{}
	for _, file := range requested.Files {
		selected = append(selected, DiskSetFile{Path: file.Path, SHA256: file.SHA256, ProvidedDigest: file.SHA256 != ""})
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })
	sources, sourceID, files, err := openDiskFiles(ctx, r.Path, selected)
	if err != nil {
		return empty, err
	}
	defer closeDiskFiles(sources)
	in := diskSetInput{SourceLockProtocol: image.SourceLockProtocol, SourceDirectory: r.Path, SourceIdentity: sourceID, Destination: requested.Destination, ParentIdentity: parentID, Files: files, Disks: requested.Disks, Chains: [][]image.Info{}, RequiredFreeBytes: 64 << 20}
	names := map[string]bool{}
	for _, source := range sources {
		names[source.Path] = true
	}
	ids, paths := map[string]bool{}, map[string]bool{}
	for _, disk := range in.Disks {
		if ids[disk.ID] || paths[disk.Path] || !names[disk.Path] || !image.Format(disk.Format) {
			return empty, domain.Fail("INVALID_INPUT", "unique disk IDs/paths, explicit formats and selected source members required")
		}
		ids[disk.ID] = true
		paths[disk.Path] = true
	}
	in.Tool, err = s.FilesTool.Identity(ctx)
	if err != nil {
		return empty, err
	}
	workspace, err := os.MkdirTemp("", "virmill-disk-preview-")
	if err != nil {
		return empty, err
	}
	defer os.RemoveAll(workspace)
	for _, disk := range in.Disks {
		chain, e := s.FilesTool.InspectFiles(ctx, sources, workspace, disk.Path, disk.Format, disk.MaximumVirtualBytes)
		if e != nil {
			return empty, e
		}
		in.Chains = append(in.Chains, chain)
		// Budget what the conversion measurably writes, not the worst case (ADR 0060).
		size, e := s.FilesTool.MeasureFiles(ctx, sources, workspace, disk.Path, disk.Format)
		if e != nil {
			return empty, e
		}
		budget := image.OutputBudget(size, chain[0].VirtualSize)
		in.OutputBytes = append(in.OutputBytes, budget)
		in.RequiredFreeBytes += budget
	}
	if err = available(parent, in.RequiredFreeBytes); err != nil {
		return empty, err
	}
	// Reopening also detects path replacements while held FDs were inspected.
	currentFiles, currentID, current, err := openDiskFiles(ctx, r.Path, files)
	if err != nil {
		return empty, err
	}
	closeDiskFiles(currentFiles)
	if currentID != sourceID || !equalDiskValue(files, current) {
		return empty, domain.Fail("SOURCE_CHANGED", "source set changed during preview")
	}
	sourceDigest, err := sourceSetDigest(files)
	if err != nil {
		return empty, err
	}
	resources := []string{"import-artifact:local:" + in.Destination}
	locked := map[string]bool{}
	for _, file := range files {
		resources = append(resources, "local-file|"+file.OriginalPath)
		key := fmt.Sprintf("disk-source:local:%d:%d", file.Identity.Device, file.Identity.Inode)
		if !locked[key] {
			resources = append(resources, key)
			locked[key] = true
		}
	}
	step := domain.Step{ID: "prepare", Action: "import.prepare-disks", Preconditions: []string{"all selected files retain exact identity, change time and SHA-256", "detected formats match explicit disk formats", "complete contained backing/extent graph", "QEMU writer locks respected", "absent destination, pinned parent/tool and sufficient output space"}, Idempotency: "reconcile-before-retry", Compensation: "Keep original files; retain uncertain private staging and never reuse incomplete conversion output", Reconciliation: "Verify published independent disk set against its durable receipt; never repeat conversion", CompletionPredicate: "Every selected root disk has an independent verified qcow2 artifact and a durable source report"}
	return s.Engine.Plan(ctx, uid, "local", "import.prepare-disks", resources, map[string]string{"source-set": sourceDigest}, in, []domain.Step{step}, []string{"write-import-artifacts", "offline-source-files"}, []string{"Makes independent copies of the selected disks; your original files are not changed", "Only the files you selected are read", "Keep the disks closed in every other program, such as VirtualBox, while they are copied; some programs do not lock their files", "A disk file does not record the VM's CPU, firmware or network; those come from the VM settings", "A checksum confirms each copy matches your file, not that it will boot", "No VM is created or started by this step alone"})
}
func sourceSetDigest(files []DiskSetFile) (string, error) {
	b, err := operations.Canonical(files)
	if err != nil {
		return "", err
	}
	return hashBytes(b), nil
}
func (s *DiskSetService) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var in diskSetInput
	if err := wire.Decode(b, &in); err != nil {
		return nil, err
	}
	return map[string]any{"sourceKind": "existing-disk-set", "sourceLockProtocol": in.SourceLockProtocol, "sourceDirectory": in.SourceDirectory, "files": in.Files, "diskMappings": in.Disks, "backingChains": in.Chains, "destination": in.Destination, "stagingDirectory": diskSetStagePath(p, in), "requiredFreeBytes": in.RequiredFreeBytes, "outputBytes": in.OutputBytes, "tool": in.Tool, "sourceHardware": "unknown; explicit creation assumptions required", "vmDefined": false, "guestBootVerified": false}, nil
}
func (s *DiskSetService) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	return s.validate(ctx, p, b, true)
}
func (s *DiskSetService) validate(ctx context.Context, p domain.Plan, b []byte, space bool) error {
	var in diskSetInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	if in.SourceLockProtocol != image.SourceLockProtocol || !filepath.IsAbs(in.Destination) || filepath.Clean(in.Destination) != in.Destination || len(in.Disks) < 1 || len(in.Disks) > 64 || len(in.Disks) != len(in.Chains) {
		return domain.Fail("INVALID_INPUT", "invalid disk preparation recipe")
	}
	files, id, current, err := openDiskFiles(ctx, in.SourceDirectory, in.Files)
	if err != nil {
		return err
	}
	defer closeDiskFiles(files)
	if id != in.SourceIdentity || !equalDiskValue(current, in.Files) {
		return domain.Fail("SOURCE_CHANGED", "disk source identities or content changed")
	}
	for _, file := range in.Files {
		found := false
		for _, resource := range p.ResourceIDs {
			found = found || resource == "local-file|"+file.OriginalPath
		}
		if !found {
			return domain.Fail("STALE_PLAN", "source file concurrency contract changed; review a new plan")
		}
	}
	tool, err := s.FilesTool.Identity(ctx)
	if err != nil {
		return err
	}
	if tool != in.Tool {
		return domain.Fail("SOURCE_CHANGED", "image tool changed after preview")
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
func (s *DiskSetService) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (result error) {
	if err := s.cancelableDiskWork(ctx, func(worker context.Context) error { return s.Validate(worker, p, b) }); err != nil {
		return err
	}
	var in diskSetInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	var sources []platform.DiskSourceFile
	var sourceID identity
	var files []DiskSetFile
	err := s.cancelableDiskWork(ctx, func(worker context.Context) error {
		var e error
		sources, sourceID, files, e = openDiskFiles(worker, in.SourceDirectory, in.Files)
		return e
	})
	if err != nil {
		closeDiskFiles(sources)
		return err
	}
	defer closeDiskFiles(sources)
	if sourceID != in.SourceIdentity || !equalDiskValue(files, in.Files) {
		return domain.Fail("SOURCE_CHANGED", "source changed before conversion")
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
	stage := diskSetStage(p)
	if err = root.Mkdir(stage, 0700); err != nil {
		return err
	}
	workRoot, err := root.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer workRoot.Close()
	published := false
	defer func() {
		if errors.Is(result, operations.ErrCanceledSafely) && !published {
			if err := root.RemoveAll(stage); err != nil {
				result = fmt.Errorf("canceled but private stage removal failed: %w", err)
			}
		}
	}()
	if err = workRoot.MkdirAll("artifact/disks", 0700); err != nil {
		return err
	}
	artifactRoot, err := workRoot.OpenRoot("artifact")
	if err != nil {
		return err
	}
	defer artifactRoot.Close()
	sourceDigest, err := sourceSetDigest(in.Files)
	if err != nil {
		return err
	}
	artifact := Artifact{APIVersion: domain.APIVersion, Kind: "PreparedDiskSet", PlanID: p.ID, OperationID: operations.OperationID(ctx), InputDigest: p.InputDigest, CreatedAt: time.Now().UTC(), SourceSHA256: sourceDigest, SourceFiles: in.Files, Tool: in.Tool, System: importer.System{ID: "selected-disks", Name: "User-selected existing disk set", Items: []importer.Item{}, DiskIDs: []string{}}, Disks: []PreparedDisk{}}
	for i, mapping := range in.Disks {
		if canceled, e := operations.CancellationRequested(ctx, s.Store); e != nil {
			return e
		} else if canceled {
			return operations.ErrCanceledSafely
		}
		name := fmt.Sprintf("disk-work-%03d", i)
		if err = workRoot.Mkdir(name, 0700); err != nil {
			return err
		}
		workspace := filepath.Join(diskSetStagePath(p, in), name)
		if err = operations.Note(ctx, s.Store, "Inspect selected file graph before converting disk "+mapping.ID); err != nil {
			return err
		}
		var chain []image.Info
		if err = s.cancelableDiskWork(ctx, func(worker context.Context) error {
			var e error
			chain, e = s.FilesTool.InspectFiles(worker, sources, workspace, mapping.Path, mapping.Format, mapping.MaximumVirtualBytes)
			return e
		}); err != nil {
			return err
		}
		if !equalDiskValue(chain, in.Chains[i]) || len(chain) == 0 {
			return domain.Fail("SOURCE_CHANGED", "disk graph changed after preview")
		}
		if err = operations.Note(ctx, s.Store, "Intent persisted: convert selected disk "+mapping.ID+" into an independent private qcow2 artifact"); err != nil {
			return err
		}
		if err = s.cancelableDiskWork(ctx, func(worker context.Context) error {
			return s.FilesTool.ConvertFiles(worker, sources, workspace, mapping.Path, mapping.Format, chain[0].VirtualSize, diskOutputBytes(in, i))
		}); err != nil {
			return err
		}
		output := name + "/disk.qcow2"
		f, e := workRoot.OpenFile(output, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if e != nil {
			return e
		}
		st, e := f.Stat()
		if e != nil {
			f.Close()
			return e
		}
		if !st.Mode().IsRegular() {
			f.Close()
			return domain.Fail("RECOVERY_REQUIRED", "converter output is not an ordinary file")
		}
		digest, size, e := hashFile(ctx, f, outputBound(mapping.MaximumVirtualBytes))
		if e == nil {
			e = f.Chmod(0400)
		}
		if e == nil {
			e = f.Sync()
		}
		if closeErr := f.Close(); e == nil {
			e = closeErr
		}
		if e != nil {
			return e
		}
		target := fmt.Sprintf("disks/disk-%03d.qcow2", i)
		if err = workRoot.Rename(output, "artifact/"+target); err != nil {
			return err
		}
		artifact.System.DiskIDs = append(artifact.System.DiskIDs, mapping.ID)
		artifact.Disks = append(artifact.Disks, PreparedDisk{SourceID: mapping.ID, SourcePath: mapping.Path, Path: target, Format: "qcow2", VirtualBytes: chain[0].VirtualSize, FileBytes: size, SHA256: digest, SourceChain: chain, Verification: "virtual-size+qemu-check+qemu-compare"})
	}
	if err = s.cancelableDiskWork(ctx, func(worker context.Context) error { return s.validate(worker, p, b, false) }); err != nil {
		return err
	}
	report, err := operations.Canonical(map[string]any{"sourceKind": "existing-disk-set", "sourceLockProtocol": in.SourceLockProtocol, "sourceDirectory": in.SourceDirectory, "files": in.Files, "diskMappings": in.Disks, "backingChains": in.Chains, "sourceHardware": "unknown", "publisherAuthenticity": "not-established"})
	if err != nil {
		return err
	}
	if err = writeFile(artifactRoot, "disk-source-report.json", report); err != nil {
		return err
	}
	artifact.ReportSHA256 = hashBytes(report)
	manifest, err := operations.Canonical(artifact)
	if err != nil {
		return err
	}
	if err = validation.Schema("prepared-disk-set", manifest); err != nil {
		return err
	}
	if err = writeFile(artifactRoot, "manifest.json", manifest); err != nil {
		return err
	}
	disks, err := artifactRoot.OpenRoot("disks")
	if err != nil {
		return err
	}
	err = syncRoot(disks)
	disks.Close()
	if err != nil {
		return err
	}
	if err = syncRoot(artifactRoot); err != nil {
		return err
	}
	if canceled, e := operations.CancellationRequested(ctx, s.Store); e != nil {
		return e
	} else if canceled {
		return operations.ErrCanceledSafely
	}
	if err = operations.Note(ctx, s.Store, "Intent persisted: publish complete independent disk set after source revalidation"); err != nil {
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
func (s *DiskSetService) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	var in diskSetInput
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
	if actual.Kind != "PreparedDiskSet" || actual.PlanID != p.ID || actual.InputDigest != p.InputDigest || !equalDiskValue(actual, expected) {
		return false, domain.Fail("RECOVERY_REQUIRED", "published disk set differs from durable preparation receipt")
	}
	job, err := s.Store.Job(actual.OperationID)
	if err != nil || job.PlanID != p.ID {
		return false, domain.Fail("RECOVERY_REQUIRED", "published disk set has no matching durable operation")
	}
	return true, nil
}
func (s *DiskSetService) Result(ctx context.Context, uid uint32, id string) (any, error) {
	job, err := s.Store.Job(id)
	if err != nil {
		return nil, err
	}
	p, b, err := s.Store.Plan(job.PlanID)
	if err != nil {
		return nil, err
	}
	if p.ActorUID != uid || p.Operation != "import.prepare-disks" {
		return nil, domain.Fail("INVALID_INPUT", "not your disk preparation operation")
	}
	var in diskSetInput
	if err = wire.Decode(b, &in); err != nil {
		return nil, err
	}
	if job.State != "succeeded" {
		return map[string]any{"operation": job, "destination": in.Destination, "stagingDirectory": diskSetStagePath(p, in), "publicationStatus": "unconfirmed"}, domain.Fail("RECOVERY_REQUIRED", "disk preparation is not confirmed complete")
	}
	artifact, directory, err := Approved(ctx, s.Store, uid, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"artifact": artifact, "directory": directory}, nil
}

var _ FilesDiskTool = image.Tool{}
