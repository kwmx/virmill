package validation

import (
	"encoding/json"
	"errors"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"virmill.local/core/internal/wire"
)

// Dynamic validates bounded plugin schemas without enabling URL/file resolution.
// The compiler's default Go regexp implementation also bounds regex execution.
func Dynamic(schema, document []byte) error {
	if len(schema) > 64<<10 {
		return errors.New("plugin schema exceeds 64 KiB limit")
	}
	var s, v any
	if err := wire.Decode(schema, &s); err != nil {
		return err
	}
	if err := wire.Decode(document, &v); err != nil {
		return err
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(offlineLoader{})
	if err := c.AddResource("https://virmill.example/runtime/action.schema.json", s); err != nil {
		return err
	}
	compiled, err := c.Compile("https://virmill.example/runtime/action.schema.json")
	if err != nil {
		return err
	}
	return compiled.Validate(v)
}

func DynamicValue(schema json.RawMessage, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return Dynamic(schema, b)
}
