package importer

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDescribeMetadataHasNoIntegrityOrBootClaim(t *testing.T) {
	archive := fixture(t, descriptor, nil)
	filename := filepath.Join(t.TempDir(), "appliance.ova")
	if err := os.WriteFile(filename, archive, 0600); err != nil {
		t.Fatal(err)
	}
	report, err := Describe(context.Background(), filename, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if report.Source != filename || report.SHA256 != "" || report.Integrity != "not-verified" || report.Readiness != "metadata-only" || len(report.Warnings) == 0 {
		t.Fatalf("metadata advertised trust or lost source identity: %+v", report)
	}
	if len(report.Disks) != 2 || len(report.Systems) != 2 || len(report.Systems[0].DiskIDs) != 2 || len(report.Members) != 3 {
		t.Fatalf("metadata lost disks or systems: %+v", report)
	}
	for _, member := range report.Members {
		if member.SHA256 != "" || len(member.digests) != 0 {
			t.Fatal("metadata fabricated a member checksum")
		}
	}
	verified, err := Inspect(context.Background(), filename, DefaultLimits())
	if err != nil || verified.SHA256 == "" || verified.Readiness != "inspection-only" {
		t.Fatalf("full inspection contract changed: %+v %v", verified, err)
	}
}

func describeHeader(t *testing.T, name string, size int64) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: size, Typeflag: tar.TypeReg, Format: tar.FormatGNU}); err != nil {
		t.Fatal(err)
	}
	// No payload/Close: these complete header bytes are followed by an explicitly
	// sized sparse extent in the caller's disposable ordinary fixture file.
	return append([]byte{}, buffer.Bytes()...)
}

type countedDescriptionSource struct {
	io.ReadSeeker
	readBytes int64
	seeks     int
}

func (r *countedDescriptionSource) Read(b []byte) (int, error) {
	if r.readBytes+int64(len(b)) > 128<<10 {
		return 0, errors.New("fixture detected full payload reading instead of seek")
	}
	n, err := r.ReadSeeker.Read(b)
	r.readBytes += int64(n)
	return n, err
}
func (r *countedDescriptionSource) Seek(offset int64, whence int) (int64, error) {
	r.seeks++
	return r.ReadSeeker.Seek(offset, whence)
}

