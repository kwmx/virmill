//go:build linux && amd64

package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func sealedPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		if os.Getenv("VIRMILL_TEST_REQUIRE_IPC") == "1" {
			t.Fatal(err)
		}
		t.Skipf("BLOCKED: private Unix descriptor IPC unavailable: %v", err)
	}
	conns := make([]*net.UnixConn, 2)
	// FileConn duplicates its input and may itself be denied by the outer
	// sandbox. Keep both original descriptors owned until that step completes.
	for _, fd := range fds {
		t.Cleanup(func() { _ = unix.Close(fd) })
	}
	for i, fd := range fds {
		dup, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		f := os.NewFile(uintptr(dup), "private test socket")
		c, err := net.FileConn(f)
		_ = f.Close()
		if err != nil {
			if os.Getenv("VIRMILL_TEST_REQUIRE_IPC") != "1" && (errors.Is(err, unix.EPERM) || errors.Is(err, unix.EACCES)) {
				t.Skipf("BLOCKED: private Unix descriptor IPC unavailable: %v", err)
			}
			t.Fatal(err)
		}
		conns[i] = c.(*net.UnixConn)
		t.Cleanup(func() { c.Close() })
	}
	return conns[0], conns[1]
}
func sealedFixture(t *testing.T) *os.File {
	t.Helper()
	f, err := CaptureSealed(context.Background(), strings.NewReader("synthetic TPM state"), 19)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
func TestSealedSnapshotTransferAndImmutability(t *testing.T) {
	f := sealedFixture(t)
	a, b := sealedPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	meta, _ := json.Marshal(map[string]any{"version": 1, "fixture": "metadata only"})
	done := make(chan error, 1)
	go func() { done <- SendSealedSnapshot(ctx, a, meta, f) }()
	got, received, err := ReceiveSealedSnapshot(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	defer received.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, meta) {
		t.Fatal("metadata changed")
	}
	data, err := io.ReadAll(io.NewSectionReader(received, 0, 19))
	if err != nil || string(data) != "synthetic TPM state" {
		t.Fatal("sealed contents changed", err)
	}
	if _, err = received.WriteAt([]byte("x"), 0); !errors.Is(err, unix.EPERM) {
		t.Fatal("received snapshot writable", err)
	}
	if err = received.Truncate(1); !errors.Is(err, unix.EPERM) {
		t.Fatal("received snapshot truncatable", err)
	}
	flags, err := unix.FcntlInt(received.Fd(), unix.F_GETFD, 0)
	if err != nil || flags&unix.FD_CLOEXEC == 0 {
		t.Fatal("received descriptor inheritable", err)
	}
	// Closing the sender's reference must not destroy the receiver's contents.
	f.Close()
	data, err = io.ReadAll(io.NewSectionReader(received, 0, 19))
	if err != nil || string(data) != "synthetic TPM state" {
		t.Fatal("snapshot lifetime depended on sender", err)
	}
}
func TestSealedCaptureRejectsWrongLengthAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		data string
		size int64
	}{{"a", 0}, {"a", MaxAuxiliarySnapshotBytes + 1}, {"a", 2}, {"ab", 1}} {
		if f, err := CaptureSealed(context.Background(), strings.NewReader(tc.data), tc.size); err == nil {
			f.Close()
			t.Fatal("invalid source size accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if f, err := CaptureSealed(ctx, strings.NewReader("a"), 1); !errors.Is(err, context.Canceled) || f != nil {
		t.Fatal("canceled capture succeeded", err)
	}
}
func TestSealedReceiveRejectsDescriptorForgeriesWithoutLeaks(t *testing.T) {
	for _, kind := range []string{"missing", "multiple", "truncated-control", "unsealed", "malformed-json", "two-frames", "oversized-metadata"} {
		t.Run(kind, func(t *testing.T) {
			f := sealedFixture(t)
			a, b := sealedPair(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			rights := unix.UnixRights(int(f.Fd()))
			payload := []byte("{\"version\":1}\n")
			if kind == "missing" {
				rights = nil
			}
			if kind == "multiple" {
				rights = unix.UnixRights(int(f.Fd()), int(f.Fd()))
			}
			if kind == "truncated-control" {
				fds := make([]int, 32)
				for i := range fds {
					fds[i] = int(f.Fd())
				}
				rights = unix.UnixRights(fds...)
			}
			if kind == "unsealed" {
				fd, err := unix.MemfdCreate("unsealed-test", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
				if err != nil {
					t.Fatal(err)
				}
				defer unix.Close(fd)
				if _, err = unix.Write(fd, []byte("mutable")); err != nil {
					t.Fatal(err)
				}
				rights = unix.UnixRights(fd)
			}
			if kind == "malformed-json" {
				payload = []byte("{\"a\":1,\"a\":2}\n")
			}
			if kind == "two-frames" {
				payload = []byte("{}\n{}\n")
			}
			if kind == "oversized-metadata" {
				payload = []byte("{\"x\":\"" + strings.Repeat("x", maxSnapshotEnvelope) + "\"}\n")
			}
			baseline, err := os.ReadDir("/proc/self/fd")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				_ = a.SetWriteDeadline(time.Now().Add(2 * time.Second))
				n, _, err := a.WriteMsgUnix(payload, rights, nil)
				if err == nil && n < len(payload) {
					_, err = a.Write(payload[n:])
				}
				done <- err
			}()
			_, got, err := ReceiveSealedSnapshot(ctx, b)
			if err == nil || got != nil {
				if got != nil {
					got.Close()
				}
				t.Fatal("forged snapshot accepted")
			}
			if sendErr := <-done; sendErr != nil {
				t.Fatal("forged fixture was not fully delivered", sendErr)
			}
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				t.Fatal("fixture was rejected only because the receiver timed out", err)
			}
			after, err := os.ReadDir("/proc/self/fd")
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != len(baseline) {
				t.Fatalf("descriptor leak: before=%d after=%d", len(baseline), len(after))
			}
		})
	}
}
func TestSealedReceiveCancellation(t *testing.T) {
	_, b := sealedPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, f, err := ReceiveSealedSnapshot(ctx, b)
		if f != nil {
			f.Close()
		}
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled receive succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled receive remained blocked")
	}
}

