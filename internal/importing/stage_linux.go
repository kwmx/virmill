//go:build linux && amd64

// Package importing coordinates durable preparation of independent import artifacts.
package importing

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

type DiskTool interface {
	Identity(context.Context) (image.Identity, error)
	Inspect(context.Context, string, string, string, string, int64, map[string]bool) ([]image.Info, error)
	Convert(context.Context, string, string, string, string, int64, int64) error
}
type Service struct {
	Engine *operations.Engine
	Store  *store.Store
	Tool   DiskTool
}
type Mapping struct {
	ID                  string `json:"id"`
	Format              string `json:"format"`
	MaximumVirtualBytes int64  `json:"maximumVirtualBytes"`
}
type request struct {
	Destination string    `json:"destination"`
	SystemID    string    `json:"systemID"`
	Disks       []Mapping `json:"disks"`
}
type identity struct {
	Device     uint64 `json:"device,string"`
	Inode      uint64 `json:"inode,string"`
	Size       int64  `json:"size"`
	ModifiedNS int64  `json:"modifiedNS,string"`
}
type stageInput struct {
	Source            importer.Report `json:"source"`
	SourceIdentity    identity        `json:"sourceIdentity"`
	ParentIdentity    identity        `json:"parentIdentity"`
	Destination       string          `json:"destination"`
	System            importer.System `json:"system"`
	Mappings          []Mapping       `json:"mappings"`
	Tool              image.Identity  `json:"tool"`
	RequiredFreeBytes int64           `json:"requiredFreeBytes"`
}
type PreparedDisk struct {
	SourceID     string       `json:"sourceID"`
	SourcePath   string       `json:"sourcePath"`
	Path         string       `json:"path"`
	Format       string       `json:"format"`
	VirtualBytes int64        `json:"virtualBytes"`
	FileBytes    int64        `json:"fileBytes"`
	SHA256       string       `json:"sha256"`
	SourceChain  []image.Info `json:"sourceChain"`
	Verification string       `json:"verification"`
}
type PreparedMedia struct {
	SourceID     string `json:"sourceID"`
	SourcePath   string `json:"sourcePath"`
	Path         string `json:"path"`
	Format       string `json:"format"`
	FileBytes    int64  `json:"fileBytes"`
	SHA256       string `json:"sha256"`
	Recognition  string `json:"recognition"`
	Verification string `json:"verification"`
}
type Artifact struct {
	APIVersion        string          `json:"apiVersion"`
	Kind              string          `json:"kind"`
	PlanID            string          `json:"planID"`
	OperationID       string          `json:"operationID"`
	InputDigest       string          `json:"inputDigest"`
	CreatedAt         time.Time       `json:"createdAt"`
	SourceSHA256      string          `json:"sourceSHA256"`
	System            importer.System `json:"system"`
	Tool              image.Identity  `json:"tool"`
	Disks             []PreparedDisk  `json:"disks"`
	DescriptorSHA256  string          `json:"descriptorSHA256"`
	ReportSHA256      string          `json:"reportSHA256"`
	VMDefined         bool            `json:"vmDefined"`
	GuestBootVerified bool            `json:"guestBootVerified"`
	SourceFiles       []DiskSetFile   `json:"sourceFiles,omitempty"`
	Media             []PreparedMedia `json:"media,omitempty"`
}

