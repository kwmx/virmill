//go:build linux && amd64

package restic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A generated-file subprocess fixture, never the production restic executable.
// The inherited descriptor layout and command lifetime are the real transport.
func TestResticProcessHelper(t *testing.T) {
	if len(os.Args) < 2 || !strings.HasPrefix(os.Args[len(os.Args)-1], "restic-fixture-") {
		return
	}
	mode := strings.TrimPrefix(os.Args[len(os.Args)-1], "restic-fixture-")
	if mode == "fds" {
		if os.Getenv("RESTIC_PASSWORD") != "" || os.Getenv("RESTIC_REPOSITORY") != "" || os.Getenv("RESTIC_PASSWORD_COMMAND") != "" || os.Getenv("LD_PRELOAD") != "" {
			os.Exit(90)
		}
		b, err := os.ReadFile("/proc/self/fd/5")
		if err != nil || string(b) != testPassword {
			os.Exit(91)
		}
		b, err = os.ReadFile("member")
		if err != nil || string(b) != "generated captured member" {
			os.Exit(92)
		}
		cwd, err := os.Getwd()
		if err != nil {
			os.Exit(93)
		}
		fmt.Print(cwd)
		os.Exit(0)
	}
	if mode == "hang" {
		if err := os.WriteFile("/proc/self/fd/4/worker-pid", []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			os.Exit(94)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	if mode == "stdout-flood" {
		fmt.Print(strings.Repeat("x", maxOutput+1))
		for {
			time.Sleep(time.Second)
		}
	}
	if mode == "stderr-flood" {
		fmt.Fprint(os.Stderr, strings.Repeat("opaque-secret", maxDiagnostic))
		for {
			time.Sleep(time.Second)
		}
	}
	if strings.HasPrefix(mode, "exit-") {
		code, _ := strconv.Atoi(strings.TrimPrefix(mode, "exit-"))
		fmt.Fprint(os.Stderr, "opaque-secret in raw diagnostic\n")
		fmt.Print(`{"message_type":"summary","snapshot_id":"` + testSnapshot + `"}`)
		os.Exit(code)
	}
	os.Exit(95)
}

func processFixture(t *testing.T, mode string) (*os.File, invocation, string) {
	t.Helper()
	r, source := fixture(t)
	p, err := prepare(context.Background(), r, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.close)
	s, err := openPrivate(source, true, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { binary.Close() })
	return binary, invocation{args: []string{"-test.run=^TestResticProcessHelper$", "--", "restic-fixture-" + mode}, files: []*os.File{p.repo.f, p.password.f, s.f}, dir: fmt.Sprintf("/proc/self/fd/%d", s.f.Fd())}, r.Path
}

func TestResticProcessInheritedPasswordAndPinnedWorkingDirectory(t *testing.T) {
	t.Setenv("RESTIC_PASSWORD", "must-not-inherit")
	t.Setenv("RESTIC_PASSWORD_COMMAND", "must-not-run")
	t.Setenv("RESTIC_REPOSITORY", "sftp:must-not-contact")
	binary, in, _ := processFixture(t, "fds")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := runProcess(ctx, binary, in)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Readlink(in.dir)
	if err != nil || string(b) != want {
		t.Fatal("working directory did not bind original source path", string(b), want, err)
	}
}

func TestResticProcessExitBoundsCancellationAndReaping(t *testing.T) {
	for _, tc := range []struct{ mode, code string }{{"exit-3", "INCOMPLETE_BACKUP"}, {"exit-10", "NOT_FOUND"}, {"exit-11", "RESOURCE_BUSY"}, {"exit-12", "PERMISSION_DENIED"}, {"exit-99", "OPERATION_FAILED"}, {"stdout-flood", "OPERATION_FAILED"}, {"stderr-flood", "OPERATION_FAILED"}, {"hang", ""}} {
		t.Run(tc.mode, func(t *testing.T) {
			binary, in, repo := processFixture(t, tc.mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if tc.mode == "hang" {
				go func() {
					defer cancel()
					deadline := time.Now().Add(3 * time.Second)
					for time.Now().Before(deadline) {
						if _, err := os.Stat(filepath.Join(repo, "worker-pid")); err == nil {
							return
						}
						time.Sleep(5 * time.Millisecond)
					}
				}()
			}
			start := time.Now()
			b, err := runProcess(ctx, binary, in)
			if err == nil || len(b) != 0 || time.Since(start) > 4*time.Second {
				t.Fatal("failed process returned output or outlived bound", len(b), err, time.Since(start))
			}
			if tc.code != "" {
				assertError(t, err, tc.code)
			} else {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
				b, e := os.ReadFile(filepath.Join(repo, "worker-pid"))
				if e != nil {
					t.Fatal(e)
				}
				pid, e := strconv.Atoi(string(b))
				if e != nil {
					t.Fatal(e)
				}
				if e = syscall.Kill(pid, 0); e != syscall.ESRCH {
					t.Fatal("worker not reaped", pid, e)
				}
			}
		})
	}
}

func TestResticSystemIdentityFailsExplicitlyWhenUnavailable(t *testing.T) {
	if _, err := os.Stat(executable); !os.IsNotExist(err) {
		t.Skip("system identity runtime qualification belongs to installed dependency evidence")
	}
	id, err := (Tool{}).Identity(context.Background())
	if id != (Identity{}) {
		t.Fatal("missing tool exposed identity", id)
	}
	assertError(t, err, "UNSUPPORTED_CAPABILITY")
}
