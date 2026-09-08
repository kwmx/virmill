package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/ui"
	"virmill.local/core/internal/validation"
	"virmill.local/core/internal/wire"
)

const eventBatchLimit = 1000
const eventPollInterval = 200 * time.Millisecond

var eventOperationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type responseEmitter func(app.Response) error

// writeResponse preserves the existing single-response shape and reports broken
// pipes/short writes. Once a write fails, callers must not append another frame.
func writeResponse(out io.Writer, format string, quiet bool, response app.Response) error {
	if quiet && format == "table" {
		if response.Error != nil {
			return response.Error
		}
		return nil
	}
	var data []byte
	var err error
	if format == "table" {
		data, err = json.MarshalIndent(response, "", "  ")
	} else {
		data, err = json.Marshal(response)
	}
	if err != nil {
		return err
	}
	if format == "table" {
		data = []byte(validation.SafeText(string(data)))
	}
	data = append(data, '\n')
	n, err := out.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	if response.Error != nil {
		return response.Error
	}
	return nil
}

func responseFailure(err error) *domain.Error {
	var typed *domain.Error
	if errors.As(err, &typed) {
		copy := *typed
		copy.SafeNextActions = append([]string{}, typed.SafeNextActions...)
		return &copy
	}
	return domain.Fail("OPERATION_FAILED", err.Error())
}
func streamFailure(ctx context.Context, err error, id string, cursor int64, last *domain.Job) app.Response {
	failure := responseFailure(err)
	switch ctx.Err() {
	case context.Canceled:
		failure = domain.Fail("CLIENT_INTERRUPTED", "client interrupted; following detached without canceling the operation")
	case context.DeadlineExceeded:
		failure = domain.Fail("WAIT_TIMEOUT", "client wait timed out; operation may still be running")
	}
	details := map[string]any{"operationID": id, "cursor": cursor, "detached": true}
	if failure.Details != nil {
		details["cause"] = failure.Details
	}
	failure.Details = details
	failure.OperationID = id
	response := app.Response{APIVersion: domain.APIVersion, Warnings: []string{"Following stopped; no operation cancellation was requested"}, Error: failure}
	if last != nil {
		response.Data = *last
	}
	return response
}
func decodeStreamData(value any, target any) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > wire.MaxFrame || string(data) == "null" {
		return domain.Fail("INVALID_STATE", "operation response data is absent or exceeds bounds")
	}
	if err = wire.Decode(data, target); err != nil {
		return domain.Fail("INVALID_STATE", "operation response data does not match its contract")
	}
	return nil
}
func decodeStreamJob(response app.Response, id string) (domain.Job, error) {
	var job domain.Job
	if response.APIVersion != domain.APIVersion {
		return job, domain.Fail("INVALID_STATE", "operation response version differs")
	}
	if response.Error != nil {
		return job, response.Error
	}
	if err := decodeStreamData(response.Data, &job); err != nil {
		return job, err
	}
	if !eventOperationID.MatchString(job.ID) || (id != "" && job.ID != id) || job.PlanID == "" || job.CreatedAt.IsZero() || job.Step < 0 {
		return domain.Job{}, domain.Fail("INVALID_STATE", "operation response identity or state is incomplete")
	}
	switch job.State {
	case "queued", "validating", "awaiting-approval", "running", "verifying", "cancel-requested", "canceling", "interrupted", "reconciling", "succeeded", "failed", "partial", "canceled", "recovery-required":
	default:
		return domain.Job{}, domain.Fail("INVALID_STATE", "operation response has an unknown state")
	}
	job.CreatedAt = job.CreatedAt.UTC()
	return job, nil
}
func terminalResponse(job domain.Job, warnings []string, cursor int64) app.Response {
	response := app.Response{APIVersion: domain.APIVersion, Data: job, Warnings: warnings}
	if response.Warnings == nil {
		response.Warnings = []string{}
	}
	switch job.State {
	case "partial":
		response.Error = domain.Fail("PARTIAL_APPLY", "operation has retained partial effects")
	case "recovery-required":
		response.Error = domain.Fail("RECOVERY_REQUIRED", "operation requires reviewed recovery")
	default:
		if job.Error != nil {
			response.Error = responseFailure(job.Error)
		} else {
			switch job.State {
			case "failed":
				response.Error = domain.Fail("OPERATION_FAILED", "operation failed")
			case "canceled":
				response.Error = domain.Fail("CANCELED", "operation was canceled")
			}
		}
	}
	if response.Error != nil {
		// The terminal state determines whether recovery is required. Keep the
		// original failure intact in Job data and as the cause of this envelope.
		response.Error.OperationID = job.ID
		details := map[string]any{"operationID": job.ID, "cursor": cursor, "detached": false}
		if job.Error != nil {
			details["cause"] = job.Error
		}
		response.Error.Details = details
	}
	return response
}
func streamCall(ctx context.Context, client ui.Client, method string, r app.Request) (app.Response, error) {
	if err := ctx.Err(); err != nil {
		return app.Response{}, err
	}
	response, err := client.Call(ctx, method, r)
	if err != nil {
		return app.Response{}, err
	}
	if err = ctx.Err(); err != nil {
		return app.Response{}, err
	}
	if response.APIVersion != domain.APIVersion {
		return app.Response{}, domain.Fail("INVALID_STATE", "operation response version differs")
	}
	if response.Error != nil {
		return app.Response{}, response.Error
	}
	return response, nil
}
func decodeEventBatch(response app.Response, id string, after int64) ([]domain.Event, error) {
	var events []domain.Event
	if err := decodeStreamData(response.Data, &events); err != nil {
		return nil, err
	}
	if len(events) > eventBatchLimit {
		return nil, domain.Fail("INVALID_STATE", "operation event batch exceeds the service bound")
	}
	previous := after
	for i := range events {
		event := &events[i]
		if previous == math.MaxInt64 || event.APIVersion != domain.APIVersion || event.OperationID != id || event.Seq != previous+1 || event.At.IsZero() || event.Phase == "" || event.Severity == "" {
			return nil, domain.Fail("INVALID_STATE", "operation event identity, time or contiguous cursor differs")
		}
		previous = event.Seq
		event.At = event.At.UTC()
	}
	return events, nil
}
func waitEventPoll(ctx context.Context) error {
	timer := time.NewTimer(eventPollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// followOperation retains one bounded batch. A terminal state is observed before
// the final event drain because the store commits a job transition and its event
// together. The final response is an observation, not a claim that later recovery
// operations cannot add more history. It never applies, retries or cancels a job.
func followOperation(ctx context.Context, client ui.Client, connection, id string, after int64, initial *domain.Job, events bool, emit responseEmitter) error {
	if !eventOperationID.MatchString(id) || after < 0 {
		return domain.Fail("INVALID_INPUT", "bounded operation ID and nonnegative event cursor required")
	}
	cursor := after
	var last *domain.Job
	failure := func(err error) error { return emit(streamFailure(ctx, err, id, cursor, last)) }
	var warnings []string
	if initial != nil {
		copy := *initial
		last = &copy
	} else {
		response, err := streamCall(ctx, client, "operation.get", app.Request{Connection: connection, ID: id})
		if err != nil {
			return failure(err)
		}
		job, err := decodeStreamJob(response, id)
		if err != nil {
			return failure(err)
		}
		last = &job
		warnings = response.Warnings
	}
	terminalSeen := domain.Terminal(last.State)
	for {
		if err := ctx.Err(); err != nil {
			return failure(err)
		}
		if events {
			response, err := streamCall(ctx, client, "operation.watch", app.Request{Connection: connection, ID: id, After: cursor})
			if err != nil {
				return failure(err)
			}
			batch, err := decodeEventBatch(response, id, cursor)
			if err != nil {
				return failure(err)
			}
			// Validate the whole batch before emitting any of it. Cursor advances only
			// after its complete envelope write succeeds; a partial write cannot be undone.
			for i, event := range batch {
				if err := ctx.Err(); err != nil {
					return failure(err)
				}
				notices := []string{}
				if i == 0 {
					notices = append(notices, response.Warnings...)
				}
				if err := emit(app.Response{APIVersion: domain.APIVersion, Data: event, Warnings: notices}); err != nil {
					return err
				}
				cursor = event.Seq
			}
			if len(batch) == 0 {
				warnings = append(warnings, response.Warnings...)
			}
			if len(batch) == eventBatchLimit {
				continue
			}
		}
		if terminalSeen {
			if err := ctx.Err(); err != nil {
				return failure(err)
			}
			return emit(terminalResponse(*last, warnings, cursor))
		}
		response, err := streamCall(ctx, client, "operation.get", app.Request{Connection: connection, ID: id})
		if err != nil {
			return failure(err)
		}
		job, err := decodeStreamJob(response, id)
		if err != nil {
			return failure(err)
		}
		if job.PlanID != last.PlanID || !job.CreatedAt.Equal(last.CreatedAt) || job.RecoveryOf != last.RecoveryOf {
			return failure(domain.Fail("INVALID_STATE", "operation immutable identity changed while following"))
		}
		last = &job
		warnings = response.Warnings
		terminalSeen = domain.Terminal(job.State)
		if terminalSeen {
			continue
		}
		if err := waitEventPoll(ctx); err != nil {
			return failure(err)
		}
	}
}
