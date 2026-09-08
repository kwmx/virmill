package plugins

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/buildinfo"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

type Actions struct {
	Manager  *Manager
	Provider domain.ComputeProvider
	Cache    string
}
type selectedVM struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}
type invocation struct {
	PluginID string           `json:"pluginID"`
	Version  InstalledVersion `json:"version"`
	Action   string           `json:"action"`
	Input    map[string]any   `json:"input"`
	Context  struct {
		SelectedVMs []selectedVM `json:"selectedVMs"`
	} `json:"context"`
	PlanToken      json.RawMessage   `json:"planToken"`
	Description    actionDescription `json:"description"`
	GrantExpiresAt time.Time         `json:"grantExpiresAt"`
}
type actionDescription struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	ReadOnly     bool            `json:"readOnly"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
}

func (a *Actions) Review(ctx context.Context, p domain.Plan, b []byte) (map[string]any, error) {
	var in invocation
	if err := wire.Decode(b, &in); err != nil {
		return nil, err
	}
	return map[string]any{"pluginID": in.PluginID, "version": in.Version.Version, "packageDigest": in.Version.Digest, "action": in.Action, "parameters": in.Input, "selectedVMs": in.Context.SelectedVMs, "effectivePermissions": Effective(in.Version.Manifest.Permissions, in.Version.Grants, in.Version.Grants), "grantExpiresAt": in.GrantExpiresAt, "externalEffects": []string{}}, nil
}

// Every run takes its executable from a fresh verified copy. Same-user changes to
// installation directories cannot race the hash check and the executable bind.
func (a *Actions) session(ctx context.Context, v InstalledVersion) (*Session, func(), error) {
	if err := enabledSupport(v); err != nil {
		return nil, nil, err
	}
	verified, _, err := verifiedSource(filepath.Join(a.Manager.versionDir(v.Digest), ".virmill-package.tar"), v.KeyID, v.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	if verified.Digest != v.Digest {
		return nil, nil, domain.Fail("SOURCE_CHANGED", "pinned package changed")
	}
	stage, err := verified.Extract(a.Cache)
	if err != nil {
		return nil, nil, err
	}
	workspace, err := os.MkdirTemp(a.Cache, "virmill-invocation-")
	if err != nil {
		os.RemoveAll(stage)
		return nil, nil, err
	}
	cleanup := func() { os.RemoveAll(workspace); os.RemoveAll(stage) }
	entry := v.Manifest.Entrypoints["linux/amd64"]
	s, err := startPackage(ctx, stage, entry.Path, workspace)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return s, func() { s.Close(); cleanup() }, nil
}

func limitedCall(ctx context.Context, s *Session, method string, p any, limit time.Duration) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	return s.Call(ctx, method, p)
}

func negotiate(ctx context.Context, s *Session, v InstalledVersion) ([]actionDescription, error) {
	data, err := limitedCall(ctx, s, "initialize", map[string]any{"protocolVersions": []string{"1.0"}, "host": map[string]string{"version": buildinfo.Version, "os": "linux", "arch": "amd64"}, "sessionID": domain.ID(), "permissions": Effective(v.Manifest.Permissions, v.Grants, v.Grants), "limits": map[string]int{"maxMessageBytes": wire.MaxFrame}}, 10*time.Second)
	if err != nil {
		return nil, err
	}
	var response struct {
		ProtocolVersion string `json:"protocolVersion"`
		Plugin          struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"plugin"`
		ExtensionTypes []string `json:"extensionTypes"`
	}
	if err = wire.Decode(data, &response); err != nil {
		return nil, err
	}
	if response.ProtocolVersion != "1.0" || response.Plugin.ID != v.Manifest.ID || response.Plugin.Version != v.Version || !jsonEqual(response.ExtensionTypes, v.Manifest.ExtensionTypes) {
		return nil, domain.Fail("PERMISSION_DENIED", "plugin negotiated a different identity, version or extension type")
	}
	data, err = limitedCall(ctx, s, "describe", map[string]any{}, 5*time.Second)
	if err != nil {
		return nil, err
	}
	var description struct {
		Actions []actionDescription `json:"actions"`
	}
	if err = wire.Decode(data, &description); err != nil {
		return nil, err
	}
	if len(description.Actions) > 64 {
		return nil, domain.Fail("INVALID_INPUT", "plugin action count limit")
	}
	seen := map[string]bool{}
	for _, d := range description.Actions {
		if d.ID == "" || len(d.ID) > 128 || seen[d.ID] || validation.SafeText(d.ID) != d.ID || len(d.InputSchema) == 0 || len(d.OutputSchema) == 0 {
			return nil, domain.Fail("INVALID_INPUT", "invalid or duplicate action description")
		}
		seen[d.ID] = true
	}
	return description.Actions, nil
}

