//go:build linux && cgo

package libvirt

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"virmill.local/core/internal/domain"
)

const restoreTestID = "87654321-4321-4321-8321-cba987654321"

func coldRestoreDefinitionFixture() domain.ColdRestoreDefinition {
	return domain.ColdRestoreDefinition{
		UUID: restoreTestID, Name: "restored-cold", DisconnectNICs: true,
		SourceXML: `<domain type='kvm' xmlns:x='urn:opaque'><name>captured-cold</name><uuid>12345678-1234-4234-8234-123456789abc</uuid><metadata><x:policy exact='keep'> retained opaque text </x:policy></metadata><memory unit='KiB'>524288</memory><vcpu>2</vcpu><os><type arch='x86_64' machine='pc-q35-10.2'>hvm</type></os><devices><disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='/original/root.qcow2'/><target dev='vda' bus='virtio'/><boot order='1'/><alias name='ua-root'/></disk><interface type='network'><source network='original'/></interface><memballoon model='none'/></devices></domain>`,
		Disks:     []domain.ColdRestoredDisk{{Target: "vda", Format: "qcow2", Volume: domain.CreatedVolume{Intent: domain.VolumeIntent{PoolID: "11111111-2222-4333-8444-555555555555", Name: "virmill-" + restoreTestID + "-disk-000.qcow2", SourceID: "vda", VirtualBytes: 1048576, FileBytes: 32768, SHA256: strings.Repeat("a", 64)}, BackendKey: "/staged/root.qcow2", Path: "/staged/root.qcow2", Generation: "linux-statx-v1:1:2:3:1700000000:000000001"}}},
	}
}

type coldRestoreFixture struct {
	t            *testing.T
	def          domain.ColdRestoreDefinition
	vm           domain.VM
	secureXML    string
	found        bool
	calls        []string
	volumes      []domain.CreatedVolume
	exceptions   []string
	defines      int
	observeCalls int
	secureCalls  int
	closed       int
	fault        func(string, int) error
	onObserve    func(int, *domain.VM)
	onSecure     func(int)
	onClose      func()
}

func newColdRestoreFixture(t *testing.T) *coldRestoreFixture {
	t.Helper()
	f := &coldRestoreFixture{t: t, def: coldRestoreDefinitionFixture(), found: true}
	wanted, err := coldRestoreXML(f.def)
	if err != nil {
		t.Fatal(err)
	}
	f.vm = domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: f.def.UUID}, Name: f.def.Name, State: "stopped", PersistentXML: wanted, Ownership: "external", Tags: []string{}}
	f.vm.Fingerprint = fingerprint(f.vm)
	f.secureXML = wanted
	return f
}
func (f *coldRestoreFixture) event(name string, count int) error {
	f.calls = append(f.calls, name)
	if f.fault != nil {
		return f.fault(name, count)
	}
	return nil
}
func (f *coldRestoreFixture) open(uri string) (coldRestoreSession, error) {
	if uri != "qemu:///system" {
		f.t.Fatal("unreviewed URI", uri)
	}
	if err := f.event("open", 0); err != nil {
		return nil, err
	}
	return f, nil
}
func (f *coldRestoreFixture) absent(id, name string) error {
	if id != f.def.UUID || name != f.def.Name {
		f.t.Fatal("wrong absent identity")
	}
	return f.event("absent", len(f.calls))
}
func (f *coldRestoreFixture) volume(_ context.Context, volume domain.CreatedVolume, except string) error {
	f.volumes = append(f.volumes, volume)
	f.exceptions = append(f.exceptions, except)
	return f.event("volume", len(f.volumes))
}
func (f *coldRestoreFixture) lookup(id string) (bool, error) {
	if id != f.def.UUID {
		f.t.Fatal("wrong lookup identity")
	}
	return f.found, f.event("lookup", 0)
}
func (f *coldRestoreFixture) define(raw string) error {
	f.defines++
	if raw != f.vm.PersistentXML {
		f.t.Fatal("defined XML is not exact expected patch", raw)
	}
	return f.event("define", f.defines)
}
func (f *coldRestoreFixture) observe() (domain.VM, error) {
	f.observeCalls++
	vm := f.vm
	if f.onObserve != nil {
		f.onObserve(f.observeCalls, &vm)
	}
	return vm, f.event("observe", f.observeCalls)
}
func (f *coldRestoreFixture) secure() (string, error) {
	f.secureCalls++
	if f.onSecure != nil {
		f.onSecure(f.secureCalls)
	}
	return f.secureXML, f.event("secure", f.secureCalls)
}
func (f *coldRestoreFixture) close() error {
	f.closed++
	if f.onClose != nil {
		f.onClose()
	}
	return f.event("close", f.closed)
}
func coldRestoreError(t *testing.T, vm domain.VM, matched bool, err error, code string) {
	t.Helper()
	var typed *domain.Error
	if err == nil || !reflect.DeepEqual(vm, domain.VM{}) || matched || !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("got VM=%#v matched=%t err=%v, want zero/%s", vm, matched, err, code)
	}
	if strings.Contains(err.Error(), "OPAQUE-SENSITIVE") {
		t.Fatal("native diagnostic leaked", err)
	}
}

