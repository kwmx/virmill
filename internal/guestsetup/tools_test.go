package guestsetup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/guestssh"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/ui"
)

func TestGuestToolsCatalogAndExactBuiltinRecipes(t *testing.T) {
	data, err := (&Service{}).ToolsCatalog(context.Background(), 1000, app.Request{})
	if err != nil {
		t.Fatal(err)
	}
	profiles := data.([]ToolProfile)
	if len(profiles) != 5 || profiles[4].ID != "windows" || profiles[4].Automatic || len(profiles[4].ManualSteps) == 0 {
		t.Fatal("Windows must retain the explicit manual path", profiles)
	}
	for _, profile := range profiles[:4] {
		for _, desktop := range []bool{false, true} {
			r, ok := builtinToolsRecipe(profile.ID, desktop)
			if !ok || r.Validate() != nil || !isBuiltinToolsRecipe(r) || r.Spec.Privilege != "sudo" || r.Spec.Reboot != "never" {
				t.Fatal("invalid built-in", r)
			}
			if got, _ := operations.Digest(r); !desktop && got != profile.RecipeSHA256 || desktop && got != profile.DesktopRecipeSHA256 {
				t.Fatal("catalog digest does not bind exact recipe")
			}
			encoded, _ := json.Marshal(r)
			decoded, err := DecodeRecipe(encoded)
			if err != nil || decoded != r {
				t.Fatal("builtin cannot round-trip", err)
			}
			for _, content := range []string{r.Spec.Check, r.Spec.Apply, r.Spec.Verify} {
				// Syntax check only. Never run these scripts on the development host.
				cmd := exec.Command("/bin/sh", "-n")
				cmd.Stdin = strings.NewReader(content)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("invalid POSIX syntax: %v %s", err, out)
				}
			}
			for _, alter := range []func(*Recipe){
				func(v *Recipe) { v.Spec.Apply += "\nsudo arbitrary-command\n" },
				func(v *Recipe) { v.Metadata.Version = "1.0.1" },
				func(v *Recipe) { v.Spec.TimeoutSeconds-- },
				func(v *Recipe) { v.Spec.Check = "exit 3\n" },
				func(v *Recipe) { v.Spec.Idempotent = false },
				func(v *Recipe) { v.Spec.Privilege = "non-root" },
			} {
				changed := r
				alter(&changed)
				if changed.Validate() == nil || isBuiltinToolsRecipe(changed) {
					t.Fatal("modified recipe retained guest sudo authorization")
				}
			}
		}
	}
}

func TestGuestToolsExamplesUseInputsWithoutRecipeFiles(t *testing.T) {
	for _, path := range []string{"../../examples/guest-tools/linux-auto.json", "../../examples/guest-tools/ubuntu-desktop.json"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var input toolsParameters
		if err := strictDecode(data, &input); err != nil {
			t.Fatal(path, err)
		}
		r, ok := builtinToolsRecipe(input.Profile, input.Desktop)
		if !ok || r.Validate() != nil {
			t.Fatal("example selects unavailable recipe", path)
		}
	}
}

type toolsFixtureTransport struct {
	base       *recipeTransport
	recipe     Recipe
	exits      map[string]int
	inspectErr error
}

func (s *toolsFixtureTransport) Identity(ctx context.Context) (guestssh.Identity, error) {
	return s.base.Identity(ctx)
}
func (s *toolsFixtureTransport) InspectTarget(ctx context.Context, target guestssh.Target) (guestssh.TargetIdentity, error) {
	if s.inspectErr != nil {
		return guestssh.TargetIdentity{}, s.inspectErr
	}
	return s.base.InspectTarget(ctx, target)
}
func (s *toolsFixtureTransport) Run(ctx context.Context, target guestssh.Target, input guestssh.Script) (guestssh.Result, error) {
	stage := ""
	for _, name := range stageNames {
		if string(input.Content) == script(s.recipe, name) {
			stage = name
			break
		}
	}
	if stage == "" || len(input.Arguments) != 0 {
		return guestssh.Result{}, errors.New("unexpected builtin content or arguments")
	}
	if stage != "readiness" {
		input.Content = []byte(stage)
	}
	result, err := s.base.Run(ctx, target, input)
	if code, ok := s.exits[stage]; ok {
		result.ExitCode = code
	}
	return result, err
}

func toolsRequest(h *recipeHarness) app.Request {
	h.provider.vm.LiveXML = "<domain id='7'><devices><channel type='unix'><target type='virtio' name='org.qemu.guest_agent.0' state='disconnected'/></channel></devices></domain>"
	return app.Request{Connection: h.request.Connection, ID: h.request.ID, Input: map[string]any{"profile": "linux-auto", "desktop": false, "address": "192.0.2.42", "port": 2222, "user": "guest", "identityFile": "/private/identity", "knownHostsFile": "/private/known-hosts"}}
}

