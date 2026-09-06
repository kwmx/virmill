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
}

func Listen(path string, service *app.Service) (*Server, error) {
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
	return &Server{Listener: l, lock: lock, Service: service, ctx: ctx, cancel: cancel, connections: map[*net.UnixConn]bool{}}, nil
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
					result := s.Service.Call(s.ctx, uid, r.Method, r.Params)
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
		return out, domain.Fail("COORDINATOR_UNAVAILABLE", "start virmilld as your user: "+e.Error())
	}
	defer raw.Close()
	conn := raw.(*net.UnixConn)
	uid, e := peer(conn)
	if e != nil || uid != uint32(os.Getuid()) {
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
		return out, e
	}
	frame, e := wire.ReadFrame(bufio.NewReader(conn))
	if e != nil {
		if ne, ok := e.(net.Error); ok && ne.Timeout() {
			return out, domain.Fail("WAIT_TIMEOUT", "client wait timed out; accepted server jobs continue")
		}
		if e == io.EOF {
			return out, errors.New("coordinator disconnected")
		}
		return out, e
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
