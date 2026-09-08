package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

const recipeCLIVM = "12345678-1234-1234-1234-123456789abc"
const recipeCLIPath = "/private/recipes/owner setup.json"
const recipeCLIInput = `{"address":"192.0.2.25","port":2222,"user":"operator","identityFile":"/private/keys/owner key","knownHostsFile":"/private/keys/known hosts","arguments":["literal $(touch never)","two words","ضيف","--not-a-host-flag"]}`

// The real command and shared dispatcher run here. Only the guest service
// extension is replaced; these tests do not execute SSH or establish readiness.
func recipeCLIService(fn func(context.Context, uint32, app.Request) (any, error)) *recoveryCLIServiceClient {
	return &recoveryCLIServiceClient{service: app.Service{Extensions: map[string]func(context.Context, uint32, app.Request) (any, error){
		"guest.recipe.run": fn, "guest.recipe.result": fn,
	}}}
}

func recipeCLIExecute(t *testing.T, ctx context.Context, c *recoveryCLIServiceClient, output string, args ...string) (app.Response, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	cmd := New(c, &out, &stderr)
	cmd.SetIn(forbiddenPrompt{})
	cmd.SetArgs(append(args, "--connection", "qemu:///session", "--output", output, "--non-interactive"))
	err := cmd.ExecuteContext(ctx)
	if stderr.Len() != 0 || output == "ndjson" && strings.Count(out.String(), "\n") != 1 {
		t.Fatal("unclean machine response", out.String(), stderr.String())
	}
	var r app.Response
	if e := json.Unmarshal(out.Bytes(), &r); e != nil {
		t.Fatal("invalid response envelope", out.String(), e)
	}
	return r, err
}

func TestGuestRecipeCLIReviewedPlanAndLiteralInput(t *testing.T) {
	p := coldCLIPlan(t, "create")
	p.Operation = "guest.recipe.run"
	p.Acknowledgements = []string{"guest-execution", "guest-host-key-binding", "non-root-guest-setup", "non-idempotent-recipe"}
	p.InputDigest = strings.Repeat("a", 64)
	p.Review = map[string]any{"recipeSHA256": strings.Repeat("b", 64), "privilege": "non-root", "reboot": "never", "nativeAddressBindingVerified": false, "rawOutputRetained": false}
	var err error
	p.Digest, err = operations.PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(recipeCLIInput), &input); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{"json", "ndjson"} {
		for _, explicit := range []bool{false, true} {
			t.Run(output+map[bool]string{false: "/default-plan", true: "/explicit-plan"}[explicit], func(t *testing.T) {
				var received app.Request
				calls := 0
				c := recipeCLIService(func(_ context.Context, uid uint32, r app.Request) (any, error) {
					calls++
					received = r
					if uid != 1000 {
						t.Fatal("actor changed", uid)
					}
					return p, nil
				})
				args := []string{"guest", "recipe", "run", recipeCLIVM, recipeCLIPath, "--input", recipeCLIInput}
				if explicit {
					args = append(args, "--plan")
				}
				r, err := recipeCLIExecute(t, context.Background(), c, output, args...)
				if err != nil || r.Error != nil || c.calls != 1 || calls != 1 || c.method != "guest.recipe.run" {
					t.Fatal("run did not return one shared preview", r, err, c.method, c.calls, calls)
				}
				wantRequest := app.Request{Connection: "qemu:///session", ID: recipeCLIVM, Path: recipeCLIPath, Action: "run", Input: input}
				if !reflect.DeepEqual(received, wantRequest) || !reflect.DeepEqual(c.request, wantRequest) {
					t.Fatal("VM/path/arguments changed or implicit apply authority appeared", received, c.request)
				}
				want, _ := operations.Canonical(app.Response{APIVersion: domain.APIVersion, Data: p, Warnings: []string{}})
				got, _ := operations.Canonical(r)
				if !bytes.Equal(want, got) {
					t.Fatal("review, content digest, acknowledgements or evidence boundary changed", string(got))
				}
			})
		}
	}
}

