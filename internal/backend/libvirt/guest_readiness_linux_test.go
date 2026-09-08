//go:build linux && cgo

package libvirt

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	native "libvirt.org/go/libvirt"
	"virmill.local/core/internal/domain"
)

const readinessTestID = "38ef8646-93f3-4b8e-b550-8b7568892a8b"
const readinessTestURI = "qemu:///system"

type readinessFake struct {
	values                        []guestReadinessObservation
	reads, commands, closes       int
	readErr, commandErr, closeErr error
	readFail                      int
	reply                         string
	commandText                   string
	timeout                       native.DomainQemuAgentCommandTimeout
	flags                         uint32
	onRead                        func(int)
	onCommand, onClose            func()
}

func (s *readinessFake) observe() (guestReadinessObservation, error) {
	s.reads++
	if s.onRead != nil {
		s.onRead(s.reads)
	}
	if s.reads == s.readFail {
		return guestReadinessObservation{}, s.readErr
	}
	index := s.reads - 1
	if index >= len(s.values) {
		index = len(s.values) - 1
	}
	return s.values[index], nil
}
func (s *readinessFake) command(text string, timeout native.DomainQemuAgentCommandTimeout, flags uint32) (string, error) {
	s.commands++
	s.commandText, s.timeout, s.flags = text, timeout, flags
	if s.onCommand != nil {
		s.onCommand()
	}
	return s.reply, s.commandErr
}
func (s *readinessFake) close() error {
	s.closes++
	if s.onClose != nil {
		s.onClose()
	}
	return s.closeErr
}
func (s *readinessFake) open(uri, id string) (guestReadinessSession, error) {
	if uri != readinessTestURI || id != readinessTestID {
		panic("unexpected selected identity")
	}
	return s, nil
}

func readinessXML(state string) string {
	channel := ""
	if state != "absent" {
		value := ""
		if state != "unknown" {
			value = " state='" + state + "'"
		}
		channel = "<channel type='unix'><source mode='bind' path='/private/agent.sock'/><target type='virtio' name='org.qemu.guest_agent.0'" + value + "/><alias name='channel0'/><address type='virtio-serial' controller='0' bus='0' port='1'/></channel>"
	}
	return "<domain type='kvm' id='7'><name>Selected VM</name><uuid>" + readinessTestID + "</uuid><metadata><x:private xmlns:x='urn:private'>secret XML bytes</x:private></metadata><devices><channel type='spicevmc'><target type='virtio' name='com.redhat.spice.0'/></channel>" + channel + "</devices></domain>"
}

func readinessFixture(state string) *readinessFake {
	v := domain.VM{Key: domain.ResourceKey{ProviderID: "libvirt", ConnectionID: readinessTestURI, Kind: "vm", UUID: readinessTestID}, Name: "Selected VM", State: "running", LiveXML: readinessXML(state), PersistentXML: readinessXML("connected")}
	return &readinessFake{values: []guestReadinessObservation{{vm: v, runtimeID: 7}}, reply: `{"return":{}}`}
}

func readinessErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("error=%v, want %s", err, code)
	}
	if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "/private/") {
		t.Fatalf("opaque data leaked: %v", err)
	}
}

