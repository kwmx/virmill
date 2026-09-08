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
)

// Only the native observer is substituted; the registered CLI and shared
// service run normally, with no Engine or job authority in this fixture.
type readinessCLIObserver struct {
	domain.ComputeProvider
	result         domain.GuestReadiness
	err            error
	connection, id string
	calls          int
}

func (o *readinessCLIObserver) InspectGuestReadiness(ctx context.Context, connection, id string) (domain.GuestReadiness, error) {
	o.calls++
	o.connection, o.id = connection, id
	if err := ctx.Err(); err != nil {
		return domain.GuestReadiness{}, err
	}
	if o.err != nil {
		return domain.GuestReadiness{}, o.err
	}
	return o.result, nil
}

func TestGuestReadinessCLIExactObservationAndNoApplicationClaim(t *testing.T) {
	for _, connection := range []string{"qemu:///session", "qemu:///system"} {
		for _, state := range []string{"responsive", "unresponsive", "disconnected", "absent", "unknown"} {
			t.Run(connection+"/"+state, func(t *testing.T) {
				id := "12345678-1234-1234-1234-123456789abc"
				o := &readinessCLIObserver{result: domain.GuestReadiness{Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: connection, Kind: "vm", UUID: id}, State: state, AgentConnected: state == "responsive" || state == "unresponsive", AgentResponsive: state == "responsive", Evidence: "fixture-observation-" + state, ObservedAt: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}}
				client := &recoveryCLIServiceClient{service: app.Service{Provider: o}}
				for _, output := range []string{"json", "ndjson"} {
					var out, stderr bytes.Buffer
					cmd := New(client, &out, &stderr)
					cmd.SetIn(forbiddenPrompt{})
					cmd.SetArgs([]string{"vm", "readiness", "show", id, "--connection", connection, "--output", output, "--non-interactive"})
					if err := cmd.Execute(); err != nil {
						t.Fatal(err)
					}
					var r struct {
						APIVersion string                `json:"apiVersion"`
						Data       domain.GuestReadiness `json:"data"`
						Warnings   []string              `json:"warnings"`
						Error      *domain.Error         `json:"error"`
					}
					if err := json.Unmarshal(out.Bytes(), &r); err != nil || r.Error != nil || !reflect.DeepEqual(r.Data, o.result) {
						t.Fatal("readiness observation changed", out.String(), err)
					}
					if stderr.Len() != 0 || output == "ndjson" && strings.Count(out.String(), "\n") != 1 {
						t.Fatal("unclean machine output")
					}
					if client.method != "vm.readiness.show" || client.request.ID != id || client.request.Connection != connection || client.request.Action != "" || client.request.Apply != nil || len(client.request.Input) != 0 || o.id != id || o.connection != connection {
						t.Fatal("observation changed selection or acquired authority", client.request)
					}
					for _, invented := range []string{"applicationReady", "guestBootVerified", "operationID", "planDigest", "ipAddress"} {
						if strings.Contains(out.String(), invented) {
							t.Fatal("readiness invented stronger evidence", invented)
						}
					}
				}
				if o.calls != 2 || client.calls != 2 {
					t.Fatal("hidden observer call", o.calls, client.calls)
				}
			})
		}
	}
}

func TestGuestReadinessCLIErrorsHaveNoObservationOrAuthority(t *testing.T) {
	for _, fault := range []string{"native", "canceled", "transport"} {
		t.Run(fault, func(t *testing.T) {
			o := &readinessCLIObserver{}
			client := &recoveryCLIServiceClient{service: app.Service{Provider: o}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			code := "OPERATION_FAILED"
			switch fault {
			case "native":
				code = "SOURCE_CHANGED"
				o.err = domain.Fail(code, "VM runtime identity changed")
			case "canceled":
				cancel()
			case "transport":
				client.transportError = errors.New("readiness transport canceled")
			}
			var out bytes.Buffer
			cmd := New(client, &out, &out)
			cmd.SetArgs([]string{"vm", "readiness", "show", "12345678-1234-1234-1234-123456789abc", "--output", "json", "--non-interactive"})
			err := cmd.ExecuteContext(ctx)
			var r app.Response
			if e := json.Unmarshal(out.Bytes(), &r); e != nil || err == nil || r.Error == nil || r.Error.Code != code {
				t.Fatal("failure hidden", out.String(), err, e)
			}
			// Shared errors can carry a zero typed observation, never a usable one.
			if r.Data != nil {
				allocationCLIEmptyErrorData[domain.GuestReadiness](t, r.Data)
			}
			if client.calls != 1 || client.request.Apply != nil || client.request.Action != "" {
				t.Fatal("failure created job authority")
			}
		})
	}
}
