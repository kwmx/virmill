//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	native "libvirt.org/go/libvirt"
	"os"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/backend/image"
	"virmill.local/core/internal/domain"
)

func acceptanceProfile(t domain.CreationTarget, policy *domain.CreationDevicePolicy) error {
	if policy == nil {
		return domain.Fail("INVALID_INPUT", "explicit complete device policy required for acceptance")
	}
	if err := policy.Validate(t.Spec.Machine); err != nil {
		return err
	}
	if t.Spec.Firmware.Mode != "bios" || t.Spec.Firmware.TPM {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "acceptance of firmware/TPM auxiliary state requires its dedicated recovery adapter")
	}
	return nil
}

func acceptanceDomain(c *native.Connect, uri string, t domain.CreationTarget, policy *domain.CreationDevicePolicy, volumes []domain.CreatedVolume, binding string) (domain.VM, error) {
	var empty domain.VM
	d, err := c.LookupDomainByUUIDString(t.Spec.UUID)
	if err != nil {
		return empty, err
	}
	defer d.Free()
	v, err := observe(d, uri)
	if err != nil {
		return v, err
	}
	if v.State != "stopped" || v.PersistentXML == "" || v.HasManagedSave || v.Autostart {
		return v, domain.Fail("RESOURCE_BUSY", "acceptance requires a stopped persistent VM without managed-save state or autostart")
	}
	snapshots, err := d.SnapshotNum(0)
	if err != nil {
		return v, err
	}
	checkpoints, err := d.ListAllCheckpoints(0)
	if err != nil {
		return v, err
	}
	count := len(checkpoints)
	for i := range checkpoints {
		checkpoints[i].Free()
	}
	if snapshots != 0 || count != 0 {
		return v, domain.Fail("UNSUPPORTED_CAPABILITY", "existing snapshots/checkpoints need a separate recovery review")
	}
	secure, err := d.GetXMLDesc(native.DOMAIN_XML_INACTIVE | native.DOMAIN_XML_SECURE)
	if err != nil {
		return v, domain.Fail("PERMISSION_DENIED", "secure XML observation required for acceptance")
	}
	if err = checkSecureConfiguration(v.PersistentXML, secure); err != nil {
		return v, err
	}
	t.Spec.DevicePolicy = policy
	wanted, err := creationXML(t, volumes, binding)
	if err != nil {
		return v, err
	}
	if err = matchesCreationPolicy(wanted, secure, policy); err != nil {
		return v, err
	}
	return v, nil
}

func (p *Provider) InspectCreationAcceptance(ctx context.Context, uri string, original domain.CreationTarget, policy *domain.CreationDevicePolicy, volumes []domain.CreatedVolume, binding string) (domain.CreationAcceptanceObservation, error) {
	var out domain.CreationAcceptanceObservation
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := acceptanceProfile(original, policy); err != nil {
		return out, err
	}
	if len(volumes) == 0 || len(volumes) != len(original.Spec.Disks)+len(original.Spec.Media) {
		return out, domain.Fail("RECOVERY_REQUIRED", "complete retained volume set required for acceptance")
	}
	c, err := connect(uri, true)
	if err != nil {
		return out, err
	}
	defer c.Close()
	v, err := acceptanceDomain(c, uri, original, policy, volumes, binding)
	if err != nil {
		return out, err
	}
	// The original environment must still match. A new device probe digest is
	// additionally pinned because the explicitly selected devices can differ.
	current, err := p.preflightCreation(ctx, uri, original.Spec, true)
	if err != nil {
		return out, err
	}
	if inventoryDigest(current) != inventoryDigest(original) {
		return out, domain.Fail("STALE_PLAN", "original creation environment changed; device acceptance cannot substitute it")
	}
	selected := original.Spec
	selected.DevicePolicy = policy
	current, err = p.preflightCreation(ctx, uri, selected, true)
	if err != nil {
		return out, err
	}
	out.TargetFingerprint = inventoryDigest(current)
	for _, volume := range volumes {
		if err = requireUnattachedExcept(c, volume, original.Spec.UUID); err != nil {
			return out, err
		}
		if volume.Generation == "" {
			return out, domain.Fail("RECOVERY_REQUIRED", "acceptance requires every retained file generation")
		}
		disk, e := lookupCreated(c, volume)
		if e != nil {
			return out, e
		}
		x, e := disk.GetXMLDesc(0)
		disk.Free()
		if e != nil {
			return out, e
		}
		if e = verifyVolumeMetadata(x, volume.Intent); e != nil {
			return out, e
		}
		f, identity, e := fileidentity.Open(volume.Path, false, true)
		if e != nil {
			return out, domain.Fail("PERMISSION_DENIED", "retained file requires ordinary-user read access for guarded acceptance")
		}
		guard, e := image.AcquireReadGuard(f)
		f.Close()
		if e != nil {
			return out, e
		}
		guard.Close()
		if identity.Generation != volume.Generation || identity.Size != volume.Intent.FileBytes {
			return out, domain.Fail("SOURCE_CHANGED", "retained file generation or size changed")
		}
		out.VolumeFingerprints = append(out.VolumeFingerprints, inventoryDigest(identity))
	}
	latest, err := acceptanceDomain(c, uri, original, policy, volumes, binding)
	if err != nil {
		return out, err
	}
	if latest.Fingerprint != v.Fingerprint {
		return out, domain.Fail("STALE_PLAN", "VM changed during acceptance observation")
	}
	out.VMFingerprint = v.Fingerprint
	sum := sha256.Sum256([]byte(v.PersistentXML))
	out.PersistentXMLSHA256 = hex.EncodeToString(sum[:])
	return out, nil
}

