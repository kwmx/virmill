//go:build linux && amd64

package creating

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
)

func nvramValidationFixture(t testing.TB) (domain.Plan, input, Receipt, nvramDeclarationBinding) {
	t.Helper()
	p := domain.Plan{ID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", ConnectionID: "fixture", Operation: "vm.create", InputDigest: strings.Repeat("a", 64)}
	in := input{NVRAMDeclarationVersion: 1, Target: domain.CreationTarget{FirmwareDigest: strings.Repeat("b", 64), Spec: domain.CreationSpec{
		UUID:     "11111111-2222-4333-8444-555555555555",
		Firmware: domain.CreationFirmware{Mode: "uefi", Code: "/fixture/firmware/CODE.fd", Template: "/fixture/firmware/template VARS.fd", Format: "raw"},
	}}}
	binding, err := operations.Digest([]string{p.ID, p.InputDigest})
	if err != nil {
		t.Fatal(err)
	}
	r := Receipt{Version: 1, PlanID: p.ID, OperationID: "99999999-aaaa-4bbb-8ccc-dddddddddddd", VMID: in.Target.Spec.UUID, Connection: p.ConnectionID, Binding: binding}
	b := nvramDeclarationBinding{Version: 1, PlanID: p.ID, InputDigest: p.InputDigest, OperationID: r.OperationID,
		Resource:        domain.ResourceKey{ProviderID: "libvirt", ConnectionID: p.ConnectionID, Kind: "vm", UUID: r.VMID},
		CreationBinding: binding, FirmwareDigest: in.Target.FirmwareDigest, ObservedFingerprint: strings.Repeat("c", 64),
		Firmware: domain.ColdFirmware{Loader: in.Target.Spec.Firmware.Code, LoaderType: "pflash", LoaderReadOnly: "yes", LoaderSecure: "no", LoaderFormat: "raw",
			NVRAM: &domain.ColdNVRAM{Path: "/fixture/state/guest VARS.fd", Format: "raw", Template: in.Target.Spec.Firmware.Template, TemplateFormat: "raw"}},
	}
	return p, in, r, b
}

func TestNVRAMValidationCanonicalPath(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		valid      bool
	}{
		{"absolute", "/fixture/VARS.fd", true},
		{"spaces in filename", "/fixture/Virmill UEFI TPM cold probe_VARS.fd", true},
		{"unicode filename", "/fixture/ضيف VARS.fd", true},
		{"ordinary dots", "/fixture/.guest..VARS.fd", true},
		{"4096 bytes", "/" + strings.Repeat("a/", 2046) + "abc", true},
		{"empty", "", false},
		{"root", "/", false},
		{"relative", "VARS.fd", false},
		{"dot", ".", false},
		{"parent", "..", false},
		{"dot component", "/fixture/./VARS.fd", false},
		{"parent component", "/fixture/../VARS.fd", false},
		{"doubled root separator", "//fixture/VARS.fd", false},
		{"doubled separator", "/fixture//VARS.fd", false},
		{"trailing separator", "/fixture/VARS.fd/", false},
		{"leading space", " /fixture/VARS.fd", false},
		{"trailing space", "/fixture/VARS.fd ", false},
		{"whitespace only", " \n\t", false},
		{"tab", "/fixture/guest\tVARS.fd", false},
		{"newline", "/fixture/guest\nVARS.fd", false},
		{"carriage return", "/fixture/guest\rVARS.fd", false},
		{"NUL", "/fixture/guest\x00VARS.fd", false},
		{"DEL", "/fixture/guest\x7fVARS.fd", false},
		{"C1 control", "/fixture/guest\u0085VARS.fd", false},
		{"bidi format", "/fixture/guest\u202eVARS.fd", false},
		{"invalid UTF8", "/fixture/guest\xffVARS.fd", false},
		{"4097 bytes", "/" + strings.Repeat("a/", 2046) + "abcd", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nvramPath(tc.path); got != tc.valid {
				t.Fatalf("path validity=%t, want %t", got, tc.valid)
			}
			// Path syntax is also enforced when accepting a declaration proof.
			p, in, r, b := nvramValidationFixture(t)
			b.Firmware.NVRAM.Path = tc.path
			if err := validateNVRAMBinding(p, in, r, b); (err == nil) != tc.valid {
				t.Fatalf("binding accepted=%t, want %t: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestNVRAMValidationExactFirmwareMapping(t *testing.T) {
	for _, format := range []string{"raw", "qcow2"} {
		for _, secure := range []bool{false, true} {
			p, in, r, b := nvramValidationFixture(t)
			in.Target.Spec.Firmware.Format, in.Target.Spec.Firmware.SecureBoot = format, secure
			b.Firmware.LoaderFormat, b.Firmware.NVRAM.Format, b.Firmware.NVRAM.TemplateFormat = format, format, format
			if secure {
				b.Firmware.LoaderSecure = "yes"
			}
			if err := validateNVRAMBinding(p, in, r, b); err != nil {
				t.Fatal("exact reviewed firmware mapping refused", format, secure, err)
			}
		}
	}
	for name, change := range map[string]func(*domain.CreationFirmware, *domain.ColdFirmware){
		"BIOS recipe":               func(w *domain.CreationFirmware, _ *domain.ColdFirmware) { w.Mode = "bios" },
		"unsupported wanted format": func(w *domain.CreationFirmware, _ *domain.ColdFirmware) { w.Format = "vmdk" },
		"relative wanted code":      func(w *domain.CreationFirmware, _ *domain.ColdFirmware) { w.Code = "CODE.fd" },
		"unsafe wanted template":    func(w *domain.CreationFirmware, _ *domain.ColdFirmware) { w.Template = "/fixture/../VARS.fd" },
		"changed code":              func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.Loader = "/fixture/OTHER_CODE.fd" },
		"changed template":          func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.NVRAM.Template = "/fixture/OTHER_VARS.fd" },
		"missing NVRAM":             func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.NVRAM = nil },
		"missing loader type":       func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.LoaderType = "" },
		"ROM loader":                func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.LoaderType = "rom" },
		"unknown readonly":          func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.LoaderReadOnly = "" },
		"writable loader":           func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.LoaderReadOnly = "no" },
		"unknown security":          func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.LoaderSecure = "" },
		"security enabled":          func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.LoaderSecure = "yes" },
		"security disabled":         func(w *domain.CreationFirmware, _ *domain.ColdFirmware) { w.SecureBoot = true },
		"stateless yes":             func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.LoaderStateless = "yes" },
		"unreviewed explicit no":    func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.LoaderStateless = "no" },
		"missing code format":       func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.LoaderFormat = "" },
		"changed code format":       func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.LoaderFormat = "qcow2" },
		"missing state format":      func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.NVRAM.Format = "" },
		"changed state format":      func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.NVRAM.Format = "qcow2" },
		"missing template format":   func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.NVRAM.TemplateFormat = "" },
		"changed template format":   func(_ *domain.CreationFirmware, f *domain.ColdFirmware) { f.NVRAM.TemplateFormat = "qcow2" },
	} {
		t.Run(name, func(t *testing.T) {
			p, in, r, b := nvramValidationFixture(t)
			change(&in.Target.Spec.Firmware, &b.Firmware)
			if err := validateNVRAMBinding(p, in, r, b); err == nil {
				t.Fatal("different or incomplete firmware mapping accepted as the reviewed declaration")
			}
		})
	}
}

