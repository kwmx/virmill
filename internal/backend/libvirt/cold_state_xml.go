//go:build linux && cgo

package libvirt

import (
	"encoding/hex"
	"encoding/xml"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"virmill.local/core/internal/domain"
)

const coldStateXMLLimit = 1 << 20

func coldInvalid(reason string) error {
	return domain.Fail("INVALID_INPUT", "invalid cold-state layout: "+reason)
}
func coldUnsupported(reason string) error {
	return domain.Fail("UNSUPPORTED_CAPABILITY", "cold-state layout unsupported: "+reason)
}

// InspectColdStateXML extracts only declared facts. Before capture, callers must
// separately verify persistent definition identity and stopped state. This
// function does not open paths, resolve secrets, copy state or authorize access.
func InspectColdStateXML(raw string) (domain.ColdStateLayout, error) {
	empty := domain.ColdStateLayout{}
	root, err := coldStateTree(raw)
	if err != nil {
		return empty, err
	}
	uuid, err := coldChild(root, "uuid", true)
	if err != nil {
		return empty, err
	}
	if err = coldAttrs(uuid, nil, nil); err != nil || len(uuid.children) != 0 {
		return empty, coldInvalid("domain UUID must be scalar")
	}
	id, err := coldUUID(uuid.text)
	if err != nil {
		return empty, err
	}
	out := domain.ColdStateLayout{VMID: id, SecretReferences: []string{}}
	osNode, err := coldChild(root, "os", true)
	if err != nil {
		return empty, err
	}
	if out.Firmware, err = coldFirmware(osNode); err != nil {
		return empty, err
	}
	devices, err := coldChild(root, "devices", true)
	if err != nil {
		return empty, err
	}
	if n, err := coldChild(devices, "nvram", false); err != nil {
		return empty, err
	} else if n != nil {
		return empty, coldUnsupported("pSeries NVRAM device requires a separate state adapter")
	}
	tpm, err := coldChild(devices, "tpm", false)
	if err != nil {
		return empty, err
	}
	if tpm != nil {
		value, err := coldTPM(tpm)
		if err != nil {
			return empty, err
		}
		out.TPM = &value
	}
	secrets := map[string]bool{}
	if out.TPM != nil && out.TPM.EncryptionSecret != "" {
		secrets[out.TPM.EncryptionSecret] = true
	}
	if err = coldSecrets(root, secrets); err != nil {
		return empty, err
	}
	for secret := range secrets {
		out.SecretReferences = append(out.SecretReferences, secret)
	}
	sort.Strings(out.SecretReferences)
	return out, nil
}

// Build a bounded tree using the backend's existing node representation. Text
// builders avoid quadratic concatenation when comments split scalar text.
func coldStateTree(raw string) (*xmlNode, error) {
	if raw == "" || len(raw) > coldStateXMLLimit {
		return nil, coldInvalid("missing XML or XML exceeds 1 MiB")
	}
	type frame struct {
		node *xmlNode
		text strings.Builder
	}
	d := xml.NewDecoder(strings.NewReader(raw))
	var stack []*frame
	var root *xmlNode
	nodes, tokens := 0, 0
	declaration := false
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, coldInvalid("malformed XML")
		}
		tokens++
		if tokens > 65536 {
			return nil, coldInvalid("XML token limit")
		}
		switch v := token.(type) {
		case xml.StartElement:
			nodes++
			if nodes > 16384 || len(stack) >= 32 {
				return nil, coldInvalid("XML structure limit")
			}
			seen := map[xml.Name]bool{}
			for _, a := range v.Attr {
				if seen[a.Name] {
					return nil, coldInvalid("duplicate XML attribute")
				}
				seen[a.Name] = true
			}
			n := &xmlNode{name: v.Name, attrs: v.Attr}
			if len(stack) == 0 {
				if root != nil || v.Name != (xml.Name{Local: "domain"}) {
					return nil, coldInvalid("one unnamespaced domain root required")
				}
				root = n
			} else {
				p := stack[len(stack)-1].node
				p.children = append(p.children, n)
			}
			stack = append(stack, &frame{node: n})
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, coldInvalid("unbalanced XML")
			}
			last := stack[len(stack)-1]
			last.node.text = last.text.String()
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(v)) != "" {
					return nil, coldInvalid("text outside domain XML")
				}
			} else {
				stack[len(stack)-1].text.Write(v)
			}
		case xml.Directive:
			return nil, coldInvalid("XML directives forbidden")
		case xml.ProcInst:
			if v.Target != "xml" || declaration || root != nil {
				return nil, coldInvalid("XML processing instruction forbidden")
			}
			declaration = true
		}
	}
	if root == nil || len(stack) != 0 {
		return nil, coldInvalid("incomplete domain XML")
	}
	return root, nil
}

