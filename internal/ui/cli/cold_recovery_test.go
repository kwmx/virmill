package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

// This is a UI transport seam. The fixed service responses exercise rendering
// and approval transfer, not capture execution or native restore qualification.
type coldCLIClient struct {
	methods  []string
	requests []app.Request
	response app.Response
	err      error
}

func (c *coldCLIClient) Call(ctx context.Context, method string, r app.Request) (app.Response, error) {
	c.methods = append(c.methods, method)
	c.requests = append(c.requests, r)
	if err := ctx.Err(); err != nil {
		return app.Response{}, err
	}
	return c.response, c.err
}

func coldCLIPlan(t *testing.T, action string) domain.Plan {
	t.Helper()
	p := domain.Plan{APIVersion: domain.APIVersion, ID: "860223f8-fbb4-4be7-a053-71a49818e768", CreatedAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), ActorUID: 1000, ConnectionID: "qemu:///session", Operation: "snapshot." + action,
		ResourceIDs:      []string{"libvirt|qemu:///session|vm|12345678-1234-1234-1234-123456789abc"},
		Acknowledgements: []string{"offline-source-read", "private-recovery-state"},
		Review:           map[string]any{"snapshotID": "a349c6aa-42fa-4931-99ab-091c315a7c6e", "catalog": "/private/recovery sets", "guestBootVerified": false, "sourceMutation": "none"}}
	if action == "restore" {
		p.Acknowledgements = []string{"all-network-interfaces-disconnected", "new-restored-identity"}
		p.Review = map[string]any{"snapshotID": "a349c6aa-42fa-4931-99ab-091c315a7c6e", "newName": "Restored ضيف", "newVMID": "12345678-1234-1234-1234-123456789abc", "disconnectAllNICs": true, "guestBootVerified": false}
	}
	var err error
	p.Digest, err = operations.PlanDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func coldCLIExecute(t *testing.T, ctx context.Context, c *coldCLIClient, output string, args ...string) (app.Response, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	cmd := New(c, &out, &stderr)
	cmd.SetIn(forbiddenPrompt{})
	cmd.SetArgs(append(args, "--output", output, "--connection", "qemu:///session", "--non-interactive"))
	err := cmd.ExecuteContext(ctx)
	if stderr.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %q", stderr.String())
	}
	var r app.Response
	if decode := json.Unmarshal(out.Bytes(), &r); decode != nil {
		t.Fatalf("unclean response %q: %v", out.String(), decode)
	}
	if output == "ndjson" && strings.Count(out.String(), "\n") != 1 {
		t.Fatal("not one bounded NDJSON envelope")
	}
	return r, err
}

func TestColdRecoveryCLIExactPreviewAndExplicitApply(t *testing.T) {
	for _, output := range []string{"json", "ndjson"} {
		for _, action := range []string{"create", "restore"} {
			t.Run(output+"/"+action, func(t *testing.T) {
				p := coldCLIPlan(t, action)
				c := &coldCLIClient{response: app.Response{APIVersion: domain.APIVersion, Data: p, Warnings: []string{"fixture: no native execution"}}}
				before, _ := operations.Canonical(c.response)
				input := `{"sourceRoot":"/private/source images","auxiliaryRootID":"uefi-tpm"}`
				if action == "restore" {
					input = `{"name":"Restored ضيف","poolID":"a349c6aa-42fa-4931-99ab-091c315a7c6e"}`
				}
				for _, explicit := range []bool{false, true} {
					args := []string{"snapshot", action, "12345678-1234-1234-1234-123456789abc", "--input", input}
					if explicit {
						args = append(args, "--plan")
					}
					r, err := coldCLIExecute(t, context.Background(), c, output, args...)
					if err != nil {
						t.Fatal(err)
					}
					raw, _ := operations.Canonical(r)
					if !bytes.Equal(raw, before) {
						t.Fatal("CLI changed service preview", string(raw))
					}
					var wanted map[string]any
					_ = json.Unmarshal([]byte(input), &wanted)
					q := c.requests[len(c.requests)-1]
					if q.ID != "12345678-1234-1234-1234-123456789abc" || q.Action != action || q.Path != "" || q.Apply != nil || q.Connection != "qemu:///session" || !reflect.DeepEqual(q.Input, wanted) {
						t.Fatal("preview changed input or acquired authority", q)
					}
				}
				if !reflect.DeepEqual(c.methods, []string{"snapshot." + action, "snapshot." + action}) {
					t.Fatal("hidden dispatch", c.methods)
				}
				args := []string{"plan", "apply", p.ID, "--digest", p.Digest, "--idempotency-key", "cold-ui-reviewed"}
				for _, ack := range p.Acknowledgements {
					args = append(args, "--ack", ack)
				}
				c.response = app.Response{APIVersion: domain.APIVersion, Error: domain.Fail("STALE_PLAN", "source changed; no restore submitted"), Warnings: []string{}}
				r, err := coldCLIExecute(t, context.Background(), c, output, args...)
				q := c.requests[len(c.requests)-1]
				if err == nil || r.Error == nil || r.Error.Code != "STALE_PLAN" || r.Data != nil || c.methods[len(c.methods)-1] != "operation.apply" || q.Apply == nil {
					t.Fatal("apply refusal hidden", r, err)
				}
				if q.Apply.PlanID != p.ID || q.Apply.PlanDigest != p.Digest || q.Apply.IdempotencyKey != "cold-ui-reviewed" || !reflect.DeepEqual(q.Apply.Acknowledgements, p.Acknowledgements) || q.ID != "" || len(q.Input) != 0 {
					t.Fatal("approval changed immutable plan", q)
				}
			})
		}
	}
}