func Register(appService *app.Service) {
	s := &Service{Engine: appService.Engine, Store: appService.Engine.Store, Tool: image.Tool{}}
	disks := &DiskSetService{Service: s, FilesTool: image.Tool{}}
	install := &InstallationService{Service: s, EmptyTool: image.Tool{}}
	appService.Engine.Handlers["import.prepare-install"] = install
	appService.Extensions["import.prepare-install"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return install.Plan(ctx, uid, r) }
	appService.Engine.Handlers["import.prepare-disks"] = disks
	appService.Extensions["import.prepare-disks"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return disks.Plan(ctx, uid, r) }
	appService.Engine.Handlers["import.prepare"] = s
	appService.Extensions["import.prepare"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return s.Plan(ctx, uid, r) }
	appService.Extensions["import.result"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) {
		job, err := s.Store.Job(r.ID)
		if err != nil {
			return nil, err
		}
		p, input, err := s.Store.Plan(job.PlanID)
		if err != nil {
			return nil, err
		}
		if p.ActorUID != uid {
			return nil, domain.Fail("PERMISSION_DENIED", "not your preparation operation")
		}
		if p.Operation == "import.prepare-install" {
			return install.Result(ctx, uid, r.ID)
		}
		if p.Operation == "import.prepare-disks" {
			return disks.Result(ctx, uid, r.ID)
		}
		if p.Operation != "import.prepare" {
			return nil, domain.Fail("INVALID_INPUT", "operation is not an import preparation")
		}
		var in stageInput
		if err = wire.Decode(input, &in); err != nil {
			return nil, err
		}
		if job.State != "succeeded" {
			return map[string]any{"operation": job, "destination": in.Destination, "stagingDirectory": stagePath(p, in), "publicationStatus": "unconfirmed"}, domain.Fail("RECOVERY_REQUIRED", "preparation is not confirmed complete; inspect or reconcile this operation")
		}
		var artifact Artifact
		if err = s.Store.Get("import-artifact", p.ID, &artifact); err != nil {
			return nil, err
		}
		return map[string]any{"artifact": artifact, "directory": in.Destination}, nil
	}
	appService.Extensions["import.verify"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) { return Verify(ctx, r.Path) }
}

func fileIdentity(st os.FileInfo) (identity, error) {
	n, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return identity{}, errors.New("native file identity unavailable")
	}
	return identity{Device: uint64(n.Dev), Inode: n.Ino, Size: st.Size(), ModifiedNS: st.ModTime().UnixNano()}, nil
}
func sourceFile(filename string) (*os.File, identity, error) {
	f, err := os.OpenFile(filename, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, identity{}, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, identity{}, err
	}
	if !st.Mode().IsRegular() {
		f.Close()
		return nil, identity{}, domain.Fail("INVALID_INPUT", "import source must be an ordinary non-symlink file")
	}
	id, err := fileIdentity(st)
	if err != nil {
		f.Close()
		return nil, id, err
	}
	return f, id, nil
}

func parentDirectory(destination string) (*os.File, *os.Root, identity, error) {
	parent := filepath.Dir(destination)
	fd, err := unix.Openat2(unix.AT_FDCWD, parent, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, nil, identity{}, fmt.Errorf("open approved import parent without symlinks: %w", err)
	}
	f := os.NewFile(uintptr(fd), parent)
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, identity{}, err
	}
	n := st.Sys().(*syscall.Stat_t)
	if n.Uid != uint32(os.Getuid()) || st.Mode().Perm()&0022 != 0 {
		f.Close()
		return nil, nil, identity{}, domain.Fail("PERMISSION_DENIED", "destination parent must be user-owned and not writable by group/others")
	}
	r, err := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		f.Close()
		return nil, nil, identity{}, err
	}
	id, err := fileIdentity(st)
	id.Size = 0
	id.ModifiedNS = 0
	return f, r, id, err
}

