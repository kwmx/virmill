//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"virmill.local/core/internal/backend/fileidentity"
	imageTool "virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
)

type externalFixture struct {
	t          *testing.T
	dir, pool  string
	selected   string
	kept       string
	candidates []domain.CleanupCandidate
	volumes    map[string]cleanupGraphVolume
	backing    map[string]imageTool.Info
	inspected  []string
}

// newExternalFixture lays out a pool directory with one selected volume and one
// kept volume. Files outside it stand for other guests' disks and firmware.
func newExternalFixture(t *testing.T) *externalFixture {
	dir := t.TempDir()
	f := &externalFixture{t: t, dir: dir, pool: filepath.Join(dir, "pool"), backing: map[string]imageTool.Info{}}
	for _, folder := range []string{f.pool, filepath.Join(dir, "images")} {
		if err := os.Mkdir(folder, 0700); err != nil {
			t.Fatal(err)
		}
	}
	f.selected, f.kept = filepath.Join(f.pool, "new.qcow2"), filepath.Join(f.pool, "kept.qcow2")
	f.write(f.selected, "QFI\xfbselected")
	f.write(f.kept, "QFI\xfbkept")
	identity, err := fileidentity.Observe(f.selected, false)
	if err != nil {
		t.Fatal(err)
	}
	f.candidates = []domain.CleanupCandidate{{Allocated: &domain.CreatedVolume{Path: f.selected, Generation: identity.Generation}}, {Allocated: &domain.CreatedVolume{Path: filepath.Join(f.pool, "gone.qcow2")}}}
	f.volumes = map[string]cleanupGraphVolume{f.selected: {path: f.selected}, f.kept: {path: f.kept}}
	return f
}

