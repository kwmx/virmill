package guestsetup

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/guestssh"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

const operation = "guest.recipe.run"
const stageKind = "guest-recipe-stage-v1"
const readiness = "id -u\n"

var stageNames = []string{"readiness", "check", "apply", "verify"}
var sshUser = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)

type Transport interface {
	Identity(context.Context) (guestssh.Identity, error)
	InspectTarget(context.Context, guestssh.Target) (guestssh.TargetIdentity, error)
	Run(context.Context, guestssh.Target, guestssh.Script) (guestssh.Result, error)
}
type Service struct {
	Engine    *operations.Engine
	Provider  domain.ComputeProvider
	Transport Transport
}

func Register(a *app.Service) {
	s := &Service{Engine: a.Engine, Provider: a.Provider, Transport: guestssh.Tool{}}
	s.Register(a)
}

type parameters struct {
	Address        string   `json:"address"`
	Port           uint16   `json:"port"`
	User           string   `json:"user"`
	IdentityFile   string   `json:"identityFile"`
	KnownHostsFile string   `json:"knownHostsFile"`
	Arguments      []string `json:"arguments"`
}
type frozenInput struct {
	Version      int                `json:"version"`
	Resource     domain.ResourceKey `json:"resource"`
	Fingerprint  string             `json:"fingerprint"`
	Recipe       Recipe             `json:"recipe"`
	RecipeSHA256 string             `json:"recipeSHA256"`
	ScriptSHA256 map[string]string  `json:"scriptSHA256"`
	Target       guestssh.Target    `json:"target"`
	Arguments    []string           `json:"arguments"`
	Tool         guestssh.Identity  `json:"sshTool"`
}

func (s *Service) Register(a *app.Service) {
	if a.Extensions == nil {
		a.Extensions = map[string]func(context.Context, uint32, app.Request) (any, error){}
	}
	a.Extensions[operation] = s.Plan
	a.Extensions["guest.recipe.result"] = s.Result
	a.Extensions["guest.tools.catalog"] = s.ToolsCatalog
	a.Extensions["guest.tools.install"] = s.PlanTools
	if s.Engine != nil {
		if s.Engine.Handlers == nil {
			s.Engine.Handlers = map[string]operations.Handler{}
		}
		s.Engine.Handlers[operation] = s
	}
}
func (s *Service) RetainCompletedEffects() bool { return true }
func safeFailure(err error, message string) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var d *domain.Error
	if errors.As(err, &d) {
		switch d.Code {
		case "PERMISSION_DENIED", "UNSUPPORTED_CAPABILITY", "SOURCE_CHANGED", "RESOURCE_BUSY", "RECOVERY_REQUIRED", "INVALID_INPUT", "INVALID_STATE", "GUEST_TRANSPORT_FAILED":
			return domain.Fail(d.Code, message)
		}
	}
	return domain.Fail("OPERATION_FAILED", message)
}
func local(uri string) bool { return uri == "qemu:///system" || uri == "qemu:///session" }
func script(r Recipe, name string) string {
	switch name {
	case "readiness":
		return readiness
	case "check":
		return r.Spec.Check
	case "apply":
		return r.Spec.Apply
	case "verify":
		return r.Spec.Verify
	}
	return ""
}
func scriptDigests(r Recipe) map[string]string {
	d := map[string]string{}
	for _, name := range stageNames {
		d[name] = hash([]byte(script(r, name)))
	}
	return d
}
func resources(k domain.ResourceKey) []string {
	out := []string{k.String(), "guest-setup:" + k.String()}
	sort.Strings(out)
	return out
}
func acknowledgements(r Recipe) []string {
	a := []string{"guest-execution", "guest-host-key-binding", "non-root-guest-setup"}
	if r.Spec.Privilege == "sudo" {
		a[2] = "guest-admin-package-install"
	}
	if !r.Spec.Idempotent {
		a = append(a, "non-idempotent-recipe")
	}
	return a
}
func steps() []domain.Step {
	out := make([]domain.Step, 0, 4)
	for _, name := range stageNames {
		out = append(out, domain.Step{ID: name, Action: "guest recipe " + name, Preconditions: []string{"exact approved running VM and SSH target"}, Idempotency: "never replay an existing stage intent", Compensation: "retain uncertain guest effect", Reconciliation: "verify durable stage receipt only; never reconnect to rerun", CompletionPredicate: "bound stage receipt confirms the declared exit policy"})
	}
	return out
}
func validArguments(args []string) bool {
	if args == nil || len(args) > 64 {
		return false
	}
	total := 0
	for _, arg := range args {
		total += len(arg)
		if !safeText(arg, 4096, false) {
			return false
		}
	}
	return total <= 16384
}
func validTarget(t guestssh.Target, requireHashes bool) bool {
	address, err := netip.ParseAddr(t.Address)
	if err != nil || address.Is4In6() || address.Zone() != "" || !address.IsGlobalUnicast() || address.IsLoopback() || address.String() != t.Address || t.Port == 0 || !sshUser.MatchString(t.User) || strings.EqualFold(t.User, "root") {
		return false
	}
	for _, p := range []string{t.IdentityFile, t.KnownHostsFile} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || !safeText(p, 4096, false) {
			return false
		}
	}
	return !requireHashes || digestPattern.MatchString(t.IdentitySHA256) && digestPattern.MatchString(t.KnownHostsSHA256)
}
func (s *Service) ready() error {
	if s.Engine == nil || s.Engine.Store == nil || s.Provider == nil || s.Transport == nil {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "guest recipe provider, journal and SSH transport are required")
	}
	return nil
}
func (s *Service) Plan(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uid == 0 {
		return nil, domain.Fail("PERMISSION_DENIED", "guest recipes require an ordinary coordinator actor")
	}
	if !local(r.Connection) {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "guest recipes require an explicit local libvirt connection")
	}
	if !validUUID(r.ID) || r.Path == "" || r.After != 0 || r.Apply != nil || r.Action != "" && r.Action != "run" {
		return nil, invalid("run requires one VM UUID, recipe path and exact SSH input")
	}
	if err := s.ready(); err != nil {
		return nil, err
	}
	data, err := operations.Canonical(r.Input)
	if err != nil {
		return nil, invalid("invalid SSH input")
	}
	var args parameters
	if err = strictDecode(data, &args); err != nil {
		return nil, err
	}
	target := guestssh.Target{Address: args.Address, Port: args.Port, User: args.User, IdentityFile: args.IdentityFile, KnownHostsFile: args.KnownHostsFile}
	if !validTarget(target, false) || !validArguments(args.Arguments) {
		return nil, invalid("explicit literal-IP SSH target and bounded non-secret arguments required")
	}
	data, err = readRecipe(ctx, r.Path)
	if err != nil {
		return nil, err
	}
	recipe, err := DecodeRecipe(data)
	if err != nil {
		return nil, err
	}
	return s.planRecipe(ctx, uid, r, recipe, args)
}

