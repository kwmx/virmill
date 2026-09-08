package protection

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Named zones remain available without a host tzdata package.
	"unicode"

	"virmill.local/core/internal/domain"
	"virmill.local/core/internal/validation"
)

const (
	MaxPolicyPreviewCount = 32
	PolicyPreviewYears    = 8
	policyCronLimit       = 512
	policySelectorLimit   = 4096
)

type PolicySchedule struct {
	Cron      string `json:"cron"`
	Timezone  string `json:"timezone"`
	MissedRun string `json:"missedRun"`
}

type PolicySelector struct {
	VMIDs []string `json:"vmIDs,omitempty"`
	Tags  []string `json:"tags,omitempty"`
}

type PolicyCapture struct {
	Mode          string `json:"mode"`
	Consistency   string `json:"consistency"`
	AllowShutdown bool   `json:"allowShutdown"`
	OnUnavailable string `json:"onUnavailable"`
}

type PolicyRetention struct {
	KeepDaily   uint64 `json:"keepDaily"`
	KeepWeekly  uint64 `json:"keepWeekly"`
	KeepMonthly uint64 `json:"keepMonthly"`
	RespectPins bool   `json:"respectPins"`
}

type PolicyVerification struct {
	AfterCapture bool   `json:"afterCapture"`
	TestRestore  string `json:"testRestore"`
}

// PolicyReport describes an offline declaration and optional occurrence preview.
// It is not a persisted policy, an occurrence job, or standing authorization.
type PolicyReport struct {
	APIVersion                  string             `json:"apiVersion"`
	Valid                       bool               `json:"valid"`
	ValidationScope             string             `json:"validationScope"`
	PolicyName                  string             `json:"policyName"`
	RepositoryRef               string             `json:"repositoryRef"`
	Selector                    PolicySelector     `json:"selector"`
	Schedule                    PolicySchedule     `json:"schedule"`
	Capture                     PolicyCapture      `json:"capture"`
	Retention                   PolicyRetention    `json:"retention"`
	Verification                PolicyVerification `json:"verification"`
	PreviewAfter                *time.Time         `json:"previewAfter,omitempty"`
	NextRuns                    []time.Time        `json:"nextRuns"`
	HostPreflight               string             `json:"hostPreflight"`
	ScheduleInstalled           bool               `json:"scheduleInstalled"`
	CaptureVerified             bool               `json:"captureVerified"`
	IndependentRecoveryVerified bool               `json:"independentRecoveryVerified"`
}

type policyDeclaration struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		RepositoryRef string             `json:"repositoryRef"`
		Selector      PolicySelector     `json:"selector"`
		Schedule      PolicySchedule     `json:"schedule"`
		Capture       PolicyCapture      `json:"capture"`
		Retention     PolicyRetention    `json:"retention"`
		Verification  PolicyVerification `json:"verification"`
	} `json:"spec"`
}

type policyField struct {
	bits uint64
	star bool
}

func (f policyField) contains(value int) bool { return f.bits&(uint64(1)<<value) != 0 }

type policyCron struct {
	minute, hour, day, month, weekday policyField
	location                          *time.Location
}

func policyInvalid(message string) error { return domain.Fail("INVALID_INPUT", message) }

// ValidatePolicy checks strict bundled JSON/YAML schema, selector uniqueness,
// named timezone, the documented five-field cron grammar and calendar feasibility.
// It never resolves repository/VM references or inspects credentials/capabilities.
func ValidatePolicy(raw []byte) (PolicyReport, error) {
	report, _, err := parsePolicy(raw)
	return report, err
}

func parsePolicy(raw []byte) (PolicyReport, policyCron, error) {
	value, normalized, err := validation.Document(raw)
	if err != nil {
		return PolicyReport{}, policyCron{}, policyInvalid(err.Error())
	}
	if value["kind"] != "BackupPolicy" {
		return PolicyReport{}, policyCron{}, policyInvalid("BackupPolicy document required")
	}
	var declaration policyDeclaration
	if err := json.Unmarshal(normalized, &declaration); err != nil {
		return PolicyReport{}, policyCron{}, policyInvalid("backup policy values exceed supported integer or field bounds")
	}
	spec := declaration.Spec
	// Document's generic JSON representation uses binary64 numbers. Refuse
	// retention values above its exact-integer range rather than returning a
	// rounded policy as though the declaration had been preserved.
	for _, keep := range []uint64{spec.Retention.KeepDaily, spec.Retention.KeepWeekly, spec.Retention.KeepMonthly} {
		if keep > 1<<53-1 {
			return PolicyReport{}, policyCron{}, policyInvalid("retention count exceeds the exact JSON integer range")
		}
	}
	if err := validatePolicySelection(spec.Selector); err != nil {
		return PolicyReport{}, policyCron{}, err
	}
	cron, err := parsePolicyCron(spec.Schedule)
	if err != nil {
		return PolicyReport{}, policyCron{}, err
	}
	if !cron.calendarPossible() {
		return PolicyReport{}, policyCron{}, policyInvalid("cron has no possible Gregorian calendar date")
	}
	return PolicyReport{
		APIVersion: domain.APIVersion, Valid: true, ValidationScope: "declaration",
		PolicyName: declaration.Metadata.Name, RepositoryRef: spec.RepositoryRef,
		Selector: spec.Selector, Schedule: spec.Schedule, Capture: spec.Capture,
		Retention: spec.Retention, Verification: spec.Verification,
		NextRuns: []time.Time{}, HostPreflight: "not-run",
	}, cron, nil
}