func (a *Actions) Plan(ctx context.Context, uid uint32, r app.Request) (domain.Plan, error) {
	var empty domain.Plan
	if err := checkedInput(r.Input, "action", "parameters", "vmIDs"); err != nil {
		return empty, err
	}
	installation, err := a.Manager.Show(r.ID)
	if err != nil {
		return empty, err
	}
	if !installation.Enabled || installation.Removed {
		return empty, domain.Fail("PERMISSION_DENIED", "plugin is disabled or removed")
	}
	var in invocation
	in.PluginID, in.Version = r.ID, installation.Versions[installation.Active]
	in.Action, _ = r.Input["action"].(string)
	in.Input, _ = r.Input["parameters"].(map[string]any)
	if in.Input == nil {
		in.Input = map[string]any{}
	}
	idsRaw, err := json.Marshal(r.Input["vmIDs"])
	if err != nil {
		return empty, err
	}
	var ids []string
	if err = wire.Decode(idsRaw, &ids); err != nil {
		return empty, err
	}
	if len(ids) > 256 {
		return empty, domain.Fail("INVALID_INPUT", "select at most 256 VMs per action invocation")
	}
	sort.Strings(ids)
	before := map[string]string{}
	resources := []string{"plugin-version:local:" + r.ID + ":" + installation.Active}
	in.Context.SelectedVMs = []selectedVM{}
	if len(ids) > 0 && !subset([]Permission{{"vm.read", "selection"}}, in.Version.Grants) {
		return empty, domain.Fail("PERMISSION_DENIED", "selected VM read scope is not granted")
	}
	for i, id := range ids {
		if i > 0 && ids[i-1] == id {
			return empty, domain.Fail("INVALID_INPUT", "duplicate selected VM ID")
		}
		v, err := a.Provider.Get(ctx, r.Connection, id)
		if err != nil {
			return empty, err
		}
		in.Context.SelectedVMs = append(in.Context.SelectedVMs, selectedVM{ID: id, Name: v.Name, State: v.State})
		before[v.Key.String()] = v.Fingerprint
		resources = append(resources, v.Key.String())
	}
	s, close, err := a.session(ctx, in.Version)
	if err != nil {
		return empty, err
	}
	defer close()
	descriptions, err := negotiate(ctx, s, in.Version)
	if err != nil {
		return empty, err
	}
	found := false
	for _, d := range descriptions {
		if d.ID == in.Action {
			in.Description = d
			found = true
		}
	}
	if !found {
		return empty, domain.Fail("INVALID_INPUT", "action is not advertised by this installed plugin")
	}
	if !in.Description.ReadOnly {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "installed invocation currently supports read-only confined actions; typed mutation mediation is not yet implemented")
	}
	if err = validation.DynamicValue(in.Description.InputSchema, in.Input); err != nil {
		return empty, domain.Fail("INVALID_INPUT", err.Error())
	}
	params := map[string]any{"action": in.Action, "input": in.Input, "context": in.Context}
	data, err := limitedCall(ctx, s, "action.plan", params, 30*time.Second)
	if err != nil {
		return empty, err
	}
	var planned struct {
		Summary   string            `json:"summary"`
		Effects   []json.RawMessage `json:"effects"`
		Requires  []Permission      `json:"requires"`
		PlanToken json.RawMessage   `json:"planToken"`
	}
	if err = wire.Decode(data, &planned); err != nil {
		return empty, err
	}
	if planned.Effects == nil || len(planned.Effects) != 0 || !subset(planned.Requires, in.Version.Grants) {
		return empty, domain.Fail("PERMISSION_DENIED", "action declared unapproved effects or scopes")
	}
	if len(planned.PlanToken) == 0 || len(planned.PlanToken) > 64<<10 {
		return empty, domain.Fail("INVALID_INPUT", "bounded opaque plan token required")
	}
	in.PlanToken = planned.PlanToken
	in.GrantExpiresAt = time.Now().UTC().Add(15 * time.Minute)
	if _, err = limitedCall(ctx, s, "shutdown", map[string]any{}, 5*time.Second); err != nil {
		return empty, err
	}
	key, _ := hex.DecodeString(in.Version.PublicKey)
	if len(key) != ed25519.PublicKeySize {
		return empty, errors.New("invalid pinned public key")
	}
	step := domain.Step{ID: "invoke", Action: "plugin.call", Preconditions: []string{"pinned package digest, active installation and current grants", "unchanged selected VM fingerprints", "matching input and action description"}, Idempotency: "reconcile-before-retry", Compensation: "Private invocation workspace is discarded; an uncertain result is never silently replayed", Reconciliation: "Read the durable result bound to this core plan and package digest", CompletionPredicate: "Schema-valid result is durable for the exact approved invocation"}
	return a.Manager.Engine.Plan(ctx, uid, r.Connection, "plugin.call", resources, before, in, []domain.Step{step}, []string{"invoke-plugin:" + r.ID}, []string{"Executes an external program under confinement; no network, helper access, secrets or persistent data mounts are granted", validation.SafeText(planned.Summary), "Package SHA-256: " + in.Version.Digest, "Signing-key SHA-256: " + hashBytes(key)})
}