func TestGuestReadinessObservedStatesAndFixedPing(t *testing.T) {
	for _, state := range []string{"absent", "unknown", "disconnected", "connected"} {
		t.Run(state, func(t *testing.T) {
			s := readinessFixture(state)
			start := time.Now()
			out, err := inspectGuestReadiness(context.Background(), readinessTestURI, readinessTestID, s.open)
			if err != nil {
				t.Fatal(err)
			}
			wantState, wantEvidence, wantCommands := state, "libvirt-live-channel-"+state, 0
			if state == "connected" {
				wantState, wantEvidence, wantCommands = "responsive", "libvirt-guest-ping", 1
			}
			if out.Resource != s.values[0].vm.Key || out.State != wantState || out.Evidence != wantEvidence || out.AgentConnected != (wantCommands == 1) || out.AgentResponsive != (wantCommands == 1) {
				t.Fatalf("unexpected observation: %+v", out)
			}
			if out.ObservedAt.Before(start) || out.ObservedAt.After(time.Now()) || out.ObservedAt.Location() != time.UTC {
				t.Fatal("observation lacks final UTC time")
			}
			if s.reads != 2 || s.commands != wantCommands || s.closes != 1 {
				t.Fatalf("reads=%d commands=%d closes=%d", s.reads, s.commands, s.closes)
			}
			if s.commands == 1 && (s.commandText != `{"execute":"guest-ping"}` || s.timeout != 5 || s.flags != 0) {
				t.Fatalf("unexpected native request %q/%d/%d", s.commandText, s.timeout, s.flags)
			}
			encoded, _ := json.Marshal(out)
			for _, private := range []string{"/private/", "secret XML", "PersistentXML", "LiveXML"} {
				if strings.Contains(string(encoded), private) {
					t.Fatalf("private XML leaked in %s", encoded)
				}
			}
		})
	}
}

func TestGuestReadinessNativeNegativeEvidenceAndNoRetry(t *testing.T) {
	for _, tc := range []struct {
		name     string
		code     native.ErrorNumber
		evidence string
	}{
		{"unresponsive", native.ERR_AGENT_UNRESPONSIVE, "guest-ping-unresponsive"},
		{"timeout", native.ERR_AGENT_COMMAND_TIMEOUT, "guest-ping-timeout"},
		{"sync", native.ERR_AGENT_UNSYNCED, "guest-ping-unsynchronized"},
		{"rejected", native.ERR_AGENT_COMMAND_FAILED, "guest-ping-rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := readinessFixture("connected")
			s.commandErr = native.Error{Code: tc.code, Message: "SECRET guest payload"}
			out, err := inspectGuestReadiness(context.Background(), readinessTestURI, readinessTestID, s.open)
			if err != nil {
				t.Fatal(err)
			}
			if out.State != "unresponsive" || !out.AgentConnected || out.AgentResponsive || out.Evidence != tc.evidence || s.commands != 1 || s.reads != 2 || s.closes != 1 {
				t.Fatalf("out=%+v seam=%+v", out, s)
			}
		})
	}
	s := readinessFixture("connected")
	s.reply = `{"error":{"class":"CommandDisabled","desc":"SECRET command policy"}}`
	out, err := inspectGuestReadiness(context.Background(), readinessTestURI, readinessTestID, s.open)
	if err != nil || out.State != "unresponsive" || out.Evidence != "guest-ping-rejected" || out.AgentResponsive || s.reads != 2 || s.commands != 1 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestGuestReadinessRefusesWrongSelectionAndChangingRuntime(t *testing.T) {
	for _, stage := range []string{"before", "after"} {
		for _, mutation := range []string{"uuid", "provider", "uri", "kind", "stopped", "paused", "blocked", "live XML absent", "XML UUID", "runtime ID", "name", "channel"} {
			// A new valid runtime/name/channel is only a conflict relative to
			// the observation preceding this call's ping.
			if stage == "before" && (mutation == "runtime ID" || mutation == "name" || mutation == "channel") {
				continue
			}
			t.Run(stage+"/"+mutation, func(t *testing.T) {
				s := readinessFixture("connected")
				v := s.values[0]
				switch mutation {
				case "uuid":
					v.vm.Key.UUID = "28ef8646-93f3-4b8e-b550-8b7568892a8b"
				case "provider":
					v.vm.Key.ProviderID = "foreign"
				case "uri":
					v.vm.Key.ConnectionID = "qemu:///session"
				case "kind":
					v.vm.Key.Kind = "network"
				case "stopped", "paused", "blocked":
					v.vm.State = mutation
				case "live XML absent":
					v.vm.LiveXML = ""
				case "XML UUID":
					v.vm.LiveXML = strings.Replace(v.vm.LiveXML, readinessTestID, "28ef8646-93f3-4b8e-b550-8b7568892a8b", 1)
				case "runtime ID":
					v.runtimeID = 8
					v.vm.LiveXML = strings.Replace(v.vm.LiveXML, "id='7'", "id='8'", 1)
				case "name":
					v.vm.Name = "Renamed VM"
				case "channel":
					v.vm.LiveXML = readinessXML("disconnected")
				}
				if stage == "before" {
					s.values[0] = v
				} else {
					s.values = append(s.values, v)
				}
				out, err := inspectGuestReadiness(context.Background(), readinessTestURI, readinessTestID, s.open)
				if err == nil || !reflect.DeepEqual(out, domain.GuestReadiness{}) || s.closes != 1 {
					t.Fatalf("out=%+v err=%v closes=%d", out, err, s.closes)
				}
				want := 0
				if stage == "after" {
					want = 1
				}
				if s.commands != want {
					t.Fatalf("commands=%d want=%d", s.commands, want)
				}
			})
		}
	}
}