func validatePolicySelection(selector PolicySelector) error {
	items := selector.VMIDs
	if len(items) == 0 {
		items = selector.Tags
	}
	if len(items) > policySelectorLimit {
		return policyInvalid("backup policy selector exceeds 4096 entries")
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != item || item == "" {
			return policyInvalid("backup policy selector contains an empty or surrounding-whitespace value")
		}
		for _, r := range item {
			if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
				return policyInvalid("backup policy selector contains control or formatting characters")
			}
		}
		if seen[item] {
			return policyInvalid("backup policy selector contains a duplicate value")
		}
		seen[item] = true
	}
	return nil
}

func parsePolicyCron(schedule PolicySchedule) (policyCron, error) {
	zone := schedule.Timezone
	if zone == "" || zone == "Local" || zone == "localtime" || zone == "posixrules" ||
		len(zone) > 256 || strings.TrimSpace(zone) != zone ||
		path.IsAbs(zone) || path.Clean(zone) != zone || strings.Contains(zone, "\\") {
		return policyCron{}, policyInvalid("explicit named timezone required; host-local and path aliases are not accepted")
	}
	for _, r := range zone {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return policyCron{}, policyInvalid("timezone contains control or formatting characters")
		}
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return policyCron{}, policyInvalid("named timezone cannot be resolved: " + zone)
	}
	if len(schedule.Cron) == 0 || len(schedule.Cron) > policyCronLimit {
		return policyCron{}, policyInvalid("cron must contain at most 512 bytes")
	}
	for _, r := range schedule.Cron {
		if r != '\t' && (r < ' ' || r > '~') {
			return policyCron{}, policyInvalid("cron accepts ASCII syntax with space/tab separators only")
		}
	}
	parts := strings.Fields(schedule.Cron)
	if len(parts) != 5 {
		return policyCron{}, policyInvalid("cron requires exactly five fields: minute hour day-of-month month day-of-week")
	}
	cron := policyCron{location: location}
	for i, field := range []struct {
		target *policyField
		min    int
		max    int
		names  string
	}{
		{&cron.minute, 0, 59, ""}, {&cron.hour, 0, 23, ""}, {&cron.day, 1, 31, ""},
		{&cron.month, 1, 12, "JAN FEB MAR APR MAY JUN JUL AUG SEP OCT NOV DEC"},
		{&cron.weekday, 0, 6, "SUN MON TUE WED THU FRI SAT"},
	} {
		parsed, err := parsePolicyField(parts[i], field.min, field.max, field.names)
		if err != nil {
			return policyCron{}, policyInvalid(fmt.Sprintf("cron field %d: %s", i+1, err))
		}
		*field.target = parsed
	}
	return cron, nil
}

