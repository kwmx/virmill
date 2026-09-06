package lab

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"virmill.local/core/internal/app/network"
)

type VM struct {
	Architecture string `json:"architecture"`
	Source       struct {
		Type        string `json:"type"`
		TemplateRef string `json:"templateRef"`
		Path        string `json:"path"`
	} `json:"source"`
	Nics  []network.NIC `json:"nics"`
	Disks []struct {
		ID string `json:"id"`
	} `json:"disks"`
	Firmware struct {
		Mode       string `json:"mode"`
		SecureBoot bool   `json:"secureBoot"`
		TPM        bool   `json:"tpm"`
	} `json:"firmware"`
}
type Definition struct {
	Spec struct {
		Networks []struct {
			ID          string        `json:"id"`
			Spec        *network.Spec `json:"spec"`
			ExternalRef string        `json:"externalRef"`
		} `json:"networks"`
		Machines []struct {
			ID        string `json:"id"`
			Spec      VM     `json:"spec"`
			DependsOn []struct {
				Machine        string `json:"machine"`
				Ready          string `json:"ready"`
				TimeoutSeconds int    `json:"timeoutSeconds"`
			} `json:"dependsOn"`
		} `json:"machines"`
	} `json:"spec"`
}
type Report struct {
	Valid         bool     `json:"valid"`
	Order         []string `json:"dependencyOrder"`
	Warnings      []string `json:"warnings"`
	HostPreflight string   `json:"hostPreflight"`
}

func ValidateVM(v VM, nets map[string]network.Spec) ([]string, error) {
	if v.Firmware.SecureBoot && v.Firmware.Mode != "uefi" {
		return nil, errors.New("Secure Boot requires UEFI")
	}
	ids := map[string]bool{}
	for _, d := range v.Disks {
		if ids[d.ID] {
			return nil, errors.New("duplicate disk ID")
		}
		ids[d.ID] = true
	}
	return network.ValidateNICs(v.Nics, nets)
}
func Validate(raw []byte) (Report, error) {
	var d Definition
	if e := json.Unmarshal(raw, &d); e != nil {
		return Report{}, e
	}
	out := Report{Order: []string{}, Warnings: []string{}, HostPreflight: "not-run"}
	nets := map[string]network.Spec{}
	prefixes := []netip.Prefix{}
	for _, n := range d.Spec.Networks {
		if _, ok := nets[n.ID]; ok {
			return out, errors.New("duplicate network ID")
		}
		if n.Spec != nil {
			if e := network.Validate(*n.Spec); e != nil {
				return out, e
			}
			nets[n.ID] = *n.Spec
			if n.Spec.IPv4 != nil && n.Spec.IPv4.CIDR != "auto" {
				p, _ := netip.ParsePrefix(n.Spec.IPv4.CIDR)
				for _, q := range prefixes {
					if p.Overlaps(q) {
						return out, errors.New("overlapping lab networks")
					}
				}
				prefixes = append(prefixes, p)
			}
		} else {
			nets[n.ID] = network.Spec{Type: "bridge"}
			out.Warnings = append(out.Warnings, "External network "+n.ID+" requires host resolution; preserved during teardown")
		}
	}
	graph := map[string][]string{}
	for _, m := range d.Spec.Machines {
		if _, ok := graph[m.ID]; ok {
			return out, errors.New("duplicate machine ID")
		}
		graph[m.ID] = []string{}
		warnings, e := ValidateVM(m.Spec, nets)
		if e != nil {
			return out, e
		}
		out.Warnings = append(out.Warnings, warnings...)
		for _, dep := range m.DependsOn {
			graph[m.ID] = append(graph[m.ID], dep.Machine)
		}
		if m.Spec.Source.Type == "template" {
			out.Warnings = append(out.Warnings, "Template "+m.Spec.Source.TemplateRef+" requires immutable host resolution")
		}
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return errors.New("dependency cycle")
		}
		if state[id] == 2 {
			return nil
		}
		deps, ok := graph[id]
		if !ok {
			return fmt.Errorf("unknown dependency %s", id)
		}
		state[id] = 1
		sort.Strings(deps)
		for _, dep := range deps {
			if e := visit(dep); e != nil {
				return e
			}
		}
		state[id] = 2
		out.Order = append(out.Order, id)
		return nil
	}
	ids := []string{}
	for id := range graph {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if e := visit(id); e != nil {
			return out, e
		}
	}
	out.Valid = true
	return out, nil
}
