package protection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

func policyExample(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../virmill-v1-spec/examples/backup-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func policyInput(t *testing.T, cron, zone string, change func(map[string]any)) []byte {
	t.Helper()
	v, _, err := validation.Document(policyExample(t))
	if err != nil {
		t.Fatal(err)
	}
	spec := v["spec"].(map[string]any)
	schedule := spec["schedule"].(map[string]any)
	schedule["cron"], schedule["timezone"] = cron, zone
	if change != nil {
		change(spec)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func policyInstant(t *testing.T, text string) time.Time {
	t.Helper()
	instant, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		t.Fatal(err)
	}
	return instant
}

func policyInvalidResult(t *testing.T, report PolicyReport, err error) {
	t.Helper()
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != "INVALID_INPUT" || !reflect.DeepEqual(report, PolicyReport{}) {
		t.Fatalf("expected typed invalid input and zero report; report=%+v error=%v", report, err)
	}
}

func TestBackupPolicyNormativeExamplePreservesIntentWithoutScheduling(t *testing.T) {
	raw := policyExample(t)
	before := string(raw)
	report, err := ValidatePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != before || !report.Valid || report.APIVersion != domain.APIVersion ||
		report.Schedule != (PolicySchedule{"0 2 * * *", "Asia/Riyadh", "run-once"}) ||
		report.Capture != (PolicyCapture{"auto", "filesystem-consistent", false, "fail"}) ||
		report.PolicyName != "development-nightly" || report.RepositoryRef != "local-encrypted-backup" ||
		!reflect.DeepEqual(report.Selector.Tags, []string{"development"}) ||
		report.Retention != (PolicyRetention{7, 4, 3, true}) ||
		report.Verification != (PolicyVerification{true, "manual"}) {
		t.Fatalf("declaration changed: %+v", report)
	}
	if report.ValidationScope != "declaration" || report.HostPreflight != "not-run" || report.ScheduleInstalled ||
		report.CaptureVerified || report.IndependentRecoveryVerified || report.PreviewAfter != nil || len(report.NextRuns) != 0 {
		t.Fatalf("offline validation promoted to scheduling/recovery: %+v", report)
	}
	_, normalized, err := validation.Document(raw)
	if err != nil {
		t.Fatal(err)
	}
	jsonReport, err := ValidatePolicy(normalized)
	if err != nil || !reflect.DeepEqual(jsonReport, report) {
		t.Fatalf("JSON/YAML differ: %+v %v", jsonReport, err)
	}
}

func TestBackupPolicySchemaAndSelectionFailuresHaveNoSuccessPayload(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"duplicate_tags", func(s map[string]any) { s["selector"] = map[string]any{"tags": []string{"a", "a"}} }},
		{"duplicate_vm_ids", func(s map[string]any) { s["selector"] = map[string]any{"vmIDs": []string{"vm-a", "vm-a"}} }},
		{"blank_tag", func(s map[string]any) { s["selector"] = map[string]any{"tags": []string{" "}} }},
		{"padded_id", func(s map[string]any) { s["selector"] = map[string]any{"vmIDs": []string{" vm-a"}} }},
		{"control_id", func(s map[string]any) { s["selector"] = map[string]any{"vmIDs": []string{"vm\x00a"}} }},
		{"format_tag", func(s map[string]any) { s["selector"] = map[string]any{"tags": []string{"a\u202eb"}} }},
		{"both_selectors", func(s map[string]any) { s["selector"] = map[string]any{"vmIDs": []string{"a"}, "tags": []string{"b"}} }},
		{"unknown_field", func(s map[string]any) { s["future"] = true }},
		{"capture_fallback", func(s map[string]any) { s["capture"].(map[string]any)["onUnavailable"] = "downgrade" }},
		{"ignore_pins", func(s map[string]any) { s["retention"].(map[string]any)["respectPins"] = false }},
		{"no_verification", func(s map[string]any) { s["verification"].(map[string]any)["afterCapture"] = false }},
		{"unknown_missed_run", func(s map[string]any) { s["schedule"].(map[string]any)["missedRun"] = "replay-all" }},
		{"negative_retention", func(s map[string]any) { s["retention"].(map[string]any)["keepDaily"] = -1 }},
		{"inexact_retention", func(s map[string]any) { s["retention"].(map[string]any)["keepDaily"] = uint64(1<<53 + 1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := ValidatePolicy(policyInput(t, "0 2 * * *", "UTC", tc.edit))
			policyInvalidResult(t, report, err)
		})
	}
	raw := policyInput(t, "0 2 * * *", "UTC", nil)
	for _, changed := range [][]byte{
		[]byte(strings.Replace(string(raw), `"kind":"BackupPolicy"`, `"kind":"BackupPolicy","kind":"BackupPolicy"`, 1)),
		append(append([]byte{}, raw...), []byte(` {}`)...),
		[]byte("apiVersion: virmill/v1\nkind: BackupPolicy\nkind: BackupPolicy\n"),
	} {
		report, err := ValidatePolicy(changed)
		policyInvalidResult(t, report, err)
	}
}

func TestBackupPolicyCronRejectsInvalidUnsupportedAndImpossibleSchedules(t *testing.T) {
	for _, cron := range []string{
		"", "* * * *", "* * * * * *", "@daily", "CRON_TZ=UTC 0 2 * * *", "TZ=UTC 0 2 * * *",
		"60 * * * *", "0 24 * * *", "0 0 0 * *", "0 0 * 13 *", "0 0 * * 7",
		"0 0 * * FUNDAY", "0 0 * JANN *", "*/0 * * * *", "*/-1 * * * *", "*/+2 * * * *",
		"1-0 * * * *", "1-2-3 * * * *", "1//2 * * * *", "1, * * * *", ",1 * * * * *",
		"? * * * *", "0 0 L * *", "0 0 * * MON#2", "0 0 * * 1L", "0 0 31 FEB *",
		"0 0 31 APR *", "0 0 * * *\n", "0\x00 0 * * *", strings.Repeat("0", 513),
	} {
		t.Run(cron, func(t *testing.T) {
			report, err := ValidatePolicy(policyInput(t, cron, "UTC", nil))
			policyInvalidResult(t, report, err)
		})
	}
	for _, cron := range []string{"*/15 9-17 * * MON-FRI", "0,30 8,16 * JAN,MAR *", "5/10 * * * *", "0 0 29 FEB *", "0 0 31 FEB MON", "0/9223372036854775807 * * * *"} {
		if report, err := ValidatePolicy(policyInput(t, cron, "UTC", nil)); err != nil || !report.Valid || report.Schedule.Cron != cron {
			t.Fatalf("valid bounded grammar refused/rewritten %q: %+v %v", cron, report, err)
		}
	}
}

func TestBackupPolicyRequiresExplicitResolvableTimezone(t *testing.T) {
	for _, zone := range []string{"", " ", "Local", "localtime", "posixrules", "Asia/Does-Not-Exist", " Asia/Riyadh", "UTC ", "/etc/localtime", "../UTC", "Asia//Riyadh", "Asia\\Riyadh", "UTC\x00", "UTC\u202e"} {
		t.Run(zone, func(t *testing.T) {
			report, err := ValidatePolicy(policyInput(t, "0 2 * * *", zone, nil))
			policyInvalidResult(t, report, err)
		})
	}
}

func TestBackupPolicySelectorAndCronResourceBounds(t *testing.T) {
	for _, count := range []int{4096, 4097} {
		raw := policyInput(t, "0 2 * * *", "UTC", func(spec map[string]any) {
			tags := make([]string, count)
			for i := range tags {
				tags[i] = fmt.Sprintf("generated-tag-%04d", i)
			}
			spec["selector"] = map[string]any{"tags": tags}
		})
		report, err := ValidatePolicy(raw)
		if count == 4096 {
			if err != nil || !report.Valid || len(report.Selector.Tags) != count {
				t.Fatalf("bounded selector refused: %+v %v", report, err)
			}
		} else {
			policyInvalidResult(t, report, err)
		}
	}
	report, err := ValidatePolicy(policyInput(t, strings.Repeat("0,", 64)+"0 * * * *", "UTC", nil))
	policyInvalidResult(t, report, err)
	vm, err := os.ReadFile("../../../virmill-v1-spec/examples/vms/workstation.yaml")
	if err != nil {
		t.Fatal(err)
	}
	report, err = ValidatePolicy(vm)
	policyInvalidResult(t, report, err)
}

func TestBackupPolicyPreviewNamedTimezoneAndStrictFutureUTC(t *testing.T) {
	after := policyInstant(t, "2026-09-08T02:00:00+03:00")
	report, err := PreviewPolicy(policyExample(t), after, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-09-08T23:00:00Z", "2026-09-09T23:00:00Z", "2026-09-10T23:00:00Z"}
	for i, next := range report.NextRuns {
		if next.Location() != time.UTC || next.Format(time.RFC3339) != want[i] || !next.After(after) {
			t.Fatalf("occurrence %d differs: %s", i, next)
		}
	}
	if report.PreviewAfter == nil || report.PreviewAfter.Location() != time.UTC ||
		!report.PreviewAfter.Equal(after) || report.ScheduleInstalled || report.CaptureVerified || report.Schedule.MissedRun != "run-once" {
		t.Fatalf("preview changed policy or implied activation: %+v", report)
	}
}

func TestBackupPolicyPreviewDSTGapFoldAndHalfHourTransition(t *testing.T) {
	for _, tc := range []struct {
		name, cron, zone, after string
		want                    []string
	}{
		{"spring_gap", "30 2 * * *", "America/New_York", "2026-03-07T08:00:00Z", []string{"2026-03-09T06:30:00Z"}},
		{"autumn_fold", "30 1 * * *", "America/New_York", "2026-11-01T04:00:00Z", []string{"2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z"}},
		{"half_hour_fold", "45 1 * * *", "Australia/Lord_Howe", "2026-04-04T13:00:00Z", []string{"2026-04-04T14:45:00Z", "2026-04-04T15:15:00Z"}},
		{"sub_minute_history", "* * * * *", "Africa/Monrovia", "1970-01-01T00:00:01Z", []string{"1970-01-01T00:00:30Z", "1970-01-01T00:01:30Z"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := PreviewPolicy(policyInput(t, tc.cron, tc.zone, nil), policyInstant(t, tc.after), len(tc.want))
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range tc.want {
				if got := report.NextRuns[i].Format(time.RFC3339); got != want {
					t.Fatalf("occurrence %d: got %s want %s", i, got, want)
				}
			}
		})
	}
}

func TestBackupPolicyPreviewDayAlternativesLeapYearAndSteps(t *testing.T) {
	for _, tc := range []struct {
		cron, after string
		want        []string
	}{
		{"0 0 31 FEB MON", "2026-02-01T00:00:00Z", []string{"2026-02-02T00:00:00Z"}},
		{"0 0 * FEB MON", "2026-02-01T00:00:00Z", []string{"2026-02-02T00:00:00Z", "2026-02-09T00:00:00Z"}},
		{"0 0 */2 FEB MON", "2026-02-01T00:00:00Z", []string{"2026-02-09T00:00:00Z"}},
		{"0 0 29 FEB *", "2097-03-01T00:00:00Z", []string{"2104-02-29T00:00:00Z"}},
		{"5/10 * * * *", "2026-09-08T00:04:59.999999999Z", []string{"2026-09-08T00:05:00Z", "2026-09-08T00:15:00Z"}},
		{"0,30 8,16 * JAN,MAR *", "2026-01-01T08:00:00Z", []string{"2026-01-01T08:30:00Z", "2026-01-01T16:00:00Z"}},
	} {
		t.Run(tc.cron+tc.after, func(t *testing.T) {
			report, err := PreviewPolicy(policyInput(t, tc.cron, "UTC", nil), policyInstant(t, tc.after), len(tc.want))
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range tc.want {
				if got := report.NextRuns[i].Format(time.RFC3339); got != want {
					t.Fatalf("got %s want %s", got, want)
				}
			}
		})
	}
}

func TestBackupPolicyPreviewBoundsAndCancellationDiscardPartialOccurrences(t *testing.T) {
	raw := policyExample(t)
	after := policyInstant(t, "2026-09-08T00:00:00Z")
	for _, count := range []int{-1, 0, 33} {
		report, err := PreviewPolicy(raw, after, count)
		policyInvalidResult(t, report, err)
	}
	for _, invalid := range []time.Time{{}, time.Date(9992, 1, 1, 0, 0, 0, 0, time.UTC)} {
		report, err := PreviewPolicy(raw, invalid, 1)
		policyInvalidResult(t, report, err)
	}
	report, err := PreviewPolicy(policyInput(t, "0 0 1 JAN *", "UTC", nil), after, 32)
	policyInvalidResult(t, report, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err = PreviewPolicyContext(ctx, raw, after, 1)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(report, PolicyReport{}) {
		t.Fatalf("canceled request yielded a result: %+v %v", report, err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	report, err = PreviewPolicyContext(ctx, policyInput(t, "0 0 29 FEB *", "UTC", nil), after, 32)
	if !errors.Is(err, context.DeadlineExceeded) || !reflect.DeepEqual(report, PolicyReport{}) {
		t.Fatalf("active preview cancellation yielded partial success: %+v %v", report, err)
	}
}

func TestBackupPolicyDoesNotInventHostDependentCaptureRetentionRestrictions(t *testing.T) {
	raw := policyInput(t, "0 2 * * *", "UTC", func(s map[string]any) {
		s["selector"] = map[string]any{"vmIDs": []string{"stable-vm-reference"}}
		s["schedule"].(map[string]any)["missedRun"] = "skip"
		s["capture"].(map[string]any)["mode"] = "cold"
		s["capture"].(map[string]any)["consistency"] = "application-consistent"
		s["verification"].(map[string]any)["testRestore"] = "scheduled"
		s["retention"] = map[string]any{"keepDaily": 0, "keepWeekly": 0, "keepMonthly": 0, "respectPins": true}
	})
	report, err := ValidatePolicy(raw)
	if err != nil || !report.Valid || report.Capture.AllowShutdown || report.Capture.Consistency != "application-consistent" ||
		report.Schedule.MissedRun != "skip" || report.Verification.TestRestore != "scheduled" || report.ScheduleInstalled ||
		report.Retention.KeepDaily != 0 || report.HostPreflight != "not-run" {
		t.Fatalf("offline validator invented runtime decisions: %+v %v", report, err)
	}
}
