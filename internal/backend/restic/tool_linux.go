//go:build linux && amd64

package restic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)

func operationTag(id string) (string, error) {
	if !uuid.MatchString(id) || id == "00000000-0000-0000-0000-000000000000" {
		return "", domain.Fail("INVALID_INPUT", "canonical nonzero operation UUID required")
	}
	return "virmill-operation:" + id, nil
}
func validHash(id string) bool { return hash.MatchString(id) && id != strings.Repeat("0", 64) }
func badOutput() error {
	return domain.Fail("OPERATION_FAILED", "restic returned invalid or unbound structured output; effects require observation")
}

type repositoryPins struct{ repo, password *pinned }

func (p *repositoryPins) close() {
	if p != nil {
		p.repo.close()
		p.password.close()
	}
}
func prepare(ctx context.Context, r Repository, create bool) (*repositoryPins, error) {
	if !localPath(r.Path) || !localPath(r.PasswordFile) || within(r.PasswordFile, r.Path) {
		return nil, domain.Fail("INVALID_INPUT", "local repository and external private credential paths required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	password, err := openPrivate(r.PasswordFile, false, false)
	if err != nil {
		return nil, err
	}
	parent, err := openPrivate(filepath.Dir(r.PasswordFile), true, true)
	if err != nil {
		password.close()
		return nil, err
	}
	password.parent = parent
	var repo *pinned
	if create {
		repo, err = makePrivate(r.Path)
	} else {
		repo, err = openPrivate(r.Path, true, true)
	}
	if err != nil {
		password.close()
		return nil, err
	}
	p := &repositoryPins{repo: repo, password: password}
	if _, err = scan(ctx, repo); err != nil {
		p.close()
		return nil, err
	}
	return p, nil
}
func (p *repositoryPins) invoke(ctx context.Context, t Tool, label string, args []string, extra *pinned, working bool) ([]byte, error) {
	if err := p.repo.recheck(false); err != nil {
		return nil, err
	}
	if err := p.password.recheck(true); err != nil {
		return nil, err
	}
	in := invocation{label: label, args: append([]string{"--json", "--quiet", "--no-cache", "--repo", "/proc/self/fd/4", "--password-file", "/proc/self/fd/5"}, args...), files: []*os.File{p.repo.f, p.password.f}}
	if extra != nil {
		if err := extra.recheck(false); err != nil {
			return nil, err
		}
		in.files = append(in.files, extra.f)
		if working {
			in.dir = fmt.Sprintf("/proc/self/fd/%d", extra.f.Fd())
		}
	}
	b, err := t.invoke(ctx, in)
	if err != nil {
		return nil, err
	}
	if err = p.repo.recheck(false); err != nil {
		return nil, err
	}
	if err = p.password.recheck(true); err != nil {
		return nil, err
	}
	if extra != nil {
		if err = extra.recheck(false); err != nil {
			return nil, err
		}
	}
	if len(b) > maxOutput {
		return nil, badOutput()
	}
	return b, nil
}

// A map allows forward-compatible extra restic statistics, but known field
// names are exact and duplicate keys are rejected before decoding values.
func object(raw []byte) (map[string]json.RawMessage, error) {
	if err := wire.Validate(raw); err != nil {
		return nil, badOutput()
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil, badOutput()
	}
	return m, nil
}
func field(m map[string]json.RawMessage, key string, to any, required bool) error {
	for k := range m {
		if strings.EqualFold(k, key) && k != key {
			return badOutput()
		}
	}
	raw, ok := m[key]
	if !ok {
		if required {
			return badOutput()
		}
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, to) != nil {
		return badOutput()
	}
	return nil
}
func summary(raw []byte, kind string) (map[string]json.RawMessage, error) {
	lines := bytes.Split(bytes.TrimSpace(raw), []byte{'\n'})
	if len(lines) == 0 || len(lines) > 4096 {
		return nil, badOutput()
	}
	var result map[string]json.RawMessage
	for i, line := range lines {
		if len(line) > 1<<20 {
			return nil, badOutput()
		}
		m, err := object(line)
		if err != nil {
			return nil, err
		}
		var typ string
		if field(m, "message_type", &typ, true) != nil {
			return nil, badOutput()
		}
		if typ == kind {
			if result != nil || i != len(lines)-1 {
				return nil, badOutput()
			}
			result = m
			continue
		}
		if typ != "status" {
			return nil, badOutput()
		}
		var errors uint64
		if field(m, "error_count", &errors, false) != nil || errors != 0 {
			return nil, badOutput()
		}
	}
	if result == nil {
		return nil, badOutput()
	}
	return result, nil
}
func snapshots(raw []byte, tag string) ([]Snapshot, error) {
	if len(raw) > maxOutput || wire.Validate(raw) != nil {
		return nil, badOutput()
	}
	var records []json.RawMessage
	if json.Unmarshal(raw, &records) != nil || records == nil || len(records) > 4096 {
		return nil, badOutput()
	}
	out := make([]Snapshot, 0, len(records))
	seen := map[string]bool{}
	for _, raw := range records {
		m, err := object(raw)
		if err != nil {
			return nil, err
		}
		var s Snapshot
		if field(m, "id", &s.ID, true) != nil || field(m, "tags", &s.Tags, true) != nil || field(m, "paths", &s.Paths, true) != nil || field(m, "time", &s.Time, true) != nil {
			return nil, badOutput()
		}
		if !validHash(s.ID) || seen[s.ID] || s.Time.IsZero() || len(s.Tags) != 1 || len(s.Paths) != 1 || !localPath(s.Paths[0]) {
			return nil, badOutput()
		}
		owned, e := operationTag(strings.TrimPrefix(s.Tags[0], "virmill-operation:"))
		if e != nil || s.Tags[0] != owned || (tag != "" && s.Tags[0] != tag) {
			return nil, badOutput()
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (t Tool) observe(ctx context.Context, p *repositoryPins, tag string) ([]Snapshot, error) {
	b, err := p.invoke(ctx, t, "snapshots", []string{"snapshots", "--tag", tag}, nil, false)
	if err != nil {
		return nil, err
	}
	return snapshots(b, tag)
}

func (t Tool) Init(ctx context.Context, r Repository) error {
	p, err := prepare(ctx, r, true)
	if err != nil {
		return err
	}
	defer p.close()
	b, err := p.invoke(ctx, t, "init", []string{"init", "--repository-version", "2"}, nil, false)
	if err != nil {
		return err
	}
	m, err := summary(b, "initialized")
	if err != nil {
		return err
	}
	var id string
	if field(m, "id", &id, true) != nil || !validHash(id) {
		return badOutput()
	}
	_, err = scan(ctx, p.repo)
	return err
}

func (t Tool) Observe(ctx context.Context, r Repository, operationID string) ([]Snapshot, error) {
	tag, err := operationTag(operationID)
	if err != nil {
		return nil, err
	}
	p, err := prepare(ctx, r, false)
	if err != nil {
		return nil, err
	}
	defer p.close()
	return t.observe(ctx, p, tag)
}

func (t Tool) Backup(ctx context.Context, r Repository, sourceDirectory, operationID string) (Snapshot, error) {
	var empty Snapshot
	tag, err := operationTag(operationID)
	if err != nil {
		return empty, err
	}
	if !localPath(sourceDirectory) || !disjoint(sourceDirectory, r.Path) || within(r.PasswordFile, sourceDirectory) {
		return empty, domain.Fail("INVALID_INPUT", "backup source must be distinct from repository and credential")
	}
	source, err := openPrivate(sourceDirectory, true, false)
	if err != nil {
		return empty, err
	}
	defer source.close()
	before, err := scan(ctx, source)
	if err != nil {
		return empty, err
	}
	if len(before) == 0 {
		return empty, domain.Fail("INVALID_INPUT", "backup source must contain a captured set")
	}
	p, err := prepare(ctx, r, false)
	if err != nil {
		return empty, err
	}
	defer p.close()
	previous, err := t.observe(ctx, p, tag)
	if err != nil {
		return empty, err
	}
	if len(previous) != 0 {
		return empty, domain.Fail("RECOVERY_REQUIRED", "operation already has repository snapshots; observe instead of replaying backup")
	}
	b, err := p.invoke(ctx, t, "backup", []string{"backup", "--force", "--tag", tag, "--", "."}, source, true)
	if err != nil {
		return empty, err
	}
	m, err := summary(b, "summary")
	if err != nil {
		return empty, err
	}
	var id string
	var dry bool
	if field(m, "snapshot_id", &id, true) != nil || !validHash(id) || field(m, "dry_run", &dry, false) != nil || dry {
		return empty, badOutput()
	}
	after, err := scan(ctx, source)
	if err != nil {
		return empty, err
	}
	if !sameTree(before, after) {
		return empty, domain.Fail("SOURCE_CHANGED", "backup source tree changed during repository write")
	}
	if err = source.recheck(true); err != nil {
		return empty, err
	}
	observed, err := t.observe(ctx, p, tag)
	if err != nil {
		return empty, err
	}
	if len(observed) != 1 || observed[0].ID != id || observed[0].Paths[0] != sourceDirectory {
		return empty, domain.Fail("RECOVERY_REQUIRED", "backup result does not identify exactly one matching operation and source snapshot")
	}
	return observed[0], nil
}

func (t Tool) Restore(ctx context.Context, r Repository, snapshotID, destination string) error {
	if !validHash(snapshotID) || !localPath(destination) || !disjoint(destination, r.Path) || within(r.PasswordFile, destination) {
		return domain.Fail("INVALID_INPUT", "full snapshot hash and new distinct local restore destination required")
	}
	p, err := prepare(ctx, r, false)
	if err != nil {
		return err
	}
	defer p.close()
	b, err := p.invoke(ctx, t, "snapshots", []string{"snapshots", "--", snapshotID}, nil, false)
	if err != nil {
		return err
	}
	list, err := snapshots(b, "")
	if err != nil {
		return err
	}
	if len(list) != 1 || list[0].ID != snapshotID {
		return domain.Fail("NOT_FOUND", "exact Virmill repository snapshot unavailable")
	}
	if !disjoint(destination, list[0].Paths[0]) {
		return domain.Fail("INVALID_INPUT", "restore destination must not overlap the original source")
	}
	target, err := makePrivate(destination)
	if err != nil {
		return err
	}
	defer target.close()
	// Restic deliberately refuses a symlink as the extraction root, including
	// /proc/self/fd/N. Use the new canonical private directory, retaining its
	// inode and parent pins before/after the call. Other processes with this
	// same UID remain inside the caller's trust boundary.
	b, err = p.invoke(ctx, t, "restore", []string{"restore", "--target", destination, "--overwrite", "never", "--verify", "--", snapshotID}, target, false)
	if err != nil {
		return err
	}
	m, err := summary(b, "summary")
	if err != nil {
		return err
	}
	for _, key := range []string{"files_skipped", "bytes_skipped", "files_deleted"} {
		var n uint64
		if field(m, key, &n, false) != nil || n != 0 {
			return badOutput()
		}
	}
	_, err = scan(ctx, target)
	return err
}

func (t Tool) Check(ctx context.Context, r Repository) error {
	p, err := prepare(ctx, r, false)
	if err != nil {
		return err
	}
	defer p.close()
	b, err := p.invoke(ctx, t, "check", []string{"check", "--read-data"}, nil, false)
	if err != nil {
		return err
	}
	m, err := summary(b, "summary")
	if err != nil {
		return err
	}
	var n uint64
	var broken []string
	var repair bool
	// Restic 0.19.1 emits a null slice for an empty broken_packs result.
	// Normalize only this documented nullable list; counts must still be present.
	if bytes.Equal(bytes.TrimSpace(m["broken_packs"]), []byte("null")) {
		m["broken_packs"] = json.RawMessage("[]")
	}
	if field(m, "num_errors", &n, true) != nil || n != 0 || field(m, "broken_packs", &broken, false) != nil || len(broken) != 0 || field(m, "suggest_repair_index", &repair, false) != nil || repair {
		return domain.Fail("INCOMPLETE_BACKUP", "restic repository check reported damaged or missing data")
	}
	return nil
}
