//go:build linux && amd64

package helper

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/wire"
)

const SocketPath = "/run/virmill-host-helper/control.sock"

type Client struct{ KeyPath string }

func (c Client) key() (ed25519.PrivateKey, string, error) {
	if !filepath.IsAbs(c.KeyPath) || filepath.Clean(c.KeyPath) != c.KeyPath {
		return nil, "", errors.New("canonical private helper key path required")
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, c.KeyPath, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK, Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS})
	if err != nil {
		return nil, "", domain.Fail("PERMISSION_DENIED", "private helper-key.pem unavailable; complete the documented administrator setup")
	}
	f := os.NewFile(uintptr(fd), "private helper signing key")
	defer f.Close()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return nil, "", err
	}
	if st.Uid != uint32(os.Getuid()) || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0777 != 0600 || st.Nlink != 1 || st.Size > 8192 {
		return nil, "", errors.New("helper key must be an actor-owned single-link regular file with mode 0600 and at most 8192 bytes")
	}
	b, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil {
		return nil, "", err
	}
	block, rest := pem.Decode(b)
	if block == nil || block.Type != "PRIVATE KEY" || len(block.Headers) != 0 || len(rest) != 0 || len(b) > 8192 {
		return nil, "", errors.New("one unencrypted PKCS8 private-key PEM block required")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, "", errors.New("invalid helper PKCS8 key")
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, "", errors.New("helper signing key must be Ed25519")
	}
	digest := sha256.Sum256(key.Public().(ed25519.PublicKey))
	return key, hex.EncodeToString(digest[:]), nil
}
func (c Client) Identity() (any, error) {
	key, id, err := c.key()
	if err != nil {
		return nil, err
	}
	public := hex.EncodeToString(key.Public().(ed25519.PublicKey))
	result := map[string]any{"keyPath": c.KeyPath, "keyID": id, "publicKey": public, "actorUID": os.Getuid(), "policyPath": PolicyPath, "socketPath": SocketPath, "policyApproved": false}
	p, err := loadPolicy()
	if err != nil {
		result["setupStatus"] = "administrator policy unavailable"
		return result, nil
	}
	allowed := false
	for _, uid := range p.Actors {
		allowed = allowed || uid == uint32(os.Getuid())
	}
	result["policyApproved"] = p.APIVersion == domain.APIVersion && allowed && p.Keys[id] == public
	result["roots"] = p.Roots
	return result, nil
}
func (c Client) Root(id string) (string, error) {
	p, err := loadPolicy()
	if err != nil {
		return "", domain.Fail("PERMISSION_DENIED", "readable administrator helper policy required")
	}
	root := p.Roots[id]
	if root == "" {
		return "", domain.Fail("PERMISSION_DENIED", "root ID is not approved by helper policy")
	}
	return root, nil
}
func (c Client) Call(ctx context.Context, r Request) (AccessResult, error) {
	var result AccessResult
	if r.ActorUID != uint32(os.Getuid()) || r.ActorUID == 0 {
		return result, domain.Fail("PERMISSION_DENIED", "helper client requires its ordinary-user actor")
	}
	key, id, err := c.key()
	if err != nil {
		return result, err
	}
	if r.KeyID != "" && r.KeyID != id {
		return result, domain.Fail("SOURCE_CHANGED", "helper key differs from the reviewed identity")
	}
	r.KeyID = id
	if r.ExpiresAt.IsZero() {
		r.ExpiresAt = time.Now().UTC().Add(5 * time.Minute)
	}
	data, err := SignedBytes(r)
	if err != nil {
		return result, err
	}
	r.Signature = hex.EncodeToString(ed25519.Sign(key, data))
	p, err := loadPolicy()
	if err != nil {
		return result, err
	}
	if err = Authorize(r.ActorUID, r, p, time.Now()); err != nil {
		return result, domain.Fail("PERMISSION_DENIED", err.Error())
	}
	if err = ownedRoot(filepath.Dir(SocketPath), true); err != nil {
		return result, err
	}
	st, err := os.Lstat(SocketPath)
	if err != nil {
		return result, err
	}
	if st.Mode()&os.ModeSocket == 0 {
		return result, errors.New("helper endpoint is not a Unix socket")
	}
	var socketStat unix.Stat_t
	if err = unix.Lstat(SocketPath, &socketStat); err != nil {
		return result, err
	}
	if socketStat.Uid != 0 {
		return result, errors.New("helper endpoint is not root-owned")
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", SocketPath)
	if err != nil {
		return result, err
	}
	defer conn.Close()
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	peer, _, err := PeerGroups(conn.(*net.UnixConn))
	if err != nil {
		return result, err
	}
	if peer.Uid != 0 {
		return result, errors.New("helper peer is not kernel-authenticated root")
	}
	if err = json.NewEncoder(conn).Encode(r); err != nil {
		return result, err
	}
	frame, err := wire.ReadFrame(bufio.NewReader(conn))
	if err != nil {
		return result, err
	}
	var response Response
	if err = wire.Decode(frame, &response); err != nil {
		return result, err
	}
	if response.APIVersion != domain.APIVersion || !response.Success {
		return result, domain.Fail("OPERATION_FAILED", "host helper refused or could not confirm the operation: "+response.Error)
	}
	if err = wire.Decode(response.Access, &result); err != nil {
		return result, err
	}
	binding, err := accessBinding(r)
	if err != nil {
		return result, err
	}
	if result.Version != 1 || result.JobID != r.JobID || result.Binding != binding || (r.Mode != "check" && !result.Complete) {
		return result, domain.Fail("RECOVERY_REQUIRED", "helper response does not prove the requested operation")
	}
	return result, nil
}

// KeyID is a public fingerprint. Private material never enters an operation.
func (c Client) KeyID() (string, error) { _, id, err := c.key(); return id, err }
