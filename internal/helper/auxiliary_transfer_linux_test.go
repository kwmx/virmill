//go:build linux && amd64

package helper

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/domain"
)

type auxiliaryTransferFixture struct {
	*auxiliaryFixture
	runtime auxiliaryTransferRuntime
	key     ed25519.PrivateKey
}

// Close the original socketpair descriptors immediately after FileConn takes
// its duplicates. Keeping hidden duplicates alive would turn peer-close tests
// into timeout tests, unlike the production dedicated connection.
func auxiliaryTransferPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		if os.Getenv("VIRMILL_TEST_REQUIRE_IPC") != "1" {
			t.Skipf("BLOCKED: private Unix descriptor IPC unavailable: %v", err)
		}
		t.Fatal(err)
	}
	defer func() {
		for _, fd := range fds {
			if fd >= 0 {
				unix.Close(fd)
			}
		}
	}()
	conns := make([]*net.UnixConn, 2)
	for i, fd := range fds {
		f := os.NewFile(uintptr(fd), "private auxiliary transfer socket")
		conn, err := net.FileConn(f)
		_ = f.Close()
		fds[i] = -1
		if err != nil {
			if os.Getenv("VIRMILL_TEST_REQUIRE_IPC") != "1" && (errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES)) {
				t.Skipf("BLOCKED: private Unix descriptor IPC unavailable: %v", err)
			}
			t.Fatal(err)
		}
		conns[i] = conn.(*net.UnixConn)
		t.Cleanup(func() { _ = conn.Close() })
	}
	return conns[0], conns[1]
}

func newAuxiliaryTransferFixture(t *testing.T) *auxiliaryTransferFixture {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("ordinary-user synthetic auxiliary transport fixture refuses root")
	}
	f := newAuxiliaryCaptureFixture(t)
	public, key, err := ed25519.GenerateKey(rand.Reader)
	mustAuxiliary(t, err)
	f.request.ActorUID = uint32(os.Getuid())
	f.request.ExpiresAt = time.Now().UTC().Add(time.Minute)
	f.policy.Auxiliary[0].ActorUID = f.request.ActorUID
	f.policy.Actors = []uint32{f.request.ActorUID}
	f.policy.Keys = map[string]string{f.request.KeyID: hex.EncodeToString(public)}
	signAuxiliaryAuth(t, &f.request, key)
	dir := t.TempDir()
	mustAuxiliary(t, os.Chmod(dir, 0700))
	out := &auxiliaryTransferFixture{auxiliaryFixture: f, key: key, runtime: auxiliaryTransferRuntime{journalDirectory: dir, journalOwnerUID: uint32(os.Getuid())}}
	out.runtime.policy = func() (Policy, error) { return f.policy, nil }
	return out
}

func transferTestServe(ctx context.Context, a *net.UnixConn, f *auxiliaryTransferFixture, r Request, executor *AuxiliaryExecutor) <-chan error {
	done := make(chan error, 1)
	go func() {
		err := f.runtime.serve(ctx, a, executor, r, f.policy)
		if err != nil {
			code := "OPERATION_FAILED"
			var d *domain.Error
			if errors.As(err, &d) {
				code = d.Code
			}
			_ = writeAuxiliaryFrame(ctx, a, Response{APIVersion: domain.APIVersion, Success: false, Error: err.Error(), ErrorCode: code})
		}
		done <- err
	}()
	return done
}

func transferStart(t *testing.T, f *auxiliaryTransferFixture) (*AuxiliaryTransfer, <-chan error) {
	t.Helper()
	a, b := auxiliaryTransferPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	done := transferTestServe(ctx, a, f, f.request, &f.exec)
	transferCtx, transferCancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(transferCancel)
	x, err := receiveAuxiliaryTransfer(transferCtx, b, f.request, transferCancel)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = x.Close() })
	return x, done
}

