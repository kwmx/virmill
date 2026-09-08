package guestssh

import (
	"strings"
	"testing"
	"time"
)

func validTarget() Target {
	return Target{Address: "192.0.2.42", Port: 2222, User: "guest", IdentityFile: "/private/key", KnownHostsFile: "/private/hosts"}
}
func validScript() Script {
	return Script{Content: []byte("printf '%s\\n' \"$1\"\n"), Arguments: []string{"data"}, Timeout: time.Second}
}

func TestTargetRejectsAddressExpansionOptionsRootAndHashAmbiguity(t *testing.T) {
	for _, address := range []string{"host.example", "127.0.0.1", "::1", "169.254.1.2", "fe80::1", "fe80::1%eth0", "::", "0.0.0.0", "224.0.0.1", "ff02::1", "255.255.255.255", "::ffff:192.0.2.42", "[2001:db8::1]", "2001:DB8::1", "192.000.2.42", " 192.0.2.42", "192.0.2.42;touch /tmp/x", "-oProxyCommand=touch", "guest@192.0.2.42", "ssh://192.0.2.42"} {
		t.Run(address, func(t *testing.T) {
			target := validTarget()
			target.Address = address
			if validateTarget(target) == nil {
				t.Fatal("invalid address accepted")
			}
		})
	}
	for _, user := range []string{"", "root", "ROOT", "-guest", "guest@host", "guest;id", "guest\nname", strings.Repeat("u", 33)} {
		target := validTarget()
		target.User = user
		if validateTarget(target) == nil {
			t.Fatalf("invalid user %q accepted", user)
		}
	}
	for _, hashes := range [][2]string{{strings.Repeat("a", 64), ""}, {"", strings.Repeat("a", 64)}, {strings.Repeat("A", 64), strings.Repeat("a", 64)}, {strings.Repeat("g", 64), strings.Repeat("a", 64)}} {
		target := validTarget()
		target.IdentitySHA256, target.KnownHostsSHA256 = hashes[0], hashes[1]
		if validateTarget(target) == nil {
			t.Fatal("ambiguous digest accepted")
		}
	}
	for _, mutate := range []func(*Target){func(v *Target) { v.Port = 0 }, func(v *Target) { v.IdentityFile = "relative" }, func(v *Target) { v.IdentityFile = "/private/../key" }, func(v *Target) { v.IdentityFile = "/private/key\x00" }, func(v *Target) { v.KnownHostsFile = v.IdentityFile }} {
		v := validTarget()
		mutate(&v)
		if validateTarget(v) == nil {
			t.Fatal("invalid target accepted")
		}
	}
	for _, address := range []string{"192.0.2.42", "10.2.3.4", "2001:db8::1", "fd00::42"} {
		v := validTarget()
		v.Address = address
		if err := validateTarget(v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRemotePOSIXArgumentsAreQuotedAsData(t *testing.T) {
	args := []string{"", "plain", "a'b", "$(touch /tmp/x); `id`", "--help", "line\nsecond", "α"}
	want := "sh -s -- '' 'plain' 'a'\"'\"'b' '$(touch /tmp/x); `id`' '--help' 'line\nsecond' 'α'"
	if got := remoteCommand(args); got != want {
		t.Fatalf("command=%q want=%q", got, want)
	}
	if got := remoteCommand(nil); got != "sh -s --" {
		t.Fatalf("unexpected fixed command: %q", got)
	}
	for _, mutate := range []func(*Script){
		func(s *Script) { s.Content = nil }, func(s *Script) { s.Content = []byte{0xff} }, func(s *Script) { s.Content = []byte("x\x00") }, func(s *Script) { s.Content = []byte(strings.Repeat("x", maxScript+1)) },
		func(s *Script) { s.Timeout = 0 }, func(s *Script) { s.Timeout = time.Nanosecond }, func(s *Script) { s.Timeout = maxTimeout + 1 }, func(s *Script) { s.Arguments = make([]string, maxArguments+1) },
		func(s *Script) { s.Arguments = []string{strings.Repeat("x", maxArgument+1)} }, func(s *Script) { s.Arguments = []string{"a\x00b"} },
		func(s *Script) {
			s.Arguments = make([]string, 17)
			for i := range s.Arguments {
				s.Arguments[i] = strings.Repeat("x", 4096)
			}
		},
	} {
		s := validScript()
		mutate(&s)
		if validateScript(s) == nil {
			t.Fatal("unbounded or malformed script accepted")
		}
	}
	s := validScript()
	s.Arguments = args
	if err := validateScript(s); err != nil {
		t.Fatalf("safe quoted data refused: %v", err)
	}
}
