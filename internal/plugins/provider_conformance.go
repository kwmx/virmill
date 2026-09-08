package plugins

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"virmill.local/core/internal/buildinfo"

	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const fixtureProviderID = "example.virmill.provider-fixture"
const fixtureProviderConnection = "fixture:///default"

type providerMethodSchema struct {
	Input  json.RawMessage `json:"inputSchema"`
	Output json.RawMessage `json:"outputSchema"`
}
type providerDescription struct {
	ProviderID   string                          `json:"providerID"`
	ConnectionID string                          `json:"connectionID"`
	Simulated    bool                            `json:"simulated"`
	Capabilities map[string]json.RawMessage      `json:"capabilities"`
	Methods      map[string]providerMethodSchema `json:"methods"`
}
type providerFixtureResource struct {
	ID  string `json:"id"`
	Key struct {
		ProviderID   string `json:"providerID"`
		ConnectionID string `json:"connectionID"`
		Kind         string `json:"kind"`
		ExternalID   string `json:"externalID"`
	} `json:"key"`
	Name         string                     `json:"name"`
	State        string                     `json:"state"`
	OperationID  string                     `json:"operationID"`
	Revision     uint64                     `json:"revision"`
	Capabilities map[string]json.RawMessage `json:"capabilities"`
}
type providerFixtureOutcome struct {
	providerFixtureResource
	Status     string `json:"status"`
	Reconciled bool   `json:"reconciled"`
	Simulated  bool   `json:"simulated"`
}
type providerFixturePage struct {
	Resources  []providerFixtureResource `json:"resources"`
	NextCursor *string                   `json:"nextCursor"`
	Generation uint64                    `json:"generation"`
	Simulated  bool                      `json:"simulated"`
}
type providerFixturePlan struct {
	Summary   string   `json:"summary"`
	Effects   []string `json:"effects"`
	Requires  []string `json:"requires"`
	Token     string   `json:"planToken"`
	State     string   `json:"resultState"`
	Simulated bool     `json:"simulated"`
}

func fixtureCapabilities() map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, name := range []string{"create", "start", "stop", "delete", "configurationEdits", "networks", "devices", "snapshots", "backups", "guestTransports", "cancel"} {
		out[name] = json.RawMessage("false")
		if name == "create" || name == "start" || name == "stop" || name == "delete" {
			out[name] = json.RawMessage("true")
		}
	}
	return out
}
func checkProviderCapabilities(caps map[string]json.RawMessage) error {
	want := fixtureCapabilities()
	if len(caps) != len(want) {
		return errors.New("reference provider capability set is incomplete or unsupported")
	}
	for name, value := range want {
		if !bytes.Equal(bytes.TrimSpace(caps[name]), value) {
			return fmt.Errorf("reference provider capability %s differs", name)
		}
	}
	return nil
}
func checkProviderResource(r providerFixtureResource) error {
	if len(r.ID) > 128 || !strings.HasPrefix(r.ID, "fixture-") || len(r.ID) <= len("fixture-") || r.Key.ProviderID != "fixture" || r.Key.ConnectionID != fixtureProviderConnection || r.Key.Kind != "vm" || r.Key.ExternalID != r.ID || r.Name == "" || len(r.Name) > 128 || r.OperationID == "" || len(r.OperationID) > 120 || r.Revision == 0 {
		return errors.New("provider resource identity/namespace/revision is incomplete")
	}
	for _, s := range []string{r.ID, r.OperationID} {
		for _, c := range s {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
				return errors.New("provider external/operation ID is not bounded ASCII")
			}
		}
	}
	switch r.State {
	case "defined", "running", "stopped", "deleted":
	default:
		return errors.New("unknown provider lifecycle state")
	}
	return checkProviderCapabilities(r.Capabilities)
}

func checkProviderManifest(m Manifest) error {
	if m.ManifestVersion != "1" || m.ID != fixtureProviderID || m.Version != "0.2.0" || m.Protocol.MinVersion != "1.0" || m.Protocol.MaxVersion != "1.0" || m.Protocol.Transport != "stdio-jsonrpc" || len(m.ExtensionTypes) != 1 || m.ExtensionTypes[0] != "provider" || m.Network != "none" || len(m.Permissions) != 0 {
		return errors.New("provider conformance supports only the permission-free, network-none simulated reference provider 0.2.0")
	}
	return nil
}

