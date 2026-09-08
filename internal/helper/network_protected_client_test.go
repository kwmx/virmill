//go:build linux && amd64

package helper

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/domain"
)

// Generated metadata and private AF_UNIX connections exercise the decoder after
// its production root-peer authentication boundary. No helper endpoint, private
// key file, privileged executor, firewall command or native backend is invoked.
func protectedClientFixture(version int, mode string) (Request, Response) {
	id := "12345678-1234-4234-8234-123456789abc"
	d := domain.NetworkDefinition{UUID: id, Name: "virmill-" + id, Bridge: "vm123456781234", Type: "lab", IPv4CIDR: "192.168.240.0/24", IPv6Mode: "disabled", HostAccess: "services-only", Egress: "none"}
	if version == 1 {
		d.HostAccess = "allow"
	}
	r := Request{APIVersion: domain.APIVersion, ActorUID: uint32(os.Getuid()), Operation: networkOperation(version), ResourceID: id, PlanDigest: strings.Repeat("a", 64), JobID: "87654321-1234-4234-8234-123456789abc", Mode: mode, Network: &NetworkRequest{Version: version, Definition: d}}
	response := Response{APIVersion: domain.APIVersion, Success: true, Network: &NetworkResponse{Version: version, ResourceID: id, Bridge: d.Bridge, PlanDigest: r.PlanDigest, JobID: r.JobID, RuntimePresent: mode != "check", PermanentPresent: mode != "check"}}
	return r, response
}

func protectedClientJSON(t *testing.T, response Response) []byte {
	t.Helper()
	value, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return append(value, '\n')
}

func protectedClientExchange(t *testing.T, r Request, frame []byte, rights []int) (NetworkResponse, error) {
	t.Helper()
	a, b := sealedPair(t)
	baseline := sealedFDCount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_ = a.SetWriteDeadline(time.Now().Add(time.Second))
		var control []byte
		if len(rights) != 0 {
			control = unix.UnixRights(rights...)
		}
		n, on, err := a.WriteMsgUnix(frame, control, nil)
		if err == nil && (n != len(frame) || on != len(control)) {
			err = io.ErrShortWrite
		}
		done <- err
	}()
	out, err := receiveNetworkFilter(ctx, b, r)
	if sent := <-done; sent != nil {
		t.Fatal("generated frame did not enter private transport", sent)
	}
	if current := sealedFDCount(t); current != baseline {
		t.Fatalf("received network response leaked descriptor: before=%d after=%d", baseline, current)
	}
	return out, err
}

func protectedClientRejected(t *testing.T, out NetworkResponse, err error) {
	t.Helper()
	if err == nil || out != (NetworkResponse{}) {
		t.Fatalf("failure returned complete/partial policy proof: %+v %v", out, err)
	}
}

func TestProtectedClientRejectsConfusedFamiliesBeforeKeyOrSocketAccess(t *testing.T) {
	for _, change := range []func(*Request){
		func(r *Request) { r.Network = nil },
		func(r *Request) { r.Network.Version = 1 },
		func(r *Request) { r.Network.Version = 3 },
		func(r *Request) { r.Operation = "network.ipv6-filter" },
		func(r *Request) { r.Operation = "storage.prepare-directory" },
		func(r *Request) { r.Access = &AccessRequest{} },
		func(r *Request) { r.Auxiliary = &AuxiliaryRequest{} },
		func(r *Request) { r.Network.Definition.HostAccess = "allow" },
	} {
		r, _ := protectedClientFixture(2, "apply")
		change(&r)
		out, err := (Client{KeyPath: "must-not-be-opened"}).NetworkFilter(context.Background(), r)
		protectedClientRejected(t, out, err)
		var typed *domain.Error
		if !errors.As(err, &typed) || typed.Code != "INVALID_INPUT" {
			t.Fatal("confused family reached key lookup or another client boundary", err)
		}
	}
	r, _ := protectedClientFixture(2, "apply")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := (Client{KeyPath: "must-not-be-opened"}).NetworkFilter(ctx, r)
	protectedClientRejected(t, out, err)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("canceled client reached key/socket access", err)
	}
	if _, err = (Client{KeyPath: "must-not-be-opened"}).Call(context.Background(), r); err == nil {
		t.Fatal("protected request entered legacy access client")
	}
}

