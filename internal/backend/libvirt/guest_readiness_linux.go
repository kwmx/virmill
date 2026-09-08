//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

const (
	guestReadinessPing           = `{"execute":"guest-ping"}`
	guestReadinessReplyLimit     = 4096
	guestReadinessTimeoutSeconds = 5
	guestReadinessChannel        = "org.qemu.guest_agent.0"
)

var _ domain.GuestReadinessProvider = (*Provider)(nil)

type guestReadinessObservation struct {
	vm        domain.VM
	runtimeID uint
}

// The seam owns its handles until close. No goroutine frees a handle while a
// synchronous native call may still be using it.
type guestReadinessSession interface {
	observe() (guestReadinessObservation, error)
	command(string, native.DomainQemuAgentCommandTimeout, uint32) (string, error)
	close() error
}
type guestReadinessOpen func(string, string) (guestReadinessSession, error)

// InspectGuestReadiness sends at most one fixed nonmutating guest-ping. The
// positive native timeout is at most five seconds, shortened to whole seconds
// remaining in the caller deadline. Synchronous connection, observation and
// cleanup C calls cannot be interrupted by context; this is not a total native
// transaction deadline. Context is checked before and after each boundary.
func (p *Provider) InspectGuestReadiness(ctx context.Context, uri, id string) (domain.GuestReadiness, error) {
	return inspectGuestReadiness(ctx, uri, id, openGuestReadiness)
}

func inspectGuestReadiness(ctx context.Context, uri, id string, open guestReadinessOpen) (out domain.GuestReadiness, err error) {
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = Connection(uri); err != nil {
		return out, err
	}
	if !uuidPattern.MatchString(id) || id == "00000000-0000-0000-0000-000000000000" {
		return out, domain.Fail("INVALID_INPUT", "guest readiness requires a canonical nonzero VM UUID")
	}
	s, err := open(uri, id)
	if err != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, guestReadinessNativeError(err)
	}
	defer func() {
		if cleanup := s.close(); cleanup != nil && err == nil {
			err = domain.Fail("OPERATION_FAILED", "native guest readiness handle cleanup failed; details withheld")
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			out = domain.GuestReadiness{}
		}
	}()
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}
	read := func() (guestReadinessObservation, string, error) {
		if err := ctx.Err(); err != nil {
			return guestReadinessObservation{}, "", err
		}
		v, err := s.observe()
		if ctx.Err() != nil {
			return guestReadinessObservation{}, "", ctx.Err()
		}
		if err != nil {
			return guestReadinessObservation{}, "", guestReadinessNativeError(err)
		}
		if v.vm.Key != key || v.vm.State != "running" || v.vm.LiveXML == "" || v.runtimeID == uint(^uint32(0)) || v.runtimeID == ^uint(0) {
			return guestReadinessObservation{}, "", domain.Fail("SOURCE_CHANGED", "selected VM must be running with an exact live native identity")
		}
		state, err := guestReadinessChannelState(v.vm.LiveXML, id, v.runtimeID)
		return v, state, err
	}
	before, state, err := read()
	if err != nil {
		return out, err
	}
	out = domain.GuestReadiness{Resource: key, State: state, Evidence: "libvirt-live-channel-" + state}
	if state == "connected" {
		out.AgentConnected = true
		seconds, err := guestReadinessSeconds(ctx)
		if err != nil {
			return domain.GuestReadiness{}, err
		}
		reply, commandErr := s.command(guestReadinessPing, native.DomainQemuAgentCommandTimeout(seconds), 0)
		if err = ctx.Err(); err != nil {
			return domain.GuestReadiness{}, err
		}
		out.State, out.Evidence = "unresponsive", "guest-ping-unresponsive"
		if commandErr != nil {
			var ne native.Error
			if !errors.As(commandErr, &ne) {
				return domain.GuestReadiness{}, guestReadinessNativeError(commandErr)
			}
			switch ne.Code {
			case native.ERR_AGENT_UNRESPONSIVE:
			case native.ERR_AGENT_COMMAND_TIMEOUT:
				out.Evidence = "guest-ping-timeout"
			case native.ERR_AGENT_UNSYNCED:
				out.Evidence = "guest-ping-unsynchronized"
			case native.ERR_AGENT_COMMAND_FAILED:
				out.Evidence = "guest-ping-rejected"
			default:
				return domain.GuestReadiness{}, guestReadinessNativeError(commandErr)
			}
		} else {
			accepted, err := guestReadinessReply(reply)
			if err != nil {
				return domain.GuestReadiness{}, err
			}
			if accepted {
				out.State, out.AgentResponsive, out.Evidence = "responsive", true, "libvirt-guest-ping"
			} else {
				out.Evidence = "guest-ping-rejected"
			}
		}
	}
	after, finalState, err := read()
	if err != nil {
		return domain.GuestReadiness{}, err
	}
	if after.runtimeID != before.runtimeID || after.vm.Name != before.vm.Name || finalState != state {
		return domain.GuestReadiness{}, domain.Fail("SOURCE_CHANGED", "selected VM runtime identity or agent channel changed during observation")
	}
	out.ObservedAt = time.Now().UTC()
	return out, nil
}

