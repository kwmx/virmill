package tui

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const (
	detailDepth     = 24
	detailNodes     = 32768
	detailRows      = 65536
	detailBytes     = 4 << 20
	nativeXMLNotice = "Native XML available in advanced details"
	detailOmission  = "Additional details omitted: presentation safety limit reached; use advanced details."
)

// HumanDetails renders structured observations without inferring success or
// permission. Map keys are sorted; array order is retained. It does not invoke
// arbitrary Stringer/Marshaler methods. Widths below two columns use two columns
// so a wide Unicode grapheme can remain intact instead of being truncated.
func HumanDetails(value any, width int) []string {
	f := newDetails(width)
	f.value("", reflect.ValueOf(value), 0)
	return f.lines
}

// PlanDetails keeps approval identities first, then presents all plan fields.
// False, zero, empty and unknown values remain visible; review is not a claim
// that the plan has been applied or its completion predicates verified.
func PlanDetails(p domain.Plan, width int) []string {
	f := newDetails(width)
	fields := []struct {
		label string
		value any
	}{
		{"Operation", p.Operation}, {"Plan ID", p.ID}, {"Plan digest", p.Digest},
		{"Affected resources", p.ResourceIDs}, {"Acknowledgements", p.Acknowledgements},
		{"Risks", p.Risks}, {"Steps", p.Steps}, {"Estimates", p.Estimates},
		{"Input digest", p.InputDigest}, {"Connection ID", p.ConnectionID},
		{"Actor UID", p.ActorUID}, {"Created at", p.CreatedAt}, {"Expires at", p.ExpiresAt},
		{"API version", p.APIVersion}, {"Before fingerprints", p.Before},
		{"Required grants", p.RequiredGrants}, {"Review", p.Review},
	}
	for _, field := range fields {
		f.value(field.label, reflect.ValueOf(field.value), 0)
	}
	return f.lines
}

type details struct {
	width, nodes, bytes int
	lines               []string
	stopped             bool
}

func newDetails(width int) *details { return &details{width: max(2, width)} }

func (f *details) omit() {
	if f.stopped {
		return
	}
	f.stopped = true
	f.lines = append(f.lines, strings.Split(ansi.Hardwrap(detailOmission, f.width, true), "\n")...)
}

func (f *details) line(text string, depth int) {
	if f.stopped {
		return
	}
	if len(text) > detailBytes-f.bytes {
		f.omit()
		return
	}
	f.bytes += len(text)
	// SafeText retains tabs and newlines. Expand tabs before measuring cells;
	// no untrusted escape, bidi or other formatting controls reach the terminal.
	text = strings.ReplaceAll(validation.SafeText(text), "\t", "    ")
	indent := strings.Repeat(" ", min(depth*2, f.width-2))
	wrapped := strings.Split(ansi.Hardwrap(text, f.width-len(indent), true), "\n")
	if len(wrapped) > detailRows-len(f.lines) {
		f.omit()
		return
	}
	for _, line := range wrapped {
		f.lines = append(f.lines, indent+line)
	}
}

func (f *details) scalar(label, text string, depth int) {
	if label != "" {
		text = label + ": " + text
	}
	f.line(text, depth)
}

