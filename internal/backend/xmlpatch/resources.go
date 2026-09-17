package xmlpatch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"virmill.local/core/internal/domain"
)

// Digest binds opaque XML without persisting expert settings or possible secrets.
func Digest(data string) string { h := sha256.Sum256([]byte(data)); return hex.EncodeToString(h[:]) }

// ResourceEdit changes fixed boot resources only. Advanced layouts need an
// adapter that can explicitly review and update every dependent field.
type ResourceEdit struct {
	VCPUs     *uint64
	MemoryMiB *uint64
}
type resourceField struct {
	attrs []xml.Attr
	text  strings.Builder
}

func resourceFields(data string) (map[string][]*resourceField, error) {
	if err := Validate(data); err != nil {
		return nil, domain.Fail("INVALID_INPUT", "invalid or ambiguous persistent domain XML")
	}
	fields := map[string][]*resourceField{}
	watched := map[string]bool{}
	for _, path := range []string{"domain/memory", "domain/currentMemory", "domain/vcpu", "domain/vcpus", "domain/cputune", "domain/cpu/topology", "domain/cpu/numa", "domain/numatune", "domain/maxMemory", "domain/memtune", "domain/devices/memory", "domain/memoryBacking"} {
		watched[path] = true
	}
	d := xml.NewDecoder(strings.NewReader(data))
	stack := []string{}
	nodes := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch v := tok.(type) {
		case xml.StartElement:
			nodes++
			if nodes > 65536 {
				return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "persistent resource configuration exceeds the bounded node budget")
			}
			name := v.Name.Local
			if v.Name.Space != "" {
				name = "{" + v.Name.Space + "}" + name
			}
			stack = append(stack, name)
			if len(stack) == 1 && name != "domain" {
				return nil, domain.Fail("INVALID_INPUT", "persistent domain XML required")
			}
			path := strings.Join(stack, "/")
			if watched[path] {
				fields[path] = append(fields[path], &resourceField{attrs: append([]xml.Attr(nil), v.Attr...)})
			}
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				list := fields[strings.Join(stack, "/")]
				if len(list) > 0 {
					list[len(list)-1].text.Write([]byte(v))
				}
			}
		}
	}
	return fields, nil
}
func singleResource(fields map[string][]*resourceField, path string) (*resourceField, error) {
	if len(fields[path]) != 1 {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", path+" must occur exactly once for a fixed-resource edit")
	}
	return fields[path][0], nil
}
func resourceAttrs(f *resourceField, allowed ...string) (map[string]string, error) {
	attrs := map[string]string{}
	for _, a := range f.attrs {
		if a.Name.Space != "" {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "namespaced resource attributes require a dedicated edit adapter")
		}
		found := false
		for _, name := range allowed {
			if a.Name.Local == name {
				found = true
			}
		}
		if _, duplicate := attrs[a.Name.Local]; !found || duplicate {
			return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "dependent or unknown resource attributes require a dedicated edit adapter")
		}
		attrs[a.Name.Local] = a.Value
	}
	return attrs, nil
}
func positiveResource(value string) (uint64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, domain.Fail("INVALID_INPUT", "positive existing resource value required")
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, domain.Fail("INVALID_INPUT", "integer existing resource value required")
		}
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || n == 0 {
		return 0, domain.Fail("INVALID_INPUT", "bounded positive existing resource value required")
	}
	return n, nil
}
func memoryResource(f *resourceField) (uint64, uint64, error) {
	a, err := resourceAttrs(f, "unit", "dumpCore")
	if err != nil {
		return 0, 0, err
	}
	if core, ok := a["dumpCore"]; ok && core != "on" && core != "off" {
		return 0, 0, domain.Fail("UNSUPPORTED_CAPABILITY", "unknown memory dumpCore policy")
	}
	units := map[string]uint64{"": 1024, "b": 1, "bytes": 1, "KB": 1000, "k": 1024, "KiB": 1024, "MB": 1000000, "M": 1 << 20, "MiB": 1 << 20, "GB": 1000000000, "G": 1 << 30, "GiB": 1 << 30, "TB": 1000000000000, "T": 1 << 40, "TiB": 1 << 40}
	unit, ok := units[a["unit"]]
	if !ok {
		return 0, 0, domain.Fail("UNSUPPORTED_CAPABILITY", "unknown existing memory unit")
	}
	n, err := positiveResource(f.text.String())
	if err != nil {
		return 0, 0, err
	}
	if n > ^uint64(0)/unit {
		return 0, 0, domain.Fail("INVALID_INPUT", "existing memory value overflows bytes")
	}
	return n * unit, unit, nil
}