func TestGuestRecipeCLIRejectsMissingPathImplicitApplyAndMalformedInput(t *testing.T) {
	base := []string{"guest", "recipe", "run", recipeCLIVM, recipeCLIPath}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"missing-vm-and-path", base[:3]},
		{"missing-path", base[:4]},
		{"extra-path", append(append([]string{}, base...), "/private/another.json")},
		{"implicit-apply", append(append([]string{}, base...), "--plan=false")},
		{"duplicate-input", append(append([]string{}, base...), "--input", `{"user":"operator","user":"root"}`)},
		{"input-array", append(append([]string{}, base...), "--input", `[]`)},
		{"trailing-input", append(append([]string{}, base...), "--input", `{} {}`)},
		{"result-extra-path", []string{"guest", "recipe", "result", recipeCLIVM, recipeCLIPath}},
		{"result-plan-flag", []string{"guest", "recipe", "result", recipeCLIVM, "--plan"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &coldCLIClient{}
			var out, stderr bytes.Buffer
			cmd := New(c, &out, &stderr)
			cmd.SetIn(forbiddenPrompt{})
			cmd.SetArgs(append(append([]string{}, tc.args...), "--output", "json", "--non-interactive"))
			if err := cmd.Execute(); err == nil || len(c.requests) != 0 || out.Len() != 0 {
				t.Fatal("invalid request reached service or displayed success", err, c.requests, out.String())
			}
		})
	}
}

func TestGuestRecipeCLIResultIsReadOnlyAndPreservesUnknownEffect(t *testing.T) {
	result := map[string]any{"operationID": "860223f8-fbb4-4be7-a053-71a49818e768", "state": "recovery-required", "complete": false, "rawOutputRetained": false, "nativeAddressBindingVerified": false,
		"stages": []any{map[string]any{"stage": "apply", "complete": false, "effectUnknown": true, "receipt": nil}}}
	for _, output := range []string{"json", "ndjson"} {
		t.Run(output, func(t *testing.T) {
			var received app.Request
			c := recipeCLIService(func(_ context.Context, _ uint32, r app.Request) (any, error) { received = r; return result, nil })
			r, err := recipeCLIExecute(t, context.Background(), c, output, "guest", "recipe", "result", result["operationID"].(string))
			if err != nil || r.Error != nil || c.calls != 1 || c.method != "guest.recipe.result" || !reflect.DeepEqual(r.Data, result) {
				t.Fatal("read-only result changed completion evidence", r, err, c.method)
			}
			want := app.Request{Connection: "qemu:///session", ID: result["operationID"].(string), Input: map[string]any{}}
			if !reflect.DeepEqual(received, want) || !reflect.DeepEqual(c.request, want) {
				t.Fatal("result acquired recipe or apply authority", received, c.request)
			}
		})
	}
}

func TestGuestRecipeCLIFailuresHaveNoSuccessfulPayload(t *testing.T) {
	for _, action := range []string{"run", "result"} {
		for _, failure := range []string{"SOURCE_CHANGED", "PERMISSION_DENIED", "INVALID_INPUT", "RECOVERY_REQUIRED", "canceled", "transport"} {
			t.Run(action+"/"+failure, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				calls := 0
				c := recipeCLIService(func(ctx context.Context, _ uint32, _ app.Request) (any, error) {
					calls++
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					return nil, domain.Fail(failure, "guest recipe fixture refused")
				})
				want := failure
				if failure == "canceled" {
					cancel()
					want = "OPERATION_FAILED"
				}
				if failure == "transport" {
					c.transportError = errors.New("guest recipe transport unavailable")
					want = "OPERATION_FAILED"
				}
				args := []string{"guest", "recipe", action, recipeCLIVM}
				if action == "run" {
					args = append(args, recipeCLIPath, "--input", recipeCLIInput)
				}
				r, err := recipeCLIExecute(t, ctx, c, "json", args...)
				if err == nil || r.Error == nil || r.Error.Code != want || r.Data != nil || c.calls != 1 || c.method != "guest.recipe."+action || c.request.Apply != nil {
					t.Fatal("refusal hidden or success payload/authority retained", r, err, c.request)
				}
				if calls != 1 && failure != "transport" || calls != 0 && failure == "transport" {
					t.Fatal("failure retried service", calls)
				}
			})
		}
	}
}
