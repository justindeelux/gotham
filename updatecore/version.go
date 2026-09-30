package updatecore

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed semver-ish version: major.minor.patch with an optional
// prerelease suffix (everything after the first hyphen, before build metadata).
// It intentionally ignores build metadata ("+...").
type Version struct {
	Major int
	Minor int
	Patch int
	Pre   string
}

// ParseVersion parses "v1.2.3", "1.2.3-rc.1" and shorter "1.2"/"1" forms.
func ParseVersion(raw string) (Version, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return Version{}, fmt.Errorf("updates: empty version")
	}
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i]
	}
	var pre string
	if i := strings.IndexByte(s, '-'); i >= 0 {
		pre = s[i+1:]
		s = s[:i]
	}

	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return Version{}, fmt.Errorf("updates: invalid version %q", raw)
	}
	var numbers [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return Version{}, fmt.Errorf("updates: invalid version %q", raw)
		}
		numbers[i] = n
	}
	return Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2], Pre: pre}, nil
}

// Compare returns -1, 0 or 1 when v orders before, equal to, or after other.
// A release outranks a prerelease of the same major.minor.patch.
func (v Version) Compare(other Version) int {
	pairs := [][2]int{{v.Major, other.Major}, {v.Minor, other.Minor}, {v.Patch, other.Patch}}
	for _, pair := range pairs {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	return comparePrerelease(v.Pre, other.Pre)
}

// comparePrerelease orders prerelease strings per semver: absence outranks
// presence, numeric identifiers compare numerically and rank below alphanumeric
// ones, and a shorter identifier list ranks below a longer equal prefix.
func comparePrerelease(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aErr := strconv.Atoi(as[i])
		bn, bErr := strconv.Atoi(bs[i])
		switch {
		case aErr == nil && bErr == nil:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		default:
			if as[i] != bs[i] {
				if as[i] < bs[i] {
					return -1
				}
				return 1
			}
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	default:
		return 0
	}
}

// IsPrerelease reports whether the version carries a prerelease suffix.
func (v Version) IsPrerelease() bool { return v.Pre != "" }

// String renders the version with a leading "v".
func (v Version) String() string {
	base := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		return base + "-" + v.Pre
	}
	return base
}