// guardedAcceptanceRead retains every guard until all hashes and final native
// observations complete. This is a cooperative QEMU lock, not an assertion that
// arbitrary external writers honor it. No image parser runs in this process.
func guardedAcceptanceRead(ctx context.Context, volumes []domain.CreatedVolume, expected domain.CreationAcceptanceObservation, observeAgain func() error) error {
	if len(volumes) == 0 || len(volumes) != len(expected.VolumeFingerprints) {
		return domain.Fail("RECOVERY_REQUIRED", "incomplete acceptance observation")
	}
	files := []*os.File{}
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	for i, v := range volumes {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, identity, err := fileidentity.Open(v.Path, false, true)
		if err != nil {
			return domain.Fail("PERMISSION_DENIED", "retained file cannot be opened read-only for guarded acceptance")
		}
		guard, err := image.AcquireReadGuard(f)
		f.Close()
		if err != nil {
			return err
		}
		files = append(files, guard)
		if v.Generation == "" || identity.Generation != v.Generation || inventoryDigest(identity) != expected.VolumeFingerprints[i] {
			return domain.Fail("SOURCE_CHANGED", "retained file changed before acceptance readback")
		}
	}
	if err := observeAgain(); err != nil {
		return err
	}
	buffer := make([]byte, 1<<20)
	for i, f := range files {
		h := sha256.New()
		var size uint64
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, err := f.Read(buffer)
			size += uint64(n)
			if size > volumes[i].Intent.FileBytes {
				return domain.Fail("SOURCE_CHANGED", "retained file exceeds reviewed length")
			}
			h.Write(buffer[:n])
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
		}
		if size != volumes[i].Intent.FileBytes || hex.EncodeToString(h.Sum(nil)) != volumes[i].Intent.SHA256 {
			return domain.Fail("RECOVERY_REQUIRED", "retained bytes differ from the complete original creation receipt")
		}
		identity, err := fileidentity.InspectFile(f, false)
		if err != nil {
			return err
		}
		if inventoryDigest(identity) != expected.VolumeFingerprints[i] {
			return domain.Fail("SOURCE_CHANGED", "retained file changed during acceptance readback")
		}
	}
	return observeAgain()
}

func (p *Provider) VerifyCreationAcceptance(ctx context.Context, uri string, t domain.CreationTarget, policy *domain.CreationDevicePolicy, volumes []domain.CreatedVolume, binding string, expected domain.CreationAcceptanceObservation) error {
	check := func() error {
		got, err := p.InspectCreationAcceptance(ctx, uri, t, policy, volumes, binding)
		if err != nil {
			return err
		}
		if inventoryDigest(got) != inventoryDigest(expected) {
			return domain.Fail("STALE_PLAN", "acceptance observation changed; retained locks require fresh review")
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	return guardedAcceptanceRead(ctx, volumes, expected, check)
}

var _ domain.CreationAcceptanceBackend = (*Provider)(nil)
