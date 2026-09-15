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
	if _, handed, err := HandedOver(db, operationID); err != nil {
		return Artifact{}, "", err
	} else if handed {
		return Artifact{}, "", domain.Fail("SOURCE_CHANGED", "this prepared copy was handed over to a new VM; import the original again")
	}
	return approved(ctx, db, uid, operationID, true)
}

// Receipt returns a preparation's durable receipt without re-reading its files.
// Only the creation its prepared copy was handed over to may rely on it, because
// that creation verifies every new volume against the same digests (ADR 0060).
func Receipt(ctx context.Context, db *store.Store, uid uint32, operationID string) (Artifact, string, error) {
	return approved(ctx, db, uid, operationID, false)
}

func approved(ctx context.Context, db *store.Store, uid uint32, operationID string, verify bool) (Artifact, string, error) {
	var empty Artifact
	j, err := db.Job(operationID)
	if err != nil {
		return empty, "", err
	}
	p, input, err := db.Plan(j.PlanID)
	if err != nil {
		return empty, "", err
	}
	if p.ActorUID != uid || (p.Operation != "import.prepare" && p.Operation != "import.prepare-disks" && p.Operation != "import.prepare-install") || j.State != "succeeded" {
		return empty, "", domain.Fail("INVALID_INPUT", "source must be your successful local import preparation operation")
	}
	var destination, kind string
	if p.Operation == "import.prepare-install" {
		var in installationInput
		if err = wire.Decode(input, &in); err != nil {
			return empty, "", err
		}
		destination = in.Destination
		kind = "PreparedInstallation"
	} else if p.Operation == "import.prepare-disks" {
		var in diskSetInput
		if err = wire.Decode(input, &in); err != nil {
			return empty, "", err
		}
		destination = in.Destination
		kind = "PreparedDiskSet"
	} else {
		var in stageInput
		if err = wire.Decode(input, &in); err != nil {
			return empty, "", err
		}
		destination = in.Destination
		kind = "PreparedImport"
	}
	var expected Artifact
	stored, err := db.MetadataBytes("import-artifact", p.ID)
	if err != nil {
		return empty, "", err
	}
	if err = wire.Decode(stored, &expected); err != nil {
		return empty, "", err
	}
	actual := expected
	if verify {
		if actual, err = Verify(ctx, destination); err != nil {
			return empty, "", err
		}
	}
	a, err := operations.Canonical(actual)
	if err != nil {
		return empty, "", err
	}
	b, err := operations.Canonical(expected)
	if err != nil {
		return empty, "", err
	}
	if !bytes.Equal(a, b) || actual.Kind != kind || actual.PlanID != p.ID || actual.OperationID != operationID || actual.InputDigest != p.InputDigest {
		return empty, "", domain.Fail("SOURCE_CHANGED", "prepared artifact differs from its durable conversion receipt")
	}
	return actual, destination, nil
}
