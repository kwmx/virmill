//go:build linux && cgo

package libvirt

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"virmill.local/core/internal/domain"
)

// All request tests use this generated in-process seam. No native connection,
// event loop, VM request or IPC is opened by these tests.
type rebootFixture struct {
	vm                         domain.VM
	calls                      []string
	observed, requested, freed int
	unsubscribed               int
	callback                   func(rebootSignal)
	onObserve                  func(int)
	onSubscribe, onRequest     func()
	onUnsubscribe, onClose     func()
	observeError, subscribeErr error
	requestError, cleanupError error
	closeError                 error
}

func (s *rebootFixture) observe() (domain.VM, error) {
	s.calls = append(s.calls, "observe")
	s.observed++
	if s.onObserve != nil {
		s.onObserve(s.observed)
	}
	return s.vm, s.observeError
}
func (s *rebootFixture) subscribe(cb func(rebootSignal)) (func() error, error) {
	s.calls = append(s.calls, "subscribe")
	s.callback = cb
	if s.onSubscribe != nil {
		s.onSubscribe()
	}
	if s.subscribeErr != nil {
		return nil, s.subscribeErr
	}
	return func() error {
		s.calls = append(s.calls, "unsubscribe")
		s.unsubscribed++
		if s.onUnsubscribe != nil {
			s.onUnsubscribe()
		}
		return s.cleanupError
	}, nil
}
func (s *rebootFixture) request() error {
	s.calls = append(s.calls, "request")
	s.requested++
	if s.onRequest != nil {
		s.onRequest()
	}
	return s.requestError
}
func (s *rebootFixture) close() error {
	s.calls = append(s.calls, "close")
	s.freed++
	if s.onClose != nil {
		s.onClose()
	}
	return s.closeError
}

func newRebootFixture() (*rebootFixture, rebootHooks) {
	s := &rebootFixture{vm: domain.VM{
		Key:  domain.ResourceKey{ProviderID: "libvirt", ConnectionID: "qemu:///system", Kind: "vm", UUID: "108708af-93db-402c-bb03-322845296fa4"},
		Name: "generated-reboot-fixture", State: "running", LiveXML: "<domain/>",
	}}
	s.vm.Fingerprint = fingerprint(s.vm)
	return s, rebootHooks{wait: 20 * time.Millisecond, open: func(uri, id string) (rebootSession, error) {
		s.calls = append(s.calls, "open")
		return s, nil
	}}
}

func requireRebootError(t *testing.T, out domain.RebootObservation, err error, code string) {
	t.Helper()
	if out != (domain.RebootObservation{}) {
		t.Fatalf("failure retained a completion observation: %+v", out)
	}
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}

func TestRebootRejectsUnsupportedAndMalformedRequestsBeforeOpening(t *testing.T) {
	for _, uri := range []string{"", "test:///default", "qemu+ssh://host/system", "qemu:///system?socket=/tmp/other"} {
		t.Run(uri, func(t *testing.T) {
			s, hooks := newRebootFixture()
			out, err := rebootWith(context.Background(), uri, s.vm.Key.UUID, s.vm.Fingerprint, hooks)
			requireRebootError(t, out, err, "UNSUPPORTED_CAPABILITY")
			if len(s.calls) != 0 {
				t.Fatal(s.calls)
			}
			out, err = (&Provider{}).Reboot(context.Background(), uri, s.vm.Key.UUID, s.vm.Fingerprint)
			requireRebootError(t, out, err, "UNSUPPORTED_CAPABILITY")
		})
	}
	for _, malformed := range []struct{ id, digest string }{
		{"", strings.Repeat("a", 64)},
		{"not-a-uuid", strings.Repeat("a", 64)},
		{"108708AF-93db-402c-bb03-322845296fa4", strings.Repeat("a", 64)},
		{"108708af-93db-402c-bb03-322845296fa4", ""},
		{"108708af-93db-402c-bb03-322845296fa4", strings.Repeat("A", 64)},
	} {
		s, hooks := newRebootFixture()
		out, err := rebootWith(context.Background(), s.vm.Key.ConnectionID, malformed.id, malformed.digest, hooks)
		requireRebootError(t, out, err, "INVALID_INPUT")
		if len(s.calls) != 0 {
			t.Fatal(s.calls)
		}
	}
}