// planRecipe freezes an already validated recipe directly; built-ins never
// need a temporary file or a caller-supplied script path.
func (s *Service) planRecipe(ctx context.Context, uid uint32, r app.Request, recipe Recipe, args parameters) (any, error) {
	if err := recipe.Validate(); err != nil {
		return nil, err
	}
	target := guestssh.Target{Address: args.Address, Port: args.Port, User: args.User, IdentityFile: args.IdentityFile, KnownHostsFile: args.KnownHostsFile}
	if !validTarget(target, false) || !validArguments(args.Arguments) {
		return nil, invalid("provide the guest's literal IP address, SSH port, non-root user, absolute private-key path and verified known-hosts path")
	}
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: r.Connection, Kind: "vm", UUID: r.ID}
	vm, err := s.Provider.Get(ctx, r.Connection, r.ID)
	if err != nil {
		return nil, safeFailure(err, "native VM observation failed; details withheld")
	}
	if err = validateVM(vm, key); err != nil {
		return nil, err
	}
	tool, err := s.Transport.Identity(ctx)
	if err != nil {
		return nil, safeFailure(err, "SSH executable identity is unavailable")
	}
	identity, err := s.Transport.InspectTarget(ctx, target)
	if err != nil {
		return nil, safeFailure(err, "SSH key or known-hosts file is missing or unsafe; select readable private files and verify this guest's host key first")
	}
	target.IdentitySHA256, target.KnownHostsSHA256 = identity.IdentitySHA256, identity.KnownHostsSHA256
	digest, err := operations.Digest(recipe)
	if err != nil {
		return nil, invalid("recipe digest unavailable")
	}
	in := frozenInput{Version: 1, Resource: key, Fingerprint: vm.Fingerprint, Recipe: recipe, RecipeSHA256: digest, ScriptSHA256: scriptDigests(recipe), Target: target, Arguments: append([]string{}, args.Arguments...), Tool: tool}
	warnings := []string{"The reviewer binds the supplied address and known-hosts key to this VM; native IP identity is unavailable.", "Reviewed recipe scripts and arguments persist in private journal input. Do not embed credentials.", "A remote script may change guest state before an interrupted SSH response; reconciliation never reruns it."}
	if recipe.Spec.Privilege == "sudo" {
		warnings = append(warnings, "This built-in uses passwordless sudo inside the guest to install packages from its configured repositories and start qemu-guest-agent. Dependencies may change; there is no automatic rollback or reboot.", "The guest needs the org.qemu.guest_agent.0 virtio channel. Desktop package installation does not prove clipboard or display integration works.")
	}
	p, err := s.Engine.Plan(ctx, uid, r.Connection, operation, resources(key), map[string]string{key.String(): vm.Fingerprint}, in, steps(), acknowledgements(recipe), warnings)
	if err != nil {
		return nil, safeFailure(err, "guest recipe plan could not be accepted")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return p, nil
}
func validateVM(v domain.VM, key domain.ResourceKey) error {
	if v.Key != key || v.PersistentXML == "" || v.State != "running" || v.HasManagedSave || !digestPattern.MatchString(v.Fingerprint) {
		return domain.Fail("INVALID_STATE", "guest recipe requires the exact persistent running VM without managed-save state")
	}
	return nil
}
func decodeInput(p domain.Plan, raw []byte) (frozenInput, error) {
	var in frozenInput
	if len(raw) > 1<<20 {
		return in, invalid("stored recipe input exceeds its bound")
	}
	if err := strictDecode(raw, &in); err != nil {
		return in, err
	}
	if in.Version != 1 || !validUUID(in.Resource.UUID) || in.Resource != (domain.ResourceKey{ProviderID: "libvirt", ConnectionID: p.ConnectionID, Kind: "vm", UUID: in.Resource.UUID}) || !local(p.ConnectionID) || p.ActorUID == 0 || p.Operation != operation || !digestPattern.MatchString(in.Fingerprint) {
		return in, invalid("stored recipe identity is invalid")
	}
	if err := in.Recipe.Validate(); err != nil {
		return in, err
	}
	digest, err := operations.Digest(in.Recipe)
	if err != nil || digest != in.RecipeSHA256 || !reflect.DeepEqual(in.ScriptSHA256, scriptDigests(in.Recipe)) || !validArguments(in.Arguments) || !validTarget(in.Target, true) || in.Tool.Path != "/usr/bin/ssh" || !digestPattern.MatchString(in.Tool.SHA256) || in.Tool.Version == "" || !safeText(in.Tool.Version, 512, false) {
		return in, domain.Fail("SOURCE_CHANGED", "stored recipe content or SSH identity differs")
	}
	actual, err := operations.Digest(json.RawMessage(raw))
	if err != nil || actual != p.InputDigest || !reflect.DeepEqual(p.ResourceIDs, resources(in.Resource)) || !reflect.DeepEqual(p.Before, map[string]string{in.Resource.String(): in.Fingerprint}) || !reflect.DeepEqual(p.Steps, steps()) || !reflect.DeepEqual(p.Acknowledgements, acknowledgements(in.Recipe)) {
		return in, domain.Fail("SOURCE_CHANGED", "stored recipe plan binding differs")
	}
	return in, nil
}
func (s *Service) Validate(ctx context.Context, p domain.Plan, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ready(); err != nil {
		return err
	}
	in, err := decodeInput(p, raw)
	if err != nil {
		return err
	}
	if err = s.boundary(ctx); err != nil {
		return err
	}
	vm, err := s.Provider.Get(ctx, p.ConnectionID, in.Resource.UUID)
	if err != nil {
		return safeFailure(err, "native VM revalidation failed; details withheld")
	}
	if err = validateVM(vm, in.Resource); err != nil {
		return err
	}
	if vm.Fingerprint != in.Fingerprint {
		return domain.Fail("SOURCE_CHANGED", "VM changed after guest recipe review")
	}
	tool, err := s.Transport.Identity(ctx)
	if err != nil {
		return safeFailure(err, "SSH executable revalidation failed")
	}
	if tool != in.Tool {
		return domain.Fail("SOURCE_CHANGED", "SSH executable changed after review")
	}
	identity, err := s.Transport.InspectTarget(ctx, in.Target)
	if err != nil {
		return safeFailure(err, "SSH target files changed or became unavailable")
	}
	if identity.IdentitySHA256 != in.Target.IdentitySHA256 || identity.KnownHostsSHA256 != in.Target.KnownHostsSHA256 {
		return domain.Fail("SOURCE_CHANGED", "SSH target files changed after review")
	}
	return s.boundary(ctx)
}
func (s *Service) Review(ctx context.Context, p domain.Plan, raw []byte) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	in, err := decodeInput(p, raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{"recipe": in.Recipe, "recipeSHA256": in.RecipeSHA256, "scriptSHA256": in.ScriptSHA256, "resource": in.Resource, "address": in.Target.Address, "port": in.Target.Port, "user": in.Target.User, "identitySHA256": in.Target.IdentitySHA256, "knownHostsSHA256": in.Target.KnownHostsSHA256, "arguments": in.Arguments, "sshTool": in.Tool, "nativeAddressBindingVerified": false, "reboot": "never", "rawOutputRetained": false}, nil
}
func (s *Service) boundary(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	id := operations.OperationID(ctx)
	if id == "" {
		return nil
	}
	j, err := s.Engine.Store.Job(id)
	if err != nil {
		return safeFailure(err, "guest recipe job observation failed")
	}
	if j.CancelRequested && j.State != "recovery-required" && j.State != "interrupted" {
		return domain.Fail("RECOVERY_REQUIRED", "guest recipe canceled; retain stage intents and receipts for explicit reconciliation")
	}
	return ctx.Err()
}

// Canceling a client does not cancel an accepted job. An explicit durable cancel
// request does cancel the SSH child; its remote effect may remain unknown.
func (s *Service) runContext(ctx context.Context, timeout time.Duration) (context.Context, func()) {
	child, cancel := context.WithTimeout(ctx, timeout)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-child.Done():
				return
			case <-tick.C:
				if err := s.boundary(ctx); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return child, func() { cancel(); <-done }
}
