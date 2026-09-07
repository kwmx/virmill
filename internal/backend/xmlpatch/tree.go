package xmlpatch

import (
	"encoding/json"
	"encoding/xml"
	"io"
	"sort"
	"strings"
	"virmill.local/core/internal/domain"
)

// Positioned XML is only used to select exact edit spans. Unknown content is
// retained in the source and in comparison tokens; it is never reconstructed.
type xmlPart struct {
	child *positionedNode
	kind  string
	text  string
}
type positionedNode struct {
	name                           xml.Name
	attrs                          []xml.Attr
	start, startEnd, endStart, end int
	empty                          bool
	parts                          []xmlPart
	exterior                       []xmlPart
}

func positionedXML(data string) (*positionedNode, error) {
	if len(data) > Limit {
		return nil, domain.Fail("INVALID_INPUT", "domain XML exceeds byte bound")
	}
	d := xml.NewDecoder(strings.NewReader(data))
	stack := []*positionedNode{}
	var root *positionedNode
	var prolog []xmlPart
	nodes := 0
	for {
		before := int(d.InputOffset())
		token, err := d.Token()
		after := int(d.InputOffset())
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, domain.Fail("INVALID_INPUT", "invalid domain XML")
		}
		switch v := token.(type) {
		case xml.Directive:
			return nil, domain.Fail("INVALID_INPUT", "XML directives are forbidden")
		case xml.StartElement:
			nodes++
			if nodes > 65536 || len(stack) >= 64 {
				return nil, domain.Fail("INVALID_INPUT", "domain XML exceeds node or depth bound")
			}
			n := &positionedNode{name: v.Name, attrs: append([]xml.Attr(nil), v.Attr...), start: before, startEnd: after}
			seen := map[xml.Name]bool{}
			for _, a := range n.attrs {
				if seen[a.Name] {
					return nil, domain.Fail("INVALID_INPUT", "duplicate XML attribute")
				}
				seen[a.Name] = true
			}
			if len(stack) == 0 {
				if root != nil {
					return nil, domain.Fail("INVALID_INPUT", "multiple XML roots")
				}
				root = n
				root.exterior = prolog
			} else {
				p := stack[len(stack)-1]
				p.parts = append(p.parts, xmlPart{child: n})
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, domain.Fail("INVALID_INPUT", "unbalanced XML")
			}
			n := stack[len(stack)-1]
			n.endStart = before
			n.end = after
			n.empty = after == before
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(v)) != "" {
					return nil, domain.Fail("INVALID_INPUT", "text outside XML root")
				}
			} else {
				p := stack[len(stack)-1]
				text := string(v)
				if len(p.parts) > 0 && p.parts[len(p.parts)-1].kind == "text" {
					p.parts[len(p.parts)-1].text += text
				} else {
					p.parts = append(p.parts, xmlPart{kind: "text", text: text})
				}
			}
		case xml.Comment:
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.parts = append(p.parts, xmlPart{kind: "comment", text: string(v)})
			} else if root == nil {
				prolog = append(prolog, xmlPart{kind: "prolog-comment", text: string(v)})
			} else {
				root.exterior = append(root.exterior, xmlPart{kind: "epilog-comment", text: string(v)})
			}
		case xml.ProcInst:
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.parts = append(p.parts, xmlPart{kind: "processing", text: v.Target + " " + string(v.Inst)})
			} else if root == nil {
				prolog = append(prolog, xmlPart{kind: "prolog-processing", text: v.Target + " " + string(v.Inst)})
			} else {
				root.exterior = append(root.exterior, xmlPart{kind: "epilog-processing", text: v.Target + " " + string(v.Inst)})
			}
		}
	}
	if root == nil || len(stack) != 0 || root.name != (xml.Name{Local: "domain"}) {
		return nil, domain.Fail("INVALID_INPUT", "one unnamespaced domain root required")
	}
	return root, nil
}
func (n *positionedNode) children(name string) []*positionedNode {
	out := []*positionedNode{}
	for _, p := range n.parts {
		if p.child != nil && p.child.name == (xml.Name{Local: name}) {
			out = append(out, p.child)
		}
	}
	return out
}
func (n *positionedNode) attr(name string) (string, bool) {
	for _, a := range n.attrs {
		if a.Name == (xml.Name{Local: name}) {
			return a.Value, true
		}
	}
	return "", false
}
func (n *positionedNode) text() string {
	var b strings.Builder
	for _, p := range n.parts {
		if p.kind == "text" {
			b.WriteString(p.text)
		}
	}
	return b.String()
}
func (n *positionedNode) onlyAttrs(names ...string) bool {
	for _, a := range n.attrs {
		allowed := false
		for _, name := range names {
			if a.Name == (xml.Name{Local: name}) {
				allowed = true
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}
func onlyChild(n *positionedNode, name string) (*positionedNode, error) {
	list := n.children(name)
	if len(list) != 1 {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "exactly one "+name+" element required for this edit")
	}
	return list[0], nil
}

// HardwareDigest admits only these representation differences: attribute order,
// empty-tag spelling, indentation at the five known containers below, and the
// position of a boot child among disk/interface children. Opaque text, namespaces,
// attributes, comments and all other child ordering remain significant.
func HardwareDigest(data string) (string, error) {
	root, err := positionedXML(data)
	if err != nil {
		return "", err
	}
	var encode func(*positionedNode, string, bool) any
	encode = func(n *positionedNode, parent string, preserve bool) any {
		name := n.name.Local
		if n.name.Space != "" {
			name = "{" + n.name.Space + "}" + name
		}
		path := parent + "/" + name
		for _, attr := range n.attrs {
			if attr.Name == (xml.Name{Space: "http://www.w3.org/XML/1998/namespace", Local: "space"}) {
				preserve = attr.Value != "default"
			}
		}
		attrs := append([]xml.Attr(nil), n.attrs...)
		sort.Slice(attrs, func(i, j int) bool {
			if attrs[i].Name.Space != attrs[j].Name.Space {
				return attrs[i].Name.Space < attrs[j].Name.Space
			}
			return attrs[i].Name.Local < attrs[j].Name.Local
		})
		knownContainer := !preserve && (path == "/domain" || path == "/domain/os" || path == "/domain/devices" || path == "/domain/devices/disk" || path == "/domain/devices/interface")
		bootContainer := knownContainer && (path == "/domain/devices/disk" || path == "/domain/devices/interface")
		parts := []any{}
		boots := []any{}
		for _, p := range n.parts {
			if p.child != nil {
				value := encode(p.child, path, preserve)
				if bootContainer && p.child.name == (xml.Name{Local: "boot"}) {
					boots = append(boots, value)
				} else {
					parts = append(parts, value)
				}
			} else if !(knownContainer && p.kind == "text" && strings.TrimSpace(p.text) == "") {
				parts = append(parts, []string{p.kind, p.text})
			}
		}
		parts = append(parts, boots...)
		return []any{n.name, attrs, parts}
	}
	exterior := make([][]string, 0, len(root.exterior))
	for _, part := range root.exterior {
		exterior = append(exterior, []string{part.kind, part.text})
	}
	b, err := json.Marshal([]any{encode(root, "", false), exterior})
	if err != nil {
		return "", err
	}
	return Digest(string(b)), nil
}

type spanReplacement struct {
	start, end int
	value      string
}

func replaceSpans(data string, spans []spanReplacement) (string, error) {
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end > spans[j].end
		}
		return spans[i].start > spans[j].start
	})
	last := len(data)
	for _, s := range spans {
		if s.start < 0 || s.end < s.start || s.end > last {
			return "", domain.Fail("INVALID_INPUT", "overlapping XML edit spans")
		}
		data = data[:s.start] + s.value + data[s.end:]
		last = s.start
	}
	if _, err := positionedXML(data); err != nil {
		return "", err
	}
	return data, nil
}

