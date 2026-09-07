//go:build linux && amd64

package importing

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"virmill.local/core/internal/operations"
)

func TestApprovedSourceRequiresCompletedOwnedJournalReceipt(t *testing.T) {
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
	artifact, directory, err := Approved(context.Background(), s.Store, 1000, j.ID)
	if err != nil || directory != dest {
		t.Fatal("approved source unavailable", err)
	}
	if _, _, err = Approved(context.Background(), s.Store, 1001, j.ID); err == nil {
		t.Fatal("another actor's source accepted")
	}
	// A self-consistent public manifest is not authority to bypass conversion.
	artifact.SourceSHA256 = strings.Repeat("f", 64)
	b, err := operations.Canonical(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(dest, "manifest.json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dest, "manifest.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(context.Background(), dest); err != nil {
		t.Fatal("fixture public manifest is not self-consistent", err)
	}
	if _, _, err = Approved(context.Background(), s.Store, 1000, j.ID); err == nil {
		t.Fatal("rewritten public receipt accepted as conversion authority")
	}
	j.State = "recovery-required"
	if err = s.Store.Update(j, "synthetic unconfirmed state"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Approved(context.Background(), s.Store, 1000, j.ID); err == nil {
		t.Fatal("uncertain preparation accepted")
	}
	t.Log("coordinator authority test with synthetic converter output, not an image-format or hardware test")
}

func TestPreparedReceiptRejectsFutureFieldsAtApproval(t *testing.T) {
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
	var receipt map[string]any
	if err = s.Store.Get("import-artifact", p.ID, &receipt); err != nil {
		t.Fatal(err)
	}
	receipt["futureAuthority"] = true
	// Private receipt decoding must reject fields unsupported by this binary too.
	if err = s.Store.Put("import-artifact", p.ID, receipt); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Approved(context.Background(), s.Store, 1000, j.ID); err == nil {
		t.Fatal("unknown private receipt authority silently ignored")
	}
}