func TestNVRAMValidationBindingIdentity(t *testing.T) {
	for name, change := range map[string]func(*domain.Plan, *input, *Receipt, *nvramDeclarationBinding){
		"legacy recipe": func(_ *domain.Plan, in *input, _ *Receipt, _ *nvramDeclarationBinding) {
			in.NVRAMDeclarationVersion = 0
		},
		"future recipe": func(_ *domain.Plan, in *input, _ *Receipt, _ *nvramDeclarationBinding) {
			in.NVRAMDeclarationVersion = 2
		},
		"negative recipe": func(_ *domain.Plan, in *input, _ *Receipt, _ *nvramDeclarationBinding) {
			in.NVRAMDeclarationVersion = -1
		},
		"missing proof version": func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) { b.Version = 0 },
		"future proof version":  func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) { b.Version = 2 },
		"different plan":        func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) { b.PlanID = "other-plan" },
		"different input": func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) {
			b.InputDigest = strings.Repeat("d", 64)
		},
		"different operation": func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) {
			b.OperationID = "other-operation"
		},
		"missing operation in both": func(_ *domain.Plan, _ *input, r *Receipt, b *nvramDeclarationBinding) {
			r.OperationID, b.OperationID = "", ""
		},
		"different provider": func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) {
			b.Resource.ProviderID = "other-provider"
		},
		"different connection": func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) {
			b.Resource.ConnectionID = "other-connection"
		},
		"different kind":         func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) { b.Resource.Kind = "volume" },
		"different VM":           func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) { b.Resource.UUID = "other-vm" },
		"different receipt plan": func(_ *domain.Plan, _ *input, r *Receipt, _ *nvramDeclarationBinding) { r.PlanID = "other-plan" },
		"different receipt VM":   func(_ *domain.Plan, _ *input, r *Receipt, _ *nvramDeclarationBinding) { r.VMID = "other-vm" },
		"different receipt connection": func(_ *domain.Plan, _ *input, r *Receipt, _ *nvramDeclarationBinding) {
			r.Connection = "other-connection"
		},
		"different receipt creation binding": func(_ *domain.Plan, _ *input, r *Receipt, _ *nvramDeclarationBinding) {
			r.Binding = strings.Repeat("d", 64)
		},
		"different proof creation binding": func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) {
			b.CreationBinding = strings.Repeat("d", 64)
		},
		"both creation bindings forged": func(_ *domain.Plan, _ *input, r *Receipt, b *nvramDeclarationBinding) {
			r.Binding, b.CreationBinding = strings.Repeat("d", 64), strings.Repeat("d", 64)
		},
		"different firmware digest": func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) {
			b.FirmwareDigest = strings.Repeat("d", 64)
		},
		"empty firmware digests": func(_ *domain.Plan, in *input, _ *Receipt, b *nvramDeclarationBinding) {
			in.Target.FirmwareDigest, b.FirmwareDigest = "", ""
		},
		"nonhex firmware digests": func(_ *domain.Plan, in *input, _ *Receipt, b *nvramDeclarationBinding) {
			in.Target.FirmwareDigest, b.FirmwareDigest = strings.Repeat("z", 64), strings.Repeat("z", 64)
		},
		"uppercase firmware digests": func(_ *domain.Plan, in *input, _ *Receipt, b *nvramDeclarationBinding) {
			in.Target.FirmwareDigest, b.FirmwareDigest = strings.Repeat("B", 64), strings.Repeat("B", 64)
		},
		"missing observation fingerprint": func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) { b.ObservedFingerprint = "" },
		"short observation fingerprint": func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) {
			b.ObservedFingerprint = strings.Repeat("c", 63)
		},
		"nonhex observation fingerprint": func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) {
			b.ObservedFingerprint = strings.Repeat("g", 64)
		},
		"uppercase observation fingerprint": func(_ *domain.Plan, _ *input, _ *Receipt, b *nvramDeclarationBinding) {
			b.ObservedFingerprint = strings.Repeat("C", 64)
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, in, r, b := nvramValidationFixture(t)
			change(&p, &in, &r, &b)
			if err := validateNVRAMBinding(p, in, r, b); err == nil {
				t.Fatal("unbound, incompatible or mismatched declaration proof accepted")
			}
		})
	}
}