func providerSampleResource() providerFixtureResource {
	r := providerFixtureResource{ID: "fixture-schema", Name: "schema fixture", State: "defined", OperationID: "schema", Revision: 1, Capabilities: fixtureCapabilities()}
	r.Key.ProviderID, r.Key.ConnectionID, r.Key.Kind, r.Key.ExternalID = "fixture", fixtureProviderConnection, "vm", r.ID
	return r
}
func providerCapabilitySample() map[string]any {
	return map[string]any{"providerID": "fixture", "connectionID": fixtureProviderConnection, "simulated": true, "capabilities": fixtureCapabilities(), "create": true, "backup": false, "usb": false, "cancel": false}
}

func checkProviderCapabilityResult(raw json.RawMessage) error {
	var actual map[string]json.RawMessage
	if err := wire.Decode(raw, &actual); err != nil {
		return err
	}
	// JSON object ordering, including inside capabilities, is not protocol state.
	// Decode first so duplicate keys and malformed JSON cannot be normalized away.
	if !jsonEqual(actual, providerCapabilitySample()) {
		return errors.New("provider capabilities differ from the explicit simulated model")
	}
	return nil
}

func checkProviderDescription(raw []byte) (providerDescription, error) {
	var d providerDescription
	if len(raw) > 512<<10 {
		return d, errors.New("provider description exceeds bounds")
	}
	if err := wire.Decode(raw, &d); err != nil {
		return d, err
	}
	if d.ProviderID != "fixture" || d.ConnectionID != fixtureProviderConnection || !d.Simulated || len(d.Methods) != 8 {
		return d, errors.New("unsupported or incomplete simulated provider description")
	}
	if err := checkProviderCapabilities(d.Capabilities); err != nil {
		return d, err
	}
	r := providerSampleResource()
	for _, method := range []string{"capabilities", "inventory", "get", "plan", "apply", "status", "reconcile", "cancel"} {
		schema, ok := d.Methods["provider."+method]
		if !ok || len(schema.Input) == 0 || len(schema.Output) == 0 {
			return d, errors.New("provider method lacks declared input/output schema")
		}
		input := map[string]any{"connectionID": fixtureProviderConnection}
		var output any
		switch method {
		case "capabilities":
			output = providerCapabilitySample()
		case "inventory":
			input["limit"] = 1
			output = providerFixturePage{Resources: []providerFixtureResource{r}, Simulated: true}
		case "get":
			input["id"] = r.ID
			output = r
		case "plan":
			input["operation"], input["name"] = "create", "schema fixture"
			output = providerFixturePlan{"Simulate create", []string{"create one private JSON resource"}, []string{}, strings.Repeat("a", 64), "defined", true}
		case "apply":
			input["operation"], input["name"], input["operationID"], input["idempotencyKey"] = "create", "schema fixture", "schema", "schema-key"
			output = providerFixtureOutcome{r, "succeeded", false, true}
		case "status", "reconcile":
			input["operationID"] = "schema"
			output = providerFixtureOutcome{r, "partial", true, true}
		case "cancel":
			input["operationID"] = "schema"
		}
		if err := validation.DynamicValue(schema.Input, input); err != nil {
			return d, fmt.Errorf("%s input schema: %w", method, err)
		}
		invalid := map[string]any{"connectionID": fixtureProviderConnection, "unrecognized": true}
		if validation.DynamicValue(schema.Input, invalid) == nil {
			return d, fmt.Errorf("%s input schema accepts unknown parameters", method)
		}
		if method == "cancel" {
			if !bytes.Equal(bytes.TrimSpace(schema.Output), []byte("false")) || validation.DynamicValue(schema.Output, map[string]any{}) == nil {
				return d, errors.New("unsupported cancellation must declare no successful output")
			}
			continue
		}
		if err := validation.DynamicValue(schema.Output, output); err != nil {
			return d, fmt.Errorf("%s output schema: %w", method, err)
		}
		if validation.DynamicValue(schema.Output, nil) == nil || validation.DynamicValue(schema.Output, map[string]any{}) == nil {
			return d, fmt.Errorf("%s output schema accepts missing result fields", method)
		}
	}
	return d, nil
}

