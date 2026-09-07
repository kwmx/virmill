//go:build linux && amd64

package helper

import (
	"context"
	"net"
	"time"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

// InspectAuxiliary authenticates a dedicated metadata-only request. A received
// descriptor is always refused and closed, including on malformed responses.
// Capture and delivery need their own durable operation; inspection creates none.
func (c Client) InspectAuxiliary(ctx context.Context, r Request) (AuxiliaryResponse, error) {
	if r.Operation != "state.auxiliary" || r.Mode != "inspect" || r.Access != nil || r.Auxiliary == nil || r.Auxiliary.Expected != nil {
		return AuxiliaryResponse{}, domain.Fail("INVALID_INPUT", "dedicated auxiliary inspection request required")
	}
	// The descriptor-aware receiver applies its context's deadline to the socket.
	// Keep the helper transport cap even when the CLI has a longer wait timeout.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, r, err := c.connectRequest(ctx, r)
	if err != nil {
		return AuxiliaryResponse{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	return receiveAuxiliaryInspection(ctx, conn, r)
}

// The connection's root peer must already have been verified by connectRequest.
// Keeping this decoder independent permits adversarial private-socket tests.
func receiveAuxiliaryInspection(ctx context.Context, conn *net.UnixConn, r Request) (AuxiliaryResponse, error) {
	frame, f, err := receiveSnapshotFrame(ctx, conn, true)
	if f != nil {
		_ = f.Close()
		return AuxiliaryResponse{}, domain.Fail("RECOVERY_REQUIRED", "metadata-only helper inspection received an unexpected descriptor")
	}
	if err != nil {
		return AuxiliaryResponse{}, err
	}
	var response Response
	if err = wire.Decode(frame, &response); err != nil {
		return AuxiliaryResponse{}, err
	}
	if response.APIVersion != domain.APIVersion || response.Access != nil {
		return AuxiliaryResponse{}, domain.Fail("RECOVERY_REQUIRED", "helper response contract differs from auxiliary inspection")
	}
	if !response.Success {
		if response.Auxiliary != nil || response.Error == "" {
			return AuxiliaryResponse{}, domain.Fail("RECOVERY_REQUIRED", "ambiguous helper inspection refusal")
		}
		code := response.ErrorCode
		switch code {
		case "PERMISSION_DENIED", "SOURCE_CHANGED", "STALE_PLAN", "UNSUPPORTED_CAPABILITY", "INVALID_INPUT", "INVALID_STATE", "INCOMPLETE_BACKUP", "OPERATION_FAILED":
		default:
			code = "OPERATION_FAILED"
		}
		return AuxiliaryResponse{}, domain.Fail(code, response.Error)
	}
	if response.Error != "" || response.ErrorCode != "" {
		return AuxiliaryResponse{}, domain.Fail("RECOVERY_REQUIRED", "helper inspection combines success and error")
	}
	if err = ValidateAuxiliaryInspection(r, response.Auxiliary); err != nil {
		return AuxiliaryResponse{}, err
	}
	if err = ctx.Err(); err != nil {
		return AuxiliaryResponse{}, err
	}
	return *response.Auxiliary, nil
}
