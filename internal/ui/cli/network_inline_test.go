package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/validation"
)

func inlineNetworkCLIInput(t *testing.T) (string, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(protectedNetworkExample(t, "protected-nat.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := validation.Document(raw)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"document": doc}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded), input
}

func TestInlineNetworkCLIDispatchesDocumentWithoutPositionalPath(t *testing.T) {
	input, want := inlineNetworkCLIInput(t)
	recorder := &recorder{}
	var out bytes.Buffer
	command := New(recorder, &out, &out)
	command.SetArgs([]string{"network", "create", "--input", input, "--plan", "--output", "json", "--non-interactive"})
	if err := command.Execute(); err != nil {
		t.Fatal(err, out.String())
	}
	if recorder.method != "network.create" || recorder.request.Action != "create" || recorder.request.Path != "" || recorder.request.ID != "" || recorder.request.Apply != nil || !reflect.DeepEqual(recorder.request.Input, want) {
		t.Fatal("inline command changed document or selected another operation", recorder)
	}
}

func TestInlineNetworkCLIAndExistingFileReachSharedProtectedReview(t *testing.T) {
	input, _ := inlineNetworkCLIInput(t)
	path := protectedNetworkExample(t, "protected-nat.yaml")
	for _, source := range []string{"file", "inline"} {
		t.Run(source, func(t *testing.T) {
			client := newProtectedNetworkClient(t)
			args := []string{"network", "create"}
			if source == "file" {
				args = append(args, path)
			} else {
				args = append(args, "--input", input)
			}
			response, _, err := protectedNetworkCLI(t, client, "json", append(args, "--plan")...)
			if err != nil {
				t.Fatal(err)
			}
			plan := protectedNetworkPlan(t, response)
			assertProtectedNetworkReview(t, plan, "nat", "services-only", "10.197.240.0/24", true, 2)
			if len(client.requests) != 1 || client.methods[0] != "network.create" || client.requests[0].Apply != nil {
				t.Fatal("CLI bypassed shared preview", client.requests)
			}
			request := client.requests[0]
			if source == "file" && (request.Path != filepath.Clean(path) || len(request.Input) != 0) || source == "inline" && (request.Path != "" || len(request.Input) != 1) {
				t.Fatal("source selection changed during dispatch", request)
			}
			assertProtectedNetworkNoMutation(t, client)
		})
	}
}

func TestInlineNetworkCLIMixedOrEmptySourceHasNoReviewOrEffects(t *testing.T) {
	input, _ := inlineNetworkCLIInput(t)
	for _, args := range [][]string{
		{"network", "create", "--plan"},
		{"network", "create", "--input", `{}`, "--plan"},
		{"network", "create", "--input", `{"document":null}`, "--plan"},
		{"network", "create", protectedNetworkExample(t, "protected-nat.yaml"), "--input", input, "--plan"},
	} {
		client := newProtectedNetworkClient(t)
		response, text, err := protectedNetworkCLI(t, client, "json", args...)
		if err == nil || response.Error == nil || strings.Contains(text, "networkXML") || strings.Contains(text, "network.policy-filter") {
			t.Fatal("invalid input displayed a successful review", args, response, err)
		}
		if len(client.requests) != 1 || client.requests[0].Apply != nil || client.backend.checks != 0 || client.firewall.checks != 0 {
			t.Fatal("invalid input reached native/grant preflight")
		}
		assertProtectedNetworkNoMutation(t, client)
	}
}
