//go:build linux && amd64

package helper

import (
	"context"

	"virmill.local/core/internal/domain"
)

// Serve authenticates the actual kernel peer and current policy before dispatch.
// AuxiliaryExecutor repeats structural authorization and derives the full set.
func inspectAuxiliaryRequest(ctx context.Context, backend domain.ManagedFileAccessBackend, r Request, p Policy) (*AuxiliaryResponse, error) {
	if r.Mode != "inspect" {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "auxiliary capture and durable delivery are not yet implemented")
	}
	inspector, ok := backend.(domain.ColdStateInspector)
	if !ok {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "native auxiliary-state inspector unavailable")
	}
	in, err := (AuxiliaryExecutor{Backend: inspector}).Inspect(ctx, r, p)
	if err != nil {
		return nil, err
	}
	binding, err := AuxiliaryBinding(r)
	if err != nil {
		return nil, err
	}
	response := &AuxiliaryResponse{Version: 1, JobID: r.JobID, Binding: binding, Stage: "inspected", Inventory: &in}
	if err = ValidateAuxiliaryInspection(r, response); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return response, nil
}