func TestColdRestoreNativeSequenceAndExactVolumeReceipts(t *testing.T) {
	f := newColdRestoreFixture(t)
	vm, ok, err := coldRestoreRun(context.Background(), "qemu:///system", f.def, true, f.open)
	if err != nil || !ok || vm.Fingerprint != f.vm.Fingerprint || f.defines != 1 || f.closed != 1 {
		t.Fatal(vm, ok, err, f.calls)
	}
	wantCalls := []string{"open", "absent", "volume", "absent", "volume", "define", "observe", "secure", "volume", "observe", "secure", "observe", "close"}
	if !reflect.DeepEqual(f.calls, wantCalls) || !reflect.DeepEqual(f.exceptions, []string{"", "", f.def.UUID}) {
		t.Fatal(f.calls, f.exceptions)
	}
	for _, got := range f.volumes {
		if !reflect.DeepEqual(got, f.def.Disks[0].Volume) {
			t.Fatal("volume receipt weakened", got)
		}
	}
	if strings.Contains(vm.PersistentXML, "<interface") || !strings.Contains(vm.PersistentXML, "<x:policy exact='keep'> retained opaque text </x:policy>") {
		t.Fatal("restore policy or preservation differs")
	}
}

func TestColdRestoreChecksEveryMultiDiskVolumeBeforeDefinition(t *testing.T) {
	for _, failSecond := range []bool{false, true} {
		f := newColdRestoreFixture(t)
		f.def.SourceXML = strings.Replace(f.def.SourceXML, "captured-cold", "Virmill BIOS multi-disk probe", 1)
		f.def.SourceXML = strings.Replace(f.def.SourceXML, "</devices>", `<disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='/original/data.qcow2'/><target dev='vdb' bus='virtio'/></disk></devices>`, 1)
		second := f.def.Disks[0]
		second.Target = "vdb"
		second.Volume.Intent.SourceID = "vdb"
		second.Volume.Intent.Name = "virmill-" + restoreTestID + "-disk-001.qcow2"
		second.Volume.Path, second.Volume.BackendKey = "/staged/data.qcow2", "/staged/data.qcow2"
		second.Volume.Generation = "linux-statx-v1:1:2:4:1700000000:000000001"
		f.def.Disks = append(f.def.Disks, second)
		wanted, err := coldRestoreXML(f.def)
		if err != nil {
			t.Fatal(err)
		}
		f.vm.PersistentXML, f.secureXML = wanted, wanted
		f.vm.Fingerprint = fingerprint(f.vm)
		if failSecond {
			f.fault = func(name string, count int) error {
				if name == "volume" && count == 4 {
					return errors.New("second generation changed")
				}
				return nil
			}
		}
		vm, ok, err := coldRestoreRun(context.Background(), "qemu:///system", f.def, true, f.open)
		if failSecond {
			coldRestoreError(t, vm, ok, err, "SOURCE_CHANGED")
			if f.defines != 0 {
				t.Fatal("late volume mismatch reached native define")
			}
		} else if err != nil || !ok || f.defines != 1 || len(f.volumes) != 6 {
			t.Fatal(vm, ok, err, f.calls)
		}
		for i, volume := range f.volumes {
			if !reflect.DeepEqual(volume, f.def.Disks[i%2].Volume) {
				t.Fatal("volume omitted or reordered", i, volume)
			}
		}
	}
}

func TestObserveColdRestoreNeverDefinesOrReplays(t *testing.T) {
	for _, present := range []bool{false, true} {
		f := newColdRestoreFixture(t)
		f.found = present
		vm, matched, err := coldRestoreRun(context.Background(), "qemu:///system", f.def, false, f.open)
		if err != nil || matched != present || f.defines != 0 || f.closed != 1 {
			t.Fatal(vm, matched, err, f.calls)
		}
		if !present && (!reflect.DeepEqual(vm, domain.VM{}) || len(f.volumes) != 0) {
			t.Fatal("absent VM claimed volume validation", vm, f.volumes)
		}
	}
}

