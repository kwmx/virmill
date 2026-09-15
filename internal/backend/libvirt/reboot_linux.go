//go:build linux && cgo

package libvirt

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
)

const rebootEventWait = 60 * time.Second

var _ domain.RebootProvider = (*Provider)(nil)

// Libvirt requires event registration before any connection is opened. Keep
// initialization failure local to reboot; ordinary provider calls then work
// without events.
//
// Once a default implementation is registered, libvirt releases a closed
// connection's socket only on a later loop iteration. The one process-lifetime
// loop therefore starts with the process: when it waited for the first reboot,
// every connection closed before then kept its socket open.
var rebootEvents = struct {
	initialization error
	start          sync.Once
	running        atomic.Bool
	failed         chan struct{}
}{failed: make(chan struct{})}

func init() {
	rebootEvents.initialization = native.EventRegisterDefaultImpl()
	if rebootEvents.initialization == nil {
		_ = startRebootEvents()
	}
}

func startRebootEvents() error {
	if rebootEvents.initialization != nil {
		return domain.Fail("UNSUPPORTED_CAPABILITY", "native reboot event implementation is unavailable")
	}
	rebootEvents.start.Do(func() {
		rebootEvents.running.Store(true)
		go func() {
			for {
				if err := native.EventRunDefaultImpl(); err != nil {
					close(rebootEvents.failed)
					return
				}
			}
		}()
	})
	select {
	case <-rebootEvents.failed:
		return domain.Fail("UNSUPPORTED_CAPABILITY", "native reboot event loop failed")
	default:
		return nil
	}
}

// Reboot requests one graceful native reboot and observes a subsequent selected
// domain event. A running VM is never evidence of reboot completion. Libvirt
// does not correlate this event with a request ID: it is an observed event, not
// proof that no other authorized client or guest initiated a concurrent reboot.
//
// The bound applies to waiting for the event. The official binding's synchronous
// Connect/Observe/Reboot/cleanup C calls cannot be interrupted by context. Check
// context around them; never free a native handle while a C call still owns it.
func (p *Provider) Reboot(ctx context.Context, uri, id, reviewed string) (domain.RebootObservation, error) {
	return rebootWith(ctx, uri, id, reviewed, rebootHooks{
		open: func(uri, id string) (rebootSession, error) {
			if err := startRebootEvents(); err != nil {
				return nil, err
			}
			return openNativeReboot(uri, id)
		},
		loopFailed: rebootEvents.failed,
		wait:       rebootEventWait,
	})
}

type rebootSession interface {
	observe() (domain.VM, error)
	subscribe(func(rebootSignal)) (func() error, error)
	request() error
	close() error
}

type rebootHooks struct {
	open       func(string, string) (rebootSession, error)
	loopFailed <-chan struct{}
	wait       time.Duration
}

type rebootSignal struct {
	uuid   string
	failed bool
}

// Callbacks never block on the request or its consumer, and retained memory is
// constant even if the native source sends repeated or unrelated events.
type rebootSignals struct {
	mu      sync.Mutex
	id      string
	armed   bool
	closed  bool
	failed  bool
	at      time.Time
	changed chan struct{}
}

func (s *rebootSignals) receive(event rebootSignal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if event.failed {
		s.failed = true
	} else if s.armed && event.uuid == s.id && s.at.IsZero() {
		s.at = time.Now().UTC()
	}
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

func (s *rebootSignals) arm() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed {
		return false
	}
	s.armed = true
	return true
}

func (s *rebootSignals) observation() (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.at, s.failed
}

func (s *rebootSignals) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

func rebootUncertain(reason string) error {
	return domain.Fail("RECOVERY_REQUIRED", "graceful reboot "+reason+"; no reboot retry, reset or hard stop attempted")
}