func (a *Actions) Validate(ctx context.Context, p domain.Plan, b []byte) error {
	var in invocation
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	installation, err := a.Manager.Show(in.PluginID)
	if err != nil {
		return err
	}
	if installation.Removed || !installation.Enabled || installation.Active != in.Version.Digest {
		return domain.Fail("STALE_PLAN", "plugin activation changed; create a fresh invocation plan")
	}
	if !time.Now().Before(in.GrantExpiresAt) {
		return domain.Fail("PERMISSION_REQUIRED", "invocation grant expired")
	}
	current := installation.Versions[installation.Active]
	if !jsonEqual(current, in.Version) {
		return domain.Fail("STALE_PLAN", "plugin permissions or identity changed")
	}
	if _, err = a.Manager.verifyInstalled(current); err != nil {
		return err
	}
	for _, expected := range in.Context.SelectedVMs {
		v, err := a.Provider.Get(ctx, p.ConnectionID, expected.ID)
		if err != nil {
			return err
		}
		if p.Before[v.Key.String()] != v.Fingerprint || v.Name != expected.Name || v.State != expected.State {
			return domain.Fail("STALE_PLAN", "selected VM facts changed since preview")
		}
	}
	return enabledSupport(current)
}

type actionResult struct {
	SchemaVersion int             `json:"schemaVersion"`
	PlanID        string          `json:"planID"`
	OperationID   string          `json:"operationID"`
	PackageDigest string          `json:"packageDigest"`
	InputDigest   string          `json:"inputDigest"`
	Result        json.RawMessage `json:"result"`
}

func (a *Actions) Execute(ctx context.Context, p domain.Plan, b []byte, _ domain.Step) error {
	if err := a.Validate(ctx, p, b); err != nil {
		return err
	}
	var in invocation
	if err := wire.Decode(b, &in); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	s, close, err := a.session(ctx, in.Version)
	if err != nil {
		return err
	}
	defer close()
	description, err := negotiate(ctx, s, in.Version)
	if err != nil {
		return err
	}
	found := false
	for _, d := range description {
		if jsonEqual(d, in.Description) {
			found = true
		}
	}
	if !found {
		return domain.Fail("STALE_PLAN", "action schema or effects changed")
	}
	operationID := operations.OperationID(ctx)
	if operationID == "" {
		return errors.New("durable core operation context required")
	}
	grants := []string{}
	if len(in.Context.SelectedVMs) > 0 && subset([]Permission{{"vm.read", "selection"}}, in.Version.Grants) {
		grants = append(grants, p.ID+":vm.read:selection")
	}
	result, err := s.Call(ctx, "action.execute", map[string]any{"action": in.Action, "input": in.Input, "context": in.Context, "planToken": in.PlanToken, "operationID": operationID, "grants": grants})
	if err != nil {
		return err
	}
	if err = validation.Dynamic(in.Description.OutputSchema, result); err != nil {
		return domain.Fail("INVALID_INPUT", "plugin result violates output schema: "+err.Error())
	}
	if err = a.Manager.Store.ComparePut("plugin-action-result", p.ID, nil, actionResult{SchemaVersion: 1, PlanID: p.ID, OperationID: operationID, PackageDigest: in.Version.Digest, InputDigest: p.InputDigest, Result: result}); err != nil {
		return err
	}
	_, err = limitedCall(ctx, s, "shutdown", map[string]any{}, 5*time.Second)
	return err
}

func (a *Actions) Reconcile(ctx context.Context, p domain.Plan, b []byte, _ domain.Step) (bool, error) {
	var in invocation
	if err := wire.Decode(b, &in); err != nil {
		return false, err
	}
	var result actionResult
	stored, err := a.Manager.Store.MetadataBytes("plugin-action-result", p.ID)
	if err != nil || stored == nil {
		return false, err
	}
	if err = wire.Decode(stored, &result); err != nil {
		return false, err
	}
	if result.SchemaVersion != 1 || result.PlanID != p.ID || result.PackageDigest != in.Version.Digest || result.InputDigest != p.InputDigest {
		return false, domain.Fail("RECOVERY_REQUIRED", "stored plugin result binding differs")
	}
	if err = validation.Dynamic(in.Description.OutputSchema, result.Result); err != nil {
		return false, err
	}
	return true, nil
}

func (a *Actions) Result(operationID string) (any, error) {
	j, err := a.Manager.Store.Job(operationID)
	if err != nil {
		return nil, err
	}
	var result actionResult
	if err = a.Manager.Store.Get("plugin-action-result", j.PlanID, &result); err != nil {
		return nil, domain.Fail("RECOVERY_REQUIRED", "no durable plugin result is available for this operation")
	}
	if result.OperationID != operationID {
		return nil, domain.Fail("RECOVERY_REQUIRED", "result belongs to another operation")
	}
	return result, nil
}
