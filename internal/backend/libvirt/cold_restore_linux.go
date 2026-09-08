//go:build linux && cgo

package libvirt

import (
	"context"
	"errors"
	"strings"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
)

var _ domain.ColdRestoreBackend = (*Provider)(nil)
var _ domain.ColdCaptureBackend = (*Provider)(nil)

// A session owns all native handles. Tests replace this boundary without
// opening libvirt, reading existing disk bytes or defining a guest.
type coldRestoreSession interface {
	absent(string, string) error
	volume(context.Context, domain.CreatedVolume, string) error
	lookup(string) (bool, error)
	define(string) error
	observe() (domain.VM, error)
	secure() (string, error)
	close() error
}
type coldRestoreOpen func(string) (coldRestoreSession, error)

func (p *Provider) DefineRestoredVM(ctx context.Context, uri string, def domain.ColdRestoreDefinition) (domain.VM, error) {
	vm, _, err := coldRestoreRun(ctx, uri, def, true, openColdRestore)
	return vm, err
}
func (p *Provider) ObserveRestoredVM(ctx context.Context, uri string, def domain.ColdRestoreDefinition) (domain.VM, bool, error) {
	return coldRestoreRun(ctx, uri, def, false, openColdRestore)
}

func coldRestoreXML(def domain.ColdRestoreDefinition) (string, error) {
	if !def.DisconnectNICs || def.NVRAMPath != "" || def.TPMPath != "" {
		return "", domain.Fail("UNSUPPORTED_CAPABILITY", "initial cold restore requires disconnected NICs and no auxiliary target paths; qualified auxiliary staging is unavailable")
	}
	if len(def.Disks) > 256 || len(def.SourceXML) > coldStateXMLLimit {
		return "", domain.Fail("INVALID_INPUT", "cold restore source or disk count exceeds capture bounds")
	}
	patch := xmlpatch.ColdRestorePatch{UUID: def.UUID, Name: def.Name, DisconnectNICs: true}
	keys, identities, generations := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, disk := range def.Disks {
		v := disk.Volume
		if validateVolume(v.Intent) != nil || v.Intent.PoolID == "00000000-0000-0000-0000-000000000000" || !strings.HasPrefix(v.Intent.Name, "virmill-"+def.UUID+"-") || v.BackendKey == "" || len(v.BackendKey) > 4096 || len(v.Generation) > 256 || !strings.HasPrefix(v.Generation, "linux-statx-v1:") {
			return "", domain.Fail("INVALID_INPUT", "restored disk requires an exact new managed-volume intent, backend key and generation")
		}
		format := "qcow2"
		if v.Intent.ContentType == "cdrom-iso" {
			format = "raw"
		}
		if disk.Format != format {
			return "", domain.Fail("UNSUPPORTED_CAPABILITY", "restored disk format differs from its supported staged volume content type")
		}
		identity := v.Intent.PoolID + "/" + v.Intent.Name
		if keys[v.BackendKey] || identities[identity] || generations[v.Generation] {
			return "", domain.Fail("INVALID_INPUT", "restored volumes alias one backend key, identity or generation")
		}
		keys[v.BackendKey], identities[identity], generations[v.Generation] = true, true, true
		patch.Disks = append(patch.Disks, xmlpatch.ColdRestoreDisk{Target: disk.Target, Path: v.Path, Format: disk.Format})
	}
	return xmlpatch.ColdRestore(def.SourceXML, patch)
}

func coldRestoreUncertain() error {
	return domain.Fail("RECOVERY_REQUIRED", "restored definition outcome is uncertain; inspect its original new UUID without redefining, starting or removing it")
}

