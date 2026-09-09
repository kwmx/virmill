//go:build linux

// Package local provides authenticated JSON-RPC over a private Unix socket.
package local

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	platform "virmill.local/core/internal/platform/linux"
	"virmill.local/core/internal/wire"
)

type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Method  string      `json:"method"`
	Params  app.Request `json:"params"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type reply struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      any           `json:"id"`
	Result  *app.Response `json:"result,omitempty"`
	Error   *rpcError     `json:"error,omitempty"`
}

func peer(c *net.UnixConn) (uint32, error) {
	raw, e := c.SyscallConn()
	if e != nil {
		return 0, e
	}
	var cred *unix.Ucred
	var inner error
	e = raw.Control(func(fd uintptr) { cred, inner = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	if e != nil {
		return 0, e
	}
	if inner != nil {
		return 0, inner
	}
	return cred.Uid, nil
}

type Server struct {
	Listener    *net.UnixListener
	lock        *os.File
	Service     *app.Service
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	connections map[*net.UnixConn]bool
	heavyReads  chan struct{}
}

func Listen(path string, service *app.Service) (*Server, error) {
	// Linux sockaddr_un reserves one byte for the pathname terminator.
	if len(path) > 107 {
		return nil, errors.New("coordinator socket path exceeds 107 bytes; choose a shorter XDG_RUNTIME_DIR")
	}
	if os.Getuid() == 0 {
		return nil, errors.New("virmilld must run as an ordinary user")
	}
	if e := platform.PrivateDir(filepath.Dir(path)); e != nil {
		return nil, e
	}
	fd, e := unix.Open(filepath.Join(filepath.Dir(path), "coordinator.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, e
	}
	lock := os.NewFile(uintptr(fd), "coordinator.lock")
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		lock.Close()
		return nil, errors.New("another coordinator holds the singleton lock")
	}
	fail := func(e error) (*Server, error) { lock.Close(); return nil, e }
	if st, e := os.Lstat(path); e == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return fail(errors.New("refusing to replace a non-socket path"))
		}
		if e = os.Remove(path); e != nil {
			return fail(e)
		}
	} else if !os.IsNotExist(e) {
		return fail(e)
	}
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		return fail(e)
	}
	if e = os.Chmod(path, 0600); e != nil {
		l.Close()
		return fail(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{Listener: l, lock: lock, Service: service, ctx: ctx, cancel: cancel, connections: map[*net.UnixConn]bool{}, heavyReads: make(chan struct{}, 1)}, nil
}
func (s *Server) Serve() error {
	slots := make(chan struct{}, 32)
	for {
		c, e := s.Listener.AcceptUnix()
		if e != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			return e
		}
		select {
		case slots <- struct{}{}:
		default:
			c.Close()
			continue
		}
		s.mu.Lock()
		s.connections[c] = true
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { <-slots; s.mu.Lock(); delete(s.connections, c); s.mu.Unlock(); c.Close() }()
			uid, e := peer(c)
			if e != nil || uid != uint32(os.Getuid()) {
				return
			}
			reader := bufio.NewReader(c)
			for {
				c.SetReadDeadline(time.Now().Add(5 * time.Minute))
				frame, e := wire.ReadFrame(reader)
				if e != nil {
					return
				}
				var r Request
				resp := reply{JSONRPC: "2.0"}
				if e = wire.Decode(frame, &r); e != nil {
					resp.Error = &rpcError{Code: -32700, Message: "Invalid JSON frame"}
				} else if r.JSONRPC != "2.0" || r.ID == "" || r.Method == "" {
					resp.ID = r.ID
					resp.Error = &rpcError{Code: -32600, Message: "Invalid request"}
				} else {
					resp.ID = r.ID
					result := s.call(c, uid, r.Method, r.Params)
					resp.Result = &result
				}
				b, e := json.Marshal(resp)
				if e != nil || len(b) > wire.MaxFrame {
					return
				}
				c.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if _, e = c.Write(append(b, '\n')); e != nil {
					return
				}
			}
		}()
	}
}

func heavyRead(method string) bool {
	switch method {
	case "import.source.describe", "import.describe", "import.inspect", "import.prepare", "import.prepare-install", "import.prepare-disks":
		return true
	}
	return false
}

func (s *Server) call(c *net.UnixConn, uid uint32, method string, request app.Request) app.Response {
	if !heavyRead(method) {
		// In particular, apply retains coordinator-lifetime acceptance semantics.
		// Already accepted jobs execute under the engine's independent context.
		return s.Service.Call(s.ctx, uid, method, request)
	}
	fail := func(code, message string) app.Response {
		return app.Response{APIVersion: domain.APIVersion, Warnings: []string{}, Error: domain.Fail(code, message)}
	}
	select {
	case s.heavyReads <- struct{}{}:
		defer func() { <-s.heavyReads }()
	default:
		return fail("RESOURCE_BUSY", "Another import inspection or preview is still finishing. Wait for it to finish or cancel, then try again.")
	}
	ctx, cleanup, err := disconnectedReadContext(s.ctx, c)
	if err != nil {
		return fail("OPERATION_FAILED", "Cannot monitor import request cancellation: "+err.Error())
	}
	defer cleanup()
	result := s.Service.Call(ctx, uid, method, request)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fail("WAIT_TIMEOUT", "Import inspection or preview exceeded its 20 minute limit; no job was submitted.")
	}
	return result
}

// disconnectedReadContext observes only socket hangup/error flags on a held
// duplicate FD. It never reads or peeks at framed input, including pipelined
// requests already buffered by the server. The FD is closed by its sole polling
// goroutine, avoiding descriptor reuse races during request cleanup.
func disconnectedReadContext(parent context.Context, conn *net.UnixConn) (context.Context, func(), error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	fd := -1
	var duplicateErr error
	if err = raw.Control(func(original uintptr) {
		fd, duplicateErr = unix.FcntlInt(original, unix.F_DUPFD_CLOEXEC, 0)
	}); err != nil {
		return nil, nil, err
	}
	if duplicateErr != nil {
		return nil, nil, duplicateErr
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Minute)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer unix.Close(fd)
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLRDHUP | unix.POLLHUP | unix.POLLERR}}
		for ctx.Err() == nil {
			_, err := unix.Poll(poll, 100)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil || poll[0].Revents&(unix.POLLRDHUP|unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
				cancel()
				return
			}
		}
	}()
	return ctx, func() { cancel(); <-done }, nil
}

func (s *Server) Close() error {
	s.cancel()
	e := s.Listener.Close()
	s.mu.Lock()
	for c := range s.connections {
		c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	s.lock.Close()
	return e
}

type Client struct {
	Socket  string
	Timeout time.Duration
}

func (c Client) Call(ctx context.Context, method string, r app.Request) (app.Response, error) {
	var out app.Response
	d := net.Dialer{}
	raw, e := d.DialContext(ctx, "unix", c.Socket)
	if e != nil {
		if ctx.Err() != nil {
			return out, clientWaitError(ctx, method, e)
		}
		return out, domain.Fail("COORDINATOR_UNAVAILABLE", "start virmilld as your user: "+e.Error())
	}
	defer raw.Close()
	conn := raw.(*net.UnixConn)
	stopCancellation := context.AfterFunc(ctx, func() { conn.Close() })
	defer stopCancellation()
	uid, e := peer(conn)
	if e != nil || uid != uint32(os.Getuid()) {
		if ctx.Err() != nil {
			return out, clientWaitError(ctx, method, e)
		}
		return out, domain.Fail("PERMISSION_DENIED", "coordinator peer UID mismatch")
	}
	deadline := time.Now().Add(c.Timeout)
	if c.Timeout == 0 {
		deadline = time.Now().Add(30 * time.Second)
	}
	if dl, ok := ctx.Deadline(); ok {
		deadline = dl
	}
	conn.SetDeadline(deadline)
	id := "h-" + domain.ID()
	b, e := json.Marshal(Request{"2.0", id, method, r})
	if e != nil {
		return out, e
	}
	if len(b) > wire.MaxFrame {
		return out, errors.New("request too large")
	}
	if _, e = conn.Write(append(b, '\n')); e != nil {
		return out, clientWaitError(ctx, method, e)
	}
	frame, e := wire.ReadFrame(bufio.NewReader(conn))
	if e != nil {
		return out, clientWaitError(ctx, method, e)
	}
	var response reply
	if e = wire.Decode(frame, &response); e != nil {
		return out, e
	}
	if response.JSONRPC != "2.0" || response.ID != id {
		return out, errors.New("unexpected RPC response identity")
	}
	if response.Error != nil {
		return out, fmt.Errorf("RPC %d: %s", response.Error.Code, response.Error.Message)
	}
	if response.Result == nil {
		return out, errors.New("missing RPC result")
	}
	return *response.Result, nil
}

func clientWaitError(ctx context.Context, method string, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		message := "Request canceled."
		if heavyRead(method) {
			message = "Import inspection or preview canceled; no job was submitted."
		} else if method == "operation.apply" {
			message = "Stopped waiting for submission. Check Jobs before submitting again; accepted jobs continue."
		}
		return domain.Fail("CLIENT_INTERRUPTED", message)
	}
	var networkError net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
		message := "Request timed out; no result was received."
		if method == "import.inspect" || method == "import.describe" || method == "import.source.describe" {
			message = "Appliance inspection timed out; no job was submitted."
		} else if heavyRead(method) {
			message = "Import preview timed out; no job was submitted."
		} else if method == "operation.apply" {
			message = "Submission wait timed out; acceptance is uncertain. Check Jobs before submitting again."
		}
		return domain.Fail("WAIT_TIMEOUT", message)
	}
	if err == io.EOF {
		return errors.New("coordinator disconnected")
	}
	return err
}
