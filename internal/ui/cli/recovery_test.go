package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/domain"
)

func TestRecoveryInspectionAndDeclaredVerificationUseSharedService(t *testing.T) {
	for _, tc := range []struct {
		args       []string
		method, id string
	}{
		{[]string{"vm", "recovery", "inspect", "vm-id"}, "vm.recovery.inspect", "vm-id"},
		{[]string{"backup", "verify-manifest", "/tmp/manifest.json", "--input", `{"root":"/tmp/recovery"}`}, "backup.verify-manifest", ""},
	} {
		r := &recorder{}
		var out bytes.Buffer
		c := New(r, &out, &out)
		c.SetArgs(tc.args)
		if err := c.Execute(); err != nil {
			t.Fatal(err)
		}
		if r.method != tc.method || r.request.ID != tc.id || r.request.Action != "" || r.request.Apply != nil {
			t.Fatal("read-only recovery command changed", r)
		}
		if tc.id == "" && (r.request.Path != "/tmp/manifest.json" || r.request.Input["root"] != "/tmp/recovery") {
			t.Fatal("manifest input lost", r)
		}
	}
}

type recoveryCLIServiceClient struct {
	service        app.Service
	calls          int
	method         string
	request        app.Request
	transportError error
}

func (c *recoveryCLIServiceClient) Call(ctx context.Context, method string, request app.Request) (app.Response, error) {
	c.calls++
	c.method, c.request = method, request
	if c.transportError != nil {
		return app.Response{}, c.transportError
	}
	return c.service.Call(ctx, 1000, method, request), nil
}

func recoveryCLIManifest(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	m := protection.Manifest{APIVersion: domain.APIVersion, ID: "cli-backup", VMID: "cli-vm", Mode: "cold", Consistency: "cold-complete", IndependentlyRecoverable: true}
	for _, kind := range []string{"disk", "persistent-xml"} {
		body := []byte("CLI declared " + kind)
		hash := sha256.Sum256(body)
		if err := os.WriteFile(filepath.Join(dir, kind), body, 0600); err != nil {
			t.Fatal(err)
		}
		m.Required = append(m.Required, kind)
		m.Members = append(m.Members, protection.Member{ID: kind, Kind: kind, Path: kind, Size: int64(len(body)), SHA256: hex.EncodeToString(hash[:])})
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(filename, b, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, filename
}

func TestRecoveryCLIManifestRootReachesActualMemberVerification(t *testing.T) {
	dir, filename := recoveryCLIManifest(t)
	input, _ := json.Marshal(map[string]any{"root": dir})
	client := &recoveryCLIServiceClient{}
	for _, verifyMembers := range []bool{false, true} {
		var out bytes.Buffer
		cmd := New(client, &out, &out)
		args := []string{"backup", "verify-manifest", filename, "--output", "json", "--non-interactive", "--connection", "qemu:///session"}
		if verifyMembers {
			args = append(args, "--input", string(input))
		}
		cmd.SetArgs(args)
		err := cmd.Execute()
		var response app.Response
		if decodeErr := json.Unmarshal(out.Bytes(), &response); decodeErr != nil {
			t.Fatal("CLI did not return a clean service envelope", decodeErr)
		}
		if runtime.GOOS != "linux" {
			if err == nil || response.Error == nil || response.Error.Code != "UNSUPPORTED_CAPABILITY" || response.Data != nil {
				t.Fatal("unsupported platform returned verification", response)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		data, ok := response.Data.(map[string]any)
		if !ok || data["membersChecked"] != verifyMembers || data["completeCaptureVerified"] != false || data["independentRecoveryVerified"] != false || data["bootTested"] != false {
			t.Fatal("CLI lost verification scope", response)
		}
		if client.method != "backup.verify-manifest" || client.request.Connection != "qemu:///session" || client.request.Action != "" || client.request.Apply != nil {
			t.Fatal("CLI verification bypassed read-only shared method")
		}
	}
	if runtime.GOOS != "linux" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "disk"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd := New(client, &out, &out)
	cmd.SetArgs([]string{"backup", "verify-manifest", filename, "--input", string(input), "--output", "json"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("CLI root option did not reach member integrity verification")
	}
	var response app.Response
	if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.Error == nil || response.Data != nil {
		t.Fatal("failed member verification retained success payload", response, err)
	}
}

func TestRecoveryCLIFailuresAndCancellationAreVisible(t *testing.T) {
	for _, tc := range []struct {
		name      string
		canceled  bool
		transport error
		args      []string
		code      string
	}{
		{"unsupported-native-adapter", false, nil, []string{"vm", "recovery", "inspect", "vm-id"}, "UNSUPPORTED_CAPABILITY"},
		{"canceled-inspection", true, nil, []string{"vm", "recovery", "inspect", "vm-id"}, "OPERATION_FAILED"},
		{"canceled-member-verification", true, nil, []string{"backup", "verify-manifest", "/unopened-manifest.json", "--input", `{"root":"/unopened-members"}`}, "OPERATION_FAILED"},
		{"transport-failure", false, errors.New("fixture disconnected"), []string{"vm", "recovery", "inspect", "vm-id"}, "OPERATION_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &recoveryCLIServiceClient{transportError: tc.transport}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			var out bytes.Buffer
			cmd := New(client, &out, &out)
			cmd.SetArgs(append(tc.args, "--output", "json", "--non-interactive"))
			err := cmd.ExecuteContext(ctx)
			var response app.Response
			if decodeErr := json.Unmarshal(out.Bytes(), &response); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if err == nil || domain.ExitCode(err) == 0 || response.Error == nil || response.Error.Code != tc.code || response.Data != nil {
				t.Fatal("CLI failure was hidden or retained success data", response, err)
			}
			if tc.code == "UNSUPPORTED_CAPABILITY" && domain.ExitCode(err) != 3 {
				t.Fatal("unsupported runtime lost its CLI exit classification")
			}
		})
	}
}

func TestRecoveryCLIStrictInputAndPathNormalizationRefuseBeforeDispatch(t *testing.T) {
	for _, args := range [][]string{
		{"backup", "verify-manifest", "/tmp/manifest.json", "--input", `{"root":"/tmp/a","root":"/tmp/b"}`},
		{"backup", "verify-manifest", "/tmp/alias/../manifest.json"},
		{"backup", "verify-manifest", "/tmp/manifest.json", "--input", `{"root":"/tmp/alias/../members"}`},
	} {
		client := &recoveryCLIServiceClient{}
		var out bytes.Buffer
		cmd := New(client, &out, &out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil || client.calls != 0 {
			t.Fatal("ambiguous recovery input was normalized into a dispatched request", err, client.request)
		}
	}
	client := &recorder{}
	var out bytes.Buffer
	cmd := New(client, &out, &out)
	cmd.SetArgs([]string{"backup", "verify-manifest", "manifest.json", "--input", `{"root":"members"}`})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	manifestPath, _ := filepath.Abs("manifest.json")
	memberRoot, _ := filepath.Abs("members")
	if client.request.Path != manifestPath || client.request.Input["root"] != memberRoot {
		t.Fatal("canonical relative paths did not resolve at the client")
	}
}
