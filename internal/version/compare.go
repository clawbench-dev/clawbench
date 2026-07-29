package version

import "strings"

// CompareVersions compares two semver-like version strings.
// Strips optional "v" prefix and build-time suffix (e.g. "-07241030" mmddHHMM format).
// Pre-release versions (with "-" suffix) are considered older than the same release version.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func CompareVersions(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")
	// Strip build-time suffix like "-07241030" (mmddHHMM format, 8 digits after last '-')
	a = stripBuildTimeSuffix(a)
	b = stripBuildTimeSuffix(b)

	// Split off pre-release suffix (after first '-')
	aCore, aPre := splitPreRelease(a)
	bCore, bPre := splitPreRelease(b)

	aParts := strings.Split(aCore, ".")
	bParts := strings.Split(bCore, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := range maxLen {
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
// (has a pre-release suffix like "-5-gabc" or is a short git VCS hash / "dev").
func IsDevBuild(v string) bool {
	v = strings.TrimPrefix(v, "v")
	// Strip mmddHHMM build-time suffix
	v = stripBuildTimeSuffix(v)
	if v == "dev" {
		return true
	}
	// Git short hash format: 7+ hex chars, optionally prefixed with 'g'
	if isGitHash(v) {
		return true
	}
	_, pre := splitPreRelease(v)
	return pre != ""
}

// stripBuildTimeSuffix removes a trailing "-MMDDHHMM" build-time suffix (8 digits after last '-').
// e.g. "v0.70.0-5-g830bb6c-07291030" → "v0.70.0-5-g830bb6c"
// "v1.0.0-07291030" → "v1.0.0"
// "v0.70.0-5-g830bb6c" → "v0.70.0-5-g830bb6c" (no change, not a build-time suffix)
func stripBuildTimeSuffix(v string) string {
	idx := strings.LastIndex(v, "-")
	if idx < 0 {
		return v
	}
	suffix := v[idx+1:]
	if len(suffix) == 8 && isAllDigits(suffix) {
		return v[:idx]
	}
	return v
}

// isAllDigits checks if the string consists entirely of digits.
func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isGitHash checks if the string looks like a git VCS short hash
// (7+ hex characters, optionally prefixed with 'g').
func isGitHash(v string) bool {
	s := v
	if s != "" && s[0] == 'g' {
		s = s[1:]
	}
	if len(s) < 7 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
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
