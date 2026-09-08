//go:build linux && amd64

// Package localbackup coordinates durable local restic operations through the
// shared engine. It does not define, start or otherwise mutate a guest.
package localbackup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/backend/restic"
	"virmill.local/core/internal/coldstore"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

type ResticTool interface {
	Identity(context.Context) (restic.Identity, error)
	Init(context.Context, restic.Repository) error
	Backup(context.Context, restic.Repository, string, string) (restic.Snapshot, error)
	Observe(context.Context, restic.Repository, string) ([]restic.Snapshot, error)
	Restore(context.Context, restic.Repository, string, string) error
	Check(context.Context, restic.Repository) error
}
type Service struct {
	Engine         *operations.Engine
	Tool           ResticTool
	Catalog, Cache string
}
type handler struct {
	service *Service
	kind    string
}
type binding struct {
	Path     string                `json:"path"`
	Exists   bool                  `json:"exists"`
	Identity fileidentity.Identity `json:"identity"`
	Parent   fileidentity.Identity `json:"parent"`
}
type recipe struct {
	Version        int               `json:"version"`
	Kind           string            `json:"kind"`
	ActorUID       uint32            `json:"actorUID"`
	Connection     string            `json:"connection"`
	Repository     restic.Repository `json:"repository"`
	Root           binding           `json:"root"`
	Config         *binding          `json:"config"`
	Password       binding           `json:"password"`
	Tool           restic.Identity   `json:"tool"`
	Catalog        *binding          `json:"catalog"`
	Cache          *binding          `json:"cache"`
	CaptureID      string            `json:"captureID"`
	ManifestSHA256 string            `json:"manifestSHA256"`
	SnapshotID     string            `json:"snapshotID"`
}

// Proof records the observed scope of a completed repository operation. It is
// not a guest-readiness result or an authorization token for another operation.
type Proof struct {
	Version               int                   `json:"version"`
	OperationID           string                `json:"operationID"`
	PlanID                string                `json:"planID"`
	PlanDigest            string                `json:"planDigest"`
	Kind                  string                `json:"kind"`
	ActorUID              uint32                `json:"actorUID"`
	Connection            string                `json:"connection"`
	Repository            string                `json:"repository"`
	Tool                  restic.Identity       `json:"tool"`
	RepositoryConfig      fileidentity.Identity `json:"repositoryConfig"`
	CaptureID             string                `json:"captureID"`
	ManifestSHA256        string                `json:"manifestSHA256"`
	Snapshot              *restic.Snapshot      `json:"snapshot"`
	SnapshotID            string                `json:"snapshotID"`
	Destination           string                `json:"destination"`
	RepositoryDataChecked bool                  `json:"repositoryDataChecked"`
	RoundtripVerified     bool                  `json:"roundtripVerified"`
	RecoveredSetVerified  bool                  `json:"recoveredSetVerified"`
	GuestBootVerified     bool                  `json:"guestBootVerified"`
	VerifiedAt            time.Time             `json:"verifiedAt"`
}

var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validID(id string) bool {
	return uuid.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}
func validHash(id string) bool     { return hash.MatchString(id) && id != strings.Repeat("0", 64) }
func operation(kind string) string { return "backup.local-" + kind + "-v1" }
func allowed(kind string) bool {
	return kind == "repository-init" || kind == "repository-check" || kind == "create" || kind == "restore"
}
func recovery(message string) error { return domain.Fail("RECOVERY_REQUIRED", message) }
func invalid(message string) error  { return domain.Fail("INVALID_INPUT", message) }
func same(a, b any) bool            { return reflect.DeepEqual(a, b) }

