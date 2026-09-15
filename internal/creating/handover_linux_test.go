//go:build linux && amd64

package creating

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/importing"
	"virmill.local/core/internal/store"
)

// handOverFixture is the creation fixture with a pool on the prepared copy's
// filesystem, a request that asks for the hand-over, and a receipt loader that
// does not re-read the prepared files.
func handOverFixture(t *testing.T) (*Service, *fixtureBackend, domain.Plan, map[string][]byte, string) {
	t.Helper()
	if !canRelease(os.TempDir()) {
		t.Skip("the test filesystem cannot release file ranges")
	}
	s, backend, r, dir := creationFixture(t)
	backend.poolPath = t.TempDir()
	artifact, directory, err := s.LoadSource(context.Background(), s.Store, 1000, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.LoadReceipt = func(context.Context, *store.Store, uint32, string) (importing.Artifact, string, error) {
		return artifact, directory, nil
	}
	originals := map[string][]byte{}
	for _, name := range []string{"boot", "data"} {
		if originals[name], err = os.ReadFile(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	r.Input["preparedCopy"] = "hand-over"
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	return s, backend, p, originals, dir
}

func TestHandOverReleasesThePreparedCopyWhileCopying(t *testing.T) {
	s, backend, p, originals, dir := handOverFixture(t)
	if !slices.Contains(p.Acknowledgements, "hand-over-prepared-copy") {
		t.Fatalf("acknowledgements %v lack the hand-over", p.Acknowledgements)
	}
	in, _ := estimateRecipe(t, s, p)
	// Only the reserve and 16 MiB headroom per disk: the copy's space is reused.
	if in.HandOver == nil || in.RequiredBytes != 64<<20+2*(16<<20) {
		t.Fatalf("hand-over %+v, budget %d", in.HandOver, in.RequiredBytes)
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	receipt, err := s.load(p.ID)
	if err != nil || !receipt.Defined || !receipt.VolumesVerified {
		t.Fatal("creation incomplete", receipt, err)
	}
	record, handed, err := importing.HandedOver(s.Store, in.SourceOperationID)
	if err != nil || !handed || record.CreationPlanID != p.ID {
		t.Fatalf("hand-over record %+v %v %v", record, handed, err)
	}
	for _, v := range receipt.Volumes {
		name := v.Intent.SourceID
		backend.mu.Lock()
		copied := backend.volumes[v.Intent.Name]
		backend.mu.Unlock()
		if !bytes.Equal(copied, originals[name]) {
			t.Fatalf("volume %s does not hold the prepared bytes", v.Intent.Name)
		}
		released, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(released) != len(originals[name]) || !bytes.Equal(released, make([]byte, len(released))) {
			t.Fatalf("prepared %s was not released: %q %v", name, released, err)
		}
	}
	// Only this creation may rely on the receipt of the copy it took over.
	other := p
	other.ID = domain.ID()
	if err = s.checkSource(context.Background(), other, in); err == nil {
		t.Fatal("another creation used the handed-over prepared copy")
	}
}

func TestHandOverNeedsThePoolOnTheSameFilesystem(t *testing.T) {
	if !canRelease(os.TempDir()) {
		t.Skip("the test filesystem cannot release file ranges")
	}
	s, _, r, _ := creationFixture(t) // its pool reports no folder
	r.Input["preparedCopy"] = "hand-over"
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	in, _ := estimateRecipe(t, s, p)
	if in.HandOver != nil || slices.Contains(p.Acknowledgements, "hand-over-prepared-copy") || in.RequiredBytes != 64<<20+2*(16<<20)+78 {
		t.Fatalf("hand-over %+v, acks %v, budget %d", in.HandOver, p.Acknowledgements, in.RequiredBytes)
	}
}
