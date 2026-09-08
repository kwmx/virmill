package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
	"virmill.local/core/internal/store"
	"virmill.local/core/internal/ui"
)

type streamClientFunc func(context.Context, string, app.Request) (app.Response, error)

func (f streamClientFunc) Call(ctx context.Context, method string, r app.Request) (app.Response, error) {
	return f(ctx, method, r)
}
func streamEnvelope(data any) app.Response {
	return app.Response{APIVersion: domain.APIVersion, Data: data, Warnings: []string{}}
}
func streamJob(state string) domain.Job {
	return domain.Job{ID: "stream-job", PlanID: "reviewed-plan", State: state, CreatedAt: time.Date(2026, 9, 8, 12, 0, 0, 0, time.FixedZone("fixture", 3*3600))}
}
func streamEvent(seq int64) domain.Event {
	return domain.Event{APIVersion: domain.APIVersion, OperationID: "stream-job", Seq: seq, At: streamJob("").CreatedAt, Phase: "running", Severity: "info", Message: "fixture\nnot a new frame\x1b[2J"}
}

type streamFrame struct {
	APIVersion string          `json:"apiVersion"`
	Data       json.RawMessage `json:"data"`
	Warnings   []string        `json:"warnings"`
	Error      *domain.Error   `json:"error"`
}

func streamFrames(t *testing.T, out []byte) []streamFrame {
	t.Helper()
	if len(out) == 0 || out[len(out)-1] != '\n' || bytes.ContainsRune(out, '\x1b') {
		t.Fatalf("unclean or empty NDJSON: %q", out)
	}
	lines := bytes.Split(bytes.TrimSuffix(out, []byte{'\n'}), []byte{'\n'})
	result := make([]streamFrame, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal(line, &result[i]); err != nil {
			t.Fatalf("line %d: %v: %q", i, err, line)
		}
		if result[i].APIVersion != domain.APIVersion {
			t.Fatalf("line %d version differs", i)
		}
	}
	return result
}

type forbiddenPrompt struct{}

func (forbiddenPrompt) Read([]byte) (int, error) { panic("noninteractive event command read stdin") }
func executeStream(t *testing.T, ctx context.Context, client ui.Client, args ...string) ([]streamFrame, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	cmd := New(client, &out, &stderr)
	cmd.SetIn(forbiddenPrompt{})
	cmd.SetArgs(append(args, "--non-interactive"))
	err := cmd.ExecuteContext(ctx)
	if stderr.Len() != 0 {
		t.Fatalf("command wrote hidden human diagnostics: %q", stderr.String())
	}
	return streamFrames(t, out.Bytes()), err
}
func streamError(t *testing.T, err error, code string) {
	t.Helper()
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}
func streamCursor(t *testing.T, frame streamFrame, want int64) {
	t.Helper()
	if frame.Error == nil {
		t.Fatal("missing detach error")
	}
	details, ok := frame.Error.Details.(map[string]any)
	if !ok || details["cursor"] != float64(want) || details["detached"] != true {
		t.Fatalf("lost cursor/detachment: %#v", frame.Error.Details)
	}
}

func TestNDJSONFollowDrainsTerminalEventAndPreservesResumeCursor(t *testing.T) {
	calls := 0
	client := streamClientFunc(func(_ context.Context, method string, r app.Request) (app.Response, error) {
		calls++
		if r.Connection != "qemu:///session" || r.ID != "stream-job" || r.Apply != nil {
			t.Fatalf("request authority changed: %#v", r)
		}
		switch calls {
		case 1:
			if method != "operation.get" {
				t.Fatal(method)
			}
			return streamEnvelope(streamJob("running")), nil
		case 2:
			if method != "operation.watch" || r.After != 7 {
				t.Fatalf("cursor: %#v", r)
			}
			return streamEnvelope([]domain.Event{streamEvent(8), streamEvent(9)}), nil
		case 3:
			if method != "operation.get" {
				t.Fatal(method)
			}
			return streamEnvelope(streamJob("succeeded")), nil
		case 4:
			if method != "operation.watch" || r.After != 9 {
				t.Fatalf("final drain: %#v", r)
			}
			return streamEnvelope([]domain.Event{streamEvent(10)}), nil
		default:
			t.Fatalf("unexpected repeat %s", method)
			return app.Response{}, nil
		}
	})
	frames, err := executeStream(t, context.Background(), client, "operation", "watch", "stream-job", "--follow", "--after", "7", "--output", "ndjson", "--connection", "qemu:///session", "--quiet")
	if err != nil || len(frames) != 4 || calls != 4 {
		t.Fatalf("follow: frames=%d calls=%d err=%v", len(frames), calls, err)
	}
	for i, frame := range frames[:3] {
		var ev domain.Event
		if err := json.Unmarshal(frame.Data, &ev); err != nil || ev.Seq != int64(i+8) || ev.At.Location() != time.UTC || !bytes.Contains(frame.Data, []byte(`09:00:00Z`)) {
			t.Fatalf("event order/time: %s (%v)", frame.Data, err)
		}
	}
	var final domain.Job
	if err := json.Unmarshal(frames[3].Data, &final); err != nil || final.State != "succeeded" || final.CreatedAt.Location() != time.UTC || frames[3].Error != nil {
		t.Fatalf("terminal observation: %s", frames[3].Data)
	}
}