func coldChild(parent *xmlNode, name string, required bool) (*xmlNode, error) {
	var found *xmlNode
	for _, n := range parent.children {
		if n.name.Local != name {
			continue
		}
		if n.name.Space != "" || found != nil {
			return nil, coldInvalid("foreign or duplicate " + name + " element")
		}
		found = n
	}
	if required && found == nil {
		return nil, coldInvalid("missing " + name + " element")
	}
	return found, nil
}

func coldAttrs(n *xmlNode, required, optional []string) error {
	allowed, seen := map[string]bool{}, map[string]bool{}
	for _, name := range required {
		allowed[name] = true
	}
	for _, name := range optional {
		allowed[name] = true
	}
	for _, a := range n.attrs {
		if a.Name.Space != "" || !allowed[a.Name.Local] || seen[a.Name.Local] || a.Value == "" {
			return coldInvalid("foreign, unknown, duplicate or empty " + n.name.Local + " attribute")
		}
		seen[a.Name.Local] = true
		if err := coldScalar(a.Value, 4096); err != nil {
			return err
		}
	}
	for _, name := range required {
		if !seen[name] {
			return coldInvalid("missing " + n.name.Local + " attribute")
		}
	}
	return nil
}

func coldScalar(value string, limit int) error {
	if !utf8.ValidString(value) || len(value) > limit || strings.TrimSpace(value) != value {
		return coldInvalid("noncanonical or oversized scalar")
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return coldInvalid("control character in scalar")
		}
	}
	return nil
}

func coldUUID(value string) (string, error) {
	if err := coldScalar(value, 36); err != nil {
		return "", err
	}
	if len(value) == 36 {
		if value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
			return "", coldInvalid("malformed UUID")
		}
		value = value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
	}
	if len(value) != 32 {
		return "", coldInvalid("malformed UUID")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", coldInvalid("malformed UUID")
	}
	value = strings.ToLower(value)
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:], nil
}

func coldPath(value string) error {
	if err := coldScalar(value, 4096); err != nil {
		return err
	}
	if value == "/" || !path.IsAbs(value) || path.Clean(value) != value {
		return coldInvalid("explicit canonical absolute state path required")
	}
	return nil
}