func (f *details) value(label string, v reflect.Value, depth int) {
	if f.stopped {
		return
	}
	f.nodes++
	if depth > detailDepth || f.nodes > detailNodes {
		f.omit()
		return
	}
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			f.scalar(label, "Unknown (null)", depth)
			return
		}
		v = v.Elem()
		// Pointer cycles must not escape the recursive traversal bound.
		f.nodes++
		if f.nodes > detailNodes {
			f.omit()
			return
		}
	}
	if !v.IsValid() {
		f.scalar(label, "Unknown (null)", depth)
		return
	}
	if v.CanInterface() {
		switch x := v.Interface().(type) {
		case time.Time:
			if x.IsZero() {
				f.scalar(label, "Unknown (zero timestamp)", depth)
			} else {
				f.scalar(label, x.Format(time.RFC3339Nano), depth)
			}
			return
		case json.RawMessage:
			if len(x) > detailBytes-f.bytes {
				f.omit()
				return
			}
			if wire.Validate(x) != nil {
				f.scalar(label, "Unrenderable JSON; inspect advanced details", depth)
				return
			}
			d := json.NewDecoder(bytes.NewReader(x))
			d.UseNumber()
			var decoded any
			if d.Decode(&decoded) != nil {
				f.scalar(label, "Unrenderable JSON; inspect advanced details", depth)
				return
			}
			f.value(label, reflect.ValueOf(decoded), depth+1)
			return
		}
	}
	if nativeXMLField(label) && v.Kind() == reflect.String && v.Len() > 0 {
		f.scalar(label, nativeXMLNotice, depth)
		return
	}
	switch v.Kind() {
	case reflect.String:
		text := v.String()
		if text == "" {
			text = "Empty"
		}
		f.scalar(label, text, depth)
	case reflect.Bool:
		f.scalar(label, strconv.FormatBool(v.Bool()), depth)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f.scalar(label, strconv.FormatInt(v.Int(), 10), depth)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		f.scalar(label, strconv.FormatUint(v.Uint(), 10), depth)
	case reflect.Float32, reflect.Float64:
		f.scalar(label, strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits()), depth)
	case reflect.Map:
		if v.IsNil() {
			f.scalar(label, "Unknown (null)", depth)
			return
		}
		if v.Len() == 0 {
			f.scalar(label, "None (empty collection)", depth)
			return
		}
		if v.Type().Key().Kind() != reflect.String {
			f.scalar(label, "Unsupported map keys; inspect advanced details", depth)
			return
		}
		if v.Len() > detailNodes-f.nodes {
			f.omit()
			return
		}
		if label != "" {
			f.line(label+":", depth)
			depth++
		}
		keys := v.MapKeys()
		sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
		for _, key := range keys {
			if len(key.String()) > detailBytes-f.bytes {
				f.omit()
				return
			}
			f.value(detailLabel(key.String()), v.MapIndex(key), depth)
		}
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			f.scalar(label, "Unknown (null)", depth)
			return
		}
		if v.Len() == 0 {
			f.scalar(label, "None (empty collection)", depth)
			return
		}
		if label != "" {
			f.line(label+":", depth)
			depth++
		}
		for i := 0; i < v.Len() && !f.stopped; i++ {
			item := v.Index(i)
			if detailContainer(item) {
				f.value("Item "+strconv.Itoa(i+1), item, depth)
			} else {
				// Scalar lists retain ordinary bullets without JSON punctuation.
				start := len(f.lines)
				f.value("", item, depth+1)
				if start < len(f.lines) && !f.stopped {
					prefix := strings.Repeat(" ", min((depth+1)*2, f.width-2))
					if len(prefix) >= 2 {
						f.lines[start] = prefix[:len(prefix)-2] + "- " + strings.TrimPrefix(f.lines[start], prefix)
					}
				}
			}
		}
	case reflect.Struct:
		if label != "" {
			f.line(label+":", depth)
			depth++
		}
		t := v.Type()
		for i := 0; i < v.NumField() && !f.stopped; i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			f.value(detailLabel(name), v.Field(i), depth)
		}
	default:
		f.scalar(label, "Unsupported value; inspect advanced details", depth)
	}
}

func detailContainer(v reflect.Value) bool {
	for v.IsValid() && v.Kind() == reflect.Interface {
		if v.IsNil() {
			return false
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return false
	}
	return v.Kind() == reflect.Struct || v.Kind() == reflect.Map || v.Kind() == reflect.Slice || v.Kind() == reflect.Array || v.Kind() == reflect.Pointer
}

func nativeXMLField(label string) bool {
	switch strings.ToLower(strings.ReplaceAll(label, " ", "")) {
	case "xml", "nativexml", "persistentxml", "livexml", "sourcexml", "inactivexml":
		return true
	}
	return false
}

func detailLabel(key string) string {
	// Identifier/path keys remain exact. Only conventional field identifiers are
	// humanized; e.g. a UUID-to-fingerprint map becomes separate readable entries.
	for _, r := range key {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != ' ' {
			return key
		}
	}
	if key == "" {
		return "Empty field name"
	}
	if len(key) == 32 || len(key) == 64 {
		hex := true
		for _, r := range key {
			if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
				hex = false
				break
			}
		}
		if hex {
			return key
		}
	}
	key = strings.NewReplacer("IPv4", "Ipv4", "IPv6", "Ipv6", "XMLSHA256", "XmlSha256", "UUIDs", "Uuids", "IDs", "Ids", "NICs", "Nics", "CPUs", "Cpus").Replace(key)
	runes := []rune(key)
	var words []string
	start := 0
	for i, r := range runes {
		boundary := i > start && unicode.IsUpper(r) && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1]) || i+1 < len(runes) && unicode.IsLower(runes[i+1]) && unicode.IsUpper(runes[i-1]))
		if r == '_' || r == ' ' || boundary {
			if i > start {
				words = append(words, string(runes[start:i]))
			}
			start = i
			if r == '_' || r == ' ' {
				start++
			}
		}
	}
	if start < len(runes) {
		words = append(words, string(runes[start:]))
	}
	if len(words) == 0 {
		return key
	}
	for i, word := range words {
		switch strings.ToLower(word) {
		case "ids":
			words[i] = "IDs"
		case "uuids":
			words[i] = "UUIDs"
		case "nics":
			words[i] = "NICs"
		case "cpus":
			words[i] = "CPUs"
		case "ipv4":
			words[i] = "IPv4"
		case "ipv6":
			words[i] = "IPv6"
		case "id", "uuid", "api", "cpu", "ram", "tpm", "nvram", "usb", "pci", "iommu", "cidr", "dhcp", "dns", "uid", "gid", "xml", "sha256":
			words[i] = strings.ToUpper(word)
		default:
			words[i] = strings.ToLower(word)
		}
	}
	first := []rune(words[0])
	first[0] = unicode.ToUpper(first[0])
	words[0] = string(first)
	return strings.Join(words, " ")
}
