package importer

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// Offsets let a disk be read in place from its archive (ADR 0060). A long name
// puts an extended header before its entry's data.
func TestInspectTarRecordsWhereEachMemberStarts(t *testing.T) {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, m := range []struct {
		name string
		data []byte
	}{
		{"appliance.ovf", []byte(descriptor)},
		{"boot.vmdk", bytes.Repeat([]byte("b"), 1000)},
		{strings.Repeat("n", 120) + ".nvram", []byte("nvram")},
		{"data.vmdk", bytes.Repeat([]byte("d"), 3000)},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: m.name, Mode: 0600, Size: int64(len(m.data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(m.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	report, err := InspectTar(context.Background(), bytes.NewReader(archive.Bytes()), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	b := archive.Bytes()
	if len(report.Members) != 4 {
		t.Fatalf("members %+v", report.Members)
	}
	for _, m := range report.Members {
		if m.Offset < 512 || m.Offset%512 != 0 || m.Offset+m.Size > int64(len(b)) {
			t.Fatalf("%s: offset %d", m.Path, m.Offset)
		}
		sum := sha256.Sum256(b[m.Offset : m.Offset+m.Size])
		if hex.EncodeToString(sum[:]) != m.SHA256 {
			t.Fatalf("%s: the bytes at offset %d are not the member", m.Path, m.Offset)
		}
	}
}
