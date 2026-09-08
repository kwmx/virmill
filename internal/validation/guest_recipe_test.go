package validation

import (
	"os"
	"strings"
	"testing"
)

func TestGuestRecipeDeclaration(t *testing.T) {
	raw, err := os.ReadFile("../../examples/guest-recipes/posix-readiness.json")
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := Document(raw)
	if err != nil || value["kind"] != "GuestRecipe" {
		t.Fatal(value, err)
	}
	for _, change := range [][2]string{{`"non-root"`, `"root"`}, {`"never"`, `"automatic"`}, {`"ssh"`, `"host-shell"`}, {`"1.0.0"`, `"latest"`}, {`"timeoutSeconds": 15`, `"timeoutSeconds": 301`}, {`"idempotent": true`, `"idempotent": null`}} {
		if _, _, err = Document([]byte(strings.Replace(string(raw), change[0], change[1], 1))); err == nil {
			t.Fatal("unsafe declaration accepted", change)
		}
	}
}
