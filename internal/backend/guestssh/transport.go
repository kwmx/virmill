// Package guestssh runs approved POSIX script bytes through a fixed OpenSSH
// transport. Callers own recipe authorization, guest identity and stage policy.
package guestssh

import (
	"bytes"
	"encoding/hex"
	"net/netip"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"virmill.local/core/internal/domain"
)

type Target struct {
	Address          string `json:"address"`
	Port             uint16 `json:"port"`
	User             string `json:"user"`
	IdentityFile     string `json:"identityFile"`
	KnownHostsFile   string `json:"knownHostsFile"`
	IdentitySHA256   string `json:"identitySHA256"`
	KnownHostsSHA256 string `json:"knownHostsSHA256"`
}
type TargetIdentity struct {
	IdentitySHA256   string `json:"identitySHA256"`
	KnownHostsSHA256 string `json:"knownHostsSHA256"`
}
type Identity struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}
type Script struct {
	// SSHSHA256 binds execution to the executable reviewed by the caller.
	SSHSHA256 string
	Content   []byte
	Arguments []string
	Timeout   time.Duration
}
type Result struct {
	ExitCode       int
	Stdout, Stderr []byte
}
type Tool struct{}

const (
	executable       = "/usr/bin/ssh"
	maxScript        = 1 << 20
	maxArguments     = 128
	maxArgument      = 4096
	maxArgumentBytes = 64 << 10
	maxCredential    = 1 << 20
	maxExecutable    = 64 << 20
	maxStdout        = 1 << 20
	maxStderr        = 256 << 10
	maxTimeout       = 30 * time.Minute
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,31}$`)

func failure(code, message string) error { return domain.Fail(code, "guest SSH: "+message) }
func safePath(p string) bool {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p || len(p) > 4096 || !utf8.ValidString(p) {
		return false
	}
	for _, r := range p {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
func validHash(s string) bool {
	if len(s) != 64 || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}
func validateTarget(t Target) error {
	a, err := netip.ParseAddr(t.Address)
	if err != nil || a.Zone() != "" || a.Is4In6() || !a.IsGlobalUnicast() || a.String() != t.Address || t.Port == 0 {
		return failure("INVALID_INPUT", "an explicit canonical unzoned unicast IPv4/IPv6 address and nonzero port are required")
	}
	if !usernamePattern.MatchString(t.User) || strings.EqualFold(t.User, "root") {
		return failure("INVALID_INPUT", "an explicit bounded non-root guest username is required")
	}
	if !safePath(t.IdentityFile) || !safePath(t.KnownHostsFile) || t.IdentityFile == t.KnownHostsFile {
		return failure("INVALID_INPUT", "distinct canonical absolute identity and known-hosts file paths are required")
	}
	if (t.IdentitySHA256 == "") != (t.KnownHostsSHA256 == "") || t.IdentitySHA256 != "" && (!validHash(t.IdentitySHA256) || !validHash(t.KnownHostsSHA256)) {
		return failure("INVALID_INPUT", "expected credential hashes must both be absent or both be lowercase SHA-256 values")
	}
	return nil
}
func validateScript(s Script) error {
	if s.SSHSHA256 != "" && !validHash(s.SSHSHA256) {
		return failure("INVALID_INPUT", "expected SSH executable hash must be lowercase SHA-256")
	}
	if len(s.Content) == 0 || len(s.Content) > maxScript || !utf8.Valid(s.Content) || bytes.IndexByte(s.Content, 0) >= 0 || s.Timeout < time.Second || s.Timeout > maxTimeout || len(s.Arguments) > maxArguments {
		return failure("INVALID_INPUT", "a bounded UTF-8 POSIX script and timeout from one second through thirty minutes are required")
	}
	total := 0
	for _, a := range s.Arguments {
		total += len(a)
		if len(a) > maxArgument || total > maxArgumentBytes || !utf8.ValidString(a) || strings.IndexByte(a, 0) >= 0 {
			return failure("INVALID_INPUT", "script arguments exceed byte/count bounds or contain invalid text")
		}
	}
	return nil
}

// Only this fixed command is sent to the guest login shell. Quoted arguments
// remain data, including apostrophes, newlines and shell metacharacters.
func remoteCommand(arguments []string) string {
	var b strings.Builder
	b.WriteString("sh -s --")
	for _, a := range arguments {
		b.WriteString(" '")
		b.WriteString(strings.ReplaceAll(a, "'", "'\"'\"'"))
		b.WriteByte('\'')
	}
	return b.String()
}
