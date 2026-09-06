package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func TestConcurrentCancellation(t *testing.T) {
	inW, inR := io.Pipe()
	outW, outR := io.Pipe()
	defer inW.Close()
	defer inR.Close()
	defer outW.Close()
	defer outR.Close()
	started := make(chan struct{})
	server := &Server{Identity: Identity{"example.virmill.test", "0.1.0"}, ExtensionTypes: []string{"action"}, Description: map[string]any{}, Methods: map[string]Handler{"action.execute": func(ctx context.Context, p json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}}
	done := make(chan error, 1)
	go func() { done <- server.Serve(context.Background(), inW, outR) }()
	reader := bufio.NewReader(outW)
	send := func(s string) {
		if _, e := io.WriteString(inR, s+"\n"); e != nil {
			t.Fatal(e)
		}
	}
	read := func() map[string]any {
		b, e := reader.ReadBytes('\n')
		if e != nil {
			t.Fatal(e)
		}
		var m map[string]any
		if e = json.Unmarshal(b, &m); e != nil {
			t.Fatal(e)
		}
		return m
	}
	send(`{"jsonrpc":"2.0","id":"h-1","method":"initialize","params":{"protocolVersions":["1.0"]}}`)
	if read()["result"] == nil {
		t.Fatal("initialize failed")
	}
	send(`{"jsonrpc":"2.0","id":"h-2","method":"action.execute","params":{}}`)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler never started")
	}
	send(`{"jsonrpc":"2.0","method":"request.cancel","params":{"requestID":"h-2"}}`)
	if read()["error"] == nil {
		t.Fatal("cancel was not processed concurrently")
	}
	send(`{"jsonrpc":"2.0","id":"h-3","method":"shutdown","params":{}}`)
	read()
	if e := <-done; e != nil {
		t.Fatal(e)
	}
}
