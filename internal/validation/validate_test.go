package validation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundledExamplesOffline(t *testing.T) {
	files, e := filepath.Glob("../../examples/*/*.yaml")
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		b, e := os.ReadFile(f)
		if e != nil {
			t.Fatal(e)
		}
		if _, _, e = Document(b); e != nil {
			t.Errorf("%s: %v", f, e)
		}
	}
}
func TestYAMLAndTerminalSafety(t *testing.T) {
	for _, s := range []string{"a: 1\na: 2", "a: &a [*a]", "a: !!python/object x", "a: 1\n---\na: 2"} {
		if _, _, e := Document([]byte(s)); e == nil {
			t.Fatal("accepted malicious YAML")
		}
	}
	if _, e := DisplayName("safe\x1b[2J"); e == nil {
		t.Fatal("terminal controls accepted")
	}
	if got := SafeText("a\x1b[2Jb\x1b]52;c;secret\ac"); got != "abc" {
		t.Fatal(got)
	}
	if strings.Contains(SafeText("x\u202ey"), "\u202e") {
		t.Fatal("bidi control preserved")
	}
}