func TestProtectedClientMetadataOnlyVersionAndModeBindings(t *testing.T) {
	for _, version := range []int{1, 2} {
		for _, mode := range []string{"check", "apply", "observe"} {
			r, response := protectedClientFixture(version, mode)
			out, err := protectedClientExchange(t, r, protectedClientJSON(t, response), nil)
			if err != nil || out != *response.Network || out.PacketVerified {
				t.Fatalf("valid version %d mode %s response changed or overstated proof: %+v %v", version, mode, out, err)
			}
		}
	}
}

func TestProtectedClientRejectsResponseConfusionAndInventedProof(t *testing.T) {
	cases := map[string]func(*Response){
		"outer-version":     func(r *Response) { r.APIVersion = "virmill/v2" },
		"missing-network":   func(r *Response) { r.Network = nil },
		"version-downgrade": func(r *Response) { r.Network.Version = 1 },
		"resource":          func(r *Response) { r.Network.ResourceID = domain.ID() },
		"bridge":            func(r *Response) { r.Network.Bridge = "ens3" },
		"plan":              func(r *Response) { r.Network.PlanDigest = strings.Repeat("b", 64) },
		"job":               func(r *Response) { r.Network.JobID = domain.ID() },
		"packet-proof":      func(r *Response) { r.Network.PacketVerified = true },
		"no-runtime":        func(r *Response) { r.Network.RuntimePresent = false },
		"no-permanent":      func(r *Response) { r.Network.PermanentPresent = false },
		"access-payload":    func(r *Response) { r.Access = json.RawMessage(`{}`) },
		"auxiliary-payload": func(r *Response) { r.Auxiliary = &AuxiliaryResponse{} },
		"success-and-error": func(r *Response) { r.Error = "generated refusal" },
		"success-and-code":  func(r *Response) { r.ErrorCode = "PERMISSION_DENIED" },
		"error-and-proof":   func(r *Response) { r.Success = false; r.Error = "generated refusal" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r, response := protectedClientFixture(2, "apply")
			change(&response)
			out, err := protectedClientExchange(t, r, protectedClientJSON(t, response), nil)
			protectedClientRejected(t, out, err)
			var typed *domain.Error
			if !errors.As(err, &typed) || typed.Code != "RECOVERY_REQUIRED" {
				t.Fatal("response confusion lacked reconciliation disposition", err)
			}
		})
	}
}

func TestProtectedClientMalformedFramesAndKnownErrors(t *testing.T) {
	r, response := protectedClientFixture(2, "observe")
	raw := protectedClientJSON(t, response)
	for _, frame := range [][]byte{[]byte("{\n"), []byte(`{"apiVersion":"virmill/v1","success":true,"success":false}` + "\n"),
		[]byte(strings.Replace(string(raw), `"success":true`, `"success":true,"unknown":true`, 1)),
		append(append([]byte{}, raw...), raw...)} {
		out, err := protectedClientExchange(t, r, frame, nil)
		protectedClientRejected(t, out, err)
	}
	for _, code := range []string{"PERMISSION_DENIED", "SOURCE_CHANGED", "STALE_PLAN", "UNSUPPORTED_CAPABILITY", "INVALID_INPUT", "INVALID_STATE", "RECOVERY_REQUIRED", "OPERATION_FAILED", "unrecognized-code"} {
		response = Response{APIVersion: domain.APIVersion, Error: "generated helper refusal", ErrorCode: code}
		out, err := protectedClientExchange(t, r, protectedClientJSON(t, response), nil)
		protectedClientRejected(t, out, err)
		want := code
		if code == "unrecognized-code" {
			want = "OPERATION_FAILED"
		}
		var typed *domain.Error
		if !errors.As(err, &typed) || typed.Code != want {
			t.Fatal("helper refusal code changed", code, err)
		}
	}
}

