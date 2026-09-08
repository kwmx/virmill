package main

import "sort"

// Fixture-owned schemas describe the actual method payloads, without changing
// the language-neutral core protocol or claiming arbitrary provider support.
func description() map[string]any {
	object := func(properties map[string]any, required []string) map[string]any {
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	allObject := func(properties map[string]any) map[string]any {
		keys := []string{}
		for k := range properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return object(properties, keys)
	}
	text := func(max int) map[string]any { return map[string]any{"type": "string", "maxLength": max} }
	fixed := func(value any) map[string]any { return map[string]any{"const": value} }
	capProperties := map[string]any{}
	for _, name := range []string{"create", "start", "stop", "delete", "configurationEdits", "networks", "devices", "snapshots", "backups", "guestTransports", "cancel"} {
		capProperties[name] = fixed(name == "create" || name == "start" || name == "stop" || name == "delete")
	}
	caps := allObject(capProperties)
	resourceProperties := map[string]any{"id": text(128), "key": allObject(map[string]any{"providerID": fixed("fixture"), "connectionID": fixed(connection), "kind": fixed("vm"), "externalID": text(128)}), "name": text(128), "state": map[string]any{"enum": []string{"defined", "running", "stopped", "deleted"}}, "operationID": text(120), "revision": map[string]any{"type": "integer", "minimum": 1}, "capabilities": caps}
	resource := allObject(resourceProperties)
	outcomeProperties := map[string]any{}
	for k, v := range resourceProperties {
		outcomeProperties[k] = v
	}
	outcomeProperties["status"] = map[string]any{"enum": []string{"succeeded", "partial"}}
	outcomeProperties["reconciled"] = map[string]any{"type": "boolean"}
	outcomeProperties["simulated"] = fixed(true)
	outcome := allObject(outcomeProperties)
	outputs := map[string]any{
		"capabilities": allObject(map[string]any{"providerID": fixed("fixture"), "connectionID": fixed(connection), "simulated": fixed(true), "capabilities": caps, "create": fixed(true), "backup": fixed(false), "usb": fixed(false), "cancel": fixed(false)}),
		"inventory":    allObject(map[string]any{"resources": map[string]any{"type": "array", "maxItems": pageLimit, "items": resource}, "nextCursor": map[string]any{"type": []string{"string", "null"}, "maxLength": 1024}, "generation": map[string]any{"type": "integer", "minimum": 0}, "simulated": fixed(true)}),
		"get":          resource,
		"plan":         allObject(map[string]any{"summary": text(128), "effects": map[string]any{"type": "array", "minItems": 1, "maxItems": 1, "items": text(128)}, "requires": map[string]any{"type": "array", "maxItems": 0}, "planToken": map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"}, "resultState": map[string]any{"enum": []string{"defined", "running", "stopped", "deleted"}}, "simulated": fixed(true)}),
		"apply":        outcome, "status": outcome, "reconcile": outcome, "cancel": false,
	}
	methods := map[string]any{}
	inputs := map[string][]string{"capabilities": {}, "inventory": {"cursor", "limit"}, "get": {"id"}, "plan": {"operation", "id", "name"}, "apply": {"operation", "id", "name", "operationID", "idempotencyKey", "partial", "planToken"}, "status": {"id", "operationID", "idempotencyKey"}, "reconcile": {"id", "operationID", "idempotencyKey"}, "cancel": {"id", "operationID", "idempotencyKey"}}
	for method, fields := range inputs {
		properties := map[string]any{"connectionID": fixed(connection)}
		for _, field := range fields {
			var v any = text(128)
			switch field {
			case "cursor":
				v = text(1024)
			case "operationID":
				v = text(120)
			case "planToken":
				v = map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"}
			case "partial":
				v = map[string]any{"type": "boolean"}
			case "limit":
				v = map[string]any{"type": "integer", "minimum": 1, "maximum": pageLimit}
			}
			properties[field] = v
		}
		required := []string{}
		switch method {
		case "get":
			required = []string{"id"}
		case "plan":
			required = []string{"operation"}
		case "apply":
			required = []string{"operation", "operationID", "idempotencyKey"}
		}
		input := object(properties, required)
		if method == "status" || method == "reconcile" {
			input["anyOf"] = []any{map[string]any{"required": []string{"id"}}, map[string]any{"required": []string{"operationID"}}, map[string]any{"required": []string{"idempotencyKey"}}}
		}
		methods["provider."+method] = map[string]any{"inputSchema": input, "outputSchema": outputs[method]}
	}
	return map[string]any{"providerID": "fixture", "connectionID": connection, "simulated": true, "capabilities": capabilities(), "methods": methods}
}