// Session currently exposes application errors as bounded JSON in a fixed
// wrapper. Parse the complete typed object; never mistake a transport failure or
// a message containing a desired code for an expected provider refusal.
func expectedProviderError(err error, expected string) error {
	if err == nil {
		return fmt.Errorf("provider unexpectedly succeeded instead of %s", expected)
	}
	const prefix = "plugin returned application error: "
	text := err.Error()
	if len(text) > 16<<10 || !strings.HasPrefix(text, prefix) {
		return fmt.Errorf("expected %s; got transport/non-application failure: %w", expected, err)
	}
	var rpc struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Code            string   `json:"code"`
			Retryable       *bool    `json:"retryable"`
			SafeNextActions []string `json:"safeNextActions"`
		} `json:"data"`
	}
	if wire.Decode([]byte(strings.TrimPrefix(text, prefix)), &rpc) != nil || rpc.Code != -32010 || rpc.Message == "" || len(rpc.Message) > 2048 || rpc.Data.Code != expected || rpc.Data.Retryable == nil || *rpc.Data.Retryable || rpc.Data.SafeNextActions == nil || len(rpc.Data.SafeNextActions) > 16 {
		return fmt.Errorf("provider did not return exact typed %s refusal", expected)
	}
	return nil
}

type providerHarness struct {
	ctx         context.Context
	session     *Session
	description providerDescription
	calls       int
}

