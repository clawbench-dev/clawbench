package platform

import (
	"debug/buildinfo"
	"fmt"
	"runtime"
	"slices"
	"strings"
)

// AMD64 ISA levels, in GOAMD64 terms. Go's names are v1/v2/v3/v4; v2 here is
// the plain v2 build, whose CPU list is SSE3/SSE4.1/SSE4.2/POPCNT.
const (
	amd64LevelV1 = 1
	amd64LevelV2 = 2
	amd64LevelV3 = 3
	amd64LevelV4 = 4
)

// CPUMinISA explains why a binary was judged too new, for the log line.
type CPUMinISA struct {
	Level   string   // "x86-64-v3"
	Missing []string // CPU features the binary needs but the host lacks
}

// String describes the requirement, naming the absent CPU flags so the log
// points at the hardware rather than at a version number.
func (m CPUMinISA) String() string {
	if len(m.Missing) == 0 {
		return m.Level
	}
	return m.Level + "（CPU 缺少 " + strings.Join(m.Missing, "、") + "）"
}

// CheckBinaryCPUCompatibility reports whether a Linux amd64 binary can run on
// this host. ok is false only for a positively-detected mismatch: a binary
// whose GOAMD64 level needs CPU features this machine lacks. Anything that
// cannot be inspected (another OS/arch, a non-Go binary, an unreadable file)
// is treated as compatible, because a false negative would pin users on an old
// version forever while a false positive only costs a failed restart.
func CheckBinaryCPUCompatibility(path string) (CPUMinISA, bool) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return CPUMinISA{}, true
	}
	level, err := requiredAMD64Level(path)
	if err != nil || level <= amd64LevelV2 {
		return CPUMinISA{}, true
	}
	host, ok := hostAMD64Features()
	if !ok {
		// The CPU's flags cannot be read (non-standard rootfs, chroot). An
		// unknown host is not evidence of a mismatch: blocking would pin every
		// user of such an environment on an old version.
		return CPUMinISA{}, true
	}
	missing := missingAMD64Features(level, host)
	if len(missing) == 0 {
		return CPUMinISA{}, true
	}
	return CPUMinISA{Level: amd64LevelName(level), Missing: missing}, false
}

// requiredAMD64Level reads the GOAMD64 the binary was built with from its Go
// build info — the same record `go version -m` prints.
func requiredAMD64Level(path string) (int, error) {
	bi, err := buildinfo.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, s := range bi.Settings {
		if s.Key != "GOAMD64" {
			continue
		}
		level, ok := amd64LevelFromRecord(s.Value)
		if !ok {
			return 0, fmt.Errorf("unrecognized GOAMD64 build setting %q", s.Value)
		}
		return level, nil
	}
	// Go always records GOAMD64 for amd64 binaries; its absence means the file
	// is not a Go amd64 build, which is not a CPU verdict.
	return 0, fmt.Errorf("no GOAMD64 build setting")
}

// amd64LevelFromRecord maps a GOAMD64 value onto its ISA level. "v2-lzcnt" is
// a v2 build whose runtime additionally uses LZCNT, so it requires LZCNT just
// like v3 does — the level callers compare must know that.
func amd64LevelFromRecord(rec string) (int, bool) {
	switch rec {
	case "v1":
		return amd64LevelV1, true
	case "v2":
		return amd64LevelV2, true
	case "v2-lzcnt":
		return amd64LevelLZCNT, true
	case "v3":
		return amd64LevelV3, true
	case "v4":
		return amd64LevelV4, true
	default:
		return 0, false
	}
}

// amd64LevelLZCNT is the synthetic level for "v2-lzcnt": a v2 build plus a
// mandatory LZCNT. It sits above v2 so the feature check actually runs.
const amd64LevelLZCNT = 21

// cpuinfoFlag records the /proc/cpuinfo spelling of the CPU features the levels
// below require. LZCNT is the one that matters: the kernel reports it as "abm"
// (the AMD ABM bit GCC's -mlzcnt requires), never as "lzcnt", so asking for
// "lzcnt" would make every v3 binary look incompatible on every Linux host.
var cpuinfoFlag = map[string]string{
	"lzcnt": "abm",
}

func cpuHas(host []string, feature string) bool {
	flag, ok := cpuinfoFlag[feature]
	if !ok {
		flag = feature
	}
	// Some kernels/patches print LZCNT directly; accept either spelling so a
	// capable CPU is never rejected over vocabulary.
	return slices.Contains(host, flag) || (flag != feature && slices.Contains(host, feature))
}

// missingAMD64Features returns the host gaps for the given level. The lists
// mirror Go's own CPU feature requirements per level; LZCNT is included only
// for the builds that require it, because Go's v2 build does not.
func missingAMD64Features(level int, host []string) []string {
	var required []string
	switch {
	case level == amd64LevelV4:
		required = []string{
			"avx", "avx2", "bmi1", "bmi2", "fma", "lzcnt",
			"avx512f", "avx512bw", "avx512cd", "avx512dq", "avx512vl",
		}
	case level == amd64LevelV3 || level == amd64LevelLZCNT:
		required = []string{"avx", "avx2", "bmi1", "bmi2", "fma", "lzcnt"}
	case level >= amd64LevelV2:
		required = []string{"sse3", "sse4_1", "sse4_2", "popcnt"}
	}
	missing := make([]string, 0, len(required))
	for _, feature := range required {
		if !cpuHas(host, feature) {
			missing = append(missing, feature)
		}
	}
	return missing
}

func amd64LevelName(level int) string {
	switch {
	case level == amd64LevelV4:
		return "x86-64-v4"
	case level == amd64LevelV3:
		return "x86-64-v3"
	case level == amd64LevelLZCNT:
		return "x86-64-v2-lzcnt"
	case level >= amd64LevelV2:
		return "x86-64-v2"
	default:
		return "x86-64"
	}
}