func transferObserve(t *testing.T, f *auxiliaryTransferFixture) (AuxiliaryResponse, error) {
	t.Helper()
	r := cloneAuxiliaryAuth(t, f.request)
	r.Mode = "observe"
	r.ExpiresAt = time.Now().UTC().Add(time.Minute)
	signAuxiliaryAuth(t, &r, f.key)
	a, b := auxiliaryTransferPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := transferTestServe(ctx, a, f, r, nil)
	out, fd, err := receiveAuxiliaryDelivery(ctx, b, r, "delivered", false)
	if fd != nil {
		fd.Close()
		t.Fatal("metadata observation returned an auxiliary descriptor")
	}
	serverErr := <-done
	if (serverErr == nil) != (err == nil) {
		t.Fatalf("helper/client disagree on observed success: server=%v client=%v", serverErr, err)
	}
	return out, err
}

func transferNoDelivery(t *testing.T, f *auxiliaryTransferFixture) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(f.runtime.journalDirectory, f.request.JobID+".auxiliary-intent.json")); err != nil {
		t.Fatal("capture failure lost its pre-copy intent", err)
	}
	if _, err := os.Stat(filepath.Join(f.runtime.journalDirectory, f.request.JobID+".auxiliary-delivered.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("incomplete delivery published a successful record", err)
	}
	before := f.native.calls
	result, err := transferObserve(t, f)
	if err == nil || !reflect.DeepEqual(result, AuxiliaryResponse{}) || f.native.calls != before {
		t.Fatal("incomplete observation recopied or returned proof", err)
	}
	// A repeated capture of even an unchanged inventory is not an implicit retry.
	a, b := auxiliaryTransferPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := transferTestServe(ctx, a, f, f.request, &f.exec)
	result, fd, err := receiveAuxiliaryDelivery(ctx, b, f.request, "captured", true)
	if fd != nil {
		fd.Close()
	}
	if err == nil || !reflect.DeepEqual(result, AuxiliaryResponse{}) || <-done == nil || f.native.calls != before {
		t.Fatal("retained intent permitted capture replay")
	}
}

