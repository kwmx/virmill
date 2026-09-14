// Package update finds newer Virmill releases on GitHub and prepares their
// packages for installation. It runs in the user's CLI or TUI, never in the
// coordinator, and it never installs anything itself: the caller runs the
// distribution's package manager with the verified files.
package update

import (
	"fmt"
	"regexp"
	"strconv"
)

var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})(?:-beta\.([1-9][0-9]{0,8}))?$`)

// Version is a Virmill product version: MAJOR.MINOR.PATCH with an optional -beta.N.
type Version struct{ Major, Minor, Patch, Beta int }

func ParseVersion(s string) (Version, error) {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("not a Virmill version: %q", s)
	}
	n := func(s string) int {
		v, _ := strconv.Atoi(s)
		return v
	}
	v := Version{Major: n(m[1]), Minor: n(m[2]), Patch: n(m[3])}
	if m[4] != "" {
		v.Beta = n(m[4])
	}
	return v, nil
}

func (v Version) Prerelease() bool { return v.Beta > 0 }

func (v Version) base() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

func (v Version) String() string {
	if v.Prerelease() {
		return fmt.Sprintf("%s-beta.%d", v.base(), v.Beta)
	}
	return v.base()
}

// Less orders versions; a release sorts after its betas.
func (v Version) Less(o Version) bool {
	for _, d := range [3]int{v.Major - o.Major, v.Minor - o.Minor, v.Patch - o.Patch} {
		if d != 0 {
			return d < 0
		}
	}
	rank := func(x Version) int {
		if x.Beta == 0 {
			return int(^uint(0) >> 1)
		}
		return x.Beta
	}
	return rank(v) < rank(o)
}

// rpmVersion and debVersion are the versions scripts/package.py writes into the
// packages. Only betas have a defined package version so far.
func (v Version) rpmVersion() (version, release string, ok bool) {
	if !v.Prerelease() {
		return "", "", false
	}
	return v.base(), fmt.Sprintf("0.beta.%d", v.Beta), true
}

func (v Version) debVersion() (string, bool) {
	if !v.Prerelease() {
		return "", false
	}
	return fmt.Sprintf("%s~beta.%d", v.base(), v.Beta), true
}
