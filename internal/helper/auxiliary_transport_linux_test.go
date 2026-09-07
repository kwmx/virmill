//go:build linux && amd64

package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/domain"
)

// Private socket tests exercise typed decoding after peer authentication. They
// do not invoke the privileged endpoint or claim real native observations.
func auxiliaryTransportFixture(t *testing.T) (Request, Response) {
	t.Helper()
	r, _, _, _ := auxiliaryAuthFixture(t, "capture")
	inventory := r.Auxiliary.Expected
	r.Mode, r.Auxiliary.Expected = "inspect", nil
	binding, err := AuxiliaryBinding(r)
	if err != nil {
		t.Fatal(err)
	}
	return r, Response{APIVersion: domain.APIVersion, Success: true, Auxiliary: &AuxiliaryResponse{Version: 1, JobID: r.JobID, Binding: binding, Stage: "inspected", Inventory: inventory}}
}

func auxiliaryTransportJSON(t *testing.T, response Response) []byte {
	t.Helper()
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func auxiliaryTransportEmpty(t *testing.T, result AuxiliaryResponse, err error) {
	t.Helper()
	if err == nil || !reflect.DeepEqual(result, AuxiliaryResponse{}) {
		t.Fatalf("rejected inspection returned successful/partial proof: result=%#v err=%v", result, err)
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		t.Fatalf("fixture was rejected only by timeout: %v", err)
	}
}

func auxiliaryTransportExchange(t *testing.T, r Request, payload []byte, rights []int) (AuxiliaryResponse, error) {
	t.Helper()
	a, b := sealedPair(t)
	baseline := sealedFDCount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_ = a.SetWriteDeadline(time.Now().Add(2 * time.Second))
		var control []byte
		if len(rights) > 0 {
			control = unix.UnixRights(rights...)
		}
		n, on, err := a.WriteMsgUnix(payload, control, nil)
		if err == nil && on != len(control) {
			err = io.ErrShortWrite
		}
		for err == nil && n < len(payload) {
			var wrote int
			wrote, err = a.Write(payload[n:])
			n += wrote
			if err == nil && wrote == 0 {
				err = io.ErrShortWrite
			}
		}
		if err == nil {
			err = a.CloseWrite()
		}
		done <- err
	}()
	result, err := receiveAuxiliaryInspection(ctx, b, r)
	if sendErr := <-done; sendErr != nil {
		t.Fatalf("fixture did not fully enter the private socket: %v", sendErr)
	}
	if after := sealedFDCount(t); after != baseline {
		t.Fatalf("inspection leaked received descriptor: before=%d after=%d", baseline, after)
	}
	return result, err
}

func TestAuxiliaryTransportMetadataOnlySuccessAndFragmentation(t *testing.T) {
	for _, chunk := range []int{0, 1, 17, 4096} {
		t.Run("chunk-"+strconv.Itoa(chunk), func(t *testing.T) {
			r, response := auxiliaryTransportFixture(t)
			payload := auxiliaryTransportJSON(t, response)
			if chunk == 0 {
				got, err := auxiliaryTransportExchange(t, r, payload, nil)
				if err != nil || !reflect.DeepEqual(got, *response.Auxiliary) {
					t.Fatalf("valid metadata-only response changed: %#v %v", got, err)
				}
				return
			}
			a, b := sealedPair(t)
			baseline := sealedFDCount(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_ = a.SetWriteDeadline(time.Now().Add(2 * time.Second))
				for start := 0; start < len(payload); start += chunk {
					end := min(start+chunk, len(payload))
					if n, err := a.Write(payload[start:end]); err != nil || n != end-start {
						if err == nil {
							err = io.ErrShortWrite
						}
						done <- err
						return
					}
				}
				done <- nil
			}()
			got, err := receiveAuxiliaryInspection(ctx, b, r)
			if sendErr := <-done; sendErr != nil {
				t.Fatal(sendErr)
			}
			if err != nil || !reflect.DeepEqual(got, *response.Auxiliary) {
				t.Fatalf("fragmented metadata response changed: %#v %v", got, err)
			}
			if after := sealedFDCount(t); after != baseline {
				t.Fatal("metadata-only receive changed descriptor ownership")
			}
		})
	}
}

