package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var acknowledgementID = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)+$`)

// ackName reports whether an identifier holds acknowledgements: acks, ackText,
// startVMAck, expectedCreationAcks, networkAcknowledgements. A lowercase
// "callback" or "stack" does not match.
func ackName(name string) bool {
	lower := strings.ToLower(name)
	return lower == "acks" || strings.HasPrefix(lower, "ack") || strings.HasSuffix(name, "Ack") ||
		strings.HasSuffix(name, "Acks") || strings.Contains(lower, "acknowledg")
}

// plannerAcknowledgements collects every acknowledgement a planner under
// internal/ can ask for: the acknowledgement argument of Engine.Plan, and the
// string literals held in, appended to or returned by names for
// acknowledgements.
func plannerAcknowledgements(t *testing.T) map[string]string {
	t.Helper()
	found := map[string]string{}
	fset := token.NewFileSet()
	collect := func(node ast.Node) {
		ast.Inspect(node, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if value, err := strconv.Unquote(lit.Value); err == nil && acknowledgementID.MatchString(value) {
					found[value] = fset.Position(lit.Pos()).String()
				}
			}
			return true
		})
	}
	err := filepath.WalkDir(filepath.Join("..", ".."), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Plan" && len(x.Args) == 10 {
					collect(x.Args[8])
				}
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "append" && len(x.Args) > 1 {
					if target, ok := x.Args[0].(*ast.Ident); ok && ackName(target.Name) {
						for _, arg := range x.Args[1:] {
							collect(arg)
						}
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range x.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && ackName(id.Name) && i < len(x.Rhs) {
						collect(x.Rhs[i])
					}
				}
			case *ast.ValueSpec:
				for i, name := range x.Names {
					if ackName(name.Name) && i < len(x.Values) {
						collect(x.Values[i])
					}
				}
			case *ast.FuncDecl:
				if ackName(x.Name.Name) && x.Body != nil {
					collect(x.Body)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// With one confirmation instead of checkboxes, the plain label is the whole
// explanation of what the user agrees to (ADR 0065). A planner that asks for a
// new acknowledgement must explain it here first.
func TestEveryPlannerAcknowledgementHasAPlainLabel(t *testing.T) {
	found := plannerAcknowledgements(t)
	// The collector must see the constructs planners really use, or this test
	// would pass by finding nothing.
	for _, want := range []string{"host-mutation", "write-import-artifacts", "offline-source-files", "pool-overcommit",
		"start-vm", "delete-prepared-copy", "network-firewall", "guest-reboot", "data-loss-delete-old-copy"} {
		if _, ok := found[want]; !ok {
			t.Fatalf("the collector did not find %s; it no longer sees how planners declare acknowledgements", want)
		}
	}
	missing := []string{}
	for id, at := range found {
		if _, known := plainAcknowledgement(id); !known {
			missing = append(missing, id+" ("+at+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("acknowledgements without a plain label:\n  %s", strings.Join(missing, "\n  "))
	}
}
