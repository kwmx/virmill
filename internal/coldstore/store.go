// Package coldstore publishes immutable, private recovery sets from held
// positional readers. It validates storage integrity, not capture provenance.
package coldstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"path"
	"reflect"
	"strings"

	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/domain"
)

type Source struct {
	Member protection.CaptureMember
	Reader io.ReaderAt
}

type Receipt struct {
	Version        int                        `json:"version"`
	SnapshotID     string                     `json:"snapshotID"`
	OperationID    string                     `json:"operationID"`
	ManifestSHA256 string                     `json:"manifestSHA256"`
	Manifest       protection.CaptureManifest `json:"manifest"`
}

const documentLimit = 8 << 20
const copyChunk = 256 << 10

func invalid(message string) error    { return domain.Fail("INVALID_INPUT", message) }
func incomplete(message string) error { return domain.Fail("INCOMPLETE_BACKUP", message) }

func validID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' || strings.ToLower(s) != s || s == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return err == nil
}

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// Freeze caller-owned maps/slices through the same strict wire validator used
// for recovered manifests. Reader lifetime and source guards remain the caller's.
func prepare(ctx context.Context, manifest protection.CaptureManifest, sources []Source) (Receipt, []byte, []byte, []Source, error) {
	var empty Receipt
	if err := ctx.Err(); err != nil {
		return empty, nil, nil, nil, err
	}
	if err := manifest.Validate(); err != nil {
		return empty, nil, nil, nil, err
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return empty, nil, nil, nil, err
	}
	m, err := protection.DecodeCaptureManifest(raw)
	if err != nil {
		return empty, nil, nil, nil, err
	}
	if _, err = expectedTree(m); err != nil {
		return empty, nil, nil, nil, err
	}
	if len(sources) != len(m.Members) {
		return empty, nil, nil, nil, invalid("exactly one positional source per capture member required")
	}
	byID := make(map[string]Source, len(sources))
	for _, source := range sources {
		if source.Reader == nil || byID[source.Member.ID].Reader != nil {
			return empty, nil, nil, nil, invalid("nil or duplicate capture source")
		}
		value := reflect.ValueOf(source.Reader)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				return empty, nil, nil, nil, invalid("nil capture reader")
			}
		}
		byID[source.Member.ID] = source
	}
	ordered := make([]Source, len(m.Members))
	for i, member := range m.Members {
		source, ok := byID[member.ID]
		if !ok || source.Member != member {
			return empty, nil, nil, nil, invalid("capture source does not exactly match its declared member")
		}
		ordered[i] = source
	}
	receipt := Receipt{Version: 1, SnapshotID: m.ID, OperationID: m.OperationID, ManifestSHA256: digest(raw), Manifest: m}
	receiptRaw, err := json.Marshal(receipt)
	if err != nil || len(receiptRaw) > documentLimit {
		return empty, nil, nil, nil, invalid("capture receipt exceeds document bound")
	}
	if err := ctx.Err(); err != nil {
		return empty, nil, nil, nil, err
	}
	return receipt, raw, receiptRaw, ordered, nil
}

// Directory entries are derived exclusively from validated manifest paths.
func expectedTree(m protection.CaptureManifest) (map[string]map[string]bool, error) {
	tree := map[string]map[string]bool{"": {"manifest.json": false, "receipt.json": false}}
	for _, member := range m.Members {
		first := strings.ToLower(strings.SplitN(member.Path, "/", 2)[0])
		if first == "manifest.json" || first == "receipt.json" {
			return nil, invalid("capture member collides with publication metadata")
		}
		parts := strings.Split(member.Path, "/")
		parent := ""
		for i, part := range parts {
			directory := i < len(parts)-1
			if prior, exists := tree[parent][part]; exists && prior != directory {
				return nil, invalid("capture path has conflicting types")
			}
			tree[parent][part] = directory
			if directory {
				parent = path.Join(parent, part)
				if tree[parent] == nil {
					tree[parent] = map[string]bool{}
				}
			}
		}
	}
	return tree, nil
}

// Exactly size bytes plus one EOF probe are read, even for an empty TPM member.
// No goroutine pretends to cancel a ReaderAt blocked inside a filesystem syscall.
func copyExact(ctx context.Context, dst io.Writer, src io.ReaderAt, size int64, expected string) error {
	buf := make([]byte, copyChunk)
	hash := sha256.New()
	for off := int64(0); off < size; {
		if err := ctx.Err(); err != nil {
			return err
		}
		want := int(min(int64(len(buf)), size-off))
		n, err := src.ReadAt(buf[:want], off)
		if cause := ctx.Err(); cause != nil {
			return cause
		}
		if n < 0 || n > want {
			return incomplete("invalid positional reader byte count")
		}
		if err != nil && err != io.EOF {
			return err
		}
		if n != want {
			return incomplete("capture source shorter than declared size")
		}
		written, writeErr := dst.Write(buf[:n])
		if writeErr != nil {
			return writeErr
		}
		if written != n {
			return io.ErrShortWrite
		}
		_, _ = hash.Write(buf[:n])
		off += int64(n)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := src.ReadAt(buf[:1], size)
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	if n != 0 || err != io.EOF {
		return incomplete("capture source has extra bytes or no exact EOF")
	}
	if hex.EncodeToString(hash.Sum(nil)) != expected {
		return incomplete("capture member SHA256 differs")
	}
	return ctx.Err()
}
