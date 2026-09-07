//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"fmt"
	native "libvirt.org/go/libvirt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
)

func TestCleanupXMLProtectsAllReferencedStorage(t *testing.T) {
	selected := map[string]bool{"/pool/new.qcow2": true}
	volumes := map[string]cleanupGraphVolume{"/pool/kept.qcow2": {path: "/pool/kept.qcow2"}}
	byName := map[string]string{"pool|new.qcow2": "/pool/new.qcow2", "pool|kept.qcow2": "/pool/kept.qcow2"}
	for _, x := range []string{
		`<domain><devices><disk><source file="/pool/new.qcow2"/></disk></devices></domain>`,
		`<domain><devices><disk><source pool="pool" volume="new.qcow2"/></disk></devices></domain>`,
		`<domainsnapshot><disks><disk><source file="/pool/new.qcow2"/></disk></disks></domainsnapshot>`,
		`<domain><devices><disk><source file="/pool/kept.qcow2"/><backingStore><source file="/pool/new.qcow2"/></backingStore></disk></devices></domain>`,
		`<domain><os><nvram>/pool/new.qcow2</nvram></os></domain>`,
		`<domain><metadata><vendor path="/pool/new.qcow2"/></metadata></domain>`,
		`<domain><devices><filesystem><source dir="/pool"/></filesystem></devices></domain>`,
		`<domain><devices><disk><source file="/outside/unresolved.qcow2"/></disk></devices></domain>`,
		`<domain><devices><disk><source protocol="rbd" name="unknown"/></disk></devices></domain>`,
		`<domain><devices><disk><source dev="/dev/unapproved"/></disk></devices></domain>`,
		`<domain><os><nvram>/outside/state.qcow2</nvram></os></domain>`,
		`<domain><devices><disk><source pool="unknown" volume="unresolved"/></disk></devices></domain>`,
	} {
		if err := checkCleanupXML(x, selected, volumes, byName); err == nil {
			t.Fatal("reference/unknown accepted", x)
		}
	}
	if err := checkCleanupXML(`<domain><devices><disk><source pool="pool" volume="kept.qcow2"/></disk><interface><source bridge="br-fixture"/></interface></devices></domain>`, selected, volumes, byName); err != nil {
		t.Fatal(err)
	}
	for _, reference := range []string{"https://host/image", "json:{bad}", `..\outside`} {
		if _, err := cleanupBackingPath("/pool/child", reference, "raw"); err == nil {
			t.Fatal("nonlocal reference accepted")
		}
	}
	if got, err := cleanupBackingPath("/pool/child", "../base.raw", "raw"); err != nil || got != "/base.raw" {
		t.Fatal(got, err)
	}
	if _, err := (&Provider{}).InspectCreationCleanup(context.Background(), "test:///default", domain.CreationSpec{}, nil, true); err == nil {
		t.Fatal("runtime test driver enabled")
	}
}