type sealedReaderFunc func([]byte) (int, error)

func (f sealedReaderFunc) Read(p []byte) (int, error) { return f(p) }

func TestSealedCaptureCancellationAtFinalEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reads := 0
	source := sealedReaderFunc(func(p []byte) (int, error) {
		reads++
		if reads == 1 {
			p[0] = 'a'
			return 1, nil
		}
		cancel()
		return 0, io.EOF
	})
	f, err := CaptureSealed(ctx, source, 1)
	if f != nil {
		defer f.Close()
	}
	if !errors.Is(err, context.Canceled) || f != nil {
		t.Fatalf("capture returned success after cancellation during the terminal EOF read: file=%v err=%v", f, err)
	}
}

func TestSealedSendRejectsMultilineMetadataBeforeTransfer(t *testing.T) {
	for _, meta := range []string{"{\n\"version\":1\n}", "{}\n", "\n{}"} {
		t.Run(strconv.Quote(meta), func(t *testing.T) {
			a, b := sealedPair(t)
			f := sealedFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := SendSealedSnapshot(ctx, a, []byte(meta), f); err == nil {
				t.Error("multiline JSON accepted despite the newline frame delimiter")
			}
			// Closing the write side exposes EOF immediately and lets the receiver
			// close any incorrectly transferred descriptor without a timeout race.
			if err := a.CloseWrite(); err != nil {
				t.Fatal(err)
			}
			buf, oob := make([]byte, 128), make([]byte, unix.CmsgSpace(4))
			n, on, _, _, err := b.ReadMsgUnix(buf, oob)
			if on != 0 {
				messages, parseErr := unix.ParseSocketControlMessage(oob[:on])
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				for _, message := range messages {
					fds, parseErr := unix.ParseUnixRights(&message)
					if parseErr != nil {
						t.Fatal(parseErr)
					}
					for _, fd := range fds {
						_ = unix.Close(fd)
					}
				}
			}
			if n != 0 || on != 0 || (err != nil && !errors.Is(err, io.EOF)) {
				t.Fatalf("invalid metadata transferred bytes or rights: bytes=%d control=%d err=%v", n, on, err)
			}
		})
	}
}

