package importer

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// Describe reads an uncompressed OVA's headers and OVF without hashing disk
// payloads. Its metadata-only report is never import authorization or integrity
// evidence. Full Inspect remains required by the preparation planner.
func Describe(ctx context.Context, filename string, limits Limits) (Report, error) {
	out := descriptionReport()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return out, err
	}
	before, err := os.Lstat(absolute)
	if err != nil {
		return out, err
	}
	if !before.Mode().IsRegular() {
		return out, errors.New("source must be a regular, non-symlink OVA file")
	}
	file, err := os.OpenFile(absolute, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return out, err
	}
	defer file.Close()
	opened, err := file.Stat()
	currentOpen, openPathErr := os.Lstat(absolute)
	if err != nil || openPathErr != nil || !sameDescriptionSource(before, opened) || !sameDescriptionSource(opened, currentOpen) {
		return out, errors.New("source changed while opening")
	}
	out, err = describeArchive(ctx, file, opened.Size(), limits)
	if err != nil {
		return out, err
	}
	end, statErr := file.Stat()
	current, pathErr := os.Lstat(absolute)
	if statErr != nil || pathErr != nil || !sameDescriptionSource(opened, end) || !sameDescriptionSource(opened, current) {
		return out, errors.New("source changed during metadata description; choose the source again")
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	out.Source = absolute
	return out, nil
}

func sameDescriptionSource(before, after os.FileInfo) bool {
	return before != nil && after != nil && after.Mode().IsRegular() && os.SameFile(before, after) &&
		before.Size() == after.Size() && before.Mode() == after.Mode() && before.ModTime().Equal(after.ModTime())
}

func descriptionReport() Report {
	return Report{Members: []Member{}, Disks: []Disk{}, Systems: []System{},
		Warnings:  []string{"Metadata only: disk contents and archive checksums have not been verified. Preparation performs full verification."},
		Integrity: "not-verified", Readiness: "metadata-only"}
}

// Keep io.Seeker visible to archive/tar. The pinned Go reader's discard routine
// seeks across ordinary payloads, then reads their last byte to detect truncation.
// A LimitedReader or TeeReader here would silently force full payload reads.
// The separate read budget also bounds PAX/GNU metadata consumed inside Next.
type descriptionReader struct {
	ctx        context.Context
	source     io.ReadSeeker
	size, pos  int64
	readBudget int64
}

func (r *descriptionReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	if r.pos == r.size {
		return 0, io.EOF
	}
	if r.readBudget <= 0 {
		return 0, errors.New("archive metadata read budget exceeded")
	}
	maximum := min(int64(len(buffer)), r.size-r.pos, r.readBudget)
	if maximum < 0 {
		return 0, errors.New("archive read position is invalid")
	}
	n, err := r.source.Read(buffer[:int(maximum)])
	r.pos += int64(n)
	r.readBudget -= int64(n)
	return n, err
}

func (r *descriptionReader) Seek(offset int64, whence int) (int64, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	base := int64(0)
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = r.pos
	case io.SeekEnd:
		base = r.size
	default:
		return 0, errors.New("invalid archive seek mode")
	}
	// Compare before addition, so even malicious int64 header sizes cannot wrap.
	if offset < -base || offset > r.size-base {
		return 0, errors.New("truncated archive member or invalid seek bounds")
	}
	position := base + offset
	actual, err := r.source.Seek(position, io.SeekStart)
	if err != nil {
		return 0, err
	}
	if actual != position {
		return 0, errors.New("archive seek did not reach the requested position")
	}
	r.pos = position
	return position, nil
}

func describeArchive(ctx context.Context, source io.ReadSeeker, size int64, limits Limits) (Report, error) {
	out := descriptionReport()
	if limits.Bytes <= 0 || limits.Bytes > 1<<50 || limits.Members <= 0 || limits.Members > MaxMembers {
		return out, errors.New("invalid description limits")
	}
	metadataBudget := int64(limits.Members)*2048 + 2*DescriptorLimit
	if size < 0 || size > limits.Bytes+metadataBudget {
		return out, errors.New("archive source byte budget exceeded")
	}
	reader := &descriptionReader{ctx: ctx, source: source, size: size, readBudget: metadataBudget}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return out, err
	}
	archive := tar.NewReader(reader)
	seen, ordinary, parents := map[string]bool{}, map[string]bool{}, map[string]bool{}
	members := map[string]bool{}
	var descriptorName string
	var descriptor []byte
	total := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
		if err := SafePath(header.Name); err != nil {
			return out, err
		}
		name := strings.TrimSuffix(header.Name, "/")
		folded := strings.ToLower(name)
		if seen[folded] {
			return out, errors.New("duplicate/case-colliding archive entry")
		}
		seen[folded] = true
		if len(seen) > limits.Members {
			return out, errors.New("archive member limit exceeded")
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA && header.Typeflag != tar.TypeDir {
			return out, errors.New("links, sparse and special archive entries forbidden")
		}
		for key := range header.PAXRecords {
			if strings.Contains(strings.ToLower(key), "sparse") {
				return out, errors.New("sparse archive entries forbidden")
			}
		}
		for parent := path.Dir(folded); parent != "."; parent = path.Dir(parent) {
			if ordinary[parent] {
				return out, errors.New("archive parent is a regular file")
			}
			parents[parent] = true
		}
		if header.Typeflag == tar.TypeDir {
			if header.Size != 0 {
				return out, errors.New("archive directory has a nonzero payload")
			}
			continue
		}
		if parents[folded] {
			return out, errors.New("archive file conflicts with child paths")
		}
		ordinary[folded] = true
		if header.Size < 0 || header.Size > limits.Bytes-total {
			return out, errors.New("archive unpack budget exceeded")
		}
		total += header.Size
		if header.Size > reader.size-reader.pos {
			return out, errors.New("truncated archive member")
		}
		extension := strings.ToLower(path.Ext(name))
		if (extension == ".ovf" || extension == ".mf") && header.Size > DescriptorLimit {
			return out, errors.New("descriptor/manifest size limit exceeded")
		}
		if extension == ".ovf" {
			if descriptorName != "" {
				return out, errors.New("select an archive containing exactly one OVF descriptor")
			}
			descriptorName = name
			descriptor, err = io.ReadAll(archive)
			if err != nil {
				return out, err
			}
			if int64(len(descriptor)) != header.Size {
				return out, errors.New("truncated OVF descriptor")
			}
		}
		out.Members = append(out.Members, Member{Path: name, Size: header.Size})
		members[name] = true
	}
	// Reject hidden appended content; ordinary tar record padding is small.
	// Excessive padding is bounded by the metadata budget rather than read as a
	// multi-gigabyte payload. No bytes are hashed or claimed verified here.
	padding := make([]byte, 4096)
	for {
		n, err := reader.Read(padding)
		for _, value := range padding[:n] {
			if value != 0 {
				return out, errors.New("nonzero payload after tar end")
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
	}
	if descriptorName == "" {
		return out, errors.New("select an archive containing exactly one OVF descriptor")
	}
	out.Descriptor = descriptorName
	if err := parseOVF(descriptor, descriptorName, &out); err != nil {
		return out, err
	}
	for _, disk := range out.Disks {
		if !members[disk.Path] {
			return out, fmt.Errorf("missing disk member %s", disk.Path)
		}
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	return out, nil
}
