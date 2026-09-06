//go:build linux

package local

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

func TestPrivateRPCAndSingleton(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	db, e := store.Open(filepath.Join(dir, "journal.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if second, e := store.Open(filepath.Join(dir, "journal.db")); e == nil {
		second.Close()
		t.Fatal("second journal writer accepted")
	}
	engine := operations.New(db)
	defer engine.Close()
	service := app.New(nil, engine)
	socket := filepath.Join(dir, "control.sock")
	server, e := Listen(socket, service)
	if e != nil {
		if os.Getenv("VIRMILL_TEST_REQUIRE_IPC") == "1" {
			t.Fatal(e)
		}
		t.Skipf("IPC sandbox unavailable: %v", e)
	}
	defer server.Close()
	done := make(chan error, 1)
	go func() { done <- server.Serve() }()
	if second, e := Listen(socket, service); e == nil {
		second.Close()
		t.Fatal("second coordinator accepted")
	}
	client := Client{Socket: socket, Timeout: time.Second}
	result, e := client.Call(context.Background(), "version", app.Request{})
	if e != nil || result.Error != nil {
		t.Fatal(e, result)
	}
	b, _ := json.Marshal(result)
	if string(b) == "" {
		t.Fatal("empty result")
	}
	for path, mode := range map[string]os.FileMode{dir: 0700, socket: 0600} {
		st, e := os.Stat(path)
		if e != nil || st.Mode().Perm() != mode {
			t.Fatal(path, e)
		}
	}
	conn, e := net.Dial("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	conn.SetDeadline(time.Now().Add(time.Second))
	conn.Write([]byte(`{"jsonrpc":"2.0","id":"h-1","id":"h-2","method":"version","params":{}}` + "\n"))
	reply := make([]byte, 1024)
	n, e := conn.Read(reply)
	conn.Close()
	if e != nil {
		t.Fatal(e)
	}
	var parsed map[string]any
	if e = json.Unmarshal(reply[:n], &parsed); e != nil || parsed["error"] == nil {
		t.Fatal("duplicate-key RPC accepted", e)
	}
	server.Close()
	if e = <-done; e != nil {
		t.Fatal(e)
	}
}
