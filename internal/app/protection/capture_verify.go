package protection

import (
	"context"
	"encoding/json"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

// VerifyContext checks the declared capture members only. A caller-supplied
// manifest cannot authenticate native observations, coordinator receipts or the
// completeness of an auxiliary-state inventory, even when every digest matches.
func (m CaptureManifest) VerifyContext(ctx context.Context, root string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.Validate(); err != nil {
		return err
	}
	members := make([]Member, len(m.Members))
	for i, member := range m.Members {
		members[i] = Member(member)
	}
	return verifyManifestMembers(ctx, root, members)
}

func ReadCaptureManifestContext(ctx context.Context, filename string) (CaptureManifest, error) {
	if err := ctx.Err(); err != nil {
		return CaptureManifest{}, err
	}
	data, err := readManifestDocument(ctx, filename)
	if err != nil {
		return CaptureManifest{}, err
	}
	m, err := DecodeCaptureManifest(data)
	if err != nil {
		return CaptureManifest{}, err
	}
	if err := ctx.Err(); err != nil {
		return CaptureManifest{}, err
	}
	return m, nil
}

// CheckManifestContext preserves the legacy declaration contract and dispatches
// versioned cold manifests explicitly. Missing or unknown versioned fields must
// never cause fallback to weaker legacy validation.
func CheckManifestContext(ctx context.Context, filename, root string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := readManifestDocument(ctx, filename)
	if err != nil {
		return nil, err
	}
	var envelope map[string]json.RawMessage
	if err := wire.Decode(data, &envelope); err != nil || envelope == nil {
		return nil, domain.Fail("INVALID_INPUT", "invalid recovery manifest JSON")
	}
	report := map[string]any{"verification": "manifest-checked", "membersChecked": root != "", "completeCaptureVerified": false, "independentRecoveryVerified": false, "bootTested": false}
	if _, versioned := envelope["kind"]; versioned {
		m, err := DecodeCaptureManifest(data)
		if err != nil {
			return nil, err
		}
		if root != "" {
			if err := m.VerifyContext(ctx, root); err != nil {
				return nil, err
			}
		}
		report["manifestKind"], report["manifestVersion"] = m.Kind, m.Version
	} else {
		var m Manifest
		if err := wire.Decode(data, &m); err != nil {
			return nil, domain.Fail("INVALID_INPUT", "invalid legacy recovery manifest JSON")
		}
		if err := m.Validate(); err != nil {
			return nil, err
		}
		if root != "" {
			if err := m.VerifyContext(ctx, root); err != nil {
				return nil, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return report, nil
}
