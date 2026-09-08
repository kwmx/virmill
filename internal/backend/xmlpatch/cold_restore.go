package xmlpatch

import (
	"bytes"
	"encoding/xml"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

type ColdRestoreDisk struct{ Target, Path, Format string }
type ColdRestorePatch struct {
	UUID, Name     string
	Disks          []ColdRestoreDisk
	NVRAMPath      string
	TPMPath        string
	DisconnectNICs bool
}

var restoreUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var restoreName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func restoreInvalid(message string) error {
	return domain.Fail("INVALID_INPUT", "cold restore XML: "+message)
}
func restoreUnsupported(message string) error {
	return domain.Fail("UNSUPPORTED_CAPABILITY", "cold restore XML: "+message)
}
func restoreValidUUID(value string) bool {
	return restoreUUID.MatchString(value) && value != "00000000-0000-0000-0000-000000000000"
}
func restorePath(value string) bool {
	if !utf8.ValidString(value) || len(value) > 4096 || value == "/" || !path.IsAbs(value) || path.Clean(value) != value || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
func restoreEscape(value string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(value))
	return b.String()
}

// restoreChild refuses a foreign element with the relevant local name; the
// ordinary positioned-tree selector deliberately ignores unrelated namespaces.
func restoreChild(n *positionedNode, name string, required bool) (*positionedNode, error) {
	var found *positionedNode
	for _, part := range n.parts {
		c := part.child
		if c != nil && c.name.Local == name {
			if c.name.Space != "" || found != nil {
				return nil, restoreInvalid("duplicate or foreign " + name)
			}
			found = c
		}
	}
	if required && found == nil {
		return nil, restoreInvalid("missing " + name)
	}
	return found, nil
}
func restoreAttrs(n *positionedNode, allowed ...string) error {
	if !n.onlyAttrs(allowed...) {
		return restoreUnsupported("unsupported " + n.name.Local + " attributes")
	}
	for _, a := range n.attrs {
		if a.Value == "" {
			return restoreInvalid("empty " + n.name.Local + " attribute")
		}
	}
	return nil
}
func restoreScalar(n *positionedNode) bool {
	for _, p := range n.parts {
		if p.child != nil || p.kind != "text" {
			return false
		}
	}
	return true
}
func restoreEmpty(n *positionedNode) bool {
	if strings.TrimSpace(n.text()) != "" {
		return false
	}
	for _, p := range n.parts {
		if p.child != nil || p.kind != "text" && p.kind != "comment" {
			return false
		}
	}
	return true
}
func restoreSetAttr(raw string, n *positionedNode, name, value string) (spanReplacement, error) {
	if _, present := n.attr(name); present {
		return attributeSpan(raw, n, name, restoreEscape(value))
	}
	i := n.startEnd - 1
	if n.empty {
		i--
	}
	return spanReplacement{i, i, ` ` + name + `="` + restoreEscape(value) + `"`}, nil
}
func restoreRemoveAttr(raw string, n *positionedNode, name string) (spanReplacement, error) {
	s, err := attributeSpan(raw, n, name, "")
	if err != nil {
		return s, err
	}
	// The existing locator selects only the value. Walk backward across its
	// quote, equals and XML whitespace to remove precisely this named attribute.
	i := s.start - 2
	for i >= n.start && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\r' || raw[i] == '\n' || raw[i] == '=') {
		i--
	}
	start := i - len(name) + 1
	if start <= n.start || raw[start:i+1] != name {
		return s, restoreInvalid("cannot locate source attribute")
	}
	return spanReplacement{start, s.end + 1, ""}, nil
}

