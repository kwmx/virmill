//go:build linux && amd64

package creating

import (
	"context"
	"strings"
	"testing"
	"virmill.local/core/internal/domain"
)

func TestManagedOwnershipRequiresBothJournalAndNativeIdentity(t *testing.T) {
	s, _, request, _ := creationFixture(t)
	p, err := s.Plan(context.Background(), 1000, request)
	if err != nil {
		t.Fatal(err)
	}
	j := awaitCreation(t, s, applyCreation(t, s, p).ID)
	if j.State != "succeeded" {
		t.Fatal(j.Error)
	}
	receipt, err := s.load(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	vm := domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "fixture", Kind: "vm", UUID: receipt.VMID}, Ownership: "external", Name: "arbitrary label"}
	vm.PersistentXML = `<domain><uuid>` + receipt.VMID + `</uuid><metadata><v:creation xmlns:v="urn:virmill:v1" apiVersion="virmill/v1" binding="` + receipt.Binding + `"/></metadata><vcpu>4</vcpu></domain>`
	got, err := s.Ownership(context.Background(), vm)
	if err != nil || got.Ownership != "managed" {
		t.Fatal("verified creation not cataloged", got, err)
	}
	vm.PersistentXML = strings.Replace(vm.PersistentXML, receipt.Binding, strings.Repeat("0", 64), 1)
	if _, err = s.Ownership(context.Background(), vm); err == nil {
		t.Fatal("replaced native identity accepted")
	}
	vm.Key.UUID = domain.ID()
	vm.Name = "virmill-looks-managed"
	got, err = s.Ownership(context.Background(), vm)
	if err != nil || got.Ownership != "external" {
		t.Fatal("name/metadata alone adopted external VM", got, err)
	}
	vm.Key.ConnectionID = "another-local-connection"
	got, err = s.Ownership(context.Background(), vm)
	if err != nil || got.Ownership != "external" {
		t.Fatal("cross-connection ownership collision", got, err)
	}
}
