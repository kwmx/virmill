package plugins

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/wire"
)

const installationVersion = 1

func jsonEqual(a, b any) bool {
	x, err := operations.Canonical(a)
	if err != nil {
		return false
	}
	y, err := operations.Canonical(b)
	return err == nil && bytes.Equal(x, y)
}

type InstalledVersion struct {
	Version   string       `json:"version"`
	Digest    string       `json:"packageDigest"`
	KeyID     string       `json:"signingKeyID"`
	PublicKey string       `json:"publicKey"`
	Manifest  Manifest     `json:"manifest"`
	Grants    []Permission `json:"grants"`
}

// Each invocation is bound to one immutable version. Lifecycle edits change the
// active reference only; retained bytes are never rewritten by an update.
type Installation struct {
	SchemaVersion int                         `json:"schemaVersion"`
	ID            string                      `json:"id"`
	Active        string                      `json:"activeDigest"`
	Previous      string                      `json:"previousDigest,omitempty"`
	Enabled       bool                        `json:"enabled"`
	Removed       bool                        `json:"removed"`
	Versions      map[string]InstalledVersion `json:"versions"`
	LastPlanID    string                      `json:"lastPlanID"`
}

type Manager struct {
	Store  *store.Store
	Engine *operations.Engine
	Root   string
}

type lifecycleInput struct {
	ID           string       `json:"id"`
	Action       string       `json:"action"`
	Source       string       `json:"source,omitempty"`
	SourceDigest string       `json:"sourceDigest,omitempty"`
	Before       string       `json:"before"`
	After        Installation `json:"after"`
	FinishJobs   []string     `json:"finishJobs"`
}

func (m *Manager) record(id string) (Installation, []byte, error) {
	var r Installation
	if !validPluginID(id) {
		return r, nil, domain.Fail("INVALID_INPUT", "valid reverse-domain plugin ID required")
	}
	b, err := m.Store.MetadataBytes("plugin-installation", id)
	if err != nil || b == nil {
		return r, b, err
	}
	if err = wire.Decode(b, &r); err != nil {
		return r, nil, err
	}
	if r.SchemaVersion != installationVersion {
		return r, nil, domain.Fail("UNSUPPORTED_CAPABILITY", "plugin installation schema requires a compatible coordinator")
	}
	return r, b, nil
}

func validPluginID(id string) bool {
	// The authoritative pattern comes from the bundled manifest schema.
	probe := []byte(fmt.Sprintf(`{"manifestVersion":"1","id":%q,"name":"Identity validation","version":"0.1.0","protocol":{"minVersion":"1.0","maxVersion":"1.0","transport":"stdio-jsonrpc"},"entrypoints":{"linux/amd64":{"path":"entry"}},"extensionTypes":["action"],"permissions":[],"network":"none"}`, id))
	_, err := ValidateManifest(probe)
	return err == nil
}

func (m *Manager) List() ([]Installation, error) {
	rows, err := m.Store.Metadata("plugin-installation")
	if err != nil {
		return nil, err
	}
	out := []Installation{}
	for _, b := range rows {
		var r Installation
		if err = wire.Decode(b, &r); err != nil {
			return nil, err
		}
		if r.SchemaVersion != installationVersion {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "plugin installation schema requires a compatible coordinator")
		}
		if !r.Removed {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *Manager) Show(id string) (Installation, error) {
	r, b, err := m.record(id)
	if err == nil && b == nil {
		err = domain.Fail("INVALID_INPUT", "plugin is not installed")
	}
	return r, err
}

func readRegular(path string, limit int64) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > limit {
		return nil, errors.New("bounded ordinary file required; symlinks are not accepted")
	}
	f, err := openReadNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(st, opened) {
		return nil, domain.Fail("SOURCE_CHANGED", "file changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("file exceeds bound")
	}
	return b, nil
}

func verifiedSource(path, keyID, keyHex string) (Verified, []byte, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return Verified{}, nil, domain.Fail("INVALID_INPUT", "32-byte hexadecimal Ed25519 public key required")
	}
	b, err := readRegular(path, PackageLimit+(8<<20))
	if err != nil {
		return Verified{}, nil, err
	}
	v, err := Verify(bytes.NewReader(b), map[string]ed25519.PublicKey{keyID: key})
	return v, b, err
}

func checkedInput(input map[string]any, allowed ...string) error {
	for k := range input {
		found := false
		for _, a := range allowed {
			if a == k {
				found = true
			}
		}
		if !found {
			return domain.Fail("INVALID_INPUT", "unknown input field: "+k)
		}
	}
	return nil
}

