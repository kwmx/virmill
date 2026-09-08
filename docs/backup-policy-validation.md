# BackupPolicy validation and occurrence preview

BackupPolicy validation checks the supplied declaration offline. An occurrence
preview lists future matching instants without installing a schedule, accessing
a repository, selecting actual VMs, using credentials or starting a backup.

The shared interfaces use `backup policy validate PATH`,
`backup policy preview PATH`, and the existing `config validate PATH` for a
BackupPolicy document. Preview input contains an optional RFC3339 `after` instant
and `count`; the service defaults to five occurrences when count is omitted.
For example:

```text
virmill backup policy validate policy.yaml --output json --non-interactive
virmill backup policy preview policy.yaml --input '{"after":"2026-09-08T00:00:00Z","count":5}' --output json --non-interactive
```

The validator accepts one bounded UTF-8 JSON or YAML document through the existing
bundled document validator. It retains strict schema behavior, including unknown
fields, duplicate JSON/YAML keys, aliases, multiple documents and unsupported
declarative kinds. VM-ID and tag selectors remain mutually exclusive. Selector
values must be nonempty, distinct and free of surrounding whitespace, control or
Unicode formatting characters. Internal spaces and ordinary Unicode remain
allowed. Selectors have a 4096-entry limit and do not resolve VM existence or
repository ownership.

The report preserves the declared cron text, timezone, missed-run policy,
repository and selector, capture mode/consistency/shutdown permission, retention
and verification settings. It is a validation report, not a persisted execution
recipe or a round-trip representation of extension fields. Retention counts must
fit the exact JSON integer range, 0 through 9007199254740991, so oversized values
cannot silently round through the shared document representation.

## Supported cron grammar

The normative aid requires five cron fields and an explicit timezone. This
implementation completes their parsing with the following documented dialect;
it does not add an external scheduler dependency.

```text
minute hour day-of-month month day-of-week
```

| Field | Values |
|---|---|
| Minute | 0–59 |
| Hour | 0–23 |
| Day of month | 1–31 |
| Month | 1–12 or JAN–DEC |
| Day of week | 0–6 or SUN–SAT; Sunday is 0 |

Each field supports `*`, individual values, comma-separated lists, ascending
inclusive ranges, and positive decimal steps. Examples are `*/15`, `9-17`,
`JAN,MAR`, `MON-FRI`, and `5/10` (from 5 to that field's maximum in steps of 10).
Names are ASCII and case-insensitive. A step larger than the remaining field
range selects only its starting value; integer overflow is refused or avoided.
Spaces and tabs separate fields. The complete expression is limited to 512 bytes,
and each field to 64 list entries.

Seconds/year fields, `@daily` and other descriptors, embedded `TZ=`/`CRON_TZ=`
directives, descending/wrapping ranges, signed or zero steps, day-of-week 7,
`?`, `L`, `W`, `#`, newlines and non-ASCII cron syntax are refused. Use the separate
timezone property and Sunday 0/SUN.

When both day-of-month and day-of-week are restricted, a date matching either
field is eligible. When either contains a wildcard, including `*/n`, both field
masks must match. Thus `0 0 31 FEB *` is impossible, while `0 0 31 FEB MON` means
the Mondays in February. Calendar feasibility uses the complete 400-year
Gregorian cycle, including century leap-year rules. Validation never treats
February 31 as a future date by normalizing it into March.

## Timezones and bounded preview

The timezone must resolve by its explicit name, for example `Asia/Riyadh`,
`America/New_York`, or `UTC`. It is preserved without trimming or replacing it
with a local default. `Local`, `localtime`, `posixrules`, absolute paths, path
aliases, surrounding whitespace, controls and formatting characters are refused.
The standard-library timezone database is embedded as a fallback when the host
has no tzdata package. Available host/Go timezone data can change with their
versions; a future scheduler must generate and recheck each occurrence's plan.

Preview requires 1–32 occurrences and a nonzero input instant whose UTC year is
1–9991. Results are strictly after that instant and contain UTC timestamps.
The search ends eight calendar years after the supplied instant. If the requested
count does not fit the horizon, preview returns an error and no partial report.
A valid yearly policy can therefore validate successfully while a request for
32 future occurrences exceeds the preview horizon.

The preview follows actual instants through timezone transitions. A nonexistent
wall time during a spring-forward gap is skipped; both instances of a repeated
wall time during a fall-back fold are returned separately. Half-hour transitions
and historical offsets with seconds are handled without assuming that every
local minute begins at UTC second zero. Gregorian leap years are observed:
February 29 after March 2097 next occurs in 2104, not 2100.

This is an occurrence list, not a promise that a backup job will run twice in a
fold. The actual scheduler still must enforce overlap exclusion, fresh plans and
its durable occurrence/missed-run rules. The declared `skip` or `run-once` value
is retained but is not executed by a preview. No missed interval is replayed and
no overdue record is created here.

## Report and error boundaries

Successful reports contain `valid: true`, `validationScope: "declaration"`, the
preserved policy fields, `nextRuns`, and optional UTC `previewAfter`. They also
state:

```json
{
  "hostPreflight": "not-run",
  "scheduleInstalled": false,
  "captureVerified": false,
  "independentRecoveryVerified": false
}
```

Invalid declarations, unsupported cron grammar, unresolved timezone names,
impossible calendar expressions and preview-bound violations return typed
`INVALID_INPUT` errors. Context cancellation propagates without a successful or
partial report; shared service callers must emit null data on errors.

The Go entry points are:

```go
func ValidatePolicy(raw []byte) (PolicyReport, error)
func PreviewPolicy(raw []byte, after time.Time, count int) (PolicyReport, error)
func PreviewPolicyContext(ctx context.Context, raw []byte, after time.Time, count int) (PolicyReport, error)
```

The context-aware method checks cancellation before validation and during the
bounded search, at most every 1024 candidate minutes, as well as before returning
success. A validation result contains no job ID, plan digest or standing grant.

All-zero retention is permitted by the schema; protecting the last known-good
backup belongs to retention execution. Cold capture without shutdown permission
may be valid for an already stopped VM. Requested application consistency remains
requested until actual host/guest preflight proves it; validation never reduces
it. `testRestore: scheduled` is a preserved request and does not install a restore
schedule. Repository availability, locked credentials, encryption, removable
storage identity, service persistence, missed-run coalescing, overlap locks,
capture completeness, pruning safety and independent restore remain separate
mandatory work.

## Verification scope

The focused tests use supplied declarative examples and generated in-memory
documents/times. They cover JSON/YAML equivalence; invalid schema and selectors;
cron grammar/ranges/steps/bounds; named-zone refusal; impossible dates; UTC output;
New York spring/fall transitions; Lord Howe's half-hour fold; Monrovia's historical
sub-minute offset; the non-leap year 2100; context cancellation; horizon failure
without partial data; and preservation of capture/retention/verification intent.

```sh
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor ./internal/app/protection -run '^TestBackupPolicy' -count=1
GOPROXY=off GOSUMDB=off ./scripts/go test -mod=vendor -race ./internal/app/protection -run '^TestBackupPolicy' -count=1
```

These checks contribute BAK-05, REL-03 and shared-interface UX-01/UX-03
prerequisites. They do not complete scheduled capture/reboot qualification,
retention verification, backup capture or independent recovery acceptance.

On 2026-09-08 the initial focused normal run passed in 0.419 seconds. After the
final selector-bound and host-local timezone regressions were added, all ten
focused test functions passed with the race detector in 4.315 seconds, with no
skips. No native, host-mutation, SSH or IPC test was used for this contribution.
Shared service and CLI/TUI integration tests are recorded separately.
