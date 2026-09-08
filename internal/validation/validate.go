package validation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/unicode/norm"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
	"virmill.local/core/internal/wire"
	"virmill.local/core/schemas"
)

var once sync.Once
var compiled map[string]*jsonschema.Schema
var loadErr error

func initSchemas() {
	compiled = map[string]*jsonschema.Schema{}
	c := jsonschema.NewCompiler()
	// Match the normative validator: bundled date-time formats are constraints.
	c.AssertFormat()
	c.UseLoader(offlineLoader{})
	c.UseRegexpEngine(func(pattern string) (jsonschema.Regexp, error) {
		r, err := regexp2.Compile(pattern, regexp2.ECMAScript)
		if err != nil {
			return nil, err
		}
		r.MatchTimeout = 100 * time.Millisecond
		return schemaRegexp{r}, nil
	})
	files, e := schemas.Files.ReadDir(".")
	if e != nil {
		loadErr = e
		return
	}
	for _, f := range files {
		b, e := schemas.Files.ReadFile(f.Name())
		if e != nil {
			loadErr = e
			return
		}
		var v any
		if e = json.Unmarshal(b, &v); e != nil {
			loadErr = e
			return
		}
		if e = c.AddResource("https://virmill.example/schemas/v1/"+f.Name(), v); e != nil {
			loadErr = e
			return
		}
	}
	for _, f := range files {
		s, e := c.Compile("https://virmill.example/schemas/v1/" + f.Name())
		if e != nil {
			loadErr = e
			return
		}
		compiled[strings.TrimSuffix(f.Name(), ".schema.json")] = s
	}
}

type schemaRegexp struct{ *regexp2.Regexp }

type offlineLoader struct{}

func (offlineLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("schema is not bundled; network resolution forbidden: %s", url)
}

func (r schemaRegexp) MatchString(s string) bool {
	ok, err := r.Regexp.MatchString(s)
	return err == nil && ok
}
func Schema(name string, b []byte) error {
	once.Do(initSchemas)
	if loadErr != nil {
		return loadErr
	}
	if e := wire.Validate(b); e != nil {
		return e
	}
	s, ok := compiled[name]
	if !ok {
		return errors.New("unknown bundled schema")
	}
	var v any
	if e := json.Unmarshal(b, &v); e != nil {
		return e
	}
	return s.Validate(v)
}
func Document(b []byte) (map[string]any, []byte, error) {
	if len(b) > wire.MaxFrame || !utf8.Valid(b) {
		return nil, nil, errors.New("document exceeds bounds or is not UTF-8")
	}
	var value map[string]any
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		if e := wire.Decode(b, &value); e != nil {
			return nil, nil, e
		}
	} else {
		d := yaml.NewDecoder(bytes.NewReader(b))
		var n yaml.Node
		if e := d.Decode(&n); e != nil {
			return nil, nil, e
		}
		var walk func(*yaml.Node, int) error
		walk = func(n *yaml.Node, depth int) error {
			if depth > 64 {
				return errors.New("YAML nesting limit")
			}
			if n.Kind == yaml.AliasNode || n.Anchor != "" {
				return errors.New("YAML aliases/anchors not allowed")
			}
			switch n.Tag {
			case "", "!!map", "!!seq", "!!str", "!!int", "!!bool", "!!null", "!!float":
			default:
				return fmt.Errorf("YAML tag %q forbidden", n.Tag)
			}
			if n.Kind == yaml.MappingNode {
				keys := map[string]bool{}
				for i := 0; i < len(n.Content); i += 2 {
					k := n.Content[i]
					if k.Tag != "!!str" || keys[k.Value] {
						return errors.New("non-string or duplicate YAML key")
					}
					keys[k.Value] = true
				}
			}
			for _, child := range n.Content {
				if e := walk(child, depth+1); e != nil {
					return e
				}
			}
			return nil
		}
		if e := walk(&n, 0); e != nil {
			return nil, nil, e
		}
		if e := n.Decode(&value); e != nil {
			return nil, nil, e
		}
		var extra yaml.Node
		if e := d.Decode(&extra); e != io.EOF {
			return nil, nil, errors.New("only one YAML document allowed")
		}
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return nil, nil, e
	}
	kind, _ := value["kind"].(string)
	name := map[string]string{"VM": "vm", "Network": "network", "Lab": "lab", "BackupPolicy": "backup-policy", "GuestRecipe": "guest-recipe"}[kind]
	if name == "" {
		return nil, nil, errors.New("unknown declarative kind")
	}
	if e = Schema(name, raw); e != nil {
		return nil, nil, e
	}
	if md, ok := value["metadata"].(map[string]any); ok {
		if dn, ok := md["displayName"].(string); ok {
			if _, e = DisplayName(dn); e != nil {
				return nil, nil, e
			}
			md["displayName"] = norm.NFC.String(dn)
		}
	}
	raw, e = json.Marshal(value)
	return value, raw, e
}
func DisplayName(s string) (string, error) {
	if !utf8.ValidString(s) || len(s) == 0 || len(s) > 256 {
		return "", errors.New("invalid display name length or UTF-8")
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) || r == '/' || r == '\\' {
			return "", errors.New("display name contains control or path separator")
		}
	}
	return norm.NFC.String(s), nil
}

// SafeText removes controls, bidi formatting and escape commands from untrusted display data.
func SafeText(s string) string {
	var b strings.Builder
	escape := 0
	for _, r := range s {
		if escape == 1 {
			if r == '[' {
				escape = 2
				continue
			}
			if r == ']' {
				escape = 3
				continue
			}
			escape = 0
			continue
		}
		if escape == 2 {
			if r >= 0x40 && r <= 0x7e {
				escape = 0
			}
			continue
		}
		if escape == 3 {
			if r == 7 {
				escape = 0
			}
			if r == 27 {
				escape = 4
			}
			continue
		}
		if escape == 4 {
			if r == '\\' {
				escape = 0
			} else {
				escape = 3
			}
			continue
		}
		if r == 27 {
			escape = 1
			continue
		}
		if r == '\n' || r == '\t' {
			b.WriteRune(r)
		} else if !unicode.IsControl(r) && !unicode.In(r, unicode.Cf) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
