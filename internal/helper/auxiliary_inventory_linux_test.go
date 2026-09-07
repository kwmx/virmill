//go:build linux

package helper

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileaccess"
	"virmill.local/core/internal/domain"
)

type auxiliaryNativeFixture struct {
	observation domain.ColdStateInspection
	calls       int
	onCall      func(int)
	errAt       int
}

func (f *auxiliaryNativeFixture) InspectColdState(ctx context.Context, uri, id string) (domain.ColdStateInspection, error) {
	f.calls++
	if uri != "qemu:///system" || id != "11111111-2222-1333-8444-555555555555" {
		return domain.ColdStateInspection{}, errors.New("unexpected native fixture selection")
	}
	if f.onCall != nil {
		f.onCall(f.calls)
	}
	if f.calls == f.errAt {
		return domain.ColdStateInspection{}, errors.New("synthetic native observation failure")
	}
	return f.observation, nil
}

type auxiliaryFixture struct {
	root    string
	exec    AuxiliaryExecutor
	request Request
	policy  Policy
	native  *auxiliaryNativeFixture
}

func newAuxiliaryFixture(t *testing.T) *auxiliaryFixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "approved")
	for _, relative := range []string{"state/nvram", "state/tpm/nested"} {
		if err := os.MkdirAll(filepath.Join(root, relative), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for relative, payload := range map[string]string{
		"state/nvram/vars":           "synthetic confidential NVRAM bytes",
		"state/tpm/nested/permanent": "synthetic confidential TPM bytes",
		"state/tpm/empty":            "", "state/tpm/.lock": "", "state/tpm/.metadata": "synthetic opaque state",
	} {
		if err := os.WriteFile(filepath.Join(root, relative), []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
	}
	id := "11111111-2222-1333-8444-555555555555"
	fingerprint := strings.Repeat("a", 64)
	layout := domain.ColdStateLayout{VMID: id, Firmware: domain.ColdFirmware{Loader: "/unopened/firmware/code", LoaderType: "pflash", NVRAM: &domain.ColdNVRAM{Path: filepath.Join(root, "state/nvram/vars"), Format: "raw"}}, TPM: &domain.ColdTPM{Model: "tpm-crb", Version: "2.0", SourceType: "dir", SourcePath: filepath.Join(root, "state/tpm")}, SecretReferences: []string{}}
	native := &auxiliaryNativeFixture{observation: domain.ColdStateInspection{Persistent: true, State: "stopped", Resource: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: id}, Fingerprint: fingerprint, Layout: layout, Source: &domain.ColdSourceLayout{State: layout}}}
	request := Request{APIVersion: domain.APIVersion, ActorUID: 1000, KeyID: "synthetic-key", Operation: "state.auxiliary", ResourceID: id, RootID: "synthetic-root", Mode: "inspect", Auxiliary: &AuxiliaryRequest{Version: 1, Fingerprint: fingerprint}}
	policy := Policy{APIVersion: domain.APIVersion, Roots: map[string]string{request.RootID: root}, Auxiliary: []AuxiliaryPermission{{ActorUID: request.ActorUID, KeyID: request.KeyID, ResourceID: id, RootID: request.RootID, StateUID: uint32(os.Getuid()), StateGID: uint32(os.Getgid()), MaxBytes: 1 << 20, MaxMembers: MaxAuxiliaryMembers, AllowCapture: true}}}
	return &auxiliaryFixture{root: root, exec: AuxiliaryExecutor{Backend: native, rootOwnerUID: uint32(os.Getuid())}, request: request, policy: policy, native: native}
}

func auxiliaryMustRefuse(t *testing.T, f *auxiliaryFixture, ctx context.Context) error {
	t.Helper()
	out, err := f.exec.Inspect(ctx, f.request, f.policy)
	if err == nil || !reflect.DeepEqual(out, AuxiliaryInventory{}) {
		t.Fatalf("refusal retained metadata or returned success: version=%d members=%d directories=%d, %v", out.Version, len(out.Members), len(out.Directories), err)
	}
	return err
}

func TestAuxiliaryInventoryMetadataOnlyDeterministicAndPinned(t *testing.T) {
	f := newAuxiliaryFixture(t)
	nvram := filepath.Join(f.root, "state/nvram/vars")
	if err := os.Chmod(nvram, 0000); err != nil {
		t.Fatal(err)
	}
	var before unix.Stat_t
	if err := unix.Stat(nvram, &before); err != nil {
		t.Fatal(err)
	}
	out, err := f.exec.Inspect(context.Background(), f.request, f.policy)
	if err != nil {
		t.Fatal(err)
	}
	if f.native.calls != 3 || len(out.Members) != 4 || len(out.Directories) != 4 || out.TPMLock == nil || out.TPMLock.ID != "tpm-lock" || out.TPMLock.Kind != "tpm-lock" || out.TPMLock.State.Size != 0 {
		t.Fatal("native repetition, complete bounded set or separate control file lost", out, f.native.calls)
	}
	var total uint64
	zeroTPM := false
	for i, member := range out.Members {
		if member.ID != fmt.Sprintf("members/%03d", i) || i > 0 && out.Members[i-1].RelativePath >= member.RelativePath || member.RelativePath == "state/tpm/.lock" || !strings.HasPrefix(member.State.Generation, "linux-aux-statx-v1:") || member.State.UID != uint32(os.Getuid()) || member.State.GID != uint32(os.Getgid()) {
			t.Fatal("member identity, ordering or complete ownership omitted", member)
		}
		total += member.State.Size
		zeroTPM = zeroTPM || member.Kind == "tpm" && member.State.Size == 0
	}
	if total != out.TotalBytes || !zeroTPM {
		t.Fatal("empty TPM member or total omitted", out)
	}
	raw, err := json.Marshal(out)
	if err != nil || bytes.Contains(raw, []byte("synthetic confidential")) || bytes.Contains(raw, []byte("synthetic opaque state")) || len(raw) > auxiliaryMetadataLimit {
		t.Fatal("inventory exposed content or exceeded its envelope", err)
	}
	var after unix.Stat_t
	if err := unix.Stat(nvram, &after); err != nil || before.Atim != after.Atim || before.Mtim != after.Mtim || before.Ctim != after.Ctim || after.Mode&0777 != 0 {
		t.Fatal("metadata inspection read or changed the unreadable NVRAM member", err)
	}
	second, err := f.exec.Inspect(context.Background(), f.request, f.policy)
	if err != nil || !reflect.DeepEqual(out, second) {
		t.Fatal("unchanged observation was not deterministic", err)
	}
	for _, mode := range []string{"capture", "observe"} {
		f.request.Mode, f.request.Auxiliary.Expected = mode, &out
		if actual, err := f.exec.Inspect(context.Background(), f.request, f.policy); err != nil || !reflect.DeepEqual(actual, out) {
			t.Fatal("exact expected inventory refused for metadata reinspection", mode, err)
		}
	}
	copy := *out.TPMLock
	copy.State.Generation += "-forged"
	out.TPMLock = &copy
	err = auxiliaryMustRefuse(t, f, context.Background())
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != "STALE_PLAN" {
		t.Fatal("expected producer-lock generation was not bound", err)
	}
}

func TestAuxiliaryInventoryAuthorityRefusesBeforeNativeObservation(t *testing.T) {
	for name, edit := range map[string]func(*auxiliaryFixture){
		"operation":              func(f *auxiliaryFixture) { f.request.Operation = "storage.grant-read" },
		"contract":               func(f *auxiliaryFixture) { f.request.APIVersion = "unknown" },
		"policy contract":        func(f *auxiliaryFixture) { f.policy.APIVersion = "unknown" },
		"missing request":        func(f *auxiliaryFixture) { f.request.Auxiliary = nil },
		"foreign access payload": func(f *auxiliaryFixture) { f.request.Access = &AccessRequest{} },
		"fingerprint":            func(f *auxiliaryFixture) { f.request.Auxiliary.Fingerprint = "not-a-digest" },
		"unapproved root":        func(f *auxiliaryFixture) { f.request.RootID = "other-root" },
		"unapproved actor":       func(f *auxiliaryFixture) { f.request.ActorUID++ },
		"unapproved key":         func(f *auxiliaryFixture) { f.request.KeyID = "other-key" },
		"missing permission":     func(f *auxiliaryFixture) { f.policy.Auxiliary = nil },
		"duplicate permission":   func(f *auxiliaryFixture) { f.policy.Auxiliary = append(f.policy.Auxiliary, f.policy.Auxiliary[0]) },
		"unbounded permission":   func(f *auxiliaryFixture) { f.policy.Auxiliary[0].MaxBytes = MaxAuxiliaryPayloadBytes + 1 },
		"inspect expected":       func(f *auxiliaryFixture) { f.request.Auxiliary.Expected = &AuxiliaryInventory{} },
		"capture lacks expected": func(f *auxiliaryFixture) { f.request.Mode = "capture" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newAuxiliaryFixture(t)
			edit(f)
			auxiliaryMustRefuse(t, f, context.Background())
			if f.native.calls != 0 {
				t.Fatal("unauthorized structure reached native observer")
			}
		})
	}
}