func outputBound(virtual int64) int64 { return virtual + virtual/4 + (16 << 20) }
func ensureAbsent(r *os.Root, name string) error {
	_, err := r.Lstat(name)
	if !os.IsNotExist(err) {
		return domain.Fail("STALE_PLAN", "destination exists or cannot be proven absent")
	}
	return nil
}
func available(f *os.File, required int64) error {
	var st unix.Statfs_t
	if err := unix.Fstatfs(int(f.Fd()), &st); err != nil {
		return err
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	if required < 0 || uint64(required) > free {
		return domain.Fail("INSUFFICIENT_SPACE", fmt.Sprintf("preparation needs up to %d free bytes; %d available", required, free))
	}
	return nil
}
func stageName(p domain.Plan) string { return ".virmill-import-" + p.ID }
func stagePath(p domain.Plan, in stageInput) string {
	return filepath.Join(filepath.Dir(in.Destination), stageName(p))
}

func (s *Service) Plan(ctx context.Context, uid uint32, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	b, err := json.Marshal(r.Input)
	if err != nil {
		return empty, err
	}
	if err = validation.Schema("import-preparation-input", b); err != nil {
		return empty, domain.Fail("INVALID_INPUT", err.Error())
	}
	var requested request
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&requested); err != nil {
		return empty, domain.Fail("INVALID_INPUT", err.Error())
	}
	if requested.Destination == "" || !filepath.IsAbs(requested.Destination) || !filepath.IsAbs(r.Path) {
		return empty, domain.Fail("INVALID_INPUT", "absolute source and destination required")
	}
	requested.Destination = filepath.Clean(requested.Destination)
	f, root, parent, err := parentDirectory(requested.Destination)
	if err != nil {
		return empty, err
	}
	defer f.Close()
	defer root.Close()
	if err = ensureAbsent(root, filepath.Base(requested.Destination)); err != nil {
		return empty, err
	}
	source, id, err := sourceFile(r.Path)
	if err != nil {
		return empty, err
	}
	defer source.Close()
	report, err := importer.InspectTar(ctx, source, importer.DefaultLimits())
	if err != nil {
		return empty, err
	}
	report.Source = r.Path
	end, err := source.Stat()
	if err != nil {
		return empty, err
	}
	after, _ := fileIdentity(end)
	if after != id {
		return empty, domain.Fail("SOURCE_CHANGED", "archive changed during preview")
	}
	if requested.SystemID == "" && len(report.Systems) == 1 {
		requested.SystemID = report.Systems[0].ID
	}
	var selected *importer.System
	for i := range report.Systems {
		if report.Systems[i].ID == requested.SystemID {
			selected = &report.Systems[i]
		}
	}
	if selected == nil {
		return empty, domain.Fail("INVALID_INPUT", "select one exact virtual system ID; multiple systems are never silently truncated")
	}
	if len(selected.DiskIDs) == 0 || len(selected.DiskIDs) > 64 || len(requested.Disks) != len(selected.DiskIDs) {
		return empty, domain.Fail("INVALID_INPUT", "provide one explicit mapping for every selected disk (1–64)")
	}
	mappings := map[string]Mapping{}
	for _, d := range requested.Disks {
		if !image.Format(d.Format) || d.MaximumVirtualBytes < 1<<20 || d.MaximumVirtualBytes > 512<<30 || mappings[d.ID].ID != "" {
			return empty, domain.Fail("INVALID_INPUT", "distinct disk IDs, supported explicit formats and bounds of 1 MiB–512 GiB required")
		}
		mappings[d.ID] = d
	}
	in := stageInput{Source: report, SourceIdentity: id, ParentIdentity: parent, Destination: requested.Destination, System: *selected, Mappings: []Mapping{}, RequiredFreeBytes: 64 << 20}
	seen := map[string]bool{}
	for _, id := range selected.DiskIDs {
		d, ok := mappings[id]
		if !ok || seen[id] {
			return empty, domain.Fail("INVALID_INPUT", "disk mapping is incomplete or descriptor attachment is ambiguous")
		}
		seen[id] = true
		in.Mappings = append(in.Mappings, d)
		in.RequiredFreeBytes += outputBound(d.MaximumVirtualBytes)
	}
	for _, member := range report.Members {
		in.RequiredFreeBytes += member.Size
	}
	if err = available(f, in.RequiredFreeBytes); err != nil {
		return empty, err
	}
	in.Tool, err = s.Tool.Identity(ctx)
	if err != nil {
		return empty, err
	}
	step := domain.Step{ID: "prepare", Action: "import.prepare", Preconditions: []string{"unchanged archive identity and SHA-256", "explicit complete disk mapping", "pinned qemu-img executable", "private staging, absent destination and sufficient space"}, Idempotency: "reconcile-before-retry", Compensation: "Keep original archive and uncommitted job staging; never resume partial conversion files", Reconciliation: "Verify the published artifact against the durable receipt without conversion replay", CompletionPredicate: "Every selected disk is independently converted, size-checked, compared and durably published with its descriptor"}
	return s.Engine.Plan(ctx, uid, "local", "import.prepare", []string{"import-artifact:local:" + in.Destination}, map[string]string{"archive": report.SHA256}, in, []domain.Step{step}, []string{"write-import-artifacts"}, []string{"Creates independent qcow2 copies; originals are never modified", "This preparation does not define/start a VM or verify guest boot", "Interrupted conversion output is retained but never reused automatically"})
}