func TestSealedReceiveRejectsWriteOnlySnapshot(t *testing.T) {
	f := sealedFixture(t)
	fd, err := unix.Open("/proc/self/fd/"+strconv.Itoa(int(f.Fd())), unix.O_WRONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	a, b := sealedPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err = a.WriteMsgUnix([]byte("{}\n"), unix.UnixRights(fd), nil); err != nil {
		t.Fatal(err)
	}
	_, got, err := ReceiveSealedSnapshot(ctx, b)
	if got != nil {
		defer got.Close()
	}
	if err == nil || got != nil {
		var readErr error
		if got != nil {
			_, readErr = got.ReadAt(make([]byte, 1), 0)
		}
		t.Fatalf("unreadable snapshot accepted: file=%v err=%v read=%v", got, err, readErr)
	}
}

func sealedSparseFixture(t *testing.T, size int64, seals int) *os.File {
	t.Helper()
	fd, err := unix.MemfdCreate("private-sealed-boundary-fixture", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		t.Fatal(err)
	}
	f := os.NewFile(uintptr(fd), "private sealed boundary fixture")
	t.Cleanup(func() { _ = f.Close() })
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, seals); err != nil {
		t.Fatal(err)
	}
	return f
}

func sealedFDCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestSealedReceiveRejectsInvalidObjectsWithoutLeaks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		size  int64
		seals int
	}{
		{"empty", 0, snapshotSeals},
		{"oversized", MaxAuxiliarySnapshotBytes + 1, snapshotSeals},
		{"missing-write-seal", 1, snapshotSeals &^ unix.F_SEAL_WRITE},
		{"missing-grow-seal", 1, snapshotSeals &^ unix.F_SEAL_GROW},
		{"missing-shrink-seal", 1, snapshotSeals &^ unix.F_SEAL_SHRINK},
		{"missing-seal-seal", 1, snapshotSeals &^ unix.F_SEAL_SEAL},
		{"future-write-is-insufficient", 1, snapshotSeals&^unix.F_SEAL_WRITE | unix.F_SEAL_FUTURE_WRITE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := sealedSparseFixture(t, tc.size, tc.seals)
			a, b := sealedPair(t)
			baseline := sealedFDCount(t)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if n, _, err := a.WriteMsgUnix([]byte("{}\n"), unix.UnixRights(int(f.Fd())), nil); err != nil || n != 3 {
				t.Fatalf("send fixture: bytes=%d err=%v", n, err)
			}
			_, got, err := ReceiveSealedSnapshot(ctx, b)
			if got != nil {
				_ = got.Close()
			}
			if err == nil || got != nil {
				t.Fatal("invalid sealed object accepted", err)
			}
			if after := sealedFDCount(t); after != baseline {
				t.Fatalf("descriptor leak: before=%d after=%d", baseline, after)
			}
		})
	}
}

func sealedSetSocketOption(t *testing.T, conn *net.UnixConn, option, value int) {
	t.Helper()
	raw, err := conn.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var optionErr error
	if err := raw.Control(func(fd uintptr) {
		optionErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, option, value)
	}); err != nil || optionErr != nil {
		t.Fatalf("set private socket option: %v %v", err, optionErr)
	}
}