func TestAuxiliaryInventoryRequiresFreshExactStoppedPersistentNativeState(t *testing.T) {
	for name, edit := range map[string]func(*domain.ColdStateInspection){
		"running":                      func(n *domain.ColdStateInspection) { n.State = "running" },
		"unknown state":                func(n *domain.ColdStateInspection) { n.State = "" },
		"transient":                    func(n *domain.ColdStateInspection) { n.Persistent = false },
		"managed save":                 func(n *domain.ColdStateInspection) { n.HasManagedSave = true },
		"autostart":                    func(n *domain.ColdStateInspection) { n.Autostart = true },
		"other resource":               func(n *domain.ColdStateInspection) { n.Resource.UUID = "aaaaaaaa-bbbb-1ccc-8ddd-eeeeeeeeeeee" },
		"other provider":               func(n *domain.ColdStateInspection) { n.Resource.ProviderID = "other" },
		"other connection":             func(n *domain.ColdStateInspection) { n.Resource.ConnectionID = "qemu:///session" },
		"other fingerprint":            func(n *domain.ColdStateInspection) { n.Fingerprint = strings.Repeat("b", 64) },
		"other layout VM":              func(n *domain.ColdStateInspection) { n.Layout.VMID = "other" },
		"missing persistent source":    func(n *domain.ColdStateInspection) { n.Source = nil },
		"mismatched persistent source": func(n *domain.ColdStateInspection) { n.Source.State.VMID = "other" },
	} {
		for _, at := range []int{1, 2, 3} {
			t.Run(fmt.Sprintf("%s/observation-%d", name, at), func(t *testing.T) {
				f := newAuxiliaryFixture(t)
				f.native.onCall = func(call int) {
					if call == at {
						edit(&f.native.observation)
					}
				}
				auxiliaryMustRefuse(t, f, context.Background())
			})
		}
	}
	for _, at := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("native-error-%d", at), func(t *testing.T) {
			f := newAuxiliaryFixture(t)
			f.native.errAt = at
			auxiliaryMustRefuse(t, f, context.Background())
		})
	}
	f := newAuxiliaryFixture(t)
	f.exec.Backend = nil
	auxiliaryMustRefuse(t, f, context.Background())
}