func permissionsFrom(input any) ([]Permission, error) {
	b, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var p []Permission
	if err = wire.Decode(b, &p); err != nil {
		return nil, err
	}
	if p == nil {
		p = []Permission{}
	}
	seen := map[Permission]bool{}
	for _, grant := range p {
		if grant.Name == "" || grant.Scope == "" || seen[grant] {
			return nil, domain.Fail("INVALID_INPUT", "permissions must be distinct named exact scopes")
		}
		seen[grant] = true
	}
	sort.Slice(p, func(i, j int) bool {
		if p[i].Name == p[j].Name {
			return p[i].Scope < p[j].Scope
		}
		return p[i].Name < p[j].Name
	})
	return p, nil
}

func subset(requested, declared []Permission) bool {
	allowed := map[Permission]bool{}
	for _, p := range declared {
		allowed[p] = true
	}
	for _, p := range requested {
		if !allowed[p] {
			return false
		}
	}
	return true
}

func (m *Manager) Plan(ctx context.Context, uid uint32, action string, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	var in lifecycleInput
	var fresh *InstalledVersion
	acks := []string{"plugin-installation-change"}
	risks := []string{"Changes this user's plugin installation; plugin-created VMs, backups and external resources are preserved"}
	if action == "install" || action == "update" {
		if err := checkedInput(r.Input, "keyID", "publicKey", "permissions", "activeJobs"); err != nil {
			return empty, err
		}
		keyID, _ := r.Input["keyID"].(string)
		keyHex, _ := r.Input["publicKey"].(string)
		source, err := filepath.Abs(r.Path)
		if err != nil {
			return empty, err
		}
		v, raw, err := verifiedSource(source, keyID, keyHex)
		if err != nil {
			return empty, err
		}
		grants, err := permissionsFrom(r.Input["permissions"])
		if err != nil {
			return empty, err
		}
		if !subset(grants, v.Manifest.Permissions) {
			return empty, domain.Fail("PERMISSION_DENIED", "cannot grant permissions absent from manifest")
		}
		fresh = &InstalledVersion{Version: v.Manifest.Version, Digest: v.Digest, KeyID: keyID, PublicKey: strings.ToLower(keyHex), Manifest: v.Manifest, Grants: grants}
		in.ID, in.Source, in.SourceDigest = v.Manifest.ID, source, hashBytes(raw)
		key, _ := hex.DecodeString(keyHex)
		acks = append(acks, "trust-key-sha256:"+hashBytes(key))
		risks = append(risks, "Author identity must be reviewed independently of a valid signature; trust is pinned to this package, not a global key grant", "New installations and updates start disabled; enabling is a separate reviewed operation")
	} else {
		if err := checkedInput(r.Input, "permissions", "activeJobs"); err != nil {
			return empty, err
		}
		in.ID = r.ID
	}
	old, before, err := m.record(in.ID)
	if err != nil {
		return empty, err
	}
	in.FinishJobs, err = m.activeJobs(old)
	if err != nil {
		return empty, err
	}
	if len(in.FinishJobs) > 0 {
		if r.Input["activeJobs"] != "finish" {
			return empty, domain.Fail("PERMISSION_REQUIRED", "active or uncertain invocations exist; review activeJobs:finish or safely resolve them through Jobs before changing the installation")
		}
		acks = append(acks, "allow-active-jobs-to-finish")
		risks = append(risks, "Existing invocations remain pinned to their reviewed version until they finish or are reconciled: "+strings.Join(in.FinishJobs, ", "))
	}
	in.Before, in.Action = string(before), action
	if action == "install" {
		if before != nil && !old.Removed {
			return empty, domain.Fail("STALE_PLAN", "plugin is already installed; use update")
		}
		if before == nil {
			old = Installation{SchemaVersion: installationVersion, ID: in.ID, Versions: map[string]InstalledVersion{}}
		}
	} else if before == nil || old.Removed {
		return empty, domain.Fail("INVALID_INPUT", "active plugin installation required")
	}
	if fresh != nil {
		for _, prior := range old.Versions {
			if prior.Version == fresh.Version && prior.Digest != fresh.Digest {
				return empty, domain.Fail("SOURCE_CHANGED", "an installed semantic version cannot be replaced with different bytes")
			}
		}
		if fresh.Digest == old.Active && !old.Removed {
			return empty, domain.Fail("INVALID_INPUT", "this exact package is already active")
		}
		old.Previous, old.Active = old.Active, fresh.Digest
		old.Versions[fresh.Digest] = *fresh
		old.Enabled, old.Removed = false, false
		for _, p := range fresh.Grants {
			acks = append(acks, "grant:"+p.Name+":"+p.Scope)
		}
	}
	switch action {
	case "install", "update":
	case "enable":
		active := old.Versions[old.Active]
		if err = enabledSupport(active); err != nil {
			return empty, err
		}
		old.Enabled = true
	case "disable":
		old.Enabled = false
	case "remove":
		old.Enabled, old.Removed = false, true
		risks = append(risks, "Verified package versions and configuration are retained for recovery; this action does not delete plugin-owned data")
	case "rollback":
		if old.Previous == "" {
			return empty, domain.Fail("INVALID_INPUT", "no retained previous version")
		}
		old.Active, old.Previous = old.Previous, old.Active
		old.Enabled = false
		risks = append(risks, "Rollback changes the executable reference only; shared plugin-data migrations are not supported and are never run")
	case "grant", "revoke":
		permissions, err := permissionsFrom(r.Input["permissions"])
		if err != nil {
			return empty, err
		}
		if len(permissions) == 0 {
			return empty, domain.Fail("INVALID_INPUT", "explicit permissions required")
		}
		active := old.Versions[old.Active]
		if !subset(permissions, active.Manifest.Permissions) {
			return empty, domain.Fail("PERMISSION_DENIED", "cannot grant permissions absent from manifest")
		}
		set := map[Permission]bool{}
		for _, p := range active.Grants {
			set[p] = true
		}
		for _, p := range permissions {
			if action == "grant" {
				set[p] = true
				acks = append(acks, "grant:"+p.Name+":"+p.Scope)
			} else {
				delete(set, p)
			}
		}
		active.Grants = []Permission{}
		for p := range set {
			active.Grants = append(active.Grants, p)
		}
		active.Grants, _ = permissionsFrom(active.Grants)
		old.Versions[old.Active], old.Enabled = active, false
		risks = append(risks, "Grant changes disable new invocations until a separate enable plan checks available scopes")
	default:
		return empty, domain.Fail("INVALID_INPUT", "unknown plugin lifecycle action")
	}
	in.After = old
	step := domain.Step{ID: "activate", Action: "plugin." + action, Preconditions: []string{"unchanged installation record", "verified immutable package bytes and key", "exact reviewed permissions"}, Idempotency: "reconcile-before-retry", Compensation: "Preserve immutable package versions and create a fresh reviewed recovery plan", Reconciliation: "Observe the atomic installation record and retained signed bytes without launching a plugin", CompletionPredicate: "Installation matches this exact plan and verified package digest"}
	return m.Engine.Plan(ctx, uid, "local", "plugin."+action, []string{"plugin:local:" + in.ID}, map[string]string{"installation": hashBytes(before)}, in, []domain.Step{step}, acks, risks)
}

