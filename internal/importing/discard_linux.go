package importing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"syscall"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/wire"
)

// Removing a finished preparation's folders is its own reviewed operation
// (ADR 0058). It never touches the original source, VMs or their volumes.
const discardedKind = "import-discarded"

var discardUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type discardInput struct {
	Version           int    `json:"version"`
	SourceOperationID string `json:"sourceOperationID"`
	Parent            string `json:"parent"`
	Stage             string `json:"stage"`
	Artifact          string `json:"artifact"`
	RemoveArtifact    bool   `json:"removeArtifact"`
}

type discardRecord struct {
	Version     int    `json:"version"`
	OperationID string `json:"operationID"`
	PlanID      string `json:"planID"`
}

type DiscardService struct {
	Store  *store.Store
	Engine *operations.Engine
}

// preparation returns a finished preparation's destination and staging name.
func (s *DiscardService) preparation(uid uint32, id string) (string, string, error) {
	j, err := s.Store.Job(id)
	if err != nil {
		return "", "", err
	}
	p, raw, err := s.Store.Plan(j.PlanID)
	if err != nil {
		return "", "", err
	}
	if p.ActorUID != uid || j.State != "succeeded" {
		return "", "", domain.Fail("INVALID_INPUT", "choose your own successful image preparation")
	}
	var in struct {
		Destination string `json:"destination"`
	}
	// Only the destination is read here; each preparation owns its full schema.
	if err = json.Unmarshal(raw, &in); err != nil || !filepath.IsAbs(in.Destination) || filepath.Clean(in.Destination) != in.Destination {
		return "", "", domain.Fail("INVALID_INPUT", "preparation destination is unreadable")
	}
	switch p.Operation {
	case "import.prepare":
		return in.Destination, stageName(p), nil
	case "import.prepare-disks":
		return in.Destination, diskSetStage(p), nil
	case "import.prepare-install":
		return in.Destination, installStage(p), nil
	}
	return "", "", domain.Fail("INVALID_INPUT", "operation is not an image preparation")
}

// busy refuses while any job that names this preparation is running or needs
// recovery. Matching the ID anywhere in a plan input can only over-refuse.
func (s *DiscardService) busy(source, self string) error {
	jobs, err := s.Store.Jobs()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.ID == source || j.ID == self || (domain.Terminal(j.State) && j.State != "partial" && j.State != "recovery-required") {
			continue
		}
		_, raw, err := s.Store.Plan(j.PlanID)
		if err != nil {
			return err
		}
		if bytes.Contains(raw, []byte(source)) {
			return domain.Fail("RESOURCE_BUSY", "job "+j.ID+" still uses these prepared images; finish or recover it first")
		}
	}
	return nil
}

func folderIdentity(st fs.FileInfo) (string, error) {
	n, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || st.Mode()&fs.ModeSymlink != 0 {
		return "", domain.Fail("SOURCE_CHANGED", "not a plain folder")
	}
	return fmt.Sprintf("%d:%d", n.Dev, n.Ino), nil
}

// observe binds the parent by its identity and each child folder by its own;
// folder times change as other imports come and go beside it.
func observe(in discardInput) (*os.Root, map[string]string, error) {
	st, err := os.Lstat(in.Parent)
	if err != nil {
		return nil, nil, err
	}
	parent, err := folderIdentity(st)
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(in.Parent)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]string{"parent": parent}
	for key, name := range map[string]string{"stage": in.Stage, "artifact": in.Artifact} {
		st, err := root.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			out[key] = "absent"
			continue
		}
		if err == nil {
			out[key], err = folderIdentity(st)
		}
		if err != nil {
			root.Close()
			return nil, nil, err
		}
	}
	if st, err = root.Lstat("."); err != nil {
		root.Close()
		return nil, nil, err
	}
	if opened, err := folderIdentity(st); err != nil || opened != parent {
		root.Close()
		return nil, nil, domain.Fail("SOURCE_CHANGED", "import folder changed while opening it")
	}
	return root, out, nil
}