func TestAuxiliaryInventoryRefusesUnsafeOrIncompleteFilesystemSources(t *testing.T) {
	for name, edit := range map[string]func(*testing.T, *auxiliaryFixture){
		"root ownership": func(t *testing.T, f *auxiliaryFixture) { f.exec.rootOwnerUID++ },
		"root writable":  func(t *testing.T, f *auxiliaryFixture) { mustAuxiliary(t, os.Chmod(f.root, 0777)) },
		"state UID":      func(t *testing.T, f *auxiliaryFixture) { f.policy.Auxiliary[0].StateUID++ },
		"state GID":      func(t *testing.T, f *auxiliaryFixture) { f.policy.Auxiliary[0].StateGID++ },
		"state world writable": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Chmod(filepath.Join(f.root, "state/tpm/empty"), 0666))
		},
		"directory world writable": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Chmod(filepath.Join(f.root, "state/tpm/nested"), 0777))
		},
		"implicit TPM":    func(t *testing.T, f *auxiliaryFixture) { f.native.observation.Layout.TPM.SourcePath = "" },
		"unsupported TPM": func(t *testing.T, f *auxiliaryFixture) { f.native.observation.Layout.TPM.SourceType = "network" },
		"no auxiliary": func(t *testing.T, f *auxiliaryFixture) {
			f.native.observation.Layout.TPM = nil
			f.native.observation.Layout.Firmware.NVRAM = nil
		},
		"no TPM members": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.RemoveAll(filepath.Join(f.root, "state/tpm")))
			mustAuxiliary(t, os.Mkdir(filepath.Join(f.root, "state/tpm"), 0700))
		},
		"empty NVRAM": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Truncate(filepath.Join(f.root, "state/nvram/vars"), 0))
		},
		"outside root": func(t *testing.T, f *auxiliaryFixture) {
			f.native.observation.Layout.Firmware.NVRAM.Path = filepath.Join(filepath.Dir(f.root), "outside")
		},
		"path control": func(t *testing.T, f *auxiliaryFixture) { f.native.observation.Layout.Firmware.NVRAM.Path += "\n" },
		"unclean path": func(t *testing.T, f *auxiliaryFixture) {
			f.native.observation.Layout.Firmware.NVRAM.Path = f.root + "/state/../state/nvram/vars"
		},
		"TPM root type": func(t *testing.T, f *auxiliaryFixture) {
			f.native.observation.Layout.TPM.SourcePath = filepath.Join(f.root, "state/tpm/empty")
		},
		"shared NVRAM TPM file": func(t *testing.T, f *auxiliaryFixture) {
			f.native.observation.Layout.TPM.SourceType = "file"
			f.native.observation.Layout.TPM.SourcePath = f.native.observation.Layout.Firmware.NVRAM.Path
		},
		"overlapping TPM tree": func(t *testing.T, f *auxiliaryFixture) {
			f.native.observation.Layout.TPM.SourcePath = filepath.Join(f.root, "state")
		},
		"byte cap":   func(t *testing.T, f *auxiliaryFixture) { f.policy.Auxiliary[0].MaxBytes = 1 },
		"member cap": func(t *testing.T, f *auxiliaryFixture) { f.policy.Auxiliary[0].MaxMembers = 3 },
		"nested lock": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, "state/tpm/nested/.lock"), nil, 0600))
		},
		"nonempty lock": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, "state/tpm/.lock"), []byte("unknown"), 0600))
		},
		"directory lock": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Remove(filepath.Join(f.root, "state/tpm/.lock")))
			mustAuxiliary(t, os.Mkdir(filepath.Join(f.root, "state/tpm/.lock"), 0700))
		},
		"case alias": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, "state/tpm/EMPTY"), nil, 0600))
		},
		"control entry": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, "state/tpm/line\nbreak"), nil, 0600))
		},
		"depth cap": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.MkdirAll(filepath.Join(f.root, "state/tpm/"+strings.Repeat("a/", auxiliaryDepthLimit)), 0700))
		},
		"directory cap": func(t *testing.T, f *auxiliaryFixture) {
			for i := 0; i < auxiliaryDirectoryLimit; i++ {
				mustAuxiliary(t, os.Mkdir(filepath.Join(f.root, fmt.Sprintf("state/tpm/dir-%03d", i)), 0700))
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAuxiliaryFixture(t)
			edit(t, f)
			f.native.observation.Source.State = f.native.observation.Layout
			auxiliaryMustRefuse(t, f, context.Background())
		})
	}
	for _, role := range []string{"state/nvram/vars", "state/tpm/empty", "state/tpm/.lock"} {
		for _, kind := range []string{"symlink", "hardlink", "fifo"} {
			t.Run(role+"/"+kind, func(t *testing.T) {
				f := newAuxiliaryFixture(t)
				target := filepath.Join(f.root, role)
				mustAuxiliary(t, os.Remove(target))
				switch kind {
				case "symlink":
					mustAuxiliary(t, os.Symlink("nested/permanent", target))
				case "hardlink":
					mustAuxiliary(t, os.Link(filepath.Join(f.root, "state/tpm/nested/permanent"), target))
				case "fifo":
					mustAuxiliary(t, unix.Mkfifo(target, 0600))
				}
				auxiliaryMustRefuse(t, f, context.Background())
			})
		}
	}
}