// This supervisor currently provides isolated action execution with no direct
// host API, networking or persistent plugin data. Unsupported extensions cannot
// be enabled merely because they were successfully unpacked.
func enabledSupport(v InstalledVersion) error {
	if v.Manifest.Protocol.MinVersion != "1.0" || v.Manifest.Protocol.MaxVersion != "1.0" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "only protocol 1.0 is implemented")
	}
	if _, ok := v.Manifest.Entrypoints["linux/amd64"]; !ok {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "no Linux/amd64 entrypoint")
	}
	if len(v.Manifest.ExtensionTypes) != 1 || v.Manifest.ExtensionTypes[0] != "action" || v.Manifest.Network != "none" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "installed runtime currently enables confined action plugins with network:none only")
	}
	for _, p := range v.Manifest.Permissions {
		if p.Name != "vm.read" || p.Scope != "selection" {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "this installed runtime does not implement permission "+p.Name+":"+p.Scope)
		}
	}
	if !subset(v.Manifest.Permissions, v.Grants) {
		return domain.Fail("PERMISSION_REQUIRED", "required manifest scopes are not granted")
	}
	return nil
}

func (m *Manager) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var in lifecycleInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	current, before, err := m.record(in.ID)
	if err != nil {
		return err
	}
	jobs, err := m.activeJobs(current)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		approved := false
		for _, id := range in.FinishJobs {
			if id == job {
				approved = true
			}
		}
		if !approved {
			return domain.Fail("STALE_PLAN", "a new plugin invocation started after lifecycle preview")
		}
	}
	expected := []byte(in.Before)
	if len(expected) == 0 {
		expected = nil
	}
	if !bytes.Equal(before, expected) {
		return domain.Fail("STALE_PLAN", "plugin installation changed after preview")
	}
	if p.Operation != "plugin."+in.Action || in.After.ID != in.ID {
		return domain.Fail("INVALID_INPUT", "plugin plan identity differs")
	}
	active, ok := in.After.Versions[in.After.Active]
	if !ok {
		return domain.Fail("INVALID_INPUT", "active package missing from plan")
	}
	if in.Source != "" {
		v, raw, err := verifiedSource(in.Source, active.KeyID, active.PublicKey)
		if err != nil {
			return err
		}
		if v.Digest != active.Digest || hashBytes(raw) != in.SourceDigest || !jsonEqual(v.Manifest, active.Manifest) {
			return domain.Fail("SOURCE_CHANGED", "package changed after preview")
		}
	} else if _, err = m.verifyInstalled(active); err != nil {
		return err
	}
	if in.After.Enabled {
		return enabledSupport(active)
	}
	return nil
}