func (s *DiscardService) Plan(ctx context.Context, uid uint32, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	if !discardUUID.MatchString(r.ID) || r.Path != "" || r.After != 0 || r.Apply != nil || r.Action != "discard" {
		return empty, domain.Fail("INVALID_INPUT", "removing a prepared copy needs one preparation operation ID")
	}
	keep := false
	for key, value := range r.Input {
		b, ok := value.(bool)
		if key != "keepImages" || !ok {
			return empty, domain.Fail("INVALID_INPUT", "only keepImages true or false is accepted")
		}
		keep = b
	}
	destination, stage, err := s.preparation(uid, r.ID)
	if err != nil {
		return empty, err
	}
	if raw, err := s.Store.MetadataBytes(discardedKind, r.ID); err != nil || len(raw) != 0 {
		return empty, errors.Join(err, domain.Fail("INVALID_STATE", "these prepared images were already removed"))
	}
	if err = s.busy(r.ID, ""); err != nil {
		return empty, err
	}
	in := discardInput{Version: 1, SourceOperationID: r.ID, Parent: filepath.Dir(destination), Stage: stage, Artifact: filepath.Base(destination), RemoveArtifact: !keep}
	root, before, err := observe(in)
	if err != nil {
		return empty, err
	}
	root.Close()
	step := func(id, action, predicate string) domain.Step {
		return domain.Step{ID: id, Action: action, Preconditions: []string{"same import folder and folder identities", "no running job uses the preparation"}, Idempotency: "reconcile-before-retry", Compensation: "None; removed files are not recoverable, and the original source and created VMs are untouched", Reconciliation: "Observe that the reviewed folder is absent", CompletionPredicate: predicate}
	}
	steps := []domain.Step{}
	if before["stage"] != "absent" {
		steps = append(steps, step("stage", "import.discard.stage", "The preparation's work folder is absent"))
	}
	if in.RemoveArtifact && before["artifact"] != "absent" {
		steps = append(steps, step("artifact", "import.discard.artifact", "The prepared images folder is absent"))
	}
	if len(steps) == 0 {
		return empty, domain.Fail("INVALID_STATE", "nothing is left to remove for this preparation")
	}
	risks := []string{"Deletes the preparation's work folder with its extracted source members and conversion files", "The original source files and any VM already created keep their own copies"}
	if in.RemoveArtifact {
		risks = append(risks, "Deletes the prepared images; Create VM can no longer use this preparation")
	}
	return s.Engine.Plan(ctx, uid, "local", "import.discard", []string{"import-artifact:local:" + destination}, before, in, steps, []string{"delete-prepared-copy"}, risks)
}

func parseDiscard(p domain.Plan, raw []byte) (discardInput, error) {
	var in discardInput
	if wire.Decode(raw, &in) != nil || in.Version != 1 || p.Operation != "import.discard" || p.ConnectionID != "local" || !discardUUID.MatchString(in.SourceOperationID) || !filepath.IsAbs(in.Parent) || filepath.Clean(in.Parent) != in.Parent || in.Stage == "" || in.Artifact == "" || filepath.Base(in.Stage) != in.Stage || filepath.Base(in.Artifact) != in.Artifact || in.Stage == in.Artifact {
		return in, domain.Fail("INVALID_INPUT", "invalid prepared-copy removal binding")
	}
	if len(p.ResourceIDs) != 1 || p.ResourceIDs[0] != "import-artifact:local:"+filepath.Join(in.Parent, in.Artifact) || p.Before["parent"] == "" || p.Before["stage"] == "" || p.Before["artifact"] == "" {
		return in, domain.Fail("INVALID_INPUT", "invalid prepared-copy removal binding")
	}
	return in, nil
}

