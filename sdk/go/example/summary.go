// Package example provides a complete small read-only action using the public SDK.
package example

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"virmill.local/sdk"
)

type VM struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}
type Params struct {
	Action string `json:"action"`
	Input  struct {
		SortBy string `json:"sortBy"`
	} `json:"input"`
	Context struct {
		VMs []VM `json:"selectedVMs"`
	} `json:"context"`
	PlanToken   string   `json:"planToken"`
	OperationID string   `json:"operationID"`
	Grants      []string `json:"grants"`
}

func New(id string) *sdk.Server {
	s := &sdk.Server{Identity: sdk.Identity{ID: id, Version: "0.1.0"}, ExtensionTypes: []string{"action"}, Required: []sdk.Permission{{Name: "vm.read", Scope: "selection"}}, Description: map[string]any{"actions": []map[string]any{{"id": "summary", "title": "Summarize selected VMs", "readOnly": true, "inputSchema": map[string]any{"type": "object"}, "outputSchema": map[string]any{"type": "object"}}}}, Methods: map[string]sdk.Handler{}}
	parse := func(b json.RawMessage) (Params, string, error) {
		var p Params
		if e := json.Unmarshal(b, &p); e != nil {
			return p, "", &sdk.Error{Code: -32602, Message: "Invalid params"}
		}
		if p.Action != "summary" || len(p.Context.VMs) == 0 || len(p.Context.VMs) > 5000 || (p.Input.SortBy != "" && p.Input.SortBy != "name" && p.Input.SortBy != "state") {
			return p, "", &sdk.Error{Code: -32602, Message: "Select VMs and a supported sort field"}
		}
		bytes, _ := json.Marshal(struct {
			Action  string
			Input   any
			Context any
		}{p.Action, p.Input, p.Context})
		hash := sha256.Sum256(bytes)
		return p, hex.EncodeToString(hash[:]), nil
	}
	s.Methods["action.plan"] = func(ctx context.Context, b json.RawMessage) (any, error) {
		_, token, e := parse(b)
		if e != nil {
			return nil, e
		}
		return map[string]any{"summary": "Summarize selected VMs", "effects": []any{}, "requires": []any{}, "planToken": token}, nil
	}
	s.Methods["action.execute"] = func(ctx context.Context, b json.RawMessage) (any, error) {
		p, token, e := parse(b)
		if e != nil {
			return nil, e
		}
		if token != p.PlanToken {
			return nil, sdk.Failure("STALE_PLAN", "input or context changed")
		}
		sort.Slice(p.Context.VMs, func(i, j int) bool {
			if p.Input.SortBy == "state" {
				return p.Context.VMs[i].State < p.Context.VMs[j].State
			}
			return p.Context.VMs[i].Name < p.Context.VMs[j].Name
		})
		return map[string]any{"count": len(p.Context.VMs), "rows": p.Context.VMs}, nil
	}
	return s
}