func (m *Manager) versionDir(digest string) string { return filepath.Join(m.Root, digest) }

func (m *Manager) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var in lifecycleInput
	if err := wire.Decode(b, &in); err != nil {
		return nil, err
	}
	active := in.After.Versions[in.After.Active]
	return map[string]any{"pluginID": in.ID, "action": in.Action, "source": in.Source, "sourceSHA256": in.SourceDigest, "version": active.Version, "packageDigest": active.Digest, "signingKeyID": active.KeyID, "publicKey": active.PublicKey, "declaredPermissions": active.Manifest.Permissions, "grantedPermissions": active.Grants, "enabledAfter": in.After.Enabled, "removedAfter": in.After.Removed, "retainedVersionCount": len(in.After.Versions), "finishExistingJobs": in.FinishJobs}, nil
}

func (m *Manager) activeJobs(r Installation) ([]string, error) {
	jobs := []string{}
	for digest := range r.Versions {
		ids, err := m.Store.ResourceJobs("plugin-version:local:" + r.ID + ":" + digest)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, ids...)
	}
	sort.Strings(jobs)
	return jobs, nil
}

func (m *Manager) verifyInstalled(v InstalledVersion) (string, error) {
	if len(v.Digest) != 64 {
		return "", errors.New("invalid package digest")
	}
	if _, err := hex.DecodeString(v.Digest); err != nil {
		return "", err
	}
	dir := m.versionDir(v.Digest)
	verified, _, err := verifiedSource(filepath.Join(dir, ".virmill-package.tar"), v.KeyID, v.PublicKey)
	if err != nil {
		return "", err
	}
	if verified.Digest != v.Digest || !jsonEqual(verified.Manifest, v.Manifest) {
		return "", domain.Fail("SOURCE_CHANGED", "retained package differs from installation")
	}
	for _, f := range verified.Index.Files {
		path := filepath.Join(dir, f.Path)
		b, err := readRegular(path, PackageLimit)
		if err != nil {
			return "", err
		}
		if int64(len(b)) != f.Size || hashBytes(b) != f.SHA256 {
			return "", domain.Fail("SOURCE_CHANGED", "installed payload changed: "+f.Path)
		}
	}
	return dir, nil
}

func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (m *Manager) Execute(ctx context.Context, p domain.Plan, b []byte, _ domain.Step) error {
	// Repeat the check in this boundary as well: no stale cached verification grants
	// authority to write the active record.
	if err := m.Validate(ctx, p, b); err != nil {
		return err
	}
	var in lifecycleInput
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	active := in.After.Versions[in.After.Active]
	if in.Source != "" {
		v, raw, err := verifiedSource(in.Source, active.KeyID, active.PublicKey)
		if err != nil {
			return err
		}
		if hashBytes(raw) != in.SourceDigest || v.Digest != active.Digest {
			return domain.Fail("SOURCE_CHANGED", "package changed before staging")
		}
		destination := m.versionDir(v.Digest)
		if _, err = os.Lstat(destination); os.IsNotExist(err) {
			stage, err := v.Extract(m.Root)
			if err != nil {
				return err
			}
			defer os.RemoveAll(stage)
			if err = writeExclusive(filepath.Join(stage, ".virmill-package.tar"), raw, 0600); err != nil {
				return err
			}
			if err = syncTree(stage); err != nil {
				return err
			}
			if err = renameNew(stage, destination); err != nil {
				return err
			}
			if err = syncDir(m.Root); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	if _, err := m.verifyInstalled(active); err != nil {
		return err
	}
	in.After.LastPlanID = p.ID
	previous := []byte(in.Before)
	if len(previous) == 0 {
		previous = nil
	}
	reviewed := map[string][]string{}
	for digest := range in.After.Versions {
		reviewed["plugin-version:local:"+in.ID+":"+digest] = in.FinishJobs
	}
	return m.Store.ComparePutWithResourceJobs("plugin-installation", in.ID, previous, in.After, reviewed)
}

func (m *Manager) Reconcile(ctx context.Context, p domain.Plan, b []byte, _ domain.Step) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var in lifecycleInput
	if err := wire.Decode(b, &in); err != nil {
		return false, err
	}
	actual, _, err := m.record(in.ID)
	if err != nil {
		return false, err
	}
	in.After.LastPlanID = p.ID
	if !jsonEqual(actual, in.After) {
		return false, nil
	}
	_, err = m.verifyInstalled(actual.Versions[actual.Active])
	return err == nil, err
}