func TestColdRestoreRejectsProfileAndVolumeBindingBeforeNativeOpen(t *testing.T) {
	for name, mutate := range map[string]func(*domain.ColdRestoreDefinition){
		"NICs connected":      func(d *domain.ColdRestoreDefinition) { d.DisconnectNICs = false },
		"auxiliary nvram":     func(d *domain.ColdRestoreDefinition) { d.NVRAMPath = "/new/nvram" },
		"auxiliary TPM":       func(d *domain.ColdRestoreDefinition) { d.TPMPath = "/new/tpm" },
		"missing generation":  func(d *domain.ColdRestoreDefinition) { d.Disks[0].Volume.Generation = "" },
		"missing backend key": func(d *domain.ColdRestoreDefinition) { d.Disks[0].Volume.BackendKey = "" },
		"old volume name": func(d *domain.ColdRestoreDefinition) {
			d.Disks[0].Volume.Intent.Name = "virmill-12345678-1234-4234-8234-123456789abc-disk-000.qcow2"
		},
		"format mismatch":     func(d *domain.ColdRestoreDefinition) { d.Disks[0].Format = "raw" },
		"missing source hash": func(d *domain.ColdRestoreDefinition) { d.Disks[0].Volume.Intent.SHA256 = "" },
		"source path reused":  func(d *domain.ColdRestoreDefinition) { d.Disks[0].Volume.Path = "/original/root.qcow2" },
		"extra volume":        func(d *domain.ColdRestoreDefinition) { d.Disks = append(d.Disks, d.Disks[0]) },
		"missing mapped disk": func(d *domain.ColdRestoreDefinition) { d.Disks = nil },
	} {
		t.Run(name, func(t *testing.T) {
			f := newColdRestoreFixture(t)
			mutate(&f.def)
			vm, ok, err := coldRestoreRun(context.Background(), "qemu:///system", f.def, true, f.open)
			if err == nil || ok || !reflect.DeepEqual(vm, domain.VM{}) || len(f.calls) != 0 {
				t.Fatal(vm, ok, err, f.calls)
			}
		})
	}
	f := newColdRestoreFixture(t)
	vm, ok, err := coldRestoreRun(context.Background(), "test:///default", f.def, true, f.open)
	coldRestoreError(t, vm, ok, err, "UNSUPPORTED_CAPABILITY")
	if len(f.calls) != 0 {
		t.Fatal("unsupported URI opened native connection")
	}
}

func TestColdRestoreNativeFaultsAreSanitizedAndNeverRetried(t *testing.T) {
	for _, phase := range []string{"open", "absent", "volume-before", "define", "observe", "secure", "volume-after", "close"} {
		t.Run(phase, func(t *testing.T) {
			f := newColdRestoreFixture(t)
			f.fault = func(name string, count int) error {
				if name == phase || phase == "volume-before" && name == "volume" && count == 2 || phase == "volume-after" && name == "volume" && count == 3 {
					return errors.New("OPAQUE-SENSITIVE native error")
				}
				return nil
			}
			vm, ok, err := coldRestoreRun(context.Background(), "qemu:///system", f.def, true, f.open)
			code := "RECOVERY_REQUIRED"
			if phase == "open" {
				code = "PERMISSION_DENIED"
			}
			if phase == "absent" {
				code = "STALE_PLAN"
			}
			if phase == "volume-before" {
				code = "SOURCE_CHANGED"
			}
			coldRestoreError(t, vm, ok, err, code)
			if f.defines > 1 || phase != "open" && f.closed != 1 {
				t.Fatal("retry or leaked native session", f.calls)
			}
		})
	}
}