// DefineXML lacks a create-only compare-and-swap flag. Repeated UUID/name
// absence and volume observations cannot exclude an external privileged writer;
// callers must coordinate that writer and retain their original durable intent.
// Context checks surround synchronous C calls; they cannot interrupt C safely.
func coldRestoreRun(ctx context.Context, uri string, def domain.ColdRestoreDefinition, define bool, open coldRestoreOpen) (vm domain.VM, matched bool, err error) {
	if err = ctx.Err(); err != nil {
		return vm, false, err
	}
	if err = Connection(uri); err != nil {
		return vm, false, err
	}
	wanted, err := coldRestoreXML(def)
	if err != nil {
		return vm, false, err
	}
	s, err := open(uri)
	if err != nil {
		return vm, false, domain.Fail("PERMISSION_DENIED", "native cold restore connection unavailable; details withheld")
	}
	submitted := false
	defer func() {
		if cleanup := s.close(); cleanup != nil {
			err = domain.Fail("OPERATION_FAILED", "native cold restore handle cleanup failed; details withheld")
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			vm, matched = domain.VM{}, false
			if submitted {
				err = coldRestoreUncertain()
			}
		}
	}()
	volumes := func(except string) error {
		for _, disk := range def.Disks {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := s.volume(ctx, disk.Volume, except); err != nil {
				return domain.Fail("SOURCE_CHANGED", "staged volume identity, metadata or attachment changed; native details withheld")
			}
		}
		return ctx.Err()
	}
	if define {
		for pass := 0; pass < 2; pass++ {
			if err := ctx.Err(); err != nil {
				return vm, false, err
			}
			if err := s.absent(def.UUID, def.Name); err != nil {
				return vm, false, domain.Fail("STALE_PLAN", "restored UUID/name is not proven absent; no existing definition overwritten")
			}
			if err := volumes(""); err != nil {
				return vm, false, err
			}
		}
		if err := ctx.Err(); err != nil {
			return vm, false, err
		}
		submitted = true
		if err := s.define(wanted); err != nil {
			return vm, false, coldRestoreUncertain()
		}
	} else {
		found, err := s.lookup(def.UUID)
		if err != nil {
			return vm, false, domain.Fail("OPERATION_FAILED", "native restored VM lookup failed; details withheld")
		}
		if !found {
			return vm, false, nil
		}
	}
	first, err := coldRestoredObservation(ctx, s, uri, def, wanted)
	if err != nil {
		return vm, false, err
	}
	if err := volumes(def.UUID); err != nil {
		return vm, false, err
	}
	latest, err := coldRestoredObservation(ctx, s, uri, def, wanted)
	if err != nil {
		return vm, false, err
	}
	if first.Fingerprint != latest.Fingerprint {
		return vm, false, domain.Fail("SOURCE_CHANGED", "restored VM changed during observation")
	}
	final, err := s.observe()
	if err != nil {
		return vm, false, domain.Fail("OPERATION_FAILED", "final restored VM observation failed; details withheld")
	}
	if err := coldStoppedVM(final, uri, def.UUID); err != nil {
		return vm, false, err
	}
	if final.Fingerprint != latest.Fingerprint {
		return vm, false, domain.Fail("SOURCE_CHANGED", "restored VM changed after secure observation")
	}
	return final, true, ctx.Err()
}

func coldStoppedVM(vm domain.VM, uri, id string) error {
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}
	if vm.Key != key || vm.State != "stopped" || vm.PersistentXML == "" || len(vm.PersistentXML) > coldStateXMLLimit || vm.LiveXML != "" || vm.HasManagedSave || vm.Autostart || !digestPattern.MatchString(vm.Fingerprint) || vm.Fingerprint != fingerprint(vm) {
		return domain.Fail("SOURCE_CHANGED", "cold VM identity, stopped state, persistence, autostart, saved state or fingerprint differs")
	}
	return nil
}

