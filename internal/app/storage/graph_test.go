package storage

import "testing"

func TestGraphRefusesReferencedUncertainAndCyclicBases(t *testing.T) {
	g := Graph{"base": {ID: "base", Immutable: true, Ownership: "managed"}, "leaf": {ID: "leaf", Parent: "base", Ownership: "managed", References: []string{"vm"}}}
	c, e := g.DeletionCandidates()
	if e != nil || len(c) != 0 {
		t.Fatal(c, e)
	}
	v := g["leaf"]
	v.Uncertain = true
	g["leaf"] = v
	if _, e = g.DeletionCandidates(); e == nil {
		t.Fatal("uncertainty ignored")
	}
	v.Uncertain = false
	g["leaf"] = v
	b := g["base"]
	b.Parent = "leaf"
	g["base"] = b
	if g.Validate() == nil {
		t.Fatal("mutable parent/cycle accepted")
	}
}