func TestProtectedClientRejectsEveryReceivedDescriptorWithoutLeaks(t *testing.T) {
	sealed := sealedFixture(t)
	unsealed, err := os.CreateTemp(t.TempDir(), "ordinary-generated")
	if err != nil {
		t.Fatal(err)
	}
	defer unsealed.Close()
	r, response := protectedClientFixture(2, "check")
	for _, rights := range [][]int{{int(sealed.Fd())}, {int(unsealed.Fd())}, {int(sealed.Fd()), int(sealed.Fd())}} {
		out, err := protectedClientExchange(t, r, protectedClientJSON(t, response), rights)
		protectedClientRejected(t, out, err)
	}
}

func TestProtectedClientReceiveReplacesAuthenticationDeadlineAndCancelsPromptly(t *testing.T) {
	r, response := protectedClientFixture(2, "check")
	a, b := sealedPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := b.SetReadDeadline(time.Now().Add(5 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	frame := protectedClientJSON(t, response)
	go func() {
		time.Sleep(25 * time.Millisecond)
		for _, part := range [][]byte{frame[:17], frame[17:]} {
			if _, err := a.Write(part); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	out, err := receiveNetworkFilter(ctx, b, r)
	if sent := <-done; sent != nil {
		t.Fatal(sent)
	}
	if err != nil || out != *response.Network {
		t.Fatal("receive retained the expired authentication deadline", out, err)
	}
	ctx, stop := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		out, err = receiveNetworkFilter(ctx, b, r)
		close(finished)
	}()
	stop()
	select {
	case <-finished:
		protectedClientRejected(t, out, err)
	case <-time.After(time.Second):
		t.Fatal("canceled network receive remained blocked")
	}
}

func TestProtectedClientSignedGrantRequiresFreshLifetimeAndExactPolicyTuple(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	r, _ := protectedClientFixture(2, "check")
	r.ActorUID, r.KeyID, r.ExpiresAt = 1000, "generated-only-key", now.Add(5*time.Minute)
	grant := NetworkPermission{ActorUID: r.ActorUID, KeyID: r.KeyID, ResourceID: r.ResourceID}
	p := Policy{APIVersion: domain.APIVersion, Actors: []uint32{1000}, Keys: map[string]string{r.KeyID: hex.EncodeToString(pub)}, ProtectedNetworks: []NetworkPermission{grant}}
	sign := func(value *Request) {
		raw, err := SignedBytes(*value)
		if err != nil {
			t.Fatal(err)
		}
		value.Signature = hex.EncodeToString(ed25519.Sign(key, raw))
	}
	sign(&r)
	if err := Authorize(1000, r, p, now); err != nil {
		t.Fatal(err)
	}
	for _, expires := range []time.Time{now, now.Add(-time.Second), now.Add(15*time.Minute + time.Nanosecond)} {
		copy := r
		copy.ExpiresAt = expires
		sign(&copy)
		if Authorize(1000, copy, p, now) == nil {
			t.Fatal("fresh signature over invalid lifetime authorized policy")
		}
	}
	for _, change := range []func(*NetworkPermission){func(g *NetworkPermission) { g.ActorUID = 0 }, func(g *NetworkPermission) { g.KeyID = "*" }, func(g *NetworkPermission) { g.ResourceID = "*" }} {
		copy := grant
		change(&copy)
		p.ProtectedNetworks = []NetworkPermission{copy}
		if Authorize(1000, r, p, now) == nil {
			t.Fatal("partial/wildcard protected policy tuple authorized action")
		}
	}
}