func TestNDJSONFollowDrainsFullBoundedBatches(t *testing.T) {
	watches := 0
	job := streamJob("succeeded")
	var out bytes.Buffer
	client := streamClientFunc(func(_ context.Context, method string, r app.Request) (app.Response, error) {
		if method != "operation.watch" {
			t.Fatalf("terminal job re-polled: %s", method)
		}
		watches++
		n := 1000
		if watches == 2 {
			n = 2
		}
		if watches > 2 || r.After != int64((watches-1)*1000) {
			t.Fatal("replay or wrong cursor", r)
		}
		batch := make([]domain.Event, n)
		for i := range batch {
			batch[i] = streamEvent(r.After + int64(i) + 1)
		}
		return streamEnvelope(batch), nil
	})
	err := followOperation(context.Background(), client, "qemu:///system", job.ID, 0, &job, true, func(r app.Response) error { return writeResponse(&out, "ndjson", false, r) })
	frames := streamFrames(t, out.Bytes())
	if err != nil || watches != 2 || len(frames) != 1003 {
		t.Fatalf("batch drain: %d %d %v", len(frames), watches, err)
	}
	var last domain.Event
	json.Unmarshal(frames[1001].Data, &last)
	if last.Seq != 1002 {
		t.Fatal("final cursor", last.Seq)
	}
}

func TestNDJSONFollowRefusesEntireAmbiguousBatch(t *testing.T) {
	cases := map[string]any{
		"duplicate":     []domain.Event{streamEvent(1), streamEvent(1)},
		"reordered":     []domain.Event{streamEvent(2), streamEvent(1)},
		"gap":           []domain.Event{streamEvent(1), streamEvent(3)},
		"null":          nil,
		"object":        map[string]any{"sequence": 1},
		"unknown-field": json.RawMessage(`[{"apiVersion":"virmill/v1","operationID":"stream-job","sequence":1,"timestamp":"2026-09-08T00:00:00Z","phase":"running","severity":"info","message":"fixture","extra":true}]`),
	}
	tooLarge := streamEvent(1)
	tooLarge.Message = strings.Repeat("x", 8<<20)
	cases["encoded-byte-limit"] = []domain.Event{tooLarge}
	foreign := streamEvent(2)
	foreign.OperationID = "other-job"
	cases["foreign-job"] = []domain.Event{streamEvent(1), foreign}
	version := streamEvent(2)
	version.APIVersion = "virmill/v999"
	cases["foreign-version"] = []domain.Event{streamEvent(1), version}
	zero := streamEvent(2)
	zero.At = time.Time{}
	cases["missing-time"] = []domain.Event{streamEvent(1), zero}
	oversized := make([]domain.Event, 1001)
	for i := range oversized {
		oversized[i] = streamEvent(int64(i + 1))
	}
	cases["over-limit"] = oversized
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			client := streamClientFunc(func(_ context.Context, method string, _ app.Request) (app.Response, error) {
				calls++
				if calls == 1 && method == "operation.get" {
					return streamEnvelope(streamJob("succeeded")), nil
				}
				if calls != 2 || method != "operation.watch" {
					t.Fatal("retried invalid batch")
				}
				return streamEnvelope(data), nil
			})
			frames, err := executeStream(t, context.Background(), client, "operation", "watch", "stream-job", "--follow", "--output", "ndjson")
			streamError(t, err, "INVALID_STATE")
			if len(frames) != 1 || calls != 2 {
				t.Fatalf("emitted prefix of invalid batch: %d", len(frames))
			}
			streamCursor(t, frames[0], 0)
		})
	}
}