func Register(a *app.Service, data, cache string) {
	s := &Service{Engine: a.Engine, Tool: restic.Tool{}, Catalog: filepath.Join(data, "captures"), Cache: filepath.Join(cache, "local-backup")}
	s.Register(a)
}
func (s *Service) Register(a *app.Service) {
	if a.Extensions == nil {
		a.Extensions = map[string]func(context.Context, uint32, app.Request) (any, error){}
	}
	for _, pair := range []struct{ method, kind string }{{"backup.repository.init", "repository-init"}, {"backup.repository.check", "repository-check"}, {"backup.create", "create"}, {"backup.restore", "restore"}} {
		h := &handler{service: s, kind: pair.kind}
		a.Engine.Handlers[operation(pair.kind)] = h
		a.Extensions[pair.method] = h.Plan
	}
	a.Extensions["backup.result"] = s.Result
}
func input(r app.Request, keys ...string) (map[string]string, error) {
	if len(r.Input) != len(keys) {
		return nil, invalid("repository input fields must match this action exactly")
	}
	out := map[string]string{}
	for _, key := range keys {
		v, ok := r.Input[key].(string)
		if !ok || v == "" {
			return nil, invalid("required repository input is missing or not a string")
		}
		out[key] = v
	}
	return out, nil
}
func (h *handler) Plan(ctx context.Context, uid uint32, r app.Request) (any, error) {
	s := h.service
	if uid == 0 || uid != uint32(os.Geteuid()) || r.Apply != nil || r.After != 0 || r.Connection != "qemu:///system" && r.Connection != "qemu:///session" {
		return nil, invalid("ordinary local actor and explicit local connection required")
	}
	action := strings.TrimPrefix(h.kind, "repository-")
	if r.Action != "" && r.Action != action {
		return nil, invalid("action does not match repository workflow")
	}
	var args map[string]string
	var err error
	in := recipe{Version: 1, Kind: h.kind, ActorUID: uid, Connection: r.Connection}
	if h.kind == "repository-init" || h.kind == "repository-check" {
		if r.ID != "" || r.Path == "" {
			return nil, invalid("repository action requires one local path")
		}
		args, err = input(r, "passwordFile")
		in.Repository.Path = r.Path
	} else {
		if r.Path != "" || r.ID == "" {
			return nil, invalid("backup action requires one stable capture or snapshot ID")
		}
		keys := []string{"repository", "passwordFile"}
		if h.kind == "restore" {
			keys = append(keys, "captureID", "manifestSHA256")
		}
		args, err = input(r, keys...)
		in.Repository.Path = args["repository"]
		if h.kind == "create" {
			in.CaptureID = r.ID
		} else {
			in.SnapshotID = r.ID
			in.CaptureID = args["captureID"]
			in.ManifestSHA256 = args["manifestSHA256"]
		}
	}
	if err != nil {
		return nil, err
	}
	in.Repository.PasswordFile = args["passwordFile"]
	if !localPath(in.Repository.Path) || !localPath(in.Repository.PasswordFile) || within(in.Repository.PasswordFile, in.Repository.Path) {
		return nil, invalid("canonical local repository and external credential paths required")
	}
	in.Password, err = observeFile(in.Repository.PasswordFile, true)
	if err != nil {
		return nil, err
	}
	in.Root, err = observeDirectory(in.Repository.Path, h.kind == "repository-init")
	if err != nil {
		return nil, err
	}
	if h.kind == "repository-init" {
		if in.Root.Exists {
			return nil, invalid("initialization requires an absent repository")
		}
	} else {
		config, e := observeFile(filepath.Join(in.Repository.Path, "config"), false)
		if e != nil {
			return nil, e
		}
		in.Config = &config
	}
	if h.kind == "create" || h.kind == "restore" {
		if !validID(in.CaptureID) {
			return nil, invalid("canonical nonzero capture UUID required")
		}
		catalog, e := observeDirectory(s.Catalog, h.kind == "restore")
		if e != nil {
			return nil, e
		}
		in.Catalog = &catalog
		dest := filepath.Join(s.Catalog, in.CaptureID)
		if !disjoint(in.Repository.Path, s.Catalog) || within(in.Repository.PasswordFile, s.Catalog) {
			return nil, invalid("repository and credential must remain outside capture catalog")
		}
		if h.kind == "create" {
			capture, e := coldstore.Inspect(ctx, s.Catalog, in.CaptureID)
			if e != nil {
				return nil, e
			}
			if !capture.Manifest.IndependentlyRecoverable {
				return nil, domain.Fail("INCOMPLETE_BACKUP", "encrypted backup requires an independently recoverable capture with no unresolved dependencies")
			}
			in.ManifestSHA256 = capture.ManifestSHA256
			cache, e := observeDirectory(s.Cache, true)
			if e != nil {
				return nil, e
			}
			in.Cache = &cache
			if !disjoint(s.Cache, s.Catalog) || !disjoint(s.Cache, in.Repository.Path) || within(in.Repository.PasswordFile, s.Cache) {
				return nil, invalid("roundtrip cache must be distinct from source, repository and credential")
			}
		} else {
			if !validHash(in.SnapshotID) || !validHash(in.ManifestSHA256) {
				return nil, invalid("restore requires full snapshot and manifest hashes")
			}
			if err = absent(dest); err != nil {
				return nil, err
			}
		}
	}
	in.Tool, err = s.Tool.Identity(ctx)
	if err != nil {
		return nil, err
	}
	resources := resources(in)
	before := map[string]string{"repository": in.Root.Identity.Generation, "credential": in.Password.Identity.Generation, "restic": in.Tool.SHA256}
	acks := []string{"local-storage-mutation"}
	if h.kind == "create" {
		acks = append(acks, "encrypted-backup")
	}
	if h.kind == "restore" {
		acks = append(acks, "recovered-set-publication")
	}
	return s.Engine.Plan(ctx, uid, r.Connection, operation(h.kind), resources, before, in, []domain.Step{{ID: h.kind, Action: description(h.kind), Preconditions: []string{"exact reviewed local paths, tool and capture"}, Idempotency: "one durable intent; never replay repository write or restore", Compensation: "retain partial repository and staging", Reconciliation: "observe original effect and exact durable proof", CompletionPredicate: "repository operation and complete capture verification at declared scope"}}, acks, []string{"No guest is started or defined. Credentials remain external. Failed repository or restore effects and partial staging are retained for explicit recovery."})
}
func description(kind string) string {
	switch kind {
	case "repository-init":
		return "initialize one new local encrypted repository"
	case "repository-check":
		return "read and verify all repository data while honoring repository locks"
	case "create":
		return "upload one complete capture and verify a full restored roundtrip"
	default:
		return "restore and verify one new local capture set"
	}
}
func resources(in recipe) []string {
	digest, _ := operations.Digest(in.Repository.Path)
	out := []string{"restic-repository|" + digest}
	if in.CaptureID != "" {
		out = append(out, "cold-capture|"+in.CaptureID)
	}
	return out
}
func decode(raw []byte) (recipe, error) {
	var in recipe
	if err := wire.Decode(raw, &in); err != nil {
		return in, invalid("stored local backup recipe is invalid")
	}
	canonical, err := operations.Canonical(in)
	if err != nil {
		return in, err
	}
	original, err := operations.Canonical(json.RawMessage(raw))
	if err != nil || string(canonical) != string(original) {
		return in, invalid("stored local backup recipe has missing or aliased fields")
	}
	if in.Version != 1 || !allowed(in.Kind) || in.ActorUID == 0 || in.ActorUID != uint32(os.Geteuid()) || in.Connection != "qemu:///system" && in.Connection != "qemu:///session" || in.Root.Path != in.Repository.Path || in.Password.Path != in.Repository.PasswordFile || in.Tool.Path != "/usr/bin/restic" || !validHash(in.Tool.SHA256) || in.Tool.Version == "" {
		return in, invalid("stored local backup recipe version or identity differs")
	}
	if !localPath(in.Repository.Path) || !localPath(in.Repository.PasswordFile) || within(in.Repository.PasswordFile, in.Repository.Path) {
		return in, invalid("stored repository path scope differs")
	}
	if in.Kind == "repository-init" {
		if in.Root.Exists || in.Config != nil {
			return in, invalid("initialization recipe does not require a new repository")
		}
	} else if !in.Root.Exists || in.Config == nil || in.Config.Path != filepath.Join(in.Repository.Path, "config") {
		return in, invalid("existing repository configuration binding is missing")
	}
	if in.Kind == "create" || in.Kind == "restore" {
		if in.Catalog == nil || !validID(in.CaptureID) || !validHash(in.ManifestSHA256) || !disjoint(in.Catalog.Path, in.Repository.Path) || within(in.Repository.PasswordFile, in.Catalog.Path) {
			return in, invalid("stored capture scope differs")
		}
		if in.Kind == "create" {
			if !in.Catalog.Exists || in.Cache == nil || in.SnapshotID != "" || !disjoint(in.Cache.Path, in.Catalog.Path) || !disjoint(in.Cache.Path, in.Repository.Path) || within(in.Repository.PasswordFile, in.Cache.Path) {
				return in, invalid("stored roundtrip scope differs")
			}
		} else if in.Cache != nil || !validHash(in.SnapshotID) {
			return in, invalid("stored restore selection differs")
		}
	} else if in.Catalog != nil || in.Cache != nil || in.CaptureID != "" || in.ManifestSHA256 != "" || in.SnapshotID != "" {
		return in, invalid("repository action contains unrelated capture authority")
	}
	return in, nil
}
func (h *handler) Validate(ctx context.Context, p domain.Plan, raw []byte) error {
	in, err := decode(raw)
	if err != nil {
		return err
	}
	s := h.service
	if p.Operation != operation(h.kind) || in.Kind != h.kind || p.ActorUID != in.ActorUID || p.ConnectionID != in.Connection {
		return invalid("local backup plan identity differs")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = checkDirectory(in.Root); err != nil {
		return err
	}
	if err = checkFile(in.Password, true); err != nil {
		return err
	}
	if in.Config != nil {
		if err = checkFile(*in.Config, false); err != nil {
			return err
		}
	}
	if in.Catalog != nil {
		if in.Catalog.Path != s.Catalog {
			return invalid("capture catalog differs from reviewed service scope")
		}
		if err = checkDirectory(*in.Catalog); err != nil {
			return err
		}
	}
	if in.Cache != nil {
		if in.Cache.Path != s.Cache {
			return invalid("roundtrip cache differs from reviewed service scope")
		}
		if err = checkDirectory(*in.Cache); err != nil {
			return err
		}
	}
	if h.kind == "create" {
		if _, err = inspect(ctx, s.Catalog, in); err != nil {
			return err
		}
	}
	if h.kind == "restore" {
		if err = absent(filepath.Join(s.Catalog, in.CaptureID)); err != nil {
			return err
		}
	}
	// Plan already measured the executable once. Apply and execution validation
	// have a sealed digest and must repeat the real identity observation.
	if p.Digest != "" {
		fresh, e := s.Tool.Identity(ctx)
		if e != nil {
			return e
		}
		if fresh != in.Tool {
			return domain.Fail("SOURCE_CHANGED", "restic executable or version changed after review")
		}
	}
	return ctx.Err()
}
func (h *handler) Review(ctx context.Context, p domain.Plan, raw []byte) (map[string]any, error) {
	in, err := decode(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{"kind": in.Kind, "repository": in.Repository.Path, "passwordFile": in.Repository.PasswordFile, "tool": in.Tool, "captureID": in.CaptureID, "manifestSHA256": in.ManifestSHA256, "snapshotID": in.SnapshotID, "roundtripRequired": in.Kind == "create", "recoveredSetOnly": in.Kind == "restore", "guestBootVerified": false, "originalsPreserved": true, "partialEffects": "retained; never replayed"}, ctx.Err()
}
func (h *handler) Estimate(ctx context.Context, p domain.Plan, raw []byte) (domain.Estimates, error) {
	in, err := decode(raw)
	if err != nil {
		return domain.Estimates{}, err
	}
	out := domain.Estimates{Notes: "Repository growth and full restore staging need free capacity; deduplication savings are not assumed."}
	if in.Kind == "create" {
		receipt, e := inspect(ctx, h.service.Catalog, in)
		if e != nil {
			return out, e
		}
		for _, m := range receipt.Manifest.Members {
			if m.Size < 0 || uint64(m.Size) > ^uint64(0)-out.AdditionalBytes {
				return out, invalid("capture size sum overflow")
			}
			out.AdditionalBytes += uint64(m.Size)
		}
		if out.AdditionalBytes > (^uint64(0)-(16<<20))/2 {
			return out, invalid("backup estimate overflow")
		}
		out.AdditionalBytes = 2*out.AdditionalBytes + (16 << 20)
	}
	return out, ctx.Err()
}
func inspect(ctx context.Context, catalog string, in recipe) (coldstore.Receipt, error) {
	r, err := coldstore.Inspect(ctx, catalog, in.CaptureID)
	if err != nil {
		return coldstore.Receipt{}, err
	}
	if r.ManifestSHA256 != in.ManifestSHA256 || r.SnapshotID != in.CaptureID || !r.Manifest.IndependentlyRecoverable {
		return coldstore.Receipt{}, domain.Fail("SOURCE_CHANGED", "complete capture differs from the reviewed manifest digest")
	}
	return r, nil
}
