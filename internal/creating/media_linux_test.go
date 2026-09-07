//go:build linux && amd64

package creating

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/importing"
	"virmill.local/core/internal/store"
)

func creationMediaFixture(t *testing.T) (*Service, *fixtureBackend, app.Request) {
	t.Helper()
	s, backend, r, dir := creationFixture(t)
	load := s.LoadSource
	a, _, err := load(context.Background(), s.Store, 1000, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately synthetic coordinator media bytes, not a bootable ISO.
	media := make([]byte, 40*2048)
	copy(media, []byte("synthetic installation media"))
	if err = os.WriteFile(filepath.Join(dir, "installer.iso"), media, 0400); err != nil {
		t.Fatal(err)
	}
	a.Kind = "PreparedInstallation"
	a.Media = []importing.PreparedMedia{{SourceID: "installer", Path: "installer.iso", Format: "raw", FileBytes: int64(len(media)), SHA256: hashFixture(media)}}
	s.LoadSource = func(context.Context, *store.Store, uint32, string) (importing.Artifact, string, error) {
		return a, dir, nil
	}
	hardware := r.Input["hardware"].(map[string]any)
	for _, d := range hardware["disks"].([]any) {
		disk := d.(map[string]any)
		disk["bootOrder"] = disk["bootOrder"].(float64) + 1
	}
	hardware["media"] = []any{map[string]any{"sourceID": "installer", "bus": "sata", "bootOrder": 1}}
	return s, backend, r
}
func TestCreationCarriesReadonlyMediaThroughDurableVolumeSet(t *testing.T) {
	s, backend, r := creationMediaFixture(t)
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ack := range p.Acknowledgements {
		found = found || ack == "attach-readonly-media"
	}
	if !found {
		t.Fatal("media effect unacknowledged")
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatal(j)
	}
	receipt, err := s.load(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Volumes) != 3 || !receipt.VolumesVerified || !receipt.Defined || receipt.GuestBootVerified || backend.allocated != 3 || backend.populated != 3 || backend.definitions != 1 {
		t.Fatal("media omitted or success claim incorrect", receipt)
	}
	v := receipt.Volumes[2]
	if v.Intent.ContentType != "cdrom-iso" || v.Intent.FileBytes != 40*2048 || v.Intent.VirtualBytes != 40*2048 || v.Intent.SourceID != "installer" || !v.Verified {
		t.Fatal("media receipt differs", v)
	}
}
func TestCreationRequiresEveryPreparedMediaMapping(t *testing.T) {
	for _, mode := range []string{"omit", "wrong", "duplicate"} {
		s, _, r := creationMediaFixture(t)
		hardware := r.Input["hardware"].(map[string]any)
		switch mode {
		case "omit":
			delete(hardware, "media")
		case "wrong":
			hardware["media"].([]any)[0].(map[string]any)["sourceID"] = "other"
		case "duplicate":
			hardware["media"] = append(hardware["media"].([]any), hardware["media"].([]any)[0])
		}
		if _, err := s.Plan(context.Background(), 1000, r); err == nil {
			t.Fatal("incomplete media mapping accepted", mode)
		}
	}
}
func TestCreationMediaIsRetainedForDefinitionFailureRecovery(t *testing.T) {
	s, backend, r := creationMediaFixture(t)
	backend.fail = "define"
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "recovery-required" {
		t.Fatal(j)
	}
	receipt, err := s.load(p.ID)
	if err != nil || len(receipt.Volumes) != 3 || !receipt.VolumesVerified || receipt.Defined {
		t.Fatal(receipt, err)
	}
	for _, v := range receipt.Volumes {
		if !v.Verified || v.Allocated == nil {
			t.Fatal("incomplete volume set marked verified", v)
		}
	}
	if _, err = s.Engine.Reconcile(context.Background(), j.ID); err == nil {
		t.Fatal("missing definition became success")
	}
	backend.mu.Lock()
	allocated, populated := backend.allocated, backend.populated
	backend.mu.Unlock()
	if allocated != 3 || populated != 3 {
		t.Fatal("recovery replayed volume effects")
	}
	backend.mu.Lock()
	backend.fail = ""
	backend.mu.Unlock()
	resume, err := s.PlanResume(context.Background(), 1000, app.Request{Connection: "fixture", ID: j.ID})
	if err != nil {
		t.Fatal(err)
	}
	child := awaitCreation(t, s, applyCreation(t, s, resume).ID)
	if child.State != "succeeded" || child.RecoveryOf != j.ID {
		t.Fatal(child)
	}
	if backend.allocated != 3 || backend.populated != 3 || !backend.defined || len(backend.target.Spec.Media) != 1 {
		t.Fatal("media recovery lost mapping or replayed effects")
	}
}
func hashFixture(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func TestMediaTransferFailureKeepsPartialSetAndRefusesDefinitionResume(t *testing.T) {
	s, backend, r := creationMediaFixture(t)
	backend.fail = "populate-media"
	p, err := s.Plan(context.Background(), 1000, r)
	if err != nil {
		t.Fatal(err)
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "recovery-required" {
		t.Fatal(j)
	}
	receipt, err := s.load(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Volumes) != 3 || !receipt.Volumes[0].Verified || !receipt.Volumes[1].Verified || receipt.Volumes[2].Allocated == nil || receipt.Volumes[2].Verified || receipt.VolumesVerified || backend.definitions != 0 {
		t.Fatal("partial media transfer incorrectly completed", receipt)
	}
	if _, err = s.PlanResume(context.Background(), 1000, app.Request{Connection: "fixture", ID: j.ID}); err == nil {
		t.Fatal("unverified media allowed definition resume")
	}
}