func TestRebootRepeatsIdentityRunningAndFingerprintAfterSubscription(t *testing.T) {
	for _, phase := range []int{1, 2} {
		for name, change := range map[string]func(*domain.VM){
			"uuid":        func(v *domain.VM) { v.Key.UUID = "a08708af-93db-402c-bb03-322845296fa4" },
			"provider":    func(v *domain.VM) { v.Key.ProviderID = "other" },
			"connection":  func(v *domain.VM) { v.Key.ConnectionID = "qemu:///session" },
			"kind":        func(v *domain.VM) { v.Key.Kind = "other" },
			"state":       func(v *domain.VM) { v.State = "paused" },
			"fingerprint": func(v *domain.VM) { v.Fingerprint = strings.Repeat("0", 64) },
		} {
			t.Run(string(rune('0'+phase))+"/"+name, func(t *testing.T) {
				s, hooks := newRebootFixture()
				key, digest := s.vm.Key, s.vm.Fingerprint
				s.onObserve = func(n int) {
					if n == phase {
						change(&s.vm)
					}
				}
				out, err := rebootWith(context.Background(), key.ConnectionID, key.UUID, digest, hooks)
				requireRebootError(t, out, err, "STALE_PLAN")
				if s.requested != 0 || s.freed != 1 || s.unsubscribed != phase-1 {
					t.Fatal(s.calls)
				}
			})
		}
	}
}

func TestRebootRequiresSelectedEventFollowingRequest(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		pre, foreign, following bool
	}{
		{"running_without_event", false, false, false},
		{"old_event_does_not_qualify", true, false, false},
		{"foreign_event_does_not_qualify", false, true, false},
		{"old_and_foreign_do_not_qualify", true, true, false},
		{"event_during_request_acknowledgement", false, false, true},
		{"old_then_selected_after_request", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, hooks := newRebootFixture()
			var requestedAt time.Time
			s.onSubscribe = func() {
				if tc.pre {
					s.callback(rebootSignal{uuid: s.vm.Key.UUID})
				}
			}
			s.onRequest = func() {
				requestedAt = time.Now()
				if tc.foreign {
					s.callback(rebootSignal{uuid: "a08708af-93db-402c-bb03-322845296fa4"})
				}
				if tc.following {
					s.callback(rebootSignal{uuid: s.vm.Key.UUID})
				}
			}
			out, err := rebootWith(context.Background(), s.vm.Key.ConnectionID, s.vm.Key.UUID, s.vm.Fingerprint, hooks)
			if tc.following {
				if err != nil || out.Resource != s.vm.Key || out.Evidence != "libvirt-reboot-event" || out.ObservedAt.Before(requestedAt) || out.ObservedAt.After(time.Now()) || out.ObservedAt.Location() != time.UTC {
					t.Fatalf("invalid completion: %+v %v", out, err)
				}
			} else {
				requireRebootError(t, out, err, "RECOVERY_REQUIRED")
			}
			want := []string{"open", "observe", "subscribe", "observe", "request", "unsubscribe", "close"}
			if !reflect.DeepEqual(s.calls, want) || s.requested != 1 || s.freed != 1 || s.unsubscribed != 1 {
				t.Fatal("request replay, state-based completion or cleanup failure", s.calls)
			}
		})
	}
}

func TestRebootLostAcknowledgementAndEventFailuresNeverProduceReceipt(t *testing.T) {
	for _, name := range []string{"lost-ack-with-event", "connection-before-request", "connection-after-request", "loop-before-request", "loop-during-second-observation", "loop-after-request", "loop-during-cleanup"} {
		t.Run(name, func(t *testing.T) {
			s, hooks := newRebootFixture()
			failed := make(chan struct{})
			hooks.loopFailed = failed
			code, requests, unsubs := "RECOVERY_REQUIRED", 1, 1
			s.onRequest = func() { s.callback(rebootSignal{uuid: s.vm.Key.UUID}) }
			switch name {
			case "lost-ack-with-event":
				s.requestError = errors.New("generated lost acknowledgement")
			case "connection-before-request":
				code, requests = "STALE_PLAN", 0
				s.onSubscribe = func() { s.callback(rebootSignal{failed: true}) }
			case "connection-after-request":
				s.onRequest = func() {
					s.callback(rebootSignal{uuid: s.vm.Key.UUID})
					s.callback(rebootSignal{failed: true})
				}
			case "loop-before-request":
				code, requests, unsubs = "UNSUPPORTED_CAPABILITY", 0, 0
				close(failed)
			case "loop-during-second-observation":
				code, requests = "UNSUPPORTED_CAPABILITY", 0
				s.onObserve = func(n int) {
					if n == 2 {
						close(failed)
					}
				}
			case "loop-after-request":
				s.onRequest = func() {
					s.callback(rebootSignal{uuid: s.vm.Key.UUID})
					close(failed)
				}
			case "loop-during-cleanup":
				s.onClose = func() { close(failed) }
			}
			out, err := rebootWith(context.Background(), s.vm.Key.ConnectionID, s.vm.Key.UUID, s.vm.Fingerprint, hooks)
			requireRebootError(t, out, err, code)
			if s.requested != requests || s.freed != 1 || s.unsubscribed != unsubs {
				t.Fatal(s.calls)
			}
		})
	}
}

