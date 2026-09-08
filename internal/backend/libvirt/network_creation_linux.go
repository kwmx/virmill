//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net"
	"os"
	"strings"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/backend/networkxml"
	"virmill.local/core/internal/domain"
)

const networkCreationInventoryLimit = 4096
const networkCreationXMLLimit = 1 << 20

var _ domain.NetworkCreationProvider = (*Provider)(nil)

type networkCreationHandle interface {
	networkObservation
	Create() error
	Free() error
}

type networkCreationConnection interface {
	networks(context.Context) ([]domain.VirtualNetwork, error)
	interfaces(context.Context) ([]string, error)
	ipv4Forwarding(context.Context) (bool, error)
	lookup(string) (networkCreationHandle, error)
	define(string) (networkCreationHandle, error)
	close() error
}

type networkCreationOpen func(string, bool) (networkCreationConnection, error)

func (p *Provider) CheckNetworkCreation(ctx context.Context, uri string, def domain.NetworkDefinition) error {
	_, err := networkCreationRun(ctx, uri, def, "check", openNetworkCreation)
	return err
}

func (p *Provider) DefineNetwork(ctx context.Context, uri string, def domain.NetworkDefinition) error {
	_, err := networkCreationRun(ctx, uri, def, "define", openNetworkCreation)
	return err
}

func (p *Provider) ActivateNetwork(ctx context.Context, uri string, def domain.NetworkDefinition) error {
	_, err := networkCreationRun(ctx, uri, def, "activate", openNetworkCreation)
	return err
}

func (p *Provider) InspectCreatedNetwork(ctx context.Context, uri string, def domain.NetworkDefinition) (domain.VirtualNetwork, error) {
	return networkCreationRun(ctx, uri, def, "inspect", openNetworkCreation)
}

func networkCreationUncertain(reason string) error {
	return domain.Fail("RECOVERY_REQUIRED", "network creation "+reason+"; inspect the original UUID without replay, replacement or removal")
}