func TestGuestToolsPlanAndDurableIdempotentStages(t *testing.T) {
	for _, check := range []int{0, 3} {
		t.Run(string(rune('0'+check)), func(t *testing.T) {
			h := newRecipeHarness(t)
			h.transport.check = check
			recipe, _ := builtinToolsRecipe("linux-auto", false)
			h.service.Transport = &toolsFixtureTransport{base: h.transport, recipe: recipe}
			response := h.app.Call(context.Background(), 1000, "guest.tools.install", toolsRequest(h))
			if response.Error != nil {
				t.Fatal(response.Error)
			}
			p := response.Data.(domain.Plan)
			if p.Operation != operation || !reflect.DeepEqual(p.Acknowledgements, []string{"guest-execution", "guest-host-key-binding", "guest-admin-package-install"}) || len(h.transport.calls) != 0 {
				t.Fatal("plan did not describe guest administration or executed before approval", p)
			}
			_, raw, err := h.db.Plan(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := decodeInput(p, raw)
			if err != nil || frozen.Recipe != recipe {
				t.Fatal("recipe not frozen", err)
			}
			j := h.apply(t, p)
			if j.State != "succeeded" {
				t.Fatal(j)
			}
			want := []string{"readiness", "check", "verify"}
			if check == 3 {
				want = []string{"readiness", "check", "apply", "verify"}
			}
			if !reflect.DeepEqual(h.transport.calls, want) {
				t.Fatal(h.transport.calls)
			}
			result := h.app.Call(context.Background(), 1000, "guest.recipe.result", app.Request{Connection: h.request.Connection, ID: j.ID})
			if result.Error != nil || !result.Data.(Result).Complete {
				t.Fatal(result)
			}
		})
	}
}

func TestGuestToolsRegisteredInstallActionDispatch(t *testing.T) {
	var action ui.Action
	for _, candidate := range ui.Actions {
		if candidate.Command == "guest tools install" {
			action = candidate
		}
	}
	if action.Method != "guest.tools.install" || action.Argument != "id" || action.Mutation != "install" {
		t.Fatal("guest tools command no longer dispatches the expected shared-service action", action)
	}
	h := newRecipeHarness(t)
	r := toolsRequest(h)
	// The CLI and TUI registry supply Action=install; a direct service caller
	// may omit it. Both must freeze the same built-in recipe without executing.
	for _, verb := range []string{action.Mutation, ""} {
		r.Action = verb
		response := h.app.Call(context.Background(), 1000, action.Method, r)
		if response.Error != nil {
			t.Fatalf("registered action %q rejected by shared service: %v", verb, response.Error)
		}
		p := response.Data.(domain.Plan)
		_, raw, err := h.db.Plan(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		frozen, err := decodeInput(p, raw)
		if err != nil || !isBuiltinToolsRecipe(frozen.Recipe) || frozen.Recipe.Spec.Privilege != "sudo" || p.Operation != operation {
			t.Fatal("dispatch did not freeze the built-in administrative recipe", err)
		}
	}
	for _, verb := range []string{"run", "apply", "Install", "install;anything"} {
		r.Action = verb
		if response := h.app.Call(context.Background(), 1000, action.Method, r); response.Error == nil || response.Error.Code != "INVALID_INPUT" {
			t.Fatalf("unexpected action %q accepted: %+v", verb, response)
		}
	}
	if len(h.transport.calls) != 0 {
		t.Fatal("planning dispatch executed a guest command")
	}
}

func TestGuestToolsFailureGuidanceAndNoReplay(t *testing.T) {
	for _, tc := range []struct {
		stage string
		code  int
		hint  string
	}{{"check", 20, "distribution"}, {"check", 21, "systemd"}, {"check", 22, "channel"}, {"apply", 23, "sudo"}, {"apply", 24, "repositories"}, {"verify", 25, "verification"}} {
		t.Run(tc.hint, func(t *testing.T) {
			h := newRecipeHarness(t)
			h.transport.check = 3
			recipe, _ := builtinToolsRecipe("linux-auto", false)
			h.service.Transport = &toolsFixtureTransport{base: h.transport, recipe: recipe, exits: map[string]int{tc.stage: tc.code}}
			response := h.app.Call(context.Background(), 1000, "guest.tools.install", toolsRequest(h))
			if response.Error != nil {
				t.Fatal(response.Error)
			}
			p := response.Data.(domain.Plan)
			j := h.apply(t, p)
			if j.State == "succeeded" {
				t.Fatal("failed stage reported success")
			}
			if j.Error == nil || j.Error.Code != "GUEST_RECIPE_FAILED" || !strings.Contains(j.Error.Message, tc.hint) {
				t.Fatal("durable job lost actionable guest-tools failure", j.Error)
			}
			if err := toolsStageFailure(tc.code); !strings.Contains(err.Error(), tc.hint) {
				t.Fatal(err)
			}
			count := len(h.transport.calls)
			if reconciled, err := h.app.Engine.Reconcile(context.Background(), j.ID); err == nil && reconciled.State == "succeeded" {
				t.Fatal("failed stage reconciled as success")
			}
			if len(h.transport.calls) != count {
				t.Fatal("reconciliation replayed guest commands")
			}
		})
	}
}

func TestGuestToolsMissingCredentialsAndUnsupportedInputs(t *testing.T) {
	h := newRecipeHarness(t)
	recipe, _ := builtinToolsRecipe("linux-auto", false)
	h.service.Transport = &toolsFixtureTransport{base: h.transport, recipe: recipe, inspectErr: errors.New("private diagnostic")}
	response := h.app.Call(context.Background(), 1000, "guest.tools.install", toolsRequest(h))
	if response.Error == nil || !strings.Contains(response.Error.Message, "known-hosts") || strings.Contains(response.Error.Message, "private diagnostic") {
		t.Fatal(response)
	}
	for _, tc := range []struct {
		field string
		value any
	}{{"profile", "windows"}, {"user", "root"}, {"address", "guessed-host.local"}, {"identityFile", "relative"}, {"script", "sudo anything"}} {
		r := toolsRequest(h)
		r.Input[tc.field] = tc.value
		if reply := h.app.Call(context.Background(), 1000, "guest.tools.install", r); reply.Error == nil {
			t.Fatal("invalid tools input accepted", tc.field)
		}
	}
	if len(h.transport.calls) != 0 {
		t.Fatal("planning ran SSH")
	}
}