func TestNDJSONFollowRejectsWrongJobBeforeReadingEvents(t *testing.T) {
	for _, test := range []struct {
		name string
		job  domain.Job
	}{
		{"foreign", func() domain.Job { j := streamJob("running"); j.ID = "other-job"; return j }()},
		{"unknown-state", streamJob("imaginary")},
		{"empty", domain.Job{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := streamClientFunc(func(_ context.Context, method string, _ app.Request) (app.Response, error) {
				calls++
				if method != "operation.get" || calls > 1 {
					t.Fatal("unbound watch dispatched")
				}
				return streamEnvelope(test.job), nil
			})
			frames, err := executeStream(t, context.Background(), client, "operation", "watch", "stream-job", "--follow", "--output", "ndjson")
			streamError(t, err, "INVALID_STATE")
			if len(frames) != 1 {
				t.Fatal(len(frames))
			}
		})
	}
}

func TestNDJSONFollowDetachRetainsCursorAndNeverCancels(t *testing.T) {
	for _, mode := range []string{"transport", "timeout", "interrupt"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			client := streamClientFunc(func(callCtx context.Context, method string, r app.Request) (app.Response, error) {
				calls++
				switch calls {
				case 1:
					if method != "operation.get" {
						t.Fatal(method)
					}
					return streamEnvelope(streamJob("running")), nil
				case 2:
					if method != "operation.watch" || r.After != 0 {
						t.Fatal(method, r)
					}
					return streamEnvelope([]domain.Event{streamEvent(1)}), nil
				case 3:
					if method != "operation.get" {
						t.Fatal("unexpected mutator", method)
					}
					if mode == "transport" {
						return app.Response{}, fmt.Errorf("transport: %w", domain.Fail("PERMISSION_DENIED", "fixture denial"))
					}
					if mode == "interrupt" {
						cancel()
					}
					<-callCtx.Done()
					return app.Response{}, callCtx.Err()
				default:
					t.Fatal("replayed after detach", method)
					return app.Response{}, nil
				}
			})
			args := []string{"operation", "watch", "stream-job", "--follow", "--output", "ndjson"}
			if mode == "timeout" {
				args = append(args, "--timeout", "100ms")
			}
			frames, err := executeStream(t, ctx, client, args...)
			want, exit := "PERMISSION_DENIED", 4
			if mode == "timeout" {
				want, exit = "WAIT_TIMEOUT", 7
			}
			if mode == "interrupt" {
				want, exit = "CLIENT_INTERRUPTED", 130
			}
			streamError(t, err, want)
			if domain.ExitCode(err) != exit || len(frames) != 2 || calls != 3 {
				t.Fatalf("detach: %v, %d frames, %d calls", err, len(frames), calls)
			}
			streamCursor(t, frames[1], 1)
		})
	}
}

func TestNDJSONFollowCancellationWhileIdleAndBeforeCall(t *testing.T) {
	for _, preCanceled := range []bool{false, true} {
		t.Run(fmt.Sprint(preCanceled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if preCanceled {
				cancel()
			}
			calls := 0
			client := streamClientFunc(func(_ context.Context, method string, _ app.Request) (app.Response, error) {
				calls++
				switch calls {
				case 1:
					return streamEnvelope(streamJob("running")), nil
				case 2:
					return streamEnvelope([]domain.Event{}), nil
				case 3:
					cancel()
					return streamEnvelope(streamJob("running")), nil
				default:
					t.Fatal("continued after cancellation", method)
					return app.Response{}, nil
				}
			})
			frames, err := executeStream(t, ctx, client, "operation", "watch", "stream-job", "--follow", "--output", "ndjson")
			streamError(t, err, "CLIENT_INTERRUPTED")
			if preCanceled && calls != 0 {
				t.Fatal("called after canceled context")
			}
			if len(frames) != 1 {
				t.Fatal(len(frames))
			}
			streamCursor(t, frames[0], 0)
		})
	}
	// Exercise cancellation of the bounded idle timer independently of a service call.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(waitEventPoll(ctx), context.Canceled) {
		t.Fatal("idle wait swallowed cancellation")
	}
}