func TestColdRestoreReadbackPreservesOpaqueSemanticsAndStoppedState(t *testing.T) {
	for name, change := range map[string]func(*domain.VM){
		"opaque dropped": func(v *domain.VM) {
			v.PersistentXML = strings.Replace(v.PersistentXML, "<x:policy exact='keep'> retained opaque text </x:policy>", "", 1)
		},
		"unknown attribute": func(v *domain.VM) {
			v.PersistentXML = strings.Replace(v.PersistentXML, "exact='keep'", "exact='changed'", 1)
		},
		"balloon changed": func(v *domain.VM) {
			v.PersistentXML = strings.Replace(v.PersistentXML, "model='none'", "model='virtio'", 1)
		},
		"NIC reintroduced": func(v *domain.VM) {
			v.PersistentXML = strings.Replace(v.PersistentXML, "</devices>", "<interface type='network'/></devices>", 1)
		},
		"running":        func(v *domain.VM) { v.State = "running" },
		"autostart":      func(v *domain.VM) { v.Autostart = true },
		"saved state":    func(v *domain.VM) { v.HasManagedSave = true },
		"wrong resource": func(v *domain.VM) { v.Key.UUID = "11111111-2222-4333-8444-555555555555" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newColdRestoreFixture(t)
			f.onObserve = func(_ int, v *domain.VM) { change(v); v.Fingerprint = fingerprint(*v) }
			vm, ok, err := coldRestoreRun(context.Background(), "qemu:///system", f.def, false, f.open)
			coldRestoreError(t, vm, ok, err, "SOURCE_CHANGED")
			if f.defines != 0 {
				t.Fatal("observe mutated native VM")
			}
		})
	}
	t.Run("representation only", func(t *testing.T) {
		f := newColdRestoreFixture(t)
		f.vm.PersistentXML = strings.Replace(f.vm.PersistentXML, "<memballoon model='none'/>", `<memballoon model="none"></memballoon>`, 1)
		f.vm.Fingerprint = fingerprint(f.vm)
		f.secureXML = f.vm.PersistentXML
		_, ok, err := coldRestoreRun(context.Background(), "qemu:///system", f.def, false, f.open)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
	})
}

func TestColdRestoreCancellationAndFinalSecureReadDrift(t *testing.T) {
	for _, phase := range []string{"before", "pre-define", "define", "last-secure", "close"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := newColdRestoreFixture(t)
			if phase == "before" {
				cancel()
			}
			f.fault = func(name string, count int) error {
				if phase == "pre-define" && name == "volume" && count == 2 || phase == "define" && name == "define" || phase == "last-secure" && name == "secure" && count == 2 {
					cancel()
				}
				return nil
			}
			if phase == "close" {
				f.onClose = cancel
			}
			vm, ok, err := coldRestoreRun(ctx, "qemu:///system", f.def, true, f.open)
			if phase == "before" || phase == "pre-define" {
				if !errors.Is(err, context.Canceled) || ok || !reflect.DeepEqual(vm, domain.VM{}) || f.defines != 0 {
					t.Fatal(vm, ok, err)
				}
			} else {
				coldRestoreError(t, vm, ok, err, "RECOVERY_REQUIRED")
			}
		})
	}
	f := newColdRestoreFixture(t)
	f.onSecure = func(call int) {
		if call == 2 {
			f.vm.State = "running"
			f.vm.Fingerprint = fingerprint(f.vm)
		}
	}
	vm, ok, err := coldRestoreRun(context.Background(), "qemu:///system", f.def, false, f.open)
	coldRestoreError(t, vm, ok, err, "SOURCE_CHANGED")
}

func TestCheckColdConfigurationExactFingerprintAndSecretRedaction(t *testing.T) {
	for _, scenario := range []string{"good", "redacted", "native secret failure", "stale", "running", "autostart", "save", "final drift", "late cancel", "absent"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f := newColdRestoreFixture(t)
			reviewed := f.vm.Fingerprint
			switch scenario {
			case "redacted":
				f.secureXML = strings.Replace(f.secureXML, "<metadata>", "<metadata><secret>OPAQUE-SENSITIVE</secret>", 1)
			case "native secret failure":
				f.fault = func(name string, _ int) error {
					if name == "secure" {
						return errors.New("OPAQUE-SENSITIVE")
					}
					return nil
				}
			case "stale":
				reviewed = strings.Repeat("b", 64)
			case "running":
				f.vm.State = "running"
			case "autostart":
				f.vm.Autostart = true
			case "save":
				f.vm.HasManagedSave = true
			case "final drift":
				f.onSecure = func(call int) {
					if call == 2 {
						f.vm.Autostart = true
						f.vm.Fingerprint = fingerprint(f.vm)
					}
				}
			case "late cancel":
				f.onClose = cancel
			case "absent":
				f.found = false
			}
			f.vm.Fingerprint = fingerprint(f.vm)
			err := checkColdConfigurationWith(ctx, "qemu:///system", f.def.UUID, reviewed, f.open)
			if scenario == "good" {
				if err != nil || f.observeCalls != 3 || f.secureCalls != 2 {
					t.Fatal(err, f.calls)
				}
			} else if err == nil || strings.Contains(err.Error(), "OPAQUE-SENSITIVE") {
				t.Fatal("missing/sensitive failure", err)
			}
			if scenario == "late cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if f.defines != 0 || len(f.volumes) != 0 || f.closed != 1 {
				t.Fatal("cold configuration check mutated or leaked", f.calls)
			}
		})
	}
}