// Attribute changes preserve the exact surrounding start tag and quote style.
// The token tree has already checked XML structure and attribute uniqueness.
func attributeSpan(data string, n *positionedNode, name, value string) (spanReplacement, error) {
	raw := data[n.start:n.startEnd]
	i := 1
	space := func(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }
	for i < len(raw) && !space(raw[i]) && raw[i] != '>' && raw[i] != '/' {
		i++
	}
	for i < len(raw) {
		for i < len(raw) && space(raw[i]) {
			i++
		}
		if i >= len(raw) || raw[i] == '>' || raw[i] == '/' {
			break
		}
		start := i
		for i < len(raw) && !space(raw[i]) && raw[i] != '=' {
			i++
		}
		key := raw[start:i]
		for i < len(raw) && space(raw[i]) {
			i++
		}
		if i >= len(raw) || raw[i] != '=' {
			break
		}
		i++
		for i < len(raw) && space(raw[i]) {
			i++
		}
		if i >= len(raw) || (raw[i] != '\'' && raw[i] != '"') {
			break
		}
		quote := raw[i]
		i++
		start = i
		for i < len(raw) && raw[i] != quote {
			i++
		}
		if i >= len(raw) {
			break
		}
		if key == name {
			return spanReplacement{n.start + start, n.start + i, value}, nil
		}
		i++
	}
	return spanReplacement{}, domain.Fail("UNSUPPORTED_CAPABILITY", "cannot locate a unique existing XML attribute span")
}
