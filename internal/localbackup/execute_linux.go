//go:build linux && amd64

package localbackup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/restic"
	"virmill.local/core/internal/coldstore"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

const records = "local-backup-v1"

type progress struct {
	Version        int              `json:"version"`
	Kind           string           `json:"kind"`
	OperationID    string           `json:"operationID"`
	PlanID         string           `json:"planID"`
	PlanDigest     string           `json:"planDigest"`
	InputDigest    string           `json:"inputDigest"`
	Phase          string           `json:"phase"`
	RepositoryRoot *binding         `json:"repositoryRoot"`
	WorkingRoot    *binding         `json:"workingRoot"`
	Destination    string           `json:"destination"`
	Snapshot       *restic.Snapshot `json:"snapshot"`
	Config         *binding         `json:"config"`
	Proof          *Proof           `json:"proof"`
}

func (s *Service) save(state progress, previous *[]byte) error {
	if err := s.Engine.Store.ComparePut(records, state.OperationID, *previous, state); err != nil {
		return err
	}
	raw, err := json.Marshal(state)
	if err == nil {
		*previous = raw
	}
	return err
}
func (s *Service) load(p domain.Plan, job string) (progress, []byte, error) {
	var state progress
	var raw json.RawMessage
	if err := s.Engine.Store.Get(records, job, &raw); err != nil {
		return state, nil, recovery("durable local backup intent or proof is unavailable")
	}
	if err := wire.Decode(raw, &state); err != nil {
		return state, nil, recovery("local backup journal shape is invalid")
	}
	original, err := operations.Canonical(raw)
	if err != nil {
		return state, nil, err
	}
	canonical, err := operations.Canonical(state)
	if err != nil || string(canonical) != string(original) || state.Version != 1 || state.Kind == "" || state.OperationID != job || state.PlanID != p.ID || state.PlanDigest != p.Digest || state.InputDigest != p.InputDigest || operation(state.Kind) != p.Operation {
		return state, nil, recovery("local backup journal does not bind the exact approved operation")
	}
	return state, raw, nil
}
func (s *Service) canceled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	yes, err := operations.CancellationRequested(ctx, s.Engine.Store)
	if err != nil {
		return err
	}
	if yes {
		return recovery("cancellation reached a safe boundary; retained repository effects require observation")
	}
	return nil
}
func (h *handler) Execute(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) error {
	in, err := decode(raw)
	if err != nil {
		return err
	}
	s := h.service
	job := operations.OperationID(ctx)
	if step.ID != h.kind || p.Operation != operation(h.kind) || in.Kind != h.kind || !validID(job) {
		return invalid("bound versioned local backup step required")
	}
	if err = s.canceled(ctx); err != nil {
		return err
	}
	state := progress{Version: 1, Kind: in.Kind, OperationID: job, PlanID: p.ID, PlanDigest: p.Digest, InputDigest: p.InputDigest, Phase: "intent", Config: in.Config}
	if in.Root.Exists {
		root := in.Root
		state.RepositoryRoot = &root
	}
	var previous []byte
	if err = s.save(state, &previous); err != nil {
		return recovery("local backup already has an intent; never replay an external operation")
	}
	if err = operations.Note(ctx, s.Engine.Store, "Intent persisted: "+description(h.kind)); err != nil {
		return err
	}
	switch h.kind {
	case "repository-init":
		if err = s.Tool.Init(ctx, in.Repository); err != nil {
			return err
		}
		root, e := observeDirectory(in.Repository.Path, false)
		if e != nil {
			return e
		}
		state.RepositoryRoot = &root
		config, e := observeFile(filepath.Join(in.Repository.Path, "config"), false)
		if e != nil {
			return e
		}
		state.Config = &config
	case "repository-check":
		if err = s.Tool.Check(ctx, in.Repository); err != nil {
			return err
		}
	case "create":
		if _, err = inspect(ctx, s.Catalog, in); err != nil {
			return err
		}
		snapshot, e := s.Tool.Backup(ctx, in.Repository, filepath.Join(s.Catalog, in.CaptureID), job)
		if e != nil {
			return e
		}
		if !snapshotMatches(snapshot, job, filepath.Join(s.Catalog, in.CaptureID)) {
			return recovery("repository backup did not return an exact operation-tagged source snapshot")
		}
		state.Snapshot = &snapshot
		state.Phase = "snapshot-written"
		if err = s.save(state, &previous); err != nil {
			return err
		}
		if err = s.canceled(ctx); err != nil {
			return err
		}
		if _, err = inspect(ctx, s.Catalog, in); err != nil {
			return err
		}
		if in.Cache == nil {
			return invalid("roundtrip cache binding missing")
		}
		if !in.Cache.Exists {
			if err = createDirectory(s.Cache); err != nil {
				return err
			}
		} else if err = checkDirectory(*in.Cache); err != nil {
			return err
		}
		work := filepath.Join(s.Cache, job)
		if err = createDirectory(work); err != nil {
			return err
		}
		root, e := observeDirectory(work, false)
		if e != nil {
			return e
		}
		state.WorkingRoot = &root
		state.Destination = filepath.Join(work, in.CaptureID)
		state.Phase = "roundtrip-intent"
		if err = s.save(state, &previous); err != nil {
			return err
		}
		if err = operations.Note(ctx, s.Engine.Store, "Intent persisted: restore exact repository snapshot to independent roundtrip staging"); err != nil {
			return err
		}
		if err = s.canceled(ctx); err != nil {
			return err
		}
		if err = s.Tool.Restore(ctx, in.Repository, snapshot.ID, state.Destination); err != nil {
			return err
		}
		if _, err = coldstore.SealRestored(ctx, work, in.CaptureID, in.ManifestSHA256); err != nil {
			return err
		}
	case "restore":
		if in.Catalog == nil {
			return invalid("restore catalog binding missing")
		}
		if !in.Catalog.Exists {
			if err = createDirectory(s.Catalog); err != nil {
				return err
			}
		} else if err = checkDirectory(*in.Catalog); err != nil {
			return err
		}
		root, e := observeDirectory(s.Catalog, false)
		if e != nil {
			return e
		}
		state.WorkingRoot = &root
		state.Destination = filepath.Join(s.Catalog, in.CaptureID)
		state.Phase = "restore-intent"
		if err = s.save(state, &previous); err != nil {
			return err
		}
		if err = operations.Note(ctx, s.Engine.Store, "Intent persisted: recover exact snapshot into a new catalog capture identity"); err != nil {
			return err
		}
		if err = s.canceled(ctx); err != nil {
			return err
		}
		if err = s.Tool.Restore(ctx, in.Repository, in.SnapshotID, state.Destination); err != nil {
			return err
		}
		if _, err = coldstore.SealRestored(ctx, s.Catalog, in.CaptureID, in.ManifestSHA256); err != nil {
			return err
		}
	default:
		return invalid("unknown local backup effect")
	}
	if err = s.canceled(ctx); err != nil {
		return err
	}
	if state.RepositoryRoot == nil || state.Config == nil {
		return recovery("repository identity proof missing after operation")
	}
	if err = checkDirectory(*state.RepositoryRoot); err != nil {
		return err
	}
	if err = checkFile(*state.Config, false); err != nil {
		return err
	}
	proof := makeProof(p, in, state)
	state.Proof = &proof
	state.Phase = "verified"
	return s.save(state, &previous)
}
func snapshotMatches(snapshot restic.Snapshot, job, source string) bool {
	return validHash(snapshot.ID) && !snapshot.Time.IsZero() && len(snapshot.Tags) == 1 && snapshot.Tags[0] == "virmill-operation:"+job && len(snapshot.Paths) == 1 && snapshot.Paths[0] == source
}
func makeProof(p domain.Plan, in recipe, state progress) Proof {
	proof := Proof{Version: 1, OperationID: state.OperationID, PlanID: p.ID, PlanDigest: p.Digest, Kind: in.Kind, ActorUID: p.ActorUID, Connection: p.ConnectionID, Repository: in.Repository.Path, Tool: in.Tool, RepositoryConfig: state.Config.Identity, CaptureID: in.CaptureID, ManifestSHA256: in.ManifestSHA256, Snapshot: state.Snapshot, SnapshotID: in.SnapshotID, Destination: state.Destination, RepositoryDataChecked: in.Kind == "repository-check", RoundtripVerified: in.Kind == "create", RecoveredSetVerified: in.Kind == "restore", VerifiedAt: time.Now().UTC()}
	if state.Snapshot != nil {
		proof.SnapshotID = state.Snapshot.ID
	}
	return proof
}
func proofMatches(p domain.Plan, in recipe, state progress) bool {
	if state.Proof == nil || state.Config == nil || state.Proof.VerifiedAt.IsZero() {
		return false
	}
	expected := makeProof(p, in, state)
	expected.VerifiedAt = state.Proof.VerifiedAt
	return same(*state.Proof, expected)
}
func (h *handler) Reconcile(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) (bool, error) {
	in, err := decode(raw)
	if err != nil {
		return false, err
	}
	s := h.service
	job := operations.OperationID(ctx)
	if !validID(job) || step.ID != h.kind || p.Operation != operation(h.kind) || in.Kind != h.kind || in.ActorUID != p.ActorUID || in.Connection != p.ConnectionID {
		return false, invalid("reconciliation requires the original operation identity")
	}
	state, previous, err := s.load(p, job)
	if err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	if state.RepositoryRoot == nil || state.Config == nil || state.RepositoryRoot.Path != in.Repository.Path || state.Config.Path != filepath.Join(in.Repository.Path, "config") {
		return false, recovery("repository effect lacks its bound identity proof")
	}
	if h.kind != "repository-init" && (!same(*state.RepositoryRoot, in.Root) || !same(state.Config, in.Config)) {
		return false, recovery("repository generation differs from original intent")
	}
	if err = checkDirectory(*state.RepositoryRoot); err != nil {
		return false, err
	}
	if err = checkFile(*state.Config, false); err != nil {
		return false, err
	}
	switch h.kind {
	case "create":
		if !proofMatches(p, in, state) || state.Phase != "verified" || state.Snapshot == nil || !snapshotMatches(*state.Snapshot, job, filepath.Join(s.Catalog, in.CaptureID)) {
			return false, recovery("backup lacks an exact durable roundtrip completion proof; no backup or restore will be replayed")
		}
		work := filepath.Join(s.Cache, job)
		if state.WorkingRoot == nil || state.WorkingRoot.Path != work || state.Destination != filepath.Join(work, in.CaptureID) {
			return false, recovery("roundtrip location differs from durable intent")
		}
		if err = checkDirectory(*state.WorkingRoot); err != nil {
			return false, err
		}
		if _, err = inspect(ctx, work, in); err != nil {
			return false, err
		}
		if err = checkFile(in.Password, true); err != nil {
			return false, err
		}
		fresh, e := s.Tool.Identity(ctx)
		if e != nil {
			return false, e
		}
		if fresh != in.Tool {
			return false, domain.Fail("SOURCE_CHANGED", "restic identity changed before repository observation")
		}
		list, e := s.Tool.Observe(ctx, in.Repository, job)
		if e != nil {
			return false, e
		}
		if len(list) != 1 || !same(list[0], *state.Snapshot) {
			return false, recovery("repository operation snapshot is missing, changed or ambiguous")
		}
	case "restore":
		if state.Phase != "restore-intent" && state.Phase != "verified" || state.WorkingRoot == nil || state.WorkingRoot.Path != s.Catalog || state.Destination != filepath.Join(s.Catalog, in.CaptureID) || state.Snapshot != nil {
			return false, recovery("restore location differs from the original intent")
		}
		if err = checkDirectory(*state.WorkingRoot); err != nil {
			return false, err
		}
		if _, err = coldstore.SealRestored(ctx, s.Catalog, in.CaptureID, in.ManifestSHA256); err != nil {
			return false, err
		}
		if state.Proof == nil {
			proof := makeProof(p, in, state)
			state.Proof = &proof
			state.Phase = "verified"
			if err = s.save(state, &previous); err != nil {
				return false, err
			}
		}
		if !proofMatches(p, in, state) {
			return false, recovery("restored-set proof differs from approved identity")
		}
	default:
		if state.Phase != "verified" || !proofMatches(p, in, state) {
			return false, recovery("repository operation lacks an acknowledged durable completion proof")
		}
	}
	return true, ctx.Err()
}
func (s *Service) Result(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validID(r.ID) || r.Path != "" || r.Action != "" || r.After != 0 || r.Apply != nil || len(r.Input) != 0 {
		return nil, invalid("backup result accepts one operation UUID")
	}
	job, err := s.Engine.Store.Job(r.ID)
	if err != nil {
		return nil, err
	}
	p, raw, err := s.Engine.Store.Plan(job.PlanID)
	if err != nil {
		return nil, err
	}
	in, err := decode(raw)
	if err != nil {
		return nil, err
	}
	pd, e := operations.PlanDigest(p)
	rd, e2 := operations.Digest(json.RawMessage(raw))
	if e != nil || e2 != nil || pd != p.Digest || rd != p.InputDigest {
		return nil, domain.Fail("SOURCE_CHANGED", "stored backup plan or recipe digest differs")
	}
	if p.ActorUID != uid || uid != uint32(os.Geteuid()) || r.Connection != p.ConnectionID || p.Operation != operation(in.Kind) {
		return nil, domain.Fail("PERMISSION_DENIED", "backup result belongs to a different actor, connection or workflow")
	}
	if job.State != "succeeded" {
		return nil, recovery("local backup is not proven complete; inspect the retained operation")
	}
	state, _, err := s.load(p, r.ID)
	if err != nil {
		return nil, err
	}
	if state.Phase != "verified" || !proofMatches(p, in, state) {
		return nil, recovery("local backup completion proof is missing or inconsistent")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return state.Proof, nil
}