func (s *Service) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var in stageInput
	if err := wire.Decode(b, &in); err != nil {
		return nil, err
	}
	return map[string]any{"source": in.Source.Source, "sourceSHA256": in.Source.SHA256, "system": in.System, "mappings": in.Mappings, "destination": in.Destination, "stagingDirectory": stagePath(p, in), "requiredFreeBytes": in.RequiredFreeBytes, "tool": in.Tool, "vmDefined": false, "guestBootVerified": false}, nil
}
func (s *Service) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	return s.validate(ctx, p, b, true)
}
func (s *Service) validate(ctx context.Context, p domain.Plan, b []byte, checkSpace bool) error {
	var in stageInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	f, r, parent, err := parentDirectory(in.Destination)
	if err != nil {
		return err
	}
	defer f.Close()
	defer r.Close()
	if parent != in.ParentIdentity {
		return domain.Fail("STALE_PLAN", "destination parent identity changed")
	}
	if err = ensureAbsent(r, filepath.Base(in.Destination)); err != nil {
		return err
	}
	if checkSpace {
		if err = available(f, in.RequiredFreeBytes); err != nil {
			return err
		}
	}
	source, id, err := sourceFile(in.Source.Source)
	if err != nil {
		return err
	}
	defer source.Close()
	if id != in.SourceIdentity {
		return domain.Fail("SOURCE_CHANGED", "source file identity, size or modification time changed")
	}
	if digest, _, err := hashFile(ctx, source, id.Size); err != nil {
		return err
	} else if digest != in.Source.SHA256 {
		return domain.Fail("SOURCE_CHANGED", "source digest changed")
	}
	tool, err := s.Tool.Identity(ctx)
	if err != nil {
		return err
	}
	if tool != in.Tool {
		return domain.Fail("SOURCE_CHANGED", "image tool changed after review")
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}
func hashFile(ctx context.Context, f *os.File, maximum int64) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, f}, maximum+1))
	if err != nil {
		return "", n, err
	}
	if n > maximum {
		return "", n, errors.New("file exceeds approved size")
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
func writeFile(root *os.Root, name string, b []byte) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	return err
}
func syncRoot(r *os.Root) error {
	f, err := r.Open(".")
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (s *Service) extract(ctx context.Context, in stageInput, dest *os.Root) error {
	f, id, err := sourceFile(in.Source.Source)
	if err != nil {
		return err
	}
	defer f.Close()
	if id != in.SourceIdentity {
		return domain.Fail("SOURCE_CHANGED", "archive changed before extraction")
	}
	members := map[string]importer.Member{}
	for _, m := range in.Source.Members {
		members[m.Path] = m
	}
	h := sha256.New()
	reader := io.TeeReader(io.LimitReader(contextReader{ctx, f}, id.Size+1), h)
	tr := tar.NewReader(reader)
	seen := map[string]bool{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if err = importer.SafePath(header.Name); err != nil {
			return err
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return domain.Fail("SOURCE_CHANGED", "unexpected archive entry type")
		}
		m, ok := members[header.Name]
		if !ok || seen[header.Name] || header.Size != m.Size {
			return domain.Fail("SOURCE_CHANGED", "archive inventory changed")
		}
		seen[header.Name] = true
		if err = dest.MkdirAll(filepath.Dir(header.Name), 0700); err != nil {
			return err
		}
		out, err := dest.OpenFile(header.Name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0600)
		if err != nil {
			return err
		}
		sum := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(out, sum), contextReader{ctx, tr})
		if copyErr == nil {
			copyErr = out.Sync()
		}
		if closeErr := out.Close(); copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			return copyErr
		}
		if n != m.Size || hex.EncodeToString(sum.Sum(nil)) != m.SHA256 {
			return domain.Fail("SOURCE_CHANGED", "extracted member digest differs")
		}
		canceled, err := operations.CancellationRequested(ctx, s.Store)
		if err != nil {
			return err
		}
		if canceled {
			return operations.ErrCanceledSafely
		}
	}
	if _, err = io.Copy(io.Discard, reader); err != nil {
		return err
	}
	if len(seen) != len(members) || hex.EncodeToString(h.Sum(nil)) != in.Source.SHA256 {
		return domain.Fail("SOURCE_CHANGED", "source digest/inventory changed during extraction")
	}
	end, err := f.Stat()
	if err != nil {
		return err
	}
	endID, _ := fileIdentity(end)
	if endID != id {
		return domain.Fail("SOURCE_CHANGED", "source identity changed during extraction")
	}
	return syncRoot(dest)
}