// DefineXML has no create-only compare-and-swap flag. These checks refuse every
// observed collision but cannot exclude an authorized external writer between
// the last observation and the native call. The caller must coordinate those
// writers. Define and activation are separate effects; neither method retries,
// undefines, destroys, sets autostart or repairs an existing network.
// Context checks surround synchronous native calls; they cannot interrupt a C
// call already in progress. Cancellation after submission remains uncertain.
func networkCreationRun(ctx context.Context, uri string, def domain.NetworkDefinition, action string, open networkCreationOpen) (out domain.VirtualNetwork, err error) {
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = Connection(uri); err != nil {
		return out, err
	}
	if err = networkxml.Validate(def); err != nil {
		return out, err
	}
	if action != "check" && action != "define" && action != "activate" && action != "inspect" {
		return out, domain.Fail("INVALID_INPUT", "unknown owned network operation")
	}
	c, err := open(uri, action == "define" || action == "activate")
	if err != nil {
		return out, err
	}
	var handle networkCreationHandle
	submitted := false
	defer func() {
		var cleanup error
		if handle != nil {
			cleanup = handle.Free()
		}
		cleanup = errors.Join(cleanup, c.close())
		if cleanup != nil {
			if submitted {
				err = networkCreationUncertain("cleanup failed after native submission")
			} else {
				err = errors.Join(err, cleanup)
			}
		}
		if ctx.Err() != nil {
			if submitted {
				err = networkCreationUncertain("was canceled after native submission")
			} else {
				err = ctx.Err()
			}
		}
		if err != nil {
			out = domain.VirtualNetwork{}
		}
	}()
	if action == "check" || action == "define" {
		if err = checkNetworkCollisions(ctx, c, uri, def, false); err != nil || action == "check" {
			return out, err
		}
		raw, renderErr := networkxml.Render(def)
		if renderErr != nil {
			return out, renderErr
		}
		if err = checkNetworkCollisions(ctx, c, uri, def, false); err != nil {
			return out, err
		}
		if err = ctx.Err(); err != nil {
			return out, err
		}
		submitted = true
		handle, err = c.define(raw)
		if err != nil {
			return out, networkCreationUncertain("definition acknowledgement was lost or refused")
		}
		out, err = inspectNetworkDefinition(handle, uri, def)
		if err != nil || out.Active {
			return domain.VirtualNetwork{}, networkCreationUncertain("new definition is not the exact inactive persistent network")
		}
		return out, nil
	}
	handle, err = c.lookup(def.UUID)
	if err != nil {
		return out, err
	}
	first, err := inspectNetworkDefinition(handle, uri, def)
	if err != nil {
		return out, err
	}
	if action == "activate" {
		if first.Active {
			return out, domain.Fail("STALE_PLAN", "network is already active; activation is never replayed")
		}
		if err = checkNetworkCollisions(ctx, c, uri, def, true); err != nil {
			return out, err
		}
	}
	latest, err := inspectNetworkDefinition(handle, uri, def)
	if err != nil {
		return out, err
	}
	if latest.Fingerprint != first.Fingerprint {
		return out, domain.Fail("STALE_PLAN", "network changed during creation observation")
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if action == "inspect" {
		return latest, nil
	}
	submitted = true
	if err = handle.Create(); err != nil {
		return out, networkCreationUncertain("activation acknowledgement was lost or refused")
	}
	out, err = inspectNetworkDefinition(handle, uri, def)
	if err != nil || !out.Active {
		return domain.VirtualNetwork{}, networkCreationUncertain("activation is not the exact active persistent network")
	}
	return out, nil
}

func inspectNetworkDefinition(handle networkCreationHandle, uri string, def domain.NetworkDefinition) (domain.VirtualNetwork, error) {
	out, err := observeNetwork(handle, uri)
	if err != nil {
		return domain.VirtualNetwork{}, err
	}
	if err = matchNetworkDefinition(out, uri, def); err != nil {
		return domain.VirtualNetwork{}, err
	}
	// Ownership remains a separate durable service fact. Neither exact XML nor
	// libvirt's active bit certifies host access, routing or packet isolation.
	return out, nil
}

func matchNetworkDefinition(out domain.VirtualNetwork, uri string, def domain.NetworkDefinition) error {
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "network", UUID: def.UUID}
	if out.Key != key || out.Name != def.Name || !out.Persistent || out.Autostart || out.PersistentXML == "" || len(out.PersistentXML) > networkCreationXMLLimit || len(out.LiveXML) > networkCreationXMLLimit {
		return domain.Fail("SOURCE_CHANGED", "created network identity, persistence or autostart differs")
	}
	if err := networkxml.Match(out.PersistentXML, def); err != nil {
		return domain.Fail("SOURCE_CHANGED", "created network persistent XML differs from the reviewed definition")
	}
	if out.Active {
		if err := networkxml.Match(out.LiveXML, def); err != nil {
			return domain.Fail("SOURCE_CHANGED", "created network live XML differs from the reviewed definition")
		}
	} else if out.LiveXML != "" {
		return domain.Fail("SOURCE_CHANGED", "inactive network has an ambiguous live observation")
	}
	return nil
}

