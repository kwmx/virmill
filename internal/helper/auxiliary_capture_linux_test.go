//go:build linux && amd64

package helper

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileaccess"
	"virmill.local/core/internal/operations"
)

func prepareAuxiliaryCapture(t *testing.T, f *auxiliaryFixture) {
	t.Helper()
	f.request.Mode = "inspect"
	f.request.Auxiliary.Expected = nil
	in, err := f.exec.Inspect(context.Background(), f.request, f.policy)
	if err != nil {
		t.Fatal(err)
	}
	f.request.Mode = "capture"
	f.request.JobID = "12345678-1234-4234-8234-123456789abc"
	f.request.PlanDigest = strings.Repeat("b", 64)
	f.request.Auxiliary.Expected = &in
	f.native.calls = 0
}

func newAuxiliaryCaptureFixture(t *testing.T) *auxiliaryFixture {
	t.Helper()
	f := newAuxiliaryFixture(t)
	prepareAuxiliaryCapture(t, f)
	return f
}

func auxiliaryCaptureFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func auxiliaryCaptureRefused(t *testing.T, f *auxiliaryFixture, ctx context.Context) error {
	t.Helper()
	before := auxiliaryCaptureFDs(t)
	c, err := f.exec.Capture(ctx, f.request, f.policy)
	if c != nil {
		_ = c.Close()
		t.Fatal("failed capture returned custody or a successful payload")
	}
	if err == nil {
		t.Fatal("expected capture refusal")
	}
	if after := auxiliaryCaptureFDs(t); after != before {
		t.Fatalf("capture failure leaked descriptors: before=%d after=%d", before, after)
	}
	return err
}

func auxiliaryCaptureHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestAuxiliaryCaptureDeterministicUSTARAndSeals(t *testing.T) {
	f := newAuxiliaryCaptureFixture(t)
	expected := f.request.Auxiliary.Expected
	payloads := map[string][]byte{}
	var canonical bytes.Buffer
	tw := tar.NewWriter(&canonical)
	for _, member := range expected.Members {
		payload, err := os.ReadFile(filepath.Join(f.root, member.RelativePath))
		if err != nil {
			t.Fatal(err)
		}
		payloads[member.ID] = payload
		mustAuxiliary(t, tw.WriteHeader(&tar.Header{Name: member.ID, Typeflag: tar.TypeReg, Mode: 0600, Size: int64(len(payload)), ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR}))
		_, err = tw.Write(payload)
		mustAuxiliary(t, err)
	}
	mustAuxiliary(t, tw.Close())
	// Reads above concern only original generated fixture bytes. Capture itself
	// must preserve every source timestamp, including access time.
	stats := map[string]unix.Stat_t{}
	for _, m := range append(append([]AuxiliaryMember{}, expected.Members...), *expected.TPMLock) {
		var st unix.Stat_t
		mustAuxiliary(t, unix.Stat(filepath.Join(f.root, m.RelativePath), &st))
		stats[m.RelativePath] = st
	}
	baseline := auxiliaryCaptureFDs(t)
	c, err := f.exec.Capture(context.Background(), f.request, f.policy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Response.Stage != "captured" || c.Response.Version != 1 || c.Response.JobID != f.request.JobID || !reflect.DeepEqual(c.Response.Inventory, expected) || c.Snapshot == nil {
		t.Fatal("capture omitted exact response binding or inventory")
	}
	binding, err := AuxiliaryBinding(f.request)
	mustAuxiliary(t, err)
	digest, err := operations.Digest(expected)
	mustAuxiliary(t, err)
	artifact := c.Response.Artifact
	if c.Response.Binding != binding || artifact == nil || artifact.Format != "ustar-v1" || artifact.InventoryDigest != digest || artifact.Size != uint64(canonical.Len()) || artifact.SHA256 != auxiliaryCaptureHash(canonical.Bytes()) {
		t.Fatal("capture artifact identity, format or bounds differ")
	}
	position, err := c.Snapshot.Seek(0, io.SeekCurrent)
	if err != nil || position != 0 {
		t.Fatal("snapshot was not rewound")
	}
	actual, err := io.ReadAll(c.Snapshot)
	if err != nil || !bytes.Equal(actual, canonical.Bytes()) {
		t.Fatal("archive bytes differ from deterministic USTAR", err)
	}
	tr := tar.NewReader(bytes.NewReader(actual))
	for i, member := range expected.Members {
		header, err := tr.Next()
		if err != nil || header.Name != fmt.Sprintf("members/%03d", i) || header.Format != tar.FormatUSTAR || header.Typeflag != tar.TypeReg || header.Mode != 0600 || header.Uid != 0 || header.Gid != 0 || header.Size != int64(member.State.Size) || header.ModTime.Unix() != 0 {
			t.Fatal("archive member has unknown or source-derived header metadata", err)
		}
		payload, err := io.ReadAll(tr)
		if err != nil || !bytes.Equal(payload, payloads[member.ID]) || artifact.MemberSHA256[member.ID] != auxiliaryCaptureHash(payload) {
			t.Fatal("member bytes or digest differ", err)
		}
	}
	if _, err := tr.Next(); err != io.EOF || len(artifact.MemberSHA256) != len(expected.Members) {
		t.Fatal("archive contains extra entries or producer .lock payload")
	}
	metadata, err := json.Marshal(c.Response)
	if err != nil || bytes.Contains(metadata, []byte("synthetic confidential")) || bytes.Contains(metadata, []byte("synthetic opaque state")) || bytes.Contains(metadata, []byte("bootTested")) {
		t.Fatal("response exposed state bytes or invented complete recovery proof")
	}
	seals, err := unix.FcntlInt(c.Snapshot.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&snapshotSeals != snapshotSeals {
		t.Fatal("snapshot lacks immutable seals", err)
	}
	flags, err := unix.FcntlInt(c.Snapshot.Fd(), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("snapshot can escape across exec", err)
	}
	if _, err := c.Snapshot.WriteAt([]byte{1}, 0); !errors.Is(err, unix.EPERM) {
		t.Fatal("sealed snapshot allowed write", err)
	}
	if err := c.Snapshot.Truncate(1); !errors.Is(err, unix.EPERM) {
		t.Fatal("sealed snapshot allowed truncation", err)
	}
	mustAuxiliary(t, c.Recheck(context.Background()))
	second, err := f.exec.Capture(context.Background(), f.request, f.policy)
	if err != nil {
		t.Fatal("cooperating readers could not coexist", err)
	}
	other, err := io.ReadAll(second.Snapshot)
	if err != nil || !bytes.Equal(other, actual) || !reflect.DeepEqual(second.Response, c.Response) {
		t.Fatal("unchanged capture was not deterministic", err)
	}
	mustAuxiliary(t, second.Close())
	for path, before := range stats {
		var after unix.Stat_t
		mustAuxiliary(t, unix.Stat(filepath.Join(f.root, path), &after))
		if before.Atim != after.Atim || before.Mtim != after.Mtim || before.Ctim != after.Ctim || before.Ino != after.Ino || before.Size != after.Size {
			t.Fatal("capture changed generated source metadata")
		}
	}
	mustAuxiliary(t, c.Close())
	mustAuxiliary(t, c.Close())
	if err := c.Recheck(context.Background()); err == nil {
		t.Fatal("closed capture rechecked successfully")
	}
	if _, err := c.Snapshot.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("Close retained the owned snapshot descriptor", err)
	}
	if after := auxiliaryCaptureFDs(t); after != baseline {
		t.Fatalf("successful capture leaked descriptors: %d -> %d", baseline, after)
	}
}

func captureTestLock(t *testing.T, path string, command int, typ int16, start, length int64) (*os.File, error) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	lock := unix.Flock_t{Type: typ, Whence: io.SeekStart, Start: start, Len: length}
	err = unix.FcntlFlock(f.Fd(), command, &lock)
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func TestAuxiliaryCaptureProducerGuardLifetime(t *testing.T) {
	f := newAuxiliaryCaptureFixture(t)
	c, err := f.exec.Capture(context.Background(), f.request, f.policy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	lockPath := filepath.Join(f.root, "state/tpm/.lock")
	nvram := filepath.Join(f.root, "state/nvram/vars")
	// Closing other descriptions of the inode must not release the OFD guard.
	other, err := os.Open(lockPath)
	mustAuxiliary(t, err)
	mustAuxiliary(t, other.Close())
	for _, tt := range []struct {
		name, path string
		cmd        int
		start, len int64
	}{
		{"swtpm POSIX whole file", lockPath, unix.F_SETLK, 0, 0},
		{"swtpm future growth", lockPath, unix.F_SETLK, 4096, 1},
		{"swtpm OFD", lockPath, unix.F_OFD_SETLK, 0, 0},
		{"QEMU nonsharing region", nvram, unix.F_OFD_SETLK, 201, 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writer, err := captureTestLock(t, tt.path, tt.cmd, unix.F_WRLCK, tt.start, tt.len)
			if writer != nil {
				writer.Close()
				t.Fatal("producer guard released while capture owned it")
			}
			if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EACCES) {
				t.Fatal("unexpected lock refusal", err)
			}
		})
	}
	mustAuxiliary(t, c.Close())
	for _, path := range []string{lockPath, nvram} {
		writer, err := captureTestLock(t, path, unix.F_OFD_SETLK, unix.F_WRLCK, 0, 0)
		if err != nil {
			t.Fatal("producer guard retained after Close", err)
		}
		mustAuxiliary(t, writer.Close())
	}
}

