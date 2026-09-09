package guestsetup

import (
	"context"
	"reflect"
	"strconv"
	"time"

	"virmill.local/core/internal/app"
	"virmill.local/core/internal/backend/guestssh"
	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/operations"
)

type StageIntent struct {
	Version         int                `json:"version"`
	OperationID     string             `json:"operationID"`
	PlanID          string             `json:"planID"`
	PlanDigest      string             `json:"planDigest"`
	InputDigest     string             `json:"inputDigest"`
	Resource        domain.ResourceKey `json:"resource"`
	Stage           string             `json:"stage"`
	ScriptSHA256    string             `json:"scriptSHA256"`
	ArgumentsSHA256 string             `json:"argumentsSHA256"`
	TargetSHA256    string             `json:"targetSHA256"`
	SSHSHA256       string             `json:"sshSHA256"`
	RunPlanned      bool               `json:"runPlanned"`
	CreatedAt       time.Time          `json:"createdAt"`
}
type StageReceipt struct {
	TransportCompleted bool      `json:"transportCompleted"`
	ExitCode           *int      `json:"exitCode"`
	StdoutBytes        uint64    `json:"stdoutBytes"`
	StderrBytes        uint64    `json:"stderrBytes"`
	StdoutSHA256       string    `json:"stdoutSHA256"`
	StderrSHA256       string    `json:"stderrSHA256"`
	UID                *uint64   `json:"uid"`
	Outcome            string    `json:"outcome"`
	FinishedAt         time.Time `json:"finishedAt"`
}
type stageRecord struct {
	Intent  StageIntent   `json:"intent"`
	Receipt *StageReceipt `json:"receipt"`
}
type StageResult struct {
	Stage         string        `json:"stage"`
	Intent        *StageIntent  `json:"intent"`
	Receipt       *StageReceipt `json:"receipt"`
	Complete      bool          `json:"complete"`
	EffectUnknown bool          `json:"effectUnknown"`
}
type Result struct {
	OperationID                  string             `json:"operationID"`
	PlanID                       string             `json:"planID"`
	State                        string             `json:"state"`
	Resource                     domain.ResourceKey `json:"resource"`
	RecipeSHA256                 string             `json:"recipeSHA256"`
	ScriptSHA256                 map[string]string  `json:"scriptSHA256"`
	Stages                       []StageResult      `json:"stages"`
	Complete                     bool               `json:"complete"`
	NativeAddressBindingVerified bool               `json:"nativeAddressBindingVerified"`
	RawOutputRetained            bool               `json:"rawOutputRetained"`
}

