//go:build linux && cgo

package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

const (
	usbMaxDevices  = 4096
	usbMaxXML      = 64 << 10
	usbMaxTotalXML = wire.MaxFrame - (128 << 10)
	usbSysfsPrefix = "/sys/devices/"
)

var _ domain.USBInventoryProvider = (*Provider)(nil)

type usbObservation interface {
	GetName() (string, error)
	GetXMLDesc(native.NodeDeviceXMLFlags) (string, error)
}
type usbSession interface {
	devices() ([]usbObservation, error)
	close() error
}
type usbFacts struct {
	device                 domain.USBDevice
	nativePath, generation string
}
type usbEnrich func(context.Context, domain.USBDevice, string) (usbFacts, error)

func usbFailure(reason string) error {
	return domain.Fail("OPERATION_FAILED", "USB inventory incomplete: "+reason)
}

// InspectUSB enumerates local native USB devices read-only. Native calls and
// sysfs reads are synchronous; context is checked around them, not used to
// abandon a goroutine or close a descriptor while another call owns it.
func (p *Provider) InspectUSB(ctx context.Context, uri string) ([]domain.USBDevice, error) {
	return inspectUSB(ctx, uri, openUSB, (usbSysfs{root: "/sys/devices"}).observe)
}

func inspectUSB(ctx context.Context, uri string, open func(string) (usbSession, error), enrich usbEnrich) (out []domain.USBDevice, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if err = Connection(uri); err != nil {
		return nil, err
	}
	s, err := open(uri)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, usbNativeError(err)
	}
	defer func() {
		if cleanup := s.close(); cleanup != nil && err == nil {
			err = usbFailure("native handle cleanup failed; details withheld")
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			out = nil
		}
	}()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	devices, err := s.devices()
	if err != nil {
		return nil, usbNativeError(err)
	}
	return collectUSB(ctx, len(devices), func(i int) usbObservation { return devices[i] }, enrich)
}

