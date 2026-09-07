package app

import (
	"context"
	"virmill.local/core/internal/domain"
)

func (h *vmHandler) Estimate(ctx context.Context, _ domain.Plan, _ []byte) (domain.Estimates, error) {
	if err := ctx.Err(); err != nil {
		return domain.Estimates{}, err
	}
	estimate := domain.Estimates{Notes: "No new managed disks are allocated. Guest writes, save-state bytes, available host resources and downtime duration are not estimated."}
	switch h.action {
	case "stop", "hard-stop", "pause", "save":
		estimate.RequiresDowntime = true
		estimate.Notes += " This operation interrupts the running guest."
	case "set":
		estimate.RequiresDowntime = true
		estimate.Notes += " The configuration edit requires an already stopped guest; it does not shut down or restart the guest."
	case "start", "restore-saved", "resume", "autostart":
		estimate.Notes += " This operation does not introduce a new guest interruption; it does not verify boot or application readiness."
	default:
		return domain.Estimates{}, domain.Fail("NOT_IMPLEMENTED", "VM action lacks a reviewed downtime estimate")
	}
	return estimate, nil
}
