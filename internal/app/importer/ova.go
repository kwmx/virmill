// Package importer inspects appliance packaging without extracting or executing it.
package importer

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const DescriptorLimit = 16 << 20
const MaxMembers = 10000
const OVF = "http://schemas.dmtf.org/ovf/envelope/1"

type Limits struct {
	Bytes   int64
	Members int
}

func DefaultLimits() Limits { return Limits{Bytes: 64 << 30, Members: MaxMembers} }

type Member struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	digests map[string]string
}
type Disk struct {
	ID                      string `json:"id"`
	FileRef                 string `json:"fileRef"`
	Path                    string `json:"path"`
	Capacity                string `json:"capacity"`
	Format                  string `json:"format"`
	CapacityAllocationUnits string `json:"capacityAllocationUnits,omitempty"`
	CapacityBytes           int64  `json:"capacityBytes,omitempty"`
}
type Item struct {
	ResourceType    string   `json:"resourceType"`
	InstanceID      string   `json:"instanceID"`
	Parent          string   `json:"parent"`
	AddressOnParent string   `json:"addressOnParent"`
	Quantity        string   `json:"quantity"`
	AllocationUnits string   `json:"allocationUnits,omitempty"`
	MemoryMiB       int64    `json:"memoryMiB,omitempty"`
	HostResources   []string `json:"hostResources"`
	Connections     []string `json:"connections"`
}
type System struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Items   []Item   `json:"hardware"`
	DiskIDs []string `json:"diskIDs"`
}
type Report struct {
	Source     string   `json:"source"`
	SHA256     string   `json:"sha256"`
	Members    []Member `json:"members"`
	Disks      []Disk   `json:"disks"`
	Systems    []System `json:"systems"`
	Warnings   []string `json:"warnings"`
	Integrity  string   `json:"integrity"`
	Readiness  string   `json:"readiness"`
	Descriptor string   `json:"descriptor"`
}

