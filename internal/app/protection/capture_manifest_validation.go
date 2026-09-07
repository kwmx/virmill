package protection

import (
	"encoding/hex"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"virmill.local/core/internal/app/importer"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const (
	captureSourceEntryLimit = 1024
	captureBackingLimit     = 16
	captureNativeLimit      = 32
)

var captureDiskTarget = regexp.MustCompile(`^(?:(?:ioemu:)?(?:fd|hd|sd|vd|xvd|ubd)[a-zA-Z0-9_]+|nvme[0-9]+n[0-9]+(?:p[0-9]+)?)$`)

func captureInvalid(message string) error { return domain.Fail("INVALID_INPUT", message) }

// Validate checks a bounded, internally consistent declaration. It neither
// observes source files nor authenticates capture, graph completeness or boot.
func (m CaptureManifest) Validate() error {
	if m.APIVersion != domain.APIVersion || m.Kind != "ColdRecoveryPoint" || m.Version != 1 {
		return captureInvalid("unsupported cold recovery manifest version or kind")
	}
	if m.Members == nil || m.Disks == nil || m.TPMMembers == nil || m.Secrets == nil {
		return captureInvalid("capture collections must be explicit arrays, including empty arrays")
	}
	if !captureUUID(m.ID) || !captureUUID(m.OperationID) || !captureUUID(m.SourceVM.UUID) || m.SourceVM.ProviderID != "libvirt" || m.SourceVM.Kind != "vm" || (m.SourceVM.ConnectionID != "qemu:///system" && m.SourceVM.ConnectionID != "qemu:///session") || m.Source.State.VMID != m.SourceVM.UUID {
		return captureInvalid("canonical recovery identities and one local libvirt source VM required")
	}
	if !captureTime(m.StartedAt) || !captureTime(m.FinishedAt) || m.FinishedAt.Before(m.StartedAt) || m.StateBefore != "stopped" || m.StateAfter != "stopped" || m.HasManagedSave || !captureDigest(m.SourceFingerprint) {
		return captureInvalid("ordered UTC capture times, stopped source and valid fingerprint required")
	}
	if len(m.NativeVersions) > captureNativeLimit || !manifestText(m.NativeVersions["libvirt"]) || !manifestText(m.NativeVersions["qemu"]) || m.Source.State.TPM != nil && !manifestText(m.NativeVersions["swtpm"]) {
		return captureInvalid("bounded native versions required for the declared source")
	}
	for name, version := range m.NativeVersions {
		if !captureIdentifier(name, 64) || !manifestText(version) {
			return captureInvalid("invalid native version identifier or value")
		}
	}
	if err := validateCaptureSource(m.Source); err != nil {
		return err
	}
	if len(m.Source.External) != 0 && m.IndependentlyRecoverable {
		return captureInvalid("unresolved source dependencies forbid independent recovery")
	}
	if len(m.Members) == 0 || len(m.Members) > manifestMemberLimit || len(m.Disks) > manifestMemberLimit || len(m.TPMMembers) > manifestMemberLimit || len(m.Secrets) > manifestReferenceLimit {
		return captureInvalid("capture member or mapping count outside bounds")
	}
	members := map[string]CaptureMember{}
	paths := map[string]bool{}
	var total int64
	for _, member := range m.Members {
		if !manifestText(member.ID) || members[member.ID].ID != "" || !captureRelative(member.Path) || !captureDigest(member.SHA256) || member.Size < 0 || member.Size == 0 && member.Kind != "tpm" || member.Size > manifestMemberSizeLimit || member.Size > manifestTotalSizeLimit-total {
			return captureInvalid("invalid, duplicate or excessive capture member")
		}
		if !captureEnum(member.Kind, "disk", "media", "persistent-xml", "effective-xml", "firmware-code", "nvram", "tpm", "secret", "auxiliary-inventory") {
			return captureInvalid("unknown capture member kind")
		}
		if !captureAddPath(paths, member.Path) {
			return captureInvalid("capture member paths conflict")
		}
		members[member.ID] = member
		total += member.Size
	}
	used := map[string]bool{}
	reference := func(id, kind string) error {
		member, ok := members[id]
		if !ok {
			return domain.Fail("INCOMPLETE_BACKUP", "declared capture member is missing")
		}
		if used[id] || member.Kind != kind {
			return captureInvalid("capture member has a conflicting role or duplicate reference")
		}
		used[id] = true
		return nil
	}
	if err := reference(m.PersistentXMLMember, "persistent-xml"); err != nil {
		return err
	}
	if m.EffectiveXMLMember != m.PersistentXMLMember {
		if err := reference(m.EffectiveXMLMember, "effective-xml"); err != nil {
			return err
		}
	}
	optional := func(present bool, id, kind string) error {
		if present {
			return reference(id, kind)
		}
		if id != "" {
			return captureInvalid("capture artifact contradicts the declared source")
		}
		return nil
	}
	fw := m.Source.State.Firmware
	for _, role := range []struct {
		present  bool
		id, kind string
	}{
		{fw.Loader != "", m.FirmwareCodeMember, "firmware-code"},
		{fw.NVRAM != nil, m.NVRAMMember, "nvram"},
		{fw.NVRAM != nil || m.Source.State.TPM != nil, m.AuxiliaryInventoryMember, "auxiliary-inventory"},
	} {
		if err := optional(role.present, role.id, role.kind); err != nil {
			return err
		}
	}
	disks := map[string]domain.ColdDiskSource{}
	for _, disk := range m.Source.Disks {
		if !disk.Empty {
			disks[disk.Target] = disk
		}
	}
	for _, captured := range m.Disks {
		disk, ok := disks[captured.Target]
		if !ok || !captured.Independent || !captureEnum(captured.Format, "raw", "qcow2") {
			return captureInvalid("captured disks require unique nonempty source targets and independent raw or qcow2 output")
		}
		kind := "disk"
		if disk.Device == "cdrom" || disk.Device == "floppy" {
			kind = "media"
		}
		if err := reference(captured.MemberID, kind); err != nil {
			return err
		}
		delete(disks, captured.Target)
	}
	if len(disks) != 0 {
		return domain.Fail("INCOMPLETE_BACKUP", "nonempty source disk has no captured member")
	}
	if (m.Source.State.TPM != nil) != (len(m.TPMMembers) != 0) {
		return domain.Fail("INCOMPLETE_BACKUP", "TPM members contradict the declared source")
	}
	tpmNames := map[string]bool{}
	for _, file := range m.TPMMembers {
		if !captureRelative(file.Name) || !captureAddPath(tpmNames, file.Name) {
			return captureInvalid("invalid or conflicting TPM member name")
		}
		if err := reference(file.MemberID, "tpm"); err != nil {
			return err
		}
	}
	secrets := map[string]bool{}
	for _, id := range m.Source.State.SecretReferences {
		secrets[id] = true
	}
	if tpm := m.Source.State.TPM; tpm != nil && tpm.EncryptionSecret != "" {
		secrets[tpm.EncryptionSecret] = true
	}
	if len(secrets) > manifestReferenceLimit {
		return captureInvalid("secret reference count exceeds bound")
	}
	for _, secret := range m.Secrets {
		if !captureUUID(secret.UUID) || !secrets[secret.UUID] || secret.External == (secret.MemberID != "") {
			return captureInvalid("each source secret requires exactly one included or external disposition")
		}
		if secret.External {
			if m.IndependentlyRecoverable {
				return captureInvalid("external secrets forbid independent recovery")
			}
		} else if err := reference(secret.MemberID, "secret"); err != nil {
			return err
		}
		delete(secrets, secret.UUID)
	}
	if len(secrets) != 0 {
		return domain.Fail("INCOMPLETE_BACKUP", "source secret has no capture disposition")
	}
	if len(used) != len(members) {
		return captureInvalid("every capture member must have exactly one declared role")
	}
	return nil
}

func validateCaptureSource(s domain.ColdSourceLayout) error {
	if s.Disks == nil || s.External == nil || s.State.SecretReferences == nil {
		return captureInvalid("source collections must be explicit arrays, including empty arrays")
	}
	if !captureIdentifier(s.Architecture, 256) || !captureIdentifier(s.Machine, 256) || len(s.Disks) > manifestMemberLimit || len(s.External) > captureSourceEntryLimit || len(s.State.SecretReferences) > manifestReferenceLimit {
		return captureInvalid("invalid or excessive source inventory")
	}
	external := map[domain.ColdDependency]bool{}
	for _, dependency := range s.External {
		if !captureIdentifier(dependency.Kind, 64) || !captureDependencyTarget(dependency.Target) || external[dependency] {
			return captureInvalid("invalid or duplicate external dependency")
		}
		external[dependency] = true
	}
	targets := map[string]bool{}
	entries := 0
	for _, disk := range s.Disks {
		if disk.Backing == nil {
			return captureInvalid("source backing inventory must be an explicit array")
		}
		key := strings.TrimPrefix(disk.Target, "ioemu:")
		if len(disk.Target) > 128 || !captureDiskTarget.MatchString(disk.Target) || targets[key] || !captureEnum(disk.Device, "disk", "lun", "cdrom", "floppy") || !captureEnum(disk.Bus, "", "ide", "fdc", "scsi", "virtio", "xen", "usb", "uml", "sata", "sd", "nvme") || len(disk.Backing) > captureBackingLimit {
			return captureInvalid("invalid or duplicate source disk target, device or bus")
		}
		targets[key] = true
		entries += 1 + len(disk.Backing)
		if entries > captureSourceEntryLimit || disk.Empty && (disk.Device != "cdrom" && disk.Device != "floppy" || len(disk.Backing) != 0) || disk.Device == "cdrom" && !disk.ReadOnly || disk.Device == "lun" && !captureEnum(disk.Source.Type, "block", "network", "volume") {
			return captureInvalid("contradictory source disk or backing inventory")
		}
		identities := map[domain.ColdStorageSource]bool{}
		values := append([]domain.ColdStorageSource{disk.Source}, disk.Backing...)
		for i, source := range values {
			location := disk.Target
			if i != 0 {
				location += "/backing[" + strconv.Itoa(i) + "]"
			}
			if err := validateCaptureStorage(source, disk.Empty && i == 0, location, external); err != nil {
				return err
			}
			// Format differences do not disambiguate the same source identity.
			source.Format = ""
			if source.File != "" || source.Pool != "" {
				if identities[source] {
					return captureInvalid("repeated source identity in backing chain")
				}
				identities[source] = true
			}
		}
	}
	f := s.State.Firmware
	if f.Loader == "" && (f.LoaderType != "" || f.LoaderFormat != "" || f.LoaderReadOnly != "" || f.LoaderSecure != "" || f.LoaderStateless != "") || f.Loader != "" && !captureAbsolute(f.Loader) || !captureEnum(f.LoaderType, "", "rom", "pflash") || !captureEnum(f.LoaderFormat, "", "raw", "qcow2") || !captureEnum(f.LoaderReadOnly, "", "yes", "no") || !captureEnum(f.LoaderSecure, "", "yes", "no") || !captureEnum(f.LoaderStateless, "", "yes", "no") {
		return captureInvalid("invalid or unresolved source loader identity")
	}
	if n := f.NVRAM; n != nil {
		if f.LoaderStateless == "yes" || n.Path != "" && !captureAbsolute(n.Path) || n.Template != "" && !captureAbsolute(n.Template) || !captureEnum(n.Format, "", "raw", "qcow2") || !captureEnum(n.TemplateFormat, "", "raw", "qcow2") || n.Template == "" && n.TemplateFormat != "" {
			return captureInvalid("invalid source NVRAM declaration")
		}
	}
	if t := s.State.TPM; t != nil {
		if !captureEnum(t.Model, "", "tpm-tis", "tpm-crb", "tpm-spapr") || !captureEnum(t.Version, "", "1.2", "2.0") || !captureEnum(t.PersistentState, "", "yes", "no") || t.Model == "tpm-crb" && t.Version == "1.2" || !captureEnum(t.SourceType, "", "file", "dir") || (t.SourceType == "") != (t.SourcePath == "") || t.SourcePath != "" && !captureAbsolute(t.SourcePath) || !captureProfile(t.Profile) || !captureProfile(t.ProfileSource) || !captureEnum(t.ProfileRemoveDisabled, "", "check", "fips-host") || t.Version == "1.2" && (t.Profile != "" || t.ProfileSource != "" || t.ProfileRemoveDisabled != "") || t.EncryptionSecret != "" && !captureUUID(t.EncryptionSecret) {
			return captureInvalid("invalid source TPM declaration")
		}
	}
	secrets := map[string]bool{}
	for _, id := range s.State.SecretReferences {
		if !captureUUID(id) || secrets[id] {
			return captureInvalid("invalid or duplicate source secret UUID")
		}
		secrets[id] = true
	}
	return nil
}

func validateCaptureStorage(s domain.ColdStorageSource, empty bool, location string, external map[domain.ColdDependency]bool) error {
	if !captureIdentifier(s.Type, 64) || s.Format != "" && !captureIdentifier(s.Format, 64) {
		return captureInvalid("invalid source storage type or format")
	}
	if empty {
		if s.File != "" || s.Pool != "" || s.Volume != "" {
			return captureInvalid("empty removable source contains storage identity")
		}
		return nil
	}
	switch s.Type {
	case "file":
		if !captureAbsolute(s.File) || s.Pool != "" || s.Volume != "" {
			return captureInvalid("file source requires one canonical absolute identity")
		}
	case "volume":
		if s.File != "" || !captureIdentifier(s.Pool, 256) || !captureIdentifier(s.Volume, 256) || s.Pool == "." || s.Pool == ".." || s.Volume == "." || s.Volume == ".." {
			return captureInvalid("volume source requires one pool and volume identity")
		}
	default:
		kind := "storage-unsupported"
		if captureEnum(s.Type, "block", "network", "dir", "nvme", "vhostuser", "vhostvdpa", "ctl") {
			kind = "storage-" + s.Type
		}
		if s.File != "" || s.Pool != "" || s.Volume != "" || !external[domain.ColdDependency{Kind: kind, Target: location}] {
			return captureInvalid("opaque storage requires its exact unresolved external dependency")
		}
	}
	return nil
}

func captureUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' || strings.ToLower(s) != s || s == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	_, err := hex.DecodeString(s[:8] + s[9:13] + s[14:18] + s[19:23] + s[24:])
	return err == nil
}
func captureDigest(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
func captureTime(t time.Time) bool {
	_, offset := t.Zone()
	return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 && offset == 0
}
func captureEnum(s string, values ...string) bool {
	for _, value := range values {
		if s == value {
			return true
		}
	}
	return false
}
func captureScalar(s string, limit int) bool {
	if s == "" || len(s) > limit || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
func captureIdentifier(s string, limit int) bool {
	if !captureScalar(s, limit) {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.:-", r)) {
			return false
		}
	}
	return true
}
func captureProfile(s string) bool {
	return s == "" || captureIdentifier(s, 256) && !strings.Contains(s, "_")
}
func captureAbsolute(s string) bool {
	return captureScalar(s, 4096) && s != "/" && path.IsAbs(s) && path.Clean(s) == s
}
func captureRelative(s string) bool {
	return importer.SafePath(s) == nil && !strings.HasSuffix(s, "/") && strings.TrimSpace(s) == s
}
func captureDependencyTarget(s string) bool {
	if !captureScalar(s, 1024) || path.IsAbs(s) || path.Clean(s) != s || s == "." || s == ".." || strings.HasPrefix(s, "../") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_.:-/[]", r)) {
			return false
		}
	}
	return true
}
func captureAddPath(paths map[string]bool, name string) bool {
	key := strings.ToLower(name)
	if paths[key] {
		return false
	}
	for other := range paths {
		if strings.HasPrefix(key, other+"/") || strings.HasPrefix(other, key+"/") {
			return false
		}
	}
	paths[key] = true
	return true
}

// DecodeCaptureManifest rejects ambiguous JSON before validating the complete
// declaration. Every failure returns a zero manifest, never a partial result.
func DecodeCaptureManifest(data []byte) (CaptureManifest, error) {
	var m CaptureManifest
	if len(data) == 0 || len(data) > manifestDocumentLimit {
		return m, captureInvalid("capture manifest JSON size outside bounds")
	}
	if err := wire.Decode(data, &m); err != nil {
		return CaptureManifest{}, captureInvalid("invalid capture manifest JSON; exact fields and unambiguous values required")
	}
	if err := validation.Schema("cold-recovery-point", data); err != nil {
		return CaptureManifest{}, captureInvalid("capture manifest does not match the required wire shape")
	}
	if err := m.Validate(); err != nil {
		return CaptureManifest{}, err
	}
	return m, nil
}