func TestAuxiliaryTransportRejectsTypedConfusion(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Response)
	}{
		{"outer version", func(r *Response) { r.APIVersion = "virmill/v2" }},
		{"missing auxiliary", func(r *Response) { r.Auxiliary = nil }},
		{"version", func(r *Response) { r.Auxiliary.Version = 2 }},
		{"job", func(r *Response) { r.Auxiliary.JobID = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee" }},
		{"binding", func(r *Response) { r.Auxiliary.Binding = strings.Repeat("f", 64) }},
		{"capture stage", func(r *Response) { r.Auxiliary.Stage = "captured" }},
		{"artifact", func(r *Response) { r.Auxiliary.Artifact = &AuxiliaryArtifact{} }},
		{"missing inventory", func(r *Response) { r.Auxiliary.Inventory = nil }},
		{"inventory version", func(r *Response) { r.Auxiliary.Inventory.Version = 2 }},
		{"resource provider", func(r *Response) { r.Auxiliary.Inventory.Resource.ProviderID = "other" }},
		{"resource connection", func(r *Response) { r.Auxiliary.Inventory.Resource.ConnectionID = "qemu:///session" }},
		{"resource kind", func(r *Response) { r.Auxiliary.Inventory.Resource.Kind = "storage-pool" }},
		{"resource UUID", func(r *Response) { r.Auxiliary.Inventory.Resource.UUID = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee" }},
		{"layout UUID", func(r *Response) { r.Auxiliary.Inventory.Layout.VMID = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee" }},
		{"fingerprint", func(r *Response) { r.Auxiliary.Inventory.Fingerprint = strings.Repeat("f", 64) }},
		{"root ID", func(r *Response) { r.Auxiliary.Inventory.Root.ID = "other" }},
		{"relative root", func(r *Response) { r.Auxiliary.Inventory.Root.Path = "relative" }},
		{"root alias", func(r *Response) { r.Auxiliary.Inventory.Root.Path = "/approved/../other" }},
		{"root only", func(r *Response) { r.Auxiliary.Inventory.Root.Path = "/" }},
		{"nil members", func(r *Response) { r.Auxiliary.Inventory.Members = nil }},
		{"nil directories", func(r *Response) { r.Auxiliary.Inventory.Directories = nil }},
		{"empty members", func(r *Response) { r.Auxiliary.Inventory.Members = []AuxiliaryMember{} }},
		{"too many members", func(r *Response) { r.Auxiliary.Inventory.Members = make([]AuxiliaryMember, MaxAuxiliaryMembers+1) }},
		{"too many directories", func(r *Response) { r.Auxiliary.Inventory.Directories = make([]AuxiliaryDirectory, 129) }},
		{"false total", func(r *Response) { r.Auxiliary.Inventory.TotalBytes-- }},
		{"oversized total", func(r *Response) { r.Auxiliary.Inventory.TotalBytes = MaxAuxiliaryPayloadBytes + 1 }},
		{"overflowing members", func(r *Response) {
			r.Auxiliary.Inventory.Members[0].State.Size = ^uint64(0)
			r.Auxiliary.Inventory.Members[1].State.Size = 1
			r.Auxiliary.Inventory.TotalBytes = 0
		}},
		{"success with error", func(r *Response) { r.Error = "fixture error" }},
		{"success with error code", func(r *Response) { r.ErrorCode = "PERMISSION_DENIED" }},
		{"success with access", func(r *Response) { r.Access = json.RawMessage(`{}`) }},
		{"success with access null", func(r *Response) { r.Access = json.RawMessage(`null`) }},
		{"error with auxiliary", func(r *Response) { r.Success = false; r.Error = "fixture error" }},
		{"error with access", func(r *Response) {
			r.Success = false
			r.Error = "fixture error"
			r.Auxiliary = nil
			r.Access = json.RawMessage(`{}`)
		}},
		{"empty error", func(r *Response) { r.Success = false; r.Auxiliary = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, response := auxiliaryTransportFixture(t)
			tc.change(&response)
			got, err := auxiliaryTransportExchange(t, r, auxiliaryTransportJSON(t, response), nil)
			auxiliaryTransportEmpty(t, got, err)
		})
	}
}

func TestAuxiliaryTransportRejectsRootNUL(t *testing.T) {
	r, response := auxiliaryTransportFixture(t)
	response.Auxiliary.Inventory.Root.Path = "/approved\x00other"
	got, err := auxiliaryTransportExchange(t, r, auxiliaryTransportJSON(t, response), nil)
	auxiliaryTransportEmpty(t, got, err)
}

func TestAuxiliaryTransportRejectsRootControlsAndLength(t *testing.T) {
	for name, path := range map[string]string{
		"newline": "/approved\nother", "tab": "/approved\tother", "delete": "/approved\x7fother",
		"bidi format": "/approved\u202eother", "zero-width format": "/approved\u200bother",
		"backslash": "/approved\\other", "overlong": "/" + strings.Repeat("x", 4096),
	} {
		t.Run(name, func(t *testing.T) {
			r, response := auxiliaryTransportFixture(t)
			response.Auxiliary.Inventory.Root.Path = path
			got, err := auxiliaryTransportExchange(t, r, auxiliaryTransportJSON(t, response), nil)
			auxiliaryTransportEmpty(t, got, err)
		})
	}
}

func TestAuxiliaryTransportRejectsChangedRequestContext(t *testing.T) {
	for name, change := range map[string]func(*Request){
		"actor":                      func(r *Request) { r.ActorUID++ },
		"key":                        func(r *Request) { r.KeyID = "other" },
		"plan":                       func(r *Request) { r.PlanDigest = strings.Repeat("f", 64) },
		"job":                        func(r *Request) { r.JobID = "bbbbbbbb-bbbb-4ccc-8ddd-eeeeeeeeeeee" },
		"operation":                  func(r *Request) { r.Operation = "storage.prepare-directory" },
		"capture mode":               func(r *Request) { r.Mode = "capture" },
		"ACL payload":                func(r *Request) { r.Access = &AccessRequest{} },
		"missing auxiliary":          func(r *Request) { r.Auxiliary = nil },
		"future auxiliary":           func(r *Request) { r.Auxiliary.Version = 2 },
		"expected capture inventory": func(r *Request) { r.Auxiliary.Expected = &AuxiliaryInventory{} },
	} {
		t.Run(name, func(t *testing.T) {
			r, response := auxiliaryTransportFixture(t)
			change(&r)
			got, err := auxiliaryTransportExchange(t, r, auxiliaryTransportJSON(t, response), nil)
			auxiliaryTransportEmpty(t, got, err)
		})
	}
}

func TestAuxiliaryTransportErrorMapping(t *testing.T) {
	for _, code := range []string{"PERMISSION_DENIED", "SOURCE_CHANGED", "UNSUPPORTED_CAPABILITY", "INVALID_INPUT", "INVALID_STATE", "OPERATION_FAILED", "STALE_PLAN", "INCOMPLETE_BACKUP", "", "SUCCEEDED", "arbitrary-fixture-code"} {
		t.Run("code-"+code, func(t *testing.T) {
			r, _ := auxiliaryTransportFixture(t)
			response := Response{APIVersion: domain.APIVersion, Error: "bounded fixture refusal", ErrorCode: code}
			got, err := auxiliaryTransportExchange(t, r, auxiliaryTransportJSON(t, response), nil)
			auxiliaryTransportEmpty(t, got, err)
			want := code
			if want == "" || want == "SUCCEEDED" || want == "arbitrary-fixture-code" {
				want = "OPERATION_FAILED"
			}
			var failure *domain.Error
			if !errors.As(err, &failure) || failure.Code != want || failure.Message != response.Error {
				t.Fatalf("wrong typed refusal: %v", err)
			}
		})
	}
}

func TestAuxiliaryTransportRejectsMalformedAndOversizedFrames(t *testing.T) {
	r, response := auxiliaryTransportFixture(t)
	valid := auxiliaryTransportJSON(t, response)
	cases := map[string][]byte{
		"empty frame":                []byte("\n"),
		"truncated JSON":             []byte("{\n"),
		"duplicate key":              append([]byte("{\"success\":true,"), valid[1:]...),
		"escaped duplicate":          append([]byte("{\"succ\\u0065ss\":true,"), valid[1:]...),
		"nested duplicate":           bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		"invalid UTF8":               bytes.Replace(valid, []byte(response.Auxiliary.Inventory.Root.Path), []byte{'/', 'b', 0xff}, 1),
		"unknown field":              append([]byte("{\"unexpected\":1,"), valid[1:]...),
		"wrong type":                 []byte("{\"apiVersion\":7,\"success\":true}\n"),
		"two frames":                 append(append([]byte{}, valid...), valid...),
		"trailing value":             []byte("{} {}\n"),
		"no delimiter EOF":           bytes.TrimSuffix(valid, []byte{'\n'}),
		"oversized JSON":             []byte("{\"x\":\"" + strings.Repeat("x", maxSnapshotEnvelope) + "\"}\n"),
		"oversized before delimiter": bytes.Repeat([]byte{'x'}, maxSnapshotEnvelope+2),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := auxiliaryTransportExchange(t, r, data, nil)
			auxiliaryTransportEmpty(t, got, err)
		})
	}
}

func TestAuxiliaryTransportRejectsDescriptorsWithoutLeaks(t *testing.T) {
	for _, kind := range []string{"sealed", "unsealed", "multiple", "truncated-control", "sealed-on-error", "sealed-malformed", "sealed-oversized"} {
		t.Run(kind, func(t *testing.T) {
			r, response := auxiliaryTransportFixture(t)
			file := sealedFixture(t)
			rights := []int{int(file.Fd())}
			if kind == "unsealed" {
				fd, err := unix.MemfdCreate("original-unsealed-transport-fixture", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
				if err != nil {
					t.Fatal(err)
				}
				defer unix.Close(fd)
				if _, err := unix.Write(fd, []byte("original fixture bytes")); err != nil {
					t.Fatal(err)
				}
				rights = []int{fd}
			}
			if kind == "multiple" {
				rights = append(rights, int(file.Fd()))
			}
			if kind == "truncated-control" {
				rights = make([]int, 32)
				for i := range rights {
					rights[i] = int(file.Fd())
				}
			}
			if kind == "sealed-on-error" {
				response = Response{APIVersion: domain.APIVersion, Error: "fixture refusal", ErrorCode: "PERMISSION_DENIED"}
			}
			payload := auxiliaryTransportJSON(t, response)
			if kind == "sealed-malformed" {
				payload = []byte("{\"x\":1,\"x\":2}\n")
			}
			if kind == "sealed-oversized" {
				payload = []byte("{\"x\":\"" + strings.Repeat("x", maxSnapshotEnvelope) + "\"}\n")
			}
			got, err := auxiliaryTransportExchange(t, r, payload, rights)
			auxiliaryTransportEmpty(t, got, err)
		})
	}
}

func TestAuxiliaryTransportCancellationClosesArrivedDescriptor(t *testing.T) {
	r, _ := auxiliaryTransportFixture(t)
	file := sealedFixture(t)
	a, b := sealedPair(t)
	baseline := sealedFDCount(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		response AuxiliaryResponse
		err      error
	}
	done := make(chan outcome, 1)
	go func() { response, err := receiveAuxiliaryInspection(ctx, b, r); done <- outcome{response, err} }()
	if n, _, err := a.WriteMsgUnix([]byte(`{"apiVersion":`), unix.UnixRights(int(file.Fd())), nil); err != nil || n != 14 {
		t.Fatalf("partial descriptor response: n=%d err=%v", n, err)
	}
	deadline := time.Now().Add(time.Second)
	for sealedFDCount(t) == baseline && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if current := sealedFDCount(t); current != baseline+1 {
		t.Fatalf("fixture did not install one descriptor: before=%d now=%d", baseline, current)
	}
	cancel()
	select {
	case got := <-done:
		if got.err == nil || !reflect.DeepEqual(got.response, AuxiliaryResponse{}) {
			t.Fatalf("cancellation returned proof: %#v %v", got.response, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt active typed receive")
	}
	if after := sealedFDCount(t); after != baseline {
		t.Fatalf("cancellation leaked descriptor: before=%d after=%d", baseline, after)
	}
}

func TestAuxiliaryTransportLegacySealedReceiverStillRequiresDescriptor(t *testing.T) {
	r, response := auxiliaryTransportFixture(t)
	payload := auxiliaryTransportJSON(t, response)
	// This exact frame succeeds through the typed metadata decoder.
	if _, err := auxiliaryTransportExchange(t, r, payload, nil); err != nil {
		t.Fatal(err)
	}
	a, b := sealedPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if n, err := a.Write(payload); err != nil || n != len(payload) {
		t.Fatalf("metadata fixture send: n=%d err=%v", n, err)
	}
	frame, file, err := ReceiveSealedSnapshot(ctx, b)
	if file != nil {
		_ = file.Close()
	}
	if err == nil || file != nil || frame != nil {
		t.Fatalf("old sealed transport accepted metadata without FD: %q %v %v", frame, file, err)
	}
}
