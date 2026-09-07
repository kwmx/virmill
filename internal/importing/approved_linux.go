//go:build linux && amd64

package importing

import (
	"bytes"
	"context"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/wire"
)

// Approved locates a successful local preparation and checks the artifact against
// its coordinator receipt. An arbitrary user-supplied hash manifest is not proof
// that untrusted images passed the production confined conversion adapter.
func Approved(ctx context.Context, db *store.Store, uid uint32, operationID string) (Artifact, string, error) {
	var empty Artifact
	j, err := db.Job(operationID)
	if err != nil {
		return empty, "", err
	}
	p, input, err := db.Plan(j.PlanID)
	if err != nil {
		return empty, "", err
	}
	if p.ActorUID != uid || p.Operation != "import.prepare" || j.State != "succeeded" {
		return empty, "", domain.Fail("INVALID_INPUT", "source must be your successful local import preparation operation")
	}
	var in stageInput
	if err = wire.Decode(input, &in); err != nil {
		return empty, "", err
	}
	actual, err := Verify(ctx, in.Destination)
	if err != nil {
		return empty, "", err
	}
	var expected Artifact
	stored, err := db.MetadataBytes("import-artifact", p.ID)
	if err != nil {
		return empty, "", err
	}
	if err = wire.Decode(stored, &expected); err != nil {
		return empty, "", err
	}
	a, err := operations.Canonical(actual)
	if err != nil {
		return empty, "", err
	}
	b, err := operations.Canonical(expected)
	if err != nil {
		return empty, "", err
	}
	if !bytes.Equal(a, b) || actual.PlanID != p.ID || actual.OperationID != operationID || actual.InputDigest != p.InputDigest {
		return empty, "", domain.Fail("SOURCE_CHANGED", "prepared artifact differs from its durable conversion receipt")
	}
	return actual, in.Destination, nil
}
