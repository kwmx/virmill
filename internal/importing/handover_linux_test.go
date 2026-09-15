//go:build linux && amd64

package importing

import (
	"context"
	"path/filepath"
	"testing"
)

// A prepared copy handed over to a VM's disks may be partly released, so it is
// never approved for another VM, while its creation can still read the receipt.
func TestAHandedOverPreparationIsNeverApprovedAgain(t *testing.T) {
	s := serviceFixture(t, phaseTool{})
	dest := filepath.Join(t.TempDir(), "prepared")
	p, err := s.Plan(context.Background(), 1000, stageRequest(fixtureArchive(t, false), dest))
	if err != nil {
		t.Fatal(err)
	}
	j := finish(t, s, accepted(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	if _, _, err = Approved(context.Background(), s.Store, 1000, j.ID); err != nil {
		t.Fatal(err)
	}
	if err = RecordHandOver(s.Store, j.ID, HandOver{Version: 1, CreationPlanID: "creation-plan", CreationOperationID: "creation-job"}); err != nil {
		t.Fatal(err)
	}
	if err = RecordHandOver(s.Store, j.ID, HandOver{Version: 1, CreationPlanID: "second-plan", CreationOperationID: "second-job"}); err == nil {
		t.Fatal("a preparation was handed over twice")
	}
	if _, _, err = Approved(context.Background(), s.Store, 1000, j.ID); err == nil {
		t.Fatal("a handed-over preparation was approved for another VM")
	}
	artifact, directory, err := Receipt(context.Background(), s.Store, 1000, j.ID)
	if err != nil || directory != dest || artifact.OperationID != j.ID {
		t.Fatalf("receipt %+v %q %v", artifact, directory, err)
	}
}