func (h *providerHarness) call(method string, params any) (json.RawMessage, error) {
	if err := h.ctx.Err(); err != nil {
		return nil, err
	}
	h.calls++
	if h.calls > 96 {
		return nil, errors.New("provider conformance call bound exceeded")
	}
	if strings.HasPrefix(method, "provider.") {
		if err := validation.DynamicValue(h.description.Methods[method].Input, params); err != nil {
			return nil, fmt.Errorf("%s input does not match declared schema: %w", method, err)
		}
	}
	limit := 5 * time.Second
	if method == "initialize" {
		limit = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(h.ctx, limit)
	defer cancel()
	raw, err := h.session.Call(ctx, method, params)
	if err != nil {
		return nil, err
	}
	if err = h.ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) > 512<<10 {
		return nil, errors.New("provider conformance response exceeds bounds")
	}
	if strings.HasPrefix(method, "provider.") {
		if err = validation.Dynamic(h.description.Methods[method].Output, raw); err != nil {
			return nil, fmt.Errorf("%s output violates declared schema: %w", method, err)
		}
	}
	return raw, nil
}
func (h *providerHarness) initialize(m Manifest) error {
	raw, err := h.call("initialize", map[string]any{"protocolVersions": []string{"1.0"}, "host": map[string]string{"version": buildinfo.Version, "os": "linux", "arch": "amd64"}, "sessionID": "simulated-provider-conformance", "permissions": []Permission{}, "limits": map[string]int{"maxMessageBytes": wire.MaxFrame}})
	if err != nil {
		return err
	}
	var init struct {
		ProtocolVersion string `json:"protocolVersion"`
		Plugin          struct {
			ID      string `json:"id"`
			Version string `json:"version"`
		} `json:"plugin"`
		ExtensionTypes []string `json:"extensionTypes"`
	}
	if err = wire.Decode(raw, &init); err != nil {
		return err
	}
	if init.ProtocolVersion != "1.0" || init.Plugin.ID != m.ID || init.Plugin.Version != m.Version || !reflect.DeepEqual(init.ExtensionTypes, []string{"provider"}) {
		return errors.New("provider initialized with different identity/version/protocol/extension")
	}
	raw, err = h.call("describe", map[string]any{})
	if err != nil {
		return err
	}
	d, err := checkProviderDescription(raw)
	if err != nil {
		return err
	}
	if h.description.Methods != nil && !jsonEqual(d, h.description) {
		return errors.New("provider description changed across process restart")
	}
	h.description = d
	return nil
}
func (h *providerHarness) plan(operation, id, name string) (providerFixturePlan, map[string]any, error) {
	var plan providerFixturePlan
	params := map[string]any{"connectionID": fixtureProviderConnection, "operation": operation}
	if id != "" {
		params["id"] = id
	}
	if name != "" {
		params["name"] = name
	}
	raw, err := h.call("provider.plan", params)
	if err != nil {
		return plan, nil, err
	}
	if err = wire.Decode(raw, &plan); err != nil {
		return plan, nil, err
	}
	token, decodeErr := hex.DecodeString(plan.Token)
	wantState := map[string]string{"create": "defined", "start": "running", "stop": "stopped", "delete": "deleted"}[operation]
	if !plan.Simulated || plan.State != wantState || plan.Summary != "Simulate "+operation || !reflect.DeepEqual(plan.Effects, []string{operation + " one private JSON resource"}) || plan.Requires == nil || len(plan.Requires) != 0 || decodeErr != nil || len(token) != 32 || hex.EncodeToString(token) != plan.Token {
		return plan, nil, errors.New("provider plan does not explain the exact simulated effect")
	}
	params["planToken"] = plan.Token
	return plan, params, nil
}
func (h *providerHarness) outcome(method string, params map[string]any) (providerFixtureOutcome, error) {
	var out providerFixtureOutcome
	raw, err := h.call(method, params)
	if err != nil {
		return out, err
	}
	if err = wire.Decode(raw, &out); err != nil {
		return out, err
	}
	if !out.Simulated || (out.Status != "partial" && out.Status != "succeeded") {
		return out, errors.New("provider outcome lacks explicit simulated/partial status")
	}
	return out, checkProviderResource(out.providerFixtureResource)
}
func (h *providerHarness) get(id string) (providerFixtureResource, error) {
	var out providerFixtureResource
	raw, err := h.call("provider.get", map[string]any{"connectionID": fixtureProviderConnection, "id": id})
	if err != nil {
		return out, err
	}
	if err = wire.Decode(raw, &out); err != nil {
		return out, err
	}
	return out, checkProviderResource(out)
}
func (h *providerHarness) page(cursor string) (providerFixturePage, error) {
	var out providerFixturePage
	params := map[string]any{"connectionID": fixtureProviderConnection, "limit": 1}
	if cursor != "" {
		params["cursor"] = cursor
	}
	raw, err := h.call("provider.inventory", params)
	if err != nil {
		return out, err
	}
	if err = wire.Decode(raw, &out); err != nil {
		return out, err
	}
	if !out.Simulated || out.Resources == nil || len(out.Resources) > 1 || (out.NextCursor != nil && (*out.NextCursor == "" || len(*out.NextCursor) > 1024)) {
		return out, errors.New("provider inventory is not bounded explicit pagination")
	}
	for _, r := range out.Resources {
		if err = checkProviderResource(r); err != nil {
			return out, err
		}
		if r.State == "deleted" {
			return out, errors.New("deleted resource returned in inventory")
		}
	}
	return out, nil
}
func (h *providerHarness) inventory(expected []providerFixtureResource, generation uint64) (string, error) {
	first, err := h.page("")
	if err != nil {
		return "", err
	}
	repeated, err := h.page("")
	if err != nil {
		return "", err
	}
	if !reflect.DeepEqual(first, repeated) {
		return "", errors.New("first inventory page/cursor is unstable")
	}
	all := []providerFixtureResource{}
	page := first
	seen := map[string]bool{}
	for n := 0; n < 4; n++ {
		if page.Generation != generation {
			return "", errors.New("inventory generation changed or a fake effect was replayed")
		}
		all = append(all, page.Resources...)
		if page.NextCursor == nil {
			break
		}
		if n == 3 || seen[*page.NextCursor] || len(page.Resources) != 1 {
			return "", errors.New("inventory pagination loops or exceeds fixture bound")
		}
		seen[*page.NextCursor] = true
		page, err = h.page(*page.NextCursor)
		if err != nil {
			return "", err
		}
	}
	want := append([]providerFixtureResource{}, expected...)
	sort.Slice(want, func(i, j int) bool { return want[i].ID < want[j].ID })
	if !reflect.DeepEqual(all, want) {
		return "", errors.New("inventory does not contain the exact stable namespaced resources")
	}
	if first.NextCursor != nil {
		return *first.NextCursor, nil
	}
	return "", nil
}