func mustAuxiliary(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuxiliaryInventoryRefusesChangedRootMembershipAndLockGeneration(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, *auxiliaryFixture){
		"member same-size overwrite": func(t *testing.T, f *auxiliaryFixture) {
			p := filepath.Join(f.root, "state/tpm/nested/permanent")
			old, err := os.ReadFile(p)
			mustAuxiliary(t, err)
			old[0] ^= 1
			mustAuxiliary(t, os.WriteFile(p, old, 0600))
		},
		"member replacement": func(t *testing.T, f *auxiliaryFixture) {
			p := filepath.Join(f.root, "state/nvram/vars")
			mustAuxiliary(t, os.Remove(p))
			mustAuxiliary(t, os.WriteFile(p, []byte("same path different object"), 0600))
		},
		"member addition": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, "state/tpm/added"), nil, 0600))
		},
		"member removal": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Remove(filepath.Join(f.root, "state/tpm/empty")))
		},
		"nested directory replacement": func(t *testing.T, f *auxiliaryFixture) {
			p := filepath.Join(f.root, "state/tpm/nested")
			mustAuxiliary(t, os.Rename(p, p+"-old"))
			mustAuxiliary(t, os.Mkdir(p, 0700))
		},
		"lock replacement": func(t *testing.T, f *auxiliaryFixture) {
			p := filepath.Join(f.root, "state/tpm/.lock")
			mustAuxiliary(t, os.Remove(p))
			mustAuxiliary(t, os.WriteFile(p, nil, 0600))
		},
		"lock removal": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Remove(filepath.Join(f.root, "state/tpm/.lock")))
		},
		"root replacement": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Rename(f.root, f.root+"-old"))
			mustAuxiliary(t, os.Mkdir(f.root, 0700))
		},
		"mode changed": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Chmod(filepath.Join(f.root, "state/tpm/empty"), 0400))
		},
	} {
		for _, at := range []int{2, 3} {
			t.Run(fmt.Sprintf("%s/observation-%d", name, at), func(t *testing.T) {
				f := newAuxiliaryFixture(t)
				f.native.onCall = func(call int) {
					if call == at {
						mutate(t, f)
					}
				}
				auxiliaryMustRefuse(t, f, context.Background())
			})
		}
	}
}