func TestNVRAMValidationRecipeVersionBoundary(t *testing.T) {
	for _, mode := range []string{"bios", "uefi"} {
		for _, version := range []int{-1, 0, 1, 2} {
			p, in, _, _ := nvramValidationFixture(t)
			in.Target.Spec.Firmware.Mode, in.NVRAMDeclarationVersion = mode, version
			want := version == 0 || mode == "uefi" && version == 1
			if err := creationRecipeVersion(p, in); (err == nil) != want {
				t.Fatalf("mode=%s version=%d accepted=%t, want %t: %v", mode, version, err == nil, want, err)
			}
		}
	}
}

func TestNVRAMValidationStoredJSON(t *testing.T) {
	p, in, r, b := nvramValidationFixture(t)
	encoded, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	base := string(encoded)
	db, err := store.Open(filepath.Join(t.TempDir(), "state", "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Service{Store: db} // No backend: these tests cannot observe native state.
	if got, err := s.loadNVRAMBinding(context.Background(), p, in, r); err != nil || got != nil {
		t.Fatal("absent proof was not represented as absent", got, err)
	}
	cases := map[string]string{
		"valid":                            base,
		"empty":                            "",
		"truncated":                        base[:len(base)-1],
		"null":                             "null",
		"array":                            "[]",
		"wrong root type":                  `"binding"`,
		"trailing value":                   base + "{}",
		"unknown top field":                strings.Replace(base, "{", `{"initializationVerified":true,`, 1),
		"unknown resource field":           strings.Replace(base, `"resource":{`, `"resource":{"nativeOwner":true,`, 1),
		"unknown firmware field":           strings.Replace(base, `"firmware":{`, `"firmware":{"generation":"invented",`, 1),
		"unknown NVRAM field":              strings.Replace(base, `"nvram":{`, `"nvram":{"fresh":true,`, 1),
		"duplicate version":                strings.Replace(base, `"schemaVersion":1`, `"schemaVersion":2,"schemaVersion":1`, 1),
		"escaped duplicate version":        strings.Replace(base, `"schemaVersion":1`, `"schemaVersion":2,"schema\u0056ersion":1`, 1),
		"duplicate nested path":            strings.Replace(base, `"path":`, `"path":"/fixture/other.fd","path":`, 1),
		"case variant version alias":       strings.Replace(base, `"schemaVersion":1`, `"schemaVersion":2,"SchemaVersion":1`, 1),
		"case variant nested format alias": strings.Replace(base, `"format":"raw"`, `"format":"qcow2","Format":"raw"`, 1),
		"same value version alias":         strings.Replace(base, `"schemaVersion":1`, `"schemaVersion":1,"SchemaVersion":1`, 1),
		"same value resource alias":        strings.Replace(base, `"providerID":"libvirt"`, `"providerID":"libvirt","ProviderID":"libvirt"`, 1),
		"same value firmware alias":        strings.Replace(base, `"loaderReadOnly":"yes"`, `"loaderReadOnly":"yes","LoaderReadOnly":"yes"`, 1),
		"same value NVRAM alias":           strings.Replace(base, `"path":"`+b.Firmware.NVRAM.Path+`"`, `"path":"`+b.Firmware.NVRAM.Path+`","Path":"`+b.Firmware.NVRAM.Path+`"`, 1),
		"wrong case field only":            strings.Replace(base, `"schemaVersion":1`, `"SchemaVersion":1`, 1),
		"wrong case nested field only":     strings.Replace(base, `"loaderStateless":""`, `"LoaderStateless":""`, 1),
		"null scalar cannot mean empty":    strings.Replace(base, `"loaderStateless":""`, `"loaderStateless":null`, 1),
		"future version":                   strings.Replace(base, `"schemaVersion":1`, `"schemaVersion":2`, 1),
		"string version":                   strings.Replace(base, `"schemaVersion":1`, `"schemaVersion":"1"`, 1),
		"missing operation":                strings.Replace(base, `"operationID":"`+r.OperationID+`",`, "", 1),
		"missing NVRAM":                    strings.Replace(base, `"nvram":{`, `"nvramOmitted":{`, 1),
		"empty path":                       strings.Replace(base, `"path":"`+b.Firmware.NVRAM.Path+`"`, `"path":""`, 1),
		"escaped NUL path":                 strings.Replace(base, `"path":"`+b.Firmware.NVRAM.Path+`"`, `"path":"/fixture/guest\u0000VARS.fd"`, 1),
		"unpaired surrogate":               strings.Replace(base, b.Firmware.NVRAM.Path, `/fixture/guest\ud800VARS.fd`, 1),
		"invalid UTF8":                     strings.Replace(base, b.Firmware.NVRAM.Path, "/fixture/guest\xffVARS.fd", 1),
	}
	for _, fields := range [][]string{
		{"schemaVersion"}, {"planID"}, {"inputDigest"}, {"operationID"}, {"resource"},
		{"creationBinding"}, {"firmwareDigest"}, {"firmware"}, {"observedFingerprint"},
		{"resource", "providerID"}, {"resource", "connectionID"}, {"resource", "kind"}, {"resource", "resourceUUID"},
		{"firmware", "loader"}, {"firmware", "loaderType"}, {"firmware", "loaderReadOnly"},
		{"firmware", "loaderSecure"}, {"firmware", "loaderFormat"}, {"firmware", "loaderStateless"}, {"firmware", "nvram"},
		{"firmware", "nvram", "path"}, {"firmware", "nvram", "format"},
		{"firmware", "nvram", "template"}, {"firmware", "nvram", "templateFormat"},
	} {
		var record map[string]any
		if err := json.Unmarshal(encoded, &record); err != nil {
			t.Fatal(err)
		}
		object := record
		for _, field := range fields[:len(fields)-1] {
			object = object[field].(map[string]any)
		}
		field := fields[len(fields)-1]
		if _, present := object[field]; !present {
			t.Fatal("missing-field fixture selected a nonexistent key", strings.Join(fields, "."))
		}
		delete(object, field)
		changed, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		cases["missing "+strings.Join(fields, ".")] = string(changed)
	}
	var reordered map[string]any
	if err := json.Unmarshal(encoded, &reordered); err != nil {
		t.Fatal(err)
	}
	indented, err := json.MarshalIndent(reordered, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	cases["valid reordered and indented"] = string(indented)
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			// Deliberate corruption of a private generated journal exercises the
			// actual loader, including bytes no JSON marshaler would produce.
			if _, err := db.DB.Exec("INSERT INTO metadata(kind,id,body) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET body=excluded.body", nvramDeclarationKind, p.ID, []byte(raw)); err != nil {
				t.Fatal(err)
			}
			got, err := s.loadNVRAMBinding(context.Background(), p, in, r)
			if name == "valid" || name == "valid reordered and indented" {
				if err != nil || got == nil || !reflect.DeepEqual(*got, b) {
					t.Fatal("valid stored declaration changed or was refused", got, err)
				}
			} else if err == nil || got != nil {
				t.Fatal("invalid stored declaration returned a successful proof", got, err)
			}
			after, readErr := db.MetadataBytes(nvramDeclarationKind, p.ID)
			if readErr != nil || string(after) != raw {
				t.Fatal("failed validation rewrote the stored evidence", readErr)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := s.loadNVRAMBinding(ctx, p, in, r); !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("canceled load returned a proof", got, err)
	}
}