func rebootWith(ctx context.Context, uri, id, reviewed string, hooks rebootHooks) (out domain.RebootObservation, err error) {
	if err = ctx.Err(); err != nil {
		return out, err
	}
	if err = Connection(uri); err != nil {
		return out, err
	}
	if !uuidPattern.MatchString(id) || !digestPattern.MatchString(reviewed) {
		return out, domain.Fail("INVALID_INPUT", "canonical VM UUID and reviewed SHA-256 fingerprint required")
	}
	session, err := hooks.open(uri, id)
	if err != nil {
		return out, err
	}
	key := domain.ResourceKey{ProviderID: "libvirt", ConnectionID: uri, Kind: "vm", UUID: id}
	signals := &rebootSignals{id: id, changed: make(chan struct{}, 1)}
	var unsubscribe func() error
	submitted := false
	defer func() {
		// A callback already in flight may finish after deregistration. Its
		// bounded Go state remains valid and ignores every late event.
		signals.stop()
		var cleanup error
		if unsubscribe != nil {
			cleanup = unsubscribe()
		}
		cleanup = errors.Join(cleanup, session.close())
		if cleanup != nil {
			if submitted {
				err = rebootUncertain("native cleanup failed after submission")
			} else {
				err = errors.Join(err, cleanup)
			}
		}
		if ctx.Err() != nil {
			if submitted {
				err = rebootUncertain("was canceled after submission")
			} else {
				err = ctx.Err()
			}
		}
		select {
		case <-hooks.loopFailed:
			if submitted {
				err = rebootUncertain("event loop failed before completion was returned")
			}
		default:
		}
		if err != nil {
			out = domain.RebootObservation{}
		}
	}()
	health := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-hooks.loopFailed:
			return domain.Fail("UNSUPPORTED_CAPABILITY", "native reboot event loop failed before submission")
		default:
		}
		return nil
	}
	check := func() error {
		if err := health(); err != nil {
			return err
		}
		vm, err := session.observe()
		if err != nil {
			return err
		}
		if vm.Key != key || vm.State != "running" || vm.Fingerprint != reviewed {
			return domain.Fail("STALE_PLAN", "reboot requires the reviewed running VM identity and fingerprint")
		}
		return health()
	}
	if err = check(); err != nil {
		return out, err
	}
	if unsubscribe, err = session.subscribe(signals.receive); err != nil {
		return out, err
	}
	if err = check(); err != nil {
		return out, err
	}
	deadline := time.NewTimer(hooks.wait)
	defer deadline.Stop()
	if err = health(); err != nil {
		return out, err
	}
	// Arm immediately before the single request, so a callback delivered inside
	// Reboot is retained. Events delivered before this boundary cannot qualify.
	if !signals.arm() {
		return out, domain.Fail("STALE_PLAN", "reboot event connection failed before submission")
	}
	submitted = true
	if err = session.request(); err != nil {
		return out, rebootUncertain("request acknowledgement was not received")
	}
	for {
		if ctx.Err() != nil {
			return out, rebootUncertain("was canceled after submission")
		}
		select {
		case <-hooks.loopFailed:
			return out, rebootUncertain("event loop failed after submission")
		case <-deadline.C:
			return out, rebootUncertain("event deadline elapsed")
		default:
		}
		if at, failed := signals.observation(); failed {
			return out, rebootUncertain("event connection or identity observation failed")
		} else if !at.IsZero() {
			return domain.RebootObservation{Resource: key, ObservedAt: at, Evidence: "libvirt-reboot-event"}, nil
		}
		select {
		case <-ctx.Done():
			return out, rebootUncertain("was canceled after submission")
		case <-hooks.loopFailed:
			return out, rebootUncertain("event loop failed after submission")
		case <-deadline.C:
			return out, rebootUncertain("event deadline elapsed")
		case <-signals.changed:
		}
	}
}

type nativeReboot struct {
	connection *native.Connect
	domain     *native.Domain
	uri        string
}

func openNativeReboot(uri, id string) (rebootSession, error) {
	c, err := connect(uri, true)
	if err != nil {
		return nil, err
	}
	d, err := c.LookupDomainByUUIDString(id)
	if err != nil {
		_, closeErr := c.Close()
		return nil, errors.Join(err, closeErr)
	}
	return &nativeReboot{connection: c, domain: d, uri: uri}, nil
}

func (s *nativeReboot) observe() (domain.VM, error) { return observe(s.domain, s.uri) }
func (s *nativeReboot) request() error              { return s.domain.Reboot(0) }
func (s *nativeReboot) close() error {
	err := s.domain.Free()
	_, closeErr := s.connection.Close()
	return errors.Join(err, closeErr)
}

func (s *nativeReboot) subscribe(receive func(rebootSignal)) (func() error, error) {
	if err := s.connection.RegisterCloseCallback(func(_ *native.Connect, _ native.ConnectCloseReason) {
		receive(rebootSignal{failed: true})
	}); err != nil {
		return nil, err
	}
	id, err := s.connection.DomainEventRebootRegister(s.domain, func(_ *native.Connect, d *native.Domain) {
		// Callback Domain values are borrowed; never Free or retain them.
		id, err := d.GetUUIDString()
		receive(rebootSignal{uuid: id, failed: err != nil})
	})
	if err != nil {
		return nil, errors.Join(err, s.connection.UnregisterCloseCallback())
	}
	return func() error {
		return errors.Join(s.connection.DomainEventDeregister(id), s.connection.UnregisterCloseCallback())
	}, nil
}
