//go:build linux && amd64

package coldcapture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"virmill.local/core/internal/app/protection"
	"virmill.local/core/internal/backend/coldfiles"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/coldstore"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/helper"
	"virmill.local/core/internal/operations"
	platform "virmill.local/core/internal/platform/linux"
)

type sourceSet struct {
	held  []*coldfiles.Source
	files []*os.File
}

func (s *sourceSet) Close() {
	for _, f := range s.files {
		_ = f.Close()
	}
	for _, f := range s.held {
		_ = f.Close()
	}
}
func (s *sourceSet) Recheck(ctx context.Context) error {
	for _, f := range s.held {
		if err := f.Recheck(ctx); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func (s *sourceSet) Open(ctx context.Context, root string, f sourceFile) (*coldfiles.Source, *os.File, error) {
	rel, err := relative(root, f.Path)
	if err != nil {
		return nil, nil, err
	}
	held, err := coldfiles.Open(ctx, root, rel, f.Identity)
	if err != nil {
		return nil, nil, err
	}
	s.held = append(s.held, held)
	file, err := held.DupForTransfer()
	if err != nil {
		return nil, nil, err
	}
	s.files = append(s.files, file)
	return held, file, nil
}
func hashReader(ctx context.Context, r io.ReaderAt, size int64) (string, error) {
	if size < 0 || size > maximumDisk {
		return "", domain.Fail("INVALID_INPUT", "invalid capture size")
	}
	h := sha256.New()
	buf := make([]byte, 256<<10)
	for off := int64(0); off < size; {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		want := min(int64(len(buf)), size-off)
		n, err := r.ReadAt(buf[:want], off)
		if n != int(want) || err != nil && err != io.EOF {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return "", err
		}
		_, _ = h.Write(buf[:n])
		off += int64(n)
	}
	n, err := r.ReadAt(buf[:1], size)
	if n != 0 || err != io.EOF {
		return "", domain.Fail("SOURCE_CHANGED", "capture member does not end at its declared size")
	}
	return hex.EncodeToString(h.Sum(nil)), ctx.Err()
}
func addMember(ctx context.Context, m *protection.CaptureManifest, sources *[]coldstore.Source, id, kind, path string, r io.ReaderAt, size int64) error {
	hash, err := hashReader(ctx, r, size)
	if err != nil {
		return err
	}
	member := protection.CaptureMember{ID: id, Kind: kind, Path: path, Size: size, SHA256: hash}
	m.Members = append(m.Members, member)
	*sources = append(*sources, coldstore.Source{Member: member, Reader: r})
	return nil
}
func (s *Service) canceled(ctx context.Context) error {
	canceled, err := operations.CancellationRequested(ctx, s.Engine.Store)
	if err != nil {
		return err
	}
	if canceled {
		return domain.Fail("RECOVERY_REQUIRED", "capture canceled; original guest is unchanged and unpublished staging is retained for inspection")
	}
	return ctx.Err()
}
func (s *Service) Execute(ctx context.Context, p domain.Plan, b []byte, step domain.Step) error {
	in, err := decode(b)
	if err != nil {
		return err
	}
	if step.ID != "capture" || operations.OperationID(ctx) == "" {
		return domain.Fail("INVALID_INPUT", "bound capture step required")
	}
	if err = s.Validate(ctx, p, b); err != nil {
		return err
	}
	// The engine persisted intent before this exclusively created private stage.
	// Existing stages are never reused following a crash or ambiguous failure.
	if err = platform.PrivateDir(s.Cache); err != nil {
		return err
	}
	work := filepath.Join(s.Cache, in.SnapshotID)
	if err = os.Mkdir(work, 0700); err != nil {
		return domain.Fail("RECOVERY_REQUIRED", "capture staging already exists or cannot be created; inspect the original operation")
	}
	if err = syncDirectory(s.Cache); err != nil {
		return err
	}
	held := &sourceSet{}
	defer held.Close()
	diskFiles := make([][]platform.DiskSourceFile, len(in.Disks))
	for i, d := range in.Disks {
		for _, source := range d.Files {
			_, f, err := held.Open(ctx, in.SourceRoot, source)
			if err != nil {
				return err
			}
			rel, _ := relative(in.SourceRoot, source.Path)
			diskFiles[i] = append(diskFiles[i], platform.DiskSourceFile{Path: filepath.ToSlash(rel), File: f})
		}
	}
	var firmware *coldfiles.Source
	if in.Firmware != nil {
		firmware, _, err = held.Open(ctx, filepath.Dir(in.Firmware.Path), *in.Firmware)
		if err != nil {
			return err
		}
	}
	if err = s.Validate(ctx, p, b); err != nil {
		return err
	}
	started := time.Now().UTC()
	m := protection.CaptureManifest{APIVersion: domain.APIVersion, Kind: "ColdRecoveryPoint", Version: 1, ID: in.SnapshotID, OperationID: operations.OperationID(ctx), SourceVM: in.Native.Resource, StartedAt: started, SourceFingerprint: in.Native.Fingerprint, Source: *in.Native.Source, StateBefore: "stopped", StateAfter: "stopped", NativeVersions: in.Versions, Members: []protection.CaptureMember{}, Disks: []protection.CapturedDisk{}, TPMMembers: []protection.CapturedTPMFile{}, Secrets: []protection.CapturedSecret{}, IndependentlyRecoverable: true, PersistentXMLMember: "persistent-xml", EffectiveXMLMember: "persistent-xml"}
	sources := []coldstore.Source{}
	if err = addMember(ctx, &m, &sources, "persistent-xml", "persistent-xml", "configuration/persistent.xml", bytes.NewReader([]byte(in.PersistentXML)), int64(len(in.PersistentXML))); err != nil {
		return err
	}
	for i, d := range in.Disks {
		if err = s.canceled(ctx); err != nil {
			return err
		}
		if err = held.Recheck(ctx); err != nil {
			return err
		}
		if err = s.Validate(ctx, p, b); err != nil {
			return err
		}
		name, _ := relative(in.SourceRoot, d.Files[0].Path)
		diskWork := filepath.Join(work, fmt.Sprintf("disk-%03d", i))
		if err = os.Mkdir(diskWork, 0700); err != nil {
			return err
		}
		chain, err := s.Tool.InspectFiles(ctx, diskFiles[i], diskWork, filepath.ToSlash(name), d.Format, maximumDisk)
		if err != nil {
			return err
		}
		if len(chain) != len(d.Files) || chain[0].VirtualSize != d.VirtualBytes {
			return domain.Fail("SOURCE_CHANGED", "disk graph changed before conversion")
		}
		id := fmt.Sprintf("disk-%03d", i)
		kind, format, memberPath := "disk", "qcow2", fmt.Sprintf("disks/%03d.qcow2", i)
		var reader io.ReaderAt
		var size int64
		if d.Media {
			kind, format, memberPath = "media", "raw", fmt.Sprintf("media/%03d.raw", i)
			reader = diskFiles[i][0].File
			size = int64(d.Files[0].Identity.Size)
		} else {
			if err = operations.Note(ctx, s.Engine.Store, "Intent persisted: flatten and compare captured disk "+d.Target); err != nil {
				return err
			}
			if err = s.Tool.ConvertFiles(ctx, diskFiles[i], diskWork, filepath.ToSlash(name), d.Format, d.VirtualBytes, d.VirtualBytes+d.VirtualBytes/4+(16<<20)); err != nil {
				return err
			}
			f, identity, err := fileidentity.Open(filepath.Join(diskWork, "disk.qcow2"), false, true)
			if err != nil {
				return err
			}
			held.files = append(held.files, f)
			reader = f
			size = int64(identity.Size)
		}
		if err = addMember(ctx, &m, &sources, id, kind, memberPath, reader, size); err != nil {
			return err
		}
		m.Disks = append(m.Disks, protection.CapturedDisk{Target: d.Target, MemberID: id, Format: format, Independent: true})
	}
	if firmware != nil {
		m.FirmwareCodeMember = "firmware-code"
		if err = addMember(ctx, &m, &sources, m.FirmwareCodeMember, "firmware-code", "firmware/code", firmware, int64(in.Firmware.Identity.Size)); err != nil {
			return err
		}
	}
	if in.Auxiliary != nil {
		req := s.auxiliaryRequest(in, p.ActorUID, operations.OperationID(ctx), p.Digest, "capture")
		if err = operations.Note(ctx, s.Engine.Store, "Intent persisted: capture the exact reviewed firmware and TPM inventory through the bounded helper"); err != nil {
			return err
		}
		transfer, err := s.Helper.CaptureAuxiliary(ctx, req)
		if err != nil {
			return err
		}
		defer transfer.Close()
		if err = s.addAuxiliary(ctx, work, in, transfer, &m, &sources, held); err != nil {
			return err
		}
		if err = held.Recheck(ctx); err != nil {
			return err
		}
		if err = s.Validate(ctx, p, b); err != nil {
			return err
		}
		if err = s.canceled(ctx); err != nil {
			return err
		}
		delivered, err := transfer.Commit(ctx)
		if err != nil {
			return err
		}
		proof, err := json.Marshal(delivered)
		if err != nil {
			return err
		}
		m.AuxiliaryInventoryMember = "auxiliary-inventory"
		if err = addMember(ctx, &m, &sources, m.AuxiliaryInventoryMember, "auxiliary-inventory", "configuration/auxiliary.json", bytes.NewReader(proof), int64(len(proof))); err != nil {
			return err
		}
	}
	if err = held.Recheck(ctx); err != nil {
		return err
	}
	if err = s.Validate(ctx, p, b); err != nil {
		return err
	}
	if err = s.canceled(ctx); err != nil {
		return err
	}
	m.FinishedAt = time.Now().UTC()
	if err = m.Validate(); err != nil {
		return err
	}
	if err = operations.Note(ctx, s.Engine.Store, "Intent persisted: publish the complete verified recovery set without replacing any existing capture"); err != nil {
		return err
	}
	if err = platform.PrivateDir(s.Catalog); err != nil {
		return err
	}
	manifestBytes, err := json.Marshal(m)
	if err != nil {
		return err
	}
	manifestHash := sha256.Sum256(manifestBytes)
	intent := publicationIntent{Version: 1, SnapshotID: in.SnapshotID, OperationID: operations.OperationID(ctx), ManifestSHA256: hex.EncodeToString(manifestHash[:])}
	if err = s.Engine.Store.ComparePut("cold-capture-publication", p.ID, nil, intent); err != nil {
		return err
	}
	_, err = coldstore.Publish(ctx, s.Catalog, m, sources)
	// Keep conversion staging on failures. A successful catalog is complete and
	// can be reconciled independently if cleanup or acknowledgement is interrupted.
	if err != nil {
		return err
	}
	if err = os.RemoveAll(work); err != nil {
		return err
	}
	return syncDirectory(s.Cache)
}
func (s *Service) addAuxiliary(ctx context.Context, work string, in recipe, transfer *helper.AuxiliaryTransfer, m *protection.CaptureManifest, sources *[]coldstore.Source, held *sourceSet) error {
	if transfer == nil || transfer.Snapshot == nil || transfer.Response.Inventory == nil || transfer.Response.Artifact == nil || !exact(transfer.Response.Inventory, in.Auxiliary) {
		return domain.Fail("RECOVERY_REQUIRED", "helper capture inventory differs from the reviewed set")
	}
	offset := int64(0)
	for i, member := range in.Auxiliary.Members {
		// The typed client checked the entire deterministic archive. Each member is
		// copied positionally; SCM_RIGHTS' shared seek offset is never used.
		offset += 512
		section := io.NewSectionReader(transfer.Snapshot, offset, int64(member.State.Size))
		id := fmt.Sprintf("auxiliary-%03d", i)
		localPath := filepath.Join(work, id)
		f, err := os.OpenFile(localPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		held.files = append(held.files, f)
		if _, err = io.CopyN(f, section, int64(member.State.Size)); err != nil {
			return err
		}
		if err = f.Sync(); err != nil {
			return err
		}
		readOnly, _, err := fileidentity.Open(localPath, false, true)
		if err != nil {
			return err
		}
		held.files = append(held.files, readOnly)
		f = readOnly
		hash, err := hashReader(ctx, f, int64(member.State.Size))
		if err != nil {
			return err
		}
		if hash != transfer.Response.Artifact.MemberSHA256[member.ID] {
			return domain.Fail("SOURCE_CHANGED", "auxiliary archive member digest differs")
		}
		memberPath := "firmware/nvram"
		if member.Kind == "tpm" {
			name, err := relative(in.Native.Layout.TPM.SourcePath, filepath.Join(in.Auxiliary.Root.Path, member.RelativePath))
			if err != nil {
				return err
			}
			m.TPMMembers = append(m.TPMMembers, protection.CapturedTPMFile{Name: filepath.ToSlash(name), MemberID: id})
			memberPath = "tpm/" + filepath.ToSlash(name)
		} else if member.Kind == "nvram" {
			m.NVRAMMember = id
			format := in.Native.Layout.Firmware.NVRAM.Format
			if format == "" {
				format = "raw"
			}
			// Inspect qcow2 NVRAM only as an unprivileged, confined private copy.
			if format == "qcow2" {
				inspectWork, err := os.MkdirTemp(work, "nvram-inspect-")
				if err != nil {
					return err
				}
				chain, err := s.Tool.InspectFiles(ctx, []platform.DiskSourceFile{{Path: "nvram", File: f}}, inspectWork, "nvram", format, 64<<20)
				if err != nil {
					return err
				}
				if len(chain) != 1 {
					return domain.Fail("INCOMPLETE_BACKUP", "NVRAM has unresolved backing dependencies")
				}
			}
		} else {
			return domain.Fail("RECOVERY_REQUIRED", "unknown auxiliary member kind")
		}
		if err = addMember(ctx, m, sources, id, member.Kind, memberPath, f, int64(member.State.Size)); err != nil {
			return err
		}
		offset += int64(member.State.Size) + (512-int64(member.State.Size)%512)%512
	}
	return ctx.Err()
}
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func mustXML(ctx context.Context, catalog string, r coldstore.Receipt) []byte {
	if ctx.Err() != nil {
		return nil
	}
	for _, m := range r.Manifest.Members {
		if m.ID == r.Manifest.PersistentXMLMember {
			if m.Size > 8<<20 {
				return nil
			}
			root, err := os.OpenRoot(filepath.Join(catalog, r.SnapshotID))
			if err != nil {
				return nil
			}
			defer root.Close()
			f, err := root.Open(m.Path)
			if err != nil {
				return nil
			}
			defer f.Close()
			raw, err := io.ReadAll(io.LimitReader(f, m.Size+1))
			if err == nil && int64(len(raw)) == m.Size {
				return raw
			}
		}
	}
	return nil
}