func TestAuxiliaryCaptureBusyProducerRefusesAndCleansUp(t *testing.T) {
	for _, producer := range []string{"swtpm", "qemu"} {
		t.Run(producer, func(t *testing.T) {
			f := newAuxiliaryCaptureFixture(t)
			path, cmd, typ, start, length := filepath.Join(f.root, "state/tpm/.lock"), unix.F_SETLK, int16(unix.F_WRLCK), int64(0), int64(0)
			if producer == "qemu" {
				path, cmd, typ, start, length = filepath.Join(f.root, "state/nvram/vars"), unix.F_OFD_SETLK, unix.F_RDLCK, 101, 1
			}
			writer, err := captureTestLock(t, path, cmd, typ, start, length)
			mustAuxiliary(t, err)
			defer writer.Close()
			err = auxiliaryCaptureRefused(t, f, context.Background())
			if !strings.Contains(err.Error(), "RESOURCE_BUSY") {
				t.Fatal("conflicting producer did not refuse as busy", err)
			}
			mustAuxiliary(t, writer.Close())
			c, err := f.exec.Capture(context.Background(), f.request, f.policy)
			if err != nil {
				t.Fatal("failed capture retained a conflicting producer guard", err)
			}
			mustAuxiliary(t, c.Close())
		})
	}
}

func TestAuxiliaryCaptureAuthorityAndExpectedInventory(t *testing.T) {
	for name, edit := range map[string]func(*auxiliaryFixture){
		"capture permission absent": func(f *auxiliaryFixture) { f.policy.Auxiliary[0].AllowCapture = false },
		"wrong mode":                func(f *auxiliaryFixture) { f.request.Mode = "inspect" },
		"missing expected":          func(f *auxiliaryFixture) { f.request.Auxiliary.Expected = nil },
		"wrong operation":           func(f *auxiliaryFixture) { f.request.Operation = "other" },
		"wrong job":                 func(f *auxiliaryFixture) { f.request.JobID = "" },
		"wrong plan":                func(f *auxiliaryFixture) { f.request.PlanDigest = "unknown" },
		"mixed network":             func(f *auxiliaryFixture) { f.request.Network = &NetworkRequest{} },
		"wrong actor":               func(f *auxiliaryFixture) { f.request.ActorUID++ },
		"wrong root":                func(f *auxiliaryFixture) { f.request.RootID = "other" },
		"overclaimed bytes":         func(f *auxiliaryFixture) { f.request.Auxiliary.Expected.TotalBytes++ },
		"lossy metadata":            func(f *auxiliaryFixture) { f.request.Auxiliary.Expected.Root.State.SELinux = "\xff" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newAuxiliaryCaptureFixture(t)
			edit(f)
			auxiliaryCaptureRefused(t, f, context.Background())
			if f.native.calls != 0 {
				t.Fatal("malformed or unauthorized capture reached native observation")
			}
		})
	}
	for name, edit := range map[string]func(*AuxiliaryInventory){
		"member generation": func(in *AuxiliaryInventory) { in.Members[0].State.Generation += "changed" },
		"member order":      func(in *AuxiliaryInventory) { in.Members[0], in.Members[1] = in.Members[1], in.Members[0] },
		"missing directory": func(in *AuxiliaryInventory) { in.Directories = in.Directories[1:] },
		"lock identity":     func(in *AuxiliaryInventory) { in.TPMLock.State.Generation += "changed" },
		"member source":     func(in *AuxiliaryInventory) { in.Members[0].RelativePath = "state/tpm/empty" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newAuxiliaryCaptureFixture(t)
			edit(f.request.Auxiliary.Expected)
			err := auxiliaryCaptureRefused(t, f, context.Background())
			if !strings.Contains(err.Error(), "STALE_PLAN") {
				t.Fatal("expected inventory mismatch did not fail", err)
			}
		})
	}
}