func stageNumber(id string) int {
	for i, n := range stageNames {
		if id == n {
			return i
		}
	}
	return -1
}
func (s *Service) job(ctx context.Context, p domain.Plan) (domain.Job, error) {
	id := operations.OperationID(ctx)
	if !validUUID(id) {
		return domain.Job{}, domain.Fail("RECOVERY_REQUIRED", "guest recipe execution requires an accepted job identity")
	}
	j, err := s.Engine.Store.Job(id)
	if err != nil {
		return j, safeFailure(err, "guest recipe job unavailable")
	}
	if j.PlanID != p.ID {
		return j, domain.Fail("SOURCE_CHANGED", "guest recipe job and plan differ")
	}
	return j, nil
}
func intent(p domain.Plan, in frozenInput, jobID, name string, run bool) StageIntent {
	args := in.Arguments
	if name == "readiness" {
		args = []string{}
	}
	a, _ := operations.Digest(args)
	t, _ := operations.Digest(in.Target)
	return StageIntent{Version: 1, OperationID: jobID, PlanID: p.ID, PlanDigest: p.Digest, InputDigest: p.InputDigest, Resource: in.Resource, Stage: name, ScriptSHA256: in.ScriptSHA256[name], ArgumentsSHA256: a, TargetSHA256: t, SSHSHA256: in.Tool.SHA256, RunPlanned: run}
}
func utc(t time.Time) bool { _, offset := t.Zone(); return !t.IsZero() && offset == 0 }
func completed(name string, r *StageReceipt) bool {
	if r == nil {
		return false
	}
	switch name {
	case "readiness":
		return r.Outcome == "ready"
	case "check":
		return r.Outcome == "already-configured" || r.Outcome == "needs-apply"
	case "apply":
		return r.Outcome == "applied" || r.Outcome == "skip-apply"
	case "verify":
		return r.Outcome == "verified"
	}
	return false
}
func validReceipt(name string, run bool, r *StageReceipt) bool {
	if r == nil {
		return true
	}
	if !utc(r.FinishedAt) || !digestPattern.MatchString(r.StdoutSHA256) || !digestPattern.MatchString(r.StderrSHA256) || r.StdoutBytes > 1<<20 || r.StderrBytes > 1<<20 {
		return false
	}
	if !run {
		return name == "apply" && r.Outcome == "skip-apply" && !r.TransportCompleted && r.ExitCode == nil && r.UID == nil && r.StdoutBytes == 0 && r.StderrBytes == 0 && r.StdoutSHA256 == hash(nil) && r.StderrSHA256 == hash(nil)
	}
	if !r.TransportCompleted || r.ExitCode == nil || *r.ExitCode < 0 || *r.ExitCode > 254 {
		return false
	}
	if r.StdoutBytes == 0 && r.StdoutSHA256 != hash(nil) || r.StderrBytes == 0 && r.StderrSHA256 != hash(nil) {
		return false
	}
	want := "failed"
	switch name {
	case "readiness":
		if r.UID != nil {
			text := strconv.FormatUint(*r.UID, 10) + "\n"
			if *r.ExitCode != 0 || *r.UID == 0 || *r.UID > 1<<32-1 || r.StdoutBytes != uint64(len(text)) || r.StdoutSHA256 != hash([]byte(text)) || r.StderrBytes != 0 {
				return false
			}
			want = "ready"
		}
	case "check":
		if *r.ExitCode == 0 {
			want = "already-configured"
		} else if *r.ExitCode == 3 {
			want = "needs-apply"
		}
	case "apply":
		if *r.ExitCode == 0 {
			want = "applied"
		}
	case "verify":
		if *r.ExitCode == 0 {
			want = "verified"
		}
	default:
		return false
	}
	if name != "readiness" && r.UID != nil {
		return false
	}
	return r.Outcome == want
}
func (s *Service) stages(ctx context.Context, p domain.Plan, in frozenInput, j domain.Job) ([]StageResult, error) {
	results := make([]StageResult, 0, 4)
	priorComplete := true
	for _, name := range stageNames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row := StageResult{Stage: name}
		data, err := s.Engine.Store.MetadataBytes(stageKind, j.ID+":"+name)
		if err != nil {
			return nil, safeFailure(err, "guest stage journal unavailable")
		}
		if data == nil {
			priorComplete = false
			results = append(results, row)
			continue
		}
		if !priorComplete {
			return nil, domain.Fail("RECOVERY_REQUIRED", "later guest stage intent exists without completed prerequisite receipts")
		}
		if len(data) > 16384 {
			return nil, domain.Fail("RECOVERY_REQUIRED", "guest stage record exceeds bound")
		}
		var record stageRecord
		if err = strictDecode(data, &record); err != nil {
			return nil, domain.Fail("RECOVERY_REQUIRED", "guest stage record is malformed")
		}
		run := true
		if name == "apply" && results[1].Receipt.Outcome == "already-configured" {
			run = false
		}
		want := intent(p, in, j.ID, name, run)
		want.CreatedAt = record.Intent.CreatedAt
		if !reflect.DeepEqual(want, record.Intent) || !utc(record.Intent.CreatedAt) || record.Intent.CreatedAt.Before(j.CreatedAt) || !validReceipt(name, run, record.Receipt) || record.Receipt != nil && record.Receipt.FinishedAt.Before(record.Intent.CreatedAt) {
			return nil, domain.Fail("RECOVERY_REQUIRED", "guest stage receipt differs from its durable intent")
		}
		row.Intent, row.Receipt = &record.Intent, record.Receipt
		row.Complete = completed(name, record.Receipt)
		row.EffectUnknown = run && record.Receipt == nil
		priorComplete = row.Complete
		results = append(results, row)
	}
	return results, ctx.Err()
}
func (s *Service) Execute(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) error {
	if err := s.Validate(ctx, p, raw); err != nil {
		return err
	}
	in, err := decodeInput(p, raw)
	if err != nil {
		return err
	}
	j, err := s.job(ctx, p)
	if err != nil {
		return err
	}
	i := stageNumber(step.ID)
	if i < 0 || !reflect.DeepEqual(step, p.Steps[i]) || j.Step != i {
		return invalid("unexpected guest recipe execution stage")
	}
	state, err := s.stages(ctx, p, in, j)
	if err != nil {
		return err
	}
	for k := 0; k < i; k++ {
		if !state[k].Complete {
			return domain.Fail("RECOVERY_REQUIRED", "guest stage prerequisite receipt is incomplete")
		}
	}
	if state[i].Intent != nil {
		return domain.Fail("RECOVERY_REQUIRED", "guest stage already has durable intent; never rerun it")
	}
	run := !(step.ID == "apply" && state[1].Receipt.Outcome == "already-configured")
	record := stageRecord{Intent: intent(p, in, j.ID, step.ID, run)}
	record.Intent.CreatedAt = time.Now().UTC()
	if err = s.boundary(ctx); err != nil {
		return err
	}
	id := j.ID + ":" + step.ID
	if err = s.Engine.Store.ComparePut(stageKind, id, nil, record); err != nil {
		return safeFailure(err, "guest stage intent was not exclusively committed")
	}
	previous, err := s.Engine.Store.MetadataBytes(stageKind, id)
	if err != nil {
		return safeFailure(err, "guest stage intent readback failed")
	}
	var readback stageRecord
	if err = strictDecode(previous, &readback); err != nil || !reflect.DeepEqual(record, readback) {
		return domain.Fail("RECOVERY_REQUIRED", "guest stage intent readback differs; do not execute SSH")
	}
	var receipt StageReceipt
	if !run {
		receipt = StageReceipt{Outcome: "skip-apply", StdoutSHA256: hash(nil), StderrSHA256: hash(nil), FinishedAt: time.Now().UTC()}
	} else {
		if err = s.boundary(ctx); err != nil {
			return err
		}
		args := append([]string{}, in.Arguments...)
		if step.ID == "readiness" {
			args = []string{}
		}
		timeout := time.Duration(in.Recipe.Spec.TimeoutSeconds) * time.Second
		if step.ID == "readiness" && timeout > 10*time.Second {
			timeout = 10 * time.Second
		}
		child, stop := s.runContext(ctx, timeout)
		result, runErr := s.Transport.Run(child, in.Target, guestssh.Script{SSHSHA256: in.Tool.SHA256, Content: []byte(script(in.Recipe, step.ID)), Arguments: args, Timeout: timeout})
		contextErr := child.Err()
		stop()
		if runErr != nil {
			if in.Recipe.Spec.Privilege == "sudo" {
				return safeFailure(runErr, "guest tools completion is unknown; check Jobs and the guest before trying another installation; remote changes may continue")
			}
			return safeFailure(runErr, "SSH stage completion is unknown; output and diagnostics withheld")
		}
		if contextErr != nil {
			return contextErr
		}
		if result.ExitCode < 0 || result.ExitCode > 254 || len(result.Stdout) > 1<<20 || len(result.Stderr) > 1<<20 {
			return domain.Fail("RECOVERY_REQUIRED", "SSH stage returned an unsupported completion record")
		}
		receipt = received(step.ID, result)
	}
	record.Receipt = &receipt
	if err = s.Engine.Store.ComparePut(stageKind, id, previous, record); err != nil {
		return safeFailure(err, "guest stage receipt publication failed; retain its original intent")
	}
	if err = s.boundary(ctx); err != nil {
		return err
	}
	if !completed(step.ID, &receipt) {
		if in.Recipe.Spec.Privilege == "sudo" && step.ID != "readiness" && receipt.ExitCode != nil {
			return toolsStageFailure(*receipt.ExitCode)
		}
		return domain.Fail("GUEST_RECIPE_FAILED", "guest recipe stage failed its declared exit or non-root readiness policy; output withheld")
	}
	return nil
}
func received(name string, r guestssh.Result) StageReceipt {
	out := StageReceipt{TransportCompleted: true, ExitCode: &r.ExitCode, StdoutBytes: uint64(len(r.Stdout)), StderrBytes: uint64(len(r.Stderr)), StdoutSHA256: hash(r.Stdout), StderrSHA256: hash(r.Stderr), Outcome: "failed", FinishedAt: time.Now().UTC()}
	switch name {
	case "readiness":
		text := string(r.Stdout)
		if r.ExitCode == 0 && len(r.Stderr) == 0 && len(text) > 1 && text[len(text)-1] == '\n' {
			uid, err := strconv.ParseUint(text[:len(text)-1], 10, 32)
			if err == nil && uid > 0 && strconv.FormatUint(uid, 10)+"\n" == text {
				out.UID = &uid
				out.Outcome = "ready"
			}
		}
	case "check":
		if r.ExitCode == 0 {
			out.Outcome = "already-configured"
		} else if r.ExitCode == 3 {
			out.Outcome = "needs-apply"
		}
	case "apply":
		if r.ExitCode == 0 {
			out.Outcome = "applied"
		}
	case "verify":
		if r.ExitCode == 0 {
			out.Outcome = "verified"
		}
	}
	return out
}
func (s *Service) Reconcile(ctx context.Context, p domain.Plan, raw []byte, step domain.Step) (bool, error) {
	if err := s.boundary(ctx); err != nil {
		return false, err
	}
	in, err := decodeInput(p, raw)
	if err != nil {
		return false, err
	}
	j, err := s.job(ctx, p)
	if err != nil {
		return false, err
	}
	i := stageNumber(step.ID)
	if i < 0 || !reflect.DeepEqual(step, p.Steps[i]) || j.Step != i {
		return false, invalid("unexpected guest recipe reconciliation stage")
	}
	state, err := s.stages(ctx, p, in, j)
	if err != nil {
		return false, err
	}
	for k := 0; k <= i; k++ {
		if !state[k].Complete {
			return false, domain.Fail("RECOVERY_REQUIRED", "guest stage lacks a successful bound receipt; reconciliation never reruns SSH")
		}
	}
	if err = s.boundary(ctx); err != nil {
		return false, err
	}
	return true, nil
}
func (s *Service) Result(ctx context.Context, uid uint32, r app.Request) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validUUID(r.ID) || r.Path != "" || r.Action != "" || r.After != 0 || r.Apply != nil || len(r.Input) != 0 {
		return nil, invalid("result accepts only an operation UUID")
	}
	if s.Engine == nil || s.Engine.Store == nil {
		return nil, domain.Fail("UNSUPPORTED_CAPABILITY", "guest recipe journal unavailable")
	}
	j, err := s.Engine.Store.Job(r.ID)
	if err != nil {
		return nil, safeFailure(err, "guest recipe job unavailable")
	}
	p, raw, err := s.Engine.Store.Plan(j.PlanID)
	if err != nil {
		return nil, safeFailure(err, "guest recipe plan unavailable")
	}
	if uid == 0 || p.ActorUID != uid || r.Connection != p.ConnectionID {
		return nil, domain.Fail("PERMISSION_DENIED", "guest recipe belongs to another actor or connection")
	}
	digest, err := operations.PlanDigest(p)
	if err != nil || digest != p.Digest {
		return nil, domain.Fail("SOURCE_CHANGED", "guest recipe plan digest differs")
	}
	in, err := decodeInput(p, raw)
	if err != nil {
		return nil, err
	}
	state, err := s.stages(ctx, p, in, j)
	if err != nil {
		return nil, err
	}
	complete := j.State == "succeeded"
	for _, stage := range state {
		complete = complete && stage.Complete
	}
	if j.State == "succeeded" && !complete {
		return nil, domain.Fail("RECOVERY_REQUIRED", "successful guest job lacks complete stage receipts")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return Result{OperationID: j.ID, PlanID: p.ID, State: j.State, Resource: in.Resource, RecipeSHA256: in.RecipeSHA256, ScriptSHA256: in.ScriptSHA256, Stages: state, Complete: complete}, nil
}
