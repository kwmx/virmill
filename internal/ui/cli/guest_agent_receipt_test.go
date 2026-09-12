package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

func TestGuestAgentAndReceiptCLIParity(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		method string
	}{
		{[]string{"vm", "guest-agent", "enable", "selected-vm"}, "vm.plan"},
		{[]string{"vm", "guest-agent", "show", "selected-vm"}, "vm.guest-agent.show"},
		{[]string{"backup", "receipts"}, "backup.receipts.list"},
		{[]string{"backup", "receipt", "show", "operation"}, "backup.receipt.show"},
		{[]string{"backup", "receipt", "read", "receipt.json"}, "backup.receipt.read"},
	} {
		t.Run(strings.Join(tc.args, "-"), func(t *testing.T) {
			client := &coldCLIClient{response: app.Response{APIVersion: domain.APIVersion, Data: map[string]any{}}}
			_, err := coldCLIExecute(t, context.Background(), client, "json", tc.args...)
			if err != nil || len(client.methods) != 1 || client.methods[0] != tc.method {
				t.Fatal(err, client.methods)
			}
			r := client.requests[0]
			if tc.method == "vm.plan" && (r.Action != "set" || r.Input["enableGuestAgent"] != true) {
				t.Fatal(r)
			}
			if tc.method == "backup.receipt.read" && !filepath.IsAbs(r.Path) {
				t.Fatal(r)
			}
			if r.Apply != nil {
				t.Fatal("read/preview applied mutation")
			}
		})
	}
}

func TestBackupReceiptExportPreservesAndValidates(t *testing.T) {
	receipt := domain.BackupReceipt{APIVersion: domain.APIVersion, Kind: "BackupReceipt", OperationID: "11111111-1111-4111-8111-111111111111", CaptureID: "22222222-2222-4222-8222-222222222222", Connection: "qemu:///system", Repository: "/private/repository", SnapshotID: strings.Repeat("a", 64), ManifestSHA256: strings.Repeat("b", 64), VerifiedAt: time.Now().UTC()}
	path := filepath.Join(t.TempDir(), "receipt.json")
	client := &coldCLIClient{response: app.Response{APIVersion: domain.APIVersion, Data: receipt}}
	var out bytes.Buffer
	run := func() error {
		root := New(client, &out, &out)
		root.SetArgs([]string{"backup", "receipt", "export", receipt.OperationID, path, "--output", "json"})
		return root.Execute()
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got domain.BackupReceipt
	if json.Unmarshal(raw, &got) != nil || got != receipt {
		t.Fatalf("receipt changed: %s", raw)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if err = run(); err == nil {
		t.Fatal("overwrote receipt")
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(raw, again) {
		t.Fatal("receipt overwritten")
	}
	client.response.Data = map[string]any{"password": "must not export"}
	root := New(client, &out, &out)
	bad := filepath.Join(t.TempDir(), "invalid.json")
	root.SetArgs([]string{"backup", "receipt", "export", receipt.OperationID, bad})
	if root.Execute() == nil {
		t.Fatal("exported malformed receipt")
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
