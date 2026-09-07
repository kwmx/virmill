// Package provision renders explicit guest configuration. Rendering is not evidence
// of guest compatibility, cloud-init completion, routing or service readiness.
package provision

import (
	"crypto/elliptic"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"virmill.local/core/internal/domain"
)

const Profile = "nocloud-netplan-ipv4-v1"

type Config struct {
	SourceDiskID        string      `json:"sourceDiskID"`
	Profile             string      `json:"profile"`
	CloudInitCompatible bool        `json:"cloudInitCompatible"`
	SourceSHA256        string      `json:"sourceSHA256"`
	SourceReference     string      `json:"sourceReference"`
	Hostname            string      `json:"hostname"`
	UserName            string      `json:"userName"`
	AuthorizedKeys      []string    `json:"authorizedKeys"`
	PasswordlessSudo    bool        `json:"passwordlessSudo"`
	IPv6                string      `json:"ipv6"`
	MediaID             string      `json:"mediaID"`
	Interfaces          []Interface `json:"interfaces"`
}
type Interface struct {
	NICID        string   `json:"nicID"`
	Role         string   `json:"role"`
	Mode         string   `json:"mode"`
	Address      string   `json:"address"`
	Gateway      string   `json:"gateway"`
	DNS          []string `json:"dns"`
	DefaultRoute bool     `json:"defaultRoute"`
	RouteMetric  int      `json:"routeMetric"`
}

var label = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
var userName = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
var sourceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var sha = regexp.MustCompile(`^[a-f0-9]{64}$`)
var mac = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)

