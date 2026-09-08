//go:build linux

package guestssh

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

// argv is closed: the only variable values are validated target coordinates,
// owned numeric descriptor paths and one correctly quoted guest command.
func sshArgs(t Target, c *credentials, s Script) []string {
	fdpath := func(f *os.File) string { return fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), f.Fd()) }
	args := []string{"-F", "/dev/null", "-T", "-E", "/dev/null"}
	options := []string{
		"BatchMode=yes", "StrictHostKeyChecking=yes", "UserKnownHostsFile=" + fdpath(c.hostsCopy),
		"GlobalKnownHostsFile=/dev/null", "IdentitiesOnly=yes", "IdentityAgent=none", "ForwardAgent=no",
		"ClearAllForwardings=yes", "PermitLocalCommand=no", "ProxyCommand=none", "ProxyJump=none",
		"LogLevel=QUIET", "PreferredAuthentications=publickey", "PubkeyAuthentication=yes",
		"PasswordAuthentication=no", "KbdInteractiveAuthentication=no", "GSSAPIAuthentication=no",
		"HostbasedAuthentication=no", "EnableSSHKeysign=no", "NumberOfPasswordPrompts=0",
		"AddKeysToAgent=no", "CertificateFile=none", "PKCS11Provider=none", "SecurityKeyProvider=none",
		"PubkeyAcceptedAlgorithms=ssh-ed25519,rsa-sha2-512,rsa-sha2-256,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521",
		"KnownHostsCommand=none", "UpdateHostKeys=no", "VerifyHostKeyDNS=no", "CanonicalizeHostname=no",
		"CheckHostIP=yes", "ControlMaster=no", "ControlPath=none", "ControlPersist=no",
		"ForwardX11=no", "ForwardX11Trusted=no", "RequestTTY=no", "EscapeChar=none",
		"ConnectionAttempts=1", "ConnectTimeout=10", "ServerAliveInterval=5", "ServerAliveCountMax=1",
	}
	address, _ := netip.ParseAddr(t.Address)
	if address.Is4() {
		options = append(options, "AddressFamily=inet")
	} else {
		options = append(options, "AddressFamily=inet6")
	}
	for _, option := range options {
		args = append(args, "-o", option)
	}
	return append(args, "-i", fdpath(c.identityCopy), "-p", strconv.Itoa(int(t.Port)), "-l", t.User, "--", t.Address, remoteCommand(s.Arguments))
}

type invocation func(context.Context, []string, []byte, time.Duration) (Result, error)

// Run requires prepared caller authorization. Exits 0..254 are remote stage
// results for caller policy; 255 is reserved by SSH and is never trusted as a
// completed guest stage. Cancellation/output/drift discard all partial output.
func (Tool) Run(ctx context.Context, t Target, s Script) (Result, error) {
	return run(ctx, t, s, func(ctx context.Context, args []string, content []byte, timeout time.Duration) (Result, error) {
		return invoke(ctx, args, content, timeout, s.SSHSHA256)
	})
}
func run(ctx context.Context, t Target, s Script, call invocation) (out Result, err error) {
	if err = ordinary(ctx); err != nil {
		return out, err
	}
	if err = validateTarget(t); err != nil {
		return out, err
	}
	if err = validateScript(s); err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	c, err := prepare(ctx, t, true)
	if err != nil {
		return out, err
	}
	defer c.close()
	if err = c.check(); err != nil {
		return out, err
	}
	out, err = call(ctx, sshArgs(t, c, s), s.Content, s.Timeout)
	if check := c.check(); check != nil {
		err = check
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return Result{}, err
	}
	return out, nil
}
func invoke(ctx context.Context, args []string, content []byte, timeout time.Duration, expected string) (out Result, err error) {
	f, id, err := openExecutable(ctx)
	if err != nil {
		return out, err
	}
	defer f.f.Close()
	if expected != "" && expected != id.SHA256 {
		return out, failure("SOURCE_CHANGED", "SSH executable differs from approved digest")
	}
	out, err = execute(ctx, f.f, args, content, timeout)
	if check := f.check(); check != nil {
		err = check
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return Result{}, err
	}
	return out, nil
}
func (Tool) Identity(ctx context.Context) (out Identity, err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	f, id, err := openExecutable(ctx)
	if err != nil {
		return out, err
	}
	defer f.f.Close()
	r, err := execute(ctx, f.f, []string{"-V"}, nil, 5*time.Second)
	if err != nil {
		return out, err
	}
	version := strings.TrimSuffix(string(r.Stderr), "\n")
	if r.ExitCode != 0 || len(r.Stdout) != 0 || len(version) > 1024 || !strings.HasPrefix(version, "OpenSSH_") || strings.ContainsAny(version, "\r\n\x00\x1b") {
		return out, failure("UNSUPPORTED_CAPABILITY", "system SSH version output is unrecognized")
	}
	if err = f.check(); err != nil {
		return out, err
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	id.Version = version
	return id, nil
}
