package version

import "strings"

// CompareVersions compares two semver-like version strings.
// Strips optional "v" prefix and build-time suffix (e.g. " (2026-07-24)").
// Pre-release versions (with "-" suffix) are considered older than the same release version.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func CompareVersions(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")
	// Strip build-time suffix like " (2026-07-24 10:30:00)"
	if idx := strings.Index(a, " ("); idx >= 0 {
		a = a[:idx]
	}
	if idx := strings.Index(b, " ("); idx >= 0 {
		b = b[:idx]
	}

	// Split off pre-release suffix (after first '-')
	aCore, aPre := splitPreRelease(a)
	bCore, bPre := splitPreRelease(b)

	aParts := strings.Split(aCore, ".")
	bParts := strings.Split(bCore, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		var aNum, bNum int
		if i < len(aParts) {
			aNum = parseVersionPart(aParts[i])
		}
		if i < len(bParts) {
			bNum = parseVersionPart(bParts[i])
		}
		if aNum < bNum {
			return -1
		}
		if aNum > bNum {
			return 1
		}
	}

	// Core versions are equal — compare pre-release
	// A version with a pre-release suffix is newer than the same version without one
	// (dev builds like "0.66.0-5-gabc" are commits after the "0.66.0" release).
	if aPre != "" && bPre == "" {
		return 1
	}
	if aPre == "" && bPre != "" {
		return -1
	}

	return 0
}

// IsDevBuild returns true if the version string indicates a development build
// (has a pre-release suffix like "-5-gabc" or is a short VCS hash / "dev").
func IsDevBuild(v string) bool {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.Index(v, " ("); idx >= 0 {
		v = v[:idx]
	}
	if v == "dev" {
		return true
	}
	if len(v) <= 7 && !strings.Contains(v, ".") {
		return true // short VCS hash like "g7702c47"
	}
	_, pre := splitPreRelease(v)
	return pre != ""
}

// splitPreRelease splits a version string into core and pre-release parts.
// e.g. "0.66.0-5-gabc" → ("0.66.0", "5-gabc")
// "0.66.0" → ("0.66.0", "")
func splitPreRelease(v string) (core, pre string) {
	if idx := strings.Index(v, "-"); idx >= 0 {
		return v[:idx], v[idx+1:]
	}
	return v, ""
}

// parseVersionPart parses a single numeric segment, ignoring non-numeric suffixes.
// e.g., "3" → 3, "0" → 0, "beta" → 0
func parseVersionPart(s string) int {
	var num int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		} else {
			break
		}
	}
	return num
}
