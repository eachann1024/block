package updater

import (
	"regexp"
	"strconv"
	"strings"
)

// devBuildVersion is the value BuildVersion keeps when no version is injected (go run / go test).
const devBuildVersion = "1.0.379"

// BuildVersion is injected at link time via -ldflags "-X block/internal/updater.BuildVersion=1.0.xxx".
var BuildVersion = devBuildVersion

// BuildChannel is injected at link time via -ldflags "-X block/internal/updater.BuildChannel=nightly|stable".
var BuildChannel = "stable"

// ReleaseChannel controls whether the updater follows stable or nightly releases.
type ReleaseChannel string

const (
	ReleaseStable  ReleaseChannel = "stable"
	ReleaseNightly ReleaseChannel = "nightly"
)

// NormalizeReleaseChannel normalizes persisted or build-time strings into ReleaseStable or ReleaseNightly.
func NormalizeReleaseChannel(raw string) ReleaseChannel {
	switch ReleaseChannel(strings.ToLower(strings.TrimSpace(raw))) {
	case ReleaseNightly:
		return ReleaseNightly
	default:
		return ReleaseStable
	}
}

// BuildReleaseChannel returns the release channel embedded into this binary at build time.
func BuildReleaseChannel() ReleaseChannel {
	return NormalizeReleaseChannel(BuildChannel)
}

// IsPackagedBuild reports whether the build script injected a real version.
func IsPackagedBuild() bool {
	return BuildVersion != devBuildVersion
}

// Semver represents a parsed major.minor.patch semantic version.
type Semver struct {
	Major int
	Minor int
	Patch int
}

var semverTagRe = regexp.MustCompile(`(?i)^v?(\d+)\.(\d+)\.(\d+)(?:-[0-9a-z._-]+)?$`)

// ParseReleaseTag extracts the normalized "X.Y.Z" version, trimmed tag, and numeric Semver.
func ParseReleaseTag(raw string) (version string, tag string, sv Semver, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", Semver{}, false
	}
	m := semverTagRe.FindStringSubmatch(trimmed)
	if len(m) != 4 {
		return "", "", Semver{}, false
	}
	maj, err1 := strconv.Atoi(m[1])
	minV, err2 := strconv.Atoi(m[2])
	pat, err3 := strconv.Atoi(m[3])
	if err1 != nil || err2 != nil || err3 != nil {
		return "", "", Semver{}, false
	}
	ver := m[1] + "." + m[2] + "." + m[3]
	return ver, trimmed, Semver{Major: maj, Minor: minV, Patch: pat}, true
}

// CurrentVersion returns the normalized running application version.
func CurrentVersion() string {
	if ver, _, _, ok := ParseReleaseTag(BuildVersion); ok {
		return ver
	}
	return "1.0.379"
}

// CompareVersion returns -1 if a < b, 0 if a == b, and +1 if a > b.
func CompareVersion(a, b string) int {
	_, _, left, okA := ParseReleaseTag(a)
	_, _, right, okB := ParseReleaseTag(b)
	if !okA && !okB {
		return 0
	}
	if !okA {
		return -1
	}
	if !okB {
		return 1
	}
	if left.Major != right.Major {
		if left.Major < right.Major {
			return -1
		}
		return 1
	}
	if left.Minor != right.Minor {
		if left.Minor < right.Minor {
			return -1
		}
		return 1
	}
	if left.Patch != right.Patch {
		if left.Patch < right.Patch {
			return -1
		}
		return 1
	}
	return 0
}