func TestAuxiliaryTransferCommitAndObserveExactDurableDelivery(t *testing.T) {
	f := newAuxiliaryTransferFixture(t)
	intentSeen := false
	f.native.onCall = func(call int) {
		if call == 1 {
			data, err := os.ReadFile(filepath.Join(f.runtime.journalDirectory, f.request.JobID+".auxiliary-intent.json"))
			if err != nil || !bytes.Contains(data, []byte(`"binding"`)) || bytes.Contains(data, []byte("synthetic confidential")) {
				t.Error("durable metadata intent did not precede native copying", err)
			}
			intentSeen = true
		}
	}
	x, done := transferStart(t, f)
	if !intentSeen || x.Response.Stage != "captured" {
		t.Fatal("captured stage or before-copy journal boundary missing")
	}
	if _, err := os.Stat(filepath.Join(f.runtime.journalDirectory, f.request.JobID+".auxiliary-delivered.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unacknowledged descriptor delivery published completion")
	}
	writer, err := captureTestLock(t, filepath.Join(f.root, "state/tpm/.lock"), unix.F_SETLK, unix.F_WRLCK, 0, 0)
	if writer != nil {
		writer.Close()
		t.Fatal("source producer guard was released before explicit commit")
	}
	if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EACCES) {
		t.Fatal(err)
	}
	delivered, err := x.Commit(context.Background())
	if err != nil || delivered.Stage != "delivered" {
		t.Fatal("explicit commit failed", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := ValidateAuxiliaryDelivery(f.request, &delivered); err != nil {
		t.Fatal(err)
	}
	writer, err = captureTestLock(t, filepath.Join(f.root, "state/tpm/.lock"), unix.F_OFD_SETLK, unix.F_WRLCK, 0, 0)
	if err != nil {
		t.Fatal("helper retained source custody after delivery", err)
	}
	mustAuxiliary(t, writer.Close())
	before := f.native.calls
	observed, err := transferObserve(t, f)
	if err != nil || !reflect.DeepEqual(observed, delivered) || before != f.native.calls {
		t.Fatal("durable observation lost metadata or replayed native work", err)
	}
	for _, suffix := range []string{"intent", "delivered"} {
		path := filepath.Join(f.runtime.journalDirectory, f.request.JobID+".auxiliary-"+suffix+".json")
		data, err := os.ReadFile(path)
		if err != nil || bytes.Contains(data, []byte("synthetic confidential")) || bytes.Contains(data, []byte("synthetic opaque state")) {
			t.Fatal("journal contains payload or lacks a record", err)
		}
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm() != 0600 {
			t.Fatal("journal metadata mode differs", err)
		}
	}
	if result, err := x.Commit(context.Background()); err == nil || !reflect.DeepEqual(result, AuxiliaryResponse{}) {
		t.Fatal("duplicate commit sent another acknowledgement")
	}
	mustAuxiliary(t, x.Close())
}

func TestAuxiliaryTransferLostInitialAckRetainsIntentWithoutReplay(t *testing.T) {
	f := newAuxiliaryTransferFixture(t)
	x, done := transferStart(t, f)
	mustAuxiliary(t, x.Close())
	if err := <-done; err == nil {
		t.Fatal("helper accepted EOF instead of acknowledgement")
	}
	transferNoDelivery(t, f)
}

func TestAuxiliaryTransferLostFinalResponseObservedWithoutReplay(t *testing.T) {
	f := newAuxiliaryTransferFixture(t)
	a, b := auxiliaryTransferPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := transferTestServe(ctx, a, f, f.request, &f.exec)
	captured, snapshot, err := receiveAuxiliaryDelivery(ctx, b, f.request, "captured", true)
	mustAuxiliary(t, err)
	defer snapshot.Close()
	ack := auxiliaryAcknowledgement{1, captured.JobID, captured.Binding, captured.Artifact.SHA256}
	mustAuxiliary(t, writeAuxiliaryFrame(ctx, b, ack))
	// The receiver abandons its final response; the helper may already have
	// queued it. Either send outcome must leave the same durable delivery.
	mustAuxiliary(t, b.Close())
	<-done
	before := f.native.calls
	observed, err := transferObserve(t, f)
	captured.Stage = "delivered"
	if err != nil || !reflect.DeepEqual(observed, captured) || before != f.native.calls {
		t.Fatal("lost final receipt was not safely observable", err)
	}
}

func TestAuxiliaryTransferCommitFailureAndRevocationRetainIntent(t *testing.T) {
	for _, failure := range []string{"source", "permission", "key", "state owner", "policy read", "journal publication", "cancel"} {
		t.Run(failure, func(t *testing.T) {
			f := newAuxiliaryTransferFixture(t)
			if failure == "journal publication" {
				f.runtime.beforePublish = func(name string) error {
					if strings.Contains(name, "delivered") {
						return errors.New("synthetic receipt publication fault")
					}
					return nil
				}
			}
			if failure == "policy read" {
				f.runtime.policy = func() (Policy, error) { return Policy{}, errors.New("synthetic policy read failure") }
			}
			if failure == "permission" || failure == "key" || failure == "state owner" {
				// A real policy reload decodes independent bytes. Do not mutate the
				// original slice/map while a simulated server is reading its grant.
				data, err := json.Marshal(f.policy)
				mustAuxiliary(t, err)
				var refreshed Policy
				mustAuxiliary(t, json.Unmarshal(data, &refreshed))
				switch failure {
				case "permission":
					refreshed.Auxiliary[0].AllowCapture = false
				case "key":
					delete(refreshed.Keys, f.request.KeyID)
				case "state owner":
					refreshed.Auxiliary[0].StateUID++
				}
				f.runtime.policy = func() (Policy, error) { return refreshed, nil }
			}
			x, done := transferStart(t, f)
			switch failure {
			case "source":
				mustAuxiliary(t, os.WriteFile(filepath.Join(f.root, "state/tpm/empty"), []byte{1}, 0600))
			}
			ctx, cancel := context.WithCancel(context.Background())
			if failure == "cancel" {
				cancel()
			}
			out, err := x.Commit(ctx)
			cancel()
			if err == nil || !reflect.DeepEqual(out, AuxiliaryResponse{}) {
				t.Fatal("failed commit returned delivery proof", err)
			}
			if serverErr := <-done; serverErr == nil {
				t.Fatal("helper recorded a failed commit as delivered")
			}
			// Restore only test policy so authenticated historical observation can
			// prove incompleteness; no native/source repair or retry is performed.
			f.policy.APIVersion = domain.APIVersion
			f.policy.Auxiliary[0].AllowCapture = true
			f.policy.Auxiliary[0].StateUID = uint32(os.Getuid())
			f.policy.Keys[f.request.KeyID] = hex.EncodeToString(f.key.Public().(ed25519.PublicKey))
			transferNoDelivery(t, f)
		})
	}
}

func TestAuxiliaryTransferMalformedAcknowledgements(t *testing.T) {
	for _, bad := range []string{"version", "job", "binding", "digest", "unknown", "duplicate", "case alias", "descriptor"} {
		t.Run(bad, func(t *testing.T) {
			f := newAuxiliaryTransferFixture(t)
			a, b := auxiliaryTransferPair(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := transferTestServe(ctx, a, f, f.request, &f.exec)
			captured, snapshot, err := receiveAuxiliaryDelivery(ctx, b, f.request, "captured", true)
			mustAuxiliary(t, err)
			defer snapshot.Close()
			ack := auxiliaryAcknowledgement{1, captured.JobID, captured.Binding, captured.Artifact.SHA256}
			switch bad {
			case "version":
				ack.Version = 2
			case "job":
				ack.JobID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
			case "binding":
				ack.Binding = strings.Repeat("e", 64)
			case "digest":
				ack.ArtifactSHA256 = strings.Repeat("e", 64)
			}
			data, err := json.Marshal(ack)
			mustAuxiliary(t, err)
			if bad == "unknown" {
				data = append(data[:len(data)-1], []byte(`,"extra":true}`)...)
			}
			if bad == "duplicate" {
				data = append(data[:len(data)-1], []byte(`,"version":1}`)...)
			}
			if bad == "case alias" {
				data = append(data[:len(data)-1], []byte(`,"Version":1}`)...)
			}
			var rights []byte
			if bad == "descriptor" {
				rights = unix.UnixRights(int(snapshot.Fd()))
			}
			_, _, err = b.WriteMsgUnix(append(data, '\n'), rights, nil)
			mustAuxiliary(t, err)
			out, fd, err := receiveAuxiliaryDelivery(ctx, b, f.request, "delivered", false)
			if fd != nil {
				fd.Close()
			}
			if err == nil || !reflect.DeepEqual(out, AuxiliaryResponse{}) || <-done == nil {
				t.Fatal("malformed acknowledgement authorized delivery")
			}
			transferNoDelivery(t, f)
		})
	}
}

func transferMetadataFixture(t *testing.T) (Request, AuxiliaryResponse, *os.File) {
	t.Helper()
	f := newAuxiliaryCaptureFixture(t)
	c, err := f.exec.Capture(context.Background(), f.request, f.policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return f.request, c.Response, c.Snapshot
}

func TestAuxiliaryDeliveryMetadataRejectsContradictions(t *testing.T) {
	r, response, _ := transferMetadataFixture(t)
	for name, edit := range map[string]func(*AuxiliaryResponse){
		"wrong version":       func(r *AuxiliaryResponse) { r.Version = 2 },
		"wrong stage":         func(r *AuxiliaryResponse) { r.Stage = "complete" },
		"wrong job":           func(r *AuxiliaryResponse) { r.JobID = "unknown" },
		"wrong binding":       func(r *AuxiliaryResponse) { r.Binding = strings.Repeat("f", 64) },
		"missing inventory":   func(r *AuxiliaryResponse) { r.Inventory = nil },
		"changed source":      func(r *AuxiliaryResponse) { r.Inventory.Members[0].State.Generation += "changed" },
		"missing artifact":    func(r *AuxiliaryResponse) { r.Artifact = nil },
		"wrong format":        func(r *AuxiliaryResponse) { r.Artifact.Format = "tar" },
		"wrong inventory SHA": func(r *AuxiliaryResponse) { r.Artifact.InventoryDigest = strings.Repeat("f", 64) },
		"missing SHA":         func(r *AuxiliaryResponse) { r.Artifact.SHA256 = "" },
		"noncanonical SHA":    func(r *AuxiliaryResponse) { r.Artifact.SHA256 = strings.ToUpper(r.Artifact.SHA256) },
		"missing member SHA":  func(r *AuxiliaryResponse) { delete(r.Artifact.MemberSHA256, "members/000") },
		"extra lock SHA":      func(r *AuxiliaryResponse) { r.Artifact.MemberSHA256["tpm-lock"] = strings.Repeat("f", 64) },
		"wrong archive size":  func(r *AuxiliaryResponse) { r.Artifact.Size++ },
		"unbounded archive":   func(r *AuxiliaryResponse) { r.Artifact.Size = ^uint64(0) },
	} {
		t.Run(name, func(t *testing.T) {
			bad, err := auxiliaryResponseCopy(response)
			mustAuxiliary(t, err)
			edit(&bad)
			if err := ValidateAuxiliaryDelivery(r, &bad); err == nil {
				t.Fatal("contradictory metadata validated")
			}
		})
	}
	for _, stage := range []string{"captured", "delivered"} {
		response.Stage = stage
		mustAuxiliary(t, ValidateAuxiliaryDelivery(r, &response))
	}
}

func TestAuxiliaryDeliveryArchiveRejectsIntegrityAndCanonicalDrift(t *testing.T) {
	r, response, snapshot := transferMetadataFixture(t)
	data, err := io.ReadAll(io.NewSectionReader(snapshot, 0, int64(response.Artifact.Size)))
	mustAuxiliary(t, err)
	for _, bad := range []string{"header", "payload", "padding", "footer", "archive hash", "member hash", "unsealed"} {
		t.Run(bad, func(t *testing.T) {
			meta, err := auxiliaryResponseCopy(response)
			mustAuxiliary(t, err)
			copy := append([]byte(nil), data...)
			switch bad {
			case "header":
				copy[0] = 'x'
			case "payload":
				copy[512] ^= 1
			case "padding":
				copy[512+meta.Inventory.Members[0].State.Size] = 1
			case "footer":
				copy[len(copy)-1] = 1
			case "archive hash":
				meta.Artifact.SHA256 = strings.Repeat("f", 64)
			case "member hash":
				meta.Artifact.MemberSHA256["members/000"] = strings.Repeat("f", 64)
			}
			if bad == "header" || bad == "padding" || bad == "footer" || bad == "payload" {
				meta.Artifact.SHA256 = auxiliaryCaptureHash(copy)
			}
			f, err := CaptureSealed(context.Background(), bytes.NewReader(copy), int64(len(copy)))
			mustAuxiliary(t, err)
			defer f.Close()
			if bad == "unsealed" {
				f.Close()
				f, err = os.CreateTemp(t.TempDir(), "unsealed-original-fixture")
				mustAuxiliary(t, err)
				defer f.Close()
				_, err = f.Write(copy)
				mustAuxiliary(t, err)
			}
			if err := validateAuxiliaryArchive(context.Background(), r, &meta, f); err == nil {
				t.Fatal("invalid sealed archive passed full byte verification")
			}
		})
	}
	mustAuxiliary(t, validateAuxiliaryArchive(context.Background(), r, &response, snapshot))
}

func TestAuxiliaryTransferFrameFailuresCloseReceivedDescriptors(t *testing.T) {
	r, response, snapshot := transferMetadataFixture(t)
	for _, bad := range []string{"missing descriptor", "extra descriptor", "error descriptor", "mixed success", "wrong family", "unknown field", "duplicate field", "case alias", "null substitute", "delivered before commit", "trailing JSON"} {
		t.Run(bad, func(t *testing.T) {
			a, b := auxiliaryTransferPair(t)
			baseline := sealedFDCount(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			meta, err := auxiliaryResponseCopy(response)
			mustAuxiliary(t, err)
			outer := Response{APIVersion: domain.APIVersion, Success: true, Auxiliary: &meta}
			rights := unix.UnixRights(int(snapshot.Fd()))
			switch bad {
			case "missing descriptor":
				rights = nil
			case "extra descriptor":
				rights = unix.UnixRights(int(snapshot.Fd()), int(snapshot.Fd()))
			case "error descriptor":
				outer = Response{APIVersion: domain.APIVersion, Success: false, Error: "synthetic denial", ErrorCode: "PERMISSION_DENIED"}
			case "mixed success":
				outer.Error = "synthetic error"
			case "wrong family":
				outer.Access = json.RawMessage(`{}`)
			case "delivered before commit":
				meta.Stage = "delivered"
			}
			data, err := json.Marshal(outer)
			mustAuxiliary(t, err)
			switch bad {
			case "unknown field":
				data = append(data[:len(data)-1], []byte(`,"unknown":1}`)...)
			case "duplicate field":
				data = append(data[:len(data)-1], []byte(`,"success":true}`)...)
			case "case alias":
				data = append(data[:len(data)-1], []byte(`,"Success":true}`)...)
			case "null substitute":
				data = append(data[:len(data)-1], []byte(`,"access":null}`)...)
			case "trailing JSON":
				data = append(data, []byte(`{}`)...)
			}
			done := make(chan error, 1)
			go func() {
				_, _, err := a.WriteMsgUnix(append(data, '\n'), rights, nil)
				done <- err
			}()
			out, fd, err := receiveAuxiliaryDelivery(ctx, b, r, "captured", true)
			mustAuxiliary(t, <-done)
			if fd != nil {
				fd.Close()
			}
			if err == nil || !reflect.DeepEqual(out, AuxiliaryResponse{}) {
				t.Fatal("malformed helper frame returned successful capture metadata")
			}
			if after := sealedFDCount(t); after != baseline {
				t.Fatalf("malformed transfer leaked received descriptors: %d -> %d", baseline, after)
			}
		})
	}
}

func TestAuxiliaryTransferJournalRefusesCorruptionWithoutNativeWork(t *testing.T) {
	for _, bad := range []string{"truncated intent", "wrong intent binding", "wrong intent request", "aliased intent JSON", "truncated delivery", "wrong delivery digest", "symbolic record", "hardlinked record", "FIFO record", "overlong record", "journal mode"} {
		t.Run(bad, func(t *testing.T) {
			f := newAuxiliaryTransferFixture(t)
			x, done := transferStart(t, f)
			_, err := x.Commit(context.Background())
			mustAuxiliary(t, err)
			mustAuxiliary(t, <-done)
			mustAuxiliary(t, x.Close())
			intent := filepath.Join(f.runtime.journalDirectory, f.request.JobID+".auxiliary-intent.json")
			delivery := filepath.Join(f.runtime.journalDirectory, f.request.JobID+".auxiliary-delivered.json")
			switch bad {
			case "truncated intent":
				mustAuxiliary(t, os.WriteFile(intent, []byte(`{`), 0600))
			case "wrong intent binding", "wrong intent request", "aliased intent JSON":
				data, err := os.ReadFile(intent)
				mustAuxiliary(t, err)
				var value auxiliaryDeliveryIntent
				mustAuxiliary(t, json.Unmarshal(data, &value))
				if bad == "wrong intent binding" {
					value.Binding = strings.Repeat("f", 64)
				} else if bad == "wrong intent request" {
					value.Request.RootID = "other"
				}
				data, err = json.Marshal(value)
				mustAuxiliary(t, err)
				if bad == "aliased intent JSON" {
					data = append(data[:len(data)-1], []byte(`,"Version":1}`)...)
				}
				mustAuxiliary(t, os.WriteFile(intent, data, 0600))
			case "truncated delivery":
				mustAuxiliary(t, os.WriteFile(delivery, nil, 0600))
			case "wrong delivery digest":
				data, err := os.ReadFile(delivery)
				mustAuxiliary(t, err)
				var value AuxiliaryResponse
				mustAuxiliary(t, json.Unmarshal(data, &value))
				value.Artifact.InventoryDigest = strings.Repeat("f", 64)
				data, err = json.Marshal(value)
				mustAuxiliary(t, err)
				mustAuxiliary(t, os.WriteFile(delivery, data, 0600))
			case "symbolic record":
				mustAuxiliary(t, os.Rename(delivery, delivery+"-original"))
				mustAuxiliary(t, os.Symlink(filepath.Base(delivery)+"-original", delivery))
			case "hardlinked record":
				mustAuxiliary(t, os.Link(delivery, delivery+"-alias"))
			case "FIFO record":
				mustAuxiliary(t, os.Remove(delivery))
				mustAuxiliary(t, unix.Mkfifo(delivery, 0600))
			case "overlong record":
				mustAuxiliary(t, os.WriteFile(delivery, bytes.Repeat([]byte{'x'}, maxSnapshotEnvelope+1), 0600))
			case "journal mode":
				mustAuxiliary(t, os.Chmod(f.runtime.journalDirectory, 0755))
			}
			before := f.native.calls
			out, err := transferObserve(t, f)
			if err == nil || !reflect.DeepEqual(out, AuxiliaryResponse{}) || f.native.calls != before {
				t.Fatal("corrupted historical records returned proof or native replay", err)
			}
		})
	}
}

func TestAuxiliaryTransferJournalFaultBeforeCopyAndRootSubstitution(t *testing.T) {
	for _, bad := range []string{"intent fault", "canceled before intent", "journal substituted"} {
		t.Run(bad, func(t *testing.T) {
			f := newAuxiliaryTransferFixture(t)
			a, b := auxiliaryTransferPair(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			f.runtime.beforePublish = func(name string) error {
				if !strings.Contains(name, "intent") {
					return nil
				}
				switch bad {
				case "intent fault":
					return errors.New("synthetic intent publication fault")
				case "canceled before intent":
					cancel()
				case "journal substituted":
					if err := os.Rename(f.runtime.journalDirectory, f.runtime.journalDirectory+"-original"); err != nil {
						return err
					}
					return os.Mkdir(f.runtime.journalDirectory, 0700)
				}
				return nil
			}
			done := transferTestServe(ctx, a, f, f.request, &f.exec)
			out, fd, err := receiveAuxiliaryDelivery(ctx, b, f.request, "captured", true)
			if fd != nil {
				fd.Close()
			}
			if err == nil || !reflect.DeepEqual(out, AuxiliaryResponse{}) || <-done == nil || f.native.calls != 0 {
				t.Fatal("journal failure reached native capture or returned success")
			}
			files, err := os.ReadDir(f.runtime.journalDirectory)
			mustAuxiliary(t, err)
			if len(files) != 0 {
				t.Fatal("pre-intent fault produced a successful journal record")
			}
		})
	}
}

func TestAuxiliaryTransferCallerMutationAndCancellationNeverAcknowledge(t *testing.T) {
	for _, bad := range []string{"response", "descriptor", "cancel"} {
		t.Run(bad, func(t *testing.T) {
			f := newAuxiliaryTransferFixture(t)
			x, done := transferStart(t, f)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch bad {
			case "response":
				x.Response.Artifact.MemberSHA256["members/000"] = strings.Repeat("f", 64)
			case "descriptor":
				x.Snapshot = nil
			case "cancel":
				cancel()
			}
			out, err := x.Commit(ctx)
			if err == nil || !reflect.DeepEqual(out, AuxiliaryResponse{}) || <-done == nil {
				t.Fatal("invalid or canceled client acknowledged delivery")
			}
			if _, err := x.snapshot.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Fatal("failed commit retained the received descriptor", err)
			}
			transferNoDelivery(t, f)
		})
	}
}

func TestAuxiliaryTransferContextAndPublicMethodRefusals(t *testing.T) {
	r, _, _ := transferMetadataFixture(t)
	client := Client{KeyPath: "must-not-be-opened"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out, err := client.CaptureAuxiliary(ctx, r); out != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled public capture accessed setup or returned success", err)
	}
	r.Mode = "observe"
	if out, err := client.ObserveAuxiliary(ctx, r); !reflect.DeepEqual(out, AuxiliaryResponse{}) || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled public observation accessed setup or returned success", err)
	}
	r.Operation = "storage.prepare-directory"
	if out, err := client.CaptureAuxiliary(context.Background(), r); out != nil || err == nil {
		t.Fatal("foreign operation entered capture client")
	}
	if out, err := client.ObserveAuxiliary(context.Background(), r); !reflect.DeepEqual(out, AuxiliaryResponse{}) || err == nil {
		t.Fatal("foreign operation entered observation client")
	}
	soon := time.Now().Add(time.Second)
	bounded, stop := auxiliaryTransferContext(context.Background(), soon)
	defer stop()
	deadline, ok := bounded.Deadline()
	if !ok || !deadline.Equal(soon) {
		t.Fatal("transfer outlives request expiry")
	}
	bounded, stop = auxiliaryTransferContext(context.Background(), time.Now().Add(time.Hour))
	defer stop()
	deadline, ok = bounded.Deadline()
	if !ok || time.Until(deadline) > 120*time.Second {
		t.Fatal("transfer lacks overall 120 second bound")
	}
}
