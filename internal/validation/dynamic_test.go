package validation

import "testing"

func TestPluginSchemasAreOfflineAndValidateResults(t *testing.T) {
	schema := []byte(`{"type":"object","properties":{"count":{"type":"integer","minimum":0}},"required":["count"],"additionalProperties":false}`)
	if err := Dynamic(schema, []byte(`{"count":2}`)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{"count":-1}`, `{"count":"two"}`, `{"count":1,"extra":true}`, `{"count":1,"count":2}`} {
		if err := Dynamic(schema, []byte(bad)); err == nil {
			t.Fatal("invalid result accepted", bad)
		}
	}
	for _, reference := range []string{`{"$ref":"file:///etc/passwd"}`, `{"$ref":"https://virmill.example/not-bundled.json"}`} {
		if err := Dynamic([]byte(reference), []byte(`{}`)); err == nil {
			t.Fatal("external schema resolved", reference)
		}
	}
}