func TestColdRecoveryCLIReadOnlyMappingsAndFailures(t *testing.T) {
	for _, action := range []string{"list", "show"} {
		t.Run(action, func(t *testing.T) {
			c := &coldCLIClient{response: app.Response{APIVersion: domain.APIVersion, Data: map[string]any{"snapshotID": "a349c6aa-42fa-4931-99ab-091c315a7c6e", "guestBootVerified": false}, Warnings: []string{}}}
			args := []string{"snapshot", action}
			id := ""
			if action == "show" {
				id = "a349c6aa-42fa-4931-99ab-091c315a7c6e"
				args = append(args, id)
			}
			if _, err := coldCLIExecute(t, context.Background(), c, "json", args...); err != nil {
				t.Fatal(err)
			}
			if len(c.requests) != 1 || c.methods[0] != "snapshot."+action || c.requests[0].ID != id || c.requests[0].Action != "" || c.requests[0].Apply != nil {
				t.Fatal("read-only action mapping changed", c.requests)
			}
		})
	}
	for _, failure := range []string{"canceled", "disconnected", "incomplete"} {
		t.Run(failure, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := &coldCLIClient{}
			want := "OPERATION_FAILED"
			if failure == "canceled" {
				cancel()
			} else if failure == "disconnected" {
				c.err = errors.New("cold fixture disconnected")
			} else {
				want = "INCOMPLETE_BACKUP"
				c.response = app.Response{APIVersion: domain.APIVersion, Error: domain.Fail(want, "required member missing")}
			}
			r, err := coldCLIExecute(t, ctx, c, "json", "snapshot", "show", "a349c6aa-42fa-4931-99ab-091c315a7c6e")
			if err == nil || r.Error == nil || r.Error.Code != want || r.Data != nil || len(c.requests) != 1 || c.requests[0].Apply != nil {
				t.Fatal("failure gained authority or lost error", r, err)
			}
		})
	}
}

func TestColdRecoveryCLIRefusesImplicitApplyAndMalformedInput(t *testing.T) {
	for _, action := range []string{"create", "restore"} {
		for _, suffix := range [][]string{{"--plan=false"}, {"--input", `{"sourceRoot":"/a","sourceRoot":"/b"}`}, {"--input", `[]`}} {
			t.Run(action+strings.Join(suffix, " "), func(t *testing.T) {
				c := &coldCLIClient{}
				var out bytes.Buffer
				cmd := New(c, &out, &out)
				cmd.SetArgs(append([]string{"snapshot", action, "12345678-1234-1234-1234-123456789abc"}, suffix...))
				if err := cmd.Execute(); err == nil || len(c.requests) != 0 {
					t.Fatal("invalid preview reached service", c.requests, err)
				}
			})
		}
	}
}