func TestAuxiliaryCaptureUnqualifiedTPMAndMissingLock(t *testing.T) {
	for _, kind := range []string{"file", "missing lock", "implicit"} {
		t.Run(kind, func(t *testing.T) {
			f := newAuxiliaryFixture(t)
			if kind == "file" {
				f.native.observation.Layout.TPM.SourceType = "file"
				f.native.observation.Layout.TPM.SourcePath = filepath.Join(f.root, "state/tpm/empty")
			}
			if kind == "missing lock" {
				mustAuxiliary(t, os.Remove(filepath.Join(f.root, "state/tpm/.lock")))
			}
			prepareAuxiliaryCapture(t, f)
			if kind == "implicit" {
				f.native.observation.Layout.TPM.SourcePath = ""
			}
			err := auxiliaryCaptureRefused(t, f, context.Background())
			if !strings.Contains(err.Error(), "UNSUPPORTED_CAPABILITY") {
				t.Fatal("unqualified TPM storage did not explicitly refuse", err)
			}
		})
	}
}

func TestAuxiliaryCaptureSingleSourceKindsAndZeroTPMPayload(t *testing.T) {
	for _, kind := range []string{"nvram-only", "tpm-only", "zero-tpm-only"} {
		t.Run(kind, func(t *testing.T) {
			f := newAuxiliaryFixture(t)
			if kind == "nvram-only" {
				f.native.observation.Layout.TPM = nil
			} else {
				f.native.observation.Layout.Firmware.NVRAM = nil
			}
			if kind == "zero-tpm-only" {
				mustAuxiliary(t, os.Remove(filepath.Join(f.root, "state/tpm/nested/permanent")))
				mustAuxiliary(t, os.Remove(filepath.Join(f.root, "state/tpm/.metadata")))
			}
			f.native.observation.Source.State = f.native.observation.Layout
			prepareAuxiliaryCapture(t, f)
			in := f.request.Auxiliary.Expected
			f.policy.Auxiliary[0].MaxMembers = uint32(len(in.Members))
			if in.TotalBytes > 0 {
				f.policy.Auxiliary[0].MaxBytes = in.TotalBytes
			}
			c, err := f.exec.Capture(context.Background(), f.request, f.policy)
			if err != nil {
				t.Fatal("valid source subset or exact policy bound refused", err)
			}
			defer c.Close()
			if kind == "zero-tpm-only" && (in.TotalBytes != 0 || c.Response.Artifact.Size != 1536 || len(in.Members) != 1 || c.Response.Artifact.MemberSHA256["members/000"] != auxiliaryCaptureHash(nil)) {
				t.Fatal("zero-length TPM member was omitted or did not produce a bounded archive")
			}
			mustAuxiliary(t, c.Recheck(context.Background()))
		})
	}
}

