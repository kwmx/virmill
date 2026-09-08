//go:build linux

package guestssh

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type boundedOutput struct {
	mu       sync.Mutex
	data     bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.overflow || len(p) > b.limit-b.data.Len() {
		b.overflow = true
		b.cancel()
		return len(p), nil
	}
	return b.data.Write(p)
}
func (b *boundedOutput) snapshot() ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data.Bytes()...), b.overflow
}

// Waitid(WNOWAIT) observes the owned leader's exit without reaping it. Kill its
// group before cmd.Wait, while that unreaped PID cannot be recycled into an
// unrelated process group. This also closes pipes held by surviving children.
func execute(ctx context.Context, binary *os.File, args []string, stdin []byte, timeout time.Duration) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if timeout <= 0 || timeout > maxTimeout {
		return Result{}, failure("INVALID_INPUT", "process timeout is outside bounds")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.Command("/proc/self/fd/3", args...)
	cmd.Args[0] = executable
	cmd.ExtraFiles = []*os.File{binary}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "LANG=C", "TZ=UTC", "SSH_ASKPASS_REQUIRE=never"}
	cmd.Dir = "/"
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.WaitDelay = 2 * time.Second
	stdout := &boundedOutput{limit: maxStdout, cancel: cancel}
	stderr := &boundedOutput{limit: maxStderr, cancel: cancel}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Start(); err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, failure("OPERATION_FAILED", "SSH process could not start; diagnostics withheld")
	}
	exited := make(chan error, 1)
	go func() {
		var info unix.Siginfo
		for {
			err := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			exited <- err
			return
		}
	}()
	var observation error
	observed := false
	select {
	case observation = <-exited:
		observed = true
	case <-ctx.Done():
	}
	killErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if !observed {
		observation = <-exited
	}
	waitErr := cmd.Wait()
	out, overflowOut := stdout.snapshot()
	diagnostic, overflowErr := stderr.snapshot()
	if overflowOut || overflowErr {
		return Result{}, failure("OPERATION_FAILED", "SSH output exceeded its bound; guest effects require observation")
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if observation != nil || killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		return Result{}, failure("OPERATION_FAILED", "SSH child lifecycle could not be verified; diagnostics withheld")
	}
	if errors.Is(waitErr, exec.ErrWaitDelay) {
		return Result{}, failure("OPERATION_FAILED", "SSH child pipes did not close within the cleanup bound")
	}
	code := 0
	if waitErr != nil {
		var exit *exec.ExitError
		if !errors.As(waitErr, &exit) || exit.ExitCode() < 0 {
			return Result{}, failure("OPERATION_FAILED", "SSH process failed; diagnostics withheld")
		}
		code = exit.ExitCode()
	}
	if code == 255 {
		return Result{}, failure("GUEST_TRANSPORT_FAILED", "SSH transport, authentication or strict host-key verification failed; diagnostics withheld, no fallback attempted")
	}
	return Result{ExitCode: code, Stdout: out, Stderr: diagnostic}, nil
}
