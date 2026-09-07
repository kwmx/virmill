package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"virmill.local/core/internal/app"
)

func TestVersionedCaptureCLIDeclarationAndTamperedAuxiliaryMember(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("../../../tests/fixtures/protection/capture-manifest")); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]string{"root": root})
	client := &recoveryCLIServiceClient{}
	for _, corrupt := range []bool{false, true} {
		if corrupt {
			if err := os.WriteFile(filepath.Join(root, "vars.fixture"), []byte("changed auxiliary state"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		var out bytes.Buffer
		command := New(client, &out, &out)
		command.SetArgs([]string{"backup", "verify-manifest", filepath.Join(root, "manifest.json"), "--input", string(input), "--output", "json", "--non-interactive"})
		err := command.Execute()
		var response app.Response
		if e := json.Unmarshal(out.Bytes(), &response); e != nil {
			t.Fatal("CLI output was not a clean response", e)
		}
		if corrupt || runtime.GOOS != "linux" {
			if err == nil || response.Error == nil || response.Data != nil {
				t.Fatal("failed check retained success", response, err)
			}
			continue
		}
		report, ok := response.Data.(map[string]any)
		if err != nil || response.Error != nil || !ok || report["manifestKind"] != "ColdRecoveryPoint" || report["membersChecked"] != true || report["completeCaptureVerified"] != false || report["independentRecoveryVerified"] != false || report["bootTested"] != false {
			t.Fatal("CLI changed declaration checks into recovery proof", response, err)
		}
	}
}