func TestAuxiliaryCaptureRecheckAccessACL(t *testing.T) {
	f := newAuxiliaryCaptureFixture(t)
	c, err := f.exec.Capture(context.Background(), f.request, f.policy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	acl, err := fileaccess.GrantRead(nil, unix.S_IFREG|0600, uint32(os.Getuid()+1), uint32(os.Getgid()), uint32(os.Getuid()), []uint32{uint32(os.Getgid())})
	mustAuxiliary(t, err)
	// Changes only a generated fixture member owned by this test process.
	mustAuxiliary(t, unix.Setxattr(filepath.Join(f.root, "state/tpm/empty"), "system.posix_acl_access", acl, 0))
	if err := c.Recheck(context.Background()); err == nil {
		t.Fatal("access ACL drift left the capture publishable")
	}
}

func TestAuxiliaryCaptureRecheckRefusesNativeDriftPermanently(t *testing.T) {
	for name, edit := range map[string]func(*auxiliaryFixture){
		"running":        func(f *auxiliaryFixture) { f.native.observation.State = "running" },
		"managed save":   func(f *auxiliaryFixture) { f.native.observation.HasManagedSave = true },
		"autostart":      func(f *auxiliaryFixture) { f.native.observation.Autostart = true },
		"transient":      func(f *auxiliaryFixture) { f.native.observation.Persistent = false },
		"fingerprint":    func(f *auxiliaryFixture) { f.native.observation.Fingerprint = strings.Repeat("c", 64) },
		"observer error": func(f *auxiliaryFixture) { f.native.errAt = f.native.calls + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			f := newAuxiliaryCaptureFixture(t)
			c, err := f.exec.Capture(context.Background(), f.request, f.policy)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			before := f.native.observation
			edit(f)
			if err := c.Recheck(context.Background()); err == nil {
				t.Fatal("native drift was not refused")
			}
			f.native.observation, f.native.errAt = before, 0
			if err := c.Recheck(context.Background()); err == nil {
				t.Fatal("failed capture custody became publishable after restoring native state")
			}
		})
	}
}

func TestAuxiliaryCaptureRecheckCompleteMembershipAndMetadata(t *testing.T) {
	for name, edit := range map[string]func(*testing.T, *auxiliaryFixture){
		"new member": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, "state/tpm/new"), nil, 0600))
		},
		"deleted member": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Remove(filepath.Join(f.root, "state/tpm/empty")))
		},
		"new directory": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Mkdir(filepath.Join(f.root, "state/tpm/new-directory"), 0700))
		},
		"same size payload write": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, "state/tpm/.metadata"), []byte("changed opaque payload"), 0600))
		},
		"mode": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Chmod(filepath.Join(f.root, "state/tpm/empty"), 0400))
		},
		"lock replacement": func(t *testing.T, f *auxiliaryFixture) {
			p := filepath.Join(f.root, "state/tpm/.lock")
			mustAuxiliary(t, os.Remove(p))
			mustAuxiliary(t, os.WriteFile(p, nil, 0600))
		},
		"nonempty lock": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, "state/tpm/.lock"), []byte{1}, 0600))
		},
		"source symlink": func(t *testing.T, f *auxiliaryFixture) {
			p := filepath.Join(f.root, "state/nvram/vars")
			mustAuxiliary(t, os.Rename(p, p+"-old"))
			mustAuxiliary(t, os.Symlink("vars-old", p))
		},
		"source FIFO": func(t *testing.T, f *auxiliaryFixture) {
			p := filepath.Join(f.root, "state/nvram/vars")
			mustAuxiliary(t, os.Remove(p))
			mustAuxiliary(t, unix.Mkfifo(p, 0600))
		},
		"source hardlink": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Link(filepath.Join(f.root, "state/nvram/vars"), filepath.Join(f.root, "alias")))
		},
		"root replaced": func(t *testing.T, f *auxiliaryFixture) {
			mustAuxiliary(t, os.Rename(f.root, f.root+"-original"))
			mustAuxiliary(t, os.Mkdir(f.root, 0700))
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAuxiliaryCaptureFixture(t)
			c, err := f.exec.Capture(context.Background(), f.request, f.policy)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			edit(t, f)
			if err := c.Recheck(context.Background()); err == nil {
				t.Fatal("changed complete inventory or held metadata returned success")
			}
		})
	}
}

