//go:build linux && amd64 && cgo

package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"golang.org/x/sys/unix"
	native "libvirt.org/go/libvirt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/domain"
)

func TestAcceptanceNativeInMemoryStateAndExactDeviceBoundary(t *testing.T) {
	target, volumes, captured := observedQ35Creation(t)
	policy, err := domain.DefaultCreationDevices(target.Spec.Machine)
	if err != nil {
		t.Fatal(err)
	}
	policy.USBController, policy.MemoryBalloon, policy.WatchdogAction = "qemu-xhci", "virtio", "reset"
	const binding = "c1055f98ce07fbb79ce66febc46b9439fb0edd4d7004b00206e3ec6ee1509826"
	for _, mode := range []string{"stopped", "running", "autostart", "snapshot", "wrong-policy", "changed-disk", "changed-metadata"} {
		t.Run(mode, func(t *testing.T) {
			c, err := native.NewConnect("test:///default")
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			x := captured
			if mode == "changed-disk" {
				x = strings.Replace(x, "<boot order='1'", "<boot order='2'", 1)
			}
			if mode == "changed-metadata" {
				x = strings.Replace(x, "</metadata>", "<unreviewed xmlns='urn:other'/></metadata>", 1)
			}
			d, err := c.DomainDefineXMLFlags(x, native.DOMAIN_DEFINE_VALIDATE)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Free()
			selected := *policy
			switch mode {
			case "running":
				if err = d.Create(); err != nil {
					t.Fatal(err)
				}
			case "autostart":
				if err = d.SetAutostart(true); err != nil {
					t.Fatal(err)
				}
			case "wrong-policy":
				selected.WatchdogAction = "none"
			case "snapshot":
				snap, err := d.CreateSnapshotXML("<domainsnapshot><name>retained-state</name></domainsnapshot>", 0)
				if err != nil {
					t.Fatal(err)
				}
				snap.Free()
			}
			_, err = acceptanceDomain(c, "fixture", target, &selected, volumes, binding)
			if (mode == "stopped") != (err == nil) {
				t.Fatal("acceptance state/configuration predicate differs", mode, err)
			}
		})
	}
	if acceptanceProfile(target, nil) == nil {
		t.Fatal("implicit policy accepted")
	}
	for _, mode := range []string{"uefi", "tpm"} {
		changed := target
		if mode == "uefi" {
			changed.Spec.Firmware.Mode = "uefi"
		} else {
			changed.Spec.Firmware.TPM = true
		}
		if acceptanceProfile(changed, policy) == nil {
			t.Fatal("unverified auxiliary state accepted", mode)
		}
	}
	t.Log("native in-memory driver and captured XML only; no VM, storage or hardware effects")
}