func validMAC(value string) bool {
	if !mac.MatchString(value) {
		return false
	}
	decoded := strings.ReplaceAll(value, ":", "")
	if len(decoded) != 12 {
		return false
	}
	for _, c := range decoded {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	// A clone MAC must be locally administered and unicast; don't require prefix 02.
	var first byte
	for _, c := range value[:2] {
		first = first*16 + byte(strings.IndexRune("0123456789abcdef", c))
	}
	return first&3 == 2
}
func validHostname(name string) bool {
	if len(name) < 1 || len(name) > 253 {
		return false
	}
	for _, part := range strings.Split(name, ".") {
		if !label.MatchString(part) {
			return false
		}
	}
	return true
}
func Validate(c Config, spec domain.CreationSpec) error {
	if c.Profile != Profile || !c.CloudInitCompatible || c.IPv6 != "disabled" {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "this declared NoCloud profile requires a compatible Linux cloud image, Netplan/networkd and explicitly disabled IPv6; other profiles remain unsupported")
	}
	if !sourceID.MatchString(c.SourceDiskID) || !sha.MatchString(c.SourceSHA256) || !sourceID.MatchString(c.MediaID) || !validHostname(c.Hostname) || !userName.MatchString(c.UserName) || c.UserName == "root" || len(c.AuthorizedKeys) < 1 || len(c.AuthorizedKeys) > 32 {
		return domain.Fail("INVALID_INPUT", "explicit source digest, hostname, non-root user, media ID and public SSH keys required")
	}
	reference, err := url.Parse(c.SourceReference)
	if err != nil || len(c.SourceReference) > 2048 || reference.Scheme != "https" || reference.Hostname() == "" || reference.User != nil || reference.RawQuery != "" || reference.Fragment != "" {
		return domain.Fail("INVALID_INPUT", "source provenance reference must be an HTTPS URL without credentials, query or fragment; it is recorded, not fetched or signature-verified")
	}
	keys := map[string]bool{}
	for _, key := range c.AuthorizedKeys {
		canonical, err := PublicKey(key)
		if err != nil {
			return err
		}
		if canonical != key || keys[key] {
			return domain.Fail("INVALID_INPUT", "unique canonical public keys without comments or options required")
		}
		keys[key] = true
	}
	if !uuid.MatchString(spec.UUID) || len(c.Interfaces) != len(spec.NICs) {
		return domain.Fail("INVALID_INPUT", "NoCloud must bind a fresh VM UUID and every explicitly created NIC")
	}
	nicByID := map[string]domain.CreationNIC{}
	macs := map[string]bool{}
	for _, nic := range spec.NICs {
		if nicByID[nic.ID].ID != "" || !validMAC(nic.MAC) || macs[nic.MAC] {
			return domain.Fail("INVALID_INPUT", "unique NIC IDs and unique locally administered clone MACs required")
		}
		nicByID[nic.ID] = nic
		macs[nic.MAC] = true
	}
	seen := map[string]bool{}
	defaults := 0
	subnets := []netip.Prefix{}
	for _, in := range c.Interfaces {
		nic, ok := nicByID[in.NICID]
		if !ok || !sourceID.MatchString(in.NICID) || seen[in.NICID] || in.RouteMetric < 1 || in.RouteMetric > 65535 || (in.Role != "normal" && in.Role != "protected") {
			return domain.Fail("INVALID_INPUT", "unique complete NIC configuration, explicit role and bounded metric required")
		}
		seen[in.NICID] = true
		if in.DefaultRoute {
			defaults++
			if in.Role == "protected" || nic.Link != "up" {
				return domain.Fail("INVALID_INPUT", "a protected or disconnected NIC cannot supply the normal default route")
			}
		}
		if len(in.DNS) > 8 {
			return domain.Fail("INVALID_INPUT", "at most eight explicit IPv4 DNS addresses per NIC")
		}
		for _, value := range in.DNS {
			ip, e := netip.ParseAddr(value)
			if e != nil || !ip.Is4() || !ip.IsGlobalUnicast() {
				return domain.Fail("INVALID_INPUT", "invalid explicit IPv4 DNS address")
			}
		}
		if in.Role == "protected" && len(in.DNS) > 0 {
			return domain.Fail("INVALID_INPUT", "protected NIC cannot set normal guest DNS in this profile")
		}
		switch in.Mode {
		case "none":
			if in.DefaultRoute || in.Address != "" || in.Gateway != "" || len(in.DNS) > 0 {
				return domain.Fail("INVALID_INPUT", "unaddressed NIC cannot declare address, DNS or route")
			}
		case "dhcp":
			if in.Address != "" || in.Gateway != "" {
				return domain.Fail("INVALID_INPUT", "DHCP NIC cannot silently add static addresses or gateway")
			}
		case "static":
			p, e := netip.ParsePrefix(in.Address)
			if e != nil || !p.Addr().Is4() || p.Bits() < 1 || p.Bits() > 30 || !p.Addr().IsGlobalUnicast() || p.String() != in.Address || p.Addr() == p.Masked().Addr() {
				return domain.Fail("INVALID_INPUT", "canonical usable static IPv4 address/prefix required")
			}
			octets := p.Addr().As4()
			number := binary.BigEndian.Uint32(octets[:])
			hostMask := uint32(1)<<uint32(32-p.Bits()) - 1
			if number&hostMask == hostMask {
				return domain.Fail("INVALID_INPUT", "broadcast address cannot identify a guest")
			}
			for _, other := range subnets {
				if p.Overlaps(other) {
					return domain.Fail("INVALID_INPUT", "overlapping static guest NIC subnets require a separately supported routing profile")
				}
			}
			subnets = append(subnets, p)
			if in.DefaultRoute != (in.Gateway != "") {
				return domain.Fail("INVALID_INPUT", "static default route requires its explicit gateway, and only on the default NIC")
			}
			if in.Gateway != "" {
				g, e := netip.ParseAddr(in.Gateway)
				if e != nil || !g.Is4() || !g.IsGlobalUnicast() || !p.Contains(g) || g == p.Addr() || g == p.Masked().Addr() {
					return domain.Fail("INVALID_INPUT", "static gateway must be another usable address in the selected subnet")
				}
				a := g.As4()
				if binary.BigEndian.Uint32(a[:])&hostMask == hostMask {
					return domain.Fail("INVALID_INPUT", "broadcast gateway refused")
				}
			}
		default:
			return domain.Fail("INVALID_INPUT", "explicit none, DHCP or static IPv4 mode required")
		}
	}
	if defaults > 1 {
		return domain.Fail("INVALID_INPUT", "only one normal IPv4 default route is supported")
	}
	return nil
}