func TestAuxiliaryCaptureNativeBoundaryFailureCleansUp(t *testing.T) {
	for _, at := range []int{1, 2, 3, 4, 5} {
		for _, reason := range []string{"error", "running", "cancel", "payload drift"} {
			t.Run(fmt.Sprintf("%s/observation-%d", reason, at), func(t *testing.T) {
				f := newAuxiliaryCaptureFixture(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				f.native.onCall = func(call int) {
					if call != at {
						return
					}
					switch reason {
					case "error":
						f.native.errAt = at
					case "running":
						f.native.observation.State = "running"
					case "cancel":
						cancel()
					case "payload drift":
						mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, "state/tpm/empty"), []byte{1}, 0600))
					}
				}
				err := auxiliaryCaptureRefused(t, f, ctx)
				if reason == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost its context error", err)
				}
			})
		}
	}
}

// Only examines descriptor names of this ordinary-user fixture process, never
// native state bytes. The second memfd boundary is after its first read chunk.
type captureDuringCopyContext struct {
	context.Context
	seen     int
	trigger  func()
	canceled bool
}

func (c *captureDuringCopyContext) Err() error {
	if c.canceled {
		return context.Canceled
	}
	entries, _ := os.ReadDir("/proc/self/fd")
	for _, entry := range entries {
		name, _ := os.Readlink("/proc/self/fd/" + entry.Name())
		if strings.Contains(name, "memfd:virmill-auxiliary-snapshot") {
			c.seen++
			if c.seen == 2 {
				c.trigger()
			}
			break
		}
	}
	if c.canceled {
		return context.Canceled
	}
	return nil
}

func TestAuxiliaryCaptureDuringCopyMutationAndCancellationCleanup(t *testing.T) {
	for _, reason := range []string{"cancel", "mutate", "truncate"} {
		t.Run(reason, func(t *testing.T) {
			f := newAuxiliaryFixture(t)
			p := filepath.Join(f.root, "state/nvram/vars")
			mustAuxiliary(t, os.WriteFile(p, bytes.Repeat([]byte{0x63}, 192<<10), 0600))
			prepareAuxiliaryCapture(t, f)
			ctx := &captureDuringCopyContext{Context: context.Background()}
			ctx.trigger = func() {
				switch reason {
				case "cancel":
					ctx.canceled = true
				case "mutate":
					writer, err := os.OpenFile(p, os.O_WRONLY, 0)
					mustAuxiliary(t, err)
					_, err = writer.WriteAt([]byte{0x64}, 128<<10)
					mustAuxiliary(t, err)
					mustAuxiliary(t, writer.Close())
				case "truncate":
					mustAuxiliary(t, os.Truncate(p, 64<<10))
				}
			}
			err := auxiliaryCaptureRefused(t, f, ctx)
			if ctx.seen < 2 || reason == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("copy interruption boundary was not exercised", err)
			}
		})
	}
}

func TestAuxiliaryCaptureFreezesCallerMetadataAndCleanupAfterRecheckCancel(t *testing.T) {
	f := newAuxiliaryCaptureFixture(t)
	c, err := f.exec.Capture(context.Background(), f.request, f.policy)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f.policy.Roots[f.request.RootID] = "/unknown"
	f.policy.Auxiliary[0].StateUID++
	f.request.Auxiliary.Expected.Members[0].State.Generation = "forged"
	f.request.Auxiliary.Expected.Layout.Firmware.NVRAM.Path = "/unknown"
	f.request.Auxiliary.Fingerprint = strings.Repeat("d", 64)
	c.Response.Inventory.Members[0].State.Generation = "caller-modified-output"
	mustAuxiliary(t, c.Recheck(context.Background()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Recheck(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("recheck ignored cancellation", err)
	}
	if err := c.Recheck(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled custody became publishable again", err)
	}
	mustAuxiliary(t, c.Close())
}