func TestGuestReadinessRejectsMalformedLiveChannelXML(t *testing.T) {
	base := readinessXML("connected")
	for name, raw := range map[string]string{
		"duplicate UUID":      strings.Replace(base, "</uuid>", "</uuid><uuid>"+readinessTestID+"</uuid>", 1),
		"foreign UUID":        strings.ReplaceAll(base, "uuid>", "x:uuid>"),
		"duplicate devices":   strings.Replace(base, "</devices>", "</devices><devices/>", 1),
		"foreign channel":     strings.Replace(base, "<channel type='unix'>", "<channel xmlns='urn:foreign' type='unix'>", 1),
		"foreign target":      strings.Replace(base, "<target type='virtio' name='org.qemu", "<target xmlns='urn:foreign' type='virtio' name='org.qemu", 1),
		"foreign name":        strings.Replace(base, "name='org.qemu", "x:name='org.qemu", 1),
		"foreign state":       strings.Replace(base, "state='connected'", "x:state='connected'", 1),
		"duplicate state":     strings.Replace(base, "state='connected'", "state='connected' state='disconnected'", 1),
		"duplicate agent":     strings.Replace(base, "</devices>", "<channel type='unix'><target type='virtio' name='org.qemu.guest_agent.0' state='connected'/></channel></devices>", 1),
		"duplicate target":    strings.Replace(base, "<alias name='channel0'/>", "<target type='virtio' name='different'/>", 1),
		"invalid state":       strings.Replace(base, "state='connected'", "state='CONNECTED'", 1),
		"empty state":         strings.Replace(base, "state='connected'", "state=''", 1),
		"wrong target type":   strings.Replace(base, "type='virtio' name='org.qemu", "type='xen' name='org.qemu", 1),
		"unsupported backend": strings.Replace(base, "type='unix'", "type='pty'", 1),
		"child target":        strings.Replace(base, "state='connected'/>", "state='connected'><name>other</name></target>", 1),
		"stale live ID":       strings.Replace(base, "id='7'", "id='8'", 1),
		"no live ID":          strings.Replace(base, " id='7'", "", 1),
		"foreign live ID":     strings.Replace(base, "id='7'", "x:id='7'", 1),
		"DTD":                 "<!DOCTYPE domain>" + base,
		"trailing domain":     base + base,
		"malformed":           base[:len(base)-3],
		"size":                strings.Repeat(" ", coldStateXMLLimit) + base,
		"depth":               strings.Replace(base, "</metadata>", strings.Repeat("<a>", 33)+strings.Repeat("</a>", 33)+"</metadata>", 1),
	} {
		t.Run(name, func(t *testing.T) {
			s := readinessFixture("connected")
			s.values[0].vm.LiveXML = raw
			out, err := inspectGuestReadiness(context.Background(), readinessTestURI, readinessTestID, s.open)
			readinessErrorCode(t, err, "OPERATION_FAILED")
			if out != (domain.GuestReadiness{}) || s.commands != 0 || s.closes != 1 {
				t.Fatalf("out=%+v commands=%d closes=%d", out, s.commands, s.closes)
			}
		})
	}
}