func SafePath(s string) error {
	if !utf8.ValidString(s) || s == "" || len(s) > 1024 || strings.ContainsAny(s, "\\:\x00") || strings.HasPrefix(s, "/") {
		return errors.New("unsafe archive path")
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return errors.New("control in path")
		}
	}
	clean := path.Clean(s)
	if clean != strings.TrimSuffix(s, "/") || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Count(clean, "/") >= 32 {
		return errors.New("noncanonical or deep archive path")
	}
	return nil
}
func Inspect(ctx context.Context, filename string, limits Limits) (Report, error) {
	var out Report
	info, e := os.Lstat(filename)
	if e != nil {
		return out, e
	}
	if !info.Mode().IsRegular() {
		return out, errors.New("source must be a regular, non-symlink file")
	}
	f, e := os.Open(filename)
	if e != nil {
		return out, e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return out, errors.New("source changed while opening")
	}
	out, e = InspectTar(ctx, f, limits)
	if e != nil {
		return out, e
	}
	end, e := f.Stat()
	if e != nil || end.Size() != opened.Size() || !end.ModTime().Equal(opened.ModTime()) {
		return out, errors.New("source changed during inspection")
	}
	out.Source = filename
	return out, nil
}
func InspectTar(ctx context.Context, r io.Reader, limits Limits) (Report, error) {
	out := Report{Members: []Member{}, Disks: []Disk{}, Systems: []System{}, Warnings: []string{}, Integrity: "unsigned", Readiness: "inspection-only"}
	if limits.Bytes <= 0 || limits.Bytes > 1<<50 || limits.Members <= 0 || limits.Members > MaxMembers {
		return out, errors.New("invalid inspection limits")
	}
	bounded := &io.LimitedReader{R: r, N: limits.Bytes + int64(limits.Members)*2048 + 2*DescriptorLimit}
	r = bounded
	h := sha256.New()
	tr := tar.NewReader(io.TeeReader(r, h))
	seen := map[string]bool{}
	regularPaths := map[string]bool{}
	parents := map[string]bool{}
	total := int64(0)
	descriptors := map[string][]byte{}
	manifests := map[string][]byte{}
	for {
		if e := ctx.Err(); e != nil {
			return out, e
		}
		entry, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return out, e
		}
		if e = SafePath(entry.Name); e != nil {
			return out, e
		}
		name := strings.TrimSuffix(entry.Name, "/")
		fold := strings.ToLower(name)
		if seen[fold] {
			return out, errors.New("duplicate/case-colliding archive entry")
		}
		seen[fold] = true
		for parent := path.Dir(fold); parent != "."; parent = path.Dir(parent) {
			if regularPaths[parent] {
				return out, errors.New("archive parent is a regular file")
			}
			parents[parent] = true
		}
		if entry.Typeflag != tar.TypeDir {
			if parents[fold] {
				return out, errors.New("archive file conflicts with child paths")
			}
			regularPaths[fold] = true
		}
		if len(seen) > limits.Members {
			return out, errors.New("archive member limit exceeded")
		}
		if entry.Typeflag != tar.TypeReg && entry.Typeflag != tar.TypeRegA && entry.Typeflag != tar.TypeDir {
			return out, errors.New("links, sparse and special entries forbidden")
		}
		for k := range entry.PAXRecords {
			if strings.Contains(strings.ToLower(k), "sparse") {
				return out, errors.New("sparse archive entries forbidden")
			}
		}
		if entry.Typeflag == tar.TypeDir {
			continue
		}
		if entry.Size < 0 || entry.Size > limits.Bytes-total {
			return out, errors.New("archive unpack budget exceeded")
		}
		total += entry.Size
		var content bytes.Buffer
		hashers := map[string]hash.Hash{"SHA1": sha1.New(), "SHA256": sha256.New(), "SHA512": sha512.New()}
		writers := []io.Writer{}
		for _, v := range hashers {
			writers = append(writers, v)
		}
		ext := strings.ToLower(path.Ext(name))
		if ext == ".ovf" || ext == ".mf" {
			if entry.Size > DescriptorLimit {
				return out, errors.New("descriptor/manifest size limit exceeded")
			}
			writers = append(writers, &content)
		}
		n, e := io.Copy(io.MultiWriter(writers...), &contextReader{ctx, tr})
		if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			return out, e
		}
		if e != nil || n != entry.Size {
			return out, errors.New("truncated archive member")
		}
		m := Member{Path: name, Size: n, digests: map[string]string{}}
		for alg, hh := range hashers {
			m.digests[alg] = hex.EncodeToString(hh.Sum(nil))
		}
		m.SHA256 = m.digests["SHA256"]
		out.Members = append(out.Members, m)
		if ext == ".ovf" {
			descriptors[name] = content.Bytes()
		}
		if ext == ".mf" {
			manifests[name] = content.Bytes()
		}
	}
	// Consume trailing tar padding into the digest; reject hidden nonzero payload.
	tail := make([]byte, 4096)
	padding := &contextReader{ctx, r}
	for {
		n, e := padding.Read(tail)
		if n > 0 {
			h.Write(tail[:n])
			for _, b := range tail[:n] {
				if b != 0 {
					return out, errors.New("nonzero payload after tar end")
				}
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return out, e
		}
		if e = ctx.Err(); e != nil {
			return out, e
		}
	}
	if bounded.N == 0 {
		return out, errors.New("archive source byte budget exceeded")
	}
	out.SHA256 = hex.EncodeToString(h.Sum(nil))
	if len(descriptors) != 1 {
		return out, errors.New("select an archive containing exactly one OVF descriptor")
	}
	for name, data := range descriptors {
		out.Descriptor = name
		if e := parseOVF(data, name, &out); e != nil {
			return out, e
		}
	}
	byPath := map[string]Member{}
	for _, m := range out.Members {
		byPath[m.Path] = m
	}
	for _, d := range out.Disks {
		if _, ok := byPath[d.Path]; !ok {
			return out, fmt.Errorf("missing disk member %s", d.Path)
		}
	}
	// VirtualBox exports use "SHA1 (file)"; other exporters omit the space.
	// Accept horizontal spacing without relaxing member or digest validation.
	line := regexp.MustCompile(`^(SHA1|SHA256|SHA512)[ \t]*\(([^)]+)\)\s*=\s*([A-Fa-f0-9]+)$`)
	for mf, data := range manifests {
		for _, s := range strings.Split(string(data), "\n") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			match := line.FindStringSubmatch(s)
			if match == nil {
				return out, errors.New("unsupported or malformed appliance checksum")
			}
			if e := SafePath(match[2]); e != nil {
				return out, e
			}
			p := path.Join(path.Dir(mf), match[2])
			m, ok := byPath[p]
			if !ok || m.digests[match[1]] != strings.ToLower(match[3]) {
				return out, fmt.Errorf("checksum mismatch or absent member %s", p)
			}
			if match[1] == "SHA1" {
				out.Warnings = append(out.Warnings, "WEAK_CHECKSUM_SHA1")
			}
		}
		out.Integrity = "provided-checksums-valid; publisher-unverified"
	}
	if len(out.Systems) > 1 {
		out.Warnings = append(out.Warnings, "MULTI_SYSTEM_SELECTION_REQUIRED")
	}
	out.Warnings = append(out.Warnings, "DISK_FORMAT_BACKING_CHAIN_AND_GUEST_COMPATIBILITY_NOT_INSPECTED", "NETWORK_MAPPING_REQUIRED", "GUEST_BOOT_UNVERIFIED")
	sort.Slice(out.Members, func(i, j int) bool { return out.Members[i].Path < out.Members[j].Path })
	return out, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}