func checkNetworkCollisions(ctx context.Context, c networkCreationConnection, uri string, def domain.NetworkDefinition, allowSelected bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	all, err := c.networks(ctx)
	if err != nil {
		return err
	}
	if len(all) > networkCreationInventoryLimit {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "network collision inventory exceeds bounds")
	}
	seen := map[string]bool{}
	names := map[string]bool{}
	selected := false
	used := 0
	for _, current := range all {
		if err := ctx.Err(); err != nil {
			return err
		}
		if current.Key.ProviderID != "libvirt" || current.Key.ConnectionID != uri || current.Key.Kind != "network" || !uuidPattern.MatchString(current.Key.UUID) || current.Name == "" || seen[current.Key.UUID] || names[current.Name] || (!current.Active && !current.Persistent) {
			return domain.Fail("SOURCE_CHANGED", "incomplete or ambiguous existing network identities")
		}
		seen[current.Key.UUID], names[current.Name] = true, true
		if err := inventoryBudget(current, &used); err != nil {
			return err
		}
		isSelected := allowSelected && current.Key.UUID == def.UUID
		if isSelected {
			if err := matchNetworkDefinition(current, uri, def); err != nil {
				return err
			}
			if current.Active {
				return domain.Fail("STALE_PLAN", "network activated during creation preflight")
			}
			selected = true
		} else if current.Key.UUID == def.UUID || current.Name == def.Name {
			return domain.Fail("RESOURCE_BUSY", "network UUID or name already exists; refusing replacement")
		}
		for _, observation := range []struct {
			present bool
			raw     string
		}{{current.Persistent, current.PersistentXML}, {current.Active, current.LiveXML}} {
			if !observation.present {
				if observation.raw != "" {
					return domain.Fail("SOURCE_CHANGED", "unexpected existing network XML")
				}
				continue
			}
			bridge, err := networkCollisionBridge(observation.raw, current.Key.UUID, current.Name)
			if err != nil {
				return err
			}
			if !isSelected && bridge == def.Bridge {
				return domain.Fail("RESOURCE_BUSY", "bridge name is already reserved by another network")
			}
		}
	}
	if allowSelected && !selected {
		return domain.Fail("SOURCE_CHANGED", "selected network disappeared from complete inventory")
	}
	interfaces, err := c.interfaces(ctx)
	if err != nil {
		return err
	}
	if len(interfaces) > 2*networkCreationInventoryLimit {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "host interface collision inventory exceeds bounds")
	}
	for _, name := range interfaces {
		if name == "" || len(name) > 15 || strings.ContainsAny(name, "/\x00\n\r\t") {
			return domain.Fail("SOURCE_CHANGED", "host interface identity is ambiguous")
		}
		if name == def.Bridge {
			return domain.Fail("RESOURCE_BUSY", "requested bridge already exists as a host interface")
		}
	}
	if def.Type == "nat" {
		enabled, err := c.ipv4Forwarding(ctx)
		if err != nil {
			return err
		}
		if !enabled {
			return domain.Fail("UNSUPPORTED_CAPABILITY", "NAT creation requires pre-existing IPv4 forwarding; no global host setup change is authorized")
		}
	}
	return ctx.Err()
}

// Observe only the direct native identity and bridge fields of every foreign
// network. Unknown native configuration is retained by libvirt, never rendered
// or edited. Ambiguous relevant fields and malformed XML cannot prove absence.
func networkCollisionBridge(raw, uuid, name string) (string, error) {
	invalid := func() error {
		return domain.Fail("SOURCE_CHANGED", "existing network XML identity or bridge is ambiguous")
	}
	if raw == "" || len(raw) > networkCreationXMLLimit {
		return "", invalid()
	}
	d := xml.NewDecoder(strings.NewReader(raw))
	depth, roots, tokens := 0, 0, 0
	declaration := false
	seen := map[string]bool{}
	values := map[string]string{}
	field, bridge := "", ""
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", invalid()
		}
		tokens++
		if tokens > 65536 {
			return "", invalid()
		}
		switch v := token.(type) {
		case xml.StartElement:
			depth++
			if depth > 32 {
				return "", invalid()
			}
			attrs := map[xml.Name]bool{}
			for _, a := range v.Attr {
				if attrs[a.Name] {
					return "", invalid()
				}
				attrs[a.Name] = true
			}
			if depth == 1 {
				roots++
				if roots != 1 || v.Name != (xml.Name{Local: "network"}) {
					return "", invalid()
				}
			} else if depth == 2 && (v.Name.Local == "name" || v.Name.Local == "uuid" || v.Name.Local == "bridge") {
				if v.Name.Space != "" || seen[v.Name.Local] {
					return "", invalid()
				}
				seen[v.Name.Local] = true
				field = v.Name.Local
				if field == "bridge" {
					for _, a := range v.Attr {
						if a.Name.Local == "name" {
							if a.Name.Space != "" || bridge != "" || a.Value == "" || len(a.Value) > 15 || strings.ContainsAny(a.Value, "/\x00 \n\r\t") {
								return "", invalid()
							}
							bridge = a.Value
						}
					}
					if bridge == "" {
						return "", invalid()
					}
				} else if len(v.Attr) != 0 {
					return "", invalid()
				}
			} else if field != "" {
				return "", invalid()
			}
		case xml.EndElement:
			if depth == 2 {
				field = ""
			}
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(v)) != "" {
				return "", invalid()
			}
			if field != "" {
				if len(values[field])+len(v) > 4096 {
					return "", invalid()
				}
				values[field] += string(v)
			}
		case xml.Directive:
			return "", invalid()
		case xml.ProcInst:
			if v.Target != "xml" || roots != 0 || declaration {
				return "", invalid()
			}
			declaration = true
		}
	}
	if roots != 1 || depth != 0 || strings.TrimSpace(values["uuid"]) != uuid || strings.TrimSpace(values["name"]) != name || strings.TrimSpace(values["bridge"]) != "" {
		return "", invalid()
	}
	return bridge, nil
}

