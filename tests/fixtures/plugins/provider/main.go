// Simulated provider fixture. All resources are JSON records in its private workspace.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"virmill.local/sdk"
)

type Resource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	State       string `json:"state"`
	OperationID string `json:"operationID"`
}
type State struct {
	Resources map[string]Resource `json:"resources"`
	Keys      map[string]string   `json:"keys"`
}

func main() {
	var mu sync.Mutex
	state := State{Resources: map[string]Resource{}, Keys: map[string]string{}}
	if b, e := os.ReadFile("provider-state.json"); e == nil {
		if e = json.Unmarshal(b, &state); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
	}
	persist := func() error {
		b, e := json.Marshal(state)
		if e != nil {
			return e
		}
		f, e := os.OpenFile("provider-state.tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(b)
		if e == nil {
			e = f.Sync()
		}
		f.Close()
		if e != nil {
			return e
		}
		if e = os.Rename("provider-state.tmp", "provider-state.json"); e != nil {
			return e
		}
		d, e := os.Open(".")
		if e != nil {
			return e
		}
		defer d.Close()
		return d.Sync()
	}
	server := &sdk.Server{Identity: sdk.Identity{ID: "example.virmill.provider-fixture", Version: "0.1.0"}, ExtensionTypes: []string{"provider"}, Description: map[string]any{"providerID": "fixture", "simulated": true}, Methods: map[string]sdk.Handler{}}
	for _, name := range []string{"capabilities", "inventory", "get", "plan", "apply", "status", "reconcile", "cancel"} {
		method := name
		server.Methods["provider."+method] = func(ctx context.Context, b json.RawMessage) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			var p struct {
				ID             string `json:"id"`
				Name           string `json:"name"`
				Operation      string `json:"operation"`
				OperationID    string `json:"operationID"`
				IdempotencyKey string `json:"idempotencyKey"`
				Partial        bool   `json:"partial"`
			}
			if e := json.Unmarshal(b, &p); e != nil {
				return nil, &sdk.Error{Code: -32602, Message: "invalid params"}
			}
			switch method {
			case "capabilities":
				return map[string]any{"providerID": "fixture", "simulated": true, "create": true, "backup": false, "usb": false, "cancel": false}, nil
			case "inventory":
				rows := []Resource{}
				for _, r := range state.Resources {
					rows = append(rows, r)
				}
				return map[string]any{"resources": rows, "nextCursor": nil}, nil
			case "get", "status", "reconcile":
				if p.ID == "" {
					p.ID = state.Keys[p.IdempotencyKey]
				}
				r, ok := state.Resources[p.ID]
				if !ok {
					return nil, sdk.Failure("RESOURCE_MISSING", "unknown simulated resource")
				}
				return r, nil
			case "plan":
				if p.Operation != "create" {
					return nil, sdk.Failure("UNSUPPORTED_CAPABILITY", "fixture supports create only")
				}
				return map[string]any{"effects": []string{"create simulated JSON resource"}, "simulated": true}, nil
			case "apply":
				if p.Operation != "create" || p.OperationID == "" || p.IdempotencyKey == "" {
					return nil, &sdk.Error{Code: -32602, Message: "operation identity/key required"}
				}
				if id := state.Keys[p.IdempotencyKey]; id != "" {
					r := state.Resources[id]
					if r.Name != p.Name || r.OperationID != p.OperationID {
						return nil, sdk.Failure("STALE_PLAN", "key reused with changed input")
					}
					return r, nil
				}
				id := "fixture-" + p.OperationID
				r := Resource{ID: id, Name: p.Name, State: "defined", OperationID: p.OperationID}
				state.Keys[p.IdempotencyKey] = id
				state.Resources[id] = r
				if e := persist(); e != nil {
					return nil, e
				}
				if p.Partial {
					return nil, sdk.Failure("PARTIAL_EFFECT", "simulated definition persisted; readiness failed")
				}
				return r, nil
			default:
				return nil, sdk.Failure("UNSUPPORTED_CAPABILITY", "fixture does not support cancellation")
			}
		}
	}
	if e := server.Serve(context.Background(), os.Stdin, os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