func parsePolicyField(text string, min, max int, names string) (policyField, error) {
	var result policyField
	terms := strings.Split(text, ",")
	if len(terms) > 64 {
		return result, fmt.Errorf("too many list items")
	}
	parseNumber := func(value string) (int, error) {
		for i, name := range strings.Fields(names) {
			if strings.EqualFold(value, name) {
				return min + i, nil
			}
		}
		if value == "" {
			return 0, fmt.Errorf("empty number")
		}
		for _, r := range value {
			if r < '0' || r > '9' {
				return 0, fmt.Errorf("unsupported number or name")
			}
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < min || n > max {
			return 0, fmt.Errorf("value outside %d..%d", min, max)
		}
		return n, nil
	}
	for _, term := range terms {
		step := 1
		pieces := strings.Split(term, "/")
		if len(pieces) > 2 || pieces[0] == "" {
			return result, fmt.Errorf("invalid range/step")
		}
		if len(pieces) == 2 {
			if pieces[1] == "" || strings.IndexFunc(pieces[1], func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
				return result, fmt.Errorf("step must be a positive decimal integer")
			}
			var err error
			step, err = strconv.Atoi(pieces[1])
			if err != nil || step < 1 {
				return result, fmt.Errorf("step must be a positive decimal integer")
			}
		}
		lo, hi := min, max
		if pieces[0] == "*" {
			result.star = true
		} else {
			rangeParts := strings.Split(pieces[0], "-")
			if len(rangeParts) > 2 {
				return result, fmt.Errorf("invalid range")
			}
			var err error
			lo, err = parseNumber(rangeParts[0])
			if err != nil {
				return result, err
			}
			if len(rangeParts) == 2 {
				hi, err = parseNumber(rangeParts[1])
				if err != nil || hi < lo {
					return result, fmt.Errorf("invalid or descending range")
				}
			} else if len(pieces) == 1 {
				hi = lo
			}
		}
		for value := lo; ; {
			result.bits |= uint64(1) << value
			if step > hi-value {
				break // Avoid overflow even for an enormous, valid positive step.
			}
			value += step
		}
	}
	return result, nil
}

func (cron policyCron) dayMatches(day int, weekday time.Weekday) bool {
	dom, dow := cron.day.contains(day), cron.weekday.contains(int(weekday))
	// Conventional cron: restricted day-of-month and weekday are alternatives;
	// a wildcard in either field requires both masks to match. */n is a wildcard.
	if cron.day.star || cron.weekday.star {
		return dom && dow
	}
	return dom || dow
}

func (cron policyCron) calendarPossible() bool {
	// Gregorian dates/weekdays repeat every 400 years. This rejects impossible
	// combinations without scanning unbounded future minutes or assuming Feb=28.
	for year := 2000; year < 2400; year++ {
		for month := time.January; month <= time.December; month++ {
			if !cron.month.contains(int(month)) {
				continue
			}
			days := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
			for day := 1; day <= days; day++ {
				if cron.dayMatches(day, time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Weekday()) {
					return true
				}
			}
		}
	}
	return false
}

// PreviewPolicy returns the next count occurrences strictly after the supplied
// instant, as UTC instants. It does not evaluate missed-run or capture actions.
func PreviewPolicy(raw []byte, after time.Time, count int) (PolicyReport, error) {
	return PreviewPolicyContext(context.Background(), raw, after, count)
}

// PreviewPolicyContext also allows the shared service to cancel bounded searches.
func PreviewPolicyContext(ctx context.Context, raw []byte, after time.Time, count int) (PolicyReport, error) {
	if err := ctx.Err(); err != nil {
		return PolicyReport{}, err
	}
	after = after.UTC()
	if count < 1 || count > MaxPolicyPreviewCount || after.IsZero() || after.Year() < 1 || after.Year() > 9991 {
		return PolicyReport{}, policyInvalid("preview requires 1..32 occurrences and a nonzero instant in years 1..9991")
	}
	report, cron, err := parsePolicy(raw)
	if err != nil {
		return PolicyReport{}, err
	}
	deadline := after.AddDate(PolicyPreviewYears, 0, 0)
	for instant, iterations := nextPolicyMinute(after, cron.location), 0; !instant.After(deadline); iterations++ {
		if iterations%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return PolicyReport{}, err
			}
		}
		local := instant.In(cron.location)
		if cron.month.contains(int(local.Month())) && cron.dayMatches(local.Day(), local.Weekday()) &&
			cron.hour.contains(local.Hour()) && cron.minute.contains(local.Minute()) {
			report.NextRuns = append(report.NextRuns, instant)
			if len(report.NextRuns) == count {
				if err := ctx.Err(); err != nil {
					return PolicyReport{}, err
				}
				report.PreviewAfter = &after
				return report, nil
			}
		}
		instant = nextPolicyMinute(instant, cron.location)
	}
	return PolicyReport{}, policyInvalid("requested occurrence preview exceeds the bounded eight-year horizon")
}

func nextPolicyMinute(after time.Time, location *time.Location) time.Time {
	// Advance actual instants, not reconstructed wall times: gaps are skipped and
	// both folds remain visible. Zone boundaries also handle historical sub-minute
	// offsets without assuming that local minute boundaries have UTC second zero.
	instant := after.UTC().Add(time.Nanosecond)
	for {
		local := instant.In(location)
		elapsed := time.Duration(local.Second())*time.Second + time.Duration(local.Nanosecond())
		if elapsed == 0 {
			return instant
		}
		next := instant.Add(time.Minute - elapsed)
		_, boundary := local.ZoneBounds()
		if !boundary.IsZero() && boundary.After(instant) && boundary.Before(next) {
			instant = boundary.UTC()
			continue
		}
		return next
	}
}
