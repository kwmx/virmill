//go:build linux && cgo

package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	native "libvirt.org/go/libvirt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"syscall"
	"virmill.local/core/internal/backend/fileidentity"
	"virmill.local/core/internal/backend/xmlpatch"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var volumePattern = regexp.MustCompile(`^virmill-[0-9a-f-]{36}-disk-[0-9]{3}\.qcow2$`)
var mediaVolumePattern = regexp.MustCompile(`^virmill-[0-9a-f-]{36}-media-[0-9]{3}\.iso$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var macPattern = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)

func validateCreationSpec(s domain.CreationSpec) error {
	if err := s.DevicePolicy.Validate(s.Machine); err != nil {
		return err
	}
	if !uuidPattern.MatchString(s.UUID) || !uuidPattern.MatchString(s.PoolID) || s.Architecture != "x86_64" || s.Machine == "" || len(s.Machine) > 128 || s.VCPUs < 1 || s.VCPUs > 512 || s.MemoryMiB < 128 || s.MemoryMiB > 1<<20 {
		return domain.Fail("INVALID_INPUT", "explicit UUID, pool, x86_64 machine and bounded CPU/RAM required")
	}
	if _, err := validation.DisplayName(s.Name); err != nil {
		return domain.Fail("INVALID_INPUT", err.Error())
	}
	if s.CPU.Mode != "host-passthrough" && s.CPU.Mode != "host-model" && s.CPU.Mode != "custom" {
		return domain.Fail("INVALID_INPUT", "explicit CPU mode required")
	}
	if (s.CPU.Mode == "custom") != (s.CPU.Model != "") || len(s.CPU.Model) > 128 {
		return domain.Fail("INVALID_INPUT", "custom CPU mode requires one exact model")
	}
	if s.Clock != "utc" && s.Clock != "localtime" {
		return domain.Fail("INVALID_INPUT", "explicit utc or localtime clock required")
	}
	if s.Graphics != "none" && s.Graphics != "vnc-unix" && s.Graphics != "spice-unix" {
		return domain.Fail("INVALID_INPUT", "graphics must be none, vnc-unix or spice-unix")
	}
	if s.Graphics == "spice-unix" && s.DevicePolicy == nil {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "Private SPICE requires an explicit device policy with audio disabled")
	}
	f := s.Firmware
	if f.Mode == "bios" {
		if f.Code != "" || f.Template != "" || f.Format != "" || f.SecureBoot || f.TPM {
			return domain.Fail("INVALID_INPUT", "BIOS profile cannot silently add UEFI/TPM state")
		}
	} else if f.Mode == "uefi" {
		if !filepath.IsAbs(f.Code) || !filepath.IsAbs(f.Template) || (f.Format != "raw" && f.Format != "qcow2") {
			return domain.Fail("INVALID_INPUT", "UEFI requires explicit absolute code/template and raw or qcow2 format")
		}
	} else {
		return domain.Fail("INVALID_INPUT", "explicit bios or uefi firmware required")
	}
	if len(s.Disks) < 1 || len(s.Disks) > 64 || len(s.Media) > 4 || len(s.NICs) > 32 {
		return domain.Fail("INVALID_INPUT", "creation requires 1–64 disks, at most four read-only media and at most 32 NICs")
	}
	bootCount := len(s.Disks)
	for _, m := range s.Media {
		if m.BootOrder > 0 {
			bootCount++
		}
	}
	disks := map[string]bool{}
	orders := map[int]bool{}
	sata := 0
	for _, d := range s.Disks {
		if d.SourceID == "" || disks[d.SourceID] || orders[d.BootOrder] || d.BootOrder < 1 || d.BootOrder > bootCount {
			return domain.Fail("INVALID_INPUT", "disk IDs and complete boot ordering must be unique")
		}
		disks[d.SourceID] = true
		orders[d.BootOrder] = true
		switch d.Bus {
		case "sata":
			sata++
		case "virtio", "scsi":
		default:
			return domain.Fail("INVALID_INPUT", "unsupported disk bus")
		}
	}
	for _, m := range s.Media {
		if m.SourceID == "" || disks[m.SourceID] || m.BootOrder < 0 || m.BootOrder > bootCount || (m.BootOrder > 0 && orders[m.BootOrder]) {
			return domain.Fail("INVALID_INPUT", "media IDs and boot ordering must be unique across disks and media")
		}
		disks[m.SourceID] = true
		if m.BootOrder > 0 {
			orders[m.BootOrder] = true
		}
		switch m.Bus {
		case "sata":
			sata++
		case "scsi":
		default:
			return domain.Fail("INVALID_INPUT", "read-only media require SATA or SCSI")
		}
	}
	if sata > 6 {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "one explicit SATA controller supports at most six disks; choose supported SCSI/virtio mapping")
	}
	ids, macs := map[string]bool{}, map[string]bool{}
	for _, n := range s.NICs {
		if n.ID == "" || len(n.ID) > 63 || ids[n.ID] || !uuidPattern.MatchString(n.NetworkID) || !macPattern.MatchString(n.MAC) || macs[n.MAC] || n.SourceIndex < -1 {
			return domain.Fail("INVALID_INPUT", "NIC IDs, new MACs and exact network UUIDs required")
		}
		if n.Model != "virtio" && n.Model != "e1000e" && n.Model != "rtl8139" {
			return domain.Fail("INVALID_INPUT", "unsupported NIC model")
		}
		if n.Link != "up" && n.Link != "down" {
			return domain.Fail("INVALID_INPUT", "explicit NIC link state required")
		}
		ids[n.ID] = true
		macs[n.MAC] = true
	}
	return nil
}

type capEnum struct {
	Name   string   `xml:"name,attr"`
	Values []string `xml:"value"`
}
type capDevice struct {
	Supported string    `xml:"supported,attr"`
	Enums     []capEnum `xml:"enum"`
}
type domainCaps struct {
	Path    string `xml:"path"`
	Domain  string `xml:"domain"`
	Machine string `xml:"machine"`
	Arch    string `xml:"arch"`
	VCPU    struct {
		Max uint `xml:"max,attr"`
	} `xml:"vcpu"`
	OS struct {
		Supported string    `xml:"supported,attr"`
		Enums     []capEnum `xml:"enum"`
		Loader    struct {
			Supported string    `xml:"supported,attr"`
			Values    []string  `xml:"value"`
			Enums     []capEnum `xml:"enum"`
		} `xml:"loader"`
	} `xml:"os"`
	CPU struct {
		Modes []struct {
			Name      string `xml:"name,attr"`
			Supported string `xml:"supported,attr"`
			Models    []struct {
				Name   string `xml:",chardata"`
				Usable string `xml:"usable,attr"`
			} `xml:"model"`
		} `xml:"mode"`
	} `xml:"cpu"`
	Devices struct {
		Disk      capDevice `xml:"disk"`
		Interface capDevice `xml:"interface"`
		Graphics  capDevice `xml:"graphics"`
		TPM       capDevice `xml:"tpm"`
		Channel   capDevice `xml:"channel"`
	} `xml:"devices"`
}

func enumHas(enums []capEnum, name, value string) bool {
	for _, e := range enums {
		if e.Name == name {
			for _, v := range e.Values {
				if v == value {
					return true
				}
			}
		}
	}
	return false
}
func firmwareFileDigest(path string) (string, error) {
	f, err := openSystemFile(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.Mode().IsRegular() || owner.Uid != 0 || st.Mode().Perm()&0022 != 0 || st.Size() < 1 || st.Size() > 64<<20 {
		return "", domain.Fail("PERMISSION_DENIED", "firmware must be a bounded root-owned ordinary system file without group/other write access")
	}
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(f, (64<<20)+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func openSystemFile(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, domain.Fail("INVALID_INPUT", "canonical absolute system file path required")
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
func checkCaps(c domainCaps, s domain.CreationSpec) error {
	if s.GuestAgent && (c.Devices.Channel.Supported != "yes" || !enumHas(c.Devices.Channel.Enums, "type", "unix")) {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "This host does not advertise a supported UNIX guest-agent channel.")
	}
	if c.Domain != "kvm" || c.Arch != "x86_64" || c.Machine == "" || c.Path == "" || s.VCPUs > c.VCPU.Max {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "requested KVM machine or vCPU count is unavailable")
	}
	cpu := false
	for _, m := range c.CPU.Modes {
		if m.Name == s.CPU.Mode && m.Supported != "no" {
			if m.Name != "custom" {
				cpu = m.Supported == "yes"
			} else {
				for _, model := range m.Models {
					if model.Name == s.CPU.Model && model.Usable == "yes" {
						cpu = true
					}
				}
			}
		}
	}
	if !cpu {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "requested CPU mode/model is not positively supported")
	}
	for _, d := range s.Disks {
		if c.Devices.Disk.Supported != "yes" || !enumHas(c.Devices.Disk.Enums, "bus", d.Bus) {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "requested disk bus is unavailable")
		}
	}
	for _, m := range s.Media {
		if c.Devices.Disk.Supported != "yes" || !enumHas(c.Devices.Disk.Enums, "diskDevice", "cdrom") || !enumHas(c.Devices.Disk.Enums, "bus", m.Bus) {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "requested CD-ROM device/bus is not positively advertised")
		}
	}
	if len(s.NICs) > 0 && c.Devices.Interface.Supported != "yes" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "network interface support is not advertised")
	}
	if s.Graphics == "vnc-unix" && (c.Devices.Graphics.Supported != "yes" || !enumHas(c.Devices.Graphics.Enums, "type", "vnc")) {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "VNC graphics is unavailable")
	}
	if s.Graphics == "spice-unix" && (c.Devices.Graphics.Supported != "yes" || !enumHas(c.Devices.Graphics.Enums, "type", "spice")) {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "Private SPICE graphics is not advertised by this host")
	}
	if s.Firmware.Mode == "uefi" {
		found := false
		for _, v := range c.OS.Loader.Values {
			if v == s.Firmware.Code {
				found = true
			}
		}
		if c.OS.Loader.Supported != "yes" || !found || !enumHas(c.OS.Loader.Enums, "type", "pflash") {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "selected UEFI code is not an advertised pflash loader")
		}
		if s.Firmware.SecureBoot && !enumHas(c.OS.Loader.Enums, "secure", "yes") {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "selected backend does not advertise secure firmware support")
		}
	}
	if s.Firmware.TPM && (c.Devices.TPM.Supported != "yes" || !enumHas(c.Devices.TPM.Enums, "model", "tpm-crb") || !enumHas(c.Devices.TPM.Enums, "backendModel", "emulator") || !enumHas(c.Devices.TPM.Enums, "backendVersion", "2.0")) {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "TPM 2.0 emulator with CRB is not advertised")
	}
	return nil
}

func poolCreationFingerprint(p domain.StoragePool) (string, error) {
	// Capacity/allocation/available may occur in backend XML; they are checked
	// separately from settings so our own new volumes do not invalidate intent.
	var stats struct {
		Capacity   *string `xml:"capacity"`
		Allocation *string `xml:"allocation"`
		Available  *string `xml:"available"`
	}
	if err := xml.Unmarshal([]byte(p.XML), &stats); err != nil {
		return "", err
	}
	changes := map[string]string{}
	if stats.Capacity != nil {
		changes["pool/capacity"] = "0"
	}
	if stats.Allocation != nil {
		changes["pool/allocation"] = "0"
	}
	if stats.Available != nil {
		changes["pool/available"] = "0"
	}
	x, err := xmlpatch.Patch(p.XML, changes)
	if err != nil {
		return "", err
	}
	p.XML = x
	return poolFingerprint(p), nil
}
func absentDomain(c *native.Connect, id, name string) error {
	for _, lookup := range []func() (*native.Domain, error){func() (*native.Domain, error) { return c.LookupDomainByUUIDString(id) }, func() (*native.Domain, error) { return c.LookupDomainByName(name) }} {
		d, err := lookup()
		if err == nil {
			d.Free()
			return domain.Fail("STALE_PLAN", "VM UUID or name already exists; no definition overwritten")
		}
		var e native.Error
		if !errors.As(err, &e) || e.Code != native.ERR_NO_DOMAIN {
			return err
		}
	}
	return nil
}
func (p *Provider) CheckCreationIdentity(ctx context.Context, uri, id, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := connect(uri, false)
	if err != nil {
		return err
	}
	defer c.Close()
	return absentDomain(c, id, name)
}
func (p *Provider) PreflightCreation(ctx context.Context, uri string, s domain.CreationSpec) (domain.CreationTarget, error) {
	return p.preflightCreation(ctx, uri, s, false)
}

func (p *Provider) preflightCreation(ctx context.Context, uri string, s domain.CreationSpec, existing bool) (domain.CreationTarget, error) {
	out := domain.CreationTarget{Spec: s, Networks: []domain.VirtualNetwork{}}
	if err := validateCreationSpec(s); err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	// The QEMU driver rejects GetDomainCapabilities on a read-only handle.
	// This preview only observes capabilities/resources; a writable connection
	// grants API access but does not authorize allocation or domain definition.
	c, err := connect(uri, true)
	if err != nil {
		return out, err
	}
	defer c.Close()
	if !existing {
		if err = absentDomain(c, s.UUID, s.Name); err != nil {
			return out, err
		}
	}
	pool, err := c.LookupStoragePoolByUUIDString(s.PoolID)
	if err != nil {
		return out, err
	}
	defer pool.Free()
	observed, err := observePool(pool, uri)
	if err != nil {
		return out, err
	}
	if !observed.Active || observed.State != "running" || (observed.Type != "dir" && observed.Type != "fs" && observed.Type != "netfs") {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "creation requires an already active file-based libvirt pool; no pool is activated automatically")
	}
	out.PoolName = observed.Name
	var poolTarget struct {
		Target struct {
			Path string `xml:"path"`
		} `xml:"target"`
	}
	if err = xml.Unmarshal([]byte(observed.XML), &poolTarget); err != nil {
		return out, err
	}
	poolIdentity, err := fileidentity.Observe(poolTarget.Target.Path, true)
	if err != nil {
		return out, domain.Fail("UNSUPPORTED_CAPABILITY", "target pool filesystem identity is unavailable: "+err.Error())
	}
	out.PoolGeneration = poolIdentity.Generation
	out.PoolFingerprint, err = poolCreationFingerprint(observed)
	if err != nil {
		return out, err
	}
	capsXML, err := c.GetDomainCapabilities("", s.Architecture, s.Machine, "kvm", 0)
	if err != nil {
		return out, err
	}
	if len(capsXML) > 1<<20 {
		return out, domain.Fail("INVALID_INPUT", "domain capabilities exceed bound")
	}
	var caps domainCaps
	if err = xml.Unmarshal([]byte(capsXML), &caps); err != nil {
		return out, err
	}
	if err = checkCaps(caps, s); err != nil {
		return out, err
	}
	out.Spec.Machine = caps.Machine
	out.Emulator = caps.Path
	out.CapabilitiesDigest = inventoryDigest(capsXML)
	out.EmulatorDigest, err = probeCreationDevices(ctx, caps.Path, out.Spec)
	if err != nil {
		return out, err
	}
	if s.Firmware.Mode == "uefi" {
		a, e := firmwareFileDigest(s.Firmware.Code)
		if e != nil {
			return out, e
		}
		b, e := firmwareFileDigest(s.Firmware.Template)
		if e != nil {
			return out, e
		}
		descriptor, e := matchingFirmwareDescriptor(out.Spec)
		if e != nil {
			return out, e
		}
		out.FirmwareDigest = inventoryDigest([]string{a, b, descriptor})
	}
	seen := map[string]bool{}
	for _, nic := range s.NICs {
		if seen[nic.NetworkID] {
			continue
		}
		seen[nic.NetworkID] = true
		n, e := c.LookupNetworkByUUIDString(nic.NetworkID)
		if e != nil {
			return out, e
		}
		network, e := observeNetwork(n, uri)
		n.Free()
		if e != nil {
			return out, e
		}
		if !network.Active {
			return out, domain.Fail("UNSUPPORTED_CAPABILITY", "selected network is inactive; activation needs a separate approved operation")
		}
		out.Networks = append(out.Networks, network)
	}
	sort.Slice(out.Networks, func(i, j int) bool { return out.Networks[i].Key.UUID < out.Networks[j].Key.UUID })
	return out, nil
}

func validateVolume(v domain.VolumeIntent) error {
	validContent := v.ContentType == "" && volumePattern.MatchString(v.Name)
	if v.ContentType == "cdrom-iso" {
		validContent = mediaVolumePattern.MatchString(v.Name) && v.FileBytes == v.VirtualBytes && v.FileBytes >= 32768 && v.FileBytes <= 64<<30 && v.FileBytes%2048 == 0
	}
	if !uuidPattern.MatchString(v.PoolID) || !validContent || v.VirtualBytes < 1 || v.VirtualBytes > 512<<30 || v.FileBytes < 1 || v.FileBytes > v.VirtualBytes+v.VirtualBytes/4+(16<<20) || !digestPattern.MatchString(v.SHA256) {
		return domain.Fail("INVALID_INPUT", "invalid new managed-volume intent")
	}
	return nil
}
func absentVolume(pool *native.StoragePool, name string) error {
	v, err := pool.LookupStorageVolByName(name)
	if err == nil {
		v.Free()
		return domain.Fail("STALE_PLAN", "target volume already exists; never overwrite it")
	}
	var e native.Error
	if !errors.As(err, &e) || e.Code != native.ERR_NO_STORAGE_VOL {
		return err
	}
	return nil
}
func (p *Provider) VolumeAbsent(ctx context.Context, uri string, in domain.VolumeIntent) error {
	if err := validateVolume(in); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := connect(uri, false)
	if err != nil {
		return err
	}
	defer c.Close()
	pool, err := c.LookupStoragePoolByUUIDString(in.PoolID)
	if err != nil {
		return err
	}
	defer pool.Free()
	return absentVolume(pool, in.Name)
}
func (p *Provider) AllocateVolume(ctx context.Context, uri string, in domain.VolumeIntent) (domain.CreatedVolume, error) {
	out := domain.CreatedVolume{Intent: in}
	if err := validateVolume(in); err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	c, err := connect(uri, true)
	if err != nil {
		return out, err
	}
	defer c.Close()
	pool, err := c.LookupStoragePoolByUUIDString(in.PoolID)
	if err != nil {
		return out, err
	}
	defer pool.Free()
	observed, err := observePool(pool, uri)
	if err != nil {
		return out, err
	}
	if !observed.Active || observed.State != "running" || (observed.Type != "dir" && observed.Type != "fs" && observed.Type != "netfs") {
		return out, domain.Fail("SOURCE_CHANGED", "allocation target is no longer an active file-based pool")
	}
	if err = absentVolume(pool, in.Name); err != nil {
		return out, err
	}
	x := fmt.Sprintf(`<volume><name>%s</name><capacity unit="bytes">%d</capacity><allocation unit="bytes">0</allocation><target><format type="raw"/></target></volume>`, xmlText(in.Name), in.FileBytes)
	v, err := pool.StorageVolCreateXML(x, 0)
	if err != nil {
		return out, err
	}
	defer v.Free()
	out.BackendKey, err = v.GetKey()
	if err != nil {
		return out, err
	}
	out.Path, err = v.GetPath()
	if err != nil {
		return out, err
	}
	identity, err := fileidentity.Observe(out.Path, false)
	if err != nil {
		return out, err
	}
	out.Generation = identity.Generation
	return out, err
}
func lookupCreated(c *native.Connect, expected domain.CreatedVolume) (*native.StorageVol, error) {
	pool, err := c.LookupStoragePoolByUUIDString(expected.Intent.PoolID)
	if err != nil {
		return nil, err
	}
	defer pool.Free()
	v, err := pool.LookupStorageVolByName(expected.Intent.Name)
	if err != nil {
		return nil, err
	}
	key, err := v.GetKey()
	if err != nil {
		v.Free()
		return nil, err
	}
	path, err := v.GetPath()
	if err != nil {
		v.Free()
		return nil, err
	}
	if key != expected.BackendKey || path != expected.Path {
		v.Free()
		return nil, domain.Fail("SOURCE_CHANGED", "managed volume identity differs from allocation receipt")
	}
	if expected.Generation != "" {
		identity, err := fileidentity.Observe(path, false)
		if err != nil {
			v.Free()
			return nil, err
		}
		if identity.Generation != expected.Generation {
			v.Free()
			return nil, domain.Fail("SOURCE_CHANGED", "managed file was replaced after allocation; no action attempted")
		}
	}
	return v, nil
}
func requireUnattached(c *native.Connect, volume domain.CreatedVolume) error {
	return requireUnattachedExcept(c, volume, "")
}

// The sole exception is used by read-only acceptance of an exactly matched,
// stopped creation definition. Upload and ordinary verification never use it.
func requireUnattachedExcept(c *native.Connect, volume domain.CreatedVolume, acceptedVM string) error {
	domains, err := c.ListAllDomains(0)
	if err != nil {
		return err
	}
	defer func() {
		for i := range domains {
			domains[i].Free()
		}
	}()
	for i := range domains {
		if acceptedVM != "" {
			id, e := domains[i].GetUUIDString()
			if e != nil {
				return e
			}
			if id == acceptedVM {
				continue
			}
		}
		persistent, e := domains[i].IsPersistent()
		if e != nil {
			return e
		}
		active, e := domains[i].IsActive()
		if e != nil {
			return e
		}
		flags := []native.DomainXMLFlags{}
		if persistent {
			flags = append(flags, native.DOMAIN_XML_INACTIVE)
		}
		if active {
			flags = append(flags, 0)
		}
		for _, flag := range flags {
			x, e := domains[i].GetXMLDesc(flag)
			if e != nil {
				return e
			}
			tree, e := xmlTree(x)
			if e != nil {
				return e
			}
			var walk func(*xmlNode) bool
			walk = func(n *xmlNode) bool {
				if n.name.Local == "source" && (attr(n, "file") == volume.Path || attr(n, "dev") == volume.Path || attr(n, "volume") == volume.Intent.Name) {
					return true
				}
				for _, c := range n.children {
					if walk(c) {
						return true
					}
				}
				return false
			}
			if walk(tree) {
				return domain.Fail("RESOURCE_BUSY", "new volume is referenced by a VM; upload/offline verification refused")
			}
		}
	}
	return nil
}
func watchStream(ctx context.Context, s *native.Stream) func() {
	done, joined := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-ctx.Done():
			_ = s.Abort()
		case <-done:
		}
	}()
	return func() { close(done); <-joined }
}
func (p *Provider) PopulateVolume(ctx context.Context, uri string, expected domain.CreatedVolume, source io.Reader) error {
	if err := validateVolume(expected.Intent); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c, err := connect(uri, true)
	if err != nil {
		return err
	}
	defer c.Close()
	v, err := lookupCreated(c, expected)
	if err != nil {
		return err
	}
	defer v.Free()
	if err = requireUnattached(c, expected); err != nil {
		return err
	}
	stream, err := c.NewStream(0)
	if err != nil {
		return err
	}
	defer stream.Free()
	join := watchStream(ctx, stream)
	defer join()
	finished := false
	defer func() {
		if !finished {
			_ = stream.Abort()
		}
	}()
	if err = v.Upload(stream, 0, expected.Intent.FileBytes, 0); err != nil {
		return err
	}
	h := sha256.New()
	remaining := expected.Intent.FileBytes
	var sourceErr error
	err = stream.SendAll(func(_ *native.Stream, n int) ([]byte, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if remaining == 0 {
			return nil, nil
		}
		if n > 1<<20 {
			n = 1 << 20
		}
		if uint64(n) > remaining {
			n = int(remaining)
		}
		b := make([]byte, n)
		count, e := source.Read(b)
		if e != nil && e != io.EOF {
			sourceErr = e
			return nil, e
		}
		if count == 0 {
			sourceErr = io.ErrUnexpectedEOF
			return nil, sourceErr
		}
		remaining -= uint64(count)
		_, _ = h.Write(b[:count])
		return b[:count], nil
	})
	if err != nil {
		return err
	}
	if sourceErr != nil {
		return sourceErr
	}
	if remaining != 0 || hex.EncodeToString(h.Sum(nil)) != expected.Intent.SHA256 {
		return domain.Fail("SOURCE_CHANGED", "uploaded source size/hash differs; partial volume retained")
	}
	if err = stream.Finish(); err != nil {
		return err
	}
	finished = true
	return nil
}
func (p *Provider) VerifyCreatedVolume(ctx context.Context, uri string, expected domain.CreatedVolume) error {
	if err := validateVolume(expected.Intent); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// libvirt protects raw volume data with a non-read-only connection even
	// though this adapter only downloads. This runs inside an authorized job.
	c, err := connect(uri, true)
	if err != nil {
		return err
	}
	defer c.Close()
	v, err := lookupCreated(c, expected)
	if err != nil {
		return err
	}
	defer v.Free()
	if err = requireUnattached(c, expected); err != nil {
		return err
	}
	x, err := v.GetXMLDesc(0)
	if err != nil {
		return err
	}
	if err = verifyVolumeMetadata(x, expected.Intent); err != nil {
		return err
	}
	stream, err := c.NewStream(0)
	if err != nil {
		return err
	}
	defer stream.Free()
	join := watchStream(ctx, stream)
	defer join()
	finished := false
	defer func() {
		if !finished {
			_ = stream.Abort()
		}
	}()
	if err = v.Download(stream, 0, 0, 0); err != nil {
		return err
	}
	h := sha256.New()
	var size uint64
	err = stream.RecvAll(func(_ *native.Stream, b []byte) (int, error) {
		if e := ctx.Err(); e != nil {
			return 0, e
		}
		size += uint64(len(b))
		if size > expected.Intent.FileBytes {
			return 0, errors.New("volume exceeds approved physical size")
		}
		return h.Write(b)
	})
	if err != nil {
		return err
	}
	if size != expected.Intent.FileBytes || hex.EncodeToString(h.Sum(nil)) != expected.Intent.SHA256 {
		return domain.Fail("RECOVERY_REQUIRED", "stored volume bytes differ from the prepared artifact")
	}
	if err = stream.Finish(); err != nil {
		return err
	}
	finished = true
	return nil
}
func (p *Provider) DefineCreatedVM(ctx context.Context, uri string, t domain.CreationTarget, volumes []domain.CreatedVolume, binding string) (domain.VM, error) {
	var empty domain.VM
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	x, err := creationXML(t, volumes, binding)
	if err != nil {
		return empty, err
	}
	c, err := connect(uri, true)
	if err != nil {
		return empty, err
	}
	defer c.Close()
	if err = absentDomain(c, t.Spec.UUID, t.Spec.Name); err != nil {
		return empty, err
	}
	if err = checkCreationNetworks(c, uri, t.Networks); err != nil {
		return empty, err
	}
	for _, expected := range volumes {
		v, e := lookupCreated(c, expected)
		if e != nil {
			return empty, e
		}
		v.Free()
	}
	d, err := c.DomainDefineXMLFlags(x, native.DOMAIN_DEFINE_VALIDATE)
	if err != nil {
		return empty, err
	}
	defer d.Free()
	return observe(d, uri)
}
func (p *Provider) ObserveCreatedVM(ctx context.Context, uri string, t domain.CreationTarget, volumes []domain.CreatedVolume, binding string) (domain.VM, bool, error) {
	var empty domain.VM
	if err := ctx.Err(); err != nil {
		return empty, false, err
	}
	c, err := connect(uri, false)
	if err != nil {
		return empty, false, err
	}
	defer c.Close()
	d, err := c.LookupDomainByUUIDString(t.Spec.UUID)
	if err != nil {
		var e native.Error
		if errors.As(err, &e) && e.Code == native.ERR_NO_DOMAIN {
			return empty, false, nil
		}
		return empty, false, err
	}
	defer d.Free()
	v, err := observe(d, uri)
	if err != nil {
		return empty, false, err
	}
	wanted, err := creationXML(t, volumes, binding)
	if err != nil {
		return v, false, err
	}
	if err = matchesCreationPolicy(wanted, v.PersistentXML, t.Spec.DevicePolicy); err != nil {
		return v, false, err
	}
	if err = checkCreationNetworks(c, uri, t.Networks); err != nil {
		return v, false, err
	}
	for _, expected := range volumes {
		disk, e := lookupCreated(c, expected)
		if e != nil {
			return v, false, e
		}
		disk.Free()
	}
	return v, true, nil
}

func checkCreationNetworks(c *native.Connect, uri string, expected []domain.VirtualNetwork) error {
	for _, want := range expected {
		n, err := c.LookupNetworkByUUIDString(want.Key.UUID)
		if err != nil {
			return err
		}
		got, err := observeNetwork(n, uri)
		n.Free()
		if err != nil {
			return err
		}
		if !got.Active || got.Name != want.Name || got.Fingerprint != want.Fingerprint {
			return domain.Fail("SOURCE_CHANGED", "selected network identity or configuration changed; definition is not confirmed")
		}
	}
	return nil
}

var _ domain.CreationBackend = (*Provider)(nil)
