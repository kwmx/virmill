package importer

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

type cancelAfterRead struct {
	reader    io.Reader
	remaining int
	cancel    context.CancelFunc
	reads     int
}

func (r *cancelAfterRead) Read(p []byte) (int, error) {
	r.reads++
	n, err := r.reader.Read(p)
	r.remaining -= n
	if r.remaining <= 0 {
		r.cancel()
	}
	return n, err
}

func TestInspectionPreservesCancellationDuringMember(t *testing.T) {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: "large.vmdk", Size: 128 << 10, Mode: 0600}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(make([]byte, 128<<10)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &cancelAfterRead{reader: bytes.NewReader(archive.Bytes()), remaining: 1024, cancel: cancel}
	_, err := InspectTar(ctx, reader, DefaultLimits())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was masked as archive corruption: %v", err)
	}
}

func TestInspectionChecksCancellationBeforeTrailingPaddingRead(t *testing.T) {
	// Two zero blocks are a tar terminator. Cancel as tar consumes the second;
	// no following padding read is allowed after cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &cancelAfterRead{reader: bytes.NewReader(make([]byte, 8192)), remaining: 1024, cancel: cancel}
	_, err := InspectTar(ctx, reader, DefaultLimits())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("padding cancellation was lost: %v", err)
	}
	if reader.reads != 2 {
		t.Fatalf("read trailing padding after cancellation: %d reads", reader.reads)
	}
}