// ColdRestore changes only reviewed identity, storage and optional NIC spans.
// It does not open paths, flatten disks, capture state, resolve secrets or prove
// staging freshness. The caller must retain the original captured XML and bind
// every supplied path to independently verified staged bytes before definition.
func ColdRestore(raw string, change ColdRestorePatch) (string, error) {
	newName, nameErr := validation.DisplayName(change.Name)
	if !restoreValidUUID(change.UUID) || nameErr != nil || len(change.Disks) > 256 {
		return "", restoreInvalid("canonical new UUID, bounded name and disk mappings required")
	}
	root, err := positionedXML(raw)
	if err != nil {
		return "", err
	}
	if err := restoreSafeTree(root); err != nil {
		return "", err
	}
	if err := restoreNamespaces(raw); err != nil {
		return "", err
	}
	uid, err := restoreChild(root, "uuid", true)
	if err != nil {
		return "", err
	}
	name, err := restoreChild(root, "name", true)
	if err != nil {
		return "", err
	}
	oldName, nameErr := validation.DisplayName(name.text())
	if !uid.onlyAttrs() || !name.onlyAttrs() || !restoreScalar(uid) || !restoreScalar(name) || !restoreValidUUID(uid.text()) || nameErr != nil || uid.text() == change.UUID || oldName == newName {
		return "", restoreInvalid("source identity is ambiguous or aliases restored identity")
	}
	osNode, err := restoreChild(root, "os", true)
	if err != nil {
		return "", err
	}
	devices, err := restoreChild(root, "devices", true)
	if err != nil {
		return "", err
	}
	spans := []spanReplacement{{uid.startEnd, uid.endStart, change.UUID}, {name.startEnd, name.endStart, restoreEscape(change.Name)}}
	newPaths := map[string]bool{}
	registerNew := func(p string) error {
		if !restorePath(p) {
			return restoreInvalid("staged path must be a canonical absolute path")
		}
		for prior := range newPaths {
			if p == prior || strings.HasPrefix(p, prior+"/") || strings.HasPrefix(prior, p+"/") {
				return restoreInvalid("staged paths alias or overlap")
			}
		}
		newPaths[p] = true
		return nil
	}
	mappings := map[string]ColdRestoreDisk{}
	for _, d := range change.Disks {
		if !targetID.MatchString(d.Target) || mappings[d.Target].Target != "" || d.Format != "raw" && d.Format != "qcow2" {
			return "", restoreInvalid("unique observed targets and raw/qcow2 formats required")
		}
		if err := registerNew(d.Path); err != nil {
			return "", err
		}
		mappings[d.Target] = d
	}
	for _, p := range []string{change.NVRAMPath, change.TPMPath} {
		if p != "" {
			if err := registerNew(p); err != nil {
				return "", err
			}
		}
	}
	oldPaths := map[string]bool{}
	used, targets, sourceIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	disks := 0
	for _, part := range devices.parts {
		n := part.child
		if n == nil {
			continue
		}
		if (n.name.Local == "disk" || n.name.Local == "interface" || n.name.Local == "tpm" || n.name.Local == "nvram") && n.name.Space != "" {
			return "", restoreInvalid("foreign relevant device")
		}
		if n.name.Local == "interface" && change.DisconnectNICs {
			spans = append(spans, spanReplacement{n.start, n.end, ""})
		}
		if n.name.Local != "disk" || n.name.Space != "" {
			continue
		}
		disks++
		if disks > 256 {
			return "", restoreInvalid("disk count exceeds 256")
		}
		added, target, sourceID, err := restoreDisk(raw, n, mappings, oldPaths)
		if err != nil {
			return "", err
		}
		if targets[target] || sourceID != "" && sourceIDs[sourceID] {
			return "", restoreInvalid("duplicate disk target or source identity")
		}
		targets[target] = true
		if sourceID != "" {
			sourceIDs[sourceID], used[target] = true, true
		}
		spans = append(spans, added...)
	}
	if len(used) != len(mappings) {
		return "", restoreInvalid("extra or stale disk mapping")
	}
	added, err := restoreAuxiliary(raw, osNode, devices, change, oldPaths)
	if err != nil {
		return "", err
	}
	spans = append(spans, added...)
	for staged := range newPaths {
		for old := range oldPaths {
			if staged == old || strings.HasPrefix(staged, old+"/") || strings.HasPrefix(old, staged+"/") {
				return "", restoreInvalid("staged path aliases an original storage or firmware path")
			}
		}
	}
	return replaceSpans(raw, spans)
}

