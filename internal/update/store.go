package update

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	checkEvery = 24 * time.Hour
	retryAfter = 6 * time.Hour
)

// Paths are the per-user directories the updater writes to.
type Paths struct{ Config, Cache string }

func UserPaths() (Paths, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, err
	}
	return Paths{Config: filepath.Join(config, "virmill"), Cache: filepath.Join(cache, "virmill")}, nil
}

type settings struct {
	Checks *bool `json:"checks,omitempty"`
}

// ChecksEnabled reports whether daily checks are on. VIRMILL_UPDATE_CHECK=0
// or `virmill update checks off` turns them off; they are on by default.
func (p Paths) ChecksEnabled() bool {
	switch os.Getenv("VIRMILL_UPDATE_CHECK") {
	case "0", "off", "false":
		return false
	case "1", "on", "true":
		return true
	}
	var s settings
	if b, err := os.ReadFile(filepath.Join(p.Config, "updates.json")); err == nil && json.Unmarshal(b, &s) == nil && s.Checks != nil {
		return *s.Checks
	}
	return true
}

func (p Paths) SetChecks(on bool) error {
	return writePrivate(p.Config, "updates.json", settings{Checks: &on})
}

// Cached returns the last saved check, if any.
func (p Paths) Cached() (Result, bool) {
	var r Result
	b, err := os.ReadFile(filepath.Join(p.Cache, "update-check.json"))
	if err != nil || json.Unmarshal(b, &r) != nil {
		return Result{}, false
	}
	return r, true
}

func (p Paths) Save(r Result) error { return writePrivate(p.Cache, "update-check.json", r) }

// writePrivate replaces dir/name with JSON readable only by this user.
func writePrivate(dir, name string, v any) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+name+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Chmod(0600)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, name))
}

// Due reports whether a check should run: none is saved, it was for another
// version, or it is older than a day (six hours after a failed check).
func Due(r Result, ok bool, current string, now time.Time) bool {
	wait := checkEvery
	if r.Error != "" {
		wait = retryAfter
	}
	return !ok || r.Current != current || now.Sub(r.CheckedAt) >= wait || r.CheckedAt.After(now.Add(time.Hour))
}

// CheckIfDue checks when the saved result is stale and saves the outcome,
// failures included, so an offline host is not retried on every command.
func CheckIfDue(ctx context.Context, c Client, p Paths, current string, now time.Time) Result {
	cached, ok := p.Cached()
	if !Due(cached, ok, current, now) {
		return cached
	}
	fresh, err := c.Check(ctx, current)
	if err != nil {
		fresh = Result{Current: current, CheckedAt: now.UTC(), Error: Explain(err)}
		if ok && cached.Current == current {
			fresh.Latest = cached.Latest
		}
	}
	_ = p.Save(fresh)
	return fresh
}

// Explain turns a check error into a sentence for people.
func Explain(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "GitHub did not answer in time"
	}
	return err.Error()
}