func TestDescribeSeeksAcrossLargeOrdinaryMembers(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "large-*.ova")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, member := range []struct {
		name    string
		size    int64
		content []byte
	}{
		{"boot.vmdk", 8 << 30, nil},
		{"appliance.ovf", int64(len(descriptor)), []byte(descriptor)},
		{"data.vmdk", 2 << 30, nil},
	} {
		if _, err := file.Write(describeHeader(t, member.name, member.size)); err != nil {
			t.Fatal(err)
		}
		if member.content == nil {
			if _, err := file.Seek(member.size, io.SeekCurrent); err != nil {
				t.Fatal(err)
			}
		} else if _, err := file.Write(member.content); err != nil {
			t.Fatal(err)
		}
		if padding := (512 - member.size%512) % 512; padding != 0 {
			if _, err := file.Write(make([]byte, padding)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := file.Write(make([]byte, 1024)); err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	counted := &countedDescriptionSource{ReadSeeker: file}
	report, err := describeArchive(context.Background(), counted, info.Size(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Disks) != 2 || counted.readBytes > 32<<10 || counted.seeks < 5 || info.Size() < 10<<30 {
		t.Fatalf("large payload was not skipped: read=%d seeks=%d size=%d", counted.readBytes, counted.seeks, info.Size())
	}
	t.Logf("10 GiB logical sparse ordinary tar fixture: read %d bytes; %d seeks. Metadata software test, not a disk-format or guest-boot test.", counted.readBytes, counted.seeks)
}

func TestDescribeRejectsUnsafeAndTruncatedArchiveMetadata(t *testing.T) {
	for _, header := range []*tar.Header{
		{Name: "../escape", Typeflag: tar.TypeReg}, {Name: "/absolute", Typeflag: tar.TypeReg},
		{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		{Name: "hard", Typeflag: tar.TypeLink, Linkname: "boot.vmdk"},
		{Name: "device", Typeflag: tar.TypeChar}, {Name: "BOOT.VMDK", Typeflag: tar.TypeReg},
		{Name: "boot.vmdk/child", Typeflag: tar.TypeReg}, {Name: "second.ovf", Typeflag: tar.TypeReg},
	} {
		data := fixture(t, descriptor, header)
		if _, err := describeArchive(context.Background(), bytes.NewReader(data), int64(len(data)), DefaultLimits()); err == nil {
			t.Fatalf("unsafe metadata accepted: %+v", header)
		}
	}
	for _, ovf := range []string{
		"<!DOCTYPE x [<!ENTITY e SYSTEM 'file:///etc/passwd'>]>" + descriptor,
		strings.Replace(descriptor, "boot.vmdk", "../../host", 1),
		strings.Replace(descriptor, "boot.vmdk", "missing.vmdk", 1),
	} {
		data := fixture(t, ovf, nil)
		if _, err := describeArchive(context.Background(), bytes.NewReader(data), int64(len(data)), DefaultLimits()); err == nil {
			t.Fatal("unsafe or unresolved OVF accepted")
		}
	}
	for name, data := range map[string][]byte{
		"truncated payload":    append(describeHeader(t, "large.vmdk", 1<<20), []byte("short")...),
		"overflow payload":     describeHeader(t, "huge.vmdk", math.MaxInt64),
		"oversized descriptor": describeHeader(t, "large.ovf", DescriptorLimit+1),
		"compressed input":     {0x1f, 0x8b, 8, 0},
		"nonzero trailer":      append(fixture(t, descriptor, nil), 'x'),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := describeArchive(context.Background(), bytes.NewReader(data), int64(len(data)), DefaultLimits()); err == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
	data := fixture(t, descriptor, nil)
	for _, limits := range []Limits{{Bytes: 0, Members: 10}, {Bytes: math.MaxInt64, Members: 10}, {Bytes: 1 << 20, Members: 0}, {Bytes: 1 << 20, Members: MaxMembers + 1}, {Bytes: 10, Members: 10}, {Bytes: 1 << 20, Members: 2}} {
		if _, err := describeArchive(context.Background(), bytes.NewReader(data), int64(len(data)), limits); err == nil {
			t.Fatal("invalid/exceeded bounds accepted")
		}
	}
}

type describeCheckContext struct {
	context.Context
	checks  int
	onCheck func(int)
}

func (c *describeCheckContext) Err() error {
	c.checks++
	c.onCheck(c.checks)
	return c.Context.Err()
}

func TestDescribeCancellationAndSourceChanges(t *testing.T) {
	for _, action := range []string{"cancel", "mtime", "replace"} {
		t.Run(action, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "source.ova")
			data := fixture(t, descriptor, nil)
			if err := os.WriteFile(filename, data, 0600); err != nil {
				t.Fatal(err)
			}
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			changed := false
			ctx := &describeCheckContext{Context: base, onCheck: func(count int) {
				if count != 7 {
					return
				}
				changed = true
				switch action {
				case "cancel":
					cancel()
				case "mtime":
					future := time.Now().Add(time.Hour)
					if err := os.Chtimes(filename, future, future); err != nil {
						t.Fatal(err)
					}
				case "replace":
					if err := os.Rename(filename, filename+".original"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filename, data, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}}
			_, err := Describe(ctx, filename, DefaultLimits())
			if !changed || err == nil {
				t.Fatalf("source/cancellation change not detected: checks=%d changed=%v err=%v", ctx.checks, changed, err)
			}
			if action == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if action != "cancel" && !strings.Contains(err.Error(), "source changed") {
				t.Fatalf("wrong source-change failure: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Describe(ctx, "/not-opened-after-cancel", DefaultLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation did not precede opening: %v", err)
	}
}

func TestDescribeRefusesSymlinksAndNonRegularSources(t *testing.T) {
	directory := t.TempDir()
	filename := filepath.Join(directory, "source.ova")
	if err := os.WriteFile(filename, fixture(t, descriptor, nil), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link.ova")
	if err := os.Symlink(filename, link); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{link, directory} {
		if _, err := Describe(context.Background(), source, DefaultLimits()); err == nil {
			t.Fatal("nonregular source accepted")
		}
	}
}

func TestDescriptionReaderBoundsAndReadBudget(t *testing.T) {
	reader := &descriptionReader{ctx: context.Background(), source: bytes.NewReader(make([]byte, 1024)), size: 1024, readBudget: 4}
	for _, seek := range []struct {
		offset int64
		whence int
	}{{math.MaxInt64, io.SeekCurrent}, {math.MinInt64, io.SeekCurrent}, {1, io.SeekEnd}, {-1, io.SeekStart}, {0, 99}} {
		if _, err := reader.Seek(seek.offset, seek.whence); err == nil {
			t.Fatalf("invalid seek accepted: %+v", seek)
		}
	}
	if n, err := reader.Read(make([]byte, 8)); err != nil || n != 4 {
		t.Fatalf("bounded read: %d %v", n, err)
	}
	if _, err := reader.Read(make([]byte, 1)); err == nil {
		t.Fatal("metadata read budget exceeded silently")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader.ctx = ctx
	if _, err := reader.Seek(0, io.SeekStart); !errors.Is(err, context.Canceled) {
		t.Fatal(fmt.Sprint("seek ignored cancellation: ", err))
	}
}

func TestDescribeRejectsHiddenPAXAndGNUSparseHeaders(t *testing.T) {
	fixType := func(header []byte, kind byte) []byte {
		header = append([]byte{}, header...)
		header[156] = kind
		for i := 148; i < 156; i++ {
			header[i] = ' '
		}
		sum := 0
		for _, value := range header {
			sum += int(value)
		}
		copy(header[148:156], fmt.Sprintf("%06o\x00 ", sum))
		return header
	}
	for _, record := range []string{"GNU.sparse.major=99\n", "path=../escape\n"} {
		n := len(record) + 2
		for {
			actual := len(fmt.Sprintf("%d %s", n, record))
			if actual == n {
				break
			}
			n = actual
		}
		payload := []byte(fmt.Sprintf("%d %s", n, record))
		pax := fixType(describeHeader(t, "PaxHeaders/next", int64(len(payload))), tar.TypeXHeader)
		data := append(pax, payload...)
		data = append(data, make([]byte, (512-len(payload)%512)%512)...)
		data = append(data, fixture(t, descriptor, nil)...)
		if _, err := describeArchive(context.Background(), bytes.NewReader(data), int64(len(data)), DefaultLimits()); err == nil {
			t.Fatalf("hidden unsafe PAX header accepted: %q", record)
		}
	}
	sparse := fixType(describeHeader(t, "sparse.vmdk", 0), tar.TypeGNUSparse)
	data := append(sparse, fixture(t, descriptor, nil)...)
	if _, err := describeArchive(context.Background(), bytes.NewReader(data), int64(len(data)), DefaultLimits()); err == nil {
		t.Fatal("GNU sparse entry accepted")
	}
}