func TestNDJSONFollowTerminalFailuresAreNonzero(t *testing.T) {
	for _, test := range []struct {
		state, code string
		exit        int
	}{{"failed", "OPERATION_FAILED", 1}, {"canceled", "CANCELED", 1}, {"partial", "PARTIAL_APPLY", 6}, {"recovery-required", "RECOVERY_REQUIRED", 6}} {
		t.Run(test.state, func(t *testing.T) {
			client := streamClientFunc(func(_ context.Context, method string, _ app.Request) (app.Response, error) {
				if method == "operation.get" {
					return streamEnvelope(streamJob(test.state)), nil
				}
				if method != "operation.watch" {
					t.Fatal(method)
				}
				return streamEnvelope([]domain.Event{}), nil
			})
			frames, err := executeStream(t, context.Background(), client, "operation", "watch", "stream-job", "--follow", "--output", "ndjson")
			streamError(t, err, test.code)
			if domain.ExitCode(err) != test.exit || len(frames) != 1 {
				t.Fatal(err, len(frames))
			}
		})
	}
	job := streamJob("failed")
	job.Error = domain.Fail("PERMISSION_REQUIRED", "specific authority denial")
	original := *job.Error
	var out bytes.Buffer
	err := writeResponse(&out, "ndjson", false, terminalResponse(job, nil, 7))
	streamError(t, err, "PERMISSION_REQUIRED")
	frames := streamFrames(t, out.Bytes())
	details := frames[0].Error.Details.(map[string]any)
	if frames[0].Error.OperationID != job.ID || details["operationID"] != job.ID || details["cursor"] != float64(7) || details["detached"] != false || details["cause"] == nil {
		t.Fatal("existing typed terminal error lost binding or cause", frames[0].Error)
	}
	if !reflect.DeepEqual(*job.Error, original) {
		t.Fatal("terminal formatting mutated shared job")
	}
}

type failedStreamWriter struct {
	calls int
	err   error
}

func (w *failedStreamWriter) Write(p []byte) (int, error) { w.calls++; return len(p) / 2, w.err }
func TestNDJSONFollowOutputFailureNeverRetriesOrAppends(t *testing.T) {
	for _, writeErr := range []error{nil, errors.New("fixture broken pipe")} {
		t.Run(fmt.Sprint(writeErr), func(t *testing.T) {
			writer := &failedStreamWriter{err: writeErr}
			calls := 0
			client := streamClientFunc(func(_ context.Context, method string, _ app.Request) (app.Response, error) {
				calls++
				if calls == 1 {
					return streamEnvelope(streamJob("running")), nil
				}
				if calls != 2 || method != "operation.watch" {
					t.Fatal("read/replay after broken stdout")
				}
				return streamEnvelope([]domain.Event{streamEvent(1), streamEvent(2)}), nil
			})
			cmd := New(client, writer, io.Discard)
			cmd.SetIn(forbiddenPrompt{})
			cmd.SetArgs([]string{"operation", "watch", "stream-job", "--follow", "--output", "ndjson", "--non-interactive"})
			err := cmd.Execute()
			want := writeErr
			if want == nil {
				want = io.ErrShortWrite
			}
			if !errors.Is(err, want) || writer.calls != 1 || calls != 2 {
				t.Fatalf("write failure retried: %v %d %d", err, writer.calls, calls)
			}
		})
	}
	var out bytes.Buffer
	if err := writeResponse(&out, "table", true, streamEnvelope(streamJob("succeeded"))); err != nil || out.Len() != 0 {
		t.Fatal("quiet success became typed nil error", err)
	}
}

