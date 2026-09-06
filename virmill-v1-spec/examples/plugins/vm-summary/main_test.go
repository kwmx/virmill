package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDuplicateKeys(t *testing.T) {
	for _, s := range []string{`{"x":1,"x":2}`, `{"a":{"x":1,"x":2}}`, `{"a":1} {"b":2}`, `NaN`, `[] garbage`} {
		if validateJSON([]byte(s)) == nil {
			t.Fatalf("accepted bad frame %s", s)
		}
	}
	if err := validateJSON([]byte(`{"name":"A\nB","items":[1,2]}`)); err != nil {
		t.Fatal(err)
	}
}
func TestInvalidUTF8(t *testing.T) {
	if validateJSON([]byte{0xff}) == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}
func TestMalformedThenNotInitialized(t *testing.T) {
	var out bytes.Buffer
	if err := run(strings.NewReader("broken\n{\"jsonrpc\":\"2.0\",\"id\":\"h-1\",\"method\":\"describe\"}\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "-32700") || !strings.Contains(out.String(), "NOT_INITIALIZED") {
		t.Fatal(out.String())
	}
}
func TestOversizedFrame(t *testing.T) {
	var out bytes.Buffer
	if run(strings.NewReader(strings.Repeat("x", maxFrame+2)+"\n"), &out) == nil {
		t.Fatal("oversized frame accepted")
	}
}