func TestAuxiliaryInventoryTPMFileMissingLockAndExactBounds(t *testing.T) {
	f := newAuxiliaryFixture(t)
	f.native.observation.Layout.TPM.SourceType = "file"
	f.native.observation.Layout.TPM.SourcePath = filepath.Join(f.root, "state/tpm/empty")
	out, err := f.exec.Inspect(context.Background(), f.request, f.policy)
	if err != nil || len(out.Members) != 2 || out.TPMLock != nil || out.Members[1].State.Size != 0 {
		t.Fatal("explicit empty TPM regular file was not observed as metadata", out, err)
	}
	f.policy.Auxiliary[0].MaxBytes, f.policy.Auxiliary[0].MaxMembers = out.TotalBytes, uint32(len(out.Members))
	if _, err = f.exec.Inspect(context.Background(), f.request, f.policy); err != nil {
		t.Fatal("exact administrator byte/member caps refused", err)
	}
	f = newAuxiliaryFixture(t)
	mustAuxiliary(t, os.Remove(filepath.Join(f.root, "state/tpm/.lock")))
	out, err = f.exec.Inspect(context.Background(), f.request, f.policy)
	if err != nil || out.TPMLock != nil || len(out.Members) != 4 {
		t.Fatal("missing lock was guessed or state omitted", out, err)
	}
}