func TestNDJSONFollowRejectsHiddenInputsAndKeepsSnapshotCompatibility(t *testing.T) {
	for _, args := range [][]string{
		{"operation", "watch", "stream-job", "--follow", "--output", "json"},
		{"operation", "watch", "stream-job", "--follow", "--output", "ndjson", "--after", "-1"},
		{"operation", "watch", "stream-job", "--follow", "--output", "ndjson", "--input", `{"cancel":true}`},
		{"operation", "watch", "bad/id", "--follow", "--output", "ndjson"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			client := streamClientFunc(func(context.Context, string, app.Request) (app.Response, error) {
				t.Fatal("invalid follow made a request")
				return app.Response{}, nil
			})
			cmd := New(client, io.Discard, io.Discard)
			cmd.SetIn(forbiddenPrompt{})
			cmd.SetArgs(append(args, "--non-interactive"))
			streamError(t, cmd.Execute(), "INVALID_INPUT")
		})
	}
	for _, format := range []string{"json", "ndjson"} {
		t.Run("snapshot-"+format, func(t *testing.T) {
			calls := 0
			client := streamClientFunc(func(_ context.Context, method string, r app.Request) (app.Response, error) {
				calls++
				if method != "operation.watch" || r.After != 7 {
					t.Fatal("snapshot gained follow calls")
				}
				return streamEnvelope([]domain.Event{streamEvent(8)}), nil
			})
			frames, err := executeStream(t, context.Background(), client, "operation", "watch", "stream-job", "--after", "7", "--output", format)
			if err != nil || calls != 1 || len(frames) != 1 || frames[0].Data[0] != '[' {
				t.Fatal("single response changed", err, len(frames))
			}
		})
	}
}

func TestPlanApplyWaitNDJSONUsesOneBoundSubmissionAndSharedFollow(t *testing.T) {
	for _, format := range []string{"json", "ndjson"} {
		t.Run(format, func(t *testing.T) {
			calls := []string{}
			watches := 0
			client := streamClientFunc(func(_ context.Context, method string, r app.Request) (app.Response, error) {
				calls = append(calls, method)
				if r.Connection != "qemu:///session" {
					t.Fatal("lost explicit connection")
				}
				switch method {
				case "operation.apply":
					if len(calls) != 1 || r.Apply == nil || r.Apply.PlanID != "reviewed-plan" || r.Apply.PlanDigest != "reviewed-digest" || r.Apply.IdempotencyKey != "same-key" || !reflect.DeepEqual(r.Apply.Acknowledgements, []string{"ack-a", "ack-b"}) {
						t.Fatalf("unbound/repeated apply: %#v", r.Apply)
					}
					return streamEnvelope(streamJob("queued")), nil
				case "operation.get":
					if r.ID != "stream-job" {
						t.Fatal(r)
					}
					return streamEnvelope(streamJob("succeeded")), nil
				case "operation.watch":
					watches++
					if format != "ndjson" || r.After != int64(watches-1) {
						t.Fatal("unexpected event polling", r)
					}
					return streamEnvelope([]domain.Event{streamEvent(int64(watches))}), nil
				default:
					t.Fatal("unexpected service method", method)
					return app.Response{}, nil
				}
			})
			frames, err := executeStream(t, context.Background(), client, "plan", "apply", "reviewed-plan", "--wait", "--digest", "reviewed-digest", "--idempotency-key", "same-key", "--ack", "ack-a,ack-b", "--output", format, "--connection", "qemu:///session")
			if err != nil {
				t.Fatal(err)
			}
			want := 1
			if format == "ndjson" {
				want = 4
				if !bytes.Contains(frames[0].Data, []byte(`"state":"queued"`)) || watches != 2 {
					t.Fatal("missing accepted observation or terminal drain")
				}
			}
			if len(frames) != want || !bytes.Contains(frames[len(frames)-1].Data, []byte(`"state":"succeeded"`)) {
				t.Fatalf("wait output: %d frames; calls %v", len(frames), calls)
			}
		})
	}
}

func TestPlanApplyWaitTransportFailureDoesNotResubmit(t *testing.T) {
	for _, afterAcceptance := range []bool{false, true} {
		t.Run(fmt.Sprint(afterAcceptance), func(t *testing.T) {
			calls := 0
			client := streamClientFunc(func(_ context.Context, method string, _ app.Request) (app.Response, error) {
				calls++
				if calls == 1 && method == "operation.apply" && afterAcceptance {
					return streamEnvelope(streamJob("queued")), nil
				}
				if calls > 2 {
					t.Fatal("retried lost acknowledgement")
				}
				return app.Response{}, errors.New("fixture transport disconnected")
			})
			frames, err := executeStream(t, context.Background(), client, "plan", "apply", "reviewed-plan", "--wait", "--digest", "reviewed-digest", "--idempotency-key", "same-key", "--output", "ndjson")
			streamError(t, err, "OPERATION_FAILED")
			want := 1
			if afterAcceptance {
				want = 2
			}
			if len(frames) != want || calls != want {
				t.Fatal("lost transport framing", len(frames), calls)
			}
			streamCursor(t, frames[len(frames)-1], 0)
		})
	}
}