func coldRestoredObservation(ctx context.Context, s coldRestoreSession, uri string, def domain.ColdRestoreDefinition, wanted string) (domain.VM, error) {
	var empty domain.VM
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	vm, err := s.observe()
	if err != nil {
		return empty, domain.Fail("OPERATION_FAILED", "native restored VM observation failed; details withheld")
	}
	if err := coldStoppedVM(vm, uri, def.UUID); err != nil {
		return empty, err
	}
	if vm.Name != def.Name {
		return empty, domain.Fail("SOURCE_CHANGED", "restored VM name differs")
	}
	// HardwareDigest retains opaque nodes, namespaces, text, attributes and
	// comments. It permits only its existing documented representation changes;
	// no creation-default allowlist or dropped-node projection is used here.
	want, err := xmlpatch.HardwareDigest(wanted)
	if err != nil {
		return empty, domain.Fail("INVALID_INPUT", "invalid expected restored XML")
	}
	got, err := xmlpatch.HardwareDigest(vm.PersistentXML)
	if err != nil || got != want {
		return empty, domain.Fail("SOURCE_CHANGED", "restored XML differs beyond the exact reviewed transformation")
	}
	secure, err := s.secure()
	if err != nil {
		return empty, domain.Fail("PERMISSION_DENIED", "secure restored XML readback unavailable; details withheld")
	}
	if secure != vm.PersistentXML {
		return empty, domain.Fail("UNSUPPORTED_CAPABILITY", "restored public XML omits sensitive settings; no preservation claim")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return vm, nil
}

func (p *Provider) CheckColdConfiguration(ctx context.Context, uri, id, reviewed string) error {
	return checkColdConfigurationWith(ctx, uri, id, reviewed, openColdRestore)
}

func checkColdConfigurationWith(ctx context.Context, uri, id, reviewed string, open coldRestoreOpen) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = Connection(uri); err != nil {
		return err
	}
	if !uuidPattern.MatchString(id) || id == "00000000-0000-0000-0000-000000000000" || !digestPattern.MatchString(reviewed) {
		return domain.Fail("INVALID_INPUT", "canonical VM UUID and reviewed fingerprint required")
	}
	s, err := open(uri)
	if err != nil {
		return domain.Fail("PERMISSION_DENIED", "write-authorized native connection required for secure XML comparison; details withheld")
	}
	defer func() {
		if e := s.close(); e != nil {
			err = domain.Fail("OPERATION_FAILED", "cold configuration handle cleanup failed; details withheld")
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	found, err := s.lookup(id)
	if err != nil || !found {
		return domain.Fail("SOURCE_CHANGED", "selected cold VM lookup failed or is absent; details withheld")
	}
	for pass := 0; pass < 2; pass++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		vm, err := s.observe()
		if err != nil {
			return domain.Fail("OPERATION_FAILED", "cold VM observation failed; details withheld")
		}
		if err := coldStoppedVM(vm, uri, id); err != nil {
			return err
		}
		if vm.Fingerprint != reviewed {
			return domain.Fail("STALE_PLAN", "cold VM fingerprint changed")
		}
		secure, err := s.secure()
		if err != nil {
			return domain.Fail("PERMISSION_DENIED", "secure cold XML observation unavailable; details withheld")
		}
		if vm.PersistentXML != secure {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "public inactive XML omits sensitive settings; capture cannot preserve it")
		}
	}
	// Observe once after the last secure read as well, so state transitions at
	// that boundary cannot be hidden by the preceding public fingerprint.
	vm, err := s.observe()
	if err != nil {
		return domain.Fail("OPERATION_FAILED", "final cold VM observation failed; details withheld")
	}
	if err := coldStoppedVM(vm, uri, id); err != nil {
		return err
	}
	if vm.Fingerprint != reviewed {
		return domain.Fail("STALE_PLAN", "cold VM changed during secure XML comparison")
	}
	return ctx.Err()
}

type nativeColdRestore struct {
	c   *native.Connect
	d   *native.Domain
	uri string
}

func openColdRestore(uri string) (coldRestoreSession, error) {
	c, err := connect(uri, true) // Secure XML requires this even for read-only checks.
	if err != nil {
		return nil, err
	}
	return &nativeColdRestore{c: c, uri: uri}, nil
}
func (s *nativeColdRestore) absent(id, name string) error { return absentDomain(s.c, id, name) }
func (s *nativeColdRestore) lookup(id string) (bool, error) {
	d, err := s.c.LookupDomainByUUIDString(id)
	if err != nil {
		var nativeErr native.Error
		if errors.As(err, &nativeErr) && nativeErr.Code == native.ERR_NO_DOMAIN {
			return false, nil
		}
		return false, err
	}
	s.d = d
	return true, nil
}
func (s *nativeColdRestore) define(raw string) error {
	d, err := s.c.DomainDefineXMLFlags(raw, native.DOMAIN_DEFINE_VALIDATE)
	s.d = d
	return err
}
func (s *nativeColdRestore) observe() (domain.VM, error) { return observe(s.d, s.uri) }
func (s *nativeColdRestore) secure() (string, error) {
	return s.d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
}
func (s *nativeColdRestore) close() error {
	var err error
	if s.d != nil {
		err = s.d.Free()
	}
	_, e := s.c.Close()
	return errors.Join(err, e)
}
func (s *nativeColdRestore) volume(ctx context.Context, v domain.CreatedVolume, except string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	disk, err := lookupCreated(s.c, v)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, disk.Free()) }()
	xml, err := disk.GetXMLDesc(0)
	if err != nil {
		return err
	}
	if err := verifyVolumeMetadata(xml, v.Intent); err != nil {
		return err
	}
	identity, err := fileidentity.Observe(v.Path, false)
	if err != nil {
		return err
	}
	if identity.Generation != v.Generation || identity.Size != v.Intent.FileBytes {
		return domain.Fail("SOURCE_CHANGED", "staged file generation or size differs")
	}
	if err := requireUnattachedExcept(s.c, v, except); err != nil {
		return err
	}
	return ctx.Err()
}