type nativeNetworkCreation struct {
	connection *native.Connect
	uri        string
}

func openNetworkCreation(uri string, write bool) (networkCreationConnection, error) {
	c, err := connect(uri, write)
	if err != nil {
		return nil, err
	}
	return &nativeNetworkCreation{connection: c, uri: uri}, nil
}
func (s *nativeNetworkCreation) close() error { _, err := s.connection.Close(); return err }
func (s *nativeNetworkCreation) lookup(id string) (networkCreationHandle, error) {
	return s.connection.LookupNetworkByUUIDString(id)
}
func (s *nativeNetworkCreation) define(raw string) (networkCreationHandle, error) {
	return s.connection.NetworkDefineXMLFlags(raw, native.NETWORK_DEFINE_VALIDATE)
}
func (s *nativeNetworkCreation) networks(ctx context.Context) (out []domain.VirtualNetwork, err error) {
	all, err := s.connection.ListAllNetworks(0)
	if err != nil {
		return nil, err
	}
	defer func() {
		for i := range all {
			err = errors.Join(err, all[i].Free())
		}
		if err != nil {
			out = nil
		}
	}()
	if len(all) > networkCreationInventoryLimit {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "native network inventory exceeds bounds")
	}
	used := 0
	for i := range all {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		value, e := observeNetwork(&all[i], s.uri)
		if e != nil {
			return nil, e
		}
		if e = inventoryBudget(value, &used); e != nil {
			return nil, e
		}
		out = append(out, value)
	}
	return out, ctx.Err()
}
func (s *nativeNetworkCreation) interfaces(ctx context.Context) (out []string, err error) {
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	kernel, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	if len(kernel) > networkCreationInventoryLimit {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "kernel interface inventory exceeds bounds")
	}
	for _, item := range kernel {
		out = append(out, item.Name)
	}
	configured, err := s.connection.ListAllInterfaces(0)
	if err != nil {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "complete libvirt configured-interface inventory unavailable")
	}
	defer func() {
		for i := range configured {
			err = errors.Join(err, configured[i].Free())
		}
		if err != nil {
			out = nil
		}
	}()
	if len(configured) > networkCreationInventoryLimit {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "configured interface inventory exceeds bounds")
	}
	for i := range configured {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		name, e := configured[i].GetName()
		if e != nil {
			return nil, e
		}
		out = append(out, name)
	}
	return out, ctx.Err()
}

func (s *nativeNetworkCreation) ipv4Forwarding(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	// This fixed procfs observation never writes sysctls. Libvirt's NAT start
	// can otherwise enable global forwarding as an implicit side effect.
	f, err := os.Open("/proc/sys/net/ipv4/ip_forward")
	if err != nil {
		return false, domain.Fail("UNSUPPORTED_CAPABILITY", "IPv4 forwarding prerequisite cannot be observed")
	}
	data, err := io.ReadAll(io.LimitReader(f, 8))
	err = errors.Join(err, f.Close())
	if err != nil || (string(data) != "0\n" && string(data) != "1\n") {
		return false, domain.Fail("UNSUPPORTED_CAPABILITY", "IPv4 forwarding prerequisite is not a bounded native boolean")
	}
	return string(data) == "1\n", ctx.Err()
}