func TestGuestReadinessPingProtocolBoundsAndAmbiguity(t *testing.T) {
	for _, raw := range []string{`{"return":{}}`, " \n{\"return\": { }}\n"} {
		ok, err := guestReadinessReply(raw)
		if err != nil || !ok {
			t.Fatalf("valid ping refused: %v", err)
		}
	}
	for i, raw := range []string{"", `null`, `[]`, `{}`, `{"Return":{}}`, `{"return":null}`, `{"return":[]}`, `{"return":true}`, `{"return":{"unexpected":1}}`, `{"return":{},"id":1}`, `{"return":{},"error":{}}`, `{"return":{},"return":{}}`, `{"return":{},"ret\u0075rn":{}}`, `{"return":{}} {}`, `{"error":{"class":"x","class":"y","desc":"x"}}`, `{"error":{"class":"x"}}`, `{"error":{"Class":"x","desc":"x"}}`, `{"error":{"class":"x","desc":null}}`, `{"error":{"class":"x","desc":"\ud800"}}`, "{\"return\":{}}\xff", strings.Repeat(" ", guestReadinessReplyLimit) + `{"return":{}}`} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			s := readinessFixture("connected")
			s.reply = raw
			out, err := inspectGuestReadiness(context.Background(), readinessTestURI, readinessTestID, s.open)
			readinessErrorCode(t, err, "OPERATION_FAILED")
			if out != (domain.GuestReadiness{}) || s.commands != 1 || s.closes != 1 {
				t.Fatalf("out=%+v commands=%d closes=%d", out, s.commands, s.closes)
			}
		})
	}
}

func TestGuestReadinessContextTimeoutAndCleanup(t *testing.T) {
	t.Run("canceled failing connection", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		out, err := inspectGuestReadiness(ctx, readinessTestURI, readinessTestID, func(string, string) (guestReadinessSession, error) {
			cancel()
			return nil, errors.New("SECRET connection error")
		})
		if !errors.Is(err, context.Canceled) || out != (domain.GuestReadiness{}) {
			t.Fatalf("out=%+v err=%v", out, err)
		}
	})
	for _, phase := range []string{"before open", "after open", "initial observation", "command", "final observation", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := readinessFixture("connected")
			opens := 0
			if phase == "before open" {
				cancel()
			}
			s.onRead = func(n int) {
				if (phase == "initial observation" && n == 1) || (phase == "final observation" && n == 2) {
					cancel()
				}
			}
			if phase == "command" {
				s.onCommand = cancel
			}
			if phase == "cleanup" {
				s.onClose = cancel
			}
			out, err := inspectGuestReadiness(ctx, readinessTestURI, readinessTestID, func(uri, id string) (guestReadinessSession, error) {
				opens++
				if phase == "after open" {
					cancel()
				}
				return s.open(uri, id)
			})
			if !errors.Is(err, context.Canceled) || out != (domain.GuestReadiness{}) {
				t.Fatalf("out=%+v err=%v", out, err)
			}
			if phase == "before open" {
				if opens != 0 || s.closes != 0 {
					t.Fatal("canceled request opened native handles")
				}
			} else if s.closes != 1 {
				t.Fatal("native handles not closed once")
			}
			want := 0
			if phase == "command" || phase == "final observation" || phase == "cleanup" {
				want = 1
			}
			if s.commands != want {
				t.Fatalf("commands=%d want=%d", s.commands, want)
			}
		})
	}
	t.Run("positive whole second timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
		defer cancel()
		s := readinessFixture("connected")
		_, err := inspectGuestReadiness(ctx, readinessTestURI, readinessTestID, s.open)
		if err != nil || s.timeout < 1 || s.timeout > 3 {
			t.Fatalf("timeout=%d err=%v", s.timeout, err)
		}
	})
	t.Run("subsecond deadline sends no ping", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		s := readinessFixture("connected")
		out, err := inspectGuestReadiness(ctx, readinessTestURI, readinessTestID, s.open)
		readinessErrorCode(t, err, "WAIT_TIMEOUT")
		if out != (domain.GuestReadiness{}) || s.commands != 0 || s.closes != 1 {
			t.Fatalf("out=%+v commands=%d closes=%d", out, s.commands, s.closes)
		}
	})
	t.Run("expired deadline", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		_, err := inspectGuestReadiness(ctx, readinessTestURI, readinessTestID, func(string, string) (guestReadinessSession, error) {
			t.Fatal("opened on expired context")
			return nil, nil
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
}

func TestGuestReadinessNativeFailuresAreSanitized(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"unsupported", native.Error{Code: native.ERR_NO_SUPPORT, Message: "SECRET"}, "UNSUPPORTED_CAPABILITY"},
		{"denied", native.Error{Code: native.ERR_ACCESS_DENIED, Message: "SECRET"}, "PERMISSION_DENIED"},
		{"missing", native.Error{Code: native.ERR_NO_DOMAIN, Message: "SECRET"}, "SOURCE_CHANGED"},
		{"runtime drift", domain.Fail("SOURCE_CHANGED", "SECRET"), "SOURCE_CHANGED"},
		{"transport", errors.New("SECRET /private/socket"), "OPERATION_FAILED"},
	} {
		for _, phase := range []string{"open", "initial observation", "command", "final observation", "cleanup"} {
			t.Run(tc.name+"/"+phase, func(t *testing.T) {
				s := readinessFixture("connected")
				wantCode := tc.code
				switch phase {
				case "initial observation":
					s.readErr, s.readFail = tc.err, 1
				case "final observation":
					s.readErr, s.readFail = tc.err, 2
				case "command":
					s.commandErr = tc.err
				case "cleanup":
					s.closeErr = tc.err
					wantCode = "OPERATION_FAILED"
				}
				out, err := inspectGuestReadiness(context.Background(), readinessTestURI, readinessTestID, func(uri, id string) (guestReadinessSession, error) {
					if phase == "open" {
						return nil, tc.err
					}
					return s.open(uri, id)
				})
				readinessErrorCode(t, err, wantCode)
				if out != (domain.GuestReadiness{}) || s.commands > 1 {
					t.Fatalf("out=%+v commands=%d", out, s.commands)
				}
				wantClose := 1
				if phase == "open" {
					wantClose = 0
				}
				if s.closes != wantClose {
					t.Fatal("incorrect cleanup count")
				}
			})
		}
	}
}