type node struct {
	name     xml.Name
	attrs    []xml.Attr
	text     string
	children []*node
}

func (n *node) attr(name string) string {
	for _, a := range n.attrs {
		if a.Name.Local == name && a.Name.Space == OVF {
			return a.Value
		}
	}
	return ""
}
func (n *node) childrenNamed(name string) []*node {
	a := []*node{}
	for _, c := range n.children {
		if c.name.Local == name {
			a = append(a, c)
		}
	}
	return a
}
func (n *node) childText(name string) string {
	for _, c := range n.children {
		if c.name.Local == name {
			return strings.TrimSpace(c.text)
		}
	}
	return ""
}
func parseOVF(data []byte, filename string, out *Report) error {
	if !utf8.Valid(data) {
		return errors.New("invalid descriptor UTF-8")
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	stack := []*node{}
	var root *node
	count := 0
	for {
		token, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		switch n := token.(type) {
		case xml.Directive:
			return errors.New("XML entity/doctype directives forbidden")
		case xml.StartElement:
			count++
			if count > 100000 || len(stack) > 64 {
				return errors.New("XML complexity limit")
			}
			v := &node{name: n.Name, attrs: n.Attr}
			if len(stack) == 0 {
				if root != nil {
					return errors.New("multiple XML roots")
				}
				root = v
			} else {
				p := stack[len(stack)-1]
				p.children = append(p.children, v)
			}
			stack = append(stack, v)
		case xml.EndElement:
			if len(stack) == 0 {
				return errors.New("unbalanced descriptor")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(n)
			}
		}
	}
	if root == nil || root.name.Local != "Envelope" || root.name.Space != OVF {
		return errors.New("expected namespaced OVF Envelope")
	}
	files := map[string]string{}
	disks := map[string]bool{}
	systemIDs := map[string]bool{}
	for _, refs := range root.childrenNamed("References") {
		for _, f := range refs.childrenNamed("File") {
			id, href := f.attr("id"), f.attr("href")
			if id == "" || files[id] != "" {
				return errors.New("duplicate/missing file ID")
			}
			if e := SafePath(href); e != nil {
				return e
			}
			files[id] = path.Join(path.Dir(filename), href)
		}
	}
	for _, section := range root.childrenNamed("DiskSection") {
		for _, d := range section.childrenNamed("Disk") {
			id, ref := d.attr("diskId"), d.attr("fileRef")
			if id == "" || disks[id] || files[ref] == "" {
				return errors.New("duplicate disk or unresolved file reference")
			}
			disks[id] = true
			disk := Disk{ID: id, FileRef: ref, Path: files[ref], Capacity: d.attr("capacity"), Format: d.attr("format"), CapacityAllocationUnits: d.attr("capacityAllocationUnits")}
			units := disk.CapacityAllocationUnits
			if units == "" {
				// DSP0243 2.1.0 §9.1 defines bytes when this attribute is absent.
				// Preserve an explicitly empty/invalid unit as unknown instead.
				present := false
				for _, attr := range d.attrs {
					present = present || attr.Name.Local == "capacityAllocationUnits"
				}
				if !present {
					units = "byte"
				}
			}
			if capacity, known := allocationBytes(disk.Capacity, units); known {
				disk.CapacityBytes = capacity
			}
			out.Disks = append(out.Disks, disk)
		}
	}
	var visit func(*node) error
	visit = func(n *node) error {
		if n.name.Local == "VirtualSystem" && n.name.Space == OVF {
			id := n.attr("id")
			if id == "" || systemIDs[id] {
				return errors.New("missing/duplicate virtual system ID")
			}
			systemIDs[id] = true
			s := System{ID: id, Name: n.childText("Name"), Items: []Item{}, DiskIDs: []string{}}
			for _, hw := range n.childrenNamed("VirtualHardwareSection") {
				virtualBox := false
				for _, system := range hw.childrenNamed("System") {
					virtualBox = virtualBox || strings.HasPrefix(system.childText("VirtualSystemType"), "virtualbox-")
				}
				for _, item := range hw.childrenNamed("Item") {
					v := Item{ResourceType: item.childText("ResourceType"), InstanceID: item.childText("InstanceID"), Parent: item.childText("Parent"), AddressOnParent: item.childText("AddressOnParent"), Quantity: item.childText("VirtualQuantity"), AllocationUnits: item.childText("AllocationUnits"), HostResources: []string{}, Connections: []string{}}
					if v.ResourceType == "4" {
						units := v.AllocationUnits
						// VirtualBox's exporter emits MegaBytes from stored bytes / _1M;
						// its reader treats that spelling exactly like byte * 2^20.
						// Only apply the legacy convention to declared VirtualBox guests.
						// https://github.com/VirtualBox/virtualbox/blob/main/src/VBox/Main/src-server/ApplianceImplExport.cpp
						if virtualBox && units == "MegaBytes" {
							units = "byte * 2^20"
						}
						if size, known := allocationBytes(v.Quantity, units); known && size%(1<<20) == 0 {
							v.MemoryMiB = size / (1 << 20)
						}
					}
					for _, r := range item.childrenNamed("HostResource") {
						v.HostResources = append(v.HostResources, strings.TrimSpace(r.text))
						if v.ResourceType == "17" {
							prefix := "ovf:/disk/"
							if !strings.HasPrefix(strings.TrimSpace(r.text), prefix) {
								return errors.New("unsupported external disk resource")
							}
							disk := strings.TrimPrefix(strings.TrimSpace(r.text), prefix)
							if !disks[disk] {
								return errors.New("unresolved system disk reference")
							}
							s.DiskIDs = append(s.DiskIDs, disk)
						}
					}
					for _, c := range item.childrenNamed("Connection") {
						v.Connections = append(v.Connections, strings.TrimSpace(c.text))
					}
					if v.AddressOnParent != "" {
						if _, e := strconv.ParseUint(v.AddressOnParent, 10, 32); e != nil {
							return errors.New("invalid controller position")
						}
					}
					s.Items = append(s.Items, v)
				}
			}
			out.Systems = append(out.Systems, s)
		}
		if n.name.Local == "Property" {
			out.Warnings = append(out.Warnings, "OVF_PROPERTY_REQUIRES_EXPLICIT_GUEST_SETUP")
		}
		for _, c := range n.children {
			if e := visit(c); e != nil {
				return e
			}
		}
		return nil
	}
	if e := visit(root); e != nil {
		return e
	}
	if len(out.Systems) == 0 {
		return errors.New("no virtual systems")
	}
	return nil
}

var byteAllocationUnits = regexp.MustCompile(`(?i)^bytes?(?:\s*\*\s*(2|10)\s*\^\s*([0-9]+))?$`)

// allocationBytes supplies a conservative display/default hint, never a device
// mapping. Unknown units, fractional quantities and overflow preserve the raw
// descriptor fields without inventing a normalized value.
func allocationBytes(quantity, units string) (int64, bool) {
	quantity, units = strings.TrimSpace(quantity), strings.TrimSpace(units)
	if quantity == "" || len(quantity) > 19 || len(units) > 64 {
		return 0, false
	}
	for _, digit := range quantity {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	amount, err := strconv.ParseInt(quantity, 10, 64)
	if err != nil || amount <= 0 {
		return 0, false
	}
	match := byteAllocationUnits.FindStringSubmatch(units)
	if match == nil {
		return 0, false
	}
	const maximum = int64(1<<63 - 1)
	factor := int64(1)
	if match[1] != "" {
		base, _ := strconv.ParseInt(match[1], 10, 64)
		exponent, err := strconv.ParseUint(match[2], 10, 8)
		if err != nil || exponent > 62 {
			return 0, false
		}
		for range exponent {
			if factor > maximum/base {
				return 0, false
			}
			factor *= base
		}
	}
	if amount > maximum/factor {
		return 0, false
	}
	return amount * factor, true
}