func TestRealFileCleanupGraphWithNativeSimulatedInventory(t *testing.T) {
	if os.Getenv("VIRMILL_TEST_DISK_TOOLS") != "1" {
		t.Skip("explicit generated files, native test-driver inventory and confined QEMU metadata required")
	}
	c, err := native.NewConnect("test:///default")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// These are process-local libvirt TEST-driver objects. No host connection,
	// qemu domain, mounted pool, storage helper, or native host deletion is used.
	domains, err := c.ListAllDomains(0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range domains {
		if err = domains[i].Destroy(); err != nil {
			t.Fatal(err)
		}
		if err = domains[i].Undefine(); err != nil {
			t.Fatal(err)
		}
		domains[i].Free()
	}
	pools, err := c.ListAllStoragePools(0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range pools {
		if err = pools[i].Destroy(); err != nil {
			t.Fatal(err)
		}
		if err = pools[i].Undefine(); err != nil {
			t.Fatal(err)
		}
		pools[i].Free()
	}
	dir := t.TempDir()
	target, intents := creationFixture()
	p, err := c.StoragePoolDefineXML(fmt.Sprintf(`<pool type="dir"><name>cleanup-fixture</name><uuid>%s</uuid><target><path>%s</path></target></pool>`, target.Spec.PoolID, xmlText(dir)), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Free()
	if err = p.Create(0); err != nil {
		t.Fatal(err)
	}
	qemu := func(args ...string) {
		t.Helper()
		cmd := exec.Command("/usr/bin/qemu-img", args...)
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(string(b), e)
		}
	}
	addVolume := func(name, format string) *native.StorageVol {
		t.Helper()
		v, e := p.StorageVolCreateXML(fmt.Sprintf(`<volume><name>%s</name><capacity unit="bytes">8388608</capacity><target><format type="%s"/></target></volume>`, xmlText(name), format), 0)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	intent := intents[0].Intent
	v := addVolume(intent.Name, "raw")
	defer v.Free()
	path, err := v.GetPath()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(8 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	identity, err := fileidentity.Observe(path, false)
	if err != nil {
		t.Fatal(err)
	}
	key, err := v.GetKey()
	if err != nil {
		t.Fatal(err)
	}
	allocated := domain.CreatedVolume{Intent: intent, BackendKey: key, Path: path, Generation: identity.Generation}
	candidates := []domain.CleanupCandidate{{Intent: intent, Allocated: &allocated}}
	kept := addVolume(intents[1].Intent.Name, "qcow2")
	defer kept.Free()
	keepPath, err := kept.GetPath()
	if err != nil {
		t.Fatal(err)
	}
	qemu("create", "-f", "qcow2", keepPath, "8M")
	proof, err := inspectCreationCleanup(context.Background(), c, "test:///default", target.Spec, candidates, true)
	if err != nil {
		t.Fatal(err)
	}
	if proof.GraphDigest == "" || proof.Volumes[0].State != "present" || len(proof.ResourceIDs) != 1 {
		t.Fatal("incomplete positive graph", proof)
	}
	qemu("rebase", "-u", "-f", "qcow2", "-b", path, "-F", "raw", keepPath)
	keepIdentity, e := fileidentity.Observe(keepPath, false)
	if e != nil {
		t.Fatal(e)
	}
	keepKey, e := kept.GetKey()
	if e != nil {
		t.Fatal(e)
	}
	both := append(append([]domain.CleanupCandidate{}, candidates...), domain.CleanupCandidate{Intent: intents[1].Intent, Allocated: &domain.CreatedVolume{Intent: intents[1].Intent, Path: keepPath, BackendKey: keepKey, Generation: keepIdentity.Generation}})
	if _, e = inspectCreationCleanup(context.Background(), c, "test:///default", target.Spec, both, true); e == nil {
		t.Fatal("a candidate referencing another candidate was ignored")
	}

	if _, err = inspectCreationCleanup(context.Background(), c, "test:///default", target.Spec, candidates, true); err == nil || !strings.Contains(err.Error(), "references") {
		t.Fatal("real qcow backing reference was not protected", err)
	}
	qemu("rebase", "-u", "-f", "qcow2", "-b", "", keepPath)
	current, err := inspectCreationCleanup(context.Background(), c, "test:///default", target.Spec, candidates, true)
	if err != nil {
		t.Fatal(err)
	}
	if current.GraphDigest == proof.GraphDigest {
		t.Fatal("changed file metadata did not invalidate graph")
	}
	unknown := filepath.Join(dir, "unlisted")
	if err = os.WriteFile(unknown, []byte("not registered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = inspectCreationCleanup(context.Background(), c, "test:///default", target.Spec, candidates, true); err == nil {
		t.Fatal("unlisted pool file accepted")
	}
	if err = os.Remove(unknown); err != nil {
		t.Fatal(err)
	}
	// Remove only the generated fixture and the simulated inventory entry. This
	// cannot certify the qemu storage driver's actual Delete API or durability.
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = v.Delete(0); err != nil {
		t.Fatal(err)
	}
	after, err := inspectCreationCleanup(context.Background(), c, "test:///default", target.Spec, candidates, true)
	if err != nil {
		t.Fatal(err)
	}
	if after.GraphDigest != current.GraphDigest || after.Volumes[0].State != "absent" {
		t.Fatal("own candidate removal changed unrelated graph", after, current)
	}
	replacement := addVolume(intent.Name, "raw")
	defer replacement.Free()
	if err = os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	observed, err := inspectCreationCleanup(context.Background(), c, "test:///default", target.Spec, candidates, false)
	if err != nil || observed.Volumes[0].State != "unknown" {
		t.Fatal("same-name replacement acquired ownership", observed, err)
	}
	if _, err = inspectCreationCleanup(context.Background(), c, "test:///default", target.Spec, candidates, true); err == nil {
		t.Fatal("replacement file became deletable")
	}
	t.Log("real statx/bubblewrap/QEMU metadata plus in-memory libvirt test inventory; no native host deletion, guest boot or hardware evidence")
}