func guestReadinessSeconds(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	seconds := guestReadinessTimeoutSeconds
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline) / time.Second
		if remaining < 1 {
			return 0, domain.Fail("WAIT_TIMEOUT", "less than one whole second remains for the bounded native guest ping; no command sent")
		}
		if remaining < time.Duration(seconds) {
			seconds = int(remaining)
		}
	}
	return seconds, nil
}

// Reply text remains private, including error descriptions. Exact key checks
// avoid encoding/json's case-insensitive struct aliases; wire rejects duplicate
// keys, trailing values, malformed Unicode and excessive nesting.
func guestReadinessReply(raw string) (bool, error) {
	invalid := func() (bool, error) {
		return false, domain.Fail("OPERATION_FAILED", "guest ping returned an invalid or oversized protocol response; contents withheld")
	}
	if len(raw) == 0 || len(raw) > guestReadinessReplyLimit {
		return invalid()
	}
	var reply map[string]json.RawMessage
	if wire.Decode([]byte(raw), &reply) != nil || len(reply) != 1 {
		return invalid()
	}
	if value, ok := reply["return"]; ok {
		var result map[string]json.RawMessage
		if json.Unmarshal(value, &result) != nil || result == nil || len(result) != 0 {
			return invalid()
		}
		return true, nil
	}
	if value, ok := reply["error"]; ok {
		var failure map[string]json.RawMessage
		if json.Unmarshal(value, &failure) != nil || len(failure) != 2 {
			return invalid()
		}
		for _, key := range []string{"class", "desc"} {
			var value string
			if json.Unmarshal(failure[key], &value) != nil || value == "" {
				return invalid()
			}
		}
		return false, nil
	}
	return invalid()
}