func (f *externalFixture) write(path, content string) string {
	f.t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func (f *externalFixture) image(name, content string) string {
	return f.write(filepath.Join(f.dir, "images", name), content)
}

func (f *externalFixture) check(refs cleanupExternal) (map[string]cleanupExternalObservation, error) {
	f.inspected = nil
	return checkCleanupExternal(context.Background(), refs, f.candidates, map[string]bool{f.selected: true}, f.volumes, func(_ context.Context, file *os.File) (imageTool.Info, error) {
		f.inspected = append(f.inspected, file.Name())
		return f.backing[file.Name()], nil
	})
}

func TestCleanupExternalAcceptsUnrelatedOutOfPoolFiles(t *testing.T) {
	f := newExternalFixture(t)
	raw := f.image("other.qcow2", "QFI\xfbanother guest")
	nested := filepath.Join(f.dir, "images", "guest")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	child := f.write(filepath.Join(nested, "fedora.qcow2"), "QFI\xfbchild")
	base := f.image("base.qcow2", "QFI\xfbbase")
	f.backing[child] = imageTool.Info{Backing: "../base.qcow2", BackingFormat: "qcow2"}
	onPool := f.image("on-pool.qcow2", "QFI\xfbuses a kept pool volume")
	f.backing[onPool] = imageTool.Info{Backing: f.kept, BackingFormat: "qcow2"}
	plain := f.image("plain.img", "no qcow2 header")
	if err := os.Symlink(raw, filepath.Join(f.dir, "images", "linked.qcow2")); err != nil {
		t.Fatal(err)
	}
	refs := cleanupExternal{
		raw:    {"raw": true},
		child:  {"qcow2": true},
		onPool: {"qcow2": true},
		plain:  {"": true},
		filepath.Join(f.dir, "images", "linked.qcow2"):          {"qcow2": true},
		filepath.Join(f.dir, "images", "never-started_VARS.fd"): {"raw": true},
		filepath.Join(f.dir, "images", "removed.iso"):           {"raw": true, "": true},
		filepath.Join(plain, "not-a-directory"):                 {"qcow2": true},
	}
	observed, err := f.check(refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != len(refs) || !observed[filepath.Join(f.dir, "images", "never-started_VARS.fd")].Absent || observed[raw].Absent {
		t.Fatal("observations incomplete", observed)
	}
	if links := observed[child].Backing; len(links) != 1 || links[0].Path != base || links[0].Absent {
		t.Fatal("backing chain outside pools not followed", observed[child])
	}
	if links := observed[onPool].Backing; len(links) != 1 || links[0].Path != f.kept {
		t.Fatal("chain did not stop at the reconciled pool volume", observed[onPool])
	}
	for _, name := range f.inspected {
		if name == plain || name == raw {
			t.Fatal("raw or headerless file inspected as an image", name)
		}
	}
	again, err := f.check(refs)
	if err != nil || inventoryDigest(again) != inventoryDigest(observed) {
		t.Fatal("unchanged files changed the graph", err)
	}
	// The replacement exists before the rename, so it cannot reuse the old inode.
	if err = os.Rename(f.image("replacement.qcow2", "QFI\xfbreplaced"), base); err != nil {
		t.Fatal(err)
	}
	if replaced, e := f.check(refs); e != nil || inventoryDigest(replaced) == inventoryDigest(observed) {
		t.Fatal("replaced backing file did not change the graph", e)
	}
}

func TestCleanupExternalRefusesAliasesOfSelectedVolume(t *testing.T) {
	f := newExternalFixture(t)
	link := filepath.Join(f.dir, "images", "alias.qcow2")
	if err := os.Symlink(f.selected, link); err != nil {
		t.Fatal(err)
	}
	poolLink := filepath.Join(f.dir, "images", "pool-link")
	if err := os.Symlink(f.pool, poolLink); err != nil {
		t.Fatal(err)
	}
	hard := filepath.Join(f.pool, "..", "hard.qcow2")
	if err := os.Link(f.selected, hard); err != nil {
		t.Fatal(err)
	}
	backedBySelected := f.image("child.qcow2", "QFI\xfbchild")
	f.backing[backedBySelected] = imageTool.Info{Backing: f.selected, BackingFormat: "qcow2"}
	backedByAlias := f.image("alias-child.qcow2", "QFI\xfbchild")
	f.backing[backedByAlias] = imageTool.Info{Backing: "alias.qcow2", BackingFormat: "qcow2"}
	undeclared := f.image("undeclared.img", "QFI\xfbprobed")
	f.backing[undeclared] = imageTool.Info{Backing: "../pool/new.qcow2", BackingFormat: "qcow2"}
	deep := f.image("deep.qcow2", "QFI\xfbdeep")
	middle := f.image("middle.qcow2", "QFI\xfbmiddle")
	f.backing[deep] = imageTool.Info{Backing: middle, BackingFormat: "qcow2"}
	f.backing[middle] = imageTool.Info{Backing: "pool-link/new.qcow2", BackingFormat: "raw"}
	for _, ref := range []struct {
		path, format string
	}{
		{link, "raw"},
		{filepath.Join(poolLink, "new.qcow2"), "raw"},
		{f.pool + "/../pool/new.qcow2", "qcow2"},
		{f.pool + "//new.qcow2", "raw"},
		{f.pool + "/./new.qcow2", ""},
		{filepath.Clean(hard), "raw"},
		{"/proc/self/root" + f.selected, "raw"},
		{backedBySelected, "qcow2"},
		{backedByAlias, "qcow2"},
		{undeclared, ""},
		{deep, "qcow2"},
	} {
		_, err := f.check(cleanupExternal{ref.path: {ref.format: true}, filepath.Join(f.dir, "absent"): {"raw": true}})
		var failure *domain.Error
		if err == nil || !errors.As(err, &failure) || failure.Code != "RESOURCE_BUSY" {
			t.Fatal("alias of the selected volume not refused", ref, err)
		}
	}
}

func TestCleanupExternalRefusesWhatItCannotObserve(t *testing.T) {
	f := newExternalFixture(t)
	cycleA, cycleB := f.image("a.qcow2", "QFI\xfba"), f.image("b.qcow2", "QFI\xfbb")
	f.backing[cycleA] = imageTool.Info{Backing: cycleB, BackingFormat: "qcow2"}
	f.backing[cycleB] = imageTool.Info{Backing: cycleA, BackingFormat: "qcow2"}
	remote := f.image("remote.qcow2", "QFI\xfbremote")
	f.backing[remote] = imageTool.Info{Backing: "nbd://host/export", BackingFormat: "raw"}
	vmdk := f.image("disk.vmdk", "KDMV")
	cases := map[string]cleanupExternal{
		"cycle":            {cycleA: {"qcow2": true}},
		"remote backing":   {remote: {"qcow2": true}},
		"other format":     {vmdk: {"vmdk": true}},
		"device as qcow2":  {"/dev/null": {"qcow2": true}},
		"symlink loop":     {filepath.Join(f.dir, "images", "loop"): {"raw": true}},
		"relative":         {"images/relative.qcow2": {"raw": true}},
		"undeclared fifo":  {filepath.Join(f.dir, "images", "fifo"): {"": true}},
		"declared chained": {f.image("chain.qcow2", "QFI\xfbchain"): {"qcow2": true}},
	}
	if err := os.Symlink("loop", filepath.Join(f.dir, "images", "loop")); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(filepath.Join(f.dir, "images", "fifo"), 0600); err != nil {
		t.Fatal(err)
	}
	chain := filepath.Join(f.dir, "images", "chain.qcow2")
	previous := chain
	for i := 0; i < cleanupExternalChainDepth; i++ {
		next := f.image("chain-"+strings.Repeat("x", i+1)+".qcow2", "QFI\xfbnext")
		f.backing[previous] = imageTool.Info{Backing: next, BackingFormat: "qcow2"}
		previous = next
	}
	if os.Geteuid() != 0 {
		hidden := filepath.Join(f.dir, "hidden")
		if err := os.Mkdir(hidden, 0700); err != nil {
			t.Fatal(err)
		}
		unreadable := f.write(filepath.Join(f.dir, "images", "root-only.qcow2"), "QFI\xfbsecret")
		if err := os.Chmod(unreadable, 0); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(hidden, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(hidden, 0700) })
		cases["unsearchable directory"] = cleanupExternal{filepath.Join(hidden, "fedora.qcow2"): {"raw": true}}
		cases["unreadable qcow2"] = cleanupExternal{unreadable: {"qcow2": true}}
		cases["unreadable undeclared"] = cleanupExternal{unreadable: {"": true}}
	}
	for name, refs := range cases {
		if _, err := f.check(refs); err == nil {
			t.Fatal(name, "accepted without a complete observation")
		}
	}
	bad := append([]domain.CleanupCandidate{}, f.candidates...)
	bad[0].Allocated = &domain.CreatedVolume{Path: f.selected, Generation: "path-only"}
	if _, err := checkCleanupExternal(context.Background(), cleanupExternal{}, bad, nil, f.volumes, nil); err == nil {
		t.Fatal("unparseable selected generation accepted")
	}
}