func restoreSafeTree(root *positionedNode) error {
	declarations := 0
	for _, p := range root.exterior {
		if p.kind == "epilog-processing" || p.kind == "prolog-processing" && !strings.HasPrefix(p.text, "xml ") {
			return restoreInvalid("processing instructions are unsupported")
		}
		if p.kind == "prolog-processing" {
			declarations++
		}
	}
	if declarations > 1 {
		return restoreInvalid("duplicate XML declaration")
	}
	var walk func(*positionedNode) error
	walk = func(n *positionedNode) error {
		for _, p := range n.parts {
			if p.kind == "processing" {
				return restoreInvalid("processing instructions are unsupported")
			}
			if p.child != nil {
				if err := walk(p.child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(root)
}

// encoding/xml deliberately accepts unbound prefixes. The patch requires
// namespace-well-formed input while retaining every unrelated bound extension.
func restoreNamespaces(raw string) error {
	d := xml.NewDecoder(strings.NewReader(raw))
	stack := []map[string]string{}
	for {
		token, err := d.RawToken()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return restoreInvalid("malformed namespace syntax")
		}
		switch n := token.(type) {
		case xml.StartElement:
			bindings := map[string]string{"xml": "http://www.w3.org/XML/1998/namespace"}
			if len(stack) > 0 {
				for key, value := range stack[len(stack)-1] {
					bindings[key] = value
				}
			}
			for _, a := range n.Attr {
				if a.Name.Space == "xmlns" || a.Name.Space == "" && a.Name.Local == "xmlns" {
					key := a.Name.Local
					if a.Name.Space == "" {
						key = ""
					}
					if key == "xmlns" || key != "" && a.Value == "" || key == "xml" && a.Value != "http://www.w3.org/XML/1998/namespace" || key != "xml" && a.Value == "http://www.w3.org/XML/1998/namespace" || a.Value == "http://www.w3.org/2000/xmlns/" {
						return restoreInvalid("invalid namespace binding")
					}
					bindings[key] = a.Value
					if len(bindings) > 256 {
						return restoreInvalid("namespace binding limit")
					}
				}
			}
			if n.Name.Space != "" && bindings[n.Name.Space] == "" {
				return restoreInvalid("unbound element namespace")
			}
			for _, a := range n.Attr {
				if a.Name.Space != "" && a.Name.Space != "xmlns" && bindings[a.Name.Space] == "" {
					return restoreInvalid("unbound attribute namespace")
				}
			}
			stack = append(stack, bindings)
		case xml.EndElement:
			if len(stack) == 0 {
				return restoreInvalid("unbalanced namespace scope")
			}
			stack = stack[:len(stack)-1]
		}
	}
}

func restoreDisk(raw string, n *positionedNode, mappings map[string]ColdRestoreDisk, oldPaths map[string]bool) ([]spanReplacement, string, string, error) {
	fail := func(message string) ([]spanReplacement, string, string, error) {
		return nil, "", "", restoreUnsupported(message)
	}
	if strings.TrimSpace(n.text()) != "" {
		return fail("text in disk declaration")
	}
	for _, a := range n.attrs {
		if a.Name.Local == "type" || a.Name.Local == "device" {
			if a.Name.Space != "" || a.Value == "" {
				return fail("ambiguous disk type/device")
			}
		}
	}
	kind, _ := n.attr("type")
	if kind == "" {
		kind = "file" // Native default; the original attribute omission is retained.
	}
	device, _ := n.attr("device")
	if device == "" {
		device = "disk"
	}
	if kind != "file" && kind != "volume" || device != "disk" && device != "cdrom" && device != "floppy" {
		return fail("only file/simple volume disk or removable media layouts are supported")
	}
	for _, name := range []string{"mirror", "auth", "encryption", "transient", "shareable", "backenddomain", "privateData", "dataStore"} {
		c, err := restoreChild(n, name, false)
		if err != nil || c != nil {
			return fail("disk has external or shared storage policy")
		}
	}
	target, err := restoreChild(n, "target", true)
	if err != nil {
		return nil, "", "", err
	}
	id, _ := target.attr("dev")
	if !targetID.MatchString(id) || !restoreEmpty(target) {
		return fail("disk needs one scalar stable target")
	}
	for _, a := range target.attrs {
		if a.Name.Local == "dev" && a.Name.Space != "" {
			return fail("foreign disk target identity")
		}
	}
	source, err := restoreChild(n, "source", false)
	if err != nil {
		return nil, "", "", err
	}
	driver, err := restoreChild(n, "driver", false)
	if err != nil {
		return nil, "", "", err
	}
	backing, err := restoreChild(n, "backingStore", false)
	if err != nil {
		return nil, "", "", err
	}
	location, err := restoreStorageSource(source, kind, oldPaths)
	if err != nil {
		return nil, "", "", err
	}
	if location == "" {
		if device == "disk" || mappings[id].Target != "" || backing != nil && (!restoreEmpty(backing) || len(backing.attrs) != 0) {
			return fail("empty data disk, stale media mapping or empty media with backing")
		}
		return nil, id, "", nil // Empty removable media remains byte exact.
	}
	mapping, present := mappings[id]
	if !present {
		return fail("every nonempty disk/media target requires one staged mapping")
	}
	spans := []spanReplacement{}
	if kind == "file" {
		s, err := restoreSetAttr(raw, source, "file", mapping.Path)
		if err != nil {
			return nil, "", "", err
		}
		spans = append(spans, s)
	} else {
		s, err := restoreSetAttr(raw, n, "type", "file")
		if err != nil {
			return nil, "", "", err
		}
		spans = append(spans, s)
		for _, name := range []string{"pool", "volume"} {
			s, err := restoreRemoveAttr(raw, source, name)
			if err != nil {
				return nil, "", "", err
			}
			spans = append(spans, s)
		}
		s, err = restoreSetAttr(raw, source, "file", mapping.Path)
		if err != nil {
			return nil, "", "", err
		}
		spans = append(spans, s)
	}
	if driver == nil {
		spans = append(spans, spanReplacement{n.startEnd, n.startEnd, `<driver name="qemu" type="` + mapping.Format + `"/>`})
	} else {
		if strings.TrimSpace(driver.text()) != "" {
			return fail("text in disk driver")
		}
		for _, a := range driver.attrs {
			if a.Name.Local == "type" || a.Name.Local == "name" {
				if a.Name.Space != "" || a.Value == "" {
					return fail("ambiguous driver format/name")
				}
			}
		}
		if name, ok := driver.attr("name"); ok && name != "qemu" {
			return fail("disk driver is not qemu")
		}
		if format, ok := driver.attr("type"); ok && format != "raw" && format != "qcow2" {
			return fail("source disk format is unsupported")
		}
		s, err := restoreSetAttr(raw, driver, "type", mapping.Format)
		if err != nil {
			return nil, "", "", err
		}
		spans = append(spans, s)
	}
	if backing != nil {
		if err := restoreBacking(backing, oldPaths, map[string]bool{location: true}, 0); err != nil {
			return nil, "", "", err
		}
		spans = append(spans, spanReplacement{backing.start, backing.end, ""})
	}
	return spans, id, location, nil
}

func restoreStorageSource(source *positionedNode, kind string, oldPaths map[string]bool) (string, error) {
	if source == nil {
		return "", nil
	}
	allowed := []string{"startupPolicy", "index", "file"}
	if kind == "volume" {
		allowed = []string{"startupPolicy", "index", "pool", "volume"}
	}
	if err := restoreAttrs(source, allowed...); err != nil || !restoreEmpty(source) {
		return "", restoreUnsupported("structured, empty-attribute or unsupported storage source")
	}
	if policy, ok := source.attr("startupPolicy"); ok && policy != "mandatory" && policy != "requisite" && policy != "optional" {
		return "", restoreInvalid("invalid source startup policy")
	}
	if index, ok := source.attr("index"); ok {
		v, err := strconv.ParseUint(index, 10, 32)
		if err != nil || v == 0 || strconv.FormatUint(v, 10) != index {
			return "", restoreInvalid("invalid source index")
		}
	}
	if kind == "file" {
		file, present := source.attr("file")
		if !present {
			return "", nil
		}
		if !restorePath(file) {
			return "", restoreInvalid("unsafe original file path")
		}
		oldPaths[file] = true
		return "file:" + file, nil
	}
	pool, p := source.attr("pool")
	volume, v := source.attr("volume")
	if !p && !v {
		return "", nil
	}
	if !restoreName.MatchString(pool) || !restoreName.MatchString(volume) || pool == "." || pool == ".." || volume == "." || volume == ".." {
		return "", restoreInvalid("ambiguous pool/volume identity")
	}
	return "volume:" + pool + "/" + volume, nil
}

func restoreBacking(n *positionedNode, oldPaths map[string]bool, seen map[string]bool, depth int) error {
	if depth > 32 {
		return restoreInvalid("backing chain depth limit")
	}
	if len(n.attrs) == 0 && restoreEmpty(n) {
		return nil
	}
	if err := restoreAttrs(n, "type", "index"); err != nil {
		return err
	}
	kind, _ := n.attr("type")
	if kind == "" {
		kind = "file"
	}
	if kind != "file" && kind != "volume" {
		return restoreUnsupported("unsupported backing source type")
	}
	for _, p := range n.parts {
		if p.child != nil && (p.child.name.Space != "" || p.child.name.Local != "source" && p.child.name.Local != "format" && p.child.name.Local != "backingStore") || p.kind == "text" && strings.TrimSpace(p.text) != "" {
			return restoreUnsupported("unsupported backing configuration")
		}
	}
	source, err := restoreChild(n, "source", true)
	if err != nil {
		return err
	}
	location, err := restoreStorageSource(source, kind, oldPaths)
	if err != nil || location == "" || seen[location] {
		return restoreInvalid("missing, unsafe or repeated backing source")
	}
	seen[location] = true
	format, err := restoreChild(n, "format", false)
	if err != nil {
		return err
	}
	if format != nil {
		kind, _ := format.attr("type")
		if restoreAttrs(format, "type") != nil || !restoreEmpty(format) || kind != "raw" && kind != "qcow2" {
			return restoreUnsupported("unsupported backing format")
		}
	}
	next, err := restoreChild(n, "backingStore", false)
	if err != nil || next == nil {
		return err
	}
	return restoreBacking(next, oldPaths, seen, depth+1)
}

func restoreAuxiliary(raw string, osNode, devices *positionedNode, change ColdRestorePatch, oldPaths map[string]bool) ([]spanReplacement, error) {
	spans := []spanReplacement{}
	for _, unsupported := range []struct {
		parent *positionedNode
		name   string
	}{{osNode, "varstore"}, {devices, "nvram"}} {
		n, err := restoreChild(unsupported.parent, unsupported.name, false)
		if err != nil || n != nil {
			return nil, restoreUnsupported("unsupported auxiliary state device")
		}
	}
	loader, err := restoreChild(osNode, "loader", false)
	if err != nil {
		return nil, err
	}
	if loader != nil {
		if restoreAttrs(loader, "type", "readonly", "secure", "format", "stateless") != nil || !restoreScalar(loader) {
			return nil, restoreUnsupported("unsupported firmware loader metadata")
		}
		for _, field := range []string{"readonly", "secure", "stateless"} {
			if v, present := loader.attr(field); present && v != "yes" && v != "no" {
				return nil, restoreInvalid("invalid firmware boolean")
			}
		}
		if v, present := loader.attr("type"); present && v != "pflash" && v != "rom" {
			return nil, restoreUnsupported("unsupported firmware loader type")
		}
		if v, present := loader.attr("format"); present && v != "raw" && v != "qcow2" {
			return nil, restoreUnsupported("unsupported firmware loader format")
		}
		if loader.text() != "" && !restorePath(loader.text()) {
			return nil, restoreInvalid("unsafe firmware path")
		}
		if loader.text() != "" {
			oldPaths[loader.text()] = true
		}
	}
	nvram, err := restoreChild(osNode, "nvram", false)
	if err != nil {
		return nil, err
	}
	if (nvram == nil) != (change.NVRAMPath == "") {
		return nil, restoreInvalid("NVRAM remap must exactly match existing explicit state")
	}
	if nvram != nil {
		if restoreAttrs(nvram, "type", "format", "template", "templateFormat") != nil {
			return nil, restoreUnsupported("unsupported NVRAM metadata")
		}
		if loader == nil || loader.text() == "" {
			return nil, restoreUnsupported("NVRAM requires pinned pflash firmware")
		}
		if kind, _ := loader.attr("type"); kind != "pflash" {
			return nil, restoreUnsupported("NVRAM requires pflash firmware")
		}
		if stateless, _ := loader.attr("stateless"); stateless == "yes" {
			return nil, restoreInvalid("stateless firmware cannot restore NVRAM")
		}
		for _, attr := range []string{"format", "templateFormat"} {
			if value, present := nvram.attr(attr); present && value != "raw" && value != "qcow2" {
				return nil, restoreUnsupported("unsupported NVRAM format")
			}
		}
		if template, present := nvram.attr("template"); present {
			if !restorePath(template) {
				return nil, restoreInvalid("unsafe NVRAM template")
			}
			oldPaths[template] = true
		}
		source, err := restoreChild(nvram, "source", false)
		if err != nil {
			return nil, err
		}
		if source == nil {
			if _, typed := nvram.attr("type"); typed || !restoreScalar(nvram) || !restorePath(nvram.text()) {
				return nil, restoreUnsupported("implicit or ambiguous NVRAM path")
			}
			oldPaths[nvram.text()] = true
			spans = append(spans, spanReplacement{nvram.startEnd, nvram.endStart, restoreEscape(change.NVRAMPath)})
		} else {
			kind, typed := nvram.attr("type")
			if typed && kind != "file" || strings.TrimSpace(nvram.text()) != "" || restoreAttrs(source, "file") != nil || !restoreEmpty(source) {
				return nil, restoreUnsupported("unsupported NVRAM source")
			}
			for _, part := range nvram.parts {
				if part.child != nil && part.child != source {
					return nil, restoreUnsupported("unknown NVRAM source configuration")
				}
			}
			file, _ := source.attr("file")
			if !restorePath(file) {
				return nil, restoreInvalid("unsafe NVRAM source path")
			}
			oldPaths[file] = true
			s, err := restoreSetAttr(raw, source, "file", change.NVRAMPath)
			if err != nil {
				return nil, err
			}
			spans = append(spans, s)
		}
	}
	tpm, err := restoreChild(devices, "tpm", false)
	if err != nil {
		return nil, err
	}
	if (tpm == nil) != (change.TPMPath == "") {
		return nil, restoreInvalid("TPM remap must exactly match existing explicit state")
	}
	if tpm != nil {
		if restoreAttrs(tpm, "model") != nil || strings.TrimSpace(tpm.text()) != "" {
			return nil, restoreUnsupported("unsupported TPM device metadata")
		}
		model, present := tpm.attr("model")
		if present && model != "tpm-tis" && model != "tpm-crb" && model != "tpm-spapr" {
			return nil, restoreUnsupported("unsupported TPM device model")
		}
		for _, p := range tpm.parts {
			if c := p.child; c != nil {
				if c.name.Space != "" || c.name.Local != "backend" && c.name.Local != "alias" && c.name.Local != "address" {
					return nil, restoreUnsupported("unsupported TPM device child")
				}
				if _, err := restoreChild(tpm, c.name.Local, true); err != nil {
					return nil, err
				}
			}
		}
		backend, err := restoreChild(tpm, "backend", true)
		if err != nil {
			return nil, err
		}
		if kind, _ := backend.attr("type"); kind != "emulator" {
			return nil, restoreUnsupported("only emulator TPM state can be remapped")
		}
		if restoreAttrs(backend, "type", "version", "persistent_state", "debug") != nil || strings.TrimSpace(backend.text()) != "" {
			return nil, restoreUnsupported("unsupported TPM backend metadata")
		}
		if version, present := backend.attr("version"); present && version != "1.2" && version != "2.0" {
			return nil, restoreUnsupported("unsupported TPM version")
		}
		version, _ := backend.attr("version")
		if model == "tpm-crb" && version == "1.2" {
			return nil, restoreInvalid("TPM CRB cannot select version 1.2")
		}
		if value, present := backend.attr("persistent_state"); present && value != "yes" && value != "no" {
			return nil, restoreInvalid("invalid TPM persistent-state setting")
		}
		if value, present := backend.attr("debug"); present {
			v, err := strconv.ParseUint(value, 10, 8)
			if err != nil || strconv.FormatUint(v, 10) != value {
				return nil, restoreInvalid("invalid TPM debug setting")
			}
		}
		source, err := restoreChild(backend, "source", true)
		if err != nil {
			return nil, err
		}
		if restoreAttrs(source, "type", "path") != nil || !restoreEmpty(source) {
			return nil, restoreUnsupported("unsupported TPM source")
		}
		kind, _ := source.attr("type")
		old, _ := source.attr("path")
		if kind != "file" && kind != "dir" || !restorePath(old) {
			return nil, restoreUnsupported("explicit file/dir TPM source required")
		}
		for _, part := range backend.parts {
			c := part.child
			if c == nil {
				continue
			}
			if c.name.Space != "" || c.name.Local != "source" && c.name.Local != "encryption" && c.name.Local != "profile" && c.name.Local != "active_pcr_banks" {
				return nil, restoreUnsupported("unsupported TPM backend child")
			}
			if _, err := restoreChild(backend, c.name.Local, true); err != nil {
				return nil, err
			}
			if c.name.Local == "encryption" {
				secret, _ := c.attr("secret")
				if restoreAttrs(c, "secret") != nil || !restoreEmpty(c) || !restoreValidUUID(secret) {
					return nil, restoreInvalid("ambiguous TPM secret reference")
				}
			}
			if c.name.Local == "profile" {
				if version == "1.2" || restoreAttrs(c, "source", "name", "removeDisabled") != nil || !restoreEmpty(c) {
					return nil, restoreInvalid("invalid TPM profile layout")
				}
				for _, key := range []string{"source", "name"} {
					value, _ := c.attr(key)
					if len(value) > 256 || strings.Trim(value, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789.:-") != "" {
						return nil, restoreInvalid("invalid TPM profile identifier")
					}
				}
				if v, present := c.attr("removeDisabled"); present && v != "check" && v != "fips-host" {
					return nil, restoreInvalid("invalid TPM profile setting")
				}
			}
			if c.name.Local == "active_pcr_banks" {
				if version == "1.2" || !c.onlyAttrs() || strings.TrimSpace(c.text()) != "" {
					return nil, restoreInvalid("invalid TPM PCR layout")
				}
				for _, part := range c.parts {
					bank := part.child
					if bank == nil {
						continue
					}
					if bank.name.Local != "sha1" && bank.name.Local != "sha256" && bank.name.Local != "sha384" && bank.name.Local != "sha512" || !bank.onlyAttrs() || !restoreEmpty(bank) {
						return nil, restoreInvalid("invalid TPM PCR bank")
					}
					if _, err := restoreChild(c, bank.name.Local, true); err != nil {
						return nil, err
					}
				}
			}
		}
		oldPaths[old] = true
		s, err := restoreSetAttr(raw, source, "path", change.TPMPath)
		if err != nil {
			return nil, err
		}
		spans = append(spans, s)
	}
	return spans, nil
}