// editVCPUCount rewrites the <vcpu> element for a new boot count, keeping the
// spare CPU slots a definition already carries (ADR 0068). It writes only the
// two shapes libvirt itself writes — a plain maximum, and a maximum with a
// smaller current count — and reuses the element's own bytes for the attribute,
// so a stored definition still matches the reviewed one byte for byte.
func editVCPUCount(data string, requested uint64) (string, error) {
	fail := func() error {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "domain/vcpu has dependent resource policy; a dedicated reviewed adapter is required")
	}
	root, err := positionedXML(data)
	if err != nil {
		return "", err
	}
	node, err := observedResourceNode(root, "vcpu", true)
	if err != nil {
		return "", fail()
	}
	maximum, err := observedResourceScalar(node, "current", "placement")
	if err != nil {
		return "", fail()
	}
	if placement, ok := node.attr("placement"); ok && placement != "static" {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "automatic vCPU placement requires a dedicated edit adapter")
	}
	value, hasCurrent := node.attr("current")
	if hasCurrent {
		current, err := positiveResource(value)
		if err != nil || current > maximum {
			return "", fail()
		}
	}
	// Without spare slots, or when the request reaches the maximum, the element
	// carries one number, as it does today.
	if !hasCurrent || requested >= maximum {
		spans := []spanReplacement{{node.startEnd, node.endStart, strconv.FormatUint(requested, 10)}}
		if hasCurrent {
			removal, err := attributeRemoval(data, node, "current")
			if err != nil {
				return "", err
			}
			spans = append(spans, removal)
		}
		return replaceSpans(data, spans)
	}
	span, err := attributeSpan(data, node, "current", strconv.FormatUint(requested, 10))
	if err != nil {
		return "", err
	}
	return replaceSpans(data, []spanReplacement{span})
}

// attributeRemoval spans one attribute and the whitespace before it, so a start
// tag loses it exactly as libvirt would have written the tag without it.
func attributeRemoval(data string, n *positionedNode, name string) (spanReplacement, error) {
	span, err := attributeSpan(data, n, name, "")
	if err != nil {
		return span, err
	}
	refuse := domain.Fail("UNSUPPORTED_CAPABILITY", "the "+name+" attribute is written in a form this editor does not model")
	start := span.start - len(name) - 2
	if start <= n.start || data[start:span.start-1] != name+"=" {
		return spanReplacement{}, refuse
	}
	for start > n.start && (data[start-1] == ' ' || data[start-1] == '\t' || data[start-1] == '\n' || data[start-1] == '\r') {
		start--
	}
	if start == span.start-len(name)-2 {
		return spanReplacement{}, refuse
	}
	return spanReplacement{start, span.end + 1, ""}, nil
}

func EditResources(data string, edit ResourceEdit) (string, error) {
	if edit.VCPUs == nil && edit.MemoryMiB == nil {
		return "", domain.Fail("INVALID_INPUT", "no CPU/RAM edit requested")
	}
	fields, err := resourceFields(data)
	if err != nil {
		return "", err
	}
	changes := map[string]string{}
	refuse := func(paths ...string) error {
		for _, p := range paths {
			if len(fields[p]) > 0 {
				return domain.Fail("UNSUPPORTED_CAPABILITY", p+" has dependent resource policy; a dedicated reviewed adapter is required")
			}
		}
		return nil
	}
	if edit.VCPUs != nil {
		if *edit.VCPUs < 1 || *edit.VCPUs > 512 {
			return "", domain.Fail("INVALID_INPUT", "vcpus must be an integer from 1 through 512")
		}
		if err = refuse("domain/vcpus", "domain/cputune", "domain/cpu/topology", "domain/cpu/numa", "domain/numatune"); err != nil {
			return "", err
		}
		field, e := singleResource(fields, "domain/vcpu")
		if e != nil {
			return "", e
		}
		if _, e = positiveResource(field.text.String()); e != nil {
			return "", e
		}
	}
	if edit.MemoryMiB != nil {
		if *edit.MemoryMiB < 1 || *edit.MemoryMiB > 1048576 {
			return "", domain.Fail("INVALID_INPUT", "memoryMiB must be an integer from 1 through 1048576")
		}
		if err = refuse("domain/maxMemory", "domain/memtune", "domain/numatune", "domain/cpu/numa", "domain/devices/memory", "domain/memoryBacking"); err != nil {
			return "", err
		}
		memory, e := singleResource(fields, "domain/memory")
		if e != nil {
			return "", e
		}
		before, unit, e := memoryResource(memory)
		if e != nil {
			return "", e
		}
		desired := *edit.MemoryMiB << 20
		if desired%unit != 0 {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "requested MiB cannot be represented exactly in the existing memory unit")
		}
		changes["domain/memory"] = strconv.FormatUint(desired/unit, 10)
		if len(fields["domain/currentMemory"]) > 0 {
			current, e := singleResource(fields, "domain/currentMemory")
			if e != nil {
				return "", e
			}
			bytes, unit, e := memoryResource(current)
			if e != nil {
				return "", e
			}
			if bytes != before {
				return "", domain.Fail("UNSUPPORTED_CAPABILITY", "distinct maximum/current memory needs an explicit ballooning edit adapter")
			}
			if desired%unit != 0 {
				return "", domain.Fail("UNSUPPORTED_CAPABILITY", "requested MiB cannot be represented exactly in the existing current-memory unit")
			}
			changes["domain/currentMemory"] = strconv.FormatUint(desired/unit, 10)
		}
	}
	out := data
	if len(changes) > 0 {
		out, err = Patch(data, changes)
		if err != nil {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "resource edit would replace structured or ambiguous XML")
		}
	}
	// The CPU count is rewritten last, on the element's own bytes, so that spare
	// CPU slots survive the edit (ADR 0068).
	if edit.VCPUs != nil {
		if out, err = editVCPUCount(out, *edit.VCPUs); err != nil {
			return "", err
		}
	}
	return out, nil
}