func (s *DiscardService) Review(ctx context.Context, p domain.Plan, raw []byte) (map[string]any, error) {
	in, err := parseDiscard(p, raw)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"sourceOperationID": in.SourceOperationID, "workFolder": filepath.Join(in.Parent, in.Stage), "removesImages": in.RemoveArtifact, "sourceFilesChanged": false, "vmVolumesChanged": false}
	if in.RemoveArtifact {
		out["imagesFolder"] = filepath.Join(in.Parent, in.Artifact)
	}
	return out, ctx.Err()
}

// Estimate reports the space the reviewed folders use now.
func (s *DiscardService) Estimate(ctx context.Context, p domain.Plan, raw []byte) (domain.Estimates, error) {
	in, err := parseDiscard(p, raw)
	if err != nil {
		return domain.Estimates{}, err
	}
	root, err := os.OpenRoot(in.Parent)
	if err != nil {
		return domain.Estimates{}, err
	}
	defer root.Close()
	var total int64
	names := []string{in.Stage}
	if in.RemoveArtifact {
		names = append(names, in.Artifact)
	}
	for _, name := range names {
		_ = fs.WalkDir(root.FS(), name, func(_ string, d fs.DirEntry, err error) error {
			if err != nil || ctx.Err() != nil {
				return fs.SkipAll
			}
			if info, e := d.Info(); e == nil && d.Type().IsRegular() {
				if n, ok := info.Sys().(*syscall.Stat_t); ok {
					total += n.Blocks * 512
				}
			}
			return nil
		})
	}
	return domain.Estimates{Notes: fmt.Sprintf("Frees about %.1f GiB of disk space.", float64(total)/(1<<30))}, ctx.Err()
}

func (s *DiscardService) Validate(ctx context.Context, p domain.Plan, raw []byte) error {
	in, err := parseDiscard(p, raw)
	if err != nil {
		return err
	}
	if _, _, err = s.preparation(p.ActorUID, in.SourceOperationID); err != nil {
		return err
	}
	if err = s.busy(in.SourceOperationID, operations.OperationID(ctx)); err != nil {
		return err
	}
	root, now, err := observe(in)
	if err != nil {
		return err
	}
	root.Close()
	if now["parent"] != p.Before["parent"] {
		return domain.Fail("STALE_PLAN", "import folder changed since review")
	}
	for _, key := range []string{"stage", "artifact"} {
		if now[key] != p.Before[key] && now[key] != "absent" {
			return domain.Fail("STALE_PLAN", "a reviewed folder was replaced since review")
		}
	}
	return ctx.Err()
}

func (s *DiscardService) Execute(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) error {
	in, err := parseDiscard(p, raw)
	if err != nil {
		return err
	}
	name := in.Stage
	if step.ID == "artifact" {
		if !in.RemoveArtifact {
			return domain.Fail("INVALID_INPUT", "removing the prepared images was not reviewed")
		}
		// Recorded first, so an interrupted removal is never offered again.
		record := discardRecord{Version: 1, OperationID: operations.OperationID(ctx), PlanID: p.ID}
		if existing, err := s.Store.MetadataBytes(discardedKind, in.SourceOperationID); err != nil {
			return err
		} else if len(existing) == 0 {
			if err = s.Store.ComparePut(discardedKind, in.SourceOperationID, nil, record); err != nil {
				return err
			}
		}
		name = in.Artifact
	} else if step.ID != "stage" {
		return domain.Fail("INVALID_INPUT", "unknown prepared-copy removal step")
	}
	root, now, err := observe(in)
	if err != nil {
		return err
	}
	defer root.Close()
	if now["parent"] != p.Before["parent"] || (now[step.ID] != p.Before[step.ID] && now[step.ID] != "absent") {
		return domain.Fail("STALE_PLAN", "a reviewed folder changed before removal")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return root.RemoveAll(name)
}

func (s *DiscardService) Reconcile(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) (bool, error) {
	in, err := parseDiscard(p, raw)
	if err != nil {
		return false, err
	}
	root, now, err := observe(in)
	if err != nil {
		return false, err
	}
	root.Close()
	return now[step.ID] == "absent", ctx.Err()
}