// Two complete per-device projections detect observed replacement or changing
// identity. This is not an atomic snapshot or physical reconnect qualification.
func collectUSB(ctx context.Context, count int, get func(int) usbObservation, enrich usbEnrich) ([]domain.USBDevice, error) {
	if count < 0 || count > usbMaxDevices {
		return nil, usbFailure("native device count exceeds 4096")
	}
	facts := make([]usbFacts, 0, count)
	bytes := 0
	for pass := 0; pass < 2; pass++ {
		names, addresses, paths := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for i := 0; i < count; i++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			n := get(i)
			name, err := n.GetName()
			if err != nil {
				return nil, usbNativeError(err)
			}
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			raw, err := n.GetXMLDesc(0)
			if err != nil {
				return nil, usbNativeError(err)
			}
			if err = ctx.Err(); err != nil {
				return nil, err
			}
			bytes += len(raw)
			if bytes > usbMaxTotalXML {
				return nil, usbFailure("aggregate native XML exceeds response bounds")
			}
			d, nativePath, err := parseUSBDevice(name, raw)
			if err != nil {
				return nil, err
			}
			if names[d.Name] || nativePath != "" && paths[nativePath] {
				return nil, usbFailure("duplicate native name or sysfs path")
			}
			names[d.Name] = true
			paths[nativePath] = nativePath != ""
			f, err := enrich(ctx, d, nativePath)
			if err != nil {
				return nil, err
			}
			if f.device.Bus != nil && f.device.Device != nil {
				address := fmt.Sprintf("%d/%d", *f.device.Bus, *f.device.Device)
				if addresses[address] {
					return nil, usbFailure("duplicate current USB bus/device address")
				}
				addresses[address] = true
			}
			if pass == 0 {
				facts = append(facts, f)
			} else if !reflect.DeepEqual(facts[i], f) {
				return nil, domain.Fail("SOURCE_CHANGED", "USB identity or sysfs metadata changed during discovery")
			}
		}
	}
	out := make([]domain.USBDevice, 0, count)
	keys := map[string]int{}
	for _, f := range facts {
		d := f.device
		switch {
		case d.Serial != "":
			d.StableID = "usb:" + d.VendorID + ":" + d.ProductID + ":serial:" + url.PathEscape(d.Serial)
			d.Reason = "Observed serial match key; discovery does not establish attachment safety or reconnect continuity."
		case d.PhysicalPort != "":
			d.StableID = "usb:" + d.VendorID + ":" + d.ProductID + ":port:" + url.PathEscape(d.PhysicalPort)
			d.Reason = "Serial unavailable; explicit physical-port selection required. Observed topology includes root-bus numbering and may change."
		default:
			d.Ambiguous = true
			d.Reason = "Serial and usable physical-port evidence unavailable; vendor/product or ephemeral addresses cannot select a persistent identity."
		}
		if d.Bus == nil || d.Device == nil {
			d.Reason += " Current USB address is incomplete."
		}
		if d.Serial != "" && d.PhysicalPort == "" {
			d.Reason += " Physical-port evidence is unavailable."
		}
		if d.StableID != "" {
			keys[d.StableID]++
		}
		out = append(out, d)
	}
	for i := range out {
		if out[i].StableID != "" && keys[out[i].StableID] > 1 {
			out[i].Ambiguous = true
			out[i].Reason = "Duplicate observed USB match key; no device selected. Inspect distinct physical ports explicitly before any attachment."
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	used := 0
	if err := inventoryBudget(out, &used); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseUSBDevice(nativeName, raw string) (domain.USBDevice, string, error) {
	bad := func() (domain.USBDevice, string, error) {
		return domain.USBDevice{}, "", usbFailure("malformed, foreign, ambiguous or oversized native USB XML; contents withheld")
	}
	if len(raw) == 0 || len(raw) > usbMaxXML {
		return bad()
	}
	// The reused bounded native tree rejects namespaces, directives and duplicate
	// attributes. Also refuse repeated XML declarations before its root.
	decoder := xml.NewDecoder(strings.NewReader(raw))
	declarations := 0
	for {
		t, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return bad()
		}
		if p, ok := t.(xml.ProcInst); ok && p.Target == "xml" {
			declarations++
			if declarations > 1 {
				return bad()
			}
		}
	}
	root, err := pciXMLTree(raw)
	if err != nil {
		return bad()
	}
	name, err := pciUniqueChild(root, "name", true)
	if err != nil {
		return bad()
	}
	label, err := pciText(name, 256, true)
	if err != nil || label != nativeName || label != name.text {
		return bad()
	}
	d := domain.USBDevice{Name: label}
	p, err := pciUniqueChild(root, "path", false)
	if err != nil {
		return bad()
	}
	nativePath := ""
	if p != nil {
		nativePath, err = pciText(p, 4096, true)
		if err != nil || nativePath != p.text || !usbPathValid(nativePath) {
			return bad()
		}
	}
	cap, err := pciUniqueChild(root, "capability", true)
	if err != nil || !onlyAttrs(cap, []string{"type"}, nil) || attr(cap, "type") != "usb_device" || strings.TrimSpace(cap.text) != "" {
		return bad()
	}
	for _, n := range cap.children {
		switch n.name.Local {
		case "bus", "device", "vendor", "product":
		default:
			return bad()
		}
	}
	for _, field := range []struct {
		name  string
		value **uint
		max   uint64
	}{{"bus", &d.Bus, 65535}, {"device", &d.Device, 127}} {
		n, err := pciUniqueChild(cap, field.name, false)
		if err != nil {
			return bad()
		}
		if n == nil {
			continue
		}
		value, err := pciText(n, 16, true)
		if err != nil || value != n.text {
			return bad()
		}
		number, ok := usbDecimal(value, field.max)
		if !ok {
			return bad()
		}
		*field.value = &number
	}
	for _, field := range []struct {
		name      string
		id, label *string
	}{{"vendor", &d.VendorID, &d.Vendor}, {"product", &d.ProductID, &d.Product}} {
		n, err := pciUniqueChild(cap, field.name, true)
		if err != nil || len(n.attrs) != 1 || n.attrs[0].Name.Local != "id" || len(n.children) != 0 {
			return bad()
		}
		id := attr(n, "id")
		if len(id) != 6 || !strings.HasPrefix(id, "0x") {
			return bad()
		}
		decoded, err := hex.DecodeString(id[2:])
		if err != nil || len(decoded) != 2 {
			return bad()
		}
		*field.id = "0x" + strings.ToLower(id[2:])
		if !usbTextValid(n.text, 1024) {
			return bad()
		}
		*field.label = n.text
	}
	return d, nativePath, nil
}

func usbTextValid(s string, limit int) bool {
	if len(s) > limit || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
func usbPathValid(p string) bool {
	return strings.HasPrefix(p, usbSysfsPrefix) && len(p) > len(usbSysfsPrefix) && path.Clean(p) == p && usbTextValid(p, 4096)
}
func usbDecimal(s string, max uint64) (uint, bool) {
	if s == "" || len(s) > 10 {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 32)
	return uint(n), err == nil && n > 0 && n <= max && strconv.FormatUint(n, 10) == s
}

type usbSysfs struct {
	root string
	// Private generated-file seam only; production always requires SYSFS_MAGIC.
	fixture bool
}
type usbStat struct {
	Dev, Ino       uint64
	Mode, UID, GID uint32
	Size           int64
	Mtime, Ctime   unix.Timespec
}

func usbStatFD(fd int) (usbStat, error) {
	var s unix.Stat_t
	if err := unix.Fstat(fd, &s); err != nil {
		return usbStat{}, err
	}
	return usbStat{uint64(s.Dev), s.Ino, s.Mode, s.Uid, s.Gid, s.Size, s.Mtim, s.Ctim}, nil
}
func usbOpenBeneath(root int, name string, directory bool) (int, error) {
	flags := uint64(unix.O_PATH | unix.O_CLOEXEC)
	if directory {
		flags |= unix.O_DIRECTORY
	}
	resolve := uint64(unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS)
	if root != unix.AT_FDCWD {
		resolve |= unix.RESOLVE_BENEATH | unix.RESOLVE_NO_XDEV
	}
	return unix.Openat2(root, name, &unix.OpenHow{Flags: flags, Resolve: resolve})
}

func (s usbSysfs) observe(ctx context.Context, d domain.USBDevice, nativePath string) (out usbFacts, err error) {
	out = usbFacts{device: d, nativePath: nativePath}
	if err = ctx.Err(); err != nil {
		return usbFacts{}, err
	}
	if nativePath == "" {
		return out, nil
	}
	if !usbPathValid(nativePath) {
		return usbFacts{}, usbFailure("native sysfs path is outside the fixed device root")
	}
	root, err := usbOpenBeneath(unix.AT_FDCWD, s.root, true)
	if err != nil {
		return usbFacts{}, usbSysfsError(err)
	}
	defer unix.Close(root)
	var fs unix.Statfs_t
	if err = unix.Fstatfs(root, &fs); err != nil {
		return usbFacts{}, usbSysfsError(err)
	}
	if !s.fixture && fs.Type != unix.SYSFS_MAGIC {
		return usbFacts{}, domain.Fail("UNSUPPORTED_CAPABILITY", "USB attributes require the native sysfs filesystem")
	}
	rootIdentity, err := usbStatFD(root)
	if err != nil {
		return usbFacts{}, usbSysfsError(err)
	}
	relative := strings.TrimPrefix(nativePath, usbSysfsPrefix)
	dir, err := usbOpenBeneath(root, relative, true)
	if err != nil {
		return usbFacts{}, usbSysfsError(err)
	}
	defer unix.Close(dir)
	dirIdentity, err := usbStatFD(dir)
	if err != nil {
		return usbFacts{}, usbSysfsError(err)
	}
	identities := []usbStat{rootIdentity, dirIdentity}
	values := map[string]string{}
	for _, a := range []struct {
		name     string
		limit    int
		optional bool
	}{{"idVendor", 4, false}, {"idProduct", 4, false}, {"busnum", 10, false}, {"devnum", 3, false}, {"serial", 1024, true}, {"devpath", 128, true}} {
		value, identity, err := usbAttribute(ctx, dir, a.name, a.limit, a.optional)
		if err != nil {
			return usbFacts{}, err
		}
		values[a.name] = value
		identities = append(identities, identity)
	}
	for _, pair := range []struct{ attribute, want string }{{"idVendor", d.VendorID}, {"idProduct", d.ProductID}} {
		value := values[pair.attribute]
		if len(value) != 4 || "0x"+value != pair.want {
			return usbFacts{}, domain.Fail("SOURCE_CHANGED", "USB sysfs vendor/product identity differs from native observation")
		}
	}
	bus, ok := usbDecimal(values["busnum"], 65535)
	if !ok {
		return usbFacts{}, usbFailure("malformed sysfs USB bus number")
	}
	device, ok := usbDecimal(values["devnum"], 127)
	if !ok {
		return usbFacts{}, usbFailure("malformed sysfs USB device number")
	}
	if d.Bus != nil && *d.Bus != bus || d.Device != nil && *d.Device != device {
		return usbFacts{}, domain.Fail("SOURCE_CHANGED", "USB sysfs address differs from native observation")
	}
	out.device.Bus, out.device.Device = &bus, &device
	out.device.Serial = values["serial"]
	port := values["devpath"]
	if port == "0" && path.Base(nativePath) != "usb"+strconv.FormatUint(uint64(bus), 10) {
		return usbFacts{}, domain.Fail("SOURCE_CHANGED", "USB root-hub path and observed bus disagree")
	}
	if port != "" && port != "0" {
		segments := strings.Split(port, ".")
		if len(segments) > 7 {
			return usbFacts{}, usbFailure("USB port depth exceeds bounds")
		}
		for _, segment := range segments {
			if _, ok := usbDecimal(segment, 255); !ok {
				return usbFacts{}, usbFailure("malformed USB port path")
			}
		}
		if path.Base(nativePath) != strconv.FormatUint(uint64(bus), 10)+"-"+port {
			return usbFacts{}, domain.Fail("SOURCE_CHANGED", "USB sysfs path and observed port path disagree")
		}
		out.device.PhysicalPort = nativePath + "#port=" + port
	}
	// Reopen through the fixed root, not through /sys/bus/usb symlinks. Include
	// held identities in the second inventory projection to detect replacement.
	for _, check := range []struct {
		base int
		name string
		want usbStat
	}{{unix.AT_FDCWD, s.root, rootIdentity}, {root, relative, dirIdentity}} {
		fd, err := usbOpenBeneath(check.base, check.name, true)
		if err != nil {
			return usbFacts{}, usbSysfsError(err)
		}
		current, e := usbStatFD(fd)
		closeErr := unix.Close(fd)
		if e != nil || closeErr != nil {
			return usbFacts{}, usbFailure("sysfs directory recheck failed")
		}
		if current != check.want {
			return usbFacts{}, domain.Fail("SOURCE_CHANGED", "USB sysfs root or device directory changed during discovery")
		}
	}
	encoded, _ := json.Marshal(identities)
	digest := sha256.Sum256(encoded)
	out.generation = hex.EncodeToString(digest[:])
	if err = ctx.Err(); err != nil {
		return usbFacts{}, err
	}
	return out, nil
}

// Open O_PATH first: even a FIFO/device replacement cannot trigger special-file
// I/O. Reopen only our held regular inode through a private /proc/self/fd number.
// Sysfs reports virtual st_size (often 4096), so bound bytes read independently.
func usbAttribute(ctx context.Context, dir int, name string, limit int, optional bool) (string, usbStat, error) {
	if err := ctx.Err(); err != nil {
		return "", usbStat{}, err
	}
	fd, err := usbOpenBeneath(dir, name, false)
	if optional && errors.Is(err, unix.ENOENT) {
		return "", usbStat{}, nil
	}
	if err != nil {
		return "", usbStat{}, usbSysfsError(err)
	}
	defer unix.Close(fd)
	before, err := usbStatFD(fd)
	if err != nil {
		return "", usbStat{}, usbSysfsError(err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG {
		return "", usbStat{}, usbFailure("USB attribute is not a regular sysfs file")
	}
	readFD, err := unix.Open("/proc/self/fd/"+strconv.Itoa(fd), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", usbStat{}, usbSysfsError(err)
	}
	f := os.NewFile(uintptr(readFD), "private USB attribute")
	data, readErr := io.ReadAll(io.LimitReader(f, int64(limit+2)))
	after, statErr := usbStatFD(readFD)
	closeErr := f.Close()
	if ctx.Err() != nil {
		return "", usbStat{}, ctx.Err()
	}
	if readErr != nil || statErr != nil || closeErr != nil {
		return "", usbStat{}, usbFailure("USB attribute read failed; details withheld")
	}
	namedFD, err := usbOpenBeneath(dir, name, false)
	if err != nil {
		return "", usbStat{}, usbSysfsError(err)
	}
	named, namedErr := usbStatFD(namedFD)
	namedClose := unix.Close(namedFD)
	if namedErr != nil || namedClose != nil || before != after || after != named {
		return "", usbStat{}, domain.Fail("SOURCE_CHANGED", "USB attribute identity changed during discovery")
	}
	value := strings.TrimSuffix(string(data), "\n")
	if len(data) > limit+1 || !usbTextValid(value, limit) {
		return "", usbStat{}, usbFailure("USB attribute contains malformed, control or oversized text")
	}
	return value, before, nil
}

func usbSysfsError(err error) error {
	switch {
	case errors.Is(err, unix.ENOENT), errors.Is(err, unix.ENODEV):
		return domain.Fail("SOURCE_CHANGED", "native USB sysfs observation disappeared")
	case errors.Is(err, unix.EACCES), errors.Is(err, unix.EPERM):
		return domain.Fail("PERMISSION_DENIED", "USB sysfs observation is not readable")
	case errors.Is(err, unix.ENOSYS):
		return domain.Fail("UNSUPPORTED_CAPABILITY", "USB sysfs observation requires secure openat2 resolution")
	default:
		return usbFailure("USB sysfs resolution or read failed; details withheld")
	}
}
func usbNativeError(err error) error {
	var ne native.Error
	if errors.As(err, &ne) {
		switch ne.Code {
		case native.ERR_NO_SUPPORT, native.ERR_OPERATION_UNSUPPORTED:
			return domain.Fail("UNSUPPORTED_CAPABILITY", "native USB node-device inventory is unavailable")
		case native.ERR_ACCESS_DENIED, native.ERR_AUTH_FAILED, native.ERR_OPERATION_DENIED:
			return domain.Fail("PERMISSION_DENIED", "native USB node-device inventory is not readable")
		}
	}
	return usbFailure("native node-device read failed; details withheld")
}

type nativeUSBSession struct {
	conn *native.Connect
	all  []native.NodeDevice
}

func openUSB(uri string) (usbSession, error) {
	c, err := connect(uri, false)
	if err != nil {
		return nil, err
	}
	return &nativeUSBSession{conn: c}, nil
}
func (s *nativeUSBSession) devices() ([]usbObservation, error) {
	all, err := s.conn.ListAllNodeDevices(native.CONNECT_LIST_NODE_DEVICES_CAP_USB_DEV)
	if err != nil {
		return nil, err
	}
	s.all = all
	out := make([]usbObservation, len(all))
	for i := range all {
		out[i] = &s.all[i]
	}
	return out, nil
}
func (s *nativeUSBSession) close() error {
	var failures []error
	for i := range s.all {
		if err := s.all[i].Free(); err != nil {
			failures = append(failures, err)
		}
	}
	_, err := s.conn.Close()
	return errors.Join(append(failures, err)...)
}