// Render binds metadata and MAC matches to this exact creation identity. It emits
// JSON (a YAML subset) under the required cloud-config header, never interpolated
// shell code or arbitrary user YAML. Only public SSH authentication is accepted.
func Render(c Config, spec domain.CreationSpec) (map[string][]byte, error) {
	if err := Validate(c, spec); err != nil {
		return nil, err
	}
	user := map[string]any{"name": c.UserName, "lock_passwd": true, "ssh_authorized_keys": c.AuthorizedKeys, "shell": "/bin/bash"}
	if c.PasswordlessSudo {
		user["sudo"] = []string{"ALL=(ALL) NOPASSWD:ALL"}
	}
	userData := map[string]any{"hostname": c.Hostname, "preserve_hostname": false, "manage_etc_hosts": true, "users": []any{user}, "disable_root": true, "ssh_pwauth": false, "ssh_deletekeys": true, "growpart": map[string]any{"mode": "off"}, "resize_rootfs": false, "package_update": false, "package_upgrade": false, "package_reboot_if_required": false}
	metadata := map[string]any{"instance-id": "virmill-" + spec.UUID, "local-hostname": c.Hostname}
	nicByID := map[string]domain.CreationNIC{}
	for _, n := range spec.NICs {
		nicByID[n.ID] = n
	}
	ethernets := map[string]any{}
	for _, in := range c.Interfaces {
		n := nicByID[in.NICID]
		nic := map[string]any{"match": map[string]any{"macaddress": n.MAC}, "dhcp4": in.Mode == "dhcp", "dhcp6": false, "accept-ra": false, "link-local": []string{}}
		if in.Mode == "dhcp" {
			nic["dhcp4-overrides"] = map[string]any{"use-routes": in.DefaultRoute, "route-metric": in.RouteMetric, "use-dns": in.DefaultRoute && len(in.DNS) == 0, "use-domains": false, "use-hostname": false}
		}
		if in.Mode == "static" {
			nic["addresses"] = []string{in.Address}
			if in.DefaultRoute {
				nic["routes"] = []any{map[string]any{"to": "0.0.0.0/0", "via": in.Gateway, "metric": in.RouteMetric}}
			}
		}
		if len(in.DNS) > 0 {
			nic["nameservers"] = map[string]any{"addresses": in.DNS}
		}
		ethernets[in.NICID] = nic
	}
	network := map[string]any{"version": 2, "renderer": "networkd", "ethernets": ethernets}
	out := map[string][]byte{}
	for name, value := range map[string]any{"user-data": userData, "meta-data": metadata, "network-config": network} {
		b, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		if name == "user-data" {
			b = append([]byte("#cloud-config\n"), b...)
		}
		out[name] = append(b, '\n')
	}
	return out, nil
}

// PublicKey validates supported public key wire structures. Private keys,
// authorized_keys options and comments are not configuration values in this API.
func PublicKey(value string) (string, error) {
	invalid := func() (string, error) {
		return "", domain.Fail("INVALID_INPUT", "expected one canonical Ed25519, RSA (2048+ bits), or NIST ECDSA public key without options/comments")
	}
	if len(value) > 16384 || strings.ContainsAny(value, "\r\n\t") {
		return invalid()
	}
	parts := strings.Split(value, " ")
	if len(parts) != 2 {
		return invalid()
	}
	blob, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil || base64.StdEncoding.EncodeToString(blob) != parts[1] {
		return invalid()
	}
	field := func() ([]byte, error) {
		if len(blob) < 4 {
			return nil, errors.New("short key")
		}
		n := binary.BigEndian.Uint32(blob[:4])
		blob = blob[4:]
		if uint64(n) > uint64(len(blob)) {
			return nil, errors.New("short field")
		}
		out := blob[:int(n)]
		blob = blob[int(n):]
		return out, nil
	}
	kind, err := field()
	if err != nil || string(kind) != parts[0] {
		return invalid()
	}
	switch parts[0] {
	case "ssh-ed25519":
		key, e := field()
		if e != nil || len(key) != 32 {
			return invalid()
		}
	case "ssh-rsa":
		eBytes, e := field()
		if e != nil || !positiveMPInt(eBytes) || len(eBytes) > 4 {
			return invalid()
		}
		exponent := new(big.Int).SetBytes(eBytes)
		if exponent.Int64() < 3 || exponent.Bit(0) == 0 {
			return invalid()
		}
		n, e := field()
		if e != nil || !positiveMPInt(n) {
			return invalid()
		}
		bits := new(big.Int).SetBytes(n).BitLen()
		if bits < 2048 || bits > 16384 {
			return invalid()
		}
	case "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521":
		curveName, e := field()
		if e != nil || "ecdsa-sha2-"+string(curveName) != parts[0] {
			return invalid()
		}
		var curve elliptic.Curve
		switch string(curveName) {
		case "nistp256":
			curve = elliptic.P256()
		case "nistp384":
			curve = elliptic.P384()
		case "nistp521":
			curve = elliptic.P521()
		default:
			return invalid()
		}
		point, e := field()
		if e != nil {
			return invalid()
		}
		x, _ := elliptic.Unmarshal(curve, point)
		if x == nil {
			return invalid()
		}
	default:
		return invalid()
	}
	if len(blob) != 0 {
		return invalid()
	}
	return value, nil
}
func positiveMPInt(b []byte) bool {
	return len(b) > 0 && b[0]&0x80 == 0 && (b[0] != 0 || len(b) > 1 && b[1]&0x80 != 0)
}
