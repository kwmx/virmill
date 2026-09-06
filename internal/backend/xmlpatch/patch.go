// Package xmlpatch edits only byte spans belonging to explicitly selected fields.
// Unrelated namespaces, attributes, comments and device subtrees remain byte exact.
package xmlpatch

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

const Limit = 16 << 20

type span struct {
	start, end int
	value      string
}

func Patch(data string, changes map[string]string) (string, error) {
	if len(data) > Limit {
		return "", errors.New("XML size limit")
	}
	d := xml.NewDecoder(strings.NewReader(data))
	stack := []xml.Name{}
	found := map[string]int{}
	starts := map[int]int{}
	spans := []span{}
	roots := 0
	for {
		before := int(d.InputOffset())
		tok, e := d.Token()
		after := int(d.InputOffset())
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
		switch v := tok.(type) {
		case xml.Directive:
			return "", errors.New("XML directives forbidden")
		case xml.StartElement:
			if len(stack) == 0 {
				roots++
				if roots > 1 {
					return "", errors.New("multiple XML roots")
				}
			}
			stack = append(stack, v.Name)
			if len(stack) > 64 {
				return "", errors.New("XML depth limit")
			}
			names := []string{}
			for _, n := range stack {
				if n.Space != "" {
					names = append(names, "{namespace}"+n.Local)
				} else {
					names = append(names, n.Local)
				}
			}
			key := strings.Join(names, "/")
			if _, ok := changes[key]; ok {
				found[key]++
				starts[len(stack)] = after
			}
		case xml.EndElement:
			names := []string{}
			for _, n := range stack {
				if n.Space != "" {
					names = append(names, "{namespace}"+n.Local)
				} else {
					names = append(names, n.Local)
				}
			}
			key := strings.Join(names, "/")
			if value, ok := changes[key]; ok {
				start := starts[len(stack)]
				if start > before {
					return "", errors.New("cannot edit empty-element shorthand")
				}
				original := data[start:before]
				if strings.Contains(original, "<") {
					return "", errors.New("refusing lossy edit of structured field")
				}
				var b bytes.Buffer
				xml.EscapeText(&b, []byte(value))
				spans = append(spans, span{start, before, b.String()})
			}
			if len(stack) == 0 {
				return "", errors.New("unbalanced XML")
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 && strings.TrimSpace(string(v)) != "" {
				return "", errors.New("text outside XML root")
			}
		}
	}
	if roots != 1 || len(stack) != 0 {
		return "", errors.New("missing or unclosed XML root")
	}
	for key := range changes {
		if found[key] != 1 {
			return "", fmt.Errorf("field %s must occur exactly once (got %d)", key, found[key])
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start > spans[j].start })
	for _, s := range spans {
		data = data[:s.start] + s.value + data[s.end:]
	}
	return data, nil
}
func Validate(data string) error { _, e := Patch(data, map[string]string{}); return e }
