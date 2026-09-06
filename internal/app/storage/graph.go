package storage

import (
	"errors"
	"fmt"
	"sort"
)

type Volume struct {
	ID         string   `json:"id"`
	Parent     string   `json:"parent,omitempty"`
	Immutable  bool     `json:"immutable"`
	Ownership  string   `json:"ownership"`
	References []string `json:"references"`
	Uncertain  bool     `json:"uncertain"`
}
type Graph map[string]Volume

func (g Graph) Validate() error {
	seen := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if seen[id] == 1 {
			return errors.New("backing cycle")
		}
		if seen[id] == 2 {
			return nil
		}
		v, ok := g[id]
		if !ok {
			return fmt.Errorf("unknown backing object %s", id)
		}
		if id != v.ID {
			return errors.New("volume identity mismatch")
		}
		seen[id] = 1
		if v.Parent != "" {
			p, ok := g[v.Parent]
			if !ok || !p.Immutable {
				return errors.New("backing parent must be known and immutable")
			}
			if e := visit(v.Parent); e != nil {
				return e
			}
		}
		seen[id] = 2
		return nil
	}
	for id := range g {
		if e := visit(id); e != nil {
			return e
		}
	}
	return nil
}
func (g Graph) DeletionCandidates() ([]string, error) {
	if e := g.Validate(); e != nil {
		return nil, e
	}
	referenced := map[string]bool{}
	for _, v := range g {
		if v.Uncertain {
			return nil, errors.New("uncertain graph blocks all garbage collection")
		}
		if v.Parent != "" {
			referenced[v.Parent] = true
		}
		if len(v.References) > 0 {
			referenced[v.ID] = true
		}
	}
	out := []string{}
	for id, v := range g {
		if !referenced[id] && v.Ownership == "managed" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}