func TestRebootContextDeadlineEndsEventWaitWithoutReplay(t *testing.T) {
	s, hooks := newRebootFixture()
	hooks.wait = time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	started := time.Now()
	out, err := rebootWith(ctx, s.vm.Key.ConnectionID, s.vm.Key.UUID, s.vm.Fingerprint, hooks)
	requireRebootError(t, out, err, "RECOVERY_REQUIRED")
	if ctx.Err() != context.DeadlineExceeded || time.Since(started) > 500*time.Millisecond || s.requested != 1 || s.unsubscribed != 1 || s.freed != 1 {
		t.Fatal("context failed to bound the event wait", ctx.Err(), s.calls)
	}
}

func TestRebootCancellationAndCleanupBoundaries(t *testing.T) {
	for _, phase := range []string{"before-open", "first-observation", "subscription", "second-observation", "request", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			s, hooks := newRebootFixture()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s.onRequest = func() { s.callback(rebootSignal{uuid: s.vm.Key.UUID}) }
			switch phase {
			case "before-open":
				cancel()
			case "first-observation", "second-observation":
				s.onObserve = func(n int) {
					if (phase == "first-observation" && n == 1) || (phase == "second-observation" && n == 2) {
						cancel()
					}
				}
			case "subscription":
				s.onSubscribe = cancel
			case "request":
				s.onRequest = func() { s.callback(rebootSignal{uuid: s.vm.Key.UUID}); cancel() }
			case "cleanup":
				s.onClose = cancel
			}
			out, err := rebootWith(ctx, s.vm.Key.ConnectionID, s.vm.Key.UUID, s.vm.Fingerprint, hooks)
			if phase == "request" || phase == "cleanup" {
				requireRebootError(t, out, err, "RECOVERY_REQUIRED")
				if s.requested != 1 {
					t.Fatal(s.calls)
				}
			} else if !errors.Is(err, context.Canceled) || out != (domain.RebootObservation{}) || s.requested != 0 {
				t.Fatalf("cancellation submitted request: %v %+v %v", s.calls, out, err)
			}
			if phase == "before-open" {
				if len(s.calls) != 0 {
					t.Fatal(s.calls)
				}
			} else if s.freed != 1 {
				t.Fatal(s.calls)
			}
		})
	}
}

func TestRebootSetupAndCleanupErrorsCloseOwnedSession(t *testing.T) {
	for _, phase := range []string{"open", "observe", "subscribe", "unsubscribe", "close"} {
		t.Run(phase, func(t *testing.T) {
			s, hooks := newRebootFixture()
			fault := errors.New("generated " + phase + " failure")
			s.onRequest = func() { s.callback(rebootSignal{uuid: s.vm.Key.UUID}) }
			switch phase {
			case "open":
				hooks.open = func(string, string) (rebootSession, error) { return nil, fault }
			case "observe":
				s.observeError = fault
			case "subscribe":
				s.subscribeErr = fault
			case "unsubscribe":
				s.cleanupError = fault
			case "close":
				s.closeError = fault
			}
			out, err := rebootWith(context.Background(), s.vm.Key.ConnectionID, s.vm.Key.UUID, s.vm.Fingerprint, hooks)
			if phase == "unsubscribe" || phase == "close" {
				requireRebootError(t, out, err, "RECOVERY_REQUIRED")
				if s.requested != 1 || s.unsubscribed != 1 {
					t.Fatal(s.calls)
				}
			} else if !errors.Is(err, fault) || out != (domain.RebootObservation{}) || s.requested != 0 {
				t.Fatalf("setup failure was lost: %v %+v %v", s.calls, out, err)
			}
			if phase != "open" && s.freed != 1 {
				t.Fatal(s.calls)
			}
			if s.callback != nil {
				// Native callbacks in flight retain only this closed Go state.
				for i := 0; i < 100; i++ {
					s.callback(rebootSignal{uuid: s.vm.Key.UUID})
				}
			}
		})
	}
}

func TestRebootCallbackStateBoundsConcurrentAndLateSignals(t *testing.T) {
	s := &rebootSignals{id: "selected", changed: make(chan struct{}, 1)}
	s.receive(rebootSignal{uuid: "selected"})
	if at, failed := s.observation(); !at.IsZero() || failed {
		t.Fatal(at, failed)
	}
	if !s.arm() {
		t.Fatal("arm refused")
	}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for n := 0; n < 1000; n++ {
				s.receive(rebootSignal{uuid: "foreign"})
				s.receive(rebootSignal{uuid: "selected"})
			}
		}()
	}
	workers.Wait()
	at, failed := s.observation()
	if at.IsZero() || failed || len(s.changed) != 1 {
		t.Fatal(at, failed, len(s.changed))
	}
	s.stop()
	s.receive(rebootSignal{failed: true})
	if after, failed := s.observation(); !after.Equal(at) || failed {
		t.Fatal(after, failed)
	}
}
