package domain

import "testing"

func TestClientWaitExitCodesDistinguishDetachFromJobFailure(t *testing.T) {
	for _, c := range []struct {
		code string
		want int
	}{
		{"CLIENT_INTERRUPTED", 130},
		{"WAIT_TIMEOUT", 7},
		{"RECOVERY_REQUIRED", 6},
		{"OPERATION_FAILED", 1},
	} {
		t.Run(c.code, func(t *testing.T) {
			if got := ExitCode(Fail(c.code, "fixture")); got != c.want {
				t.Fatalf("exit code = %d, want %d", got, c.want)
			}
		})
	}
	if ExitCode(nil) != 0 {
		t.Fatal("accepted detached submission must retain exit zero")
	}
}