func TestAuxiliaryInventoryGlobalCountsAndMetadataEnvelopeBound(t *testing.T) {
	f := newAuxiliaryFixture(t)
	for i := 4; i < MaxAuxiliaryMembers; i++ {
		mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, fmt.Sprintf("state/tpm/member-%03d", i)), nil, 0600))
	}
	out, err := f.exec.Inspect(context.Background(), f.request, f.policy)
	if err != nil || len(out.Members) != MaxAuxiliaryMembers {
		t.Fatal("exact global member count refused", len(out.Members), err)
	}
	extra := filepath.Join(f.root, "state/tpm/one-too-many")
	mustAuxiliary(t, os.WriteFile(extra, nil, 0600))
	auxiliaryMustRefuse(t, f, context.Background())
	mustAuxiliary(t, os.Remove(extra))
	for i := 4; i < auxiliaryDirectoryLimit; i++ {
		mustAuxiliary(t, os.Mkdir(filepath.Join(f.root, fmt.Sprintf("state/tpm/directory-%03d-%s", i, strings.Repeat("d", 180))), 0700))
	}
	// Every count independently fits, but the combined typed metadata must
	// still fit the smaller response envelope rather than be silently truncated.
	err = auxiliaryMustRefuse(t, f, context.Background())
	if !strings.Contains(err.Error(), "metadata envelope bound") {
		t.Fatal("fixture did not reach the distinct metadata envelope boundary", err)
	}
	f = newAuxiliaryFixture(t)
	for i := 4; i < auxiliaryDirectoryLimit; i++ {
		mustAuxiliary(t, os.Mkdir(filepath.Join(f.root, fmt.Sprintf("state/tpm/directory-%03d", i)), 0700))
	}
	out, err = f.exec.Inspect(context.Background(), f.request, f.policy)
	if err != nil || len(out.Directories) != auxiliaryDirectoryLimit {
		t.Fatal("exact directory count refused", len(out.Directories), err)
	}
}

func TestAuxiliaryInventoryPreservesNativePathsWithoutRepairOrFallback(t *testing.T) {
	for name, mutate := range map[string]func(*testing.T, *auxiliaryFixture){
		"invalid UTF8 must not choose replacement": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, "state/nvram/\ufffd"), []byte("unrelated file"), 0600))
			f.native.observation.Layout.Firmware.NVRAM.Path = filepath.Join(f.root, "state/nvram/\xff")
		},
		"native metadata too large": func(t *testing.T, f *auxiliaryFixture) {
			f.native.observation.Layout.Firmware.Loader = strings.Repeat("a", 33<<10)
		},
		"explicit control source": func(t *testing.T, f *auxiliaryFixture) {
			f.native.observation.Layout.TPM.SourceType = "file"
			f.native.observation.Layout.TPM.SourcePath = filepath.Join(f.root, "state/tpm/.lock")
		},
		"ancestor symlink": func(t *testing.T, f *auxiliaryFixture) {
			old := filepath.Join(f.root, "state/nvram")
			mustAuxiliary(t, os.Rename(old, old+"-real"))
			mustAuxiliary(t, os.Symlink("nvram-real", old))
		},
		"root symlink": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Rename(f.root, f.root+"-real"))
			mustAuxiliary(t, os.Symlink(f.root+"-real", f.root))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAuxiliaryFixture(t)
			mutate(t, f)
			f.native.observation.Source.State = f.native.observation.Layout
			auxiliaryMustRefuse(t, f, context.Background())
		})
	}
	f := newAuxiliaryFixture(t)
	f.native.onCall = func(call int) {
		if call == 2 {
			f.native.observation.Layout.Firmware.NVRAM.Template = "/unopened/different-template"
			f.native.observation.Source.State = f.native.observation.Layout
		}
	}
	auxiliaryMustRefuse(t, f, context.Background())
}