func guestReadinessChannelState(raw, id string, runtimeID uint) (string, error) {
	invalid := func() (string, error) {
		return "", domain.Fail("OPERATION_FAILED", "live guest-agent XML identity or channel observation is malformed, ambiguous or unsupported; contents withheld")
	}
	root, err := coldStateTree(raw)
	if err != nil {
		return invalid()
	}
	for _, a := range root.attrs {
		if a.Name.Local == "id" && a.Name.Space != "" {
			return invalid()
		}
	}
	if attr(root, "id") != strconv.FormatUint(uint64(runtimeID), 10) {
		return invalid()
	}
	uuid, err := coldChild(root, "uuid", true)
	if err != nil || coldAttrs(uuid, nil, nil) != nil || len(uuid.children) != 0 || uuid.text != id {
		return invalid()
	}
	devices, err := coldChild(root, "devices", true)
	if err != nil {
		return invalid()
	}
	state, found := "absent", false
	for _, channel := range devices.children {
		if channel.name.Local != "channel" {
			continue
		}
		if channel.name.Space != "" {
			return invalid()
		}
		target, err := coldChild(channel, "target", false)
		if err != nil {
			return invalid()
		}
		if target == nil {
			continue
		}
		for _, a := range target.attrs {
			if (a.Name.Local == "type" || a.Name.Local == "name" || a.Name.Local == "state") && a.Name.Space != "" {
				return invalid()
			}
		}
		if attr(target, "name") != guestReadinessChannel {
			continue
		}
		if found || coldAttrs(target, []string{"type", "name"}, []string{"state"}) != nil || len(target.children) != 0 || strings.TrimSpace(target.text) != "" || attr(target, "type") != "virtio" {
			return invalid()
		}
		for _, a := range channel.attrs {
			if a.Name.Local == "type" && a.Name.Space != "" {
				return invalid()
			}
		}
		if attr(channel, "type") != "unix" {
			return invalid()
		}
		found = true
		state = attr(target, "state")
		switch state {
		case "":
			state = "unknown"
		case "connected", "disconnected":
		default:
			return invalid()
		}
	}
	return state, nil
}

func guestReadinessNativeError(err error) error {
	var observation *domain.Error
	if errors.As(err, &observation) && observation.Code == "SOURCE_CHANGED" {
		return domain.Fail("SOURCE_CHANGED", "selected VM runtime identity changed during native observation")
	}
	var ne native.Error
	if errors.As(err, &ne) {
		switch ne.Code {
		case native.ERR_NO_SUPPORT, native.ERR_OPERATION_UNSUPPORTED:
			return domain.Fail("UNSUPPORTED_CAPABILITY", "native guest-agent observation is unavailable on this connection")
		case native.ERR_ACCESS_DENIED, native.ERR_AUTH_FAILED, native.ERR_OPERATION_DENIED:
			return domain.Fail("PERMISSION_DENIED", "native access for the fixed guest-agent observation was denied")
		case native.ERR_NO_DOMAIN:
			return domain.Fail("SOURCE_CHANGED", "selected VM is no longer available for guest-agent observation")
		}
	}
	return domain.Fail("OPERATION_FAILED", "native guest-agent observation failed; details withheld")
}

type nativeGuestReadiness struct {
	conn *native.Connect
	dom  *native.Domain
	uri  string
}

func openGuestReadiness(uri, id string) (guestReadinessSession, error) {
	// virDomainQemuAgentCommand requires domain/write access even for guest-ping.
	c, err := connect(uri, true)
	if err != nil {
		return nil, err
	}
	d, err := c.LookupDomainByUUIDString(id)
	if err != nil {
		_, _ = c.Close()
		return nil, err
	}
	return &nativeGuestReadiness{conn: c, dom: d, uri: uri}, nil
}

func (s *nativeGuestReadiness) observe() (guestReadinessObservation, error) {
	id, err := s.dom.GetID()
	if err != nil {
		return guestReadinessObservation{}, err
	}
	vm, err := observe(s.dom, s.uri)
	if err != nil {
		return guestReadinessObservation{}, err
	}
	after, err := s.dom.GetID()
	if err != nil {
		return guestReadinessObservation{}, err
	}
	if id != after {
		return guestReadinessObservation{}, domain.Fail("SOURCE_CHANGED", "VM runtime identity changed during native observation")
	}
	return guestReadinessObservation{vm: vm, runtimeID: id}, nil
}
func (s *nativeGuestReadiness) command(command string, timeout native.DomainQemuAgentCommandTimeout, flags uint32) (string, error) {
	return s.dom.QemuAgentCommand(command, timeout, flags)
}
func (s *nativeGuestReadiness) close() error {
	freeErr := s.dom.Free()
	_, closeErr := s.conn.Close()
	return errors.Join(freeErr, closeErr)
}