func TestNDJSONFollowReadsRealSharedJournalWithoutChangingIt(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "private", "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := domain.Plan{ID: "recorded-plan", Digest: "recorded-digest"}
	if err = db.SavePlan(p, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	j, err := db.Accept(p, "fixture-key", "fixture-digest")
	if err != nil {
		t.Fatal(err)
	}
	j.State = "running"
	if err = db.Update(j, "generated intent, no native execution"); err != nil {
		t.Fatal(err)
	}
	j.State = "succeeded"
	if err = db.Update(j, "generated terminal record"); err != nil {
		t.Fatal(err)
	}
	beforeJob, _ := db.Job(j.ID)
	beforeEvents, _ := db.Events(j.ID, 0)
	service := app.New(nil, operations.New(db))
	methods := []string{}
	client := streamClientFunc(func(ctx context.Context, method string, r app.Request) (app.Response, error) {
		methods = append(methods, method)
		if method != "operation.get" && method != "operation.watch" {
			t.Fatal("read-only follow mutated journal", method)
		}
		return service.Call(ctx, 1000, method, r), nil
	})
	frames, err := executeStream(t, context.Background(), client, "operation", "watch", j.ID, "--follow", "--output", "ndjson")
	if err != nil || len(frames) != 3 || !reflect.DeepEqual(methods, []string{"operation.get", "operation.watch"}) {
		t.Fatal("shared journal follow", err, len(frames), methods)
	}
	afterJob, _ := db.Job(j.ID)
	afterEvents, _ := db.Events(j.ID, 0)
	if !reflect.DeepEqual(beforeJob, afterJob) || !reflect.DeepEqual(beforeEvents, afterEvents) {
		t.Fatal("read-only observer changed durable state")
	}
	for i := 0; i < 2; i++ {
		var ev domain.Event
		if err = json.Unmarshal(frames[i].Data, &ev); err != nil || !reflect.DeepEqual(ev, beforeEvents[i]) {
			t.Fatalf("shared event changed: %s", frames[i].Data)
		}
	}
}

func TestPlanApplyWaitRejectsForeignAcceptanceAndPreCanceledContext(t *testing.T) {
	for _, mode := range []string{"foreign-plan", "pre-canceled", "canceled-error-envelope"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "pre-canceled" {
				cancel()
			}
			calls := 0
			client := streamClientFunc(func(_ context.Context, method string, _ app.Request) (app.Response, error) {
				calls++
				if calls != 1 || method != "operation.apply" {
					t.Fatal("followed unbound acceptance")
				}
				if mode == "canceled-error-envelope" {
					cancel()
					return app.Response{APIVersion: domain.APIVersion, Error: domain.Fail("OPERATION_FAILED", "context canceled")}, nil
				}
				j := streamJob("queued")
				j.PlanID = "another-plan"
				return streamEnvelope(j), nil
			})
			frames, err := executeStream(t, ctx, client, "plan", "apply", "reviewed-plan", "--wait", "--digest", "reviewed-digest", "--idempotency-key", "same-key", "--output", "ndjson")
			want := "INVALID_STATE"
			if mode != "foreign-plan" {
				want = "CLIENT_INTERRUPTED"
			}
			streamError(t, err, want)
			if len(frames) != 1 || mode == "pre-canceled" && calls != 0 {
				t.Fatal("unbound acceptance emitted or canceled apply called")
			}
		})
	}
}

func TestNDJSONFollowRejectsChangedImmutableJobBinding(t *testing.T) {
	for _, mode := range []string{"plan", "created-at", "recovery-parent"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			client := streamClientFunc(func(_ context.Context, method string, _ app.Request) (app.Response, error) {
				calls++
				switch calls {
				case 1:
					return streamEnvelope(streamJob("running")), nil
				case 2:
					return streamEnvelope([]domain.Event{streamEvent(1)}), nil
				case 3:
					j := streamJob("succeeded")
					switch mode {
					case "plan":
						j.PlanID = "foreign-plan"
					case "created-at":
						j.CreatedAt = j.CreatedAt.Add(time.Second)
					case "recovery-parent":
						j.RecoveryOf = "other-parent"
					}
					return streamEnvelope(j), nil
				default:
					t.Fatal("continued after binding changed", method)
					return app.Response{}, nil
				}
			})
			frames, err := executeStream(t, context.Background(), client, "operation", "watch", "stream-job", "--follow", "--output", "ndjson")
			streamError(t, err, "INVALID_STATE")
			if len(frames) != 2 || calls != 3 {
				t.Fatal(len(frames), calls)
			}
			streamCursor(t, frames[1], 1)
		})
	}
}