func sealedWaitQueued(t *testing.T, conn *net.UnixConn) int {
	t.Helper()
	raw, err := conn.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var size int
		var queryErr error
		if err := raw.Control(func(fd uintptr) {
			size, queryErr = unix.IoctlGetInt(int(fd), unix.TIOCINQ)
		}); err != nil || queryErr != nil {
			t.Fatalf("query private socket queue: %v %v", err, queryErr)
		}
		if size > 0 {
			return size
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("sender did not queue any frame bytes")
	return 0
}

func TestSealedSnapshotMaximumBoundsAndPartialWrite(t *testing.T) {
	// A sparse memfd proves the exact size boundary without allocating 256 MiB.
	f := sealedSparseFixture(t, MaxAuxiliarySnapshotBytes, snapshotSeals)
	a, b := sealedPair(t)
	sealedSetSocketOption(t, a, unix.SO_SNDBUF, 4096)
	meta := []byte(`{"x":"` + strings.Repeat("x", maxSnapshotEnvelope-len(`{"x":""}`)) + `"}`)
	if len(meta) != maxSnapshotEnvelope {
		t.Fatal("incorrect boundary fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- SendSealedSnapshot(ctx, a, meta, f) }()
	if queued := sealedWaitQueued(t, b); queued >= len(meta)+1 {
		t.Fatalf("fixture did not force a partial stream write: queued=%d frame=%d", queued, len(meta)+1)
	}
	select {
	case err := <-done:
		t.Fatalf("large send finished before the receiver drained the small socket buffer: %v", err)
	default:
	}
	got, received, err := ReceiveSealedSnapshot(ctx, b)
	if err != nil {
		t.Fatal("partial-write transfer failed", err)
	}
	defer received.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, meta) {
		t.Fatal("partial writes lost or repeated metadata bytes")
	}
	st, err := received.Stat()
	if err != nil || st.Size() != MaxAuxiliarySnapshotBytes {
		t.Fatal("maximum-size sealed object changed", err)
	}
	var tail [1]byte
	if n, err := received.ReadAt(tail[:], MaxAuxiliarySnapshotBytes-1); err != nil || n != 1 || tail[0] != 0 {
		t.Fatalf("last byte inaccessible: n=%d data=%v err=%v", n, tail, err)
	}
}

func TestSealedSendCancellationWhileBlocked(t *testing.T) {
	f := sealedFixture(t)
	a, b := sealedPair(t)
	sealedSetSocketOption(t, a, unix.SO_SNDBUF, 4096)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	meta := []byte(`{"x":"` + strings.Repeat("x", maxSnapshotEnvelope-len(`{"x":""}`)) + `"}`)
	go func() { done <- SendSealedSnapshot(ctx, a, meta, f) }()
	sealedWaitQueued(t, b)
	select {
	case err := <-done:
		t.Fatalf("send was not blocked when cancellation was tested: %v", err)
	default:
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("blocked canceled send succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled send remained blocked")
	}
	if err := validateSealed(f); err != nil {
		t.Fatal("failed send consumed the caller's snapshot descriptor", err)
	}
}

func TestSealedReceiveCancellationAfterDescriptorArrival(t *testing.T) {
	f := sealedFixture(t)
	a, b := sealedPair(t)
	baseline := sealedFDCount(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, received, err := ReceiveSealedSnapshot(ctx, b)
		if received != nil {
			_ = received.Close()
		}
		done <- err
	}()
	if n, _, err := a.WriteMsgUnix([]byte(`{"x":`), unix.UnixRights(int(f.Fd())), nil); err != nil || n != 5 {
		t.Fatalf("send incomplete frame: n=%d err=%v", n, err)
	}
	// Wait until recvmsg has installed the descriptor in this process. Merely
	// canceling a goroutine immediately can exercise only the pre-canceled path.
	deadline := time.Now().Add(time.Second)
	for sealedFDCount(t) == baseline && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if installed := sealedFDCount(t); installed != baseline+1 {
		t.Fatalf("receiver did not acquire exactly one descriptor: before=%d now=%d", baseline, installed)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("incomplete canceled receive succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled receive remained blocked after acquiring a descriptor")
	}
	if after := sealedFDCount(t); after != baseline {
		t.Fatalf("cancellation leaked received descriptor: before=%d after=%d", baseline, after)
	}
}

func TestSealedReceiveUnexpectedCredentialsWithoutLeaks(t *testing.T) {
	f := sealedFixture(t)
	a, b := sealedPair(t)
	sealedSetSocketOption(t, b, unix.SO_PASSCRED, 1)
	baseline := sealedFDCount(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if n, _, err := a.WriteMsgUnix([]byte("{}\n"), unix.UnixRights(int(f.Fd())), nil); err != nil || n != 3 {
		t.Fatalf("send fixture: n=%d err=%v", n, err)
	}
	_, got, err := ReceiveSealedSnapshot(ctx, b)
	if got != nil {
		_ = got.Close()
	}
	if err == nil || got != nil {
		t.Fatal("unexpected ancillary credentials accepted", err)
	}
	if after := sealedFDCount(t); after != baseline {
		t.Fatalf("unexpected ancillary control leaked descriptor: before=%d after=%d", baseline, after)
	}
}

func TestSealedReceiveFragmentedFrames(t *testing.T) {
	for _, kind := range []string{"descriptor-first", "descriptor-last", "duplicate-later", "EOF-before-delimiter"} {
		t.Run(kind, func(t *testing.T) {
			f := sealedFixture(t)
			a, b := sealedPair(t)
			baseline := sealedFDCount(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			type result struct {
				metadata []byte
				file     *os.File
				err      error
			}
			done := make(chan result, 1)
			go func() {
				metadata, file, err := ReceiveSealedSnapshot(ctx, b)
				done <- result{metadata, file, err}
			}()
			rights := unix.UnixRights(int(f.Fd()))
			if kind == "descriptor-last" {
				rights = nil
			}
			if n, _, err := a.WriteMsgUnix([]byte(`{"x":`), rights, nil); err != nil || n != 5 {
				t.Fatalf("first fragment: n=%d err=%v", n, err)
			}
			if kind != "descriptor-last" {
				deadline := time.Now().Add(time.Second)
				for sealedFDCount(t) == baseline && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if installed := sealedFDCount(t); installed != baseline+1 {
					t.Fatalf("first fragment did not install exactly one FD: before=%d now=%d", baseline, installed)
				}
			}
			if kind == "EOF-before-delimiter" {
				if err := a.CloseWrite(); err != nil {
					t.Fatal(err)
				}
			} else {
				rights = nil
				if kind == "descriptor-last" || kind == "duplicate-later" {
					rights = unix.UnixRights(int(f.Fd()))
				}
				if n, _, err := a.WriteMsgUnix([]byte("1}\n"), rights, nil); err != nil || n != 3 {
					t.Fatalf("second fragment: n=%d err=%v", n, err)
				}
			}
			got := <-done
			if got.file != nil {
				_ = got.file.Close()
			}
			switch kind {
			case "descriptor-first", "descriptor-last":
				if got.err != nil || got.file == nil || string(got.metadata) != `{"x":1}` {
					t.Fatalf("valid fragmented frame rejected or changed: %q file=%v err=%v", got.metadata, got.file, got.err)
				}
			case "EOF-before-delimiter":
				if !errors.Is(got.err, io.ErrUnexpectedEOF) || got.file != nil || got.metadata != nil {
					t.Fatalf("incomplete frame not refused as EOF: %#v", got)
				}
			case "duplicate-later":
				if got.err == nil || got.file != nil || got.metadata != nil {
					t.Fatalf("descriptor in later fragment bypassed cardinality check: %#v", got)
				}
			}
			if after := sealedFDCount(t); after != baseline {
				t.Fatalf("fragmented frame leaked received FD: before=%d after=%d", baseline, after)
			}
		})
	}
}

func TestSealedCaptureSourceFailuresCloseMemfd(t *testing.T) {
	for _, kind := range []string{"copy-error", "final-read-error", "no-final-EOF", "canceled-during-copy"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			source := sealedReaderFunc(func(p []byte) (int, error) {
				calls++
				if calls == 1 {
					if kind == "canceled-during-copy" {
						cancel()
					}
					clear(p)
					return len(p), nil
				}
				if kind == "no-final-EOF" {
					return 0, nil
				}
				return 0, errors.New("synthetic source read failure")
			})
			size := int64(1)
			if kind == "copy-error" {
				size = (64 << 10) + 1
			}
			baseline := sealedFDCount(t)
			f, err := CaptureSealed(ctx, source, size)
			if f != nil {
				_ = f.Close()
			}
			if err == nil || f != nil {
				t.Fatal("incomplete source or canceled capture accepted", err)
			}
			if kind == "canceled-during-copy" && !errors.Is(err, context.Canceled) {
				t.Fatal("capture did not report cancellation", err)
			}
			if after := sealedFDCount(t); after != baseline {
				t.Fatalf("failed capture leaked memfd: before=%d after=%d", baseline, after)
			}
		})
	}
}

func TestSealedSnapshotReadOnlyDescriptorAndEscapedNewline(t *testing.T) {
	f := sealedFixture(t)
	fd, err := unix.Open("/proc/self/fd/"+strconv.Itoa(int(f.Fd())), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	readOnly := os.NewFile(uintptr(fd), "read-only sealed fixture")
	defer readOnly.Close()
	a, b := sealedPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	metadata := []byte(`{"escaped":"line one\nline two","unicode":"\u000a"}`)
	if err := SendSealedSnapshot(ctx, a, metadata, readOnly); err != nil {
		t.Fatal("readable sealed descriptor or escaped newline refused", err)
	}
	got, received, err := ReceiveSealedSnapshot(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	defer received.Close()
	if !bytes.Equal(got, metadata) {
		t.Fatal("metadata escapes changed")
	}
	data, err := io.ReadAll(io.NewSectionReader(received, 0, 19))
	if err != nil || string(data) != "synthetic TPM state" {
		t.Fatal("read-only descriptor cannot expose sealed contents", err)
	}
}

func TestSealedTransportNilConnections(t *testing.T) {
	f := sealedFixture(t)
	if err := SendSealedSnapshot(context.Background(), nil, []byte("{}"), f); err == nil {
		t.Fatal("nil send connection accepted")
	}
	metadata, received, err := ReceiveSealedSnapshot(context.Background(), nil)
	if received != nil {
		_ = received.Close()
	}
	if err == nil || received != nil || metadata != nil {
		t.Fatal("nil receive connection accepted", err)
	}
}