func TestAuxiliaryInventoryAccessMetadataAndXattrBounds(t *testing.T) {
	f := newAuxiliaryFixture(t)
	p := filepath.Join(f.root, "state/tpm/empty")
	// Use the current mapped UID as a named entry. The generated owner argument
	// only builds fixture bytes; no other user's file or permission is changed.
	// This also works in an outer user namespace with only one mapped UID.
	acl, err := fileaccess.GrantRead(nil, unix.S_IFREG|0600, uint32(os.Getuid()+1), uint32(os.Getgid()), uint32(os.Getuid()), []uint32{uint32(os.Getgid())})
	mustAuxiliary(t, err)
	mustAuxiliary(t, unix.Setxattr(p, "system.posix_acl_access", acl, 0))
	out, err := f.exec.Inspect(context.Background(), f.request, f.policy)
	mustAuxiliary(t, err)
	labelBytes := make([]byte, auxiliaryLabelBytes)
	labelSize, labelErr := unix.Getxattr(p, "security.selinux", labelBytes)
	wantLabel := ""
	if labelErr == nil {
		wantLabel = hex.EncodeToString(labelBytes[:labelSize])
	} else if !errors.Is(labelErr, unix.ENODATA) {
		t.Fatal(labelErr)
	}
	found := false
	for _, m := range out.Members {
		if m.RelativePath == "state/tpm/empty" {
			found = true
			if m.State.ACL != hex.EncodeToString(acl) || m.State.SELinux != wantLabel {
				t.Fatal("access ACL or existing SELinux metadata omitted or changed", m.State)
			}
		}
	}
	if !found {
		t.Fatal("ACL-bearing zero-length file disappeared")
	}
	pin, err := auxiliaryOpen(unix.AT_FDCWD, p, false, false)
	mustAuxiliary(t, err)
	defer pin.Close()
	flags, err := unix.FcntlInt(pin.Fd(), unix.F_GETFL, 0)
	mustAuxiliary(t, err)
	if flags&unix.O_PATH == 0 {
		t.Fatal("metadata pin acquired content read access")
	}
	if _, err = pin.Read(make([]byte, 1)); !errors.Is(err, unix.EBADF) {
		t.Fatal("O_PATH pin permitted a content read", err)
	}
	if _, err = auxiliaryXattr(pin, "user.virmill-missing", 8); err != nil {
		t.Fatal("absent xattr was not represented explicitly", err)
	}
	mustAuxiliary(t, unix.Setxattr(p, "user.virmill-synthetic", []byte("0123456789"), 0))
	if _, err = auxiliaryXattr(pin, "user.virmill-synthetic", 8); err == nil {
		t.Fatal("oversized xattr accepted")
	}
	if _, err = auxiliaryOpen(unix.AT_FDCWD, p, false, true); err == nil {
		t.Fatal("inventory helper allowed a readable regular file")
	}
}

func TestAuxiliaryInventoryCancellationAndFailureCloseOwnDescriptors(t *testing.T) {
	f := newAuxiliaryFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := auxiliaryMustRefuse(t, f, ctx); !errors.Is(err, context.Canceled) || f.native.calls != 0 {
		t.Fatal("pre-cancellation reached native observer", err)
	}
	before, err := os.ReadDir("/proc/self/fd")
	mustAuxiliary(t, err)
	for i := 0; i < 24; i++ {
		ctx, cancel = context.WithCancel(context.Background())
		f.native.calls = 0
		f.native.onCall = func(call int) {
			if call == 2+i%2 {
				cancel()
			}
		}
		if err := auxiliaryMustRefuse(t, f, ctx); !errors.Is(err, context.Canceled) {
			t.Fatal("late cancellation was lost", err)
		}
		cancel()
	}
	// A first-scan failure must also close objects acquired before the payload
	// bound is discovered, without relying on os.File finalizers.
	f.native.onCall = nil
	f.policy.Auxiliary[0].MaxBytes = 1
	for i := 0; i < 8; i++ {
		auxiliaryMustRefuse(t, f, context.Background())
	}
	after, err := os.ReadDir("/proc/self/fd")
	mustAuxiliary(t, err)
	if len(after) != len(before) {
		t.Fatalf("canceled inventories leaked descriptors: %d -> %d", len(before), len(after))
	}
}

func FuzzAuxiliaryNativeRelativePath(f *testing.F) {
	for _, pair := range [][2]string{{"/approved", "/approved/state/file"}, {"/approved", "/approved"}, {"/approved", "/approved/../outside"}, {"/approved", "/approved-other/file"}, {"/approved", "/approved/a/.lock"}, {"/approved", "/approved/a\nfile"}, {"/approved", "/approved/\xff"}, {"/approved", "/proc/self/fd/1"}} {
		f.Add(pair[0], pair[1])
	}
	f.Fuzz(func(t *testing.T, root, source string) {
		rel, err := auxiliaryRelative(root, source)
		if err != nil {
			if rel != "" {
				t.Fatal("failed projection returned a partial path")
			}
			return
		}
		if !filepath.IsLocal(rel) || filepath.Join(root, rel) != source || source == root || len(strings.Split(rel, "/")) > auxiliaryDepthLimit {
			t.Fatal("native source changed identity or escaped its root")
		}
		for _, component := range strings.Split(rel, "/") {
			if component == ".lock" {
				t.Fatal("payload source selected a producer control file")
			}
		}
	})
}
