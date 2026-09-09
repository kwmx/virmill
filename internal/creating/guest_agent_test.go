//go:build linux && amd64

package creating

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/wire"
)

func TestCreationGuestAgentChannelRequiresAcknowledgementAndRetainsReview(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "opt-in"}[enabled], func(t *testing.T) {
			s, backend, request, _ := creationFixture(t)
			if enabled {
				request.Input["hardware"].(map[string]any)["guestAgent"] = true
			}
			p, err := s.Plan(context.Background(), 1000, request)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(p.Acknowledgements, "guest-agent-channel") != enabled {
				t.Fatal("guest channel acknowledgement does not match explicit opt-in", p.Acknowledgements)
			}
			if target := p.Review["target"].(domain.CreationTarget); target.Spec.GuestAgent != enabled {
				t.Fatal("plan review lost the guest channel choice")
			}
			_, raw, err := s.Store.Plan(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			var frozen input
			if err = wire.Decode(raw, &frozen); err != nil || frozen.Target.Spec.GuestAgent != enabled {
				t.Fatal("stored recipe lost the guest channel choice", err)
			}
			review, err := s.Review(context.Background(), p, raw)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(review)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				Target domain.CreationTarget `json:"target"`
			}
			if err = json.Unmarshal(encoded, &decoded); err != nil || decoded.Target.Spec.GuestAgent != enabled {
				t.Fatal("regenerated JSON review lost the guest channel choice", err)
			}
			if !enabled {
				return
			}
			acks := slices.DeleteFunc(slices.Clone(p.Acknowledgements), func(value string) bool { return value == "guest-agent-channel" })
			_, err = s.Engine.Apply(context.Background(), 1000, operations.ApplyRequest{PlanID: p.ID, PlanDigest: p.Digest, IdempotencyKey: domain.ID(), Acknowledgements: acks})
			if err == nil || backend.allocated != 0 || backend.populated != 0 || backend.definitions != 0 {
				t.Fatal("unacknowledged guest channel reached an effect", err)
			}
			jobs, err := s.Store.Jobs()
			if err != nil || len(jobs) != 0 {
				t.Fatal("unacknowledged channel created a job", jobs, err)
			}
		})
	}
}
