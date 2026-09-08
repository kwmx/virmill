//go:build linux && amd64

package helper

import (
	"context"
	"time"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

func (c Client) NetworkFilter(ctx context.Context, r Request) (NetworkResponse, error) {
	var out NetworkResponse
	if r.Operation != "network.ipv6-filter" || r.Network == nil || r.Access != nil || r.Auxiliary != nil {
		return out, domain.Fail("INVALID_INPUT", "dedicated network filter request required")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	conn, r, err := c.connectRequest(ctx, r)
	if err != nil {
		return out, err
	}
	defer conn.Close()
	frame, f, err := receiveSnapshotFrame(ctx, conn, true)
	if f != nil {
		f.Close()
		return out, domain.Fail("RECOVERY_REQUIRED", "network filter returned an unexpected descriptor")
	}
	if err != nil {
		return out, err
	}
	var response Response
	if wire.Decode(frame, &response) != nil || response.APIVersion != domain.APIVersion || response.Access != nil || response.Auxiliary != nil {
		return out, domain.Fail("RECOVERY_REQUIRED", "network helper response contract differs")
	}
	if !response.Success {
		if response.Network != nil || response.Error == "" {
			return out, domain.Fail("RECOVERY_REQUIRED", "ambiguous network helper refusal")
		}
		code := response.ErrorCode
		switch code {
		case "PERMISSION_DENIED", "SOURCE_CHANGED", "STALE_PLAN", "UNSUPPORTED_CAPABILITY", "INVALID_INPUT", "INVALID_STATE", "RECOVERY_REQUIRED", "OPERATION_FAILED":
		default:
			code = "OPERATION_FAILED"
		}
		return out, domain.Fail(code, response.Error)
	}
	if response.Error != "" || response.ErrorCode != "" || response.Network == nil {
		return out, domain.Fail("RECOVERY_REQUIRED", "incomplete network helper observation")
	}
	out = *response.Network
	if out.Version != 1 || out.ResourceID != r.ResourceID || out.Bridge != r.Network.Definition.Bridge || out.PlanDigest != r.PlanDigest || out.JobID != r.JobID || out.PacketVerified {
		return NetworkResponse{}, domain.Fail("RECOVERY_REQUIRED", "network filter observation binding differs")
	}
	if r.Mode != "check" && (!out.RuntimePresent || !out.PermanentPresent) {
		return NetworkResponse{}, domain.Fail("RECOVERY_REQUIRED", "network filter runtime or permanent rules absent")
	}
	return out, ctx.Err()
}