func TestAcceptanceReferenceExceptionIsLimitedToIntendedVM(t *testing.T) {
	target, volumes, captured := observedQ35Creation(t)
	volumes[0].Path = "/synthetic/retained.qcow2"
	c, err := native.NewConnect("test:///default")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	d, err := c.DomainDefineXMLFlags(captured, native.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Free()
	if requireUnattached(c, volumes[0]) == nil {
		t.Fatal("ordinary upload/verification lost its no-reference guard")
	}
	if err = requireUnattachedExcept(c, volumes[0], target.Spec.UUID); err != nil {
		t.Fatal(err)
	}
	other := strings.ReplaceAll(captured, target.Spec.UUID, "00000000-0000-4000-8000-000000000099")
	// Keep the exact volume name while changing only the domain identity/name.
	other = strings.ReplaceAll(other, "virmill-00000000-0000-4000-8000-000000000099-disk-000.qcow2", volumes[0].Intent.Name)
	other = strings.Replace(other, target.Spec.Name, "other synthetic reference", 1)
	second, err := c.DomainDefineXMLFlags(other, native.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Free()
	if requireUnattachedExcept(c, volumes[0], target.Spec.UUID) == nil {
		t.Fatal("a second VM reference was silently excepted")
	}
}

func acceptanceReadFixture(t *testing.T) ([]domain.CreatedVolume, domain.CreationAcceptanceObservation) {
	t.Helper()
	dir := t.TempDir()
	var volumes []domain.CreatedVolume
	var observation domain.CreationAcceptanceObservation
	for _, name := range []string{"boot", "data"} {
		data := []byte("generated ordinary bytes, not a guest image: " + name)
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		identity, err := fileidentity.Observe(path, false)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		volumes = append(volumes, domain.CreatedVolume{Path: path, Generation: identity.Generation, Intent: domain.VolumeIntent{FileBytes: uint64(len(data)), SHA256: hex.EncodeToString(sum[:])}})
		observation.VolumeFingerprints = append(observation.VolumeFingerprints, inventoryDigest(identity))
	}
	return volumes, observation
}
func TestAcceptanceReadsCompleteGuardedSetAndRejectsChanges(t *testing.T) {
	for _, mode := range []string{"valid", "changed-bytes", "changed-generation", "symlink", "hardlink", "writer-permission", "canceled", "missing-generation", "incomplete-observation", "final-observation"} {
		t.Run(mode, func(t *testing.T) {
			volumes, observation := acceptanceReadFixture(t)
			ctx := context.Background()
			switch mode {
			case "changed-bytes":
				volumes[0].Intent.SHA256 = strings.Repeat("a", 64)
			case "changed-generation":
				if err := os.Rename(volumes[0].Path, volumes[0].Path+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(volumes[0].Path, []byte("replaced"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(volumes[0].Path, volumes[0].Path+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(volumes[0].Path+".old", volumes[0].Path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(volumes[0].Path, volumes[0].Path+".alias"); err != nil {
					t.Fatal(err)
				}
			case "missing-generation":
				volumes[0].Generation = ""
			case "incomplete-observation":
				observation.VolumeFingerprints = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "writer-permission":
				writer, err := os.OpenFile(volumes[0].Path, os.O_RDWR, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Close()
				lock := unix.Flock_t{Type: unix.F_RDLCK, Start: 101, Len: 1}
				if err = unix.FcntlFlock(writer.Fd(), unix.F_OFD_SETLK, &lock); err != nil {
					t.Fatal(err)
				}
			}
			observed := 0
			check := func() error {
				observed++
				if mode == "final-observation" && observed == 2 {
					return errors.New("synthetic native configuration changed after readback")
				}
				for i, v := range volumes {
					identity, err := fileidentity.Observe(v.Path, false)
					if err != nil {
						return err
					}
					if inventoryDigest(identity) != observation.VolumeFingerprints[i] {
						return errors.New("file changed")
					}
					f, err := os.OpenFile(v.Path, os.O_RDWR, 0)
					if err != nil {
						return err
					}
					lock := unix.Flock_t{Type: unix.F_WRLCK, Start: 201, Len: 3}
					err = unix.FcntlFlock(f.Fd(), unix.F_OFD_GETLK, &lock)
					f.Close()
					if err != nil {
						return err
					}
					if lock.Type == unix.F_UNLCK {
						return errors.New("whole-set guard missing during observation")
					}
				}
				return nil
			}
			err := guardedAcceptanceRead(ctx, volumes, observation, check)
			if (mode == "valid") != (err == nil) {
				t.Fatal(mode, err)
			}
			if mode == "valid" && observed != 2 {
				t.Fatal("native observation was not repeated")
			}
			// Every exit closes guards. No held locks leak into the next attempt.
			for _, v := range volumes {
				f, err := os.Open(v.Path)
				if err != nil {
					continue
				}
				lock := unix.Flock_t{Type: unix.F_WRLCK, Start: 201, Len: 3}
				err = unix.FcntlFlock(f.Fd(), unix.F_OFD_GETLK, &lock)
				f.Close()
				if err != nil || lock.Type != unix.F_UNLCK {
					t.Fatal("read guard leaked", err)
				}
			}
		})
	}
	t.Log("generated file/OFD protocol fixtures only; not hardware or actual QEMU writer qualification")
}