func coldEnum(value string, values ...string) bool {
	if value == "" {
		return true
	}
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func coldFirmware(osNode *xmlNode) (domain.ColdFirmware, error) {
	out := domain.ColdFirmware{}
	loader, err := coldChild(osNode, "loader", false)
	if err != nil {
		return out, err
	}
	nvram, err := coldChild(osNode, "nvram", false)
	if err != nil {
		return out, err
	}
	if varstore, err := coldChild(osNode, "varstore", false); err != nil {
		return out, err
	} else if varstore != nil {
		return out, coldUnsupported("varstore requires a separately supported state adapter")
	}
	if loader != nil {
		if err = coldAttrs(loader, nil, []string{"readonly", "secure", "type", "stateless", "format"}); err != nil {
			return out, err
		}
		if len(loader.children) != 0 {
			return out, coldInvalid("loader must contain only its path")
		}
		out.Loader, out.LoaderType, out.LoaderReadOnly, out.LoaderSecure = loader.text, attr(loader, "type"), attr(loader, "readonly"), attr(loader, "secure")
		out.LoaderFormat, out.LoaderStateless = attr(loader, "format"), attr(loader, "stateless")
		if !coldEnum(out.LoaderType, "rom", "pflash") || !coldEnum(out.LoaderFormat, "raw", "qcow2") || !coldEnum(out.LoaderReadOnly, "yes", "no") || !coldEnum(out.LoaderSecure, "yes", "no") || !coldEnum(out.LoaderStateless, "yes", "no") {
			return out, coldUnsupported("unrecognized loader metadata")
		}
		if out.Loader != "" {
			if err = coldPath(out.Loader); err != nil {
				return out, err
			}
		}
	}
	if nvram == nil {
		return out, nil
	}
	if out.LoaderStateless == "yes" {
		return out, coldInvalid("stateless loader cannot have NVRAM")
	}
	if err = coldAttrs(nvram, nil, []string{"type", "template", "templateFormat", "format"}); err != nil {
		return out, err
	}
	if kind := attr(nvram, "type"); kind != "" && kind != "file" {
		return out, coldUnsupported("NVRAM must use a local file layout")
	}
	n := &domain.ColdNVRAM{Format: attr(nvram, "format"), Template: attr(nvram, "template"), TemplateFormat: attr(nvram, "templateFormat")}
	if !coldEnum(n.Format, "raw", "qcow2") || !coldEnum(n.TemplateFormat, "raw", "qcow2") {
		return out, coldUnsupported("unrecognized NVRAM format")
	}
	if n.Template != "" {
		if err = coldPath(n.Template); err != nil {
			return out, err
		}
	}
	if len(nvram.children) == 0 {
		if attr(nvram, "type") != "" && nvram.text != "" {
			return out, coldInvalid("typed NVRAM must use a source element")
		}
		n.Path = nvram.text
	} else {
		if strings.TrimSpace(nvram.text) != "" || len(nvram.children) != 1 {
			return out, coldInvalid("ambiguous NVRAM source")
		}
		source, err := coldChild(nvram, "source", true)
		if err != nil {
			return out, err
		}
		if err = coldAttrs(source, []string{"file"}, nil); err != nil {
			return out, err
		}
		if len(source.children) != 0 || strings.TrimSpace(source.text) != "" {
			return out, coldUnsupported("structured NVRAM source requires a separate adapter")
		}
		n.Path = attr(source, "file")
	}
	if n.Path != "" {
		if err = coldPath(n.Path); err != nil {
			return out, err
		}
	}
	out.NVRAM = n
	return out, nil
}

func coldTPM(n *xmlNode) (domain.ColdTPM, error) {
	out := domain.ColdTPM{}
	if err := coldAttrs(n, nil, []string{"model"}); err != nil {
		return out, err
	}
	if strings.TrimSpace(n.text) != "" {
		return out, coldInvalid("text in TPM element")
	}
	out.Model = attr(n, "model")
	if !coldEnum(out.Model, "tpm-tis", "tpm-crb", "tpm-spapr") {
		return out, coldUnsupported("unsupported TPM model")
	}
	for _, name := range []string{"backend", "alias", "address"} {
		if _, err := coldChild(n, name, name == "backend"); err != nil {
			return out, err
		}
	}
	for _, c := range n.children {
		if c.name.Local != "backend" && c.name.Local != "alias" && c.name.Local != "address" {
			return out, coldUnsupported("unrecognized TPM element")
		}
	}
	b := child(n, "backend")
	if err := coldAttrs(b, []string{"type"}, []string{"version", "persistent_state", "debug"}); err != nil {
		return out, err
	}
	if attr(b, "type") != "emulator" {
		return out, coldUnsupported("only libvirt-managed emulator TPM state can be identified")
	}
	if strings.TrimSpace(b.text) != "" {
		return out, coldInvalid("text in TPM backend")
	}
	out.Version, out.PersistentState = attr(b, "version"), attr(b, "persistent_state")
	if !coldEnum(out.Version, "1.2", "2.0") || !coldEnum(out.PersistentState, "yes", "no") || out.Model == "tpm-crb" && out.Version == "1.2" {
		return out, coldUnsupported("unsupported TPM version or state policy")
	}
	if debug := attr(b, "debug"); debug != "" {
		if value, err := strconv.ParseUint(debug, 10, 8); err != nil || strconv.FormatUint(value, 10) != debug {
			return out, coldInvalid("invalid TPM debug level")
		}
	}
	for _, c := range b.children {
		if c.name.Space != "" {
			return out, coldInvalid("foreign TPM backend element")
		}
		if _, err := coldChild(b, c.name.Local, false); err != nil {
			return out, err
		}
		switch c.name.Local {
		case "source":
			if err := coldAttrs(c, []string{"type", "path"}, nil); err != nil {
				return out, err
			}
			if len(c.children) != 0 || strings.TrimSpace(c.text) != "" {
				return out, coldInvalid("TPM source must be empty")
			}
			out.SourceType, out.SourcePath = attr(c, "type"), attr(c, "path")
			if !coldEnum(out.SourceType, "file", "dir") {
				return out, coldUnsupported("unsupported TPM state source type")
			}
			if err := coldPath(out.SourcePath); err != nil {
				return out, err
			}
		case "encryption":
			if err := coldAttrs(c, []string{"secret"}, nil); err != nil {
				return out, err
			}
			if len(c.children) != 0 || strings.TrimSpace(c.text) != "" {
				return out, coldInvalid("TPM encryption must contain only a secret UUID reference")
			}
			var err error
			if out.EncryptionSecret, err = coldUUID(attr(c, "secret")); err != nil {
				return out, err
			}
		case "profile":
			if err := coldAttrs(c, nil, []string{"source", "name", "removeDisabled"}); err != nil {
				return out, err
			}
			if len(c.children) != 0 || strings.TrimSpace(c.text) != "" || out.Version == "1.2" {
				return out, coldInvalid("profile requires an emulator TPM 2.0 layout")
			}
			out.ProfileSource, out.Profile, out.ProfileRemoveDisabled = attr(c, "source"), attr(c, "name"), attr(c, "removeDisabled")
			if !coldProfile(out.ProfileSource) || !coldProfile(out.Profile) || !coldEnum(out.ProfileRemoveDisabled, "check", "fips-host") {
				return out, coldInvalid("invalid TPM profile metadata")
			}
		case "active_pcr_banks":
			if err := coldAttrs(c, nil, nil); err != nil {
				return out, err
			}
			if strings.TrimSpace(c.text) != "" || out.Version == "1.2" {
				return out, coldInvalid("PCR banks require an emulator TPM 2.0 layout")
			}
			for _, bank := range c.children {
				if _, err := coldChild(c, bank.name.Local, false); err != nil {
					return out, err
				}
				if !coldEnum(bank.name.Local, "sha1", "sha256", "sha384", "sha512") || len(bank.attrs) != 0 || len(bank.children) != 0 || strings.TrimSpace(bank.text) != "" {
					return out, coldInvalid("invalid TPM PCR bank")
				}
			}
		default:
			return out, coldUnsupported("unrecognized TPM backend layout")
		}
	}
	return out, nil
}

func coldProfile(value string) bool {
	if len(value) > 256 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == ':') {
			return false
		}
	}
	return true
}

// Native secret references outside metadata are dependencies, including disk
// encryption/auth references. Namespace-owned extension data remains opaque.
func coldSecrets(n *xmlNode, found map[string]bool) error {
	if n.name.Local == "metadata" {
		return nil
	}
	if n.name.Space != "" {
		if n.name.Local == "secret" {
			return coldInvalid("foreign secret reference")
		}
		return nil
	}
	if n.name.Local == "secret" {
		for _, a := range n.attrs {
			if a.Name.Local == "usage" {
				return coldUnsupported("secret usage references require explicit UUID resolution")
			}
		}
		if err := coldAttrs(n, []string{"type", "uuid"}, nil); err != nil {
			return err
		}
		if !coldEnum(attr(n, "type"), "passphrase", "ceph", "iscsi") || len(n.children) != 0 || strings.TrimSpace(n.text) != "" {
			return coldInvalid("malformed secret UUID reference")
		}
		id, err := coldUUID(attr(n, "uuid"))
		if err != nil {
			return err
		}
		found[id] = true
		if len(found) > 256 {
			return coldInvalid("secret reference limit")
		}
	}
	for _, c := range n.children {
		if err := coldSecrets(c, found); err != nil {
			return err
		}
	}
	return nil
}
