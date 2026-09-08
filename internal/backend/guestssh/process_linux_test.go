//go:build linux

package guestssh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Real generated test processes exercise lifecycle and pipe behavior. They do
// not connect over SSH or qualify host-key authentication on a real guest.
func TestSSHProcessFixture(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "virmill-child-fixture" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "echo":
		b, _ := io.ReadAll(os.Stdin)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"input": string(b), "environment": os.Environ()})
		os.Exit(0)
	case "exit7":
		fmt.Fprint(os.Stdout, "guest result")
		os.Exit(7)
	case "exit255":
		fmt.Fprint(os.Stderr, "SECRET diagnostic")
		os.Exit(255)
	case "overflow":
		fmt.Fprint(os.Stdout, strings.Repeat("x", maxStdout+1))
		os.Exit(0)
	case "hang":
		for {
			time.Sleep(time.Second)
		}
	}
	os.Exit(99)
}
func TestSSHRealProcessBoundsAndExitPolicy(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer binary.Close()
	t.Setenv("SSH_AUTH_SOCK", "/poison/agent")
	t.Setenv("LD_PRELOAD", "/poison/library")
	for _, mode := range []string{"echo", "exit7", "exit255", "overflow", "hang"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if mode == "hang" {
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()
			}
			start := time.Now()
			r, e := execute(ctx, binary, []string{"-test.run=^TestSSHProcessFixture$", "--", "virmill-child-fixture", mode}, []byte("literal fixture stdin"), 2*time.Second)
			if time.Since(start) > 4*time.Second {
				t.Fatal("worker exceeded cleanup bound")
			}
			switch mode {
			case "echo":
				if e != nil || r.ExitCode != 0 {
					t.Fatal(r, e)
				}
				var got struct {
					Input       string
					Environment []string
				}
				if err = json.Unmarshal(r.Stdout, &got); err != nil {
					t.Fatal(err)
				}
				want := []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "LANG=C", "TZ=UTC", "SSH_ASKPASS_REQUIRE=never"}
				if got.Input != "literal fixture stdin" || !reflect.DeepEqual(got.Environment, want) {
					t.Fatal(got)
				}
			case "exit7":
				if e != nil || r.ExitCode != 7 || string(r.Stdout) != "guest result" {
					t.Fatal(r, e)
				}
			case "exit255":
				expectCode(t, e, "GUEST_TRANSPORT_FAILED")
				if !reflect.DeepEqual(r, Result{}) {
					t.Fatal("transport failure exposed output")
				}
			case "overflow":
				if e == nil || !reflect.DeepEqual(r, Result{}) {
					t.Fatal("overflow exposed output", e)
				}
			case "hang":
				if !errors.Is(e, context.DeadlineExceeded) || !reflect.DeepEqual(r, Result{}) {
					t.Fatal("canceled process not reaped", e)
				}
			}
		})
	}
}
