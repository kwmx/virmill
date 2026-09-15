//go:build linux && amd64

package importing

import (
	"context"
	"encoding/json"
	"path/filepath"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
)

// Sources are journal observations, never permission to trust an arbitrary image.
// vm.create still verifies the artifact and all bytes against the durable receipt.
func registerSources(s *app.Service) {
	s.Extensions["import.sources"] = func(ctx context.Context, uid uint32, r app.Request) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.ID != "" || r.Path != "" || r.Action != "" || len(r.Input) != 0 || r.After != 0 || r.Apply != nil {
			return nil, domain.Fail("INVALID_INPUT", "Prepared images do not take extra parameters.")
		}
		jobs, err := s.Engine.Store.Jobs()
		if err != nil {
			return nil, err
		}
		rows := []map[string]any{}
		for _, job := range jobs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if job.State != "succeeded" {
				continue
			}
			p, raw, err := s.Engine.Store.Plan(job.PlanID)
			if err != nil {
				return nil, err
			}
			if p.ActorUID != uid || (p.Operation != "import.prepare" && p.Operation != "import.prepare-disks" && p.Operation != "import.prepare-install") {
				continue
			}
			// Removed prepared images are never offered again (ADR 0058).
			if removed, err := s.Engine.Store.MetadataBytes(discardedKind, job.ID); err != nil {
				return nil, err
			} else if len(removed) != 0 {
				continue
			}
			// A prepared copy handed over to a VM's disks is gone too (ADR 0060).
			if _, handed, err := HandedOver(s.Engine.Store, job.ID); err != nil {
				return nil, err
			} else if handed {
				continue
			}
			var in struct {
				Destination string `json:"destination"`
			}
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, err
			}
			var artifact Artifact
			if err := s.Engine.Store.Get("import-artifact", p.ID, &artifact); err != nil {
				return nil, err
			}
			name := artifact.System.Name
			if name == "" {
				name = artifact.System.ID
			}
			if name == "" {
				name = filepath.Base(in.Destination)
			}
			rows = append(rows, map[string]any{"operationID": job.ID, "name": name, "kind": artifact.Kind, "destination": in.Destination})
		}
		return rows, nil
	}
}