// cancelableDiskWork waits for the confined process to exit before reporting a
// safe cancellation. Daemon shutdown remains an uncertain operation for recovery.
func (s *Service) cancelableDiskWork(ctx context.Context, work func(context.Context) error) error {
	worker, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	var requested atomic.Bool
	var watchErr error // read only after the watcher has joined
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-worker.Done():
				return
			case <-ticker.C:
				canceled, err := operations.CancellationRequested(ctx, s.Store)
				if err != nil {
					watchErr = err
					stop()
					return
				}
				if canceled {
					requested.Store(true)
					stop()
					return
				}
			}
		}
	}()
	err := work(worker)
	stop()
	<-done
	if requested.Load() && ctx.Err() == nil {
		return operations.ErrCanceledSafely
	}
	if err == nil && watchErr != nil {
		return watchErr
	}
	return err
}

func (s *Service) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (result error) {
	if err := s.Validate(ctx, p, b); err != nil {
		return err
	}
	var in stageInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	parent, r, parentID, err := parentDirectory(in.Destination)
	if err != nil {
		return err
	}
	defer parent.Close()
	defer r.Close()
	if parentID != in.ParentIdentity {
		return domain.Fail("STALE_PLAN", "parent changed")
	}
	stage := stageName(p)
	if err = r.Mkdir(stage, 0700); err != nil {
		return err
	}
	root, err := r.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer root.Close()
	published := false
	defer func() {
		if errors.Is(result, operations.ErrCanceledSafely) && !published {
			if err := r.RemoveAll(stage); err != nil {
				result = fmt.Errorf("canceled but staging cleanup failed: %w", err)
			}
		}
	}()
	if err = operations.Note(ctx, s.Store, "Extracting approved source members into private job staging"); err != nil {
		return err
	}
	if err = root.Mkdir("source", 0700); err != nil {
		return err
	}
	source, err := root.OpenRoot("source")
	if err != nil {
		return err
	}
	defer source.Close()
	if err = s.extract(ctx, in, source); err != nil {
		return err
	}
	if err = root.Mkdir("artifact", 0700); err != nil {
		return err
	}
	artifactRoot, err := root.OpenRoot("artifact")
	if err != nil {
		return err
	}
	defer artifactRoot.Close()
	if err = artifactRoot.Mkdir("disks", 0700); err != nil {
		return err
	}
	artifact := Artifact{APIVersion: domain.APIVersion, Kind: "PreparedImport", PlanID: p.ID, OperationID: operations.OperationID(ctx), InputDigest: p.InputDigest, CreatedAt: time.Now().UTC(), SourceSHA256: in.Source.SHA256, System: in.System, Tool: in.Tool, Disks: []PreparedDisk{}}
	members := map[string]bool{}
	diskPaths := map[string]string{}
	for _, m := range in.Source.Members {
		members[m.Path] = true
	}
	for _, d := range in.Source.Disks {
		diskPaths[d.ID] = d.Path
	}
	for i, mapping := range in.Mappings {
		canceled, err := operations.CancellationRequested(ctx, s.Store)
		if err != nil {
			return err
		}
		if canceled {
			return operations.ErrCanceledSafely
		}
		workspaceName := fmt.Sprintf("disk-work-%03d", i)
		if err = root.Mkdir(workspaceName, 0700); err != nil {
			return err
		}
		workspace := filepath.Join(stagePath(p, in), workspaceName)
		sourcePath := filepath.Join(stagePath(p, in), "source")
		if err = operations.Note(ctx, s.Store, "Inspecting backing files and extents for disk "+mapping.ID); err != nil {
			return err
		}
		var chain []image.Info
		err = s.cancelableDiskWork(ctx, func(worker context.Context) error {
			var e error
			chain, e = s.Tool.Inspect(worker, sourcePath, workspace, diskPaths[mapping.ID], mapping.Format, mapping.MaximumVirtualBytes, members)
			return e
		})
		if err != nil {
			return err
		}
		if len(chain) == 0 {
			return errors.New("image inspector returned no image")
		}
		if err = operations.Note(ctx, s.Store, "Converting and comparing disk "+mapping.ID+" to independent qcow2"); err != nil {
			return err
		}
		if err = s.cancelableDiskWork(ctx, func(worker context.Context) error {
			return s.Tool.Convert(worker, sourcePath, workspace, diskPaths[mapping.ID], mapping.Format, chain[0].VirtualSize, outputBound(mapping.MaximumVirtualBytes))
		}); err != nil {
			return err
		}
		output := workspaceName + "/disk.qcow2"
		f, err := root.OpenFile(output, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return err
		}
		if !st.Mode().IsRegular() {
			f.Close()
			return errors.New("converter output is not an ordinary file")
		}
		digest, size, err := hashFile(ctx, f, outputBound(mapping.MaximumVirtualBytes))
		if err == nil {
			err = f.Sync()
		}
		if err == nil {
			err = f.Chmod(0400)
		}
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		target := fmt.Sprintf("disks/disk-%03d.qcow2", i)
		if err = root.Rename(output, "artifact/"+target); err != nil {
			return err
		}
		artifact.Disks = append(artifact.Disks, PreparedDisk{SourceID: mapping.ID, SourcePath: diskPaths[mapping.ID], Path: target, Format: "qcow2", VirtualBytes: chain[0].VirtualSize, FileBytes: size, SHA256: digest, SourceChain: chain, Verification: "virtual-size+qemu-check+qemu-compare"})
	}
	if err = operations.Note(ctx, s.Store, "Verifying original source and flushing complete import artifacts before publication"); err != nil {
		return err
	}
	if err = s.validate(ctx, p, b, false); err != nil {
		return err
	}
	descriptor, err := source.ReadFile(in.Source.Descriptor)
	if err != nil {
		return err
	}
	if err = writeFile(artifactRoot, "source.ovf", descriptor); err != nil {
		return err
	}
	artifact.DescriptorSHA256 = hashBytes(descriptor)
	report, err := operations.Canonical(in.Source)
	if err != nil {
		return err
	}
	if err = writeFile(artifactRoot, "import-report.json", report); err != nil {
		return err
	}
	artifact.ReportSHA256 = hashBytes(report)
	manifest, err := operations.Canonical(artifact)
	if err != nil {
		return err
	}
	if err = validation.Schema("prepared-import", manifest); err != nil {
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
	canceled, err := operations.CancellationRequested(ctx, s.Store)
	if err != nil {
		return err
	}
	if canceled {
		return operations.ErrCanceledSafely
	}
	// This receipt is durable before the single externally visible publication.
	if err = s.Store.ComparePut("import-artifact", p.ID, nil, artifact); err != nil {
		return err
	}
	if err = unix.Renameat2(int(parent.Fd()), stage+"/artifact", int(parent.Fd()), filepath.Base(in.Destination), unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	published = true
	if err = parent.Sync(); err != nil {
		return err
	}
	// Retain source and work files in the named job staging directory for now;
	// cleanup is a separate operation until in-flight dependency GC is complete.
	return nil
}

func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func Verify(ctx context.Context, directory string) (Artifact, error) {
	var artifact Artifact
	fd, err := unix.Openat2(unix.AT_FDCWD, directory, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return artifact, err
	}
	directoryFile := os.NewFile(uintptr(fd), directory)
	defer directoryFile.Close()
	r, err := os.OpenRoot(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return artifact, err
	}
	defer r.Close()
	f, err := r.OpenFile("manifest.json", os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return artifact, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return artifact, errors.New("manifest must be an ordinary file")
	}
	b, err := io.ReadAll(io.LimitReader(f, wire.MaxFrame+1))
	f.Close()
	if err != nil {
		return artifact, err
	}
	if err = wire.Decode(b, &artifact); err != nil {
		return artifact, err
	}
	schema := "prepared-import"
	if artifact.Kind == "PreparedDiskSet" {
		schema = "prepared-disk-set"
	}
	if artifact.Kind == "PreparedInstallation" {
		schema = "prepared-installation"
	}
	if err = validation.Schema(schema, b); err != nil {
		return artifact, err
	}
	if artifact.APIVersion != domain.APIVersion || (artifact.Kind != "PreparedImport" && artifact.Kind != "PreparedDiskSet" && artifact.Kind != "PreparedInstallation") || artifact.PlanID == "" || artifact.VMDefined || artifact.GuestBootVerified || len(artifact.Disks) == 0 || len(artifact.Disks) > 64 {
		return artifact, domain.Fail("INVALID_INPUT", "invalid prepared-import manifest")
	}
	files := map[string]struct {
		digest string
		size   int64
	}{"source.ovf": {artifact.DescriptorSHA256, importer.DescriptorLimit}, "import-report.json": {artifact.ReportSHA256, wire.MaxFrame}}
	if artifact.Kind == "PreparedDiskSet" || artifact.Kind == "PreparedInstallation" {
		digest, err := sourceSetDigest(artifact.SourceFiles)
		if err != nil || digest != artifact.SourceSHA256 {
			return artifact, domain.Fail("SOURCE_CHANGED", "disk-set source proof digest differs from its manifest")
		}
		delete(files, "source.ovf")
		delete(files, "import-report.json")
		reportName := "disk-source-report.json"
		if artifact.Kind == "PreparedInstallation" {
			reportName = "installation-report.json"
		}
		files[reportName] = struct {
			digest string
			size   int64
		}{artifact.ReportSHA256, wire.MaxFrame}
	}
	for i, disk := range artifact.Disks {
		if disk.Path != fmt.Sprintf("disks/disk-%03d.qcow2", i) || disk.Format != "qcow2" || disk.FileBytes < 0 || disk.FileBytes > outputBound(512<<30) {
			return artifact, domain.Fail("INVALID_INPUT", "invalid converted disk member")
		}
		files[disk.Path] = struct {
			digest string
			size   int64
		}{disk.SHA256, disk.FileBytes}
	}
	for i, media := range artifact.Media {
		if media.Path != fmt.Sprintf("media/media-%03d.iso", i) || media.Format != "raw" || media.FileBytes < 17*2048 || media.FileBytes > 64<<30 || media.FileBytes%2048 != 0 {
			return artifact, domain.Fail("INVALID_INPUT", "invalid prepared installation media")
		}
		files[media.Path] = struct {
			digest string
			size   int64
		}{media.SHA256, media.FileBytes}
	}
	for name, expected := range files {
		f, err := r.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return artifact, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return artifact, err
		}
		if !st.Mode().IsRegular() {
			f.Close()
			return artifact, errors.New("artifact member is not ordinary")
		}
		digest, _, err := hashFile(ctx, f, expected.size)
		f.Close()
		if err != nil {
			return artifact, err
		}
		if digest != expected.digest {
			return artifact, domain.Fail("SOURCE_CHANGED", "artifact member digest differs: "+name)
		}
	}
	return artifact, nil
}
func (s *Service) Reconcile(ctx context.Context, p domain.Plan, b []byte, step domain.Step) (bool, error) {
	var in stageInput
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
	var expected Artifact
	if err = s.Store.Get("import-artifact", p.ID, &expected); err != nil {
		return false, err
	}
	a, _ := operations.Canonical(actual)
	e, _ := operations.Canonical(expected)
	if actual.PlanID != p.ID || actual.InputDigest != p.InputDigest || !bytes.Equal(a, e) {
		return false, domain.Fail("RECOVERY_REQUIRED", "published artifact does not match the durable receipt")
	}
	return true, nil
}