// providerConformance deliberately tests only the local simulated reference
// fixture. Real provider credentials, network permissions and effects are never
// admitted. Start remains the sole executable boundary, including after restart.
func providerConformance(ctx context.Context, exe, workspace string, m Manifest) (ConformanceReport, error) {
	report := ConformanceReport{PluginID: m.ID, Checks: []string{}, EvidenceClass: "simulated-contract"}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if err := checkProviderManifest(m); err != nil {
		return report, err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	h := &providerHarness{ctx: ctx}
	defer func() {
		if h.session != nil {
			h.session.Close()
		}
	}()
	var err error
	h.session, err = Start(ctx, exe, workspace)
	if err != nil {
		return report, err
	}
	report.Confined = true
	if err = h.initialize(m); err != nil {
		return report, err
	}
	report.Checks = append(report.Checks, "simulated provider initialize identity/version/protocol and bounded describe schemas")
	raw, err := h.call("provider.capabilities", map[string]any{"connectionID": fixtureProviderConnection})
	if err != nil {
		return report, err
	}
	if err = checkProviderCapabilityResult(raw); err != nil {
		return report, err
	}
	report.Checks = append(report.Checks, "explicit simulated capabilities and fixture connection namespace")
	if _, err = h.inventory([]providerFixtureResource{}, 0); err != nil {
		return report, fmt.Errorf("fresh fixture workspace required: %w", err)
	}
	resources := []providerFixtureResource{}
	for _, suffix := range []string{"a", "b"} {
		_, params, e := h.plan("create", "", "reference resource "+suffix)
		if e != nil {
			return report, e
		}
		params["operationID"], params["idempotencyKey"] = "conformance-create-"+suffix, "conformance-key-"+suffix
		out, e := h.outcome("provider.apply", params)
		if e != nil {
			return report, e
		}
		if out.ID != "fixture-conformance-create-"+suffix || out.Name != "reference resource "+suffix || out.State != "defined" || out.Revision != 1 || out.OperationID != params["operationID"] || out.Status != "succeeded" || out.Reconciled {
			return report, errors.New("create returned a different effect/receipt")
		}
		got, e := h.get(out.ID)
		if e != nil {
			return report, e
		}
		if !reflect.DeepEqual(got, out.providerFixtureResource) {
			return report, errors.New("get differs from original creation receipt")
		}
		duplicate, e := h.outcome("provider.apply", params)
		if e != nil {
			return report, e
		}
		if !reflect.DeepEqual(duplicate, out) {
			return report, errors.New("idempotent create changed its receipt")
		}
		params["name"] = "changed request"
		_, e = h.call("provider.apply", params)
		if err = expectedProviderError(e, "STALE_PLAN"); err != nil {
			return report, err
		}
		resources = append(resources, got)
	}
	report.Checks = append(report.Checks, "reviewed create/get with stable fake IDs, exact deduplication and stale idempotency refusal")
	oldCursor, err := h.inventory(resources, 2)
	if err != nil {
		return report, err
	}
	if oldCursor == "" {
		return report, errors.New("two resources did not paginate at limit one")
	}
	report.Checks = append(report.Checks, "stable ordered paginated inventory with repeated opaque cursors")
	_, partialParams, err := h.plan("create", "", "partial reference resource")
	if err != nil {
		return report, err
	}
	partialParams["operationID"], partialParams["idempotencyKey"], partialParams["partial"] = "conformance-partial", "conformance-partial-key", true
	_, err = h.call("provider.apply", partialParams)
	if err = expectedProviderError(err, "PARTIAL_EFFECT"); err != nil {
		return report, err
	}
	report.Checks = append(report.Checks, "partial effect reported: operation=conformance-partial; resource=fixture-conformance-partial; status=partial")
	h.session.Close()
	h.session = nil
	h.session, err = Start(ctx, exe, workspace)
	if err != nil {
		return report, err
	}
	if err = h.initialize(m); err != nil {
		return report, err
	}
	selector := map[string]any{"connectionID": fixtureProviderConnection, "id": "fixture-conformance-partial", "operationID": "conformance-partial", "idempotencyKey": "conformance-partial-key"}
	partial, err := h.outcome("provider.status", selector)
	if err != nil {
		return report, err
	}
	if partial.ID != "fixture-conformance-partial" || partial.OperationID != "conformance-partial" || partial.Name != "partial reference resource" || partial.State != "defined" || partial.Revision != 1 || partial.Status != "partial" || partial.Reconciled {
		return report, errors.New("restart lost or promoted the original partial receipt")
	}
	resources = append(resources, partial.providerFixtureResource)
	_, err = h.page(oldCursor)
	if err = expectedProviderError(err, "STALE_PLAN"); err != nil {
		return report, err
	}
	if _, err = h.inventory(resources, 3); err != nil {
		return report, err
	}
	observed, err := h.outcome("provider.reconcile", selector)
	if err != nil {
		return report, err
	}
	wantPartial := partial
	wantPartial.Reconciled = true
	if !reflect.DeepEqual(observed, wantPartial) {
		return report, errors.New("reconcile replayed or promoted the original partial effect")
	}
	status, err := h.outcome("provider.status", selector)
	if err != nil {
		return report, err
	}
	if !reflect.DeepEqual(status, wantPartial) {
		return report, errors.New("reconciled partial receipt was not retained")
	}
	if _, err = h.inventory(resources, 3); err != nil {
		return report, err
	}
	report.Checks = append(report.Checks, "restart/status/reconcile without apply replay: fixture-conformance-partial remains status=partial, reconciled=true, revision=1; inventory generation unchanged")
	current := resources[0]
	for i, operation := range []string{"start", "stop", "delete"} {
		plan, params, e := h.plan(operation, current.ID, "")
		if e != nil {
			return report, e
		}
		params["operationID"], params["idempotencyKey"] = "conformance-"+operation, "conformance-key-"+operation
		out, e := h.outcome("provider.apply", params)
		if e != nil {
			return report, e
		}
		if out.ID != current.ID || out.Name != current.Name || out.State != plan.State || out.Revision != current.Revision+1 || out.OperationID != params["operationID"] || out.Status != "succeeded" || out.Reconciled {
			return report, errors.New("lifecycle operation changed identity or lost exact receipt state")
		}
		current = out.providerFixtureResource
		if operation != "delete" {
			got, e := h.get(current.ID)
			if e != nil {
				return report, e
			}
			if !reflect.DeepEqual(got, current) {
				return report, errors.New("lifecycle get differs from receipt")
			}
		}
		if i == 2 {
			_, e = h.get(current.ID)
			if err = expectedProviderError(e, "RESOURCE_MISSING"); err != nil {
				return report, err
			}
		}
	}
	if _, err = h.inventory(resources[1:], 6); err != nil {
		return report, err
	}
	tombstone, err := h.outcome("provider.status", map[string]any{"connectionID": fixtureProviderConnection, "operationID": "conformance-delete"})
	if err != nil {
		return report, err
	}
	if !reflect.DeepEqual(tombstone.providerFixtureResource, current) || tombstone.Status != "succeeded" {
		return report, errors.New("deleted resource receipt was lost")
	}
	report.Checks = append(report.Checks, "reviewed start/stop/delete preserve resource identity and retain a deleted receipt")
	_, err = h.call("provider.plan", map[string]any{"connectionID": fixtureProviderConnection, "operation": "backup"})
	if err = expectedProviderError(err, "UNSUPPORTED_CAPABILITY"); err != nil {
		return report, err
	}
	_, err = h.call("provider.cancel", selector)
	if err = expectedProviderError(err, "UNSUPPORTED_CAPABILITY"); err != nil {
		return report, err
	}
	if _, err = h.inventory(resources[1:], 6); err != nil {
		return report, err
	}
	report.Checks = append(report.Checks, "backup and lifecycle cancellation explicitly unsupported; no fake supported result")
	raw, err = h.call("shutdown", map[string]any{})
	if err != nil {
		return report, err
	}
	var shutdown struct {
		Shutdown bool `json:"shutdown"`
	}
	if wire.Decode(raw, &shutdown) != nil || !shutdown.Shutdown {
		return report, errors.New("provider shutdown not acknowledged")
	}
	if err = ctx.Err(); err != nil {
		return report, err
	}
	report.Checks = append(report.Checks, "shutdown; simulated-contract only, no real remote-provider or hardware qualification")
	return report, nil
}
