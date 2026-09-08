//go:build linux

package guestssh

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
	"virmill.local/core/internal/backend/fileidentity"
)

type heldFile struct {
	f      *os.File
	path   string
	stamp  fileidentity.Identity
	uid    uint32
	binary bool
}

func ordinary(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if os.Geteuid() == 0 || os.Getuid() != os.Geteuid() {
		return failure("PERMISSION_DENIED", "transport requires an ordinary user without elevated identity")
	}
	return nil
}
func openHeld(ctx context.Context, p string, binary bool) (*heldFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, stamp, err := fileidentity.Open(p, false, false)
	if err != nil {
		return nil, failure("PERMISSION_DENIED", "required file cannot be pinned without symlink traversal; diagnostics withheld")
	}
	fail := func() (*heldFile, error) {
		f.Close()
		return nil, failure("PERMISSION_DENIED", "required file ownership, permissions, size or type is unsupported")
	}
	var st unix.Stat_t
	if unix.Fstat(int(f.Fd()), &st) != nil {
		return fail()
	}
	uid := uint32(os.Geteuid())
	if binary {
		uid = 0
		if st.Mode&0022 != 0 || st.Mode&07000 != 0 || st.Mode&0111 == 0 || stamp.Size == 0 || stamp.Size > maxExecutable {
			return fail()
		}
		for _, ancestor := range []string{"/", "/usr", "/usr/bin"} {
			var dir unix.Stat_t
			if unix.Lstat(ancestor, &dir) != nil || dir.Mode&unix.S_IFMT != unix.S_IFDIR || dir.Uid != 0 || dir.Mode&0022 != 0 {
				return fail()
			}
		}
	} else if st.Mode&07777 != 0600 || stamp.Size == 0 || stamp.Size > maxCredential {
		return fail()
	}
	if st.Uid != uid {
		return fail()
	}
	r, err := os.OpenFile(fmt.Sprintf("/proc/self/fd/%d", f.Fd()), os.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	f.Close()
	if err != nil {
		return nil, failure("PERMISSION_DENIED", "held file cannot be read through private procfs descriptor access")
	}
	h := &heldFile{f: r, path: p, stamp: stamp, uid: uid, binary: binary}
	if err := h.check(); err != nil {
		r.Close()
		return nil, err
	}
	return h, nil
}
func (h *heldFile) check() error {
	id, err := fileidentity.InspectFile(h.f, false)
	var st unix.Stat_t
	if err != nil || id != h.stamp || unix.Fstat(int(h.f.Fd()), &st) != nil || st.Uid != h.uid || !h.binary && st.Mode&07777 != 0600 {
		return failure("SOURCE_CHANGED", "held file identity or private permissions changed")
	}
	current, err := fileidentity.Observe(h.path, false)
	if err != nil || current != h.stamp {
		return failure("SOURCE_CHANGED", "original file path no longer identifies the pinned file")
	}
	return nil
}

const credentialSeals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL

// Hash exact held bytes; optionally copy them to an immutable anonymous file.
// The scratch buffer is cleared and no credential bytes enter a result/error.
func (h *heldFile) hash(ctx context.Context, seal bool) (digest string, sealed *os.File, err error) {
	if err = h.check(); err != nil {
		return "", nil, err
	}
	if _, err = h.f.Seek(0, io.SeekStart); err != nil {
		return "", nil, failure("OPERATION_FAILED", "held file cannot be read")
	}
	if seal {
		fd, e := unix.MemfdCreate("virmill-ssh-credential", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
		if e != nil {
			return "", nil, failure("UNSUPPORTED_CAPABILITY", "sealed anonymous credential files are unavailable")
		}
		sealed = os.NewFile(uintptr(fd), "private sealed SSH credential")
		defer func() {
			if err != nil {
				sealed.Close()
				sealed = nil
			}
		}()
		if e = sealed.Chmod(0600); e != nil {
			return "", sealed, failure("PERMISSION_DENIED", "anonymous credential permissions cannot be restricted")
		}
	}
	buf := make([]byte, 32<<10)
	defer clear(buf)
	d := sha256.New()
	total := uint64(0)
	for {
		if e := ctx.Err(); e != nil {
			return "", sealed, e
		}
		n, e := h.f.Read(buf)
		if n > 0 {
			total += uint64(n)
			if total > h.stamp.Size {
				return "", sealed, failure("SOURCE_CHANGED", "held file size changed while hashing")
			}
			_, _ = d.Write(buf[:n])
			if sealed != nil {
				if written, we := sealed.Write(buf[:n]); we != nil || written != n {
					return "", sealed, failure("OPERATION_FAILED", "anonymous credential copy failed")
				}
			}
			clear(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil || n == 0 {
			return "", sealed, failure("OPERATION_FAILED", "held file read failed; diagnostics withheld")
		}
	}
	if err = ctx.Err(); err != nil {
		return "", sealed, err
	}
	if total != h.stamp.Size {
		return "", sealed, failure("SOURCE_CHANGED", "held file size changed while hashing")
	}
	if err = h.check(); err != nil {
		return "", sealed, err
	}
	if sealed != nil {
		if _, e := unix.FcntlInt(sealed.Fd(), unix.F_ADD_SEALS, credentialSeals); e != nil {
			return "", sealed, failure("UNSUPPORTED_CAPABILITY", "credential immutability seals are unavailable")
		}
		if err = checkSealed(sealed); err != nil {
			return "", sealed, err
		}
	}
	return hex.EncodeToString(d.Sum(nil)), sealed, nil
}
func checkSealed(f *os.File) error {
	var st unix.Stat_t
	seals, err := unix.FcntlInt(f.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&credentialSeals != credentialSeals || unix.Fstat(int(f.Fd()), &st) != nil || st.Mode&07777 != 0600 || st.Uid != uint32(os.Geteuid()) || st.Mode&unix.S_IFMT != unix.S_IFREG {
		return failure("SOURCE_CHANGED", "anonymous credential lost its private immutable binding")
	}
	return nil
}
func openExecutable(ctx context.Context) (*heldFile, Identity, error) {
	if err := ordinary(ctx); err != nil {
		return nil, Identity{}, err
	}
	f, err := openHeld(ctx, executable, true)
	if err != nil {
		return nil, Identity{}, failure("UNSUPPORTED_CAPABILITY", "administrator-installed native /usr/bin/ssh is unavailable or unsafe")
	}
	var magic [4]byte
	if _, err = f.f.ReadAt(magic[:], 0); err != nil || !bytes.Equal(magic[:], []byte{0x7f, 'E', 'L', 'F'}) {
		f.f.Close()
		return nil, Identity{}, failure("UNSUPPORTED_CAPABILITY", "system SSH executable must be a native ELF file")
	}
	digest, _, err := f.hash(ctx, false)
	if err != nil {
		f.f.Close()
		return nil, Identity{}, err
	}
	return f, Identity{Path: executable, SHA256: digest}, nil
}

type credentials struct {
	identity, hosts         *heldFile
	identityCopy, hostsCopy *os.File
	digests                 TargetIdentity
}

func (c *credentials) close() {
	for _, f := range []*os.File{c.identityCopy, c.hostsCopy} {
		if f != nil {
			f.Close()
		}
	}
	if c.identity != nil {
		c.identity.f.Close()
	}
	if c.hosts != nil {
		c.hosts.f.Close()
	}
}
func (c *credentials) check() error {
	if err := c.identity.check(); err != nil {
		return err
	}
	if err := c.hosts.check(); err != nil {
		return err
	}
	for _, f := range []*os.File{c.identityCopy, c.hostsCopy} {
		if f != nil {
			if err := checkSealed(f); err != nil {
				return err
			}
		}
	}
	return nil
}
func prepare(ctx context.Context, t Target, seal bool) (out *credentials, err error) {
	if err = ordinary(ctx); err != nil {
		return nil, err
	}
	if err = validateTarget(t); err != nil {
		return nil, err
	}
	c := &credentials{}
	defer func() {
		if err != nil {
			c.close()
		}
	}()
	if c.identity, err = openHeld(ctx, t.IdentityFile, false); err != nil {
		return nil, err
	}
	if c.hosts, err = openHeld(ctx, t.KnownHostsFile, false); err != nil {
		return nil, err
	}
	if c.identity.stamp.Generation == c.hosts.stamp.Generation {
		return nil, failure("INVALID_INPUT", "identity and known-hosts files alias one inode")
	}
	if c.digests.IdentitySHA256, c.identityCopy, err = c.identity.hash(ctx, seal); err != nil {
		return nil, err
	}
	if c.digests.KnownHostsSHA256, c.hostsCopy, err = c.hosts.hash(ctx, seal); err != nil {
		return nil, err
	}
	if t.IdentitySHA256 != "" && (t.IdentitySHA256 != c.digests.IdentitySHA256 || t.KnownHostsSHA256 != c.digests.KnownHostsSHA256) {
		return nil, failure("SOURCE_CHANGED", "credential bytes differ from the reviewed target identity")
	}
	if err = c.check(); err != nil {
		return nil, err
	}
	return c, nil
}

// InspectTarget has no process/SSH call. If expected hashes are supplied, both
// must match; omitting both is the discovery step used to construct a plan.
func (Tool) InspectTarget(ctx context.Context, t Target) (TargetIdentity, error) {
	c, err := prepare(ctx, t, false)
	if err != nil {
		return TargetIdentity{}, err
	}
	defer c.close()
	if err = c.check(); err != nil {
		return TargetIdentity{}, err
	}
	if err = ctx.Err(); err != nil {
		return TargetIdentity{}, err
	}
	return c.digests, nil
}