func TestPlanApplyDetachedPreservesSingleResponse(t *testing.T) {
	for _, format := range []string{"json", "ndjson"} {
		t.Run(format, func(t *testing.T) {
			calls := 0
			client := streamClientFunc(func(_ context.Context, method string, r app.Request) (app.Response, error) {
				calls++
				if method != "operation.apply" || calls != 1 || r.Apply == nil {
					t.Fatal("detached apply followed or lost binding")
				}
				return streamEnvelope(streamJob("queued")), nil
			})
			frames, err := executeStream(t, context.Background(), client, "plan", "apply", "reviewed-plan", "--detach", "--digest", "reviewed-digest", "--idempotency-key", "same-key", "--output", format)
			if err != nil || calls != 1 || len(frames) != 1 || !bytes.Contains(frames[0].Data, []byte(`"state":"queued"`)) {
				t.Fatal("detached response changed", err, calls, len(frames))
			}
		})
	}
}

func TestNDJSONFollowStoredPartialErrorsRetainCauseAndExitSix(t *testing.T) {
	for _, state := range []string{"partial", "recovery-required"} {
		t.Run(state, func(t *testing.T) {
			db, err := store.Open(filepath.Join(t.TempDir(), "private", "journal.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			p := domain.Plan{ID: "partial-plan", Digest: "partial-digest"}
			if err = db.SavePlan(p, []byte(`{}`)); err != nil {
				t.Fatal(err)
			}
			j, err := db.Accept(p, "partial-key", "partial-request")
			if err != nil {
				t.Fatal(err)
			}
			j.State = state
			j.Error = domain.Fail("OPERATION_FAILED", "generated source failure with retained effects")
			j.Error.OperationID = j.ID
			j.Error.Details = map[string]any{"stage": "generated-effect"}
			j.Error.SafeNextActions = []string{"review recovery"}
			if err = db.Update(j, "generated partial outcome, no native execution"); err != nil {
				t.Fatal(err)
			}
			var before, after []byte
			if err = db.DB.QueryRow("SELECT body FROM jobs WHERE id=?", j.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			original, _ := db.Job(j.ID)
			originalError, _ := json.Marshal(original.Error)
			service := app.New(nil, operations.New(db))
			client := streamClientFunc(func(ctx context.Context, method string, r app.Request) (app.Response, error) {
				if method != "operation.get" && method != "operation.watch" {
					t.Fatal("terminal observation invoked mutator", method)
				}
				return service.Call(ctx, 1000, method, r), nil
			})
			frames, err := executeStream(t, context.Background(), client, "operation", "watch", j.ID, "--follow", "--output", "ndjson")
			want := "PARTIAL_APPLY"
			if state == "recovery-required" {
				want = "RECOVERY_REQUIRED"
			}
			streamError(t, err, want)
			if domain.ExitCode(err) != 6 || len(frames) != 2 {
				t.Fatalf("partial outcome reported incorrectly: exit=%d frames=%d", domain.ExitCode(err), len(frames))
			}
			final := frames[1]
			details, ok := final.Error.Details.(map[string]any)
			if !ok || final.Error.OperationID != j.ID || details["operationID"] != j.ID || details["cursor"] != float64(1) || details["detached"] != false {
				t.Fatalf("missing terminal binding: %#v", final.Error)
			}
			cause, err := json.Marshal(details["cause"])
			if err != nil {
				t.Fatal(err)
			}
			var preserved domain.Error
			if err = json.Unmarshal(cause, &preserved); err != nil {
				t.Fatal(err)
			}
			preservedBytes, _ := json.Marshal(&preserved)
			if !bytes.Equal(originalError, preservedBytes) {
				t.Fatalf("stored cause changed: %s / %s", originalError, preservedBytes)
			}
			var observed domain.Job
			if err = json.Unmarshal(final.Data, &observed); err != nil || !reflect.DeepEqual(observed, original) {
				t.Fatalf("job data changed: %s", final.Data)
			}
			if err = db.DB.QueryRow("SELECT body FROM jobs WHERE id=?", j.ID).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("terminal formatting changed durable job bytes")
			}
		})
	}
}