func TestGuestReadinessSelectionRejectedBeforeNativeOpen(t *testing.T) {
	for _, tc := range []struct{ uri, id, code string }{
		{"qemu+ssh://host/system", readinessTestID, "UNSUPPORTED_CAPABILITY"},
		{"test:///default", readinessTestID, "UNSUPPORTED_CAPABILITY"},
		{"", readinessTestID, "UNSUPPORTED_CAPABILITY"},
		{readinessTestURI, "Selected VM", "INVALID_INPUT"},
		{readinessTestURI, strings.ToUpper(readinessTestID), "INVALID_INPUT"},
		{readinessTestURI, "00000000-0000-0000-0000-000000000000", "INVALID_INPUT"},
	} {
		_, err := inspectGuestReadiness(context.Background(), tc.uri, tc.id, func(string, string) (guestReadinessSession, error) {
			t.Fatal("invalid selection opened native connection")
			return nil, nil
		})
		readinessErrorCode(t, err, tc.code)
	}
	t.Run("explicit local session", func(t *testing.T) {
		s := readinessFixture("connected")
		s.values[0].vm.Key.ConnectionID = "qemu:///session"
		out, err := inspectGuestReadiness(context.Background(), "qemu:///session", readinessTestID, func(uri, id string) (guestReadinessSession, error) {
			if uri != "qemu:///session" || id != readinessTestID {
				t.Fatal("identity changed")
			}
			return s, nil
		})
		if err != nil || !out.AgentResponsive || out.Resource.ConnectionID != "qemu:///session" {
			t.Fatalf("out=%+v err=%v", out, err)
		}
	})
}
