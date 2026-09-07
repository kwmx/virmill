//go:build linux && amd64

package helper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"os"
	"time"
	"virmill.local/core/internal/wire"
)

const MaxAuxiliarySnapshotBytes int64 = 256 << 20
const maxSnapshotEnvelope = 128 << 10
const snapshotSeals = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL

// CaptureSealed creates an immutable anonymous object. Callers must already hold
// and validate the typed native source; this utility grants no root read scope.
// The source must be an ordinary bounded file reader, not an interruptible device
// or network stream. No source format is interpreted here.
func CaptureSealed(ctx context.Context, source io.Reader, size int64) (*os.File, error) {
	if size <= 0 || size > MaxAuxiliarySnapshotBytes {
		return nil, errors.New("auxiliary capture size outside bounds")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := unix.MemfdCreate("virmill-auxiliary-snapshot", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "sealed auxiliary snapshot")
	success := false
	defer func() {
		if !success {
			f.Close()
		}
	}()
	buf := make([]byte, 64<<10)
	remaining := size
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		want := int64(len(buf))
		if want > remaining {
			want = remaining
		}
		n, err := io.ReadFull(source, buf[:want])
		if err != nil {
			return nil, fmt.Errorf("incomplete auxiliary source: %w", err)
		}
		if _, err = f.Write(buf[:n]); err != nil {
			return nil, err
		}
		remaining -= int64(n)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var extra [1]byte
	if n, err := source.Read(extra[:]); n != 0 || err != io.EOF {
		return nil, errors.New("auxiliary source exceeds declared size or lacks a completed read")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err = unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, snapshotSeals); err != nil {
		return nil, err
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if err = validateSealed(f); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	success = true
	return f, nil
}

func validateSealed(f *os.File) error {
	if f == nil {
		return errors.New("sealed snapshot descriptor required")
	}
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 0 || st.Size <= 0 || st.Size > MaxAuxiliarySnapshotBytes {
		return errors.New("snapshot must be a bounded anonymous regular object")
	}
	flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_PATH != 0 || flags&unix.O_ACCMODE == unix.O_WRONLY {
		return errors.New("snapshot descriptor is not readable")
	}
	seals, err := unix.FcntlInt(f.Fd(), unix.F_GET_SEALS, 0)
	if err != nil || seals&snapshotSeals != snapshotSeals {
		return errors.New("snapshot descriptor is not immutably sealed")
	}
	return nil
}

// SendSealedSnapshot sends one framed metadata object and one immutable FD. The
// connection must be dedicated and its peer already authenticated/authorized.
// Existing helper JSON methods do not automatically use this transport.
func SendSealedSnapshot(ctx context.Context, conn *net.UnixConn, envelope []byte, f *os.File) error {
	if len(envelope) > maxSnapshotEnvelope {
		return errors.New("auxiliary metadata size limit")
	}
	if conn == nil || bytes.ContainsRune(envelope, '\n') {
		return errors.New("dedicated connection and single-line metadata required")
	}
	if err := wire.Validate(envelope); err != nil {
		return err
	}
	if err := validateSealed(f); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetWriteDeadline(time.Now()) })
	defer stop()
	frame := append(append([]byte{}, envelope...), '\n')
	n, oobn, err := conn.WriteMsgUnix(frame, unix.UnixRights(int(f.Fd())), nil)
	if err != nil {
		return err
	}
	if n == 0 || oobn != len(unix.UnixRights(int(f.Fd()))) {
		return errors.New("incomplete snapshot descriptor transfer")
	}
	// Stream writes can be partial. Never attach the descriptor a second time.
	for n < len(frame) {
		written, err := conn.Write(frame[n:])
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		n += written
	}
	return nil
}

// ReceiveSealedSnapshot refuses unexpected/multiple descriptors and closes all
// received FDs on every failure. MSG_CMSG_CLOEXEC prevents inheritance races.
// The caller still checks the root peer and the typed operation/digest binding.
func ReceiveSealedSnapshot(ctx context.Context, conn *net.UnixConn) ([]byte, *os.File, error) {
	return receiveSnapshotFrame(ctx, conn, false)
}

// permitNoFD is private: only the typed auxiliary response validator may accept
// an error or metadata-only response without a descriptor. Existing transport
// callers still require exactly one sealed object.
func receiveSnapshotFrame(ctx context.Context, conn *net.UnixConn, permitNoFD bool) ([]byte, *os.File, error) {
	if conn == nil {
		return nil, nil, errors.New("dedicated auxiliary connection required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return nil, nil, err
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stop()
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	var files []int
	success := false
	defer func() {
		if !success {
			for _, fd := range files {
				_ = unix.Close(fd)
			}
		}
	}()
	data := make([]byte, 0, 4096)
	buf := make([]byte, 16<<10)
	oob := make([]byte, unix.CmsgSpace(16*4))
	for len(data) <= maxSnapshotEnvelope {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		var n, on, flags int
		var readErr error
		err = raw.Read(func(fd uintptr) bool {
			for {
				n, on, flags, _, readErr = unix.Recvmsg(int(fd), buf, oob, unix.MSG_CMSG_CLOEXEC)
				if readErr == unix.EINTR {
					continue
				}
				return readErr != unix.EAGAIN && readErr != unix.EWOULDBLOCK
			}
		})
		if err != nil {
			return nil, nil, err
		}
		if readErr != nil {
			return nil, nil, readErr
		}
		messages, err := unix.ParseSocketControlMessage(oob[:on])
		if err != nil {
			return nil, nil, err
		}
		unexpected := false
		for _, m := range messages {
			if m.Header.Level != unix.SOL_SOCKET || m.Header.Type != unix.SCM_RIGHTS {
				unexpected = true
				continue
			}
			fds, err := unix.ParseUnixRights(&m)
			if err != nil {
				return nil, nil, err
			}
			files = append(files, fds...)
		}
		if flags&(unix.MSG_CTRUNC|unix.MSG_TRUNC) != 0 || unexpected || len(files) > 1 {
			return nil, nil, errors.New("invalid auxiliary descriptor control message")
		}
		if n == 0 {
			return nil, nil, io.ErrUnexpectedEOF
		}
		data = append(data, buf[:n]...)
		if len(data) > maxSnapshotEnvelope+1 {
			return nil, nil, errors.New("auxiliary metadata size limit")
		}
		if end := bytes.IndexByte(data, '\n'); end >= 0 {
			if end != len(data)-1 || (len(files) != 1 && !(permitNoFD && len(files) == 0)) {
				return nil, nil, errors.New("one metadata frame and one snapshot descriptor required")
			}
			if err := wire.Validate(data[:end]); err != nil {
				return nil, nil, err
			}
			if len(files) == 0 {
				if err := ctx.Err(); err != nil {
					return nil, nil, err
				}
				success = true
				return data[:end], nil, nil
			}
			f := os.NewFile(uintptr(files[0]), "received auxiliary snapshot")
			if err := validateSealed(f); err != nil {
				// os.File now owns this descriptor; avoid a finalizer/double-close.
				files = nil
				_ = f.Close()
				return nil, nil, err
			}
			if err := ctx.Err(); err != nil {
				files = nil
				_ = f.Close()
				return nil, nil, err
			}
			success = true
			return data[:end], f, nil
		}
	}
	return nil, nil, errors.New("auxiliary metadata size limit")
}
